package textmodel

import (
	"path/filepath"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/project"
)

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
	if len(models) != len(managed.LlamaPresets)+1 {
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
