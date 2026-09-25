// Package agent owns managed Pi and Maki policy and private state.
package agent

import (
	"fmt"
	"strings"

	"paracetamol/internal/catalog"
	"paracetamol/internal/identity"
	"paracetamol/internal/textmodel"
)

var ProviderID = identity.StateNamespace

const RecommendedModel = "unsloth-qwen3.8-27b-mtp-ud-q8-k-xl"

// AgentModels intersects the catalog's client capabilities with the gateway's
// frozen advertised inventory. A nil inventory deliberately means all models
// and is used only by client management commands that do not contact a gateway.
func AgentModels(managed catalog.Catalog, advertised []string) ([]textmodel.Model, error) {
	models, err := textmodel.All(managed)
	if err != nil {
		return nil, err
	}
	var selected map[string]bool
	if advertised != nil {
		selected = make(map[string]bool, len(advertised))
		for _, identifier := range advertised {
			selected[identifier] = true
		}
	}
	result := make([]textmodel.Model, 0, len(models))
	for _, model := range models {
		if model.AgentTools && (selected == nil || selected[model.ID]) {
			result = append(result, model)
		}
	}
	return result, nil
}

func DefaultModel(models []textmodel.Model, client string) (string, string, string, error) {
	if len(models) == 0 {
		return "", "", "", fmt.Errorf("gateway advertises no model maintained for %s", client)
	}
	selected := models[0]
	for _, model := range models {
		if model.ID == RecommendedModel {
			selected = model
			break
		}
	}
	return ProviderID, selected.ID, textmodel.ReasoningDefault(selected), nil
}

func DwarfStarModel(managed catalog.Catalog) (textmodel.Model, error) {
	models, err := AgentModels(managed, nil)
	if err != nil {
		return textmodel.Model{}, err
	}
	var selected []textmodel.Model
	for _, model := range models {
		if model.Backend == textmodel.BackendDwarfStar {
			selected = append(selected, model)
		}
	}
	if len(selected) != 1 {
		return textmodel.Model{}, fmt.Errorf("expected exactly one agent-capable DwarfStar preset, found %d", len(selected))
	}
	return selected[0], nil
}

// These adapters keep benchmark reasoning validation on the same shared model
// policy without making benchmark code construct backend-neutral models.
func OutputLimit(context int64) int64 { return textmodel.OutputLimit(context) }

func ReasoningLevels(preset catalog.LlamaPreset) []string {
	return textmodel.ReasoningLevels(llamaTextModel(preset))
}

func ReasoningDefault(preset catalog.LlamaPreset) string {
	return textmodel.ReasoningDefault(llamaTextModel(preset))
}

func llamaTextModel(preset catalog.LlamaPreset) textmodel.Model {
	return textmodel.Model{
		ReasoningControl: preset.ReasoningControl, ReasoningLevels: append([]string(nil), preset.ReasoningLevels...),
		ReasoningDefault: preset.ReasoningDefault, ReasoningOff: preset.ReasoningOff,
	}
}

func ClientSampling(managed catalog.Catalog, identifier string) map[string]any {
	preset, ok := managed.LlamaPresets[identifier]
	if !ok || preset.SamplingPolicy != "" {
		return nil
	}
	shared := map[string]any{"temperature": 1.0, "top_p": 0.95, "top_k": 64, "min_p": 0.0, "presence_penalty": 0.0, "repeat_penalty": 1.0}
	if identifier == "bartowski-kat-coder-v2.5-dev-q8-0" {
		return map[string]any{"temperature": 1.0, "top_p": 0.95, "top_k": 20, "min_p": 0.0, "presence_penalty": 1.5, "repeat_penalty": 1.0}
	}
	if identifier == "ggml-org-gemma4-31b-it-q8-0-mtp" || strings.HasPrefix(identifier, "meta-models-muse-glimmer-") {
		return shared
	}
	return nil
}
