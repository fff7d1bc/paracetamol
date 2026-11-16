package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/gateway"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/runtime"
)

func TestThroughputMaintenanceAdmissionRejectsGatewayButAllowsDryRun(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "execute", true: "dry-run"}[dryRun], func(t *testing.T) {
			base := gatewayOwnershipHome(t)
			data := filepath.Join(base, "absent-data")
			model := filepath.Join(base, "fixture.gguf")
			if err := os.WriteFile(model, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			environment := map[string]string{"HOME": base}
			control, err := gateway.StartLocalControl(environment)
			if err != nil {
				t.Fatal(err)
			}
			defer control.Close()
			defer control.Finish(nil)
			runner := &commandRunner{run: func(command process.Command) process.Result {
				switch command.Args[0] {
				case "info":
					return process.Result{Stdout: []byte("true")}
				case "image":
					return process.Result{Stdout: []byte("sha256:fixture")}
				default:
					t.Fatalf("blocked maintenance reached a workload or mutation: %v", command.Args)
					return process.Result{}
				}
			}}
			app, stdout, _ := testApp(t, runner)
			app.Environment, app.catalog = environment, &catalog.Catalog{}
			args := []string{"--model", model, "--profile", "cpu", "--data-dir", data}
			if dryRun {
				args = append(args, "--dry-run")
			}
			err = app.benchmarkLlama(args)
			if dryRun {
				if err != nil || !strings.Contains(stdout.String(), "No container was started.") || len(runner.commands) != 0 {
					t.Fatalf("dry run competed for maintenance ownership: err=%v commands=%v output=%q", err, runner.commands, stdout.String())
				}
			} else if err == nil || !strings.Contains(err.Error(), "already running") {
				t.Fatalf("live gateway did not reject maintenance: %v", err)
			}
			if _, err := os.Lstat(data); !os.IsNotExist(err) {
				t.Fatalf("rejected/dry maintenance prepared persistent data: %v", err)
			}
			assertGatewayOwnershipHeld(t, environment)
			select {
			case <-control.Requested():
				t.Fatal("maintenance requested gateway shutdown")
			default:
			}
		})
	}
}

func TestThroughputMaintenanceRetainsOwnershipAfterAutoRemovalAndReleasesEveryOutcome(t *testing.T) {
	for _, outcome := range []string{"success", "workload-failure", "invalid-json", "publication-failure"} {
		t.Run(outcome, func(t *testing.T) {
			base := gatewayOwnershipHome(t)
			data, resultPath := filepath.Join(base, "data"), filepath.Join(base, "result.json")
			model := filepath.Join(base, "fixture.gguf")
			if err := os.WriteFile(model, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			environment := map[string]string{"HOME": base}
			registered, started, published := false, false, false
			launch := ""
			var app *App
			runner := &commandRunner{run: func(command process.Command) process.Result {
				switch command.Args[0] {
				case "info":
					return process.Result{Stdout: []byte("true")}
				case "image":
					return process.Result{Stdout: []byte("sha256:fixture")}
				case "create":
					assertGatewayOwnershipHeld(t, environment)
					registered = true
					launch = strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
					return process.Result{Stdout: []byte(fixtureContainerID)}
				case "container":
					if !registered {
						return process.Result{Status: 1}
					}
					if command.Args[1] == "inspect" {
						return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, runtime.LlamaBenchmarkContainer, fixtureContainerLabels("llama-cpp", "benchmark", launch))}
					}
					return process.Result{}
				case "start":
					assertGatewayOwnershipHeld(t, environment)
					if err := content.WithMutation(app.Context, data, func() error { return nil }); err != nil {
						t.Fatalf("maintenance held the data lock during a workload: %v", err)
					}
					started = true
					// Model --rm completion: no container remains to make the
					// cleanup inventory protect host-side publication.
					registered = false
					switch outcome {
					case "workload-failure":
						return process.Result{Status: 7, Stderr: []byte("fixture workload failed")}
					case "invalid-json":
						return process.Result{Stdout: []byte("invalid fixture JSON")}
					case "publication-failure":
						if err := os.WriteFile(resultPath, []byte("preserve existing result"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					return process.Result{Stdout: []byte(`[{"n_prompt":32,"n_gen":0,"avg_ts":100},{"n_prompt":0,"n_gen":16,"avg_ts":10}]`)}
				default:
					t.Fatalf("unexpected maintenance fixture command: %v", command.Args)
					return process.Result{}
				}
			}}
			app, _, _ = testApp(t, runner)
			app.Environment, app.catalog = environment, &catalog.Catalog{}
			output := &strings.Builder{}
			app.Stdout = ownershipHookWriter{Writer: output, hook: func(value string) {
				if !strings.Contains(value, "Benchmark complete") {
					return
				}
				published = true
				if registered {
					t.Fatal("fixture did not auto-remove its completed trial")
				}
				assertGatewayOwnershipHeld(t, environment)
				if _, err := os.Stat(resultPath); err != nil {
					t.Fatalf("completion did not publish its result: %v", err)
				}
				for _, scope := range []string{"data", "caches", "downloads"} {
					cleanupRunner := &commandRunner{run: func(command process.Command) process.Result {
						t.Fatalf("cleanup crossed maintenance ownership with no container: %v", command.Args)
						return process.Result{}
					}}
					cleanup, _, _ := testApp(t, cleanupRunner)
					cleanup.Environment = environment
					if err := cleanup.commandCleanup([]string{scope, "--data-dir", data, "--yes"}); err == nil || !strings.Contains(err.Error(), "already running") {
						t.Fatalf("%s crossed post-container maintenance ownership: %v", scope, err)
					}
				}
			}}
			err := app.benchmarkLlama([]string{"--model", model, "--profile", "cpu", "--data-dir", data, "--output", resultPath, "--prompt-tokens", "32", "--generation-tokens", "16"})
			if (err != nil) != (outcome != "success") || !started || published != (outcome == "success") {
				t.Fatalf("outcome=%s err=%v started=%t published=%t output=%q", outcome, err, started, published, output.String())
			}
			assertGatewayOwnershipReleased(t, environment)
			if outcome == "publication-failure" {
				if contents, readErr := os.ReadFile(resultPath); readErr != nil || string(contents) != "preserve existing result" {
					t.Fatalf("publication failure replaced incumbent: %q %v", contents, readErr)
				}
			}
		})
	}
}
