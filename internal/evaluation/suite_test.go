package evaluation

import (
	"archive/tar"
	"bytes"
	"path/filepath"
	"testing"
)

func TestLoadFrozenSuite(t *testing.T) {
	suite, err := Load(filepath.Join("..", "..", "evaluations", "coding", "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.Tasks) != 11 || len(suite.Fingerprint) != 64 {
		t.Fatalf("unexpected suite: %#v", suite)
	}
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	var contents bytes.Buffer
	writer := tar.NewWriter(&contents)
	data := []byte("escape")
	if err := writer.WriteHeader(&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write(data)
	_ = writer.Close()
	if err := extractArchive(contents.Bytes(), filepath.Join(t.TempDir(), "fixture")); err == nil {
		t.Fatal("accepted traversal")
	}
}
