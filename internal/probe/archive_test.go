package probe

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectZIPHashesWithoutExtraction(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "pack.zip")
	handle, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(handle)
	for name, contents := range map[string]string{"b/workflow.json": "workflow", "a/model.bin": "model"} {
		member, _ := archive.Create(name)
		_, _ = member.Write([]byte(contents))
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := InspectZIP(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Members) != 2 || result.Members[0].Name != "a/model.bin" || result.Members[0].SHA256 == nil {
		t.Fatalf("unexpected members: %#v", result.Members)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "model.bin")); !os.IsNotExist(err) {
		t.Fatal("probe extracted a member")
	}
}

func TestInspectZIPReportsUnsafeDuplicateAndSymlink(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pack.zip")
	handle, _ := os.Create(file)
	archive := zip.NewWriter(handle)
	for _, name := range []string{"../escape", "same", "same"} {
		member, _ := archive.Create(name)
		_, _ = member.Write([]byte("value"))
	}
	header := &zip.FileHeader{Name: "link"}
	header.SetMode(os.ModeSymlink | 0o777)
	member, _ := archive.CreateHeader(header)
	_, _ = member.Write([]byte("target"))
	_ = archive.Close()
	_ = handle.Close()
	result, err := InspectZIP(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DuplicateNames) != 1 || result.DuplicateNames[0] != "same" {
		t.Fatalf("duplicates=%q", result.DuplicateNames)
	}
	if _, err := InspectZIP(file, []string{"same"}); err == nil {
		t.Fatal("ambiguous selection succeeded")
	}
	states := map[string]ArchiveMember{}
	for _, item := range result.Members {
		states[item.Name] = item
	}
	if states["../escape"].SafePath || states["link"].Type != "symlink" || states["link"].SHA256 != nil {
		t.Fatalf("unsafe states: %#v", states)
	}
}
