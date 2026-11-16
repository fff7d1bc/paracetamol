package atomicfile

import (
	"fmt"
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

func TestValidateDestinationChecksPolicyWithoutMutation(t *testing.T) {
	for _, shape := range []string{"missing", "regular", "directory", "symlink", "hardlink"} {
		for _, policy := range []Policy{Create, ReplaceRegular, Policy(99)} {
			t.Run(fmt.Sprintf("%s/policy=%d", shape, policy), func(t *testing.T) {
				base := t.TempDir()
				path := filepath.Join(base, "nested", "output")
				original := filepath.Join(base, "original")
				if err := os.WriteFile(original, []byte("original"), 0o600); err != nil {
					t.Fatal(err)
				}
				if shape != "missing" {
					if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					var err error
					switch shape {
					case "regular":
						err = os.WriteFile(path, []byte("original"), 0o600)
					case "directory":
						err = os.Mkdir(path, 0o700)
					case "symlink":
						err = os.Symlink(original, path)
					case "hardlink":
						err = os.Link(original, path)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				err := ValidateDestination(path, policy)
				wantValid := (shape == "missing" && (policy == Create || policy == ReplaceRegular)) || (shape == "regular" && policy == ReplaceRegular)
				if (err == nil) != wantValid {
					t.Fatalf("valid=%t err=%v", wantValid, err)
				}
				if got, err := os.ReadFile(original); err != nil || string(got) != "original" {
					t.Fatalf("validation changed original: %q %v", got, err)
				}
				if shape == "missing" {
					if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
						t.Fatalf("validation created parent: %v", err)
					}
				}
			})
		}
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
