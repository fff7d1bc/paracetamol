package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/verification"
)

func TestRegistryRequiresExplicitApplications(t *testing.T) {
	if _, _, err := BuildRegistry(catalog.Catalog{}, t.TempDir(), nil, "cpu", nil); err == nil {
		t.Fatal("empty application selection succeeded")
	}
}

func TestRegistryIncludesOnlyReceiptVerifiedModels(t *testing.T) {
	root := t.TempDir()
	artifact := catalog.Artifact{ID: "model", Destination: "model.gguf", Target: "llama-models", Size: 4, SHA256: "fixture"}
	managed := catalog.Catalog{
		Artifacts:        map[string]catalog.Artifact{"model": artifact},
		Bundles:          map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
		LlamaPresets:     map[string]catalog.LlamaPreset{"fixture": {ID: "fixture", Bundle: "bundle", Artifact: "model", DefaultContext: 4096}},
		DwarfStarPresets: map[string]catalog.DwarfStarPreset{},
	}
	path := content.ArtifactPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil); err == nil {
		t.Fatal("unverified model was schedulable")
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(path, 4, "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	registry, diagnostics, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 || len(registry.Models) != 1 || registry.Models[0].ID != "fixture" {
		t.Fatalf("registry=%#v diagnostics=%#v", registry, diagnostics)
	}
	managed.LlamaPresets["fixture"] = catalog.LlamaPreset{ID: "fixture", Bundle: "bundle", Artifact: "model", DefaultContext: 8192}
	changed, _, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Fingerprint == registry.Fingerprint {
		t.Fatal("model policy change did not alter the inventory fingerprint")
	}
}

func TestRegistryRejectsDwarfStarWithoutOneGPU(t *testing.T) {
	managed := catalog.Catalog{LlamaPresets: map[string]catalog.LlamaPreset{}, DwarfStarPresets: map[string]catalog.DwarfStarPreset{
		"deepseek": {ID: "deepseek", Bundle: "bundle", DefaultContext: 4096, MaxOutputTokens: 1024},
	}}
	if _, diagnostics, err := BuildRegistry(managed, t.TempDir(), []string{"dwarfstar"}, "cpu", nil); err == nil || len(diagnostics) != 1 {
		t.Fatalf("err=%v diagnostics=%#v", err, diagnostics)
	}
}
