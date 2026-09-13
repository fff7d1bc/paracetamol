// Package gateway owns the verified text-inference registry, allocation
// scheduler, OpenAI-compatible data plane, and exact backend lifecycle.
package gateway

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/platform"
	"paracetamol/internal/textmodel"
	"paracetamol/internal/verification"
)

type Model struct {
	ID               string            `json:"id"`
	Application      string            `json:"application"`
	Context          int64             `json:"context"`
	MaxOutputTokens  int64             `json:"max_output_tokens"`
	AgentTools       bool              `json:"agent_tools"`
	ReasoningControl string            `json:"reasoning_control,omitempty"`
	ReasoningLevels  []string          `json:"reasoning_levels,omitempty"`
	ReasoningDefault string            `json:"reasoning_default,omitempty"`
	ReasoningOff     bool              `json:"reasoning_off,omitempty"`
	Backend          textmodel.Backend `json:"-"`
	Bundle           string            `json:"-"`
}

type Diagnostic struct {
	Application string
	Model       string
	Reason      string
}

type Registry struct {
	Models      []Model
	Fingerprint string
	byID        map[string]Model
}

func BuildRegistry(managed catalog.Catalog, dataRoot string, applications []string, profile string, renderNodes []string, llamaBackend string) (Registry, []Diagnostic, error) {
	registry, diagnostics, readyByApplication, err := buildRegistry(managed, dataRoot, applications, profile, renderNodes, llamaBackend)
	if err != nil {
		return Registry{}, nil, err
	}
	for _, application := range applications {
		if readyByApplication[application] == 0 {
			return Registry{}, diagnostics, fmt.Errorf("selected gateway application %s has no verified schedulable model", application)
		}
	}
	return registry, diagnostics, nil
}

// DiscoverRegistry returns only candidate applications that currently have at
// least one receipt-verified model compatible with the selected hardware. A
// caller can therefore use it for an automatic default while retaining the
// strict BuildRegistry contract for explicit user selections.
func DiscoverRegistry(managed catalog.Catalog, dataRoot string, candidates []string, profile string, renderNodes []string, llamaBackend string) (Registry, []string, []Diagnostic, error) {
	registry, diagnostics, readyByApplication, err := buildRegistry(managed, dataRoot, candidates, profile, renderNodes, llamaBackend)
	if err != nil {
		return Registry{}, nil, nil, err
	}
	applications := make([]string, 0, len(candidates))
	for _, application := range candidates {
		if readyByApplication[application] > 0 {
			applications = append(applications, application)
		}
	}
	if len(applications) == 0 {
		return Registry{}, nil, diagnostics, fmt.Errorf("no built gateway application has a verified schedulable model")
	}
	filtered := diagnostics[:0]
	for _, diagnostic := range diagnostics {
		if readyByApplication[diagnostic.Application] > 0 {
			filtered = append(filtered, diagnostic)
		}
	}
	return registry, applications, filtered, nil
}

func buildRegistry(managed catalog.Catalog, dataRoot string, applications []string, profile string, renderNodes []string, llamaBackend string) (Registry, []Diagnostic, map[string]int, error) {
	modelProfile := platform.ModelProfile(profile, renderNodes)
	selected := make(map[string]bool, len(applications))
	for _, application := range applications {
		if application != string(textmodel.BackendLlamaCPP) && application != string(textmodel.BackendDwarfStar) {
			return Registry{}, nil, nil, fmt.Errorf("gateway does not support application %q", application)
		}
		if selected[application] {
			return Registry{}, nil, nil, fmt.Errorf("gateway application %q was selected more than once", application)
		}
		selected[application] = true
	}
	if len(selected) == 0 {
		return Registry{}, nil, nil, fmt.Errorf("gateway requires at least one application")
	}
	models, err := textmodel.All(managed)
	if err != nil {
		return Registry{}, nil, nil, err
	}
	store, err := verification.Load(dataRoot)
	if err != nil {
		return Registry{}, nil, nil, err
	}
	readyByApplication := make(map[string]int)
	diagnostics := []Diagnostic{}
	registry := Registry{byID: make(map[string]Model)}
	for _, candidate := range models {
		application := string(candidate.Backend)
		if !selected[application] {
			continue
		}
		if candidate.Backend == textmodel.BackendDwarfStar && (profile == "cpu" || len(renderNodes) != 1) {
			diagnostics = append(diagnostics, Diagnostic{Application: application, Model: candidate.ID, Reason: "requires exactly one GPU render node"})
			continue
		}
		if candidate.Backend == textmodel.BackendLlamaCPP {
			preset := managed.LlamaPresets[candidate.ID]
			if !preset.SupportsRuntime(llamaBackend, modelProfile) {
				diagnostics = append(diagnostics, Diagnostic{Application: application, Model: candidate.ID, Reason: "does not support llama.cpp backend " + llamaBackend + " on profile " + modelProfile})
				continue
			}
		}
		bundle, ok := managed.Bundles[candidate.Bundle]
		if !ok {
			return Registry{}, nil, nil, fmt.Errorf("text model %s references unknown bundle %s", candidate.ID, candidate.Bundle)
		}
		statuses, err := content.InspectBundle(store, managed, bundle, dataRoot, false)
		if err != nil {
			return Registry{}, nil, nil, err
		}
		ready := len(statuses) > 0
		var unavailable []string
		for _, status := range statuses {
			ready = ready && content.Ready(status.State)
			if !content.Ready(status.State) {
				unavailable = append(unavailable, fmt.Sprintf("%s=%s", status.Artifact.ID, status.State))
			}
		}
		if !ready {
			diagnostics = append(diagnostics, Diagnostic{Application: application, Model: candidate.ID, Reason: strings.Join(unavailable, ", ")})
			continue
		}
		model := Model{
			ID: candidate.ID, Application: application, Backend: candidate.Backend, Bundle: candidate.Bundle,
			Context: candidate.Context, MaxOutputTokens: candidate.MaxOutputTokens, AgentTools: candidate.AgentTools,
			ReasoningControl: candidate.ReasoningControl, ReasoningLevels: append([]string(nil), candidate.ReasoningLevels...),
			ReasoningDefault: candidate.ReasoningDefault, ReasoningOff: candidate.ReasoningOff,
		}
		if _, duplicate := registry.byID[model.ID]; duplicate {
			return Registry{}, nil, nil, fmt.Errorf("gateway model identifier %q is ambiguous", model.ID)
		}
		registry.byID[model.ID] = model
		registry.Models = append(registry.Models, model)
		readyByApplication[application]++
	}
	sort.Slice(registry.Models, func(i, j int) bool { return registry.Models[i].ID < registry.Models[j].ID })
	hash := sha256.New()
	for _, model := range registry.Models {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%d\x00%d\x00%t\x00%s\x00%s\x00%s\x00%t\x00",
			model.ID, model.Application, model.Bundle, model.Context, model.MaxOutputTokens,
			model.AgentTools, model.ReasoningControl, strings.Join(model.ReasoningLevels, "\x1f"),
			model.ReasoningDefault, model.ReasoningOff)
		bundle := managed.Bundles[model.Bundle]
		for _, artifactID := range bundle.Artifacts {
			artifact := managed.Artifacts[artifactID]
			fmt.Fprintf(hash, "%s\x00%d\x00%s\x00", artifact.ID, artifact.Size, artifact.SHA256)
		}
	}
	registry.Fingerprint = fmt.Sprintf("%x", hash.Sum(nil))
	return registry, diagnostics, readyByApplication, nil
}

func (registry Registry) Lookup(identifier string) (Model, bool) {
	model, ok := registry.byID[identifier]
	model.ReasoningLevels = append([]string(nil), model.ReasoningLevels...)
	return model, ok
}

func (registry Registry) IDs(application string) []string {
	var identifiers []string
	for _, model := range registry.Models {
		if application == "" || model.Application == application {
			identifiers = append(identifiers, model.ID)
		}
	}
	return identifiers
}
