package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/gateway"
	"paracetamol/internal/project"
	"paracetamol/tools/llama-acceptance/internal/coding"
	"paracetamol/tools/llama-acceptance/internal/contract"
	"paracetamol/tools/llama-acceptance/internal/scenarios"
	"paracetamol/tools/llama-acceptance/internal/transport"
)

type summary struct {
	Control          string            `json:"diagnostic_control,omitempty"`
	Schema           string            `json:"schema"`
	Suite            string            `json:"suite"`
	Model            string            `json:"model"`
	StartedAt        string            `json:"started_at"`
	FinishedAt       string            `json:"finished_at"`
	Complete         bool              `json:"complete"`
	Status           contract.Status   `json:"status"`
	Fixtures         map[string]string `json:"fixtures"`
	ToolSHA256       string            `json:"tool_sha256"`
	Instance         string            `json:"gateway_instance"`
	Inventory        string            `json:"gateway_inventory"`
	Backend          backendEvidence   `json:"backend"`
	GraderImage      string            `json:"grader_image,omitempty"`
	PrefillTolerance float64           `json:"prefill_regression_percent"`
	Evidence         map[string]string `json:"evidence_sha256,omitempty"`
	Cases            []contract.Result `json:"cases"`
	Error            string            `json:"error,omitempty"`
}

func runGatewayChecks(ctx context.Context, operation string, opts runOptions, stdout io.Writer) (returnedErr error) {
	fixtures, err := fixtureManifest()
	if err != nil {
		return err
	}
	origin, err := gatewayOrigin(opts.endpoint)
	if err != nil {
		return err
	}
	opts.endpoint = origin + "/v1"
	if !filepath.IsAbs(opts.dataRoot) {
		return fmt.Errorf("--data-dir must be the existing absolute data root")
	}
	root, err := project.Root()
	if err != nil {
		return err
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		return err
	}
	key, err := readKey(opts.keyFile)
	if err != nil {
		return err
	}
	backend, err := captureBackend(ctx, root, opts, managed)
	if err != nil {
		return err
	}
	status, err := gateway.FetchStatus(ctx, opts.endpoint, key, 0)
	if err != nil {
		return err
	}
	if status.Scheduler.ActiveRequests != 0 || status.Scheduler.QueuedRequests != 0 {
		return fmt.Errorf("use an idle dedicated acceptance gateway, not one serving other work")
	}
	clientOptions := transport.Options{GatewayURL: opts.endpoint, Key: key, Model: opts.model, Catalog: &managed, NativeURL: backend.NativeURL}
	if operation == "preflight" {
		client, err := transport.New(ctx, clientOptions)
		if err != nil {
			return err
		}
		defer client.Close()
		_, stopKernel, err := startKernelGuard(ctx, "")
		if err != nil {
			return err
		}
		if err := stopKernel(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "PASS  tracked fixtures, gateway policy, local receipts, exact backend image and runtime\nModel  %s\nImage  %s\nNo inference run and no output created.\n", opts.model, backend.ImageID)
		return nil
	}
	if (opts.suite == "coding" || opts.suite == "all") && opts.graderImage == "" {
		return fmt.Errorf("coding/all requires --grader-image from explicit prepare-grader and image build")
	}
	dir, err := createOutput(root, opts.output)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	toolBytes, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	record := summary{Schema: contract.Version, Suite: opts.suite, Model: opts.model, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: contract.Blocked, Fixtures: fixtures, ToolSHA256: hashBytes(toolBytes), Instance: status.InstanceID, Inventory: status.InventoryFingerprint, Backend: backend, GraderImage: opts.graderImage, Cases: []contract.Result{}}
	record.PrefillTolerance = opts.prefillTolerance
	record.Control = opts.control
	if err := savePrivateJSON(filepath.Join(dir, "manifest.json"), record); err != nil {
		return err
	}
	defer func() {
		record.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		record.Status = contract.Worst(record.Cases)
		if returnedErr != nil {
			record.Complete = false
			record.Error = returnedErr.Error()
			if record.Status == contract.Pass {
				record.Status = contract.Blocked
			}
		}
		files, err := evidenceFiles(dir)
		if err != nil {
			record.Status, record.Complete = contract.Blocked, false
			returnedErr = errors.Join(returnedErr, err)
		} else {
			record.Evidence = files
		}
		if err := savePrivateJSON(filepath.Join(dir, "summary.json"), record); err != nil {
			returnedErr = errors.Join(returnedErr, err)
		}
		fmt.Fprintf(stdout, "%s  suite %s\nEvidence  %s\n", record.Status, opts.suite, dir)
		if returnedErr == nil && record.Status != contract.Pass {
			returnedErr = errIncomplete
		}
	}()
	clientOptions.OutputDir = dir
	client, err := transport.New(ctx, clientOptions)
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, stopKernel, err := startKernelGuard(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { returnedErr = errors.Join(returnedErr, stopKernel()) }()
	ctx, stopGuard := memoryGuard(ctx, 8<<30)
	defer stopGuard()
	var mu sync.Mutex
	classifications := map[string]string{}
	var terminalError error
	call := func(callCtx context.Context, name string, request map[string]any) (contract.Response, error) {
		mu.Lock()
		if terminalError != nil {
			err := terminalError
			classifications[name] = transportErrorKind(err)
			mu.Unlock()
			return contract.Response{}, err
		}
		fmt.Fprintf(stdout, "Running  %s\n", name)
		mu.Unlock()
		response, err := client.Call(callCtx, name, request)
		kind := transportErrorKind(err)
		if kind != "" && !errors.Is(err, context.Canceled) {
			mu.Lock()
			classifications[name] = kind
			if response.RequestID != "" {
				classifications[response.RequestID] = kind
			}
			if kind != transport.OutputLimit {
				terminalError = err
			}
			mu.Unlock()
		}
		return response, err
	}
	appendResults := func(results []contract.Result) {
		mu.Lock()
		for i := range results {
			kind := classifications[results[i].Name]
			for _, id := range results[i].RequestIDs {
				if classifications[id] != "" {
					kind = classifications[id]
				}
			}
			if results[i].Status == contract.Fail && kind == transport.Infrastructure {
				results[i].Status = contract.Blocked
			}
			if results[i].Status == contract.Fail && kind == transport.OutputLimit {
				results[i].Status = contract.Review
			}
		}
		mu.Unlock()
		record.Cases = append(record.Cases, results...)
		printResults(stdout, results)
	}
	options := contract.Options{Model: opts.model, Slots: client.Slots, Tokenize: client.Tokenize}
	if opts.suite == "short" || opts.suite == "all" {
		appendResults(scenarios.Short(ctx, options, call))
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	// Deterministic failures stop expensive work. A separately classified coding
	// miss remains REVIEW, not permission to discard it and retry until green.
	if len(record.Cases) != 0 && (contract.Worst(record.Cases) == contract.Fail || contract.Worst(record.Cases) == contract.Blocked) {
		return errIncomplete
	}
	if opts.suite == "coding" || opts.suite == "all" {
		selfTests, selfErr := coding.SelfTest(ctx, coding.Options{ImageID: opts.graderImage, OutputDir: filepath.Join(dir, "grader-selftest")})
		appendResults(selfTests)
		if selfErr != nil {
			return selfErr
		}
		if contract.Worst(selfTests) != contract.Pass {
			return errIncomplete
		}
		appendResults(codingCases(ctx, dir, opts, call))
	}
	if len(record.Cases) != 0 && (contract.Worst(record.Cases) == contract.Fail || contract.Worst(record.Cases) == contract.Blocked) {
		return errIncomplete
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if opts.suite == "history" || opts.suite == "all" {
		appendResults(scenarios.History(ctx, options, call))
	}
	if len(record.Cases) != 0 && (contract.Worst(record.Cases) == contract.Fail || contract.Worst(record.Cases) == contract.Blocked) {
		return errIncomplete
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if opts.suite == "prefill" || opts.suite == "all" {
		appendResults(scenarios.Prefill(ctx, options, call))
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	final, err := captureBackend(ctx, root, opts, managed)
	if err != nil || final.ContainerID != backend.ContainerID || final.ImageID != backend.ImageID || !equalJSON(final.Arguments, backend.Arguments) {
		return fmt.Errorf("backend identity or native policy changed during the suite")
	}
	record.Complete = true
	return nil
}

func codingCases(ctx context.Context, dir string, opts runOptions, call contract.Call) []contract.Result {
	var results []contract.Result
	for _, task := range coding.Tasks() {
		messages := []any{map[string]any{"role": "user", "content": task.Prompt}}
		request := func(seed int, history []any) map[string]any {
			return map[string]any{"model": opts.model, "messages": history, "reasoning_effort": "xhigh", "seed": seed, "cache_prompt": false, "stream": false, "max_tokens": task.MaxTokens}
		}
		first, err := call(ctx, task.ID+"-first", request(42, messages))
		feedback := "No complete code was available to grade."
		grade := func(name string, response contract.Response, callErr error) bool {
			result := contract.Result{Name: name, Status: contract.Review, RequestIDs: []string{response.RequestID}}
			if callErr != nil {
				result.Detail = callErr.Error()
				kind := transportErrorKind(callErr)
				if kind == transport.Infrastructure {
					result.Status = contract.Blocked
				} else if kind == transport.Protocol || kind == transport.Policy {
					result.Status = contract.Fail
				}
				results = append(results, result)
				return false
			}
			if response.Finish != "stop" || len(response.Tools) != 0 {
				result.Detail, feedback = "expected a naturally completed code answer without tool calls", "The response did not finish with a complete standalone code answer."
				results = append(results, result)
				return false
			}
			code, err := extractCode(response.Text)
			if err != nil {
				result.Detail, feedback = err.Error(), err.Error()
				results = append(results, result)
				return false
			}
			graded, gradeErr := coding.Grade(ctx, coding.Options{ImageID: opts.graderImage, OutputDir: filepath.Join(dir, "grade-"+name)}, task.ID, code)
			if gradeErr != nil {
				result.Status, result.Detail = contract.Blocked, gradeErr.Error()
			} else if graded.Passed {
				result.Status, result.Detail = contract.Pass, graded.Detail
			} else {
				result.Detail, feedback = graded.Detail, graded.Detail
			}
			results = append(results, result)
			return result.Status == contract.Pass
		}
		passed := grade(task.ID+"-first", first, err)
		if ctx.Err() != nil {
			return results
		}
		if last := results[len(results)-1]; last.Status == contract.Fail || last.Status == contract.Blocked {
			return results
		}
		if !passed {
			if first.Message != nil {
				// One predeclared repair gets bounded actual verifier feedback and
				// the entire assistant object, never a reference implementation.
				if len(feedback) > 2048 {
					feedback = feedback[:2048]
				}
				repairHistory := append(append([]any{}, messages...), first.Message, map[string]any{"role": "user", "content": "The verifier reports: " + feedback + "\nReturn a corrected program under the unchanged contract and JSON format."})
				repaired, repairErr := call(ctx, task.ID+"-repair", request(42, repairHistory))
				grade(task.ID+"-repair", repaired, repairErr)
				if last := results[len(results)-1]; last.Status == contract.Fail || last.Status == contract.Blocked {
					return results
				}
			}
			for _, seed := range []int{1234, 2026} {
				if ctx.Err() != nil {
					return results
				}
				name := fmt.Sprintf("%s-fresh-%d", task.ID, seed)
				response, freshErr := call(ctx, name, request(seed, messages))
				grade(name, response, freshErr)
				if last := results[len(results)-1]; last.Status == contract.Fail || last.Status == contract.Blocked {
					return results
				}
			}
		}
	}
	return results
}

func extractCode(text string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') || !decoder.More() {
		return "", fmt.Errorf("expected one JSON object containing only code")
	}
	key, err := decoder.Token()
	if err != nil || key != "code" {
		return "", fmt.Errorf("expected only JSON code field")
	}
	var code string
	if err := decoder.Decode(&code); err != nil || code == "" || len(code) > 256<<10 {
		return "", fmt.Errorf("code must be a nonempty string of at most 256 KiB")
	}
	if decoder.More() {
		return "", fmt.Errorf("unexpected or duplicate JSON field")
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return "", fmt.Errorf("incomplete JSON code object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("unexpected text after JSON code object")
	}
	return code, nil
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// Joined response, report and persistence errors are independent evidence.
// Inspect every branch so an earlier output ceiling cannot hide bad policy or
// missing reports and accidentally authorize additional inference attempts.
func transportErrorKind(err error) string {
	rank := map[string]int{transport.OutputLimit: 1, transport.Infrastructure: 2, transport.Protocol: 3, transport.Policy: 4}
	best := ""
	var visit func(error)
	visit = func(current error) {
		if current == nil {
			return
		}
		if typed, ok := current.(*transport.Error); ok && rank[typed.Kind] > rank[best] {
			best = typed.Kind
		}
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			for _, child := range joined.Unwrap() {
				visit(child)
			}
		} else if wrapped, ok := current.(interface{ Unwrap() error }); ok {
			visit(wrapped.Unwrap())
		}
	}
	visit(err)
	return best
}

func memoryGuard(parent context.Context, minimum int64) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	available, err := availableMemory()
	if err != nil || available < minimum {
		if err != nil {
			cancel(fmt.Errorf("memory guard cannot observe available RAM: %w", err))
		} else {
			cancel(fmt.Errorf("memory guard stopped acceptance below %d available bytes", minimum))
		}
		return ctx, func() { cancel(context.Canceled) }
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			available, err := availableMemory()
			if err != nil || available < minimum {
				if err != nil {
					cancel(fmt.Errorf("memory guard cannot observe available RAM: %w", err))
				} else {
					cancel(fmt.Errorf("memory guard stopped acceptance below %d available bytes", minimum))
				}
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled); <-done }
}

func availableMemory() (int64, error) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		var kib int64
		if _, err := fmt.Sscanf(line, "MemAvailable: %d kB", &kib); err == nil && kib > 0 {
			return kib * 1024, nil
		}
	}
	return 0, fmt.Errorf("host available-memory observation unavailable")
}
