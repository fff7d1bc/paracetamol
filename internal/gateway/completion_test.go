package gateway

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func completionObject(t *testing.T, payload string) map[string]json.RawMessage {
	t.Helper()
	object, ok := responseObject([]byte(payload))
	if !ok {
		t.Fatalf("invalid test response: %s", payload)
	}
	return object
}

func TestCompletionCollectorClosedReasonsAndOrdering(t *testing.T) {
	var collector completionCollector
	reasons := []string{"stop", "length", "tool_calls", "function_call", "content_filter"}
	for index := len(reasons) - 1; index >= 0; index-- {
		collector.observeObject(completionObject(t, fmt.Sprintf(`{"choices":[{"index":%d,"finish_reason":%q}]}`, index, reasons[index])))
	}
	metadata := collector.metadata(false)
	if metadata.Observation != CompletionAvailable || metadata.StreamDone != nil || len(metadata.FinishReasons) != len(reasons) {
		t.Fatalf("metadata=%+v", metadata)
	}
	for index, choice := range metadata.FinishReasons {
		if choice.Index != index || choice.FinishReason != reasons[index] {
			t.Fatalf("choice=%+v", choice)
		}
	}
	// Returning or mutating a snapshot must not change retained evidence.
	metadata.FinishReasons[0].FinishReason = "caller mutation"
	if collector.metadata(false).FinishReasons[0].FinishReason != "stop" {
		t.Fatal("metadata aliases collector state")
	}
}

func TestCompletionCollectorNoInventedFinish(t *testing.T) {
	for _, payload := range []string{
		`{}`, `{"choices":null}`, `{"choices":[]}`, `{"usage":{"completion_tokens":12}}`,
		`{"choices":[{"index":0}]}`, `{"choices":[{"index":0,"finish_reason":null}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			var collector completionCollector
			collector.observeObject(completionObject(t, payload))
			collector.markStreamDone()
			for _, stream := range []bool{false, true} {
				metadata := collector.metadata(stream)
				if metadata.Observation != CompletionUnavailable || len(metadata.FinishReasons) != 0 {
					t.Fatalf("stream=%t metadata=%+v", stream, metadata)
				}
			}
		})
	}
}

func TestCompletionCollectorStreamDoneIsIndependentEvidence(t *testing.T) {
	var collector completionCollector
	collector.observeObject(completionObject(t, `{"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`))
	collector.observeObject(completionObject(t, `{"choices":[{"index":0,"delta":{"content":"private output"},"finish_reason":null}]}`))
	collector.observeObject(completionObject(t, `{"choices":[{"index":0,"finish_reason":"length"}]}`))
	beforeDone := collector.metadata(true)
	if beforeDone.Observation != CompletionPartial || beforeDone.StreamDone == nil || *beforeDone.StreamDone {
		t.Fatalf("before DONE=%+v", beforeDone)
	}
	collector.markStreamDone()
	afterDone := collector.metadata(true)
	if afterDone.Observation != CompletionAvailable || afterDone.StreamDone == nil || !*afterDone.StreamDone || afterDone.FinishReasons[0].FinishReason != "length" {
		t.Fatalf("after DONE=%+v", afterDone)
	}
	if *beforeDone.StreamDone {
		t.Fatal("snapshot stream-done pointer changed")
	}
	collector.observeObject(completionObject(t, `{"usage":{"completion_tokens":1}}`))
	if collector.metadata(true).Observation != CompletionPartial {
		t.Fatal("accepted response data after DONE as a complete observation")
	}
}

func TestCompletionCollectorMissingAndConflictingChoices(t *testing.T) {
	var collector completionCollector
	collector.observeObject(completionObject(t, `{"choices":[{"index":1,"finish_reason":null},{"index":0,"finish_reason":"stop"}]}`))
	if collector.metadata(false).Observation != CompletionPartial {
		t.Fatal("unfinished second choice concealed")
	}
	collector.observeObject(completionObject(t, `{"choices":[{"index":1,"finish_reason":"tool_calls"}]}`))
	if collector.metadata(false).Observation != CompletionAvailable {
		t.Fatal("normal null-to-finish transition marked invalid")
	}
	collector.observeObject(completionObject(t, `{"choices":[{"index":0,"finish_reason":"stop"}]}`))
	if collector.metadata(false).Observation != CompletionAvailable {
		t.Fatal("same finish reason treated as a conflict")
	}
	collector.observeObject(completionObject(t, `{"choices":[{"index":0,"finish_reason":"length"}]}`))
	collector.observeObject(completionObject(t, `{"choices":[{"index":0,"finish_reason":"stop"}]}`))
	metadata := collector.metadata(false)
	if metadata.Observation != CompletionPartial || !reflect.DeepEqual(metadata.FinishReasons, []ChoiceFinishReason{{Index: 1, FinishReason: "tool_calls"}}) {
		t.Fatalf("contradictory finish was retained: %+v", metadata)
	}
}

func TestCompletionCollectorMalformedFieldsDoNotBecomeFinishes(t *testing.T) {
	for _, payload := range []string{
		`{"choices":{}}`, `{"choices":"private"}`, `{"choices":[null]}`, `{"choices":["private"]}`,
		`{"choices":[{"finish_reason":"stop"}]}`, `{"choices":[{"index":null,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":-1,"finish_reason":"stop"}]}`, `{"choices":[{"index":16,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":1.0,"finish_reason":"stop"}]}`, `{"choices":[{"index":1e0,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":"0","finish_reason":"stop"}]}`, `{"choices":[{"index":true,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":99999999999999999999999999,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"finish_reason":1}]}`, `{"choices":[{"index":0,"finish_reason":false}]}`,
		`{"choices":[{"index":0,"finish_reason":{"private":"secret"}}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			var collector completionCollector
			collector.observeObject(completionObject(t, payload))
			metadata := collector.metadata(false)
			if metadata.Observation != CompletionPartial || len(metadata.FinishReasons) != 0 {
				t.Fatalf("metadata=%+v", metadata)
			}
		})
	}
}

func TestCompletionCollectorUnknownStringsAndIgnoredFieldsArePrivate(t *testing.T) {
	const private = "secret-prompt-tool-name-and-arguments"
	var collector completionCollector
	collector.observeObject(completionObject(t, fmt.Sprintf(`{"choices":[{"index":0,"finish_reason":%q,"message":{"content":%q,"tool_calls":[{"function":{"name":%q,"arguments":%q}}]}}],"private":%q}`, private, private, private, private, private)))
	metadata := collector.metadata(false)
	if metadata.Observation != CompletionPartial || !reflect.DeepEqual(metadata.FinishReasons, []ChoiceFinishReason{{Index: 0, FinishReason: "other"}}) {
		t.Fatalf("metadata=%+v", metadata)
	}
	serialized, err := json.Marshal(metadata)
	if err != nil || strings.Contains(string(serialized), private) || strings.Contains(fmt.Sprintf("%+v", collector), private) {
		t.Fatalf("private upstream string escaped: %s, err=%v", serialized, err)
	}
	// Escaping a known reason must not turn it into an arbitrary retained string.
	var escaped completionCollector
	escaped.observeObject(completionObject(t, `{"choices":[{"index":0,"finish_reason":"\u0073top"}]}`))
	if escaped.metadata(false).FinishReasons[0].FinishReason != "stop" {
		t.Fatal("escaped allowlisted reason not recognized")
	}
}

func TestCompletionCollectorBoundedIndicesAndObservationLoss(t *testing.T) {
	var collector completionCollector
	for index := 0; index < 100; index++ {
		collector.observeObject(completionObject(t, fmt.Sprintf(`{"choices":[{"index":%d,"finish_reason":"stop"}]}`, index)))
	}
	metadata := collector.metadata(false)
	if metadata.Observation != CompletionPartial || len(metadata.FinishReasons) != maxObservedCompletionChoices {
		t.Fatalf("bounded metadata=%+v", metadata)
	}
	for index, choice := range metadata.FinishReasons {
		if choice.Index != index {
			t.Fatalf("choice outside sorted bounded identities: %+v", choice)
		}
	}
	for _, loss := range []string{"malformed", "oversized", "dropped", "incomplete"} {
		t.Run(loss, func(t *testing.T) {
			var lost completionCollector
			lost.observeObject(completionObject(t, `{"choices":[{"index":0,"finish_reason":"stop"}]}`))
			lost.markPartial()
			lost.markStreamDone()
			if got := lost.metadata(true); got.Observation != CompletionPartial || len(got.FinishReasons) != 1 {
				t.Fatalf("lost observation=%+v", got)
			}
		})
	}
}

func TestCompletionCollectorMalformedAndOversizedChoiceEnvelope(t *testing.T) {
	for _, raw := range []string{
		`[{"index":0,"finish_reason":"stop"}`, // An incomplete envelope may still yield bounded partial evidence.
		`[{"index":0,"finish_reason":"stop"}] {}`,
		strings.Repeat(" ", maxObservedJSONBytes+1),
	} {
		var collector completionCollector
		collector.observeObject(map[string]json.RawMessage{"choices": json.RawMessage(raw)})
		if metadata := collector.metadata(false); metadata.Observation != CompletionPartial {
			t.Fatalf("malformed/oversized observation=%+v", metadata)
		}
	}
}

func TestCompletionCollectorDuplicateCompletionKeysAreNotChosen(t *testing.T) {
	for _, payload := range []string{
		`{"choices":[{"index":0,"finish_reason":"length","finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"index":1,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"finish_reason":"length"}],"choices":[{"index":0,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"finish_reason":"stop","finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"index":0,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"finish_reason":"stop"}],"choices":[{"index":0,"finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"finish_reason":"length","finish_\u0072eason":"stop"}]}`,
		`{"choices":[{"index":0,"finish_reason":"length"}],"\u0063hoices":[{"index":0,"finish_reason":"stop"}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			var collector completionCollector
			collector.observeResponse([]byte(payload), completionObject(t, payload))
			metadata := collector.metadata(false)
			if metadata.Observation != CompletionPartial || len(metadata.FinishReasons) != 0 {
				t.Fatalf("chose ambiguous completion metadata: %+v", metadata)
			}
		})
	}
}

func TestCompletionCollectorDuplicatePrivateFieldsAreNotInspected(t *testing.T) {
	const payload = `{"private":"secret-one","private":"secret-two","choices":[{"index":0,"finish_reason":"stop","private":"secret-three","private":"secret-four","message":{"index":0,"index":1,"finish_reason":"secret-five","finish_reason":"secret-six"}}]}`
	var collector completionCollector
	collector.observeResponse([]byte(payload), completionObject(t, payload))
	metadata := collector.metadata(false)
	if metadata.Observation != CompletionAvailable || !reflect.DeepEqual(metadata.FinishReasons, []ChoiceFinishReason{{Index: 0, FinishReason: "stop"}}) {
		t.Fatalf("inspected unrelated fields: %+v", metadata)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil || strings.Contains(string(encoded), "secret-") || strings.Contains(fmt.Sprintf("%+v", collector), "secret-") {
		t.Fatalf("private values retained: %s, err=%v", encoded, err)
	}
}

func TestCompletionCollectorRepeatedDoneIsPartial(t *testing.T) {
	var collector completionCollector
	collector.observeObject(completionObject(t, `{"choices":[{"index":0,"finish_reason":"stop"}]}`))
	collector.markStreamDone()
	collector.markStreamDone()
	metadata := collector.metadata(true)
	if metadata.Observation != CompletionPartial || metadata.StreamDone == nil || !*metadata.StreamDone || len(metadata.FinishReasons) != 1 {
		t.Fatalf("repeated sentinel concealed: %+v", metadata)
	}
}
