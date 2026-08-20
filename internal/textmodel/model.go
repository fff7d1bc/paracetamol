// Package textmodel derives backend-neutral serving and client capabilities
// from the application-specific catalog preset collections.
package textmodel

import (
	"fmt"
	"sort"

	"paracetamol/internal/catalog"
)

type Backend string

const (
	BackendLlamaCPP  Backend = "llama-cpp"
	BackendDwarfStar Backend = "dwarfstar"
)

type Model struct {
	ID               string
	Backend          Backend
	Bundle           string
	DSparkBundle     string
	Context          int64
	MaxOutputTokens  int64
	AgentTools       bool
	ReasoningControl string
	ReasoningLevels  []string
	ReasoningDefault string
	ReasoningOff     bool
}

func All(managed catalog.Catalog) ([]Model, error) {
	byID := make(map[string]Model, len(managed.LlamaPresets)+len(managed.DwarfStarPresets))
	for id, preset := range managed.LlamaPresets {
		byID[id] = Model{
			ID: id, Backend: BackendLlamaCPP, Bundle: preset.Bundle,
			Context: preset.DefaultContext, MaxOutputTokens: OutputLimit(preset.DefaultContext),
			AgentTools: preset.AgentTools, ReasoningControl: preset.ReasoningControl,
			ReasoningLevels:  append([]string(nil), preset.ReasoningLevels...),
			ReasoningDefault: preset.ReasoningDefault, ReasoningOff: preset.ReasoningOff,
		}
	}
	for id, preset := range managed.DwarfStarPresets {
		if _, duplicate := byID[id]; duplicate {
			return nil, fmt.Errorf("text model identifier %q is ambiguous", id)
		}
		byID[id] = Model{
			ID: id, Backend: BackendDwarfStar, Bundle: preset.Bundle, DSparkBundle: preset.DSparkBundle,
			Context: preset.DefaultContext, MaxOutputTokens: preset.MaxOutputTokens,
			AgentTools: preset.AgentTools, ReasoningControl: preset.ReasoningControl,
			ReasoningLevels:  append([]string(nil), preset.ReasoningLevels...),
			ReasoningDefault: preset.ReasoningDefault, ReasoningOff: preset.ReasoningOff,
		}
	}
	identifiers := make([]string, 0, len(byID))
	for id := range byID {
		identifiers = append(identifiers, id)
	}
	sort.Strings(identifiers)
	models := make([]Model, 0, len(identifiers))
	for _, id := range identifiers {
		models = append(models, byID[id])
	}
	return models, nil
}

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

func ReasoningLevels(model Model) []string {
	if model.ReasoningControl == "" {
		return []string{"off"}
	}
	levels := append([]string(nil), model.ReasoningLevels...)
	if model.ReasoningControl == "toggle" {
		levels = []string{"high"}
	}
	if model.ReasoningOff {
		levels = append([]string{"off"}, levels...)
	}
	return levels
}

func ReasoningDefault(model Model) string {
	if model.ReasoningControl == "toggle" {
		return "high"
	}
	return model.ReasoningDefault
}
