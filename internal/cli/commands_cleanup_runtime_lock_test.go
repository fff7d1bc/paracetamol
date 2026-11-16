package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
)

func TestRuntimeCannotMutateDuringCleanupApproval(t *testing.T) {
	for _, scope := range []string{"data", "caches", "downloads"} {
		t.Run(scope, func(t *testing.T) {
			base := gatewayOwnershipHome(t)
			root := filepath.Join(base, "data")
			writeOwnershipSentinel(t, filepath.Join(cleanupInstallTarget(root, scope), "sentinel"))
			cleanup, _, _ := testApp(t, &commandRunner{run: gatewayOwnershipPodman})
			cleanup.Environment = map[string]string{"HOME": base}
			cleanup.Stdin = preflightTerminalReader{Reader: strings.NewReader("yes\n")}
			writer := &cleanupInstallPauseWriter{match: "Proceed with", entered: make(chan struct{}), release: make(chan struct{})}
			cleanup.Stdout = writer
			done := make(chan error, 1)
			go func() { done <- cleanup.commandCleanup([]string{scope, "--data-dir", root}) }()
			defer func() {
				close(writer.release)
				if err := <-done; err != nil {
					t.Errorf("cleanup: %v", err)
				}
			}()
			waitCleanupInstallPause(t, writer)
			for _, shape := range []string{"real", "root-alias", "ancestor-alias"} {
				selected := cleanupInstallSelection(t, base, root, shape)
				for _, mode := range []string{"comfyui", "shell-comfyui", "shell-llama-cpp"} {
					runner := &commandRunner{run: gatewayOwnershipPodman}
					app, _, _ := testApp(t, runner)
					app.Environment = cleanup.Environment
					args := []string{"run", "comfyui", "--profile", "cpu", "--detach", "--data-dir", selected}
					if strings.HasPrefix(mode, "shell-") {
						args = []string{"shell", strings.TrimPrefix(mode, "shell-"), "--data-dir", selected}
					}
					if err := app.Dispatch(args); !errors.Is(err, storage.ErrMutationBusy) {
						t.Fatalf("%s through %s crossed cleanup admission: %v", mode, shape, err)
					}
					for _, command := range runner.commands {
						if len(command.Args) > 0 && (command.Args[0] == "run" || command.Args[0] == "create" || command.Args[0] == "start") {
							t.Fatalf("blocked runtime started workload: %v", command.Args)
						}
					}
				}
				if shape != "real" {
					if err := os.Remove(filepath.Join(base, "alias")); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := os.Stat((storage.Layout{Root: root}).Application("comfyui")); !os.IsNotExist(err) {
				t.Fatalf("blocked runtime prepared application data: %v", err)
			}
		})
	}
}

func TestPublishedRuntimeRemainsVisibleToCleanupWithoutBlockingContent(t *testing.T) {
	for _, mode := range []string{"comfyui", "shell-comfyui", "shell-llama-cpp"} {
		t.Run(mode, func(t *testing.T) {
			base := gatewayOwnershipHome(t)
			root := filepath.Join(base, "data")
			application, role := "comfyui", "application"
			if strings.HasPrefix(mode, "shell-") {
				application, role = strings.TrimPrefix(mode, "shell-"), "shell"
			}
			spec, _ := config.ApplicationByID(application)
			registered, launch := false, ""
			var app *App
			runner := &commandRunner{run: func(command process.Command) process.Result {
				switch command.Args[0] {
				case "info":
					return process.Result{Stdout: []byte("true")}
				case "image":
					return process.Result{}
				case "ps":
					if registered {
						return process.Result{Stdout: []byte(spec.ContainerName)}
					}
					return process.Result{}
				case "container":
					if !registered {
						return process.Result{Status: 1}
					}
					if command.Args[1] == "inspect" {
						return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, spec.ContainerName, fixtureContainerLabels(application, role, launch))}
					}
					return process.Result{}
				case "create":
					for _, scope := range []string{"data", "caches", "downloads"} {
						cleanup, _, _ := testApp(t, app.Runner)
						cleanup.Environment = app.Environment
						if err := cleanup.commandCleanup([]string{scope, "--data-dir", root, "--yes"}); !errors.Is(err, storage.ErrMutationBusy) {
							t.Fatalf("cleanup entered unregistered %s launch: %v", mode, err)
						}
					}
					registered = true
					launch = strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
					return process.Result{Stdout: []byte(fixtureContainerID)}
				case "start":
					if err := content.WithMutation(app.Context, root, func() error { return nil }); err != nil {
						t.Fatalf("runtime held root during workload: %v", err)
					}
					for _, scope := range []string{"data", "caches", "downloads"} {
						cleanup, _, _ := testApp(t, app.Runner)
						cleanup.Environment = app.Environment
						if err := cleanup.commandCleanup([]string{scope, "--data-dir", root, "--yes"}); err == nil || !strings.Contains(err.Error(), "managed containers are still present") {
							t.Fatalf("published runtime invisible to %s: %v", scope, err)
						}
					}
					return process.Result{}
				default:
					t.Fatalf("unexpected fixture command %v", command.Args)
				}
				return process.Result{}
			}}
			app, _, _ = testApp(t, runner)
			app.Environment = map[string]string{"HOME": base}
			args := []string{"run", "comfyui", "--profile", "cpu", "--detach", "--data-dir", root}
			if role == "shell" {
				args = []string{"shell", application, "--data-dir", root}
			}
			if err := app.Dispatch(args); err != nil {
				t.Fatal(err)
			}
			if !registered {
				t.Fatal("fixture never published runtime")
			}
		})
	}
}
