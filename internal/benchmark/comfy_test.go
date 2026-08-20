package benchmark

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"rocmplete/internal/catalog"
)

func TestEveryCatalogBenchmarkLoadsAndMatches(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for identifier, spec := range managed.Benchmarks {
		t.Run(identifier, func(t *testing.T) {
			if _, err := LoadPrompt(root, spec); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPreparePromptDoesNotMutateSource(t *testing.T) {
	source := map[string]any{"1": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"seed": json.Number("9"), "filename_prefix": "old"}}}
	prompt, input, err := PreparePrompt(source, 42, "new")
	if err != nil {
		t.Fatal(err)
	}
	if input {
		t.Fatal("unexpected input")
	}
	if source["1"].(map[string]any)["inputs"].(map[string]any)["filename_prefix"] != "old" {
		t.Fatal("source mutated")
	}
	inputs := prompt["1"].(map[string]any)["inputs"].(map[string]any)
	if inputs["filename_prefix"] != "new" || inputs["seed"] != int64(42) {
		t.Fatalf("prompt was not prepared: %#v", inputs)
	}
}
