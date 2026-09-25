package catalog

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/project"
)

func TestLoadRepositoryCatalog(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Agreements) != 7 || len(loaded.Artifacts) != 71 || len(loaded.Bundles) != 53 || len(loaded.Workflows) != 28 || len(loaded.Benchmarks) != 28 || len(loaded.SamplingPolicies) != 4 || len(loaded.LlamaPresets) != 19 || len(loaded.DwarfStarPresets) != 1 {
		t.Fatalf("unexpected catalog counts: agreements=%d artifacts=%d bundles=%d workflows=%d benchmarks=%d policies=%d llama_presets=%d dwarfstar_presets=%d", len(loaded.Agreements), len(loaded.Artifacts), len(loaded.Bundles), len(loaded.Workflows), len(loaded.Benchmarks), len(loaded.SamplingPolicies), len(loaded.LlamaPresets), len(loaded.DwarfStarPresets))
	}
	preset := loaded.LlamaPresets["unsloth-qwen3.8-27b-mtp-ud-q8-k-xl"]
	if preset.ReasoningControl != "effort" || preset.ReasoningDefault != "medium" || preset.SamplingPolicy != "qwen3.8-27b" {
		t.Fatalf("unexpected qwen preset: %#v", preset)
	}
	swift := loaded.LlamaPresets["ukisai-swift1.5-qwen3.8-27b-mtp-q8-0"]
	swiftArtifact := loaded.Artifacts[swift.Artifact]
	if swift.DefaultContext != 262144 || swift.SpeculativeType != "draft-mtp" || swift.DraftTokens != 3 || swift.ChatTemplate != "qwen3.8" || swift.SamplingPolicy != "qwen3.8-27b" || !swift.AgentTools || swift.ReasoningDefault != "medium" || !swift.ReasoningPreserve {
		t.Fatalf("unexpected Swift preset: %#v", swift)
	}
	if swiftArtifact.Source.Repository != "ukisai/Swift-1.5-Qwen3.8-27B-GGUF" || swiftArtifact.License.SPDX != "LicenseRef-Swift-Open-1.0" || len(swiftArtifact.Agreements) != 1 || swiftArtifact.Agreements[0] != "swift-open-license-1.0" {
		t.Fatalf("unexpected Swift provenance: %#v", swiftArtifact)
	}
	flashNextQ4 := loaded.LlamaPresets["unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"]
	if flashNextQ4.DefaultContext != 262144 || strings.Join(flashNextQ4.Backends, ",") != "rocm,vulkan" || flashNextQ4.ModelLoad["strix-halo"] != LlamaModelLoadStreamTokenEmbedding || flashNextQ4.SpeculativeType != "" || flashNextQ4.KVCache["strix-halo"] != "f16" {
		t.Fatalf("unexpected Qwen3.8 Flash-Next Q4_K_XL preset: %#v", flashNextQ4)
	}
	for id, preset := range loaded.LlamaPresets {
		if id != flashNextQ4.ID && (len(preset.BackendProfiles) != 0 || len(preset.ModelLoad) != 0) {
			t.Fatalf("unrelated preset %s acquired an exceptional runtime policy", id)
		}
		for profile, cacheType := range preset.KVCache {
			if cacheType != "f16" {
				t.Fatalf("llama.cpp preset %s quantizes the %s target K/V cache as %s", id, profile, cacheType)
			}
		}
	}
	dwarfstar := loaded.DwarfStarPresets["antirez-deepseek-v4-flash-0731-q2-imatrix"]
	if dwarfstar.Bundle == "" || dwarfstar.DefaultContext != 131072 || dwarfstar.ReasoningDefault != "high" {
		t.Fatalf("unexpected DwarfStar preset: %#v", dwarfstar)
	}
}

func TestPublicTextPresetIDsNameTheGGUFPublisher(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for id, preset := range loaded.LlamaPresets {
		artifact := loaded.Artifacts[preset.Artifact]
		publisher := strings.ToLower(strings.SplitN(artifact.Source.Repository, "/", 2)[0])
		if !strings.HasPrefix(id, publisher+"-") || !strings.HasPrefix(preset.Bundle, "llama-"+publisher+"-") {
			t.Errorf("preset %q and bundle %q should name GGUF publisher %q", id, preset.Bundle, publisher)
		}
	}
	for id, preset := range loaded.DwarfStarPresets {
		bundle := loaded.Bundles[preset.Bundle]
		artifact := loaded.Artifacts[bundle.Artifacts[0]]
		publisher := strings.ToLower(strings.SplitN(artifact.Source.Repository, "/", 2)[0])
		if !strings.HasPrefix(id, publisher+"-") || !strings.HasPrefix(preset.Bundle, "dwarfstar-"+publisher+"-") {
			t.Errorf("preset %q and bundle %q should name GGUF publisher %q", id, preset.Bundle, publisher)
		}
	}
}

func TestPublisherRenameKeepsExistingArtifactIdentity(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for presetID, want := range map[string]struct{ artifact, destination string }{
		"unsloth-qwen3.8-27b-mtp-ud-q8-k-xl":                              {"qwen3.8-27b-ud-q8-k-xl-gguf", "qwen3.8-27b/Qwen3.8-27B-UD-Q8_K_XL.gguf"},
		"meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k": {"muse-glimmer-30b-kquant-dynamic-gguf", "muse-glimmer-30b/muse-glimmer-30B-kquant-dynamic.gguf"},
	} {
		preset := loaded.LlamaPresets[presetID]
		if preset.Artifact != want.artifact || loaded.Artifacts[preset.Artifact].Destination != want.destination {
			t.Errorf("preset %q changed installed artifact identity: %#v", presetID, preset)
		}
	}
}

func TestLlamaPresetBackendProfileRestrictions(t *testing.T) {
	preset := LlamaPreset{Backends: []string{"rocm", "vulkan"}, BackendProfiles: map[string][]string{"rocm": {"strix-halo"}}}
	for _, profile := range []string{"auto", "cpu", "rdna4", "strix-point", "strix-halo"} {
		if preset.SupportsRuntime("rocm", profile) != (profile == "strix-halo") || !preset.SupportsRuntime("vulkan", profile) {
			t.Fatalf("wrong backend gate for %s", profile)
		}
	}
	for _, policy := range []string{
		`{"cuda":["strix-halo"]}`, `{"rocm":[]}`, `{"rocm":["auto"]}`,
		`{"rocm":["cpu"]}`, `{"rocm":["strix-halo","strix-halo"]}`,
	} {
		raw := `{"bundle":"fixture","artifact":"fixture","default_context":4096,"backend_profiles":` + policy + `}`
		if _, err := loadLlamaPreset("fixture", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid backend profile restriction %s", policy)
		}
	}
}

func TestLlamaPresetRejectsUnknownOrDuplicateBackends(t *testing.T) {
	for _, raw := range []string{
		`{"bundle":"fixture","artifact":"fixture","backends":["cuda"],"default_context":4096}`,
		`{"bundle":"fixture","artifact":"fixture","backends":["vulkan","vulkan"],"default_context":4096}`,
	} {
		if _, err := loadLlamaPreset("fixture", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid backend policy: %s", raw)
		}
	}
}

func TestLlamaPresetRejectsUnknownModelLoadPolicy(t *testing.T) {
	for _, raw := range []string{
		`{"bundle":"fixture","artifact":"fixture","default_context":4096,"model_load":{"rdna4":"mmap-lazy-token-embedding"}}`,
		`{"bundle":"fixture","artifact":"fixture","default_context":4096,"model_load":{"strix-halo":"lazy"}}`,
	} {
		if _, err := loadLlamaPreset("fixture", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid model-load policy: %s", raw)
		}
	}
}

func TestStreamingEmbeddingRequiresTheAcceptedRuntimeTuple(t *testing.T) {
	valid := `{"bundle":"fixture","artifact":"fixture","default_context":262144,"backends":["rocm","vulkan"],"backend_profiles":{"rocm":["strix-halo"]},"model_load":{"strix-halo":"stream-token-embedding"},"flash_attention":{"strix-halo":"on"},"kv_cache":{"strix-halo":"f16"}}`
	if _, err := loadLlamaPreset("fixture", json.RawMessage(valid)); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"other profile":        strings.ReplaceAll(valid, "strix-halo", "strix-point"),
		"quantized cache":      strings.Replace(valid, `"f16"`, `"q8_0"`, 1),
		"no flash attention":   strings.Replace(valid, `"on"`, `"off"`, 1),
		"unrestricted backend": strings.Replace(valid, `"backend_profiles":{"rocm":["strix-halo"]},`, "", 1),
		"speculative decoding": strings.Replace(valid, `"bundle":`, `"speculative_type":"draft-mtp","draft_tokens":3,"bundle":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadLlamaPreset("fixture", json.RawMessage(invalid)); err == nil {
				t.Fatal("accepted unsafe streaming policy")
			}
		})
	}
}

func TestEveryCuratedWorkflowResourceMatchesCatalog(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for identifier, workflow := range loaded.Workflows {
		t.Run(identifier, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join(root, "catalog", "workflows", identifier+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if digest := fmt.Sprintf("%x", sha256.Sum256(contents)); digest != workflow.RenderedSHA256 {
				t.Fatalf("rendered hash = %s, want %s", digest, workflow.RenderedSHA256)
			}
			var document any
			if err := json.Unmarshal(contents, &document); err != nil {
				t.Fatalf("invalid workflow JSON: %v", err)
			}
		})
	}
	entries, err := os.ReadDir(filepath.Join(root, "catalog", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(loaded.Workflows) {
		t.Fatalf("workflow resource count = %d, want %d", len(entries), len(loaded.Workflows))
	}
	for _, entry := range entries {
		identifier := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || loaded.Workflows[identifier].ID == "" {
			t.Fatalf("unexpected workflow resource: %s", entry.Name())
		}
	}
}

func TestSafeRelativeRejectsTraversal(t *testing.T) {
	for _, value := range []string{"", "/absolute", "../outside", "models/../outside", "."} {
		if _, err := safeRelative(value, "fixture"); err == nil {
			t.Fatalf("unsafe path %q accepted", value)
		}
	}
}

func TestBundleRequiresSelectorGroupsAndAllMembership(t *testing.T) {
	for _, groups := range []string{`[]`, `["llama"]`, `["all", "all"]`, `["all", "unknown"]`} {
		raw := json.RawMessage(`{"description":"fixture","application":"llama-cpp","artifacts":["artifact"],"groups":` + groups + `}`)
		if _, err := loadBundle("fixture", raw); err == nil {
			t.Fatalf("accepted invalid bundle groups %s", groups)
		}
	}
	raw := json.RawMessage(`{"description":"fixture","application":"llama-cpp","artifacts":["artifact"],"groups":["all","llama"]}`)
	if _, err := loadBundle("fixture", raw); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
}
