package modelinventory

import (
	"os"
	"path/filepath"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/verification"
)

func TestRequiredProjectorCountsTowardReadinessAndSizeButNotShards(t *testing.T) {
	managed := fixtureCatalog()
	projector := catalog.Artifact{ID: "projector", Target: "llama-models", Destination: "vision/projector.gguf", Size: 4, SHA256: "0"}
	managed.Artifacts[projector.ID] = projector
	bundle := managed.Bundles["known-bundle"]
	bundle.Artifacts = append(bundle.Artifacts, projector.ID)
	managed.Bundles["known-bundle"] = bundle
	for id, preset := range managed.LlamaPresets {
		preset.MMProjArtifact = projector.ID
		managed.LlamaPresets[id] = preset
	}
	root := t.TempDir()
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for id, artifact := range managed.Artifacts {
		if id == projector.ID {
			continue
		}
		path := content.ArtifactPath(root, artifact)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, artifact.Size), 0600); err != nil {
			t.Fatal(err)
		}
		if err := store.Record(path, artifact.Size, artifact.SHA256); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	models, err := Llama(managed, root, nil)
	if err != nil || len(models) != 1 || models[0].State == "ready" || models[0].ShardCount != 2 || models[0].ExpectedShards != 2 {
		t.Fatalf("missing projector inventory=%+v err=%v", models, err)
	}
	path := content.ArtifactPath(root, projector)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("proj"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(path, projector.Size, projector.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	models, err = Llama(managed, root, nil)
	if err != nil || len(models) != 1 || models[0].State != "ready" || models[0].Size != 7 || models[0].ShardCount != 2 || len(models[0].Artifacts) != 3 {
		t.Fatalf("verified projector inventory=%+v err=%v", models, err)
	}
}
