package gateway

import (
	"bytes"
	"encoding/json"
	"io"
)

const maxObservedCompletionChoices = 16

type CompletionObservation string

const (
	CompletionAvailable   CompletionObservation = "available"
	CompletionPartial     CompletionObservation = "partial"
	CompletionUnavailable CompletionObservation = "unavailable"
)

// ChoiceFinishReason retains only a bounded choice identity and a closed reason.
// In particular, length is an output-limit finish, not proof of context loss.
type ChoiceFinishReason struct {
	Index        int    `json:"index"`
	FinishReason string `json:"finish_reason"`
}

// CompletionMetadata describes model-completion evidence, independently of the
// HTTP transport outcome. A successful proxy exchange need not report a finish.
type CompletionMetadata struct {
	Observation   CompletionObservation `json:"observation"`
	FinishReasons []ChoiceFinishReason  `json:"finish_reasons,omitempty"`
	StreamDone    *bool                 `json:"stream_done,omitempty"`
}

type completionChoice struct {
	seen     bool
	reason   string
	conflict bool
}

// The collector never retains response objects or arbitrary upstream strings.
// A fixed array bounds identities even when every event supplies new indices.
type completionCollector struct {
	choices    [maxObservedCompletionChoices]completionChoice
	partial    bool
	streamDone bool
}

func (collector *completionCollector) markPartial() {
	collector.partial = true
}

func (collector *completionCollector) markStreamDone() {
	if collector.streamDone {
		collector.markPartial()
	}
	collector.streamDone = true
}

// observeResponse checks only completion-owned keys before the decoded map can
// hide duplicates. Private message/tool objects are deliberately not inspected.
func (collector *completionCollector) observeResponse(raw []byte, object map[string]json.RawMessage) {
	if !uniqueCompletionKeys(raw, "choices") {
		collector.markPartial()
		return
	}
	collector.observeObject(object)
}

func (collector *completionCollector) observeObject(object map[string]json.RawMessage) {
	if collector.streamDone {
		collector.markPartial()
	}
	raw := object["choices"]
	if len(raw) == 0 || isJSONNull(raw) {
		return
	}
	if len(raw) > maxObservedJSONBytes {
		collector.markPartial()
		return
	}
	// Decode one choice at a time rather than allocating an upstream-sized
	// choice slice. Only the enclosing observer owns response framing/buffers.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		collector.markPartial()
		return
	}
	for decoder.More() {
		var choiceRaw json.RawMessage
		if err := decoder.Decode(&choiceRaw); err != nil {
			collector.markPartial()
			return
		}
		choice, ok := rawObject(choiceRaw)
		if !ok || !uniqueCompletionKeys(choiceRaw, "index", "finish_reason") {
			collector.markPartial()
			continue
		}
		var index int
		indexRaw := choice["index"]
		if len(indexRaw) == 0 || isJSONNull(indexRaw) || json.Unmarshal(indexRaw, &index) != nil || index < 0 || index >= maxObservedCompletionChoices {
			collector.markPartial()
			continue
		}
		state := &collector.choices[index]
		state.seen = true
		reasonRaw := choice["finish_reason"]
		// Normal streaming role/delta chunks have no finish reason, or null.
		if len(reasonRaw) == 0 || isJSONNull(reasonRaw) {
			continue
		}
		var upstreamReason string
		if json.Unmarshal(reasonRaw, &upstreamReason) != nil {
			collector.markPartial()
			continue
		}
		var reason string
		switch upstreamReason {
		case "stop":
			reason = "stop"
		case "length":
			reason = "length"
		case "tool_calls":
			reason = "tool_calls"
		case "function_call":
			reason = "function_call"
		case "content_filter":
			reason = "content_filter"
		default:
			reason = "other"
			collector.markPartial()
		}
		if state.conflict {
			continue
		}
		if state.reason != "" && state.reason != reason {
			// Do not choose a winner when an upstream contradicts itself.
			state.reason, state.conflict = "", true
			collector.markPartial()
			continue
		}
		state.reason = reason
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
		collector.markPartial()
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		collector.markPartial()
	}
}

// uniqueCompletionKeys scans direct object keys only. The fixed names supplied
// by its two callers keep duplicate tracking independent of upstream key counts.
func uniqueCompletionKeys(raw []byte, names ...string) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	var seen uint8
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		key, ok := token.(string)
		if !ok {
			return false
		}
		for index, name := range names {
			if key != name {
				continue
			}
			mask := uint8(1 << index)
			if seen&mask != 0 {
				return false
			}
			seen |= mask
		}
		var ignored json.RawMessage
		if decoder.Decode(&ignored) != nil {
			return false
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return false
	}
	var trailing any
	return decoder.Decode(&trailing) == io.EOF
}

func (collector *completionCollector) metadata(stream bool) CompletionMetadata {
	metadata := CompletionMetadata{Observation: CompletionUnavailable}
	partial, unfinished := collector.partial, false
	if stream {
		done := collector.streamDone
		metadata.StreamDone = &done
		partial = partial || !done
	}
	for index, choice := range collector.choices {
		if choice.reason != "" {
			metadata.FinishReasons = append(metadata.FinishReasons, ChoiceFinishReason{Index: index, FinishReason: choice.reason})
		} else if choice.seen {
			unfinished = true
		}
	}
	if partial || (unfinished && len(metadata.FinishReasons) > 0) {
		metadata.Observation = CompletionPartial
	} else if len(metadata.FinishReasons) > 0 {
		metadata.Observation = CompletionAvailable
	}
	return metadata
}
