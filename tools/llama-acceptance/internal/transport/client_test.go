package transport

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
	"strings"
	"sync"
	"testing"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/catalog"
	"paracetamol/internal/gateway"
)

const testModel = "synthetic-model"
const testKey = "synthetic-key-0123456789012345678901"

var testInstance = strings.Repeat("1", 32)
var testInventory = strings.Repeat("2", 64)

func pointer[T any](value T) *T { return &value }

func testCatalog() catalog.Catalog {
	on := map[string]any{"temperature": 1.0, "top_p": 0.95, "top_k": 20, "min_p": 0.05, "presence_penalty": 0, "repeat_penalty": 1}
	off := map[string]any{"temperature": 0.7, "top_p": 0.8, "top_k": 20, "min_p": 0, "presence_penalty": 0, "repeat_penalty": 1}
	return catalog.Catalog{LlamaPresets: map[string]catalog.LlamaPreset{testModel: {ID: testModel, DefaultContext: 4096, SamplingPolicy: "synthetic", ReasoningControl: "effort", ReasoningDefault: "high", ReasoningOff: true, ReasoningLevels: []string{"medium", "high", "xhigh"}}}, SamplingPolicies: map[string]catalog.SamplingPolicy{"synthetic": {ID: "synthetic", Thinking: on, NonThinking: off}}}
}

type fakeGateway struct {
	t             *testing.T
	mu            sync.Mutex
	posts         int
	report        gateway.RequestReport
	mutate        func(*gateway.RequestReport)
	response      string
	status        int
	badMarker     bool
	missingReport bool
	server        *httptest.Server
}

func newFake(t *testing.T) (*fakeGateway, *Client) {
	t.Helper()
	f := &fakeGateway{t: t, status: http.StatusOK, response: `{"id":"completion-a","choices":[{"index":0,"message":{"role":"assistant","content":"ok","reasoning_content":"full thought","extra":{"preserved":true}},"finish_reason":"stop"}]}`}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	managed := testCatalog()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	client, err := New(context.Background(), Options{GatewayURL: f.server.URL + "/v1", Key: testKey, OutputDir: dir, Model: testModel, Catalog: &managed})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return f, client
}

func (f *fakeGateway) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		http.Error(w, "bad auth", 401)
		return
	}
	switch {
	case r.URL.Path == "/v1/models":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": testModel}}})
	case r.URL.Path == "/paracetamol/v1/status":
		f.mu.Lock()
		defer f.mu.Unlock()
		status := gateway.Status{Schema: gateway.StatusSchema, InstanceID: testInstance, InventoryFingerprint: testInventory, Models: []gateway.StatusModel{{ID: testModel, Application: "llama-cpp", ConfiguredContext: 4096}}}
		if f.report.Record != nil && r.URL.Query().Get("requests") != "" {
			status.RecentRequests = []gateway.RequestRecord{*f.report.Record}
		}
		_ = json.NewEncoder(w).Encode(status)
	case r.URL.Path == "/v1/chat/completions":
		var request map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&request); err != nil {
			f.t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		f.mu.Lock()
		f.posts++
		id := fmt.Sprintf("r%s-%016x", testInstance, f.posts)
		stream, _ := request["stream"].(bool)
		finish := "stop"
		if strings.Contains(f.response, `"length"`) {
			finish = "length"
		}
		f.report = makeReport(id, request, stream, finish)
		f.report.Record.SessionID = r.Header.Get(gateway.SessionIDHeader)
		if f.mutate != nil {
			f.mutate(&f.report)
		}
		body, status, bad := f.response, f.status, f.badMarker
		f.mu.Unlock()
		w.Header().Set(gateway.RequestIDHeader, id)
		if bad {
			w.Header().Set(gateway.IdentityHeader, "foreign")
		}
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	case strings.HasPrefix(r.URL.Path, "/paracetamol/v1/requests/"):
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.missingReport {
			http.Error(w, "unavailable", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(f.report)
	default:
		http.NotFound(w, r)
	}
}

func makeReport(id string, request map[string]any, stream bool, finish string) gateway.RequestReport {
	managed := testCatalog()
	mode := "thinking"
	if request["reasoning_effort"] == "none" {
		mode = "non_thinking"
	}
	if kwargs, ok := request["chat_template_kwargs"].(map[string]any); ok {
		if kwargs["enable_thinking"] == false {
			mode = "non_thinking"
		}
	}
	controls := gateway.RequestControls{SamplingPolicy: "synthetic", SamplingMode: mode}
	for name, value := range managed.SamplingPolicies["synthetic"].Defaults(mode == "thinking") {
		source, provided := gateway.ControlGatewayDefault, false
		if explicit, exists := request[name]; exists && explicit != nil {
			value, source, provided = explicit, gateway.ControlClient, true
		}
		controls.Sampling = append(controls.Sampling, gateway.ObservedControl{Name: name, Value: fmt.Sprint(value), Source: source, Provided: provided})
	}
	for _, name := range []string{"reasoning_effort", "reasoning_strength", "chat_template_kwargs.enable_thinking", "chat_template_kwargs.reasoning_effort", "chat_template_kwargs.reasoning_strength"} {
		control := gateway.ObservedControl{Name: name, Source: gateway.ControlDefault}
		value, exists := request[name]
		if strings.HasPrefix(name, "chat_template_kwargs.") {
			if kwargs, ok := request["chat_template_kwargs"].(map[string]any); ok {
				value, exists = kwargs[strings.TrimPrefix(name, "chat_template_kwargs.")]
			}
		}
		if exists {
			control.Value, control.Source, control.Provided = fmt.Sprint(value), gateway.ControlClient, true
		}
		controls.Reasoning = append(controls.Reasoning, control)
	}
	completion := gateway.CompletionMetadata{Observation: gateway.CompletionAvailable, FinishReasons: []gateway.ChoiceFinishReason{{Index: 0, FinishReason: finish}}}
	if stream {
		completion.StreamDone = pointer(true)
	}
	return gateway.RequestReport{Schema: gateway.RequestReportSchema, InstanceID: testInstance, InventoryFingerprint: testInventory, State: "completed", Record: &gateway.RequestRecord{ID: id, Model: testModel, Application: "llama-cpp", Stream: stream, Outcome: gateway.OutcomeSucceeded, HTTPStatus: 200, Controls: controls, Completion: completion, Tokens: gateway.TokenUsage{Input: pointer(int64(100)), Output: pointer(int64(8)), Cached: pointer(int64(0))}, BackendTimings: gateway.BackendTimings{PromptTokens: pointer(int64(100)), GeneratedTokens: pointer(int64(8)), PromptMilliseconds: pointer(50.0), GeneratedMilliseconds: pointer(100.0)}}}
}

func TestCallPreservesAssistantAndEvidence(t *testing.T) {
	f, client := newFake(t)
	request := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "synthetic"}}, "reasoning_effort": "xhigh"}
	answer, err := client.Call(context.Background(), "basic", request)
	if err != nil {
		t.Fatal(err)
	}
	if answer.Text != "ok" || answer.Reasoning != "full thought" || answer.Message["extra"] == nil || answer.Finish != "stop" {
		t.Fatalf("incomplete answer: %+v", answer)
	}
	if _, exists := request["model"]; exists {
		t.Fatal("mutated caller request")
	}
	if f.posts != 1 {
		t.Fatalf("posts=%d", f.posts)
	}
	for _, name := range []string{"basic.request.json", "basic.response.json", "basic.report.json"} {
		info, err := os.Stat(filepath.Join(client.output, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode=%o", name, info.Mode().Perm())
		}
		raw, err := os.ReadFile(filepath.Join(client.output, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(testKey)) {
			t.Fatal("persisted gateway key")
		}
	}
	if _, err := client.Call(context.Background(), "basic", request); err == nil {
		t.Fatal("overwrote evidence")
	}
	if f.posts != 1 {
		t.Fatal("contacted inference before detecting collision")
	}
}

func TestThinkingOffAndExplicitSampler(t *testing.T) {
	for _, request := range []map[string]any{
		{"reasoning_effort": "none", "temperature": 0.25},
		{"chat_template_kwargs": map[string]any{"enable_thinking": false}, "top_k": 7},
		{"reasoning_effort": "medium", "top_p": 0.6},
	} {
		_, client := newFake(t)
		if _, err := client.Call(context.Background(), "mode", request); err != nil {
			t.Fatalf("request=%v: %v", request, err)
		}
	}
}

func TestNewSupportsReadonlyMetadataPreflight(t *testing.T) {
	f, _ := newFake(t)
	managed := testCatalog()
	client, err := New(context.Background(), Options{GatewayURL: f.server.URL + "/v1", Key: testKey, Model: testModel, Catalog: &managed})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	info := client.Info()
	if info.InstanceID != testInstance || info.InventoryFingerprint != testInventory || info.Model != testModel {
		t.Fatalf("info=%+v", info)
	}
	if _, err := client.Call(context.Background(), "no-output", map[string]any{}); err == nil {
		t.Fatal("inference ran without evidence directory")
	}
	if f.posts != 0 {
		t.Fatal("read-only preflight called inference")
	}
	missing := filepath.Join(t.TempDir(), "must-not-create")
	if _, err := New(context.Background(), Options{GatewayURL: f.server.URL + "/v1", Key: testKey, OutputDir: missing, Model: testModel, Catalog: &managed}); err == nil {
		t.Fatal("created missing evidence directory")
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatal("preflight created output path")
	}
}

func TestCallRejectsProtocolAndPolicyDrift(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		mutate func(*fakeGateway)
	}{
		{"marker", Protocol, func(f *fakeGateway) { f.badMarker = true }},
		{"error-envelope", Protocol, func(f *fakeGateway) { f.response = `{"error":{"message":"synthetic error"}}` }},
		{"missing-finish", Protocol, func(f *fakeGateway) {
			f.response = `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`
		}},
		{"length", OutputLimit, func(f *fakeGateway) { f.response = strings.ReplaceAll(f.response, `"stop"`, `"length"`) }},
		{"multiple-choices", Protocol, func(f *fakeGateway) { f.response = `{"id":"c","choices":[{"index":0},{"index":1}]}` }},
		{"inventory", Protocol, func(f *fakeGateway) {
			f.mutate = func(r *gateway.RequestReport) { r.InventoryFingerprint = strings.Repeat("3", 64) }
		}},
		{"instance", Infrastructure, func(f *fakeGateway) {
			f.mutate = func(r *gateway.RequestReport) { r.InstanceID = strings.Repeat("3", 32) }
		}},
		{"report-model", Protocol, func(f *fakeGateway) { f.mutate = func(r *gateway.RequestReport) { r.Record.Model = "other" } }},
		{"policy", Policy, func(f *fakeGateway) {
			f.mutate = func(r *gateway.RequestReport) { r.Record.Controls.SamplingMode = "non_thinking" }
		}},
		{"sampler-source", Policy, func(f *fakeGateway) {
			f.mutate = func(r *gateway.RequestReport) { r.Record.Controls.Sampling[0].Source = gateway.ControlClient }
		}},
		{"explicit-reasoning", Policy, func(f *fakeGateway) { f.mutate = func(r *gateway.RequestReport) { r.Record.Controls.Reasoning = nil } }},
		{"missing-report", Infrastructure, func(f *fakeGateway) { f.missingReport = true }},
		{"missing-metrics", Infrastructure, func(f *fakeGateway) {
			f.mutate = func(r *gateway.RequestReport) { r.Record.BackendTimings.PromptTokens = nil }
		}},
		{"wrong-counters", Protocol, func(f *fakeGateway) {
			f.mutate = func(r *gateway.RequestReport) { r.Record.Tokens.Cached = pointer(int64(20)) }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, client := newFake(t)
			test.mutate(f)
			_, err := client.Call(context.Background(), "failure", map[string]any{"reasoning_effort": "xhigh"})
			var classified *Error
			if !errors.As(err, &classified) || classified.Kind != test.kind {
				t.Fatalf("error=%v, want %s", err, test.kind)
			}
			if _, err := os.Stat(filepath.Join(client.output, "failure.response.json")); err != nil {
				t.Fatal("lost failure response:", err)
			}
		})
	}
}

func TestEvidenceOrphanCollisionBeforeInference(t *testing.T) {
	f, client := newFake(t)
	if err := atomicfile.Write(filepath.Join(client.output, "orphan.response.json"), []byte("old"), 0600, atomicfile.Create); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Call(context.Background(), "orphan", map[string]any{}); err == nil {
		t.Fatal("accepted orphan response collision")
	}
	if f.posts != 0 {
		t.Fatal("started inference for colliding evidence")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type canceledBody struct {
	ctx    context.Context
	read   chan struct{}
	sent   bool
	prefix string
}

func (b *canceledBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		prefix := b.prefix
		if prefix == "" {
			prefix = "data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
		}
		n := copy(p, []byte(prefix))
		close(b.read)
		return n, nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *canceledBody) Close() error { return nil }

func TestCancellationKeepsPartialReplyAndCanceledReport(t *testing.T) {
	f, client := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read := make(chan struct{})
	id := "r" + testInstance + "-0000000000000001"
	f.report = makeReport(id, map[string]any{}, true, "stop")
	f.report.Record.Outcome = gateway.OutcomeCanceled
	f.report.Record.Completion = gateway.CompletionMetadata{Observation: gateway.CompletionPartial}
	f.report.Record.Tokens = gateway.TokenUsage{}
	f.report.Record.BackendTimings = gateway.BackendTimings{}
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		f.report.Record.SessionID = r.Header.Get(gateway.SessionIDHeader)
		f.mu.Unlock()
		h := http.Header{}
		h.Set(gateway.IdentityHeader, gateway.IdentityValue)
		h.Set(gateway.RequestIDHeader, id)
		h.Set("Content-Type", "text/event-stream")
		return &http.Response{StatusCode: 200, Header: h, Body: &canceledBody{ctx: r.Context(), read: read}}, nil
	})
	go func() { <-read; cancel() }()
	answer, err := client.Call(ctx, "canceled", map[string]any{"stream": true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation identity: %v", err)
	}
	if answer.Report.Record == nil || answer.Report.Record.Outcome != gateway.OutcomeCanceled {
		t.Fatalf("lost canceled report: %+v", answer.Report)
	}
	if !answer.CancellationVerified {
		t.Fatalf("valid canceled SSE did not verify: %v", err)
	}
	for _, name := range []string{"canceled.response.sse", "canceled.report.json"} {
		if _, err := os.Stat(filepath.Join(client.output, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCancellationBeforeHeadersRecoversExactRequest(t *testing.T) {
	f, client := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := "r" + testInstance + "-0000000000000001"
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		f.report = makeReport(id, map[string]any{}, false, "stop")
		f.report.Record.SessionID = r.Header.Get(gateway.SessionIDHeader)
		f.report.Record.Outcome = gateway.OutcomeCanceled
		f.report.Record.Completion = gateway.CompletionMetadata{Observation: gateway.CompletionUnavailable}
		f.report.Record.Tokens = gateway.TokenUsage{}
		f.report.Record.BackendTimings = gateway.BackendTimings{}
		f.report.Record.HTTPStatus = 0
		f.mu.Unlock()
		cancel()
		return nil, context.Canceled
	})
	answer, err := client.Call(ctx, "before-headers", map[string]any{})
	if !errors.Is(err, context.Canceled) || answer.RequestID != id || answer.Report.Record == nil || answer.Report.Record.Outcome != gateway.OutcomeCanceled {
		t.Fatalf("answer=%+v error=%v", answer, err)
	}
	if !answer.CancellationVerified {
		t.Fatalf("valid pre-header cancellation did not verify: %v", err)
	}
	for _, name := range []string{"before-headers.correlation.json", "before-headers.response.json", "before-headers.report.json"} {
		if _, err := os.Stat(filepath.Join(client.output, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCancellationValidationCannotHideOtherFailures(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*gateway.RequestReport, http.Header)
		collision string
		prefix    string
		status    int
	}{
		{name: "wrong-mode", mutate: func(r *gateway.RequestReport, _ http.Header) { r.Record.Controls.SamplingMode = "non_thinking" }},
		{name: "wrong-source", mutate: func(r *gateway.RequestReport, _ http.Header) {
			r.Record.Controls.Sampling[0].Source = gateway.ControlClient
		}},
		{name: "wrong-default", mutate: func(r *gateway.RequestReport, _ http.Header) { r.Record.Controls.Sampling[0].Value = "999" }},
		{name: "wrong-reasoning-source", mutate: func(r *gateway.RequestReport, _ http.Header) {
			r.Record.Controls.Reasoning[0].Source = gateway.ControlDefault
		}},
		{name: "wrong-inventory", mutate: func(r *gateway.RequestReport, _ http.Header) { r.InventoryFingerprint = strings.Repeat("3", 64) }},
		{name: "wrong-instance", mutate: func(r *gateway.RequestReport, _ http.Header) { r.InstanceID = strings.Repeat("3", 32) }},
		{name: "wrong-model", mutate: func(r *gateway.RequestReport, _ http.Header) { r.Record.Model = "other" }},
		{name: "wrong-stream", mutate: func(r *gateway.RequestReport, _ http.Header) { r.Record.Stream = false }},
		{name: "wrong-session", mutate: func(r *gateway.RequestReport, _ http.Header) {
			r.Record.SessionID = "00000000-0000-4000-8000-000000000001"
		}},
		{name: "wrong-report-outcome", mutate: func(r *gateway.RequestReport, _ http.Header) { r.Record.Outcome = gateway.OutcomeSucceeded }},
		{name: "wrong-report-status", mutate: func(r *gateway.RequestReport, _ http.Header) { r.Record.HTTPStatus = 500 }},
		{name: "wrong-http-status", status: 500},
		{name: "wrong-marker", mutate: func(_ *gateway.RequestReport, h http.Header) { h.Set(gateway.IdentityHeader, "foreign") }},
		{name: "duplicate-marker", mutate: func(_ *gateway.RequestReport, h http.Header) { h.Add(gateway.IdentityHeader, gateway.IdentityValue) }},
		{name: "wrong-content-type", mutate: func(_ *gateway.RequestReport, h http.Header) { h.Set("Content-Type", "application/json") }},
		{name: "duplicate-request-id", mutate: func(_ *gateway.RequestReport, h http.Header) {
			h.Add(gateway.RequestIDHeader, h.Get(gateway.RequestIDHeader))
		}},
		{name: "response-save", collision: ".response.sse"},
		{name: "report-save", collision: ".report.json"},
		{name: "backend-error-envelope", prefix: "data: {\"error\":{\"message\":\"synthetic\"}}\n\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, client := newFake(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			read := make(chan struct{})
			id := "r" + testInstance + "-0000000000000001"
			client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				h := http.Header{}
				h.Set(gateway.IdentityHeader, gateway.IdentityValue)
				h.Set(gateway.RequestIDHeader, id)
				h.Set("Content-Type", "text/event-stream")
				f.mu.Lock()
				f.report = makeReport(id, map[string]any{"reasoning_effort": "xhigh"}, true, "stop")
				f.report.Record.Outcome = gateway.OutcomeCanceled
				f.report.Record.SessionID = r.Header.Get(gateway.SessionIDHeader)
				f.report.Record.Completion = gateway.CompletionMetadata{Observation: gateway.CompletionPartial}
				f.report.Record.Tokens = gateway.TokenUsage{}
				f.report.Record.BackendTimings = gateway.BackendTimings{}
				if test.mutate != nil {
					test.mutate(&f.report, h)
				}
				f.mu.Unlock()
				if test.collision != "" {
					if err := os.Mkdir(filepath.Join(client.output, "negative"+test.collision), 0700); err != nil {
						t.Fatal(err)
					}
				}
				status := test.status
				if status == 0 {
					status = 200
				}
				return &http.Response{StatusCode: status, Header: h, Body: &canceledBody{ctx: r.Context(), read: read, prefix: test.prefix}}, nil
			})
			go func() { <-read; cancel() }()
			answer, err := client.Call(ctx, "negative", map[string]any{"stream": true, "reasoning_effort": "xhigh"})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation error: %v", err)
			}
			if answer.CancellationVerified {
				t.Fatalf("untrusted canceled response verified despite %s: %v", test.name, err)
			}
		})
	}
}

func TestCancellationBeforeHeadersStillRequiresPolicy(t *testing.T) {
	for _, bad := range []string{"policy", "source", "save"} {
		t.Run(bad, func(t *testing.T) {
			f, client := newFake(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := "r" + testInstance + "-0000000000000001"
			client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				f.mu.Lock()
				f.report = makeReport(id, map[string]any{"reasoning_effort": "none"}, false, "stop")
				f.report.Record.SessionID = r.Header.Get(gateway.SessionIDHeader)
				f.report.Record.Outcome = gateway.OutcomeCanceled
				if bad == "policy" {
					f.report.Record.Controls.SamplingMode = "thinking"
				}
				if bad == "source" {
					f.report.Record.Controls.Sampling[0].Source = gateway.ControlClient
				}
				f.mu.Unlock()
				if bad == "save" {
					if err := os.Mkdir(filepath.Join(client.output, "early.report.json"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				cancel()
				return nil, context.Canceled
			})
			answer, err := client.Call(ctx, "early", map[string]any{"reasoning_effort": "none"})
			if !errors.Is(err, context.Canceled) || answer.CancellationVerified {
				t.Fatalf("answer=%+v error=%v", answer, err)
			}
		})
	}
}

func TestPartialJSONCancellationCanVerifyWithoutCompletionMetrics(t *testing.T) {
	f, client := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read := make(chan struct{})
	id := "r" + testInstance + "-0000000000000001"
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		f.report = makeReport(id, map[string]any{"reasoning_effort": "none"}, false, "stop")
		f.report.Record.SessionID = r.Header.Get(gateway.SessionIDHeader)
		f.report.Record.Outcome = gateway.OutcomeCanceled
		f.report.Record.Completion = gateway.CompletionMetadata{Observation: gateway.CompletionPartial}
		f.report.Record.Tokens = gateway.TokenUsage{}
		f.report.Record.BackendTimings = gateway.BackendTimings{}
		f.mu.Unlock()
		h := http.Header{}
		h.Set(gateway.IdentityHeader, gateway.IdentityValue)
		h.Set(gateway.RequestIDHeader, id)
		h.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: 200, Header: h, Body: &canceledBody{ctx: r.Context(), read: read, prefix: `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"partial`}}, nil
	})
	go func() { <-read; cancel() }()
	answer, err := client.Call(ctx, "partial-json", map[string]any{"reasoning_effort": "none"})
	if !errors.Is(err, context.Canceled) || !answer.CancellationVerified {
		t.Fatalf("answer=%+v error=%v", answer, err)
	}
}

func TestNativeDiagnosticsAndTokenizer(t *testing.T) {
	_, client := newFake(t)
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("model") != testModel {
			t.Error("missing native model selection")
		}
		if r.URL.Path == "/slots" {
			_, _ = io.WriteString(w, `[{"id":0,"is_processing":true,"n_prompt_tokens":50,"next_token":[{"n_decoded":3}]}]`)
			return
		}
		if r.URL.Path != "/tokenize" || r.Method != "POST" {
			t.Error("unexpected native route")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["add_special"] != false || body["content"] != "synthetic" || body["model"] != testModel {
			t.Errorf("tokenizer body=%v", body)
		}
		_, _ = io.WriteString(w, `{"tokens":[1,20,3]}`)
	}))
	defer native.Close()
	client.native = native.URL
	slots, err := client.Slots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 || !slots[0].Processing || slots[0].DecodedTokens == nil || *slots[0].DecodedTokens != 3 || slots[0].PromptTokens == nil || *slots[0].PromptTokens != 50 {
		t.Fatalf("slots=%+v", slots)
	}
	if _, err := os.Stat(filepath.Join(client.output, "slots-000001.json")); err != nil {
		t.Fatal("missing native slot evidence:", err)
	}
	if _, err := client.Slots(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(client.output, "slots-000002.json")); err != nil {
		t.Fatal("native slot evidence was not append-only:", err)
	}
	count, err := client.Tokenize(context.Background(), "synthetic")
	if err != nil || count != 3 {
		t.Fatalf("count=%d error=%v", count, err)
	}
}

func TestNativeOriginsAndMissingDiagnostic(t *testing.T) {
	for _, value := range []string{"http://localhost:1234", "http://192.0.2.1:1234", "https://127.0.0.1:1234", "http://127.0.0.1:1234/path", "http://user@127.0.0.1:1234", "http://127.0.0.1:1234?x=1"} {
		if validateNative(value) == nil {
			t.Errorf("accepted native origin %s", value)
		}
	}
	for _, value := range []string{"http://127.0.0.1:1234", "http://[::1]:1234"} {
		if err := validateNative(value); err != nil {
			t.Error(err)
		}
	}
	_, client := newFake(t)
	if _, err := client.Slots(context.Background()); err == nil {
		t.Fatal("invented missing native slots")
	}
	if _, err := client.Tokenize(context.Background(), "a"); err == nil {
		t.Fatal("invented missing tokenizer")
	}
}
