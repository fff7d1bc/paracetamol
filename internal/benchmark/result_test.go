package benchmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSONDoesNotReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := WriteJSON(path, map[string]any{"answer": 42}); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(path, map[string]any{"answer": 7}); err == nil {
		t.Fatal("second write replaced append-only result")
	}
	contents, _ := os.ReadFile(path)
	if string(contents) != "{\n  \"answer\": 42\n}\n" {
		t.Fatalf("unexpected result: %s", contents)
	}
}
