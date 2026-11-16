package content

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/storage"
	"paracetamol/internal/verification"
)

func TestInspectArtifactUsesCompatibleReceipt(t *testing.T) {
	root := t.TempDir()
	artifact := catalog.Artifact{ID: "model", Target: "llama-models", Destination: "model.gguf", Size: 5, SHA256: "digest"}
	file := ArtifactPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(file, 5, "digest"); err != nil {
		t.Fatal(err)
	}
	status, err := InspectArtifact(store, root, artifact, false)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != Verified {
		t.Fatalf("state = %s", status.State)
	}
}

func TestVerifyArtifactHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	contents := []byte("model")
	artifact := testArtifact(contents)
	file := ArtifactPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := VerifyArtifact(ctx, nil, root, artifact, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestContentInspectionRejectsSymlinkedParentsBeforeWork(t *testing.T) {
	for _, target := range []string{"models", "llama-models", "workflows"} {
		for _, shape := range []string{"absolute-inside", "relative-inside", "absolute-other-partition", "relative-other-partition", "absolute-outside-data", "absolute-broken", "relative-broken"} {
			for _, existing := range []string{"absent", "unverified", "compatible-receipt", "stale-receipt"} {
				if strings.Contains(shape, "broken") && existing != "absent" || shape == "absolute-outside-data" && strings.Contains(existing, "receipt") {
					continue
				}
				t.Run(target+"/"+shape+"/"+existing, func(t *testing.T) {
					base := t.TempDir()
					root := filepath.Join(base, "data")
					payload := []byte("fixture")
					artifact := testArtifact(payload)
					artifact.Target = target
					file := ArtifactPath(root, artifact)
					parent := filepath.Dir(file)
					partition := artifactRoot(root, artifact)
					if err := os.MkdirAll(partition, 0o755); err != nil {
						t.Fatal(err)
					}
					redirect := filepath.Join(partition, "real")
					if strings.Contains(shape, "other-partition") {
						redirect = filepath.Join(root, "unmanaged")
					} else if shape == "absolute-outside-data" {
						redirect = filepath.Join(base, "outside")
					}
					if !strings.Contains(shape, "broken") {
						if err := os.MkdirAll(redirect, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					link := redirect
					if strings.HasPrefix(shape, "relative") {
						var err error
						link, err = filepath.Rel(filepath.Dir(parent), redirect)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Symlink(link, parent); err != nil {
						t.Fatal(err)
					}
					if existing != "absent" {
						if err := os.WriteFile(file, payload, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					store, err := verification.Load(root)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(existing, "receipt") {
						// Reproduce a receipt minted by the old inspection path.
						if err := store.Record(file, artifact.Size, artifact.SHA256); err != nil {
							t.Fatal(err)
						}
						if err := store.Save(); err != nil {
							t.Fatal(err)
						}
						if existing == "stale-receipt" {
							if err := os.Chmod(file, 0o644); err != nil {
								t.Fatal(err)
							}
						}
						if got := store.Matches(file, artifact.Size, artifact.SHA256); got != (existing == "compatible-receipt") {
							t.Fatalf("fixture receipt matches = %t", got)
						}
					}
					before := contentTreeSnapshot(t, base)
					rejected := func(err error) {
						t.Helper()
						if err == nil || !strings.Contains(err.Error(), "symlinked content path component") {
							t.Fatalf("expected parent rejection, got %v", err)
						}
					}
					_, err = InspectArtifact(store, root, artifact, false)
					rejected(err)
					hashed := 0
					_, err = VerifyArtifact(context.Background(), store, root, artifact, func(int64) { hashed++ })
					rejected(err)
					if hashed != 0 || store.Changed {
						t.Fatalf("rejected path hashed or changed receipts: progress=%d changed=%t", hashed, store.Changed)
					}
					_, err = Plan(root, []catalog.Artifact{artifact})
					rejected(err)
					managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{artifact.ID: artifact}}
					_, err = RequireBundle(managed, catalog.Bundle{Artifacts: []string{artifact.ID}}, root)
					rejected(err)
					runner := &mirrorPreflightRunner{}
					callbacks := 0
					for _, dry := range []bool{false, true} {
						err = Install(InstallOptions{
							DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Runner: runner, DryRun: dry,
							Environment:    map[string]string{"XDG_RUNTIME_DIR": filepath.Join(base, "runtime")},
							BeforeMutation: func() error { callbacks++; return nil },
						})
						rejected(err)
					}
					if runner.calls != 0 || callbacks != 0 {
						t.Fatalf("rejected path crossed mutation boundary: process=%d callback=%d", runner.calls, callbacks)
					}
					if after := contentTreeSnapshot(t, base); !reflect.DeepEqual(before, after) {
						t.Fatalf("rejected path changed filesystem: before=%v after=%v", before, after)
					}
				})
			}
		}
	}
}

func TestContentInspectionRejectsInvalidPartitionPaths(t *testing.T) {
	for _, target := range []string{"models", "llama-models", "workflows"} {
		for _, shape := range []string{"partition-file", "partition-link", "data-root-file", "data-root-link", "escaped-destination", "unknown-target"} {
			t.Run(target+"/"+shape, func(t *testing.T) {
				base := t.TempDir()
				root := filepath.Join(base, "data")
				artifact := testArtifact([]byte("fixture"))
				artifact.Target = target
				blocked := artifactRoot(root, artifact)
				if strings.HasPrefix(shape, "data-root") {
					blocked = root
				}
				if err := os.MkdirAll(filepath.Dir(blocked), 0o755); err != nil {
					t.Fatal(err)
				}
				switch shape {
				case "partition-file", "data-root-file":
					if err := os.WriteFile(blocked, []byte("keep"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "partition-link", "data-root-link":
					if err := os.Symlink(filepath.Join(base, "absent"), blocked); err != nil {
						t.Fatal(err)
					}
				case "escaped-destination":
					artifact.Destination = "../escaped.gguf"
				case "unknown-target":
					artifact.Target = "unknown"
				}
				before := contentTreeSnapshot(t, base)
				progress := 0
				if _, err := VerifyArtifact(context.Background(), nil, root, artifact, func(int64) { progress++ }); err == nil {
					t.Fatal("verification accepted invalid partition path")
				}
				if _, err := Plan(root, []catalog.Artifact{artifact}); err == nil {
					t.Fatal("planning accepted invalid partition path")
				}
				managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{artifact.ID: artifact}}
				if _, err := RequireBundle(managed, catalog.Bundle{Artifacts: []string{artifact.ID}}, root); err == nil {
					t.Fatal("readiness accepted invalid partition path")
				}
				runner := &mirrorPreflightRunner{}
				callbacks := 0
				for _, dry := range []bool{false, true} {
					if err := Install(InstallOptions{
						DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Runner: runner, DryRun: dry,
						BeforeMutation: func() error { callbacks++; return nil },
					}); err == nil {
						t.Fatal("installation accepted invalid partition path")
					}
				}
				if progress != 0 || callbacks != 0 || runner.calls != 0 {
					t.Fatalf("invalid path caused work: progress=%d callbacks=%d process=%d", progress, callbacks, runner.calls)
				}
				if after := contentTreeSnapshot(t, base); !reflect.DeepEqual(before, after) {
					t.Fatalf("invalid path changed filesystem: before=%v after=%v", before, after)
				}
			})
		}
	}
}

func TestVerifyArtifactRechecksParentsAfterProgress(t *testing.T) {
	for _, target := range []string{"models", "llama-models", "workflows"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			artifact := testArtifact([]byte("fixture"))
			artifact.Target = target
			file := ArtifactPath(root, artifact)
			parent := filepath.Dir(file)
			if err := os.MkdirAll(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := verification.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			progress := 0
			_, err = VerifyArtifact(context.Background(), store, root, artifact, func(int64) {
				progress++
				if err := os.Rename(parent, parent+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(parent+"-original", parent); err != nil {
					t.Fatal(err)
				}
			})
			if err == nil || !strings.Contains(err.Error(), "symlinked content path component") {
				t.Fatalf("redirected parent verification = %v", err)
			}
			if progress != 1 || store.Changed || len(store.Records) != 0 {
				t.Fatalf("redirected path minted receipt: progress=%d store=%+v", progress, store)
			}
			if _, err := os.Lstat((storage.Layout{Root: root}).VerificationReceipt()); !os.IsNotExist(err) {
				t.Fatalf("redirected path wrote receipt: %v", err)
			}
		})
	}
}

func TestContentInspectionPreservesMissingRootAndVerifiedReadiness(t *testing.T) {
	for _, target := range []string{"models", "llama-models", "workflows"} {
		t.Run(target, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "absent", "data")
			artifact := testArtifact([]byte("fixture"))
			artifact.Target = target
			plan, err := Plan(root, []catalog.Artifact{artifact})
			if err != nil || len(plan.Statuses) != 1 || plan.Statuses[0].State != Missing || plan.DownloadBytes != artifact.Size {
				t.Fatalf("absent-root plan = %+v, %v", plan, err)
			}
			runner := &mirrorPreflightRunner{}
			if err := Install(InstallOptions{DataRoot: root, Artifacts: []catalog.Artifact{artifact}, DryRun: true, Runner: runner}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(root); !os.IsNotExist(err) || runner.calls != 0 {
				t.Fatalf("dry run created root or started process: %v, calls=%d", err, runner.calls)
			}
			file := ArtifactPath(root, artifact)
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := verification.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			status, err := VerifyArtifact(context.Background(), store, root, artifact, nil)
			if err != nil || status.State != Verified {
				t.Fatalf("normal verification = %+v, %v", status, err)
			}
			if err := store.Save(); err != nil {
				t.Fatal(err)
			}
			managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{artifact.ID: artifact}}
			statuses, err := RequireBundle(managed, catalog.Bundle{Artifacts: []string{artifact.ID}}, root)
			if err != nil || len(statuses) != 1 || !Ready(statuses[0].State) {
				t.Fatalf("normal readiness = %+v, %v", statuses, err)
			}
		})
	}
}

func contentTreeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := fmt.Sprint(info.Mode())
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += ":" + link
		} else if !entry.IsDir() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += ":" + string(contents)
		}
		entries[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
