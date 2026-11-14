package content

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/process"
)

type mirrorPreflightRunner struct{ calls int }

func (runner *mirrorPreflightRunner) LookPath(string) (string, error) {
	runner.calls++
	return "", fmt.Errorf("unexpected executable lookup")
}

func (runner *mirrorPreflightRunner) Run(context.Context, process.Command) (process.Result, error) {
	runner.calls++
	return process.Result{}, fmt.Errorf("unexpected subprocess")
}

func TestInstallRejectsInvalidMirrorBeforeMutation(t *testing.T) {
	for _, name := range []string{"missing", "regular-file", "dangling", "move-without-mirror", "equal", "data-inside-mirror", "mirror-inside-data", "data-alias", "missing-data-under-alias", "mirror-alias", "dangling-data-ancestor"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(name+"/dry="+strconv.FormatBool(dryRun), func(t *testing.T) {
				base := t.TempDir()
				data, mirror := filepath.Join(base, "data"), filepath.Join(base, "mirror")
				if err := os.Mkdir(mirror, 0o700); err != nil {
					t.Fatal(err)
				}
				move, want := false, "must not overlap"
				switch name {
				case "missing":
					mirror, want = filepath.Join(base, "absent"), "cannot access local mirror"
				case "regular-file":
					mirror, want = filepath.Join(base, "file"), "not a directory"
					if err := os.WriteFile(mirror, nil, 0o600); err != nil {
						t.Fatal(err)
					}
				case "dangling":
					mirror, want = filepath.Join(base, "alias"), "cannot access local mirror"
					if err := os.Symlink(filepath.Join(base, "absent"), mirror); err != nil {
						t.Fatal(err)
					}
				case "move-without-mirror":
					mirror, move, want = "", true, "requires --local-mirror"
				case "equal":
					data = mirror
				case "data-inside-mirror":
					data = filepath.Join(mirror, "new", "data")
				case "mirror-inside-data":
					data = base
				case "data-alias", "missing-data-under-alias":
					data = filepath.Join(base, "alias")
					if err := os.Symlink(mirror, data); err != nil {
						t.Fatal(err)
					}
					if name == "missing-data-under-alias" {
						data = filepath.Join(data, "new", "data")
					}
				case "mirror-alias":
					data, mirror = mirror, filepath.Join(base, "alias")
					if err := os.Symlink(data, mirror); err != nil {
						t.Fatal(err)
					}
				case "dangling-data-ancestor":
					alias := filepath.Join(base, "alias")
					if err := os.Symlink(filepath.Join(base, "absent"), alias); err != nil {
						t.Fatal(err)
					}
					data, want = filepath.Join(alias, "new", "data"), ""
				}
				// Existing roots must remain empty; absent roots must stay absent.
				_, before := os.Lstat(data)
				runner := &mirrorPreflightRunner{}
				callbackCalls := 0
				runtime := filepath.Join(base, "runtime")
				err := Install(InstallOptions{
					DataRoot: data, LocalMirror: mirror, MoveFromMirror: move, DryRun: dryRun,
					Artifacts: []catalog.Artifact{testArtifact([]byte("fixture"))}, Runner: runner,
					Environment:    map[string]string{"XDG_RUNTIME_DIR": runtime},
					BeforeMutation: func() error { callbackCalls++; return nil },
				})
				if err == nil || want != "" && !strings.Contains(err.Error(), want) {
					t.Fatalf("Install = %v; want %q", err, want)
				}
				if runner.calls != 0 || callbackCalls != 0 {
					t.Fatalf("rejected mirror crossed mutation boundary: process=%d callback=%d", runner.calls, callbackCalls)
				}
				if _, err := os.Lstat(runtime); !os.IsNotExist(err) {
					t.Fatalf("rejected mirror created runtime lock directory: %v", err)
				}
				if os.IsNotExist(before) {
					if _, err := os.Lstat(data); !os.IsNotExist(err) {
						t.Fatalf("rejected mirror created data directory: %v", err)
					}
				}
				if entries, err := os.ReadDir(filepath.Join(base, "mirror")); err != nil || len(entries) != 0 {
					t.Fatalf("rejected mirror changed root: %v, %v", entries, err)
				}
			})
		}
	}
}

func TestLocalMirrorPreflightAcceptsExplicitRootAliases(t *testing.T) {
	base := t.TempDir()
	mirror := filepath.Join(base, "mirror")
	data := filepath.Join(base, "data")
	for _, root := range []string{mirror, data} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(root, root+"-alias"); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{data + "-alias", filepath.Join(data+"-alias", "missing")} {
		if err := ValidateLocalMirror(mirror+"-alias", root, false); err != nil {
			t.Fatalf("valid root aliases rejected: %v", err)
		}
	}
}

func TestInstallRevalidatesLocalMirrorAfterPreflight(t *testing.T) {
	base := t.TempDir()
	mirror, data := filepath.Join(base, "mirror"), filepath.Join(base, "data")
	if err := os.Mkdir(mirror, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := testArtifact([]byte("fixture"))
	var output bytes.Buffer
	err := Install(InstallOptions{
		DataRoot: data, LocalMirror: mirror, Artifacts: []catalog.Artifact{artifact}, Runner: noSELinuxRunner{}, Output: &output,
		Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(base, "runtime")},
		BeforeMutation: func() error {
			if err := os.Remove(mirror); err != nil {
				return err
			}
			return os.Symlink(base, mirror)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "must not overlap") {
		t.Fatalf("changed mirror root was not revalidated: %v", err)
	}
	if _, err := os.Lstat(ArtifactPath(data, artifact)); !os.IsNotExist(err) {
		t.Fatalf("replaced mirror installed content: %v", err)
	}
	if strings.Contains(output.String(), "Checking local mirror") {
		t.Fatalf("replaced mirror was hashed: %s", output.String())
	}
}

func TestLocalMirrorRequiresMatchingNameSizeAndHashWithoutFollowingLinks(t *testing.T) {
	for _, name := range []string{"match", "wrong-name", "wrong-size", "wrong-hash", "file-symlink", "directory-symlink"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			mirror, outside := filepath.Join(base, "mirror"), filepath.Join(base, "outside")
			for _, root := range []string{mirror, outside} {
				if err := os.Mkdir(root, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			contents := []byte("fixture")
			artifact := testArtifact(contents)
			candidate := filepath.Join(mirror, "model.gguf")
			switch name {
			case "wrong-name":
				candidate = filepath.Join(mirror, "unrelated.gguf")
			case "wrong-size":
				contents = []byte("different size")
			case "wrong-hash":
				contents = []byte("changed")
			case "file-symlink":
				if err := os.Symlink(filepath.Join(outside, "model.gguf"), candidate); err != nil {
					t.Fatal(err)
				}
				candidate = filepath.Join(outside, "model.gguf")
			case "directory-symlink":
				if err := os.Symlink(outside, filepath.Join(mirror, "linked")); err != nil {
					t.Fatal(err)
				}
				candidate = filepath.Join(outside, "model.gguf")
			}
			if err := os.WriteFile(candidate, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			index, err := openMirror(mirror, filepath.Join(base, "data"), false)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			got, err := index.find(context.Background(), artifact, &output, nil)
			if err != nil || (got != "") != (name == "match") {
				t.Fatalf("find = %q, %v", got, err)
			}
		})
	}
}
