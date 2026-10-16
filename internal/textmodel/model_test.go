package textmodel

import (
	"path/filepath"
	"reflect"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/project"
)

func TestLlamaPresetCapabilitiesPreserveBenchmarkReasoning(t *testing.T) {
	preset := catalog.LlamaPreset{
		Bundle: "bundle", DefaultContext: 262144, AgentTools: true,
		ReasoningControl: "effort", ReasoningLevels: []string{"low", "medium", "xhigh"},
		ReasoningDefault: "medium", ReasoningOff: true, SamplingPolicy: "policy",
	}
	model := FromLlamaPreset("model", preset)
	if model.ID != "model" || model.Backend != BackendLlamaCPP || model.Bundle != "bundle" ||
		model.Context != 262144 || model.MaxOutputTokens != 16384 || !model.AgentTools || model.SamplingPolicy != "policy" {
		t.Fatalf("capabilities lost: %+v", model)
	}
	if ReasoningDefault(model) != "medium" || !reflect.DeepEqual(ReasoningLevels(model), []string{"off", "low", "medium", "xhigh"}) {
		t.Fatalf("benchmark reasoning changed: %+v", model)
	}
	model.ReasoningLevels[0] = "changed"
	if preset.ReasoningLevels[0] != "low" {
		t.Fatal("model aliases catalog reasoning levels")
	}
}

func TestRepositoryModelsHaveUniqueBackendNeutralIdentities(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	models, err := All(managed)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != len(managed.LlamaPresets)+len(managed.DwarfStarPresets) {
		t.Fatalf("models=%d", len(models))
	}
	last := ""
	for _, model := range models {
		if model.ID <= last {
			t.Fatalf("models are not uniquely sorted: %q after %q", model.ID, last)
		}
		last = model.ID
	}
}
