package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/verification"
)

func TestManagedProjectorIsRequiredBeforeDirectLaunch(t *testing.T) {
	root := t.TempDir()
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{
			"target":    {ID: "target", Target: "llama-models", Destination: "target.gguf", Size: 1, SHA256: "0"},
			"projector": {ID: "projector", Target: "llama-models", Destination: "projector.gguf", Size: 1, SHA256: "1"},
		},
		Bundles:      map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"target", "projector"}}},
		LlamaPresets: map[string]catalog.LlamaPreset{"vision": {ID: "vision", Bundle: "bundle", Artifact: "target", MMProjArtifact: "projector", DefaultContext: 4096}},
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	app, stdout, _ := testApp(t, &commandRunner{})
	app.catalog = &managed
	args := []string{"--preset", "vision", "--profile", "cpu", "--data-dir", root, "--dry-run"}
	for _, id := range []string{"target", "projector"} {
		artifact := managed.Artifacts[id]
		path := content.ArtifactPath(root, artifact)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := store.Record(path, artifact.Size, artifact.SHA256); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(); err != nil {
			t.Fatal(err)
		}
		if id == "target" {
			if err := app.runLlama("server", args); err == nil || !strings.Contains(err.Error(), "is not installed") {
				t.Fatalf("missing required projector accepted: %v", err)
			}
		}
	}
	if err := app.runLlama("server", args); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "PARACETAMOL_LLAMA_MMPROJ=/content/models/projector.gguf") {
		t.Fatalf("direct launch lost projector: %s", stdout)
	}
}
