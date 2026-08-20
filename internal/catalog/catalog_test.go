package catalog

import (
	"path/filepath"
	"testing"

	"rocmplete/internal/project"
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
	if len(loaded.Agreements) != 6 || len(loaded.Artifacts) != 66 || len(loaded.Bundles) != 51 || len(loaded.Workflows) != 28 || len(loaded.Benchmarks) != 28 || len(loaded.SamplingPolicies) != 3 || len(loaded.LlamaPresets) != 17 {
		t.Fatalf("unexpected catalog counts: agreements=%d artifacts=%d bundles=%d workflows=%d benchmarks=%d policies=%d presets=%d", len(loaded.Agreements), len(loaded.Artifacts), len(loaded.Bundles), len(loaded.Workflows), len(loaded.Benchmarks), len(loaded.SamplingPolicies), len(loaded.LlamaPresets))
	}
	preset := loaded.LlamaPresets["qwen3.8-27b-mtp-ud-q8-k-xl"]
	if preset.ReasoningControl != "effort" || preset.ReasoningDefault != "medium" || preset.SamplingPolicy != "qwen3.8-27b" {
		t.Fatalf("unexpected qwen preset: %#v", preset)
	}
}

func TestSafeRelativeRejectsTraversal(t *testing.T) {
	for _, value := range []string{"", "/absolute", "../outside", "models/../outside", "."} {
		if _, err := safeRelative(value, "fixture"); err == nil {
			t.Fatalf("unsafe path %q accepted", value)
		}
	}
}
