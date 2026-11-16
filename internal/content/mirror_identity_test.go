package content

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/catalog"
)

func verifiedMirrorCandidate(t *testing.T, root, source string, payload []byte) mirrorCandidate {
	t.Helper()
	index, err := openMirror(root, filepath.Join(t.TempDir(), "data"), false)
	if err != nil {
		t.Fatal(err)
	}
	artifact := testArtifact(payload)
	artifact.Source.Path = filepath.Base(source)
	candidate, err := index.find(context.Background(), artifact, io.Discard, nil)
	if err != nil || candidate.path != source {
		t.Fatalf("find = %+v, %v", candidate, err)
	}
	return candidate
}

func replaceMirrorSource(t *testing.T, source string, contents []byte) {
	t.Helper()
	other := source + ".replacement"
	if err := os.WriteFile(other, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, source); err != nil {
		t.Fatal(err)
	}
}

func TestMirrorRefusesChangedPreflightCandidate(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, change := range []string{"replacement", "same-size-edit", "resize"} {
			t.Run(change+map[bool]string{true: "/move", false: "/copy"}[move], func(t *testing.T) {
				root := t.TempDir()
				source, destination := filepath.Join(root, "model.gguf"), filepath.Join(t.TempDir(), "model.gguf")
				payload := []byte("original model")
				if err := os.WriteFile(source, payload, 0o600); err != nil {
					t.Fatal(err)
				}
				candidate := verifiedMirrorCandidate(t, root, source, payload)
				replacement := []byte("unrelated user")
				if change == "resize" {
					replacement = []byte("unrelated user file of another size")
				}
				if change == "replacement" {
					replaceMirrorSource(t, source, replacement)
				} else if err := os.WriteFile(source, replacement, 0o600); err != nil {
					t.Fatal(err)
				}
				err := (localMirror{root: root, move: move}).materialize(candidate, destination)
				if err == nil || !strings.Contains(err.Error(), "changed since verification") {
					t.Fatalf("materialize = %v", err)
				}
				if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, replacement) {
					t.Fatalf("changed source was removed/modified: %q, %v", got, err)
				}
				if _, err := os.Lstat(destination); !os.IsNotExist(err) {
					t.Fatalf("changed source was staged: %v", err)
				}
			})
		}
	}
}

func TestMirrorMoveSameFilesystemRetainsVerifiedIdentity(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "model.gguf"), filepath.Join(t.TempDir(), "model.gguf")
	payload := []byte("model")
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	candidate := verifiedMirrorCandidate(t, root, source, payload)
	if err := (localMirror{root: root, move: true}).materialize(candidate, destination); err != nil {
		t.Fatal(err)
	}
	staged, err := os.Stat(destination)
	if err != nil || !os.SameFile(original, staged) {
		t.Fatalf("move did not retain original file: %v", err)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatalf("source not moved: %v", err)
	}
}

func TestMirrorCrossDeviceMoveRevalidatesBeforeSourceRemoval(t *testing.T) {
	for _, change := range []string{"none", "copy", "publish", "publish-error", "interrupted", "bad-copy"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			source, destination := filepath.Join(root, "model.gguf"), filepath.Join(t.TempDir(), "model.gguf")
			payload, replacement := []byte("model"), []byte("other")
			if err := os.WriteFile(source, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			candidate := verifiedMirrorCandidate(t, root, source, payload)
			mirror := localMirror{root: root, move: true, rename: func(string, string) error { return syscall.EXDEV }}
			if change == "bad-copy" {
				mirror.copy = func(w io.Writer, _ io.Reader) (int64, error) {
					n, err := w.Write(replacement)
					return int64(n), err
				}
			}
			if change == "copy" || change == "interrupted" {
				mirror.copy = func(w io.Writer, r io.Reader) (int64, error) {
					if change == "interrupted" {
						n, err := io.CopyN(w, r, 2)
						if err != nil {
							return n, err
						}
						return n, context.Canceled
					}
					n, err := io.Copy(w, r)
					replaceMirrorSource(t, source, replacement)
					return n, err
				}
			}
			if change == "publish" {
				mirror.publish = func(path, temporary string, mode os.FileMode, policy atomicfile.Policy) error {
					if err := atomicfile.Publish(path, temporary, mode, policy); err != nil {
						return err
					}
					replaceMirrorSource(t, source, replacement)
					return nil
				}
			}
			if change == "publish-error" {
				mirror.publish = func(string, string, os.FileMode, atomicfile.Policy) error {
					return errors.New("fixture publication failed")
				}
			}
			err := mirror.materialize(candidate, destination)
			if (err == nil) != (change == "none") {
				t.Fatalf("materialize = %v", err)
			}
			if change == "none" {
				if _, err := os.Lstat(source); !os.IsNotExist(err) {
					t.Fatalf("source not removed: %v", err)
				}
			} else {
				want := replacement
				if change == "interrupted" || change == "bad-copy" || change == "publish-error" {
					want = payload
					if change == "interrupted" && !errors.Is(err, context.Canceled) {
						t.Fatalf("lost cancellation: %v", err)
					}
				}
				if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("changed source removed/modified: %q, %v", got, err)
				}
			}
			if change == "none" || change == "publish" {
				if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, payload) {
					t.Fatalf("verified staging lost: %q, %v", got, err)
				}
			} else {
				if _, err := os.Lstat(destination); !os.IsNotExist(err) {
					t.Fatalf("incomplete payload published: %v", err)
				}
				partials, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".mirror-*.partial"))
				if err != nil || len(partials) != 1 {
					t.Fatalf("resumable scratch lost: %v, %v", partials, err)
				}
			}
		})
	}
}

func TestMirrorMoveDoesNotFallbackAfterOtherRenameErrors(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "model.gguf"), filepath.Join(t.TempDir(), "model.gguf")
	payload := []byte("model")
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	candidate := verifiedMirrorCandidate(t, root, source, payload)
	want := errors.New("rename denied")
	mirror := localMirror{root: root, move: true, rename: func(string, string) error { return want }}
	if err := mirror.materialize(candidate, destination); !errors.Is(err, want) {
		t.Fatalf("materialize = %v", err)
	}
	if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("source changed after failed rename: %q, %v", got, err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("fallback ran after failed rename: %v", err)
	}
}

type replacingMirrorWriter struct {
	t       *testing.T
	source  string
	changed bool
}

func (writer *replacingMirrorWriter) Write(p []byte) (int, error) {
	if !writer.changed && strings.Contains(string(p), "Moving local mirror file") {
		writer.changed = true
		replaceMirrorSource(writer.t, writer.source, []byte("unrelated user file"))
	}
	return len(p), nil
}

func TestInstallRefusesMirrorReplacementAfterPreflight(t *testing.T) {
	root, mirror := filepath.Join(t.TempDir(), "data"), t.TempDir()
	source := filepath.Join(mirror, "model.gguf")
	payload := []byte("model")
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := testArtifact(payload)
	writer := &replacingMirrorWriter{t: t, source: source}
	err := Install(InstallOptions{DataRoot: root, Artifacts: []catalog.Artifact{artifact}, LocalMirror: mirror, MoveFromMirror: true, Runner: noSELinuxRunner{}, Output: writer})
	if err == nil || !writer.changed || !strings.Contains(err.Error(), "changed since verification") {
		t.Fatalf("Install = %v; changed=%v", err, writer.changed)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != "unrelated user file" {
		t.Fatalf("replacement lost: %q, %v", got, err)
	}
	if _, err := os.Lstat(payloadPath(root, artifact)); !os.IsNotExist(err) {
		t.Fatalf("replacement staged: %v", err)
	}
}
