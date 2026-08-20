package verification

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "content", "llama-cpp", "models", "model.gguf")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(file, 5, "digest"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Matches(file, 5, "digest") {
		t.Fatal("saved receipt did not match")
	}
}
