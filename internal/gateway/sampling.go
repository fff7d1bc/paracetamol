package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"paracetamol/internal/textmodel"
)

// applySamplingDefaults is the only request-rewriting policy in the gateway.
// It works from the exact frozen model, never from a model-name prefix. Raw
// values keep tools, messages and large integers out of float64 decoding.
func applySamplingDefaults(body []byte, model Model, controls *RequestControls) ([]byte, error) {
	if model.SamplingPolicy == "" {
		return body, nil
	}
	object, err := policyObject(body)
	if err != nil {
		return nil, err
	}
	thinking, err := requestThinking(object, model)
	if err != nil {
		return nil, err
	}
	defaults := model.samplingDefaults.Defaults(thinking)
	controls.SamplingPolicy = model.SamplingPolicy
	controls.SamplingMode = "thinking"
	if !thinking {
		controls.SamplingMode = "non_thinking"
	}
	changed := false
	for name, value := range defaults {
		if object[name] != nil && !isJSONNull(object[name]) {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("Cannot encode the managed sampling policy")
		}
		object[name] = encoded
		for index := range controls.Sampling {
			control := &controls.Sampling[index]
			if control.Name == name {
				control.Source, control.Value = ControlGatewayDefault, string(encoded)
			}
		}
		changed = true
	}
	if !changed {
		return body, nil
	}
	return json.Marshal(object)
}

// Reject ambiguous control objects instead of choosing whichever duplicate a
// particular engine happens to read. Error text never includes request data.
func policyObject(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, fmt.Errorf("Sampling policy requires JSON objects for reasoning controls")
	}
	object := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("Invalid sampling control object")
		}
		key, ok := token.(string)
		if !ok || object[key] != nil {
			return nil, fmt.Errorf("Duplicate JSON fields are ambiguous for sampling policy")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("Invalid sampling control object")
		}
		object[key] = raw
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("Invalid sampling control object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("Trailing data in sampling control object")
	}
	return object, nil
}

func requestThinking(object map[string]json.RawMessage, model Model) (bool, error) {
	thinking := model.ReasoningDefault != "off" && model.ReasoningDefault != "none" && model.ReasoningControl != ""
	selected, effort := false, ""
	selectMode := func(enabled bool) error {
		if !enabled && !model.ReasoningOff {
			return fmt.Errorf("This model does not support disabling reasoning")
		}
		if selected && thinking != enabled {
			return fmt.Errorf("Conflicting reasoning controls; send one thinking selection")
		}
		thinking, selected = enabled, true
		return nil
	}
	selectBool := func(raw json.RawMessage) error {
		var enabled bool
		if isJSONNull(raw) || json.Unmarshal(raw, &enabled) != nil {
			return fmt.Errorf("Thinking toggles must be boolean")
		}
		return selectMode(enabled)
	}
	selectEffort := func(raw json.RawMessage, topLevel bool) error {
		if raw == nil {
			return nil
		}
		var value string
		if isJSONNull(raw) || json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("reasoning_effort must be a supported string")
		}
		if (topLevel || model.Backend == textmodel.BackendDwarfStar) && value == "none" {
			return selectMode(false)
		}
		levels := model.ReasoningLevels
		if model.ReasoningControl == "toggle" {
			// Qwen3.6 has no effort scale. Positive OpenAI effort selections
			// still mean on, just as they do in its template.
			levels = []string{"minimal", "low", "medium", "high", "xhigh", "max"}
		}
		if !slices.Contains(levels, value) {
			return fmt.Errorf("Unsupported reasoning_effort for this model; use none at the top level to disable thinking")
		}
		if effort != "" && effort != value && model.ReasoningControl != "toggle" {
			return fmt.Errorf("Conflicting reasoning_effort selections")
		}
		effort = value
		return selectMode(true)
	}
	if err := selectEffort(object["reasoning_effort"], true); err != nil {
		return false, err
	}
	if raw := object["think"]; raw != nil && model.Backend == textmodel.BackendDwarfStar {
		if err := selectBool(raw); err != nil {
			return false, err
		}
	}
	if raw := object["thinking"]; raw != nil && model.Backend == textmodel.BackendDwarfStar {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("true")) || bytes.Equal(bytes.TrimSpace(raw), []byte("false")) {
			if err := selectBool(raw); err != nil {
				return false, err
			}
		} else {
			settings, err := policyObject(raw)
			if err != nil {
				return false, err
			}
			var kind string
			if json.Unmarshal(settings["type"], &kind) != nil || (kind != "enabled" && kind != "disabled") {
				return false, fmt.Errorf("thinking.type must be enabled or disabled")
			}
			if err := selectMode(kind == "enabled"); err != nil {
				return false, err
			}
		}
	}
	if raw := object["chat_template_kwargs"]; raw != nil {
		kwargs, err := policyObject(raw)
		if err != nil {
			return false, err
		}
		if raw := kwargs["enable_thinking"]; raw != nil {
			if err := selectBool(raw); err != nil {
				return false, err
			}
		}
		if err := selectEffort(kwargs["reasoning_effort"], false); err != nil {
			return false, err
		}
	}
	return thinking, nil
}
