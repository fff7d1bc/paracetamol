// Package agent owns managed Pi and Maki policy and private state.
package agent

import (
	"fmt"
	"sort"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/identity"
	"paracetamol/internal/verification"
)

var ProviderID = identity.StateNamespace

const (
	DwarfStarProviderID = "dwarfstar"
	DwarfStarModel      = "deepseek-v4-flash-0731-q2-imatrix"
	RecommendedModel    = "qwen3.8-27b-mtp-ud-q8-k-xl"
	DwarfStarContext    = 131072
	DwarfStarOutput     = 16000
)

func OutputLimit(context int64) int64 {
	value := context / 4
	if value < 4096 {
		value = 4096
	}
	if value > 16384 {
		value = 16384
	}
	return value
}

func ReasoningLevels(preset catalog.LlamaPreset) []string {
	if preset.ReasoningControl == "" {
		return []string{"off"}
	}
	levels := append([]string(nil), preset.ReasoningLevels...)
	if preset.ReasoningControl == "toggle" {
		levels = []string{"high"}
	}
	if preset.ReasoningOff {
		levels = append([]string{"off"}, levels...)
	}
	return levels
}

func ReasoningDefault(preset catalog.LlamaPreset) string {
	if preset.ReasoningControl == "toggle" {
		return "high"
	}
	return preset.ReasoningDefault
}

func InstalledAgentPresets(managed catalog.Catalog, dataRoot string) ([]string, error) {
	store, err := verification.Load(dataRoot)
	if err != nil {
		return nil, err
	}
	var result []string
	for identifier, preset := range managed.LlamaPresets {
		if !preset.AgentTools {
			continue
		}
		bundle := managed.Bundles[preset.Bundle]
		statuses, err := content.InspectBundle(store, managed, bundle, dataRoot, false)
		if err != nil {
			return nil, err
		}
		ready := len(statuses) > 0
		for _, status := range statuses {
			ready = ready && content.Ready(status.State)
		}
		if ready {
			result = append(result, identifier)
		}
	}
	sort.Strings(result)
	return result, nil
}

func DefaultModel(managed catalog.Catalog, dataRoot, client string) (string, string, string, error) {
	installed, err := InstalledAgentPresets(managed, dataRoot)
	if err != nil {
		return "", "", "", err
	}
	selected := ""
	for _, identifier := range installed {
		if identifier == RecommendedModel {
			selected = identifier
			break
		}
	}
	if selected == "" && len(installed) > 0 {
		selected = installed[0]
	}
	if selected != "" {
		return ProviderID, selected, ReasoningDefault(managed.LlamaPresets[selected]), nil
	}
	bundle := managed.Bundles["dwarfstar-deepseek-v4-flash-0731-q2-imatrix"]
	store, loadErr := verification.Load(dataRoot)
	if loadErr == nil {
		statuses, inspectErr := content.InspectBundle(store, managed, bundle, dataRoot, false)
		ready := inspectErr == nil && len(statuses) > 0
		for _, status := range statuses {
			ready = ready && content.Ready(status.State)
		}
		if ready {
			return DwarfStarProviderID, DwarfStarModel, "high", nil
		}
	}
	return "", "", "", fmt.Errorf("no installed model is maintained for %s\n  llama.cpp: %s\n  DwarfStar: %s", client, identity.Command("content", "install", "llama-cpp", "qwen3.8"), identity.Command("content", "install", "dwarfstar", "flash-0731-q2-imatrix"))
}

func ClientSampling(managed catalog.Catalog, identifier string) map[string]any {
	preset := managed.LlamaPresets[identifier]
	if preset.SamplingPolicy != "" {
		return nil
	}
	shared := map[string]any{"temperature": 1.0, "top_p": 0.95, "top_k": 64, "min_p": 0.0, "presence_penalty": 0.0, "repeat_penalty": 1.0}
	if identifier == "kat-coder-v2.5-dev-q8-0" {
		return map[string]any{"temperature": 1.0, "top_p": 0.95, "top_k": 20, "min_p": 0.0, "presence_penalty": 1.5, "repeat_penalty": 1.0}
	}
	if identifier == "gemma4-31b-it-q8-0-mtp" || len(identifier) >= len("muse-glimmer") && identifier[:len("muse-glimmer")] == "muse-glimmer" {
		return shared
	}
	return nil
}
