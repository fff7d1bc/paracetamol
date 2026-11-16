package cli

import (
	"fmt"
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
	// Keep the HOME-anchored Unix socket below sockaddr_un's path limit.
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
