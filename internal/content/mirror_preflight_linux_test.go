package content

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"paracetamol/internal/catalog"
)

func TestInstallDryRunDoesNotIndexOrHashLocalMirror(t *testing.T) {
	base := t.TempDir()
	mirror, data := filepath.Join(base, "mirror"), filepath.Join(base, "data")
	if err := os.Mkdir(mirror, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := []byte("fixture")
	artifact := testArtifact(contents)
	if err := os.WriteFile(filepath.Join(mirror, "model.gguf"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(watch)
	// Directory reads expose indexing; child-file opens expose hashing. Root
	// metadata inspection is intentionally allowed by the preflight contract.
	if _, err := syscall.InotifyAddWatch(watch, mirror, syscall.IN_OPEN|syscall.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	runner := &mirrorPreflightRunner{}
	callbackCalls := 0
	if err := Install(InstallOptions{
		DataRoot: data, LocalMirror: mirror, DryRun: true, Artifacts: []catalog.Artifact{artifact}, Runner: runner,
		BeforeMutation: func() error { callbackCalls++; return nil },
	}); err != nil {
		t.Fatal(err)
	}
	var events [4096]byte
	if n, err := syscall.Read(watch, events[:]); n > 0 || !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("dry run opened/read mirror content: event bytes=%d, error=%v", n, err)
	}
	if runner.calls != 0 || callbackCalls != 0 {
		t.Fatalf("dry run crossed mutation boundary: process=%d callback=%d", runner.calls, callbackCalls)
	}
	if _, err := os.Lstat(data); !os.IsNotExist(err) {
		t.Fatalf("dry run created data root: %v", err)
	}
}
