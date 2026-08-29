package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/verification"
)

func TestRegistryRequiresExplicitApplications(t *testing.T) {
	if _, _, err := BuildRegistry(catalog.Catalog{}, t.TempDir(), nil, "cpu", nil, "rocm"); err == nil {
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
	if _, _, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm"); err == nil {
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
	registry, diagnostics, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 || len(registry.Models) != 1 || registry.Models[0].ID != "fixture" {
		t.Fatalf("registry=%#v diagnostics=%#v", registry, diagnostics)
	}
	managed.LlamaPresets["fixture"] = catalog.LlamaPreset{ID: "fixture", Bundle: "bundle", Artifact: "model", DefaultContext: 8192}
	changed, _, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Fingerprint == registry.Fingerprint {
		t.Fatal("model policy change did not alter the inventory fingerprint")
	}
	managed.LlamaPresets["fixture"] = catalog.LlamaPreset{ID: "fixture", Bundle: "bundle", Artifact: "model", DefaultContext: 8192, Backends: []string{"vulkan"}}
	if _, diagnostics, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm"); err == nil || len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Reason, "backend rocm") {
		t.Fatalf("backend restriction err=%v diagnostics=%#v", err, diagnostics)
	}
}

func TestRegistryRejectsDwarfStarWithoutOneGPU(t *testing.T) {
	managed := catalog.Catalog{LlamaPresets: map[string]catalog.LlamaPreset{}, DwarfStarPresets: map[string]catalog.DwarfStarPreset{
		"deepseek": {ID: "deepseek", Bundle: "bundle", DefaultContext: 4096, MaxOutputTokens: 1024},
	}}
	if _, diagnostics, err := BuildRegistry(managed, t.TempDir(), []string{"dwarfstar"}, "cpu", nil, "rocm"); err == nil || len(diagnostics) != 1 {
		t.Fatalf("err=%v diagnostics=%#v", err, diagnostics)
	}
}

func TestDiscoverRegistrySelectsOnlyApplicationsWithVerifiedModels(t *testing.T) {
	root := t.TempDir()
	llamaArtifact := catalog.Artifact{ID: "llama-model", Destination: "llama.gguf", Target: "llama-models", Size: 5, SHA256: "llama-fixture"}
	dwarfArtifact := catalog.Artifact{ID: "dwarf-model", Destination: "dwarf.gguf", Target: "dwarfstar-models", Size: 5, SHA256: "dwarf-fixture"}
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{llamaArtifact.ID: llamaArtifact, dwarfArtifact.ID: dwarfArtifact},
		Bundles: map[string]catalog.Bundle{
			"llama-bundle": {ID: "llama-bundle", Application: "llama-cpp", Artifacts: []string{llamaArtifact.ID}},
			"dwarf-bundle": {ID: "dwarf-bundle", Application: "dwarfstar", Artifacts: []string{dwarfArtifact.ID}},
		},
		LlamaPresets: map[string]catalog.LlamaPreset{
			"llama": {ID: "llama", Bundle: "llama-bundle", Artifact: llamaArtifact.ID, DefaultContext: 4096},
		},
		DwarfStarPresets: map[string]catalog.DwarfStarPreset{
			"dwarf": {ID: "dwarf", Bundle: "dwarf-bundle", DefaultContext: 4096, MaxOutputTokens: 1024},
		},
	}
	llamaPath := content.ArtifactPath(root, llamaArtifact)
	if err := os.MkdirAll(filepath.Dir(llamaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(llamaPath, []byte("llama"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(llamaPath, llamaArtifact.Size, llamaArtifact.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	registry, applications, diagnostics, err := DiscoverRegistry(
		managed, root, []string{"llama-cpp", "dwarfstar"}, "strix-halo", []string{"/dev/dri/renderD128"}, "rocm",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(applications) != 1 || applications[0] != "llama-cpp" {
		t.Fatalf("applications=%v", applications)
	}
	if len(registry.Models) != 1 || registry.Models[0].ID != "llama" {
		t.Fatalf("registry=%#v", registry)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("optional unavailable application diagnostics=%#v", diagnostics)
	}

	dwarfPath := content.ArtifactPath(root, dwarfArtifact)
	if err := os.MkdirAll(filepath.Dir(dwarfPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dwarfPath, []byte("dwarf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(dwarfPath, dwarfArtifact.Size, dwarfArtifact.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	registry, applications, diagnostics, err = DiscoverRegistry(
		managed, root, []string{"llama-cpp", "dwarfstar"}, "strix-halo", []string{"/dev/dri/renderD128"}, "rocm",
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(applications, ",") != "llama-cpp,dwarfstar" || len(registry.Models) != 2 || len(diagnostics) != 0 {
		t.Fatalf("complete discovery applications=%v registry=%#v diagnostics=%#v", applications, registry, diagnostics)
	}
}
