package content

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/verification"
)

func TestInspectArtifactUsesCompatibleReceipt(t *testing.T) {
	root := t.TempDir()
	artifact := catalog.Artifact{ID: "model", Target: "llama-models", Destination: "model.gguf", Size: 5, SHA256: "digest"}
	file := ArtifactPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(file, 5, "digest"); err != nil {
		t.Fatal(err)
	}
	status, err := InspectArtifact(store, root, artifact, false)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != Verified {
		t.Fatalf("state = %s", status.State)
	}
}

func TestVerifyArtifactHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	contents := []byte("model")
	artifact := testArtifact(contents)
	file := ArtifactPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := VerifyArtifact(ctx, nil, root, artifact, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
