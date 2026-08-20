package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateManagedParentRejectsSymlinkedComponent(t *testing.T) {
	data := t.TempDir()
	managed := filepath.Join(data, "staging")
	outside := t.TempDir()
	if err := os.Mkdir(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(managed, "redirect")); err != nil {
		t.Fatal(err)
	}
	err := ValidateManagedParent(filepath.Join(managed, "redirect", "model.gguf"), managed, data, "staging")
	if err == nil || !strings.Contains(err.Error(), "symlinked") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateManagedParentAcceptsMissingTail(t *testing.T) {
	data := t.TempDir()
	managed := filepath.Join(data, "staging")
	if err := os.Mkdir(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateManagedParent(filepath.Join(managed, "new", "model.gguf"), managed, data, "staging"); err != nil {
		t.Fatal(err)
	}
}
