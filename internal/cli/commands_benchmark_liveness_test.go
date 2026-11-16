package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paracetamol/internal/gateway"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

func fixtureRunningContainerJSON(t *testing.T, id, name string, labels map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal([]any{map[string]any{"Id": id, "Name": name, "Config": map[string]any{"Labels": labels}, "State": map[string]any{"Running": true}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBenchmarkWaitDetectsDeadCapturedBackendAndReleasesLifecycle(t *testing.T) {
	for _, application := range []string{"comfyui", "llama-cpp"} {
		for _, outcome := range []string{"missing with reused name", "exited", "changed launch", "cleanup failure", "prompt exited"} {
			if outcome == "prompt exited" && application != "comfyui" {
				continue
			}
			t.Run(application+"/"+outcome, func(t *testing.T) {
				root := gatewayOwnershipHome(t)
				managed, bundle, source, data := comfyOwnershipFixture(t, root)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel() // A test bound, not a production loading deadline.
				name := comfyBenchmarkContainer
				if application == "llama-cpp" {
					name = speculativeBenchmarkContainer
				}
				port := benchmarkOwnershipPort(t)
				registered, started, removed, launch := false, false, false, ""
				var historyPolls atomic.Int32
				var promptExited atomic.Bool
				runner := &commandRunner{run: func(command process.Command) process.Result {
					switch command.Args[0] {
					case "info":
						return process.Result{Stdout: []byte("true")}
					case "image":
						return process.Result{Stdout: []byte("sha256:fixture")}
					case "create":
						registered = true
						launch = strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
						return process.Result{Stdout: []byte(fixtureContainerID)}
					case "container":
						if started && command.Args[len(command.Args)-1] != fixtureContainerID {
							t.Fatalf("liveness/cleanup inspected a reusable name: %v", command.Args)
						}
						if !registered || (started && outcome == "missing with reused name") {
							return process.Result{Status: 1}
						}
						if command.Args[1] == "inspect" {
							labels := fixtureContainerLabels(application, "benchmark", launch)
							if started && outcome == "changed launch" {
								labels[podman.ManagedLaunchLabel] = strings.Repeat("f", 32)
							}
							if !started || (outcome == "prompt exited" && !promptExited.Load()) {
								return process.Result{Stdout: fixtureRunningContainerJSON(t, fixtureContainerID, name, labels)}
							}
							body, _ := json.Marshal([]any{map[string]any{"Id": fixtureContainerID, "Name": "renamed-original", "Config": map[string]any{"Labels": labels}, "State": map[string]any{"Running": false, "ExitCode": 23}}})
							return process.Result{Stdout: body}
						}
						return process.Result{}
					case "start":
						started = true
						if outcome == "prompt exited" {
							listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
							if err != nil {
								t.Fatal(err)
							}
							server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								switch r.URL.Path {
								case "/system_stats":
									fmt.Fprint(w, `{}`)
								case "/prompt":
									fmt.Fprint(w, `{"prompt_id":"queued"}`)
								case "/history/queued":
									historyPolls.Add(1)
									promptExited.Store(true)
									fmt.Fprint(w, `{}`)
								default:
									t.Errorf("unexpected fixture URL %s", r.URL.Path)
								}
							})}
							t.Cleanup(func() { _ = server.Close() })
							go func() { _ = server.Serve(listener) }()
						}
						return process.Result{}
					case "rm":
						if command.Args[len(command.Args)-1] != fixtureContainerID {
							t.Fatalf("cleanup removed a reusable name: %v", command.Args)
						}
						if outcome == "cleanup failure" {
							return process.Result{Status: 125, Stderr: []byte("fixture removal failed")}
						}
						registered, removed = false, true
						return process.Result{}
					default:
						t.Fatalf("unexpected fixture subprocess %v", command.Args)
						return process.Result{}
					}
				}}
				app, _, _ := testApp(t, runner)
				app.Root, app.Context = source, ctx
				app.Environment = map[string]string{"HOME": root}
				run := func() error {
					lease, err := gateway.AcquireLocalOperation(app.Environment, "fixture maintenance")
					if err != nil {
						return err
					}
					defer lease.Close()
					if application == "comfyui" {
						_, _, err = app.executeComfyBenchmark(managed, bundle, comfyBenchmarkOptions{profile: "strix-halo", dataRoot: data, image: "fixture-image", renderNode: "/dev/dri/renderD999999", port: port, runs: 1, seed: 42, memoryPolicy: "balanced", kernelPolicy: "default", cacheMode: "isolated"}, "20260820T010203Z-abcdef12")
					} else {
						_, err = app.executeSpeculativeTrial([]string{"podman", "run", "--name", name, "fixture-image"}, port, map[string]any{"fixture": true}, data, "fixture-image")
					}
					return err
				}
				err := run()
				if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					t.Fatalf("dead backend did not fail independently of cancellation: %v", err)
				}
				if outcome == "cleanup failure" && (!strings.Contains(err.Error(), "status 23") || !strings.Contains(err.Error(), "fixture removal failed")) {
					t.Fatalf("lost backend or cleanup failure: %v", err)
				}
				wantRemoved := outcome == "exited" || outcome == "prompt exited"
				if removed != wantRemoved || (outcome == "prompt exited" && historyPolls.Load() != 1) {
					t.Fatalf("removed=%t history polls=%d err=%v", removed, historyPolls.Load(), err)
				}
				lease, leaseErr := gateway.AcquireLocalOperation(app.Environment, "after failed maintenance")
				if leaseErr != nil {
					t.Fatalf("failure retained lifecycle lease: %v", leaseErr)
				}
				_ = lease.Close()
				cache := filepath.Join(data, "apps", "comfyui", "benchmarks", ".cache", "20260820T010203Z-abcdef12-fixture")
				if _, statErr := os.Stat(cache); !os.IsNotExist(statErr) {
					t.Fatalf("failure retained isolated cache: %v", statErr)
				}
			})
		}
	}
}
