package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateIsAppendOnlyAndReplaceRequiresRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "result.json")
	if err := JSON(path, map[string]int{"value": 1}, 0o640, Create); err != nil {
		t.Fatal(err)
	}
	if err := JSON(path, map[string]int{"value": 2}, 0o640, Create); err == nil {
		t.Fatal("second create unexpectedly replaced the result")
	}
	if err := JSON(path, map[string]int{"value": 2}, 0o640, ReplaceRegular); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), `"value": 2`) {
		t.Fatalf("contents=%q err=%v", contents, err)
	}
	if status, err := os.Stat(path); err != nil || status.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%v err=%v", status.Mode(), err)
	}

	directory := filepath.Join(t.TempDir(), "checkpoint")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(directory, []byte("bad"), 0o644, ReplaceRegular); err == nil {
		t.Fatal("replace unexpectedly accepted a directory")
	}
}

func TestReplaceRegularRefusesHardLink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "result.json")
	alias := filepath.Join(directory, "alias.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o600, ReplaceRegular); err == nil {
		t.Fatal("replaced a hard-linked file")
	}
}

func TestPublishCreateDoesNotReplaceConcurrentOutput(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "archive.tar")
	temporary := filepath.Join(directory, ".archive.partial")
	if err := os.WriteFile(temporary, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Publish(path, temporary, 0o600, Create); err == nil {
		t.Fatal("replaced a concurrent output")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "other" {
		t.Fatalf("destination changed: %q, %v", contents, err)
	}
}

func TestEnsureAcceptsSameBytesAndRefusesDifferentBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	if err := Ensure(path, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(path, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(path, []byte("different\n"), 0o644); err == nil {
		t.Fatal("immutable output was replaced")
	}
}
