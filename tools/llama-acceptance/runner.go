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
	if err := validateGatewayBackend(status, backend); err != nil {
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
		if info := client.Info(); info.InstanceID != backend.GatewayInstance || info.InventoryFingerprint != status.InventoryFingerprint {
			return fmt.Errorf("gateway identity or inventory changed during preflight")
		}
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
	if opts.trace {
		record.Cases = append(record.Cases, contract.Result{Name: "diagnostic-trace-only", Status: contract.Review, Detail: "instrumented dispatch evidence only; traced timing cannot qualify a normal source update"})
	}
	defer func() {
		record.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		record.Status = operationStatus(record.Cases, returnedErr)
		if returnedErr != nil {
			record.Complete = false
			record.Error = returnedErr.Error()
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
	if info := client.Info(); info.InstanceID != backend.GatewayInstance || info.InventoryFingerprint != status.InventoryFingerprint {
		return fmt.Errorf("gateway identity or inventory changed during acceptance setup")
	}
	ctx, stopKernel, err := startKernelGuard(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { returnedErr = errors.Join(returnedErr, stopKernel()) }()
	ctx, stopGuard := memoryGuard(ctx, 8<<30)
	defer stopGuard()
	calls := newResultCalls(ctx, client.Call, stdout)
	call := calls.call
	appendResults := func(results []contract.Result) {
		for i := range results {
			calls.classify(&results[i])
		}
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
	// Exercise independently meaningful session/protocol checks before costly
	// generated-code and near-limit retrieval. A retrieval miss must not make
	// cancellation or changed-media coverage impossible to invoke on its own.
	if opts.suite == "lifecycle" || opts.suite == "all" {
		appendResults(scenarios.Lifecycle(ctx, options, call))
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
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
	if err != nil || final.ContainerID != backend.ContainerID || final.ImageID != backend.ImageID || final.GatewayInstance != backend.GatewayInstance || !equalJSON(final.Arguments, backend.Arguments) {
		return fmt.Errorf("backend identity or native policy changed during the suite")
	}
	finalStatus, err := gateway.FetchStatus(ctx, opts.endpoint, key, 0)
	if err != nil {
		return err
	}
	if err := validateGatewayBackend(finalStatus, final); err != nil {
		return err
	}
	if finalStatus.InventoryFingerprint != status.InventoryFingerprint {
		return fmt.Errorf("gateway inventory changed during the suite")
	}
	record.Complete = true
	return nil
}

func operationStatus(results []contract.Result, err error) contract.Status {
	status := contract.Worst(results)
	if err != nil && status != contract.Fail {
		return contract.Blocked
	}
	return status
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

func transportRank(kind string) int {
	switch kind {
	case transport.OutputLimit:
		return 1
	case transport.Infrastructure:
		return 2
	case transport.Protocol:
		return 3
	case transport.Policy:
		return 4
	}
	return 0
}

// Keep per-call evidence instead of relabeling all failures when a guard or the
// operator cancels the parent. Earlier completed answer oracles remain valid.
// Names and error details cover calls canceled before a request ID is received;
// IDs cover composite scenarios whose result name differs from their calls.
type resultCalls struct {
	mu       sync.Mutex
	parent   context.Context
	invoke   contract.Call
	stdout   io.Writer
	kinds    map[string]string
	errors   map[string]string
	terminal error
}

func newResultCalls(parent context.Context, invoke contract.Call, stdout io.Writer) *resultCalls {
	return &resultCalls{parent: parent, invoke: invoke, stdout: stdout, kinds: map[string]string{}, errors: map[string]string{}}
}

func (calls *resultCalls) call(ctx context.Context, name string, request map[string]any) (contract.Response, error) {
	calls.mu.Lock()
	terminal := calls.terminal
	if terminal == nil {
		fmt.Fprintf(calls.stdout, "Running  %s\n", name)
	}
	calls.mu.Unlock()
	var response contract.Response
	err := terminal
	if err == nil {
		response, err = calls.invoke(ctx, name, request)
	}
	// Intentional lifecycle cancellation is successful only after transport has
	// independently verified its report, policy, correlation and saved evidence.
	// It must not poison the subsequent recovery request or bypass a parent guard.
	parentInterrupted := calls.parent.Err() != nil && err != nil && (errors.Is(err, calls.parent.Err()) || errors.Is(err, context.Cause(calls.parent)))
	kind := transportErrorKind(err)
	if !parentInterrupted && response.CancellationVerified && errors.Is(err, context.Canceled) && transportRank(kind) <= transportRank(transport.Infrastructure) {
		return response, err
	}
	if parentInterrupted && transportRank(kind) < transportRank(transport.Infrastructure) {
		kind = transport.Infrastructure
	}
	if kind != "" {
		calls.mu.Lock()
		defer calls.mu.Unlock()
		for _, key := range []string{name, response.RequestID} {
			if key == "" {
				continue
			}
			calls.kinds[key] = kind
		}
		calls.errors[err.Error()] = kind
		if kind != transport.OutputLimit {
			calls.terminal = err
		}
	}
	return response, err
}

func (calls *resultCalls) classify(result *contract.Result) {
	calls.mu.Lock()
	defer calls.mu.Unlock()
	kinds := map[string]string{result.Name: calls.kinds[result.Name]}
	for _, id := range result.RequestIDs {
		kinds[id] = calls.kinds[id]
	}
	matchedTransport := false
	for detail, kind := range calls.errors {
		if strings.Contains(result.Detail, detail) {
			matchedTransport = true
			if transportRank(kind) > transportRank(kinds[result.Name]) {
				kinds[result.Name] = kind
			}
		}
	}
	before := result.Status
	classifyResult(result, kinds)
	// A composite result can include a failed sibling's ID as well as an
	// independent answer miss. Only a transport-derived failure detail may
	// replace FAIL; the scenario retains its own oracle failure separately.
	if before == contract.Fail && result.Status == contract.Blocked && !matchedTransport {
		result.Status = contract.Fail
	}
}

func classifyResult(result *contract.Result, kinds map[string]string) {
	kind := kinds[result.Name]
	for _, id := range result.RequestIDs {
		if transportRank(kinds[id]) > transportRank(kind) {
			kind = kinds[id]
		}
	}
	if result.Status == contract.Fail && kind == transport.Infrastructure {
		result.Status = contract.Blocked
	}
	if kind == transport.Policy || kind == transport.Protocol {
		result.Status = contract.Fail
	}
	// Only codingCases owns a bounded coding ceiling's REVIEW classification.
	// Strict scenario oracles require natural completion; a repeated answer
	// truncated at their budget must remain FAIL, not become a coding miss.
}

// Joined response, report and persistence errors are independent evidence.
// Inspect every branch so an earlier output ceiling cannot hide bad policy or
// missing reports and accidentally authorize additional inference attempts.
func transportErrorKind(err error) string {
	best := ""
	var visit func(error)
	visit = func(current error) {
		if current == nil {
			return
		}
		if typed, ok := current.(*transport.Error); ok && transportRank(typed.Kind) > transportRank(best) {
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
