package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "catalog"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "Containerfile"), filepath.Join(root, "catalog", "catalog.json")} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := validate(root)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	want, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}

func TestFindWalksToProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "Containerfile"), filepath.Join(root, "catalog", "catalog.json")} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(root, "build", "linux-amd64", "bin")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := find(nested)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got != root {
		t.Fatalf("root = %q, want %q", got, root)
	}
}
