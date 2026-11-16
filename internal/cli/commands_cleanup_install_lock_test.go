package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
)

func cleanupInstallArtifact(payload []byte) catalog.Artifact {
	artifact := catalog.Artifact{ID: "fixture-model", Target: "llama-models", Destination: "model.gguf", Size: int64(len(payload)), SHA256: fmt.Sprintf("%x", sha256.Sum256(payload))}
	artifact.Source.Provider = "huggingface"
	artifact.Source.Repository = "example/model"
	artifact.Source.Revision = strings.Repeat("a", 40)
	artifact.Source.Path = "model.gguf"
	artifact.License.Status = "verified"
	return artifact
}

type cleanupInstallPauseWriter struct {
	match   string
	entered chan struct{}
	release chan struct{}
	paused  bool
}

func (writer *cleanupInstallPauseWriter) Write(value []byte) (int, error) {
	if !writer.paused && strings.Contains(string(value), writer.match) {
		writer.paused = true
		close(writer.entered)
		<-writer.release
	}
	return len(value), nil
}

func waitCleanupInstallPause(t *testing.T, writer *cleanupInstallPauseWriter) {
	t.Helper()
	select {
	case <-writer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("operation did not reach the fixture pause")
	}
}

func cleanupInstallSelection(t *testing.T, base, root, shape string) string {
	t.Helper()
	if shape == "real" {
		return root
	}
	alias := filepath.Join(base, "alias")
	target := root
	if shape == "ancestor-alias" {
		target = base
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if shape == "ancestor-alias" {
		return filepath.Join(alias, filepath.Base(root))
	}
	return alias
}

func cleanupInstallTarget(root, scope string) string {
	switch scope {
	case "downloads":
		return filepath.Join(root, "staging")
	case "caches":
		return filepath.Join(root, "apps", "llama-cpp", "cache")
	default:
		return root
	}
}

func TestCleanupCannotDeleteActiveInstallBytes(t *testing.T) {
	for _, mode := range []string{"mirror-move", "existing-verification"} {
		for _, scope := range []string{"data", "downloads", "caches"} {
			for _, shape := range []string{"real", "root-alias", "ancestor-alias"} {
				t.Run(mode+"/"+scope+"/"+shape, func(t *testing.T) {
					base := gatewayOwnershipHome(t)
					root, mirror := filepath.Join(base, "data"), filepath.Join(base, "mirror")
					payload := []byte("offline fixture model\n")
					artifact := cleanupInstallArtifact(payload)
					writeOwnershipSentinel(t, filepath.Join(cleanupInstallTarget(root, scope), "sentinel"))
					writeOwnershipSentinel(t, filepath.Join(root, "apps", "llama-cpp", "output", "keep"))
					path := content.ArtifactPath(root, artifact)
					if mode == "mirror-move" {
						path = filepath.Join(mirror, artifact.Destination)
					}
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, payload, 0o600); err != nil {
						t.Fatal(err)
					}
					app, _, _ := testApp(t, &commandRunner{})
					selected := cleanupInstallSelection(t, base, root, shape)
					// Exercise the same alias resolution used by CLI installation;
					// destructive cleanup must still select a non-aliased path.
					resolved, err := app.resolveManagedDataRoot(selected)
					if err != nil {
						t.Fatal(err)
					}
					writer := &cleanupInstallPauseWriter{match: "SHA-256 for", entered: make(chan struct{}), release: make(chan struct{})}
					options := content.InstallOptions{DataRoot: resolved, Artifacts: []catalog.Artifact{artifact}, Environment: map[string]string{"HOME": filepath.Join(base, "installer-home")}, Runner: &commandRunner{run: gatewayOwnershipPodman}, Output: writer}
					if mode == "mirror-move" {
						options.LocalMirror, options.MoveFromMirror = mirror, true
					}
					done := make(chan error, 1)
					go func() { done <- content.Install(options) }()
					finished := false
					finish := func() {
						if !finished {
							close(writer.release)
							if err := <-done; err != nil {
								t.Errorf("installer failed after blocked cleanup: %v", err)
							}
							finished = true
						}
					}
					defer finish()
					waitCleanupInstallPause(t, writer)
					if mode == "mirror-move" {
						if _, err := os.Lstat(path); !os.IsNotExist(err) {
							t.Fatalf("fixture paused before moving the sole mirror source: %v", err)
						}
						found := false
						err := filepath.WalkDir(filepath.Join(root, "staging"), func(path string, entry fs.DirEntry, err error) error {
							if err == nil && entry.Type().IsRegular() {
								actual, readErr := os.ReadFile(path)
								found = found || readErr == nil && string(actual) == string(payload)
							}
							return err
						})
						if err != nil || !found {
							t.Fatalf("sole moved payload was not in staging: found=%t, %v", found, err)
						}
					}
					for _, yes := range []bool{false, true} {
						runner := &commandRunner{}
						cleanup, stdout, _ := testApp(t, runner)
						cleanup.Environment = map[string]string{"HOME": filepath.Join(base, "cleanup-home")}
						cleanup.Stdin = preflightTerminalReader{Reader: strings.NewReader("yes\n")}
						args := []string{scope, "--data-dir", root}
						if yes {
							args = append(args, "--yes")
						}
						if err := cleanup.commandCleanup(args); !errors.Is(err, storage.ErrMutationBusy) {
							t.Fatalf("cleanup did not exclude active %s: %v", mode, err)
						}
						if stdout.Len() != 0 || len(runner.commands) != 0 {
							t.Fatalf("busy cleanup reached checks, plan or approval: output=%q commands=%v", stdout, runner.commands)
						}
					}
					other := filepath.Join(base, "independent-data")
					otherTarget := cleanupInstallTarget(other, scope)
					writeOwnershipSentinel(t, filepath.Join(otherTarget, "selected"))
					cleanup, _, _ := testApp(t, &commandRunner{run: gatewayOwnershipPodman})
					cleanup.Environment = map[string]string{"HOME": base}
					if err := cleanup.commandCleanup([]string{scope, "--data-dir", other, "--yes"}); err != nil {
						t.Fatalf("active install blocked independent root cleanup: %v", err)
					}
					if _, err := os.Lstat(otherTarget); !os.IsNotExist(err) {
						t.Fatalf("independent cleanup did not remove its selected target: %v", err)
					}
					finish()
					plan, err := content.Plan(root, []catalog.Artifact{artifact})
					if err != nil || plan.Ready != 1 {
						t.Fatalf("cleanup damaged installed content or receipt: %+v, %v", plan, err)
					}
					for _, sentinel := range []string{filepath.Join(cleanupInstallTarget(root, scope), "sentinel"), filepath.Join(root, "apps", "llama-cpp", "output", "keep")} {
						if _, err := os.Stat(sentinel); err != nil {
							t.Fatalf("busy cleanup removed %s: %v", sentinel, err)
						}
					}
				})
			}
		}
	}
}

func TestInstallCannotMutateDuringCleanupApproval(t *testing.T) {
	for _, scope := range []string{"data", "downloads", "caches"} {
		t.Run(scope, func(t *testing.T) {
			base := gatewayOwnershipHome(t)
			root := filepath.Join(base, "data")
			writeOwnershipSentinel(t, filepath.Join(cleanupInstallTarget(root, scope), "sentinel"))
			app, _, _ := testApp(t, &commandRunner{run: gatewayOwnershipPodman})
			app.Environment = map[string]string{"HOME": base}
			app.Stdin = preflightTerminalReader{Reader: strings.NewReader("yes\n")}
			writer := &cleanupInstallPauseWriter{match: "Proceed with", entered: make(chan struct{}), release: make(chan struct{})}
			app.Stdout = writer
			done := make(chan error, 1)
			go func() { done <- app.commandCleanup([]string{scope, "--data-dir", root}) }()
			finished := false
			finish := func() {
				if !finished {
					close(writer.release)
					if err := <-done; err != nil {
						t.Errorf("cleanup failed after approval: %v", err)
					}
					finished = true
				}
			}
			defer finish()
			waitCleanupInstallPause(t, writer)
			artifact := cleanupInstallArtifact([]byte("fixture"))
			for _, shape := range []string{"real", "root-alias", "ancestor-alias"} {
				selected := cleanupInstallSelection(t, base, root, shape)
				resolved, err := app.resolveManagedDataRoot(selected)
				if err != nil {
					t.Fatal(err)
				}
				runner := &commandRunner{}
				options := content.InstallOptions{DataRoot: resolved, Artifacts: []catalog.Artifact{artifact}, Runner: runner}
				if err := content.Install(options); !errors.Is(err, storage.ErrMutationBusy) {
					t.Fatalf("installer entered approved cleanup through %s: %v", shape, err)
				}
				options.DryRun = true
				if err := content.Install(options); err != nil {
					t.Fatalf("dry-run installer acquired mutation ownership: %v", err)
				}
				if len(runner.commands) != 0 {
					t.Fatalf("blocked/dry-run installer ran subprocesses: %v", runner.commands)
				}
				if shape != "real" {
					if err := os.Remove(filepath.Join(base, "alias")); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := os.Lstat(filepath.Join(root, "staging", "llama-cpp")); !os.IsNotExist(err) {
				t.Fatalf("rejected installation prepared staging: %v", err)
			}
			finish()
			if _, err := os.Lstat(cleanupInstallTarget(root, scope)); !os.IsNotExist(err) {
				t.Fatalf("approved cleanup left its captured target: %v", err)
			}
			if err := content.Install(content.InstallOptions{DataRoot: root, Runner: &commandRunner{run: gatewayOwnershipPodman}}); err != nil {
				t.Fatalf("completed cleanup retained mutation lock: %v", err)
			}
		})
	}
}

func TestCleanupMutationLockReleasedAfterFailureOrCancellation(t *testing.T) {
	for _, scenario := range []string{"podman-failure", "declined", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			base := gatewayOwnershipHome(t)
			root := filepath.Join(base, "data")
			writeOwnershipSentinel(t, filepath.Join(root, "staging", "sentinel"))
			runner := &commandRunner{run: gatewayOwnershipPodman}
			if scenario == "podman-failure" {
				runner.run = func(process.Command) process.Result { return process.Result{Status: 1} }
			}
			app, stdout, _ := testApp(t, runner)
			app.Environment = map[string]string{"HOME": base}
			app.Stdin = preflightTerminalReader{Reader: strings.NewReader("no\n")}
			if scenario == "cancelled" {
				ctx, cancel := context.WithCancel(context.Background())
				app.Context = ctx
				reader := &blockingPromptReader{entered: make(chan struct{}), release: make(chan struct{})}
				app.Stdin = terminalPromptReader{Reader: reader}
				defer close(reader.release)
				app.Stdout = ownershipHookWriter{Writer: stdout, hook: func(value string) {
					if strings.Contains(value, "Proceed with") {
						cancel()
					}
				}}
				defer cancel()
			}
			err := app.commandCleanup([]string{"downloads", "--data-dir", root})
			if err == nil || scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("failing cleanup = %v", err)
			}
			if err := content.Install(content.InstallOptions{DataRoot: root, Runner: &commandRunner{run: gatewayOwnershipPodman}}); err != nil {
				t.Fatalf("failed cleanup retained mutation lock: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "staging", "sentinel")); err != nil {
				t.Fatalf("failed cleanup removed staging: %v", err)
			}
		})
	}
}

func TestCleanupAfterInstallFailureOrCancellation(t *testing.T) {
	for _, scenario := range []string{"hash-failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			base := gatewayOwnershipHome(t)
			root := filepath.Join(base, "data")
			artifact := cleanupInstallArtifact([]byte("right"))
			path := content.ArtifactPath(root, artifact)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("wrong"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &cleanupInstallPauseWriter{match: "SHA-256 for existing", entered: make(chan struct{}), release: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				done <- content.Install(content.InstallOptions{Context: ctx, DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Runner: &commandRunner{run: gatewayOwnershipPodman}, Output: writer})
			}()
			finished := false
			defer func() {
				if !finished {
					close(writer.release)
					<-done
				}
			}()
			waitCleanupInstallPause(t, writer)
			if scenario == "cancelled" {
				cancel()
			}
			close(writer.release)
			err := <-done
			finished = true
			if err == nil || scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("installer = %v", err)
			}
			app, _, _ := testApp(t, &commandRunner{run: gatewayOwnershipPodman})
			app.Environment = map[string]string{"HOME": base}
			if err := app.commandCleanup([]string{"data", "--data-dir", root, "--yes"}); err != nil {
				t.Fatalf("completed failing installer blocked cleanup: %v", err)
			}
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("cleanup did not remove its confirmed root: %v", err)
			}
		})
	}
}

func TestCleanupAbsentRootCreatesNoMutationState(t *testing.T) {
	for _, scope := range []string{"data", "downloads", "caches"} {
		t.Run(scope, func(t *testing.T) {
			base := gatewayOwnershipHome(t)
			root := filepath.Join(base, "missing", "data")
			runner := &commandRunner{}
			app, stdout, _ := testApp(t, runner)
			app.Environment = map[string]string{"HOME": base}
			if err := app.commandCleanup([]string{scope, "--data-dir", root, "--yes"}); err != nil {
				t.Fatal(err)
			}
			if len(runner.commands) != 0 || !strings.Contains(stdout.String(), "Data not present:") {
				t.Fatalf("absent cleanup reached checks: output=%q commands=%v", stdout, runner.commands)
			}
			if _, err := os.Lstat(filepath.Dir(root)); !os.IsNotExist(err) {
				t.Fatalf("absent cleanup created persistent state: %v", err)
			}
		})
	}
}
