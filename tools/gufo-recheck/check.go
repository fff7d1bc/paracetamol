package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/catalog"
	"paracetamol/internal/gateway"
	"paracetamol/internal/project"
)

const fixtureVersion = "gufo-literal-tools.v1"
const maxResponse = 16 * 1024 * 1024
const literalXML = `<invoke name="documentation"><parameter name="value">x</parameter></invoke>`
const literalMarker = `<tool_calls></tool_calls>`

// These requests deliberately omit all six samplers: gateway defaults and their
// provenance are part of the regression, not a client-side approximation.
//
//go:embed testdata/*.json
var frozenRequests embed.FS

var fixtureNames = []string{"literal-xml", "literal-then-read", "literal-then-read-sse"}

type checkResult struct {
	Name           string                 `json:"name"`
	Model          string                 `json:"selected_model"`
	RequestID      string                 `json:"request_id,omitempty"`
	ResponseID     string                 `json:"response_id,omitempty"`
	HTTPStatus     int                    `json:"http_status"`
	HTTPWallMS     float64                `json:"http_wall_ms"`
	ResponseBytes  int                    `json:"response_bytes"`
	Report         *gateway.RequestReport `json:"request_report,omitempty"`
	QualityPass    bool                   `json:"quality_pass"`
	ProtocolPass   bool                   `json:"protocol_pass"`
	OracleErrors   []string               `json:"oracle_errors"`
	ProtocolErrors []string               `json:"protocol_errors"`
	Error          string                 `json:"error,omitempty"`
}

type checkSummary struct {
	Schema         string        `json:"schema"`
	FixtureVersion string        `json:"fixture_version"`
	QualityPass    bool          `json:"quality_pass"`
	ProtocolPass   bool          `json:"protocol_pass"`
	Scope          string        `json:"scope"`
	CountersSource string        `json:"counters_source"`
	Requests       []checkResult `json:"requests"`
	Error          string        `json:"error,omitempty"`
}

func runChecks(ctx context.Context, args []string, stdout, stderr io.Writer) (runErr error) {
	flags := flag.NewFlagSet("gufo-recheck check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	endpoint := flags.String("gateway-url", "", "numeric loopback gateway origin (optional /v1)")
	out := flags.String("out", "", "fresh private output directory below checkout build/")
	label := flags.String("label", "", "operator-supplied backend label; not verified provenance")
	model := flags.String("model", regular, "regular model or production -dflash2 control")
	experimental := flags.Bool("experimental", false, "acknowledge developer-only experimental check")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || !*experimental || *out == "" || *label == "" || len(*label) > 256 || strings.ContainsAny(*label, "\r\n\v\f\u0085\u2028\u2029") {
		return fmt.Errorf("check requires --experimental, --gateway-url, --out, and a single-line --label (1–256 bytes), with no positional arguments")
	}
	if *model != regular && *model != regular+"-dflash2" {
		return fmt.Errorf("unsupported check model")
	}
	parsed, err := url.Parse(*endpoint)
	if err != nil || parsed.RawPath != "" || (parsed.Path != "" && parsed.Path != "/v1") {
		return fmt.Errorf("gateway URL requires a numeric loopback origin, optionally ending in /v1")
	}
	parsed.Path = ""
	base, err := origin(parsed.String())
	if err != nil {
		return err
	}
	root, err := project.Root()
	if err != nil {
		return err
	}
	catalogPath := filepath.Join(root, "catalog", "catalog.json")
	managed, err := catalog.Load(catalogPath)
	if err != nil {
		return err
	}
	if err := verifyFrozenPolicy(managed); err != nil {
		return err
	}
	if err := verifyCheckModel(managed, *model); err != nil {
		return err
	}
	ids, err := gateway.FetchModelIDs(ctx, base.String()+"/v1", "")
	if err != nil {
		return err
	}
	if !slices.Contains(ids, *model) {
		return fmt.Errorf("selected model is not available in this gateway")
	}
	dir, err := newOutputDir(root, *out)
	if err != nil {
		return err
	}
	hashes := map[string]string{}
	for _, name := range fixtureNames {
		raw, err := frozenRequests.ReadFile("testdata/" + name + ".json")
		if err != nil {
			return err
		}
		hashes[name] = digest(raw)
	}
	catalogBytes, err := os.ReadFile(catalogPath)
	if err != nil {
		return err
	}
	manifest := map[string]any{"schema": "paracetamol.gufo-recheck.manifest.v1", "fixture_version": fixtureVersion, "fixture_sha256": hashes, "catalog_sha256": digest(catalogBytes), "label": *label, "model": *model, "backend_provenance": "operator-supplied; unverified", "gateway_url": base.String()}
	if err := saveJSON(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		return err
	}
	summary := checkSummary{Schema: "paracetamol.gufo-recheck.summary.v1", FixtureVersion: fixtureVersion, QualityPass: true, ProtocolPass: true, Scope: "three frozen literal/tool regressions only; not a performance benchmark or broad quality qualification", CountersSource: "typed gateway request report; unobserved counters remain absent", Requests: []checkResult{}}
	defer func() {
		if runErr != nil {
			summary.Error = runErr.Error()
			if !errors.Is(runErr, errQualityFailed) {
				summary.QualityPass = false
				summary.ProtocolPass = false
			}
		}
		if err := saveJSON(filepath.Join(dir, "summary.json"), summary); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()
	client := experimentClient()
	defer client.CloseIdleConnections()
	for _, name := range fixtureNames {
		result, callErr := checkOne(ctx, client, base.String(), dir, name, *model, managed.SamplingPolicies[policyName].Defaults(true))
		summary.Requests = append(summary.Requests, result)
		summary.QualityPass = summary.QualityPass && result.QualityPass
		summary.ProtocolPass = summary.ProtocolPass && result.ProtocolPass
		fmt.Fprintf(stdout, "%s: quality=%t protocol=%t\n", name, result.QualityPass, result.ProtocolPass)
		if callErr != nil {
			if len(result.OracleErrors) != 0 || len(result.ProtocolErrors) != 0 {
				return errors.Join(errQualityFailed, callErr)
			}
			return callErr
		}
	}
	if !summary.QualityPass || !summary.ProtocolPass {
		return errQualityFailed
	}
	return nil
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func checkOne(ctx context.Context, client *http.Client, base, dir, name, model string, defaults map[string]any) (result checkResult, callErr error) {
	result = checkResult{Name: name, Model: model, OracleErrors: []string{}, ProtocolErrors: []string{}}
	defer func() {
		if callErr != nil {
			result.Error = callErr.Error()
		}
		result.QualityPass = callErr == nil && len(result.OracleErrors) == 0
		result.ProtocolPass = callErr == nil && len(result.ProtocolErrors) == 0
		callErr = errors.Join(callErr, saveJSON(filepath.Join(dir, name+".result.json"), result))
	}()
	raw, err := frozenRequests.ReadFile("testdata/" + name + ".json")
	if err != nil {
		return result, err
	}
	var request map[string]json.RawMessage
	if err := json.Unmarshal(raw, &request); err != nil {
		return result, err
	}
	request["model"], _ = json.Marshal(model)
	if err := saveJSON(filepath.Join(dir, name+".request.json"), request); err != nil {
		return result, err
	}
	body, _ := json.Marshal(request)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := client.Do(httpRequest)
	if err != nil {
		result.HTTPWallMS = float64(time.Since(started)) / float64(time.Millisecond)
		return result, err
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode
	result.RequestID = response.Header.Get(gateway.RequestIDHeader)
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	result.HTTPWallMS = float64(time.Since(started)) / float64(time.Millisecond)
	result.ResponseBytes = len(raw)
	if len(raw) > maxResponse {
		raw = raw[:maxResponse]
		readErr = errors.Join(readErr, fmt.Errorf("response exceeds 16 MiB; saved prefix only"))
	}
	suffix := ".response.json"
	stream := name == "literal-then-read-sse"
	if stream {
		suffix = ".response.sse"
	}
	if err := atomicfile.Write(filepath.Join(dir, name+suffix), raw, 0600, atomicfile.Create); err != nil {
		return result, err
	}
	if readErr != nil {
		result.ProtocolErrors = append(result.ProtocolErrors, readErr.Error())
	}
	if response.StatusCode != http.StatusOK {
		result.ProtocolErrors = append(result.ProtocolErrors, fmt.Sprintf("HTTP status %d", response.StatusCode))
	}
	if values := response.Header.Values(gateway.IdentityHeader); len(values) != 1 || values[0] != gateway.IdentityValue {
		result.ProtocolErrors = append(result.ProtocolErrors, "response is not gateway-marked")
	}
	if len(response.Header.Values(gateway.RequestIDHeader)) != 1 {
		result.ProtocolErrors = append(result.ProtocolErrors, "missing or ambiguous gateway request ID")
	}
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || (!stream && media != "application/json") || (stream && media != "text/event-stream") {
		result.ProtocolErrors = append(result.ProtocolErrors, "unexpected response content type")
	}
	answer, parseErr := assemble(raw, stream)
	result.ResponseID = answer.ID
	if answer.ID == "" {
		result.ProtocolErrors = append(result.ProtocolErrors, "missing response ID")
	}
	if parseErr != nil {
		result.ProtocolErrors = append(result.ProtocolErrors, parseErr.Error())
		result.OracleErrors = append(result.OracleErrors, "response cannot establish a complete answer")
	} else {
		result.OracleErrors = append(result.OracleErrors, oracle(answer, name)...)
	}
	if result.RequestID != "" {
		report, reportErr := completedReport(ctx, base, result.RequestID)
		if report.Schema != "" {
			result.Report = &report
		}
		if reportErr != nil {
			result.ProtocolErrors = append(result.ProtocolErrors, reportErr.Error())
		} else {
			result.ProtocolErrors = append(result.ProtocolErrors, verifyReport(report, result.RequestID, model, stream, answer.Finish, defaults)...)
		}
	} else {
		result.ProtocolErrors = append(result.ProtocolErrors, "no request ID for report correlation")
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, readErr
}

// The response can finish just before the gateway records its completed report.
// Only metadata polling has a deadline; inference itself has none.
func completedReport(ctx context.Context, endpoint, id string) (gateway.RequestReport, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var last gateway.RequestReport
	for {
		report, err := gateway.FetchRequestReport(waitCtx, endpoint+"/v1", "", id)
		if err != nil && !errors.Is(err, gateway.ErrRequestNotRetained) {
			return last, err
		}
		if err == nil {
			last = report
			if report.State == "completed" {
				return report, nil
			}
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return last, fmt.Errorf("completed request report unavailable: %w", waitCtx.Err())
		case <-timer.C:
		}
	}
}

func verifyReport(report gateway.RequestReport, id, model string, stream bool, finish string, defaults map[string]any) []string {
	var failures []string
	bad := func(reason string) { failures = append(failures, reason) }
	if report.State != "completed" || report.Record == nil {
		return []string{"request report is not completed"}
	}
	r := report.Record
	if report.Schema != gateway.RequestReportSchema || len(id) != 50 || report.InstanceID != id[1:33] || r.ID != id || r.Model != model || r.Stream != stream || r.Application != "llama-cpp" || report.InventoryFingerprint == "" {
		bad("request report correlation mismatch")
	}
	if r.Outcome != gateway.OutcomeSucceeded || r.HTTPStatus != http.StatusOK {
		bad("gateway request did not succeed")
	}
	completion := r.Completion
	if completion.Observation != gateway.CompletionAvailable || len(completion.FinishReasons) != 1 || completion.FinishReasons[0].Index != 0 || completion.FinishReasons[0].FinishReason != finish || (stream && (completion.StreamDone == nil || !*completion.StreamDone)) {
		bad("gateway completion evidence mismatch")
	}
	if r.Controls.SamplingPolicy != policyName || r.Controls.SamplingMode != "thinking" || len(r.Controls.Sampling) != 6 || len(r.Controls.Diagnostics) != 0 {
		bad("gateway sampling policy mismatch")
	}
	seen := map[string]bool{}
	for _, control := range r.Controls.Sampling {
		value, ok := defaults[control.Name]
		expected, _ := json.Marshal(value)
		parsed, err := strconv.ParseFloat(control.Value, 64)
		want, wantErr := strconv.ParseFloat(string(expected), 64)
		if seen[control.Name] || !ok || control.Source != gateway.ControlGatewayDefault || control.Provided || err != nil || wantErr != nil || parsed != want {
			bad("gateway sampler mismatch: " + control.Name)
		}
		seen[control.Name] = true
	}
	xhigh := 0
	for _, control := range r.Controls.Reasoning {
		if control.Name == "reasoning_effort" && control.Source == gateway.ControlClient && control.Provided && control.Value == "xhigh" {
			xhigh++
		}
	}
	if xhigh != 1 {
		bad("missing explicit client xhigh provenance")
	}
	return failures
}

type toolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type assistantMessage struct {
	Role         string          `json:"role"`
	Content      *string         `json:"content"`
	ToolCalls    []toolCall      `json:"tool_calls"`
	FunctionCall json.RawMessage `json:"function_call"`
}
type wireResponse struct {
	ID      string          `json:"id"`
	Error   json.RawMessage `json:"error"`
	Choices []struct {
		Index   *int              `json:"index"`
		Message *assistantMessage `json:"message"`
		Delta   *assistantMessage `json:"delta"`
		Finish  *string           `json:"finish_reason"`
	} `json:"choices"`
}
type assembledAnswer struct {
	ID, Content, Finish string
	Calls               []toolCall
}

func assemble(raw []byte, stream bool) (answer assembledAnswer, err error) {
	if !stream {
		var wire wireResponse
		if err := json.Unmarshal(raw, &wire); err != nil {
			return answer, fmt.Errorf("invalid JSON response: %w", err)
		}
		if presentJSON(wire.Error) {
			return answer, fmt.Errorf("backend returned an error envelope")
		}
		answer.ID = wire.ID
		if len(wire.Choices) != 1 || wire.Choices[0].Index == nil || *wire.Choices[0].Index != 0 {
			return answer, fmt.Errorf("expected exactly choice index 0")
		}
		choice := wire.Choices[0]
		if choice.Message == nil || choice.Delta != nil || choice.Message.Role != "assistant" || choice.Finish == nil || *choice.Finish == "" || presentJSON(choice.Message.FunctionCall) {
			return answer, fmt.Errorf("incomplete or invalid assistant message")
		}
		answer.Finish, answer.Calls = *choice.Finish, choice.Message.ToolCalls
		if choice.Message.Content != nil {
			answer.Content = *choice.Message.Content
		}
		return answer, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), maxResponse)
	var data []string
	done, choiceSeen, roleSeen := false, false, false
	consume := func() error {
		if len(data) == 0 {
			return nil
		}
		event := strings.Join(data, "\n")
		data = nil
		if done {
			return fmt.Errorf("SSE event after [DONE]")
		}
		if event == "[DONE]" {
			done = true
			return nil
		}
		var wire wireResponse
		if err := json.Unmarshal([]byte(event), &wire); err != nil {
			return fmt.Errorf("invalid SSE JSON")
		}
		if presentJSON(wire.Error) {
			return fmt.Errorf("backend returned an SSE error envelope")
		}
		if wire.ID != "" {
			if answer.ID != "" && answer.ID != wire.ID {
				return fmt.Errorf("SSE response ID changed")
			}
			answer.ID = wire.ID
		}
		if len(wire.Choices) == 0 {
			return nil
		} // Usage-only terminal chunk.
		if len(wire.Choices) != 1 || wire.Choices[0].Index == nil || *wire.Choices[0].Index != 0 {
			return fmt.Errorf("expected exactly SSE choice index 0")
		}
		choice := wire.Choices[0]
		if answer.Finish != "" {
			return fmt.Errorf("SSE choice after finish")
		}
		if choice.Message != nil || choice.Delta == nil || presentJSON(choice.Delta.FunctionCall) {
			return fmt.Errorf("invalid SSE assistant delta")
		}
		choiceSeen = true
		delta := choice.Delta
		if delta.Role != "" {
			if delta.Role != "assistant" || roleSeen {
				return fmt.Errorf("invalid or duplicate SSE role")
			}
			roleSeen = true
		}
		if delta.Content != nil {
			answer.Content += *delta.Content
		}
		for _, fragment := range delta.ToolCalls {
			if fragment.Index == nil || *fragment.Index < 0 || *fragment.Index > len(answer.Calls) {
				return fmt.Errorf("non-contiguous SSE tool index")
			}
			index := *fragment.Index
			if index == len(answer.Calls) {
				if fragment.ID == "" || fragment.Type != "function" {
					return fmt.Errorf("SSE tool missing ID or type")
				}
				answer.Calls = append(answer.Calls, toolCall{ID: fragment.ID, Type: fragment.Type})
			} else if fragment.ID != "" || fragment.Type != "" {
				return fmt.Errorf("duplicate SSE tool declaration")
			}
			call := &answer.Calls[index]
			call.Function.Name += fragment.Function.Name
			call.Function.Arguments += fragment.Function.Arguments
		}
		if choice.Finish != nil {
			if *choice.Finish == "" {
				return fmt.Errorf("empty SSE finish")
			}
			answer.Finish = *choice.Finish
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case line == "":
			if err := consume(); err != nil {
				return answer, err
			}
		case strings.HasPrefix(line, ":"): // SSE comment.
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			return answer, fmt.Errorf("unexpected SSE field")
		}
	}
	if err := scanner.Err(); err != nil {
		return answer, err
	}
	if len(data) != 0 {
		return answer, fmt.Errorf("unterminated SSE event")
	}
	if !done || !choiceSeen || !roleSeen || answer.Finish == "" {
		return answer, fmt.Errorf("incomplete SSE completion")
	}
	return answer, nil
}

func presentJSON(raw json.RawMessage) bool {
	return len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func oracle(answer assembledAnswer, name string) []string {
	var failures []string
	// Preserve the original cross-probe's surrounding-whitespace tolerance.
	// Literal token markup is ordinary visible text, never tool syntax here.
	content := strings.TrimSpace(answer.Content)
	if name == "literal-xml" {
		if content != literalXML {
			failures = append(failures, "literal XML content differs")
		}
		if len(answer.Calls) != 0 || answer.Finish != "stop" {
			failures = append(failures, "literal XML must stop without tool calls")
		}
		return failures
	}
	if content != literalMarker {
		failures = append(failures, "literal tool marker content differs")
	}
	if answer.Finish != "tool_calls" || len(answer.Calls) != 1 {
		return append(failures, "expected exactly one tool call and tool_calls finish")
	}
	call := answer.Calls[0]
	if call.ID == "" || call.Type != "function" || call.Function.Name != "read" || !exactReadArguments(call.Function.Arguments) {
		failures = append(failures, "expected typed read(path=fixture.xml), without other arguments")
	}
	return failures
}

func exactReadArguments(raw string) bool {
	decoder := json.NewDecoder(strings.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return false
	}
	key, err := decoder.Token()
	if err != nil || key != "path" {
		return false
	}
	var path string
	if err := decoder.Decode(&path); err != nil || path != "fixture.xml" {
		return false
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return false
	}
	_, err = decoder.Token()
	return err == io.EOF
}
