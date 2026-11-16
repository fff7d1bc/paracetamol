package content

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
)

type completionCancelRunner struct {
	noSELinuxRunner
	cancel context.CancelFunc
}

func (runner completionCancelRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	result, err := runner.noSELinuxRunner.Run(ctx, command)
	runner.cancel()
	return result, err
}

func TestInstallCompletionRunsAfterPublicationUnderSameReservation(t *testing.T) {
	for _, result := range []string{"success", "failure", "cancelled"} {
		t.Run(result, func(t *testing.T) {
			base := t.TempDir()
			root, mirror := filepath.Join(base, "data"), filepath.Join(base, "mirror")
			if err := os.Mkdir(mirror, 0o700); err != nil {
				t.Fatal(err)
			}
			payload := []byte("fixture")
			artifact := testArtifact(payload)
			if err := os.WriteFile(filepath.Join(mirror, "model.gguf"), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := errors.New("caller publication failed")
			calls := 0
			err := Install(InstallOptions{
				Context: ctx, DataRoot: root, Artifacts: []catalog.Artifact{artifact},
				LocalMirror: mirror, Runner: noSELinuxRunner{},
				AfterInstall: func() error {
					calls++
					plan, err := Plan(root, []catalog.Artifact{artifact})
					if err != nil || plan.Ready != 1 {
						t.Fatalf("completion preceded receipt publication: %+v, %v", plan, err)
					}
					if _, err := os.Lstat(stagingRoot(root, artifact)); !os.IsNotExist(err) {
						t.Fatalf("completion preceded completed staging cleanup: %v", err)
					}
					lock, err := storage.AcquireMutationLock(root)
					if lock != nil {
						lock.Close()
					}
					if !errors.Is(err, storage.ErrMutationBusy) {
						t.Fatalf("completion lost install reservation: %v", err)
					}
					if result == "failure" {
						return want
					}
					if result == "cancelled" {
						cancel()
						return ctx.Err()
					}
					return nil
				},
			})
			if calls != 1 || result == "success" && err != nil || result == "failure" && !errors.Is(err, want) || result == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("completion calls=%d, result=%v", calls, err)
			}
			lock, err := storage.AcquireMutationLock(root)
			if err != nil {
				t.Fatalf("completed install retained reservation: %v", err)
			}
			lock.Close()
		})
	}
}

func TestInstallSkipsCompletionForDryRunFailedOrCancelledArtifacts(t *testing.T) {
	for _, outcome := range []string{"dry-run", "hash-failure", "cancel-before-mutation", "cancel-before-completion"} {
		t.Run(outcome, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "data")
			artifact := testArtifact([]byte("right"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if outcome == "hash-failure" {
				path := ArtifactPath(root, artifact)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("wrong"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			artifacts := []catalog.Artifact{artifact}
			if outcome == "cancel-before-completion" {
				// Empty-artifact installs still execute the orphan guard and
				// receipt save; cancel at that guard, immediately before completion.
				artifacts = nil
			}
			calls := 0
			options := InstallOptions{
				Context: ctx, DataRoot: root, Artifacts: artifacts, Runner: noSELinuxRunner{},
				DryRun:       outcome == "dry-run",
				AfterInstall: func() error { calls++; return nil },
			}
			if outcome == "cancel-before-mutation" {
				options.BeforeMutation = func() error { cancel(); return nil }
			}
			if outcome == "cancel-before-completion" {
				options.Runner = completionCancelRunner{cancel: cancel}
			}
			err := Install(options)
			if calls != 0 || outcome == "dry-run" && err != nil || outcome != "dry-run" && err == nil {
				t.Fatalf("completion calls=%d, Install=%v", calls, err)
			}
			if outcome == "dry-run" || outcome == "cancel-before-mutation" {
				if _, err := os.Lstat(root); !os.IsNotExist(err) {
					t.Fatalf("non-mutating install created root: %v", err)
				}
			}
		})
	}
}
