package transport

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"paracetamol/tools/llama-acceptance/internal/contract"
)

// decode rejects duplicate keys recursively: choosing the last error, finish,
// tool argument or control would make acceptance depend on decoder behavior.
func decode(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) (any, error)
	value = func(depth int) (any, error) {
		if depth > 64 {
			return nil, errors.New("JSON nesting exceeds acceptance bound")
		}
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		delim, nested := token.(json.Delim)
		if !nested {
			return token, nil
		}
		switch delim {
		case '{':
			result := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, errors.New("JSON object key is not a string")
				}
				if _, exists := result[name]; exists {
					return nil, errors.New("duplicate JSON object field")
				}
				item, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				result[name] = item
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, errors.New("incomplete JSON object")
			}
			return result, nil
		case '[':
			result := []any{}
			for d.More() {
				item, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				result = append(result, item)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, errors.New("incomplete JSON array")
			}
			return result, nil
		default:
			return nil, errors.New("unexpected JSON delimiter")
		}
	}
	result, err := value(0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON data")
	}
	return result, nil
}

func object(raw []byte) (map[string]any, error) {
	value, err := decode(raw)
	if err != nil {
		return nil, err
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("expected one JSON object")
	}
	return result, nil
}

func parse(raw []byte, stream bool) (contract.Response, error) {
	message, finish, err := parseMessage(raw, stream)
	response := contract.Response{Message: message, Finish: finish}
	if err != nil {
		return response, wrap(Protocol, err)
	}
	if message["role"] != "assistant" || message["function_call"] != nil {
		return response, failure(Protocol, "invalid assistant role or legacy function call")
	}
	if value := message["content"]; value != nil {
		text, ok := value.(string)
		if !ok {
			return response, failure(Protocol, "assistant content is not text/null")
		}
		response.Text = text
	}
	for _, name := range []string{"reasoning_content", "reasoning"} {
		if value := message[name]; value != nil {
			text, ok := value.(string)
			if !ok {
				return response, failure(Protocol, "assistant reasoning is not text/null")
			}
			if response.Reasoning != "" && response.Reasoning != text {
				return response, failure(Protocol, "conflicting assistant reasoning fields")
			}
			response.Reasoning = text
		}
	}
	if value := message["tool_calls"]; value != nil {
		calls, ok := value.([]any)
		if !ok {
			return response, failure(Protocol, "tool calls are not an array")
		}
		ids := map[string]bool{}
		for _, value := range calls {
			call, ok := value.(map[string]any)
			if !ok {
				return response, failure(Protocol, "tool call is not an object")
			}
			id, _ := call["id"].(string)
			function, ok := call["function"].(map[string]any)
			if !ok || id == "" || ids[id] || call["type"] != "function" {
				return response, failure(Protocol, "tool call has no unique ID/typed function")
			}
			ids[id] = true
			name, _ := function["name"].(string)
			arguments, ok := function["arguments"].(string)
			if name == "" || !ok {
				return response, failure(Protocol, "tool function name/arguments are missing")
			}
			args, err := object([]byte(arguments))
			if err != nil {
				return response, failure(Protocol, "tool arguments are not one unambiguous JSON object")
			}
			response.Tools = append(response.Tools, contract.ToolCall{ID: id, Name: name, Arguments: args})
		}
	}
	switch finish {
	case "length":
		return response, failure(OutputLimit, "model exhausted the declared output budget")
	case "stop":
		if len(response.Tools) != 0 {
			return response, failure(Protocol, "stop finish contains unexpected tool calls")
		}
	case "tool_calls":
		if len(response.Tools) == 0 {
			return response, failure(Protocol, "tool_calls finish contains no tool calls")
		}
	default:
		return response, failure(Protocol, "missing or unsupported model finish")
	}
	return response, nil
}

func envelope(raw []byte) (map[string]any, []any, error) {
	value, err := object(raw)
	if err != nil {
		return nil, nil, err
	}
	if value["error"] != nil {
		return nil, nil, errors.New("backend returned an error envelope")
	}
	if id, ok := value["id"].(string); !ok || id == "" {
		return nil, nil, errors.New("response has no completion identity")
	}
	choices, ok := value["choices"].([]any)
	if !ok {
		return nil, nil, errors.New("response choices are missing/not an array")
	}
	return value, choices, nil
}

func choice(value any) (map[string]any, error) {
	c, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("choice is not an object")
	}
	i, ok := integer(c["index"])
	if !ok || i != 0 {
		return nil, errors.New("expected only choice index zero")
	}
	return c, nil
}

func parseMessage(raw []byte, stream bool) (map[string]any, string, error) {
	if !stream {
		_, choices, err := envelope(raw)
		if err != nil {
			return nil, "", err
		}
		if len(choices) != 1 {
			return nil, "", errors.New("expected exactly one completion choice")
		}
		c, err := choice(choices[0])
		if err != nil {
			return nil, "", err
		}
		message, ok := c["message"].(map[string]any)
		finish, finished := c["finish_reason"].(string)
		if !ok || !finished || finish == "" || c["delta"] != nil {
			return message, finish, errors.New("incomplete assistant message/finish")
		}
		return message, finish, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), maxResponse)
	message := map[string]any{}
	finish, identity := "", ""
	done, seenChoice := false, false
	var data []string
	consume := func() error {
		if len(data) == 0 {
			return nil
		}
		event := strings.Join(data, "\n")
		data = nil
		if done {
			return errors.New("SSE data after terminal marker")
		}
		if event == "[DONE]" {
			done = true
			return nil
		}
		wire, choices, err := envelope([]byte(event))
		if err != nil {
			return err
		}
		id := wire["id"].(string)
		if identity != "" && identity != id {
			return errors.New("SSE completion identity changed")
		}
		identity = id
		if len(choices) == 0 {
			if wire["usage"] == nil {
				return errors.New("empty SSE choices without usage")
			}
			return nil
		}
		if len(choices) != 1 || finish != "" {
			return errors.New("multiple/post-finish SSE choices")
		}
		c, err := choice(choices[0])
		if err != nil {
			return err
		}
		seenChoice = true
		delta, ok := c["delta"].(map[string]any)
		if !ok || c["message"] != nil {
			return errors.New("SSE has no delta or includes full message")
		}
		if err := mergeDelta(message, delta); err != nil {
			return err
		}
		if value := c["finish_reason"]; value != nil {
			text, ok := value.(string)
			if !ok || text == "" {
				return errors.New("invalid SSE finish")
			}
			finish = text
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case line == "":
			if err := consume(); err != nil {
				return message, finish, err
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			return message, finish, errors.New("unexpected SSE field")
		}
	}
	if err := scanner.Err(); err != nil {
		return message, finish, err
	}
	if len(data) != 0 || !done || !seenChoice || finish == "" {
		return message, finish, errors.New("incomplete/unterminated SSE completion")
	}
	return message, finish, nil
}

func mergeDelta(message, delta map[string]any) error {
	for key, value := range delta {
		if value == nil {
			if _, exists := message[key]; !exists {
				message[key] = nil
			}
			continue
		}
		if key == "role" {
			if value != "assistant" || message[key] != nil && message[key] != value {
				return errors.New("invalid/conflicting SSE role")
			}
			message[key] = value
			continue
		}
		if key == "tool_calls" {
			fragments, ok := value.([]any)
			if !ok {
				return errors.New("SSE tools are not an array")
			}
			calls, _ := message[key].([]any)
			for _, fragment := range fragments {
				tool, ok := fragment.(map[string]any)
				if !ok {
					return errors.New("SSE tool is not an object")
				}
				index, ok := integer(tool["index"])
				if !ok || index < 0 || index > int64(len(calls)) || index >= 64 {
					return errors.New("invalid/noncontiguous SSE tool index")
				}
				if int(index) == len(calls) {
					if tool["id"] == nil || tool["type"] != "function" {
						return errors.New("new SSE tool lacks ID/type")
					}
					calls = append(calls, map[string]any{})
				}
				dst := calls[index].(map[string]any)
				for field, item := range tool {
					if field == "index" {
						continue
					}
					if field == "id" || field == "type" {
						if old := dst[field]; old != nil && !reflect.DeepEqual(old, item) {
							return errors.New("conflicting SSE tool declaration")
						}
						dst[field] = item
						continue
					}
					if err := mergeValue(dst, field, item); err != nil {
						return err
					}
				}
			}
			message[key] = calls
			continue
		}
		if err := mergeValue(message, key, value); err != nil {
			return err
		}
	}
	return nil
}

func mergeValue(dst map[string]any, key string, value any) error {
	old, exists := dst[key]
	if !exists || old == nil {
		dst[key] = value
		return nil
	}
	if text, ok := value.(string); ok {
		previous, ok := old.(string)
		if !ok {
			return errors.New("SSE delta field changed type")
		}
		dst[key] = previous + text
		return nil
	}
	if nested, ok := value.(map[string]any); ok {
		previous, ok := old.(map[string]any)
		if !ok {
			return errors.New("SSE delta object changed type")
		}
		for field, item := range nested {
			if err := mergeValue(previous, field, item); err != nil {
				return err
			}
		}
		return nil
	}
	if !reflect.DeepEqual(old, value) {
		return fmt.Errorf("conflicting non-text SSE delta field %s", key)
	}
	return nil
}

func integer(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	result, err := strconv.ParseInt(string(number), 10, 64)
	return result, err == nil
}

func parseSlots(raw []byte) ([]contract.Slot, error) {
	value, err := decode(raw)
	if err != nil {
		return nil, wrap(Protocol, err)
	}
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > 64 {
		return nil, failure(Protocol, "native slots must be a nonempty bounded array")
	}
	slots := make([]contract.Slot, 0, len(items))
	seen := map[int64]bool{}
	for _, item := range items {
		value, ok := item.(map[string]any)
		if !ok {
			return nil, failure(Protocol, "native slot is not an object")
		}
		id, ok := integer(value["id"])
		if !ok || id < 0 || id > 64 || seen[id] {
			return nil, failure(Protocol, "native slot has no unique bounded ID")
		}
		seen[id] = true
		processing, ok := value["is_processing"].(bool)
		if !ok {
			return nil, failure(Protocol, "native slot lacks processing observation")
		}
		slot := contract.Slot{ID: int(id), Processing: processing}
		if value, exists := value["n_prompt_tokens"]; exists {
			n, ok := integer(value)
			if !ok || n < 0 {
				return nil, failure(Protocol, "invalid native prompt counter")
			}
			slot.PromptTokens = &n
		}
		if value, exists := value["next_token"]; exists {
			next, ok := value.([]any)
			if !ok || len(next) > 1 {
				return nil, failure(Protocol, "ambiguous native next-token counters")
			}
			if len(next) == 1 {
				token, ok := next[0].(map[string]any)
				if !ok {
					return nil, failure(Protocol, "invalid native next-token object")
				}
				if value, exists := token["n_decoded"]; exists {
					n, ok := integer(value)
					if !ok || n < 0 {
						return nil, failure(Protocol, "invalid native decode counter")
					}
					slot.DecodedTokens = &n
				}
			}
		}
		slots = append(slots, slot)
	}
	return slots, nil
}
