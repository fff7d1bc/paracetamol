package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/gateway"
	"paracetamol/internal/project"
)

func intPtr(n int) *int { return &n }

func fixtureReadCall() toolCall {
	call := toolCall{ID: "call_fixture", Type: "function"}
	call.Function.Name, call.Function.Arguments = "read", `{"path":"fixture.xml"}`
	return call
}

func responseJSON(name, fault string) []byte {
	message := assistantMessage{Role: "assistant"}
	content, finish := literalXML, "stop"
	if name != "literal-xml" {
		content, finish = literalMarker, "tool_calls"
		message.ToolCalls = []toolCall{fixtureReadCall()}
	}
	switch fault {
	case "empty":
		content = ""
	case "76-calls":
		message.ToolCalls = make([]toolCall, 76)
		for i := range message.ToolCalls {
			message.ToolCalls[i] = fixtureReadCall()
		}
	case "length":
		finish = "length"
	}
	message.Content = &content
	raw, _ := json.Marshal(map[string]any{"id": "chat_fixture", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}})
	return raw
}

func responseSSE(fault string) []byte {
	var output bytes.Buffer
	event := func(delta any, finish any) {
		raw, _ := json.Marshal(map[string]any{"id": "chat_fixture", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		fmt.Fprintf(&output, "data: %s\n\n", raw)
	}
	event(map[string]any{"role": "assistant", "content": literalMarker}, nil)
	call := fixtureReadCall()
	call.Index = intPtr(0)
	call.Function.Arguments = `{"path":`
	event(map[string]any{"tool_calls": []toolCall{call}}, nil)
	fragment := toolCall{Index: intPtr(0)}
	fragment.Function.Arguments = `"fixture.xml"}`
	event(map[string]any{"tool_calls": []toolCall{fragment}}, nil)
	if fault != "unfinished" {
		event(map[string]any{}, "tool_calls")
	}
	if fault != "no-done" {
		output.WriteString("data: [DONE]\n\n")
	}
	return output.Bytes()
}

func TestBackendErrorsCannotLookLikeSuccessfulCompletions(t *testing.T) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(responseJSON("literal-xml", ""), &object); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`{"message":"generation failed"}`, `"failure"`, `{}`} {
		object["error"] = json.RawMessage(value)
		body, _ := json.Marshal(object)
		if _, err := assemble(body, false); err == nil {
			t.Fatalf("accepted JSON error %s", value)
		}
		stream := strings.Replace(string(responseSSE("")), "data: [DONE]", "data: {\"error\":"+value+"}\n\ndata: [DONE]", 1)
		if _, err := assemble([]byte(stream), true); err == nil {
			t.Fatalf("accepted terminal SSE error %s", value)
		}
	}
	object["error"] = json.RawMessage(`null`)
	body, _ := json.Marshal(object)
	if _, err := assemble(body, false); err != nil {
		t.Fatal(err)
	}
}

func testDefaults(t *testing.T) map[string]any {
	t.Helper()
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFrozenPolicy(managed); err != nil {
		t.Fatal(err)
	}
	return managed.SamplingPolicies[policyName].Defaults(true)
}

const testInstance = "0123456789abcdef0123456789abcdef"

func fixtureReport(id, model string, stream bool, defaults map[string]any) gateway.RequestReport {
	finish := "stop"
	if id[len(id)-1] != '1' {
		finish = "tool_calls"
	}
	record := &gateway.RequestRecord{ID: id, Model: model, Application: "llama-cpp", Stream: stream, Outcome: gateway.OutcomeSucceeded, HTTPStatus: 200,
		Controls:   gateway.RequestControls{SamplingPolicy: policyName, SamplingMode: "thinking", Reasoning: []gateway.ObservedControl{{Name: "reasoning_effort", Source: gateway.ControlClient, Provided: true, Value: "xhigh"}}},
		Completion: gateway.CompletionMetadata{Observation: gateway.CompletionAvailable, FinishReasons: []gateway.ChoiceFinishReason{{Index: 0, FinishReason: finish}}},
	}
	if stream {
		done := true
		record.Completion.StreamDone = &done
	}
	for name, value := range defaults {
		raw, _ := json.Marshal(value)
		record.Controls.Sampling = append(record.Controls.Sampling, gateway.ObservedControl{Name: name, Source: gateway.ControlGatewayDefault, Value: string(raw)})
	}
	return gateway.RequestReport{Schema: gateway.RequestReportSchema, InstanceID: testInstance, InventoryFingerprint: "fixture_inventory", State: "completed", Record: record}
}

func TestFrozenRequests(t *testing.T) {
	expectedHashes := map[string]string{
		"literal-xml":           "507d84255cfa5e7023211f754945acdca0413f66b2c36c4b5ac60eebecdd9173",
		"literal-then-read":     "df15100344b913251b11745b1eee0bc87af839a52e3cefd86a6640dde648ce65",
		"literal-then-read-sse": "2dd5521476edf223af3048df2be006b88dbbf4411c88c1fc9792be794aa5a13d",
	}
	var previous map[string]any
	for i, name := range fixtureNames {
		raw, err := frozenRequests.ReadFile("testdata/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if digest(raw) != expectedHashes[name] {
			t.Fatalf("frozen fixture bytes changed: %s; review version and provenance", name)
		}
		var request map[string]any
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Fatal(err)
		}
		if request["reasoning_effort"] != "xhigh" || request["seed"] != float64(42) || request["max_tokens"] != float64(4096) || request["cache_prompt"] != false || request["tool_choice"] != "auto" || request["model"] != regular {
			t.Fatalf("request controls changed: %s", raw)
		}
		for field := range testDefaults(t) {
			if _, exists := request[field]; exists {
				t.Fatalf("fixture supplies sampler %s", field)
			}
		}
		if i == 2 {
			request["stream"] = false
			if !reflect.DeepEqual(previous, request) {
				t.Fatal("SSE fixture differs beyond stream")
			}
		}
		previous = request
	}
}

func TestAssemblerAndOracles(t *testing.T) {
	if got := oracle(assembledAnswer{Content: "\n" + literalXML + "\n", Finish: "stop"}, "literal-xml"); len(got) != 0 {
		t.Fatalf("legacy whitespace tolerance changed: %v", got)
	}
	for _, test := range []struct {
		name, fault               string
		stream, protocol, quality bool
	}{
		{"literal-xml", "", false, true, true}, {"literal-then-read", "", false, true, true}, {"literal-then-read-sse", "", true, true, true},
		{"literal-xml", "empty", false, true, false}, {"literal-then-read", "76-calls", false, true, false}, {"literal-then-read", "length", false, true, false},
		{"literal-then-read-sse", "unfinished", true, false, false}, {"literal-then-read-sse", "no-done", true, false, false},
	} {
		t.Run(test.name+"/"+test.fault, func(t *testing.T) {
			raw := responseJSON(test.name, test.fault)
			if test.stream {
				raw = responseSSE(test.fault)
			}
			answer, err := assemble(raw, test.stream)
			if (err == nil) != test.protocol {
				t.Fatalf("protocol err = %v", err)
			}
			quality := err == nil && len(oracle(answer, test.name)) == 0
			if quality != test.quality {
				t.Fatalf("quality=%t answer=%+v", quality, answer)
			}
		})
	}
	for _, raw := range []string{
		`{"choices":[{"index":1,"message":{"role":"assistant"},"finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"message":{"role":"assistant"},"finish_reason":"stop"},{"index":0}]}`,
	} {
		if _, err := assemble([]byte(raw), false); err == nil {
			t.Fatal("accepted ambiguous choices")
		}
	}
	bad := strings.ReplaceAll(string(responseSSE("")), `"index":0,"type":"function"`, `"index":2,"type":"function"`)
	// Struct field ordering need not match a map's ordering.
	if bad == string(responseSSE("")) {
		bad = strings.Replace(string(responseSSE("")), `"index":0,"id":"call_fixture"`, `"index":2,"id":"call_fixture"`, 1)
	}
	if _, err := assemble([]byte(bad), true); err == nil {
		t.Fatal("accepted non-contiguous tool fragment")
	}
	if _, err := assemble(append(responseSSE(""), []byte("data: [DONE]\n\n")...), true); err == nil {
		t.Fatal("accepted duplicate done")
	}
	for _, args := range []string{`{"path":"fixture.xml","path":"fixture.xml"}`, `{"path":"fixture.xml","other":0}`, `{"path":"other.xml"}`, `{"path":"fixture.xml"} {}`, `{"path":null}`} {
		if exactReadArguments(args) {
			t.Fatalf("accepted non-exact read arguments: %s", args)
		}
	}
}

func TestCheckArgumentValidation(t *testing.T) {
	for _, args := range [][]string{
		{}, {"--experimental"}, {"--experimental=false", "--gateway-url", "http://127.0.0.1:1", "--out", "build/unused", "--label", "fixture"},
		{"--experimental", "--gateway-url", "http://127.0.0.1:1/%76%31", "--out", "build/unused", "--label", "fixture"},
		{"--experimental", "--gateway-url", "http://127.0.0.1:1", "--out", "build/unused", "--label", "line\nbreak"},
		{"--experimental", "--gateway-url", "http://127.0.0.1:1", "--out", "build/unused", "--label", "fixture", "--model", "arbitrary"},
	} {
		if err := runChecks(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted invalid arguments: %v", args)
		}
	}
}

func TestMissingReportIsNotQualityEvidence(t *testing.T) {
	server, _ := syntheticGateway(t, "")
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	report, err := completedReport(ctx, server.URL, "r"+testInstance+"-0000000000000001")
	if err == nil || report.Record != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing report=%+v err=%v", report, err)
	}
}

func TestReportPolicyAndCorrelation(t *testing.T) {
	defaults := testDefaults(t)
	id := "r" + testInstance + "-0000000000000001"
	for _, fault := range []string{"", "sampler", "provided", "source", "xhigh", "completion", "model", "instance", "outcome", "duplicate-sampler"} {
		t.Run(fault, func(t *testing.T) {
			report := fixtureReport(id, regular, false, defaults)
			switch fault {
			case "sampler":
				report.Record.Controls.Sampling[0].Value = "987"
			case "provided":
				report.Record.Controls.Sampling[0].Provided = true
			case "source":
				report.Record.Controls.Sampling[0].Source = gateway.ControlClient
			case "xhigh":
				report.Record.Controls.Reasoning[0].Value = "high"
			case "completion":
				report.Record.Completion.Observation = gateway.CompletionPartial
			case "model":
				report.Record.Model += "-wrong"
			case "instance":
				report.InstanceID = strings.Repeat("0", 32)
			case "outcome":
				report.Record.Outcome = gateway.OutcomeCanceled
			case "duplicate-sampler":
				report.Record.Controls.Sampling[1] = report.Record.Controls.Sampling[0]
			}
			if got := verifyReport(report, id, regular, false, "stop", defaults); (len(got) == 0) != (fault == "") {
				t.Fatalf("errors=%v", got)
			}
		})
	}
}

func syntheticGateway(t *testing.T, fault string) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	defaults := testDefaults(t)
	var mu sync.Mutex
	calls, fetches := 0, 0
	secondStarted := make(chan struct{})
	reports := map[string]gateway.RequestReport{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
		switch {
		case r.URL.Path == "/v1/models":
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": regular}, map[string]any{"id": regular + "-dflash2"}}})
		case strings.HasPrefix(r.URL.Path, "/paracetamol/v1/requests/"):
			mu.Lock()
			defer mu.Unlock()
			fetches++
			id := strings.TrimPrefix(r.URL.Path, "/paracetamol/v1/requests/")
			report, ok := reports[id]
			if !ok {
				w.WriteHeader(404)
				return
			}
			if fault == "race" && fetches == 1 {
				w.WriteHeader(404)
				return
			}
			if fault == "race" && fetches == 2 {
				report.State = "active"
				report.Phase = "upstream"
				report.Active = &gateway.ActiveRequestReport{ID: id}
				report.Record = nil
			}
			json.NewEncoder(w).Encode(report)
		case r.URL.Path == "/v1/chat/completions":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			for key := range defaults {
				if _, exists := body[key]; exists {
					t.Errorf("client sent sampler %s", key)
				}
			}
			id := fmt.Sprintf("r%s-%016x", testInstance, n)
			stream := body["stream"] == true
			mu.Lock()
			report := fixtureReport(id, body["model"].(string), stream, defaults)
			if fault == "wrong-sampler" {
				report.Record.Controls.Sampling[0].Value = "123"
			}
			reports[id] = report
			mu.Unlock()
			w.Header().Set(gateway.RequestIDHeader, id)
			w.Header().Set("Content-Type", "application/json")
			if fault == "cancel-second" && n == 2 {
				close(secondStarted)
				<-r.Context().Done()
				return
			}
			if fault == "redirect" {
				w.Header().Set("Location", "/redirect-target")
				w.WriteHeader(307)
				return
			}
			if fault == "oversized" {
				io.Copy(w, io.LimitReader(&repeatReader{}, maxResponse+100))
				return
			}
			name := fixtureNames[n-1]
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write(responseSSE(""))
			} else {
				w.Write(responseJSON(name, fault))
			}
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	return server, secondStarted
}

type repeatReader struct{}

func (*repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func privateCheckDir(t *testing.T) string { t.Helper(); return t.TempDir() }

func TestCheckOneFailuresAndPrivateWrites(t *testing.T) {
	for _, fault := range []string{"", "empty", "76-calls", "wrong-sampler", "race", "redirect", "oversized"} {
		t.Run(fault, func(t *testing.T) {
			server, _ := syntheticGateway(t, fault)
			defer server.Close()
			dir := privateCheckDir(t)
			name := "literal-xml"
			if fault == "76-calls" {
				name = "literal-then-read"
			}
			result, err := checkOne(context.Background(), experimentClient(), server.URL, dir, name, regular, testDefaults(t))
			if fault == "oversized" {
				if err == nil {
					t.Fatal("oversized response accepted")
				}
				if result.ResponseBytes != maxResponse+1 {
					t.Fatalf("unbounded response: %d", result.ResponseBytes)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if fault == "" || fault == "race" {
				if !result.ProtocolPass || !result.QualityPass {
					t.Fatalf("result=%+v", result)
				}
			} else if result.ProtocolPass && result.QualityPass {
				t.Fatalf("fault passed: %+v", result)
			}
			for _, suffix := range []string{".request.json", ".response.json", ".result.json"} {
				info, err := os.Stat(filepath.Join(dir, name+suffix))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("non-private/missing %s: %v", suffix, err)
				}
			}
			if _, err := checkOne(context.Background(), experimentClient(), server.URL, dir, name, regular, testDefaults(t)); err == nil {
				t.Fatal("overwrote prior output")
			}
		})
	}
}

func TestRunChecksAndHelp(t *testing.T) {
	var output bytes.Buffer
	if err := runChecks(context.Background(), []string{"--help"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"", "empty", "wrong-sampler", "cancel-second"} {
		t.Run(fault, func(t *testing.T) {
			server, secondStarted := syntheticGateway(t, fault)
			defer server.Close()
			root, err := project.Root()
			if err != nil {
				t.Fatal(err)
			}
			// Exercise the same anchored build-directory validation as real runs.
			parent, err := os.MkdirTemp(filepath.Join(root, "build"), "gufo-check-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(parent)
			out := filepath.Join(parent, "results")
			ctx := context.Background()
			cancel := func() {}
			if fault == "cancel-second" {
				ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
				go func() {
					select {
					case <-secondStarted:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			defer cancel()
			err = runChecks(ctx, []string{"--experimental", "--gateway-url", server.URL + "/v1", "--out", out, "--label", "synthetic-backend"}, io.Discard, io.Discard)
			if fault == "" && err != nil {
				t.Fatal(err)
			}
			if fault == "empty" || fault == "wrong-sampler" {
				if !errors.Is(err, errQualityFailed) {
					t.Fatalf("error=%v", err)
				}
			}
			if fault == "cancel-second" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error=%v", err)
			}
			raw, readErr := os.ReadFile(filepath.Join(out, "summary.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			var summary checkSummary
			if err := json.Unmarshal(raw, &summary); err != nil {
				t.Fatal(err)
			}
			if fault == "" && (!summary.QualityPass || !summary.ProtocolPass || len(summary.Requests) != 3) {
				t.Fatalf("summary=%s", raw)
			}
			if fault == "empty" && (!summary.ProtocolPass || summary.QualityPass) {
				t.Fatalf("quality failure changed protocol result: %s", raw)
			}
			if fault == "cancel-second" && (len(summary.Requests) != 2 || !summary.Requests[0].QualityPass || summary.Requests[1].Error == "") {
				t.Fatalf("partial summary=%s", raw)
			}
		})
	}
}
