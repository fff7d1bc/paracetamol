package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

func TestComfyExecutionErrorReturnsThroughManagedCleanup(t *testing.T) {
	root := gatewayOwnershipHome(t)
	managed, bundle, source, data := comfyOwnershipFixture(t, root)
	port := benchmarkOwnershipPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var polls atomic.Int32
	registered, removed := false, false
	launch := ""
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
			if !registered {
				return process.Result{Status: 1}
			}
			if command.Args[1] == "inspect" {
				return process.Result{Stdout: fixtureRunningContainerJSON(t, fixtureContainerID, comfyBenchmarkContainer, fixtureContainerLabels("comfyui", "benchmark", launch))}
			}
			return process.Result{}
		case "start":
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/system_stats", func(output http.ResponseWriter, request *http.Request) {
				fmt.Fprint(output, `{"system":{"os":"fixture"}}`)
			})
			mux.HandleFunc("/prompt", func(output http.ResponseWriter, request *http.Request) {
				fmt.Fprint(output, `{"prompt_id":"fixture-prompt"}`)
			})
			mux.HandleFunc("/history/fixture-prompt", func(output http.ResponseWriter, request *http.Request) {
				if polls.Add(1) > 1 {
					cancel() // Bound the old infinite-poll failure in this fixture.
				}
				fmt.Fprint(output, `{"fixture-prompt":{"status":{"status_str":"error","completed":false,"messages":[["execution_error",{"exception_type":"OutOfMemoryError","exception_message":"fixture allocation failed"}]]}}}`)
			})
			server := &http.Server{Handler: mux}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			return process.Result{}
		case "inspect":
			labels, err := json.Marshal(fixtureContainerLabels("comfyui", "benchmark", launch))
			if err != nil {
				t.Fatal(err)
			}
			return process.Result{Stdout: labels}
		case "rm":
			if command.Args[len(command.Args)-1] != fixtureContainerID {
				t.Fatalf("cleanup did not use exact ID: %v", command.Args)
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
	path, _, err := app.executeComfyBenchmark(managed, bundle, comfyBenchmarkOptions{profile: "strix-halo", dataRoot: data, image: "fixture-image", renderNode: "/dev/dri/renderD999999", port: port, runs: 1, seed: 42, memoryPolicy: "balanced", kernelPolicy: "default", cacheMode: "isolated"}, "20260820T010203Z-abcdef12")
	if err == nil || !strings.Contains(err.Error(), "OutOfMemoryError: fixture allocation failed") || !removed || registered || path != "" || polls.Load() != 1 {
		t.Fatalf("path=%q err=%v removed=%t registered=%t", path, err, removed, registered)
	}
	cache := data + "/apps/comfyui/benchmarks/.cache/20260820T010203Z-abcdef12-fixture"
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("failed job retained isolated cache: %v", err)
	}
}
