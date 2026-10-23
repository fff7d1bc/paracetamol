package gateway

import (
	"os"
	"path/filepath"
	"reflect"
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
		Artifacts:    map[string]catalog.Artifact{"model": artifact},
		Bundles:      map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
		LlamaPresets: map[string]catalog.LlamaPreset{"fixture": {ID: "fixture", Bundle: "bundle", Artifact: "model", DefaultContext: 4096}},
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
	managed.SamplingPolicies = map[string]catalog.SamplingPolicy{"fixture-policy": {ID: "fixture-policy", Thinking: map[string]any{"temperature": 1.0}, NonThinking: map[string]any{"temperature": 0.7}}}
	withSampling := managed.LlamaPresets["fixture"]
	withSampling.SamplingPolicy = "fixture-policy"
	managed.LlamaPresets["fixture"] = withSampling
	frozen, _, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm")
	if err != nil {
		t.Fatal(err)
	}
	managed.SamplingPolicies["fixture-policy"].Thinking["temperature"] = 0.5
	changedSampling, _, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm")
	if err != nil || changedSampling.Fingerprint == frozen.Fingerprint {
		t.Fatalf("sampling values did not change fingerprint: %v", err)
	}
	model, _ := frozen.Lookup("fixture")
	if model.samplingDefaults.Thinking["temperature"] != 1.0 {
		t.Fatal("catalog mutation changed frozen gateway policy")
	}
	model.samplingDefaults.Thinking["temperature"] = 99
	lookup, _ := frozen.Lookup("fixture")
	if lookup.samplingDefaults.Thinking["temperature"] != 1.0 {
		t.Fatal("lookup can mutate frozen gateway policy")
	}
	managed.LlamaPresets["fixture"] = catalog.LlamaPreset{ID: "fixture", Bundle: "bundle", Artifact: "model", DefaultContext: 8192, Backends: []string{"vulkan"}}
	if _, diagnostics, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm"); err == nil || len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Reason, "backend rocm") {
		t.Fatalf("backend restriction err=%v diagnostics=%#v", err, diagnostics)
	}
	preset := managed.LlamaPresets["fixture"]
	preset.Backends = []string{"rocm", "vulkan"}
	preset.BackendProfiles = map[string][]string{"rocm": {"strix-halo"}}
	managed.LlamaPresets["fixture"] = preset
	for _, profile := range []string{"auto", "cpu", "rdna4", "strix-point", "strix-halo"} {
		_, diagnostics, err := BuildRegistry(managed, root, []string{"llama-cpp"}, profile, nil, "rocm")
		if (err == nil) != (profile == "strix-halo") {
			t.Fatalf("profile gate %s: err=%v diagnostics=%v", profile, err, diagnostics)
		}
		if _, _, err := BuildRegistry(managed, root, []string{"llama-cpp"}, profile, nil, "vulkan"); err != nil {
			t.Fatalf("lost Vulkan fallback on %s: %v", profile, err)
		}
	}
}

func TestRegistryRejectsRetiredApplication(t *testing.T) {
	if _, _, err := BuildRegistry(catalog.Catalog{}, t.TempDir(), []string{"dwarfstar"}, "cpu", nil, "rocm"); err == nil || !strings.Contains(err.Error(), "does not support application") {
		t.Fatalf("retired application error=%v", err)
	}
}

func TestRegistryFlashNextDFlash2RequiresVerifiedSharedDraftAndAcceptedRuntime(t *testing.T) {
	managed, err := catalog.Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseIDs := []string{
		"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl",
		"unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl",
	}
	presets := make(map[string]catalog.LlamaPreset)
	for _, id := range baseIDs {
		for _, suffix := range []string{"", "-dflash2"} {
			preset, ok := managed.LlamaPresets[id+suffix]
			if !ok {
				t.Fatalf("missing catalog fixture %s%s", id, suffix)
			}
			presets[id+suffix] = preset
		}
	}
	managed.LlamaPresets = presets
	root := t.TempDir()
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the real bundles and compatibility policies, but use tiny artifact
	// fixtures: this checks receipt eligibility, not model bytes or inference.
	writeFixture := func(id string, verified bool) {
		t.Helper()
		artifact := managed.Artifacts[id]
		artifact.Size, artifact.SHA256 = 4, "fixture-"+id
		managed.Artifacts[id] = artifact
		path := content.ArtifactPath(root, artifact)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
		if verified {
			if err := store.Record(path, artifact.Size, artifact.SHA256); err != nil {
				t.Fatal(err)
			}
			if err := store.Save(); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range baseIDs {
		for _, artifactID := range managed.Bundles[presets[id].Bundle].Artifacts {
			writeFixture(artifactID, true)
		}
	}
	const draftID = "incoai-qwen3.8-flash-next-dflash2-q8-0-gguf"
	for _, state := range []string{"missing", "unverified", "verified"} {
		if state != "missing" {
			writeFixture(draftID, state == "verified")
		}
		t.Run("draft-"+state, func(t *testing.T) {
			registry, diagnostics, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "strix-halo", nil, "rocm")
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range baseIDs {
				if _, ok := registry.Lookup(id); !ok {
					t.Fatalf("optional draft state changed base availability: %s", id)
				}
				if _, ok := registry.Lookup(id + "-dflash2"); ok != (state == "verified") {
					t.Fatalf("DFlash2 inventory ignored %s shared draft", state)
				}
			}
			if state == "verified" {
				if len(registry.Models) != 4 || len(diagnostics) != 0 {
					t.Fatalf("verified pair inventory=%v diagnostics=%v", registry.IDs(""), diagnostics)
				}
			} else {
				if !reflect.DeepEqual(registry.IDs(""), baseIDs) || len(diagnostics) != 2 {
					t.Fatalf("incomplete pair inventory=%v diagnostics=%v", registry.IDs(""), diagnostics)
				}
				for _, diagnostic := range diagnostics {
					if !strings.HasSuffix(diagnostic.Model, "-dflash2") || !strings.Contains(diagnostic.Reason, draftID+"=") {
						t.Fatalf("missing draft eligibility reason: %#v", diagnostic)
					}
				}
			}
		})
	}
	for _, backend := range []string{"rocm", "vulkan"} {
		for _, profile := range []string{"auto", "cpu", "rdna4", "strix-point", "strix-halo"} {
			t.Run(backend+"/"+profile, func(t *testing.T) {
				registry, diagnostics, err := BuildRegistry(managed, root, []string{"llama-cpp"}, profile, nil, backend)
				accepted := backend == "rocm" && profile == "strix-halo"
				if accepted {
					if err != nil || len(registry.Models) != 4 || len(diagnostics) != 0 {
						t.Fatalf("accepted inventory=%v diagnostics=%v err=%v", registry.IDs(""), diagnostics, err)
					}
				} else if backend == "vulkan" {
					if err != nil || !reflect.DeepEqual(registry.IDs(""), baseIDs) || len(diagnostics) != 2 {
						t.Fatalf("optional preset displaced Vulkan controls: inventory=%v diagnostics=%v err=%v", registry.IDs(""), diagnostics, err)
					}
				} else if err == nil || len(diagnostics) != 4 {
					t.Fatalf("unaccepted ROCm profile became schedulable: inventory=%v diagnostics=%v err=%v", registry.IDs(""), diagnostics, err)
				}
			})
		}
	}
}

func TestDiscoverRegistryRequiresVerifiedModels(t *testing.T) {
	root := t.TempDir()
	artifact := catalog.Artifact{ID: "model", Destination: "model.gguf", Target: "llama-models", Size: 5, SHA256: "fixture"}
	managed := catalog.Catalog{
		Artifacts:    map[string]catalog.Artifact{artifact.ID: artifact},
		Bundles:      map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{artifact.ID}}},
		LlamaPresets: map[string]catalog.LlamaPreset{"llama": {ID: "llama", Bundle: "bundle", Artifact: artifact.ID, DefaultContext: 4096}},
	}
	if _, _, _, err := DiscoverRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm"); err == nil {
		t.Fatal("missing content was discovered")
	}
	path := content.ArtifactPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("llama"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := DiscoverRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm"); err == nil {
		t.Fatal("unverified content was discovered")
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(path, artifact.Size, artifact.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	registry, applications, diagnostics, err := DiscoverRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm")
	if err != nil || len(applications) != 1 || applications[0] != "llama-cpp" || len(registry.Models) != 1 || registry.Models[0].ID != "llama" || len(diagnostics) != 0 {
		t.Fatalf("discovery applications=%v registry=%#v diagnostics=%#v err=%v", applications, registry, diagnostics, err)
	}
}
