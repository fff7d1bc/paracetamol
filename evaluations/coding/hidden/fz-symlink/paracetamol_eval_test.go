package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParacetamolEvalLazyMtimeMatchesFollowMode(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "target.jpg")
	if err := os.WriteFile(targetPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	targetTime := time.Unix(1, 0)
	if err := os.Chtimes(targetPath, targetTime, targetTime); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, "link.jpg")
	if err := os.Symlink("target.jpg", linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	targetInfo, err := os.Stat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if modTimeNS(targetInfo.ModTime()) == modTimeNS(linkInfo.ModTime()) {
		t.Skip("filesystem does not expose distinct symlink and target mtimes")
	}

	entry := Entry{Path: "link.jpg", Type: TypeFile}
	followModel := newPickerModel(SortPath)
	followModel.root = root
	followModel.followLinks = true
	followModel.mtimeCache = make(map[string]int64)
	if got, want := followModel.cachedModTimeNS(entry), modTimeNS(targetInfo.ModTime()); got != want {
		t.Fatalf("follow-mode mtime = %d, want target mtime %d", got, want)
	}

	defaultModel := newPickerModel(SortPath)
	defaultModel.root = root
	defaultModel.mtimeCache = make(map[string]int64)
	if got, want := defaultModel.cachedModTimeNS(entry), modTimeNS(linkInfo.ModTime()); got != want {
		t.Fatalf("default mtime = %d, want link mtime %d", got, want)
	}
}
