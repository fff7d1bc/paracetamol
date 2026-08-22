package gateway

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func findControl(t *testing.T, controls []ObservedControl, name string) ObservedControl {
	t.Helper()
	for _, control := range controls {
		if control.Name == name {
			return control
		}
	}
	t.Fatalf("control %q was not observed: %#v", name, controls)
	return ObservedControl{}
}

func TestInspectRequestRecognizesReasoningAndSamplerSources(t *testing.T) {
	observation, err := inspectRequest([]byte(`{
  "model":"qwen",
  "stream":true,
  "reasoning_effort":"xhigh",
  "temperature":0.7,
  "top_p":null,
  "top_k":20,
  "messages":[{"role":"user","content":"do not retain me"}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if observation.Model != "qwen" || !observation.Stream {
		t.Fatalf("observation=%#v", observation)
	}
	effort := findControl(t, observation.Controls.Reasoning, "reasoning_effort")
	if effort.Source != ControlClient || effort.Value != "xhigh" {
		t.Fatalf("effort=%#v", effort)
	}
	temperature := findControl(t, observation.Controls.Sampling, "temperature")
	if temperature.Source != ControlClient || temperature.Value != "0.7" {
		t.Fatalf("temperature=%#v", temperature)
	}
	topP := findControl(t, observation.Controls.Sampling, "top_p")
	if topP.Source != ControlDefault || !topP.Provided {
		t.Fatalf("top_p=%#v", topP)
	}
	minP := findControl(t, observation.Controls.Sampling, "min_p")
	if minP.Source != ControlDefault || minP.Provided {
		t.Fatalf("min_p=%#v", minP)
	}
	if encoded := fmt.Sprintf("%#v", observation); strings.Contains(encoded, "do not retain me") {
		t.Fatalf("request observation retained message content: %s", encoded)
	}
}

func TestInspectRequestRecognizesNestedHarnessControlsWithoutEnforcement(t *testing.T) {
	for _, test := range []struct {
		name, body, field, value string
	}{
		{"maki toggle", `{"model":"qwen","chat_template_kwargs":{"enable_thinking":false}}`, "chat_template_kwargs.enable_thinking", "false"},
		{"pi strength", `{"model":"muse","chat_template_kwargs":{"reasoning_strength":"high","preserve_thinking":true}}`, "chat_template_kwargs.reasoning_strength", "high"},
		{"nested effort", `{"model":"qwen","chat_template_kwargs":{"reasoning_effort":"medium"}}`, "chat_template_kwargs.reasoning_effort", "medium"},
	} {
		t.Run(test.name, func(t *testing.T) {
			observation, err := inspectRequest([]byte(test.body))
			if err != nil {
				t.Fatal(err)
			}
			control := findControl(t, observation.Controls.Reasoning, test.field)
			if control.Source != ControlClient || control.Value != test.value {
				t.Fatalf("control=%#v", control)
			}
		})
	}
}

func TestInspectRequestTreatsMalformedAndConflictingControlsAsDiagnostics(t *testing.T) {
	body := []byte(`{
  "model":"qwen",
  "reasoning_effort":"secret reasoning-shaped prompt",
  "temperature":{"secret":"not retained"},
  "chat_template_kwargs":{"enable_thinking":false}
}`)
	observation, err := inspectRequest(body)
	if err != nil {
		t.Fatalf("diagnostic-only controls rejected the request: %v", err)
	}
	if temperature := findControl(t, observation.Controls.Sampling, "temperature"); temperature.Source != ControlInvalid || temperature.Value != "" {
		t.Fatalf("temperature=%#v", temperature)
	}
	if effort := findControl(t, observation.Controls.Reasoning, "reasoning_effort"); effort.Source != ControlInvalid || effort.Value != "" {
		t.Fatalf("effort=%#v", effort)
	}
	if encoded := fmt.Sprintf("%#v", observation); strings.Contains(encoded, "not retained") || strings.Contains(encoded, "secret reasoning-shaped prompt") {
		t.Fatalf("request observation retained malformed control content: %s", encoded)
	}

	conflicting, err := inspectRequest([]byte(`{"model":"qwen","reasoning_effort":"medium","chat_template_kwargs":{"enable_thinking":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(conflicting.Controls.Diagnostics, ","); !strings.Contains(got, "conflicting reasoning") {
		t.Fatalf("diagnostics=%q", got)
	}
}

func TestRequestLedgerIsBoundedNewestFirstAndConcurrent(t *testing.T) {
	var ordered requestLedger
	for index := 0; index < RecentRequestLimit+6; index++ {
		ordered.Add(RequestRecord{ID: fmt.Sprint(index)})
	}
	orderedRecent := ordered.Recent(3)
	if len(orderedRecent) != 3 || orderedRecent[0].ID != "69" || orderedRecent[1].ID != "68" || orderedRecent[2].ID != "67" {
		t.Fatalf("newest ordering=%#v", orderedRecent)
	}

	var ledger requestLedger
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for index := 0; index < 32; index++ {
				ledger.Add(RequestRecord{ID: fmt.Sprintf("%d-%d", worker, index)})
			}
		}(worker)
	}
	workers.Wait()
	recent := ledger.Recent(RecentRequestLimit)
	if len(recent) != RecentRequestLimit {
		t.Fatalf("recent count=%d", len(recent))
	}
	if recent[0].ID == recent[len(recent)-1].ID {
		t.Fatalf("ledger ordering did not preserve distinct entries: %#v", recent)
	}
	if got := ledger.Recent(3); len(got) != 3 {
		t.Fatalf("limited recent count=%d", len(got))
	}
}

func TestResponseObserverReadsJSONAndFragmentedSSEUsage(t *testing.T) {
	jsonObserver := newResponseObserver("application/json")
	jsonObserver.Observe([]byte(`{"usage":{"prompt_tokens":12,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":5}}}`))
	assertTokens(t, jsonObserver.Finish(), 12, 7, 5)

	sseObserver := newResponseObserver("text/event-stream; charset=utf-8")
	for _, fragment := range [][]byte{
		[]byte("data: {\"choices\":[]"),
		[]byte("}\n\n"),
		[]byte("data: {\"usage\":{\"input_tokens\":21,"),
		[]byte("\"output_tokens\":8,\"input_tokens_details\":{\"cached_tokens\":13}}}\n\n"),
		[]byte("data: [DONE]\n\n"),
	} {
		sseObserver.Observe(fragment)
	}
	assertTokens(t, sseObserver.Finish(), 21, 8, 13)
}

func TestResponseObserverFallsBackToLlamaTimingsAndBoundsInput(t *testing.T) {
	observer := newResponseObserver("application/json")
	observer.Observe([]byte(`{"timings":{"prompt_n":33,"predicted_n":9}}`))
	tokens := observer.Finish()
	if tokens.Input == nil || *tokens.Input != 33 || tokens.Output == nil || *tokens.Output != 9 || tokens.Cached != nil {
		t.Fatalf("tokens=%#v", tokens)
	}

	oversized := newResponseObserver("application/json")
	oversized.Observe(bytes.Repeat([]byte{'x'}, maxObservedJSONBytes+1))
	if tokens := oversized.Finish(); tokens.Input != nil || tokens.Output != nil || tokens.Cached != nil {
		t.Fatalf("oversized response produced tokens: %#v", tokens)
	}
}

func assertTokens(t *testing.T, tokens TokenUsage, input, output, cached int64) {
	t.Helper()
	if tokens.Input == nil || *tokens.Input != input || tokens.Output == nil || *tokens.Output != output || tokens.Cached == nil || *tokens.Cached != cached {
		t.Fatalf("tokens=%#v", tokens)
	}
}

func TestPrefixedLineWriterEmitsWholeAtomicLines(t *testing.T) {
	var output bytes.Buffer
	shared := NewAtomicLogWriter(&output)
	backend := newPrefixedLineWriter(shared, "llama-cpp | ")
	if _, err := backend.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("partial physical line was published: %q", output.String())
	}
	_, _ = backend.Write([]byte(" line\nsecond\nthird"))
	logLine(shared, "gateway | controller")
	backend.finish()
	want := "llama-cpp | partial line\nllama-cpp | second\ngateway | controller\nllama-cpp | third\n"
	if output.String() != want {
		t.Fatalf("output:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestControllerLogLineCannotInjectUnprefixedPhysicalLines(t *testing.T) {
	var output bytes.Buffer
	logLine(&output, "gateway | failed: %s", "first\nsecond\rthird")
	if got, want := output.String(), "gateway | failed: first second third\n"; got != want {
		t.Fatalf("log=%q want=%q", got, want)
	}
}

func TestSharedLogWriterDoesNotSpliceConcurrentPrefixes(t *testing.T) {
	var output bytes.Buffer
	shared := NewAtomicLogWriter(&output)
	backend := newPrefixedLineWriter(shared, "dwarfstar | ")
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for index := 0; index < 50; index++ {
				if worker%2 == 0 {
					_, _ = backend.Write([]byte(fmt.Sprintf("%d-%d\n", worker, index)))
				} else {
					logLine(shared, "gateway | %d-%d", worker, index)
				}
			}
		}(worker)
	}
	workers.Wait()
	backend.finish()
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if !strings.HasPrefix(line, "gateway | ") && !strings.HasPrefix(line, "dwarfstar | ") {
			t.Fatalf("interleaved log line=%q", line)
		}
	}
}
