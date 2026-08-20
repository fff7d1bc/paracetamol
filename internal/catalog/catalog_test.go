package catalog

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
