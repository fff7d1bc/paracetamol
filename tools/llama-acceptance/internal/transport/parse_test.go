package transport

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"paracetamol/internal/gateway"
)

func event(raw string) string { return "data: " + raw + "\n\n" }

func funcReportMissingStreamDone(report *gateway.RequestReport) {
	report.Record.Completion.StreamDone = nil
}

func TestSSEPreservesReasoningToolsAndOtherFields(t *testing.T) {
	raw := ": heartbeat\n\n" +
		event(`{"id":"c","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"full ","extra":{"retained":true}},"finish_reason":null}]}`) +
		event(`{"id":"c","choices":[{"index":0,"delta":{"reasoning_content":"thought","tool_calls":[{"index":0,"id":"t","type":"function","function":{"name":"read","arguments":"{\"pa"}}]},"finish_reason":null}]}`) +
		event(`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"fixture.txt\"}"}}]},"finish_reason":null}]}`) +
		event(`{"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`) +
		event(`{"id":"c","choices":[],"usage":{"completion_tokens":8}}`) + event("[DONE]")
	answer, err := parse([]byte(raw), true)
	if err != nil {
		t.Fatal(err)
	}
	if answer.Reasoning != "full thought" || len(answer.Tools) != 1 || answer.Tools[0].Name != "read" || answer.Tools[0].Arguments["path"] != "fixture.txt" || answer.Message["extra"] == nil {
		t.Fatalf("lost assistant content: %+v", answer)
	}
	calls := answer.Message["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	if _, exists := call["index"]; exists {
		t.Fatal("retained streaming-only tool index in assistant history")
	}
	if call["function"].(map[string]any)["arguments"] != `{"path":"fixture.txt"}` {
		t.Fatal("lost original argument string")
	}
}

func TestSSERejectsMalformedAndTruncatedCompletions(t *testing.T) {
	start := event(`{"id":"c","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`)
	finish := event(`{"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	tests := map[string]string{
		"no-done":          start + finish,
		"no-finish":        start + event("[DONE]"),
		"no-role":          event(`{"id":"c","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + event("[DONE]"),
		"error":            event(`{"error":{"message":"synthetic"}}`) + event("[DONE]"),
		"after-done":       start + finish + event("[DONE]") + start,
		"duplicate-done":   start + finish + event("[DONE]") + event("[DONE]"),
		"after-finish":     start + finish + start + event("[DONE]"),
		"changed-id":       start + strings.ReplaceAll(finish, `"id":"c"`, `"id":"other"`) + event("[DONE]"),
		"truncated-event":  start + finish + "data: [DONE]",
		"multiple-choices": event(`{"id":"c","choices":[{"index":0,"delta":{}},{"index":1,"delta":{}}]}`) + event("[DONE]"),
		"no-choice":        event(`{"id":"c","choices":[],"usage":{}}`) + event("[DONE]"),
		"duplicate-field":  event(`{"id":"c","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":"stop","finish_reason":"length"}]}`) + event("[DONE]"),
		"bad-tool-index":   event(`{"id":"c","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":2,"id":"t","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`) + event("[DONE]"),
		"tool-arguments":   event(`{"id":"c","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"t","type":"function","function":{"name":"read","arguments":"{\"path\":\"a\",\"path\":\"b\"}"}}]},"finish_reason":"tool_calls"}]}`) + event("[DONE]"),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parse([]byte(raw), true); err == nil {
				t.Fatal("malformed SSE passed")
			}
		})
	}
}

func TestJSONRejectsAmbiguousOrInconsistentAnswers(t *testing.T) {
	for _, raw := range []string{
		`{"id":"c","error":{},"choices":[]}`,
		`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"a","content":"b"},"finish_reason":"stop"}]}`,
		`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":[]},"finish_reason":"stop"}]}`,
		`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"t","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"stop"}]}`,
		`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"tool_calls"}]}`,
		`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]} {}`,
	} {
		if _, err := parse([]byte(raw), false); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestSSECallRequiresMatchingReport(t *testing.T) {
	f, client := newFake(t)
	f.response = event(`{"id":"c","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`) + event("[DONE]")
	if _, err := client.Call(context.Background(), "stream", map[string]any{"stream": true}); err != nil {
		t.Fatal(err)
	}
	f.mutate = funcReportMissingStreamDone
	_, err := client.Call(context.Background(), "no-terminal-report", map[string]any{"stream": true})
	var classified *Error
	if !errors.As(err, &classified) || classified.Kind != Protocol {
		t.Fatalf("error=%v", err)
	}
}

func TestSlotMissingCountersStayAbsentAndMalformedCountersFail(t *testing.T) {
	slots, err := parseSlots([]byte(`[{"id":0,"is_processing":false,"next_token":[]}]`))
	if err != nil || len(slots) != 1 || slots[0].PromptTokens != nil || slots[0].DecodedTokens != nil {
		t.Fatalf("slots=%+v error=%v", slots, err)
	}
	for _, raw := range []string{
		`[]`,
		`[{"id":0}]`,
		`[{"id":0,"is_processing":false},{"id":0,"is_processing":true}]`,
		`[{"id":0,"is_processing":false,"n_prompt_tokens":-1}]`,
		`[{"id":0,"is_processing":true,"next_token":[{"n_decoded":1.5}]}]`,
		`[{"id":0,"is_processing":true,"next_token":[{"n_decoded":1},{"n_decoded":2}]}]`,
	} {
		if _, err := parseSlots([]byte(raw)); err == nil {
			t.Errorf("accepted native slots %s", raw)
		}
	}
}

func TestNoInferenceTimeoutAndNoProxy(t *testing.T) {
	_, client := newFake(t)
	transport := client.http.Transport.(*http.Transport)
	if client.http.Timeout != 0 || transport.ResponseHeaderTimeout != 0 || transport.Proxy != nil {
		t.Fatal("inference acquired a deadline/header timeout or ambient proxy")
	}
}
