package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

const fixtureContainerID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestLlamaLaunchPlanningPreservesLabelLookingPrompts(t *testing.T) {
	root := gatewayOwnershipHome(t)
	model := filepath.Join(root, "fixture.gguf")
	if err := os.WriteFile(model, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"--label=" + podman.ManagedLaunchLabel + "=bad", "--label", podman.ManagedLaunchLabel + "=bad"} {
		app, stdout, _ := testApp(t, &commandRunner{})
		app.Root = filepath.Join("..", "..")
		data := filepath.Join(root, "uncreated-data")
		if err := app.runLlama("cli", []string{"--model", model, "--prompt", prompt, "--profile", "cpu", "--data-dir", data, "--dry-run"}); err != nil {
			t.Fatalf("prompt %q: %v", prompt, err)
		}
		if !strings.Contains(stdout.String(), prompt) {
			t.Fatalf("prompt %q missing from resolved command: %s", prompt, stdout)
		}
		if _, err := os.Stat(data); !os.IsNotExist(err) {
			t.Fatalf("dry run created runtime state: %v", err)
		}
	}
}

func fixtureContainerLabels(application, role, launch string) map[string]string {
	labels := map[string]string{podman.ManagedContainerLabel: "true", podman.ManagedApplicationLabel: application, podman.ManagedRoleLabel: role}
	if launch != "" {
		labels[podman.ManagedLaunchLabel] = launch
	}
	return labels
}

func fixtureContainerJSON(t *testing.T, id, name string, labels map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal([]any{map[string]any{"Id": id, "Name": name, "Config": map[string]any{"Labels": labels}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStopRefusesForeignAndMismatchedCanonicalContainers(t *testing.T) {
	spec, _ := config.ApplicationByID("comfyui")
	for _, labels := range []map[string]string{nil, fixtureContainerLabels("llama-cpp", "application", ""), fixtureContainerLabels("comfyui", "benchmark", "")} {
		runner := &commandRunner{run: func(command process.Command) process.Result {
			switch command.Args[0] {
			case "info":
				return process.Result{Stdout: []byte("true")}
			case "container":
				if command.Args[1] == "inspect" {
					return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, spec.ContainerName, labels)}
				}
				return process.Result{}
			case "rm":
				t.Fatal("stop removed a foreign or mismatched canonical occupant")
			}
			return process.Result{}
		}}
		app, _, _ := testApp(t, runner)
		if err := app.commandStop([]string{"comfyui"}); err == nil {
			t.Fatalf("foreign labels accepted: %v", labels)
		}
	}
}

func TestStopAllValidatesCompletePlanBeforeRemovingAnyContainer(t *testing.T) {
	appSpecs := config.Applications()
	runner := &commandRunner{run: func(command process.Command) process.Result {
		switch command.Args[0] {
		case "info":
			return process.Result{Stdout: []byte("true")}
		case "container":
			if command.Args[1] == "inspect" {
				name := command.Args[len(command.Args)-1]
				labels := fixtureContainerLabels(appSpecs[0].ID, "application", "")
				if name == appSpecs[1].ContainerName {
					labels = nil
				}
				return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, name, labels)}
			}
			return process.Result{}
		case "rm":
			t.Fatal("stop all mutated before validating its complete plan")
		}
		return process.Result{}
	}}
	app, _, _ := testApp(t, runner)
	app.Environment["HOME"] = gatewayOwnershipHome(t)
	if err := app.commandStop([]string{"all"}); err == nil {
		t.Fatal("stop all accepted foreign second target")
	}
}

func TestCleanupNeverDiscoversContainersFromLegacyNames(t *testing.T) {
	runner := &commandRunner{run: func(command process.Command) process.Result {
		switch command.Args[0] {
		case "info":
			return process.Result{Stdout: []byte("true")}
		case "ps":
			return process.Result{}
		default:
			t.Fatalf("cleanup inspected or removed an unlabelled canonical/transient name: %v", command.Args)
		}
		return process.Result{}
	}}
	app, stdout, _ := testApp(t, runner)
	if err := app.commandCleanup([]string{"containers", "comfyui", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "No managed containers") {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestCleanupValidatesAllDiscoveredOwnersBeforeMutation(t *testing.T) {
	runner := &commandRunner{run: func(command process.Command) process.Result {
		switch command.Args[0] {
		case "info":
			return process.Result{Stdout: []byte("true")}
		case "ps":
			return process.Result{Stdout: []byte("first\nsecond\n")}
		case "container":
			if command.Args[1] == "inspect" {
				name := command.Args[len(command.Args)-1]
				labels := fixtureContainerLabels("comfyui", "application", "")
				if name == "second" {
					labels[podman.ManagedRoleLabel] = "unknown"
				}
				return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, name, labels)}
			}
			return process.Result{}
		case "rm":
			t.Fatal("cleanup mutated before validating all discovered owners")
		}
		return process.Result{}
	}}
	app, stdout, _ := testApp(t, runner)
	if err := app.commandCleanup([]string{"containers", "comfyui", "--yes"}); err == nil {
		t.Fatal("unreviewed role accepted")
	}
	if strings.Contains(stdout.String(), "Cleanup plan:") {
		t.Fatal("invalid plan reached confirmation")
	}
}

func TestCleanupAndStopRemoveCapturedIDAfterNameReuse(t *testing.T) {
	spec, _ := config.ApplicationByID("comfyui")
	for _, cleanup := range []bool{false, true} {
		t.Run(fmt.Sprint("cleanup=", cleanup), func(t *testing.T) {
			removed := false
			runner := &commandRunner{run: func(command process.Command) process.Result {
				switch command.Args[0] {
				case "info":
					return process.Result{Stdout: []byte("true")}
				case "ps":
					return process.Result{Stdout: []byte(spec.ContainerName)}
				case "container":
					if command.Args[1] == "inspect" {
						ref := command.Args[len(command.Args)-1]
						name := spec.ContainerName
						if ref == fixtureContainerID {
							// The old object has been renamed; a foreign object now
							// occupies the canonical name. ID lookup still finds ours.
							name = "renamed-original"
						}
						return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, name, fixtureContainerLabels("comfyui", "application", ""))}
					}
					return process.Result{}
				case "rm":
					if got := command.Args[len(command.Args)-1]; got != fixtureContainerID {
						t.Fatalf("removed replacement by reusable name %q", got)
					}
					removed = true
				}
				return process.Result{}
			}}
			app, _, _ := testApp(t, runner)
			var err error
			if cleanup {
				err = app.commandCleanup([]string{"containers", "comfyui", "--yes"})
			} else {
				err = app.commandStop([]string{"comfyui"})
			}
			if err != nil || !removed {
				t.Fatalf("removed=%t err=%v", removed, err)
			}
		})
	}
}

type launchLifecycleRunner struct {
	run func(context.Context, process.Command) (process.Result, error)
}

func (runner launchLifecycleRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	return runner.run(ctx, command)
}
func (launchLifecycleRunner) LookPath(string) (string, error) { return "podman", nil }

func TestManagedLaunchFailureCleanupRequiresThisLaunchAndPreservesLifecycle(t *testing.T) {
	spec, _ := config.ApplicationByID("comfyui")
	for _, mode := range []string{"own failure", "interrupted", "different launch", "foreign occupant", "absent", "inspection failure", "detached failure", "attached success", "detached success"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ran, removed, inspected := false, false, false
			launch := ""
			runner := launchLifecycleRunner{run: func(commandContext context.Context, command process.Command) (process.Result, error) {
				switch command.Args[0] {
				case "info":
					return process.Result{Stdout: []byte("true")}, nil
				case "image":
					return process.Result{}, nil
				case "ps":
					if ran {
						return process.Result{Stdout: []byte(spec.ContainerName)}, nil
					}
					return process.Result{}, nil
				case "create":
					ran = true
					launch = strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
					if len(launch) != 32 {
						t.Fatalf("missing launch identity: %v", command.Args)
					}
					if strings.HasSuffix(mode, "success") || mode == "interrupted" {
						return process.Result{Stdout: []byte(fixtureContainerID)}, nil
					}
					return process.Result{Status: 7}, nil
				case "start":
					if mode == "interrupted" {
						cancel()
						return process.Result{}, context.Canceled
					}
					if strings.HasSuffix(mode, "success") {
						return process.Result{}, nil
					}
					return process.Result{Status: 7}, nil
				case "container":
					if !ran || mode == "absent" {
						return process.Result{Status: 1}, nil
					}
					if commandContext.Err() != nil {
						t.Fatal("failed launch cleanup inherited canceled context")
					}
					if command.Args[1] == "inspect" {
						return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, spec.ContainerName, fixtureContainerLabels(spec.ID, "application", launch))}, nil
					}
					return process.Result{}, nil
				case "inspect":
					inspected = true
					if mode == "inspection failure" {
						return process.Result{}, errors.New("inspection unavailable")
					}
					labels := fixtureContainerLabels(spec.ID, "application", launch)
					if mode == "different launch" {
						labels[podman.ManagedLaunchLabel] = strings.Repeat("f", 32)
					} else if mode == "foreign occupant" {
						labels = nil
					}
					data, err := json.Marshal(labels)
					return process.Result{Stdout: data}, err
				case "rm":
					if commandContext.Err() != nil || command.Args[len(command.Args)-1] != fixtureContainerID {
						t.Fatalf("unsafe failed launch removal: %v %v", command.Args, commandContext.Err())
					}
					removed = true
					return process.Result{}, nil
				}
				return process.Result{}, fmt.Errorf("unexpected command %v", command.Args)
			}}
			app, _, _ := testApp(t, runner)
			app.Context = ctx
			command, token, err := podman.WithLaunchIdentity([]string{"podman", "run", "--name", spec.ContainerName, spec.Image})
			if err != nil {
				t.Fatal(err)
			}
			err = app.startManaged(spec, spec.Image, command, nil, token, t.TempDir())
			if wantError := !strings.HasSuffix(mode, "success"); (err != nil) != wantError {
				t.Fatalf("err=%v wantError=%t", err, wantError)
			}
			if removed != (mode == "own failure" || mode == "interrupted" || mode == "detached failure") {
				t.Fatalf("removed=%t err=%v", removed, err)
			}
			if mode == "interrupted" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if mode == "inspection failure" && !strings.Contains(err.Error(), "inspection unavailable") {
				t.Fatalf("lost cleanup failure: %v", err)
			}
			if strings.HasSuffix(mode, "success") {
				if inspected {
					t.Fatal("ordinary detached/success lifecycle performed failure cleanup")
				}
			} else if mode != "interrupted" {
				var controlled *controlerr.Error
				if !errors.As(err, &controlled) || controlled.Status != 7 {
					t.Fatalf("lost application exit status: %v", err)
				}
			}
		})
	}
}

type concurrentLaunchRunner struct {
	t         *testing.T
	mu        sync.Mutex
	preflight int
	ready     chan struct{}
	finish    chan struct{}
	winner    map[string]string
	removed   bool
}

func (*concurrentLaunchRunner) LookPath(string) (string, error) { return "podman", nil }
func (runner *concurrentLaunchRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	switch command.Args[0] {
	case "info":
		return process.Result{Stdout: []byte("true")}, nil
	case "image":
		return process.Result{}, nil
	case "ps":
		runner.mu.Lock()
		defer runner.mu.Unlock()
		spec, _ := config.ApplicationByID("comfyui")
		if runner.winner != nil {
			return process.Result{Stdout: []byte(spec.ContainerName)}, nil
		}
		return process.Result{}, nil
	case "container":
		runner.mu.Lock()
		if command.Args[1] == "inspect" {
			spec, _ := config.ApplicationByID("comfyui")
			data := fixtureContainerJSON(runner.t, fixtureContainerID, spec.ContainerName, runner.winner)
			runner.mu.Unlock()
			return process.Result{Stdout: data}, nil
		}
		if runner.winner == nil {
			runner.preflight++
			if runner.preflight == 2 {
				close(runner.ready)
			}
			runner.mu.Unlock()
			<-runner.ready
			return process.Result{Status: 1}, nil
		}
		runner.mu.Unlock()
		return process.Result{}, nil
	case "inspect":
		runner.mu.Lock()
		data, err := json.Marshal(runner.winner)
		runner.mu.Unlock()
		return process.Result{Stdout: data}, err
	case "create":
		launch := strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
		runner.mu.Lock()
		if runner.winner != nil {
			runner.mu.Unlock()
			return process.Result{Status: 125, Stderr: []byte("name is already in use")}, nil
		}
		runner.winner = fixtureContainerLabels("comfyui", "application", launch)
		runner.mu.Unlock()
		return process.Result{Stdout: []byte(fixtureContainerID)}, nil
	case "start":
		<-runner.finish
		return process.Result{}, nil
	case "rm":
		runner.mu.Lock()
		runner.removed = true
		runner.mu.Unlock()
		return process.Result{}, nil
	}
	return process.Result{}, fmt.Errorf("unexpected command %v", command.Args)
}

func TestConcurrentAttachedLaunchLoserNeverRemovesWinner(t *testing.T) {
	spec, _ := config.ApplicationByID("comfyui")
	runner := &concurrentLaunchRunner{t: t, ready: make(chan struct{}), finish: make(chan struct{})}
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { close(runner.finish) }) }
	defer finish()
	outcomes := make(chan error, 2)
	for range 2 {
		go func() {
			var output bytes.Buffer
			app := &App{Context: context.Background(), Runner: runner, Stdin: strings.NewReader(""), Stdout: &output, Stderr: &output}
			command, token, err := podman.WithLaunchIdentity([]string{"podman", "run", "--name", spec.ContainerName, spec.Image})
			if err != nil {
				outcomes <- err
				return
			}
			outcomes <- app.startManaged(spec, spec.Image, command, nil, token, t.TempDir())
		}()
	}
	select {
	case err := <-outcomes:
		if err == nil || !strings.Contains(err.Error(), "125") {
			t.Fatalf("expected concurrent loser, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent loser did not finish")
	}
	runner.mu.Lock()
	removed, winner := runner.removed, maps.Clone(runner.winner)
	runner.mu.Unlock()
	if removed || winner == nil {
		t.Fatalf("winner=%v removed=%t", winner, removed)
	}
	// The winning attached command is still running until its own owner ends it.
	select {
	case err := <-outcomes:
		t.Fatalf("winner exited unexpectedly: %v", err)
	default:
	}
	finish()
	select {
	case err := <-outcomes:
		if err != nil {
			t.Fatalf("winner failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("winner did not exit when its owner finished")
	}
}
