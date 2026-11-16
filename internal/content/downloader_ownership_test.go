package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

type downloaderOwnershipRunner struct {
	noSELinuxRunner
	path, scenario, name, launchID, removed string
	cancel                                  context.CancelFunc
	cleanupErr                              error
}

func (runner *downloaderOwnershipRunner) LookPath(name string) (string, error) {
	if name == "podman" {
		return name, nil
	}
	return runner.noSELinuxRunner.LookPath(name)
}

func (runner *downloaderOwnershipRunner) labels() map[string]string {
	launch := runner.launchID
	if runner.scenario == "foreign" {
		launch = strings.Repeat("f", 32)
	}
	return map[string]string{podman.ManagedContainerLabel: "true", podman.ManagedRoleLabel: "download", podman.ManagedLaunchLabel: launch}
}

func (runner *downloaderOwnershipRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	a := command.Args
	if command.Name != "podman" || len(a) == 0 {
		return process.Result{}, fmt.Errorf("unexpected command %v", command)
	}
	switch a[0] {
	case "ps", "image":
		return process.Result{}, nil
	case "info":
		return process.Result{Stdout: []byte("true")}, nil
	case "run":
		for i := 0; i+1 < len(a); i++ {
			if a[i] == "--name" {
				runner.name = a[i+1]
			}
			if a[i] == "--label" && strings.HasPrefix(a[i+1], podman.ManagedLaunchLabel+"=") {
				runner.launchID = strings.TrimPrefix(a[i+1], podman.ManagedLaunchLabel+"=")
			}
		}
		if runner.name == "" || len(runner.launchID) != 32 {
			return process.Result{}, fmt.Errorf("missing launch identity")
		}
		if err := os.WriteFile(runner.path, []byte("partial"), 0o600); err != nil {
			return process.Result{}, err
		}
		if runner.scenario == "cancelled" {
			runner.cancel()
			return process.Result{Stderr: []byte("cancelled diagnostic")}, ctx.Err()
		}
		if runner.scenario == "process-error" {
			return process.Result{Stderr: []byte("process diagnostic")}, errors.New("process failed")
		}
		return process.Result{Status: 125, Stderr: []byte("download diagnostic")}, nil
	case "inspect":
		if err := ctx.Err(); err != nil {
			return process.Result{}, err
		}
		raw, err := json.Marshal(runner.labels())
		return process.Result{Stdout: raw}, err
	case "container":
		if err := ctx.Err(); err != nil {
			return process.Result{}, err
		}
		if a[1] == "exists" {
			return process.Result{}, nil
		}
		if a[1] == "inspect" {
			name := runner.name
			if a[2] == strings.Repeat("a", 64) {
				name = "renamed-original"
			}
			raw, err := json.Marshal([]map[string]any{{"Id": strings.Repeat("a", 64), "Name": name, "Config": map[string]any{"Labels": runner.labels()}}})
			return process.Result{Stdout: raw}, err
		}
	case "rm":
		if err := ctx.Err(); err != nil {
			return process.Result{}, err
		}
		runner.removed = a[len(a)-1]
		return process.Result{}, runner.cleanupErr
	}
	return process.Result{}, fmt.Errorf("unexpected Podman command %v", a)
}

func TestDownloaderCleanupUsesLaunchIdentityAndCapturedID(t *testing.T) {
	for _, scenario := range []string{"foreign", "owned", "cancelled", "process-error", "cleanup-error"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			artifact := testArtifact([]byte("complete fixture"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := &downloaderOwnershipRunner{path: downloadPath(root, artifact), scenario: scenario, cancel: cancel}
			if scenario == "cleanup-error" {
				runner.cleanupErr = errors.New("removal failed")
			}
			var output bytes.Buffer
			err := Install(InstallOptions{Context: ctx, Runner: runner, DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Output: &output})
			if err == nil {
				t.Fatal("failed downloader succeeded")
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if scenario == "cleanup-error" && (!strings.Contains(err.Error(), "download failed") || !strings.Contains(err.Error(), "removal failed")) {
				t.Fatalf("lost errors: %v", err)
			}
			wantRemoval := strings.Repeat("a", 64)
			if scenario == "foreign" {
				wantRemoval = ""
			}
			if runner.removed != wantRemoval {
				t.Fatalf("removed %q, want %q", runner.removed, wantRemoval)
			}
			if !strings.Contains(output.String(), "diagnostic") {
				t.Fatalf("lost stderr: %s", output.String())
			}
			if got, readErr := os.ReadFile(filepath.Clean(runner.path)); readErr != nil || string(got) != "partial" {
				t.Fatalf("lost resumable staging: %q, %v", got, readErr)
			}
		})
	}
}
