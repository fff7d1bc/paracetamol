package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"paracetamol/internal/project"
)

func TestGufoReusesReviewedFlashNextArtifacts(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := Load(filepath.Join(root, "catalog/catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	id := "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"
	preset := managed.GufoPresets[id+"-gufo"]
	llama := managed.LlamaPresets[id]
	if preset.DefaultContext != 262144 || preset.CacheRAMBytes != 28<<30 || preset.Artifact != llama.Artifact || !reflect.DeepEqual(managed.Bundles[preset.Bundle].Artifacts, managed.Bundles[llama.Bundle].Artifacts) {
		t.Fatalf("Gufo policy or shared content drift: %+v", preset)
	}
	if len(managed.Bundles[preset.Bundle].Artifacts) != 4 {
		t.Fatal("expected four shards")
	}
}

func TestGufoPresetValidation(t *testing.T) {
	valid := map[string]any{"bundle": "bundle", "artifact": "model", "default_context": 262144, "max_output_tokens": 16384, "cache_ram_bytes": 30064771072, "agent_tools": true, "reasoning_control": "effort", "reasoning_levels": []string{"medium", "xhigh"}, "reasoning_default": "medium", "reasoning_off": true}
	for _, field := range []string{"", "default_context", "max_output_tokens", "cache_ram_bytes", "reasoning_default", "reasoning_levels", "reasoning_off", "unknown"} {
		t.Run(field, func(t *testing.T) {
			input := map[string]any{}
			for k, v := range valid {
				input[k] = v
			}
			switch field {
			case "":
			case "reasoning_default":
				input[field] = "xhigh"
			case "reasoning_levels":
				input[field] = []string{"high"}
			case "reasoning_off":
				input[field] = false
			default:
				input[field] = -1
			}
			raw, _ := json.Marshal(input)
			_, err := loadGufoPreset("fixture", raw)
			if (err == nil) != (field == "") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestGufoCatalogRequiresPresetCollection(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "catalog/catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, value := range []json.RawMessage{nil, json.RawMessage("null")} {
		if value == nil {
			delete(doc, "gufo_presets")
		} else {
			doc["gufo_presets"] = value
		}
		encoded, _ := json.Marshal(doc)
		path := filepath.Join(t.TempDir(), "catalog.json")
		if err := os.WriteFile(path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("missing/null preset collection accepted")
		}
	}
}
