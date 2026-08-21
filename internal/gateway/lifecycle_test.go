package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

type lifecycleRunner struct {
	mu           sync.Mutex
	port         int
	running      bool
	conflict     string
	commands     []process.Command
	logFollowers int
	logsStarted  chan struct{}
	logsOnce     sync.Once
}

func (runner *lifecycleRunner) LookPath(string) (string, error) { return "/usr/bin/podman", nil }

func (runner *lifecycleRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	runner.mu.Lock()
	runner.commands = append(runner.commands, command)
	arguments := strings.Join(command.Args, " ")
	if strings.HasPrefix(arguments, "logs --follow ") {
		runner.logFollowers++
		started := runner.logsStarted
		runner.mu.Unlock()
		if command.Stdout != nil {
			_, _ = io.WriteString(command.Stdout, "backend ready\n")
		}
		if started != nil {
			runner.logsOnce.Do(func() { close(started) })
		}
		<-ctx.Done()
		runner.mu.Lock()
		runner.logFollowers--
		runner.mu.Unlock()
		return process.Result{}, ctx.Err()
	}
	defer runner.mu.Unlock()
	switch {
	case arguments == "info --format {{.Host.Security.Rootless}}":
		return process.Result{Stdout: []byte("true\n")}, nil
	case strings.HasPrefix(arguments, "image exists "):
		return process.Result{}, nil
	case strings.HasPrefix(arguments, "ps --all "):
		return process.Result{Stdout: []byte(runner.conflict)}, nil
	case strings.HasPrefix(arguments, "container exists "):
		status := 1
		if runner.running {
			status = 0
		}
		return process.Result{Status: status}, nil
	case strings.HasPrefix(arguments, "run "):
		runner.running = true
		return process.Result{Stdout: []byte("container-id\n")}, nil
	case strings.HasPrefix(arguments, "port "):
		return process.Result{Stdout: []byte(fmt.Sprintf("127.0.0.1:%d\n", runner.port))}, nil
	case strings.HasPrefix(arguments, "inspect --format {{json .Config.Labels}} "):
		labels := fmt.Sprintf(`{"%s":"true","%s":"llama-cpp","%s":"%s"}`,
			podman.ManagedContainerLabel, podman.ManagedApplicationLabel, podman.ManagedRoleLabel, GatewayBackendRole)
		return process.Result{Stdout: []byte(labels)}, nil
	case strings.HasPrefix(arguments, "rm --force "):
		runner.running = false
		return process.Result{}, nil
	default:
		return process.Result{}, fmt.Errorf("unexpected command: %s %s", command.Name, arguments)
	}
}

func llamaLifecycleFixture(t *testing.T, runner process.Runner, logs ...io.Writer) *ContainerLifecycle {
	t.Helper()
	logOutput := io.Writer(io.Discard)
	if len(logs) > 0 {
		logOutput = logs[0]
	}
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"model": {ID: "model", Destination: "model.gguf"}},
		LlamaPresets: map[string]catalog.LlamaPreset{
			"fixture": {ID: "fixture", Bundle: "bundle", Artifact: "model", DefaultContext: 4096},
		},
	}
	registry := Registry{Models: []Model{{ID: "fixture", Application: "llama-cpp", Backend: "llama-cpp", Bundle: "bundle"}}}
	lifecycle, err := NewContainerLifecycle(LifecycleOptions{
		Catalog: managed, Registry: registry, DataRoot: t.TempDir(), Profile: "cpu",
		LlamaBackend: "rocm", LlamaModelsMax: 2, RouterPreset: "/data/models.ini",
		VolumeSuffix: ":rw", Runner: runner, Log: logOutput,
		StartupTimeout: time.Second, ReadinessInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle
}

func TestContainerLifecycleStartsOnPrivateDynamicPortAndStopsOwnedContainer(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/health" {
			t.Fatalf("readiness path=%s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	parsed, _ := url.Parse(backend.URL)
	port, _ := strconv.Atoi(parsed.Port())
	runner := &lifecycleRunner{port: port, logsStarted: make(chan struct{})}
	var logs bytes.Buffer
	lifecycle := llamaLifecycleFixture(t, runner, &logs)
	if err := lifecycle.Preflight(context.Background(), []Allocation{AllocationLlamaCPP}); err != nil {
		t.Fatal(err)
	}
	upstream, err := lifecycle.Start(context.Background(), AllocationLlamaCPP)
	if err != nil {
		t.Fatal(err)
	}
	if upstream.String() != backend.URL {
		t.Fatalf("upstream=%s want=%s", upstream, backend.URL)
	}
	select {
	case <-runner.logsStarted:
	case <-time.After(time.Second):
		t.Fatal("backend log follower did not start")
	}
	if err := lifecycle.Stop(context.Background(), AllocationLlamaCPP); err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	logFollowers := runner.logFollowers
	runner.mu.Unlock()
	if logFollowers != 0 {
		t.Fatalf("backend log followers after stop=%d", logFollowers)
	}
	joined := ""
	for _, command := range runner.commands {
		joined += "\n" + command.Name + " " + strings.Join(command.Args, " ")
	}
	for _, required := range []string{"--publish 127.0.0.1::8080/tcp", "--name paracetamol-gateway-llama-cpp", "gateway-backend", "logs --follow paracetamol-gateway-llama-cpp"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("commands lack %q:%s", required, joined)
		}
	}
	if !strings.Contains(logs.String(), "llama-cpp | backend ready") {
		t.Fatalf("backend logs lack application prefix:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "log stream ended") {
		t.Fatalf("normal stop reported a log failure:\n%s", logs.String())
	}
}

func TestContainerLifecycleRefusesManagedDirectContainer(t *testing.T) {
	runner := &lifecycleRunner{conflict: "paracetamol-llama-cpp\n"}
	lifecycle := llamaLifecycleFixture(t, runner)
	if err := lifecycle.Preflight(context.Background(), []Allocation{AllocationLlamaCPP}); err == nil || !strings.Contains(err.Error(), "paracetamol-llama-cpp") {
		t.Fatalf("err=%v", err)
	}
}

func TestContainerLifecycleReclaimsOnlyItsExactStaleContainer(t *testing.T) {
	name := gatewayContainerNames[AllocationLlamaCPP]
	runner := &lifecycleRunner{conflict: name + "\n", running: true}
	lifecycle := llamaLifecycleFixture(t, runner)
	if err := lifecycle.Preflight(context.Background(), []Allocation{AllocationLlamaCPP}); err != nil {
		t.Fatal(err)
	}
	if runner.running {
		t.Fatal("owned stale gateway container was not reclaimed")
	}

	unmanaged := &lifecycleRunner{running: true}
	lifecycle = llamaLifecycleFixture(t, unmanaged)
	if err := lifecycle.Preflight(context.Background(), []Allocation{AllocationLlamaCPP}); err == nil || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("unmanaged exact-name collision err=%v", err)
	}
}

func TestContainerLifecycleDwarfStarCommandDisablesDSpark(t *testing.T) {
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"model": {ID: "model", Destination: "model.gguf", Target: "dwarfstar-models"}},
		Bundles:   map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "dwarfstar", Artifacts: []string{"model"}}},
		DwarfStarPresets: map[string]catalog.DwarfStarPreset{
			"deepseek": {ID: "deepseek", Bundle: "bundle", DefaultContext: 131072, MaxOutputTokens: 16000},
		},
	}
	registry := Registry{Models: []Model{{ID: "deepseek", Application: "dwarfstar", Backend: "dwarfstar", Bundle: "bundle"}}}
	lifecycle, err := NewContainerLifecycle(LifecycleOptions{
		Catalog: managed, Registry: registry, DataRoot: "/data", Profile: "strix-halo",
		RenderNodes: []string{"/dev/dri/renderD128"}, LlamaBackend: "rocm", LlamaModelsMax: 2,
		VolumeSuffix: ":rw", Runner: &lifecycleRunner{}, Log: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	command, _, _, err := lifecycle.command(AllocationDwarfStar)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	if !strings.Contains(joined, "PARACETAMOL_DWARFSTAR_DSPARK=0") || strings.Contains(joined, "PARACETAMOL_DWARFSTAR_DSPARK_MODEL=") {
		t.Fatalf("unexpected DwarfStar gateway command: %s", joined)
	}
}
