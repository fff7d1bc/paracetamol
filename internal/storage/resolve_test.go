package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDataRootPreservesAliasesWithoutCreation(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", filepath.Join("missing", "data")} {
		got, err := ResolveDataRoot(filepath.Join(alias, suffix))
		want := filepath.Join(real, suffix)
		if err != nil || got != want {
			t.Fatalf("ResolveDataRoot(%q) = %q, %v; want %q", suffix, got, err, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(real, "missing")); !os.IsNotExist(err) {
		t.Fatalf("resolution created missing suffix: %v", err)
	}
}

func TestResolveDataRootRejectsInvalidAncestors(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(parent, "broken")
	if err := os.Symlink(filepath.Join(parent, "absent"), broken); err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(parent, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, filepath.Join(file, "data"), broken, filepath.Join(broken, "data"), loop} {
		if resolved, err := ResolveDataRoot(path); err == nil {
			t.Fatalf("invalid data root %s resolved to %s", path, resolved)
		}
	}
}
