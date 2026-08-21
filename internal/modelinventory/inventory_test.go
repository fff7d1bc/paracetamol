package modelinventory

import (
	"os"
	"path/filepath"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/storage"
	"paracetamol/internal/verification"
)

func fixtureCatalog() catalog.Catalog {
	first := catalog.Artifact{ID: "known-first", Destination: "known/model-00001-of-00002.gguf", Target: "llama-models", Size: 1, SHA256: "0"}
	second := catalog.Artifact{ID: "known-second", Destination: "known/model-00002-of-00002.gguf", Target: "llama-models", Size: 2, SHA256: "0"}
	return catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{first.ID: first, second.ID: second},
		Bundles:   map[string]catalog.Bundle{"known-bundle": {ID: "known-bundle", Application: "llama-cpp", Artifacts: []string{first.ID, second.ID}}},
		LlamaPresets: map[string]catalog.LlamaPreset{
			"known-preset": {ID: "known-preset", Bundle: "known-bundle", Artifact: first.ID},
			"known-alias":  {ID: "known-alias", Bundle: "known-bundle", Artifact: first.ID},
		},
	}
}

func TestLlamaGroupsCatalogAliasesAndLooseSplitModels(t *testing.T) {
	dataRoot := t.TempDir()
	root := (storage.Layout{Root: dataRoot}).LlamaModels()
	if err := os.MkdirAll(filepath.Join(root, "known"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "known/model-00001-of-00002.gguf")
	second := filepath.Join(root, "known/model-00002-of-00002.gguf")
	if err := os.WriteFile(first, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("bc"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range fixtureCatalog().Artifacts {
		if err := store.Record(filepath.Join(root, artifact.Destination), artifact.Size, artifact.SHA256); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manual.gguf"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	shard := filepath.Join(external, "other-00001-of-00002.gguf")
	if err := os.WriteFile(shard, []byte("part"), 0o644); err != nil {
		t.Fatal(err)
	}
	models, err := Llama(fixtureCatalog(), dataRoot, []string{external})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 3 {
		t.Fatalf("models=%#v", models)
	}
	if models[0].State != "ready" || len(models[0].Presets) != 2 || models[0].Size != 3 {
		t.Fatalf("catalog model=%#v", models[0])
	}
	if models[2].State != "partial" || models[2].ExpectedShards != 2 {
		t.Fatalf("split model=%#v", models[2])
	}
}

func TestLlamaReportsCatalogSizeMismatchAndDoesNotCreateMissingRoot(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "missing")
	models, err := Llama(fixtureCatalog(), dataRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if models[0].State != "missing" || models[0].Size != 3 {
		t.Fatalf("missing=%#v", models[0])
	}
	if _, err := os.Stat(dataRoot); !os.IsNotExist(err) {
		t.Fatalf("inventory created data root: %v", err)
	}

	dataRoot = t.TempDir()
	root := (storage.Layout{Root: dataRoot}).LlamaModels()
	if err := os.MkdirAll(filepath.Join(root, "known"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "known/model-00001-of-00002.gguf"), []byte("wrong"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "known/model-00002-of-00002.gguf"), []byte("bc"), 0o644); err != nil {
		t.Fatal(err)
	}
	models, err = Llama(fixtureCatalog(), dataRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if models[0].State != "size-mismatch" {
		t.Fatalf("mismatch=%#v", models[0])
	}
}

func TestLlamaRejectsInvalidExplicitScan(t *testing.T) {
	notGGUF := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(notGGUF, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Llama(fixtureCatalog(), t.TempDir(), []string{notGGUF}); err == nil {
		t.Fatal("non-GGUF scan succeeded")
	}
}
