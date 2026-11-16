package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/content"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
)

type admissionRunner struct {
	t                *testing.T
	options          LaunchOptions
	created, removed bool
	mode             string
	cancel           context.CancelFunc
	start            func(process.Command)
	commands         []process.Command
}

func (*admissionRunner) LookPath(string) (string, error) { return "podman", nil }
func (runner *admissionRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	runner.commands = append(runner.commands, command)
	id := strings.Repeat("a", 64)
	labels := map[string]string{podman.ManagedContainerLabel: "true", podman.ManagedApplicationLabel: runner.options.Application, podman.ManagedRoleLabel: runner.options.Role, podman.ManagedLaunchLabel: runner.options.LaunchID}
	switch command.Args[0] {
	case "create":
		lock, err := storage.AcquireMutationLock(runner.options.DataRoot)
		if !errors.Is(err, storage.ErrMutationBusy) {
			if lock != nil {
				lock.Close()
			}
			runner.t.Fatalf("create has no mutation reservation: %v", err)
		}
		runner.created = true
		if runner.mode == "create-cancel" {
			runner.cancel()
			return process.Result{Status: -1}, nil
		}
		if runner.mode == "create-fail" {
			return process.Result{Status: 125, Stderr: []byte("create failed")}, nil
		}
		if runner.mode == "malformed-id" {
			return process.Result{Stdout: []byte("not-an-id")}, nil
		}
		return process.Result{Stdout: []byte(id)}, nil
	case "ps":
		if runner.created && !runner.removed {
			return process.Result{Stdout: []byte("fixture")}, nil
		}
		return process.Result{}, nil
	case "inspect":
		if runner.mode == "foreign" {
			labels[podman.ManagedLaunchLabel] = strings.Repeat("b", 32)
		}
		value, _ := json.Marshal(labels)
		return process.Result{Stdout: value}, nil
	case "container":
		if runner.removed || !runner.created {
			return process.Result{Status: 1}, nil
		}
		if command.Args[1] == "inspect" {
			if runner.mode == "foreign" {
				labels[podman.ManagedLaunchLabel] = strings.Repeat("b", 32)
			}
			value, _ := json.Marshal([]any{map[string]any{"Id": id, "Name": "fixture", "Config": map[string]any{"Labels": labels}}})
			return process.Result{Stdout: value}, nil
		}
		return process.Result{}, nil
	case "start":
		if command.Args[len(command.Args)-1] != id {
			runner.t.Fatalf("start did not use immutable ID: %v", command.Args)
		}
		// A lifetime reservation would prevent ordinary content installs while
		// a server or shell runs. Prove this is a short admission reservation.
		if err := content.WithMutation(ctx, runner.options.DataRoot, func() error { return nil }); err != nil {
			runner.t.Fatalf("start retained admission: %v", err)
		}
		if runner.start != nil {
			runner.start(command)
		}
		if runner.mode == "start-cancel" {
			runner.cancel()
			return process.Result{Status: -1}, nil
		}
		if runner.mode == "start-fail" {
			return process.Result{Status: 7, Stderr: []byte("workload failed")}, nil
		}
		return process.Result{Stdout: []byte("workload output")}, nil
	case "rm":
		if ctx.Err() != nil || command.Args[len(command.Args)-1] != id {
			runner.t.Fatalf("unsafe removal: %v ctx=%v", command.Args, ctx.Err())
		}
		runner.removed = true
		return process.Result{}, nil
	}
	return process.Result{}, fmt.Errorf("unexpected command %v", command.Args)
}

func admissionOptions(t *testing.T, flags ...string) LaunchOptions {
	t.Helper()
	command := append([]string{"podman", "run", "--name", "fixture", "--rm", "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges"}, flags...)
	command = append(command, "fixture-image", "--prompt", "--detach", "--label", "opaque workload")
	command, token, err := podman.WithLaunchIdentity(command)
	if err != nil {
		t.Fatal(err)
	}
	return LaunchOptions{DataRoot: t.TempDir(), Application: "comfyui", Role: "application", Image: "fixture-image", Command: command, LaunchID: token}
}

func TestLaunchAdmissionAndAttachedDetachedShellSemantics(t *testing.T) {
	for _, flags := range [][]string{nil, {"--detach"}, {"-it"}, {"--interactive", "--tty"}} {
		t.Run(strings.Join(flags, ","), func(t *testing.T) {
			options := admissionOptions(t, flags...)
			var stdin, stdout, stderr bytes.Buffer
			options.Streams = podman.Streams{Stdin: &stdin, Stdout: &stdout, Stderr: &stderr}
			prepared := false
			options.Prepare = func() error {
				prepared = true
				if lock, err := storage.AcquireMutationLock(options.DataRoot); !errors.Is(err, storage.ErrMutationBusy) {
					if lock != nil {
						lock.Close()
					}
					t.Fatalf("prepare without admission: %v", err)
				}
				return nil
			}
			runner := &admissionRunner{t: t, options: options, start: func(command process.Command) {
				detached := len(flags) > 0 && flags[0] == "--detach"
				interactive := len(flags) > 0 && !detached
				want := []string{"start"}
				if !detached {
					want = append(want, "--attach")
				}
				if interactive {
					want = append(want, "--interactive")
				}
				want = append(want, strings.Repeat("a", 64))
				if !reflect.DeepEqual(command.Args, want) || command.Stdin != &stdin || command.Stdout != &stdout || command.Stderr != &stderr {
					t.Fatalf("start streams/flags changed: %#v want %v", command, want)
				}
			}}
			if _, err := Launch(context.Background(), runner, options); err != nil {
				t.Fatal(err)
			}
			if !prepared || runner.removed {
				t.Fatalf("prepared=%v removed=%v", prepared, runner.removed)
			}
			created := runner.commands[0]
			if created.Stdout != nil || created.Stderr != &stderr {
				t.Fatal("create ID leaked or stderr hidden")
			}
			image := -1
			for index, argument := range created.Args {
				if argument == options.Image {
					image = index
					break
				}
			}
			if image < 0 || !reflect.DeepEqual(created.Args[image+1:], []string{"--prompt", "--detach", "--label", "opaque workload"}) {
				t.Fatalf("opaque workload changed: %v", created.Args)
			}
			for _, argument := range created.Args[:image] {
				if argument == "--detach" {
					t.Fatal("run-only detach passed to create")
				}
			}
		})
	}
}

func TestLaunchBlockedBeforeAnyPreparationOrPodman(t *testing.T) {
	options := admissionOptions(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(options.DataRoot, alias); err != nil {
		t.Fatal(err)
	}
	lock, err := storage.AcquireMutationLock(options.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for _, root := range []string{options.DataRoot, alias} {
		options.DataRoot = root
		options.Prepare = func() error { t.Fatal("blocked launch prepared data"); return nil }
		runner := &admissionRunner{t: t, options: options}
		if _, err := Launch(context.Background(), runner, options); !errors.Is(err, storage.ErrMutationBusy) {
			t.Fatalf("launch crossed admission: %v", err)
		}
		if len(runner.commands) != 0 {
			t.Fatalf("blocked launch ran Podman: %v", runner.commands)
		}
		if _, err := os.Stat(filepath.Join(root, "apps")); !os.IsNotExist(err) {
			t.Fatalf("blocked launch created data: %v", err)
		}
	}
}

func TestLaunchFailureCleanupAndReservationRelease(t *testing.T) {
	for _, mode := range []string{"create-fail", "create-cancel", "malformed-id", "foreign", "start-fail", "start-cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			options := admissionOptions(t)
			runner := &admissionRunner{t: t, options: options, mode: mode, cancel: cancel}
			_, err := Launch(ctx, runner, options)
			if err == nil || runner.removed != (mode != "foreign") {
				t.Fatalf("err=%v removed=%v", err, runner.removed)
			}
			if strings.HasSuffix(mode, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if mode == "start-fail" {
				var status *controlerr.Error
				if !errors.As(err, &status) || status.Status != 7 || !strings.Contains(err.Error(), "workload failed") {
					t.Fatalf("lost status: %v", err)
				}
			}
			lock, lockErr := storage.AcquireMutationLock(options.DataRoot)
			if lockErr != nil {
				t.Fatal(lockErr)
			}
			lock.Close()
		})
	}
}

func TestLaunchRejectsInvalidPlanBeforeMutation(t *testing.T) {
	for _, mode := range []string{"image", "nonce", "option"} {
		t.Run(mode, func(t *testing.T) {
			options := admissionOptions(t)
			options.DataRoot = filepath.Join(t.TempDir(), "absent")
			switch mode {
			case "image":
				options.Image = "different"
			case "nonce":
				options.LaunchID = strings.Repeat("0", 32)
			case "option":
				options.Command = append(options.Command[:4:4], append([]string{"--unknown"}, options.Command[4:]...)...)
			}
			runner := &admissionRunner{t: t, options: options}
			if _, err := Launch(context.Background(), runner, options); err == nil {
				t.Fatal("invalid launch accepted")
			}
			if len(runner.commands) != 0 {
				t.Fatal("invalid launch called Podman")
			}
			if _, err := os.Stat(options.DataRoot); !os.IsNotExist(err) {
				t.Fatalf("invalid plan prepared root: %v", err)
			}
		})
	}
}
