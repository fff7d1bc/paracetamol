package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParacetamolEvalSourceSnapshotDetectsModification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotSource(info)
	if err := verifySourceSnapshot(path, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySourceSnapshot(path, snapshot); err == nil {
		t.Fatal("modified source should not match snapshot")
	}
}

func TestParacetamolEvalQuarantineRejectsReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.mkv")
	originalCopy := filepath.Join(dir, "original.mkv")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotSource(info)
	if err := os.Rename(path, originalCopy); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	if quarantine, err := quarantineSource(path, snapshot); err == nil || quarantine != "" {
		t.Fatalf("quarantine = %q, error = %v; want restored mismatch", quarantine, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "replacement" {
		t.Fatalf("replacement content = %q", got)
	}
}

func TestParacetamolEvalQuarantineCanRestoreMatchingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	quarantine, err := quarantineSource(path, snapshotSource(info))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source still present after quarantine: %v", err)
	}
	if err := restoreQuarantinedSource(quarantine, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("restored content = %q", got)
	}
}
