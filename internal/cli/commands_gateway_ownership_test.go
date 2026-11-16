package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/gateway"
	"paracetamol/internal/identity"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
	"paracetamol/internal/verification"
)

func gatewayOwnershipHome(t *testing.T) string {
	t.Helper()
	// Keep all ownership-test state below the repository's ignored build tree.
	build := filepath.Join("..", "..", "build")
	if err := os.MkdirAll(build, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(build, "gw-")
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(absolute) })
	return absolute
}

// All lifecycle fixtures use fake Podman responses; no test starts a workload.
func gatewayOwnershipPodman(command process.Command) process.Result {
	if command.Name != "podman" || len(command.Args) == 0 {
		return process.Result{}
	}
	switch command.Args[0] {
	case "info":
		return process.Result{Stdout: []byte("true\n")}
	case "container":
		return process.Result{Status: 1}
	default:
		return process.Result{}
	}
}

func assertGatewayOwnershipHeld(t *testing.T, environment map[string]string) {
	t.Helper()
	if err := gatewayOwnershipUnavailable(environment); err != nil {
		t.Fatal(err)
	}
}

func gatewayOwnershipUnavailable(environment map[string]string) error {
	control, err := gateway.StartLocalControl(environment)
	if err == nil {
		control.Finish(nil)
		_ = control.Close()
		return fmt.Errorf("gateway acquired ownership during a protected operation")
	}
	if !strings.Contains(err.Error(), "already running") {
		return fmt.Errorf("unexpected ownership failure: %w", err)
	}
	return nil
}

func assertGatewayOwnershipReleased(t *testing.T, environment map[string]string) {
	t.Helper()
	control, err := gateway.StartLocalControl(environment)
	if err != nil {
		t.Fatalf("operation did not release ownership: %v", err)
	}
	control.Finish(nil)
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeOwnershipSentinel(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("must survive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLiveGatewayRejectsCleanupBeforePlanningOrMutation(t *testing.T) {
	for _, resident := range []bool{false, true} {
		for _, scope := range []string{"data", "caches", "downloads", "containers", "images"} {
			for _, application := range []string{"all", "llama-cpp"} {
				if application != "all" && scope != "containers" && scope != "images" {
					continue
				}
				t.Run(fmt.Sprintf("resident=%t/%s/%s", resident, scope, application), func(t *testing.T) {
					home := gatewayOwnershipHome(t)
					environment := map[string]string{"HOME": home}
					// The default root used to contain filesystem ownership state;
					// confirmed data cleanup must not remove a live owner's ancestor.
					root := filepath.Join(home, ".local", "share", identity.StateNamespace)
					sentinel := filepath.Join(root, "apps", "llama-cpp", "cache", "sentinel")
					writeOwnershipSentinel(t, sentinel)
					writeOwnershipSentinel(t, filepath.Join(root, "staging", "sentinel"))
					control, err := gateway.StartLocalControl(environment)
					if err != nil {
						t.Fatal(err)
					}
					defer control.Close()
					defer control.Finish(nil)
					runner := &commandRunner{run: func(command process.Command) process.Result {
						if len(command.Args) > 0 && command.Args[0] == "ps" && resident {
							return process.Result{Stdout: []byte(identity.Container("gateway-llama-cpp") + "\n")}
						}
						return gatewayOwnershipPodman(command)
					}}
					app, stdout, _ := testApp(t, runner)
					app.Environment = environment
					args := []string{scope, "--yes"}
					if scope == "containers" || scope == "images" {
						args = append(args, application)
					}
					if err := app.commandCleanup(args); err == nil || !strings.Contains(err.Error(), "already running") {
						t.Fatalf("cleanup ownership error = %v", err)
					}
					if len(runner.commands) != 0 || stdout.Len() != 0 {
						t.Fatalf("cleanup reached planning: commands=%v output=%q", runner.commands, stdout.String())
					}
					if actual, err := os.ReadFile(sentinel); err != nil || string(actual) != "must survive\n" {
						t.Fatalf("cleanup changed persistent bytes: %q, %v", actual, err)
					}
					assertGatewayOwnershipHeld(t, environment)
					select {
					case <-control.Requested():
						t.Fatal("cleanup requested gateway shutdown")
					default:
					}
				})
			}
		}
	}
}

type ownershipHookWriter struct {
	io.Writer
	hook func(string)
}

func (writer ownershipHookWriter) Write(value []byte) (int, error) {
	writer.hook(string(value))
	return writer.Writer.Write(value)
}

type ownershipPromptReader struct {
	io.Reader
	environment map[string]string
}

func (ownershipPromptReader) IsTerminal() bool { return true }

func (reader ownershipPromptReader) Read(value []byte) (int, error) {
	// Probe while the command is waiting for an answer, before providing it.
	if err := gatewayOwnershipUnavailable(reader.environment); err != nil {
		return 0, err
	}
	return reader.Reader.Read(value)
}

func TestCleanupRetainsOwnershipThroughChecksConfirmationAndRemoval(t *testing.T) {
	for _, scope := range []string{"data", "caches", "downloads", "containers", "images"} {
		t.Run(scope, func(t *testing.T) {
			home := gatewayOwnershipHome(t)
			environment := map[string]string{"HOME": home}
			root := filepath.Join(home, "data")
			partition := root
			if scope == "caches" {
				partition = filepath.Join(root, "apps", "llama-cpp", "cache")
			} else if scope == "downloads" {
				partition = filepath.Join(root, "staging")
			}
			sentinel := filepath.Join(partition, "sentinel")
			writeOwnershipSentinel(t, sentinel)
			checked, prompted, removed := false, false, false
			runner := &commandRunner{run: func(command process.Command) process.Result {
				if command.Name == "podman" && len(command.Args) > 0 {
					switch command.Args[0] {
					case "ps":
						assertGatewayOwnershipHeld(t, environment)
						checked = true
						if scope == "containers" {
							return process.Result{Stdout: []byte("owned-fixture\n")}
						}
					case "rm", "rmi":
						assertGatewayOwnershipHeld(t, environment)
					}
				}
				return gatewayOwnershipPodman(command)
			}}
			app, stdout, _ := testApp(t, runner)
			app.Environment = environment
			app.Stdin = ownershipPromptReader{Reader: strings.NewReader("yes\n"), environment: environment}
			app.Stdout = ownershipHookWriter{Writer: stdout, hook: func(value string) {
				if strings.Contains(value, "Proceed with") {
					assertGatewayOwnershipHeld(t, environment)
					prompted = true
					if _, err := os.Stat(sentinel); err != nil {
						t.Fatalf("persistent bytes changed before confirmation: %v", err)
					}
				}
				if strings.Contains(value, "Removed") {
					assertGatewayOwnershipHeld(t, environment)
					removed = true
					if scope != "containers" && scope != "images" {
						if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
							t.Fatalf("cleanup did not remove its selected bytes: %v", err)
						}
					}
				}
			}}
			args := []string{scope}
			if scope == "containers" || scope == "images" {
				args = append(args, "llama-cpp")
			} else {
				args = append(args, "--data-dir", root)
			}
			if err := app.commandCleanup(args); err != nil {
				t.Fatal(err)
			}
			if !checked || !prompted || !removed {
				t.Fatalf("missing lifecycle boundary: checked=%t prompted=%t removed=%t", checked, prompted, removed)
			}
			assertGatewayOwnershipReleased(t, environment)
		})
	}
}

func TestUnrelatedCleanupDoesNotRequireGatewayOwnership(t *testing.T) {
	for _, scope := range []string{"build-cache", "containers", "images"} {
		t.Run(scope, func(t *testing.T) {
			home := gatewayOwnershipHome(t)
			environment := map[string]string{"HOME": home}
			control, err := gateway.StartLocalControl(environment)
			if err != nil {
				t.Fatal(err)
			}
			defer control.Close()
			defer control.Finish(nil)
			app, _, _ := testApp(t, &commandRunner{run: gatewayOwnershipPodman})
			app.Environment = environment
			args := []string{scope, "--yes"}
			if scope != "build-cache" {
				args = append(args, "comfyui")
			} else {
				writeOwnershipSentinel(t, filepath.Join(home, ".cache", identity.StateNamespace, "build", "pip", "sentinel"))
			}
			if err := app.commandCleanup(args); err != nil {
				t.Fatalf("unrelated cleanup rejected a live gateway: %v", err)
			}
		})
	}
}

func TestDirectLlamaRejectsGatewayBeforePersistentMutation(t *testing.T) {
	for _, mode := range []string{"server", "cli"} {
		t.Run(mode, func(t *testing.T) {
			home := gatewayOwnershipHome(t)
			root := filepath.Join(home, "data")
			managed := gatewayOwnershipCatalog(t, root)
			router := filepath.Join((storage.Layout{Root: root}).Application("llama-cpp"), "models.ini")
			writeOwnershipSentinel(t, router)
			environment := map[string]string{"HOME": home}
			control, err := gateway.StartLocalControl(environment)
			if err != nil {
				t.Fatal(err)
			}
			defer control.Close()
			defer control.Finish(nil)
			runner := &commandRunner{run: gatewayOwnershipPodman}
			app, _, _ := testApp(t, runner)
			app.Environment, app.catalog = environment, &managed
			args := []string{"--profile", "cpu", "--data-dir", root}
			if mode == "server" {
				args = append(args, "--router")
			} else {
				args = append(args, "--preset", "fixture", "--prompt", "hello")
			}
			if err := app.runLlama(mode, args); err == nil || !strings.Contains(err.Error(), "already running") {
				t.Fatalf("direct run ownership error = %v", err)
			}
			for _, command := range runner.commands {
				if command.Name == "podman" {
					t.Fatalf("direct run reached Podman: %v", command)
				}
			}
			if actual, err := os.ReadFile(router); err != nil || string(actual) != "must survive\n" {
				t.Fatalf("direct run rewrote gateway router: %q %v", actual, err)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(router), "home")); !os.IsNotExist(err) {
				t.Fatalf("direct run prepared persistent runtime: %v", err)
			}
		})
	}
}

func TestDirectLlamaPreflightPreservesRouter(t *testing.T) {
	for _, failure := range []string{"rootful", "missing-image", "managed-container", "occupied-name"} {
		t.Run(failure, func(t *testing.T) {
			home := gatewayOwnershipHome(t)
			root := filepath.Join(home, "data")
			managed := gatewayOwnershipCatalog(t, root)
			router := filepath.Join((storage.Layout{Root: root}).Application("llama-cpp"), "models.ini")
			writeOwnershipSentinel(t, router)
			runner := &commandRunner{run: func(command process.Command) process.Result {
				if command.Name == "podman" && len(command.Args) > 0 {
					switch command.Args[0] {
					case "info":
						if failure == "rootful" {
							return process.Result{Stdout: []byte("false\n")}
						}
					case "image":
						if failure == "missing-image" {
							return process.Result{Status: 1}
						}
					case "ps":
						if failure == "managed-container" {
							return process.Result{Stdout: []byte("existing-llama\n")}
						}
					case "container":
						if failure == "occupied-name" {
							return process.Result{}
						}
					case "run":
						t.Fatal("failed preflight reached Podman run")
					}
				}
				return gatewayOwnershipPodman(command)
			}}
			app, _, _ := testApp(t, runner)
			app.Environment, app.catalog = map[string]string{"HOME": home}, &managed
			if err := app.runLlama("server", []string{"--router", "--profile", "cpu", "--data-dir", root}); err == nil {
				t.Fatal("failed preflight succeeded")
			}
			if actual, err := os.ReadFile(router); err != nil || string(actual) != "must survive\n" {
				t.Fatalf("failed preflight rewrote router: %q %v", actual, err)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(router), "home")); !os.IsNotExist(err) {
				t.Fatalf("failed preflight prepared persistent runtime: %v", err)
			}
			assertGatewayOwnershipReleased(t, app.Environment)
		})
	}
}

func TestDirectLlamaRetainsOwnershipThroughRunAndFailureCleanup(t *testing.T) {
	for _, mode := range []string{"server", "detached-server", "cli", "failed-cli"} {
		t.Run(mode, func(t *testing.T) {
			home := gatewayOwnershipHome(t)
			root := filepath.Join(home, "data")
			managed := gatewayOwnershipCatalog(t, root)
			environment := map[string]string{"HOME": home}
			ran, cleaned := false, false
			runner := &commandRunner{run: func(command process.Command) process.Result {
				if command.Name == "podman" && len(command.Args) > 0 {
					switch command.Args[0] {
					case "run":
						assertGatewayOwnershipHeld(t, environment)
						ran = true
						if mode == "failed-cli" {
							return process.Result{Status: 7}
						}
					case "rm":
						assertGatewayOwnershipHeld(t, environment)
						cleaned = true
					}
				}
				return gatewayOwnershipPodman(command)
			}}
			app, _, _ := testApp(t, runner)
			app.Environment, app.catalog = environment, &managed
			args := []string{"--preset", "fixture", "--profile", "cpu", "--data-dir", root}
			actualMode := "server"
			if strings.Contains(mode, "cli") {
				actualMode = "cli"
				args = append(args, "--prompt", "hello")
			} else if mode == "detached-server" {
				args = append(args, "--detach")
			}
			err := app.runLlama(actualMode, args)
			if (err != nil) != (mode == "failed-cli") {
				t.Fatalf("run result = %v", err)
			}
			if !ran || cleaned != (mode == "failed-cli") {
				t.Fatalf("wrong lifecycle: ran=%t cleaned=%t", ran, cleaned)
			}
			assertGatewayOwnershipReleased(t, environment)
		})
	}
}

func TestDirectLlamaDryRunDoesNotAcquireOwnershipOrPrepareState(t *testing.T) {
	home := gatewayOwnershipHome(t)
	root := filepath.Join(home, "data")
	managed := gatewayOwnershipCatalog(t, root)
	environment := map[string]string{"HOME": home}
	control, err := gateway.StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	defer control.Finish(nil)
	runner := &commandRunner{run: gatewayOwnershipPodman}
	app, _, _ := testApp(t, runner)
	app.Environment, app.catalog = environment, &managed
	if err := app.runLlama("server", []string{"--router", "--profile", "cpu", "--data-dir", root, "--dry-run"}); err != nil {
		t.Fatalf("dry run competed for gateway ownership: %v", err)
	}
	for _, command := range runner.commands {
		if command.Name == "podman" {
			t.Fatalf("dry run reached Podman: %v", command)
		}
	}
	if _, err := os.Stat((storage.Layout{Root: root}).Application("llama-cpp")); !os.IsNotExist(err) {
		t.Fatalf("dry run prepared application state: %v", err)
	}
}

func TestStopAllDuringAttachedDirectRunStillStopsContainer(t *testing.T) {
	home := gatewayOwnershipHome(t)
	root := filepath.Join(home, "data")
	managed := gatewayOwnershipCatalog(t, root)
	environment := map[string]string{"HOME": home}
	directContainer := identity.Container("llama-cpp")
	removed := false
	stopRunner := &commandRunner{run: func(command process.Command) process.Result {
		if command.Name == "podman" && len(command.Args) > 0 {
			if command.Args[0] == "container" && command.Args[len(command.Args)-1] == directContainer {
				return process.Result{}
			}
			if command.Args[0] == "rm" {
				assertGatewayOwnershipHeld(t, environment)
				removed = command.Args[len(command.Args)-1] == directContainer
			}
		}
		return gatewayOwnershipPodman(command)
	}}
	stopper, stdout, _ := testApp(t, stopRunner)
	stopper.Environment = environment
	runner := &commandRunner{run: func(command process.Command) process.Result {
		if command.Name == "podman" && len(command.Args) > 0 && command.Args[0] == "run" {
			assertGatewayOwnershipHeld(t, environment)
			if err := stopper.commandStop([]string{"all"}); err != nil {
				t.Fatalf("stop all failed during attached direct reservation: %v", err)
			}
			assertGatewayOwnershipHeld(t, environment)
		}
		return gatewayOwnershipPodman(command)
	}}
	app, _, _ := testApp(t, runner)
	app.Environment, app.catalog = environment, &managed
	if err := app.runLlama("server", []string{"--preset", "fixture", "--profile", "cpu", "--data-dir", root}); err != nil {
		t.Fatal(err)
	}
	if !removed || !strings.Contains(stdout.String(), "Gateway control socket not present.") {
		t.Fatalf("stop all did not continue past direct reservation: removed=%t output=%q", removed, stdout.String())
	}
	assertGatewayOwnershipReleased(t, environment)
}

func gatewayOwnershipCatalog(t *testing.T, root string) catalog.Catalog {
	t.Helper()
	artifact := catalog.Artifact{ID: "model", Target: "llama-models", Destination: "model.gguf", Size: 1, SHA256: strings.Repeat("0", 64)}
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{artifact.ID: artifact},
		Bundles: map[string]catalog.Bundle{"bundle": {
			ID: "bundle", Application: "llama-cpp", Artifacts: []string{artifact.ID},
		}},
		LlamaPresets: map[string]catalog.LlamaPreset{"fixture": {
			ID: "fixture", Bundle: "bundle", Artifact: artifact.ID, DefaultContext: 4096,
		}},
	}
	path := content.ArtifactPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(path, artifact.Size, artifact.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	return managed
}

func TestDuplicateGatewayCannotMutateIncumbentResources(t *testing.T) {
	for _, resident := range []bool{false, true} {
		for _, samePort := range []bool{false, true} {
			for _, sameRuntime := range []bool{false, true} {
				for _, sameData := range []bool{false, true} {
					name := fmt.Sprintf("resident=%t/same-port=%t/same-runtime=%t/same-data=%t", resident, samePort, sameRuntime, sameData)
					t.Run(name, func(t *testing.T) {
						home := gatewayOwnershipHome(t)
						environment := map[string]string{"HOME": home, "XDG_RUNTIME_DIR": filepath.Join(home, "runtime-a")}
						control, err := gateway.StartLocalControl(environment)
						if err != nil {
							t.Fatal(err)
						}
						defer control.Close()
						defer control.Finish(nil)
						listener, err := net.Listen("tcp4", "127.0.0.1:0")
						if err != nil {
							t.Fatal(err)
						}
						defer listener.Close()
						port := listener.Addr().(*net.TCPAddr).Port
						if !samePort {
							other, err := net.Listen("tcp4", "127.0.0.1:0")
							if err != nil {
								t.Fatal(err)
							}
							port = other.Addr().(*net.TCPAddr).Port
							if err := other.Close(); err != nil {
								t.Fatal(err)
							}
						}
						dataRoot := filepath.Join(home, "data-a")
						if !sameData {
							dataRoot = filepath.Join(home, "data-b")
						}
						managed := gatewayOwnershipCatalog(t, dataRoot)
						routerPath := filepath.Join((storage.Layout{Root: dataRoot}).Application("llama-cpp"), "models.ini")
						if err := os.MkdirAll(filepath.Dir(routerPath), 0o700); err != nil {
							t.Fatal(err)
						}
						const incumbentRouter = "incumbent-router-must-survive\n"
						if err := os.WriteFile(routerPath, []byte(incumbentRouter), 0o600); err != nil {
							t.Fatal(err)
						}
						backendName := identity.Container("gateway-llama-cpp")
						runner := &commandRunner{run: func(command process.Command) process.Result {
							switch {
							case len(command.Args) > 0 && command.Args[0] == "info":
								return process.Result{Stdout: []byte("true\n")}
							case len(command.Args) > 0 && command.Args[0] == "ps" && resident:
								return process.Result{Stdout: []byte(backendName + "\n")}
							case len(command.Args) > 0 && command.Args[0] == "container" && !resident:
								return process.Result{Status: 1}
							case len(command.Args) > 0 && command.Args[0] == "inspect":
								labels := fmt.Sprintf(`{"%s":"true","%s":"llama-cpp","%s":"%s","%s":"%s"}`, podman.ManagedContainerLabel, podman.ManagedApplicationLabel, podman.ManagedRoleLabel, gateway.GatewayBackendRole, gateway.GatewayInstanceLabel, strings.Repeat("b", 32))
								return process.Result{Stdout: []byte(labels)}
							default:
								return process.Result{}
							}
						}}
						app, _, _ := testApp(t, runner)
						app.catalog = &managed
						app.Environment = map[string]string{"HOME": home, "XDG_RUNTIME_DIR": environment["XDG_RUNTIME_DIR"]}
						if !sameRuntime {
							app.Environment["XDG_RUNTIME_DIR"] = filepath.Join(home, "runtime-b")
						}
						err = app.runGateway([]string{"--application", "llama-cpp", "--profile", "cpu", "--data-dir", dataRoot, "--port", strconv.Itoa(port)})
						if err == nil || !strings.Contains(err.Error(), "already running") {
							t.Fatalf("duplicate startup must fail at ownership before listener binding: %v", err)
						}
						if len(runner.commands) != 0 {
							t.Fatalf("duplicate startup reached lifecycle or source probes: %v", runner.commands)
						}
						contents, err := os.ReadFile(routerPath)
						if err != nil || string(contents) != incumbentRouter {
							t.Fatalf("duplicate startup rewrote router: contents=%q err=%v", contents, err)
						}
						if _, err := os.Lstat((storage.Layout{Root: dataRoot}).Application("gateway")); !os.IsNotExist(err) {
							t.Fatalf("duplicate startup prepared persistent gateway state: %v", err)
						}
						select {
						case <-control.Requested():
							t.Fatal("duplicate startup requested incumbent shutdown")
						default:
						}
					})
				}
			}
		}
	}
}
