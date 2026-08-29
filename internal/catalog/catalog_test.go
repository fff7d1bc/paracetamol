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
	if len(loaded.Agreements) != 6 || len(loaded.Artifacts) != 69 || len(loaded.Bundles) != 52 || len(loaded.Workflows) != 28 || len(loaded.Benchmarks) != 28 || len(loaded.SamplingPolicies) != 4 || len(loaded.LlamaPresets) != 18 || len(loaded.DwarfStarPresets) != 1 {
		t.Fatalf("unexpected catalog counts: agreements=%d artifacts=%d bundles=%d workflows=%d benchmarks=%d policies=%d llama_presets=%d dwarfstar_presets=%d", len(loaded.Agreements), len(loaded.Artifacts), len(loaded.Bundles), len(loaded.Workflows), len(loaded.Benchmarks), len(loaded.SamplingPolicies), len(loaded.LlamaPresets), len(loaded.DwarfStarPresets))
	}
	preset := loaded.LlamaPresets["qwen3.8-27b-mtp-ud-q8-k-xl"]
	if preset.ReasoningControl != "effort" || preset.ReasoningDefault != "medium" || preset.SamplingPolicy != "qwen3.8-27b" {
		t.Fatalf("unexpected qwen preset: %#v", preset)
	}
	flashNext := loaded.LlamaPresets["qwen3.8-flash-next-125b-a6b-ud-iq4-xs"]
	if flashNext.DefaultContext != 262144 || strings.Join(flashNext.Backends, ",") != "vulkan" || flashNext.ModelLoad["strix-halo"] != LlamaModelLoadMMapLazyTokenEmbedding || flashNext.SpeculativeType != "" {
		t.Fatalf("unexpected Qwen3.8 Flash-Next preset: %#v", flashNext)
	}
	for id, preset := range loaded.LlamaPresets {
		for profile, cacheType := range preset.KVCache {
			if cacheType != "f16" {
				t.Fatalf("llama.cpp preset %s quantizes the %s target K/V cache as %s", id, profile, cacheType)
			}
		}
	}
	dwarfstar := loaded.DwarfStarPresets["deepseek-v4-flash-0731-q2-imatrix"]
	if dwarfstar.Bundle == "" || dwarfstar.DefaultContext != 131072 || dwarfstar.ReasoningDefault != "high" {
		t.Fatalf("unexpected DwarfStar preset: %#v", dwarfstar)
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
