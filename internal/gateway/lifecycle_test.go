package gateway

import (
	"bytes"
	"context"
	"errors"
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

	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

type lifecycleRunner struct {
	mu            sync.Mutex
	port          int
	running       bool
	conflict      string
	commands      []process.Command
	logFollowers  int
	logsStarted   chan struct{}
	logsOnce      sync.Once
	runResult     *process.Result
	runError      error
	afterRun      func()
	removeError   error
	labels        string
	portError     error
	contextErrors []error
}

func TestLifecycleKeepsRequestSlotsSeparateFromLoadedModels(t *testing.T) {
	for _, slots := range []int{0, 1, 4, 8, -1, 65} {
		lifecycle, err := NewContainerLifecycle(LifecycleOptions{
			Registry: Registry{Models: []Model{{ID: "fixture", Backend: "llama-cpp"}}},
			DataRoot: "/data", Profile: "cpu", LlamaBackend: "rocm", LlamaModelsMax: 1,
			LlamaSlots: slots, RouterPreset: "/data/models.ini", Runner: &lifecycleRunner{}, Log: io.Discard,
		})
		if slots < 0 || slots > 64 {
			if err == nil {
				t.Fatalf("accepted slots=%d", slots)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		command, _, _, err := lifecycle.command(AllocationLlamaCPP)
		if err != nil {
			t.Fatal(err)
		}
		want := slots
		if want == 0 {
			want = 4
		}
		joined := strings.Join(command, " ")
		if !strings.Contains(joined, fmt.Sprintf("PARACETAMOL_LLAMA_SLOTS=%d", want)) || !strings.Contains(joined, "PARACETAMOL_LLAMA_MODELS_MAX=1") {
			t.Fatalf("command=%s", joined)
		}
	}
}

func (runner *lifecycleRunner) LookPath(string) (string, error) { return "/usr/bin/podman", nil }

func (runner *lifecycleRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	runner.mu.Lock()
	runner.commands = append(runner.commands, command)
	runner.contextErrors = append(runner.contextErrors, ctx.Err())
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
		if runner.afterRun != nil {
			runner.afterRun()
		}
		if runner.runResult != nil || runner.runError != nil {
			result := process.Result{}
			if runner.runResult != nil {
				result = *runner.runResult
			}
			return result, runner.runError
		}
		return process.Result{Stdout: []byte("container-id\n")}, nil
	case strings.HasPrefix(arguments, "port "):
		if runner.portError != nil {
			return process.Result{}, runner.portError
		}
		return process.Result{Stdout: []byte(fmt.Sprintf("127.0.0.1:%d\n", runner.port))}, nil
	case strings.HasPrefix(arguments, "inspect --format {{json .Config.Labels}} "):
		if runner.labels != "" {
			return process.Result{Stdout: []byte(runner.labels)}, nil
		}
		labels := fmt.Sprintf(`{"%s":"true","%s":"llama-cpp","%s":"%s"}`,
			podman.ManagedContainerLabel, podman.ManagedApplicationLabel, podman.ManagedRoleLabel, GatewayBackendRole)
		return process.Result{Stdout: []byte(labels)}, nil
	case strings.HasPrefix(arguments, "rm --force "):
		if runner.removeError != nil {
			return process.Result{}, runner.removeError
		}
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
	registry := Registry{Models: []Model{{ID: "fixture", Application: "llama-cpp", Backend: "llama-cpp", Bundle: "bundle"}}}
	lifecycle, err := NewContainerLifecycle(LifecycleOptions{
		Registry: registry, DataRoot: t.TempDir(), Profile: "cpu",
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

func TestContainerLifecycleCleansOwnedBackendAfterFailedRun(t *testing.T) {
	for _, test := range []struct {
		name   string
		result process.Result
		err    error
		detail string
	}{
		{name: "runner-error", err: context.Canceled, detail: "context canceled"},
		{name: "nonzero-status", result: process.Result{Status: 125, Stderr: []byte("run failed after creation")}, detail: "run failed after creation"},
		{name: "killed-client", result: process.Result{Status: -1}, detail: "status -1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := &lifecycleRunner{runResult: &test.result, runError: test.err, afterRun: cancel}
			lifecycle := llamaLifecycleFixture(t, runner)
			upstream, err := lifecycle.Start(ctx, AllocationLlamaCPP)
			if upstream != nil || err == nil || !strings.Contains(err.Error(), test.detail) {
				t.Fatalf("upstream=%v err=%v", upstream, err)
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Fatalf("lost startup cause: %v", err)
			}
			var cleanupFailure *backendCleanupError
			if errors.As(err, &cleanupFailure) || runner.running {
				t.Fatalf("failed run left an owned backend: running=%t err=%v", runner.running, err)
			}
			afterRun := false
			cleanupCommands := []string{}
			for index, command := range runner.commands {
				if command.Args[0] == "run" {
					afterRun = true
					continue
				}
				if afterRun {
					if runner.contextErrors[index] != nil {
						t.Fatalf("cleanup used canceled context: %v", command.Args)
					}
					cleanupCommands = append(cleanupCommands, strings.Join(command.Args, " "))
				}
			}
			name := gatewayContainerNames[AllocationLlamaCPP]
			want := []string{"container exists " + name, "inspect --format {{json .Config.Labels}} " + name, "rm --force --time 2 --ignore " + name}
			if fmt.Sprint(cleanupCommands) != fmt.Sprint(want) {
				t.Fatalf("cleanup commands=%v, want %v", cleanupCommands, want)
			}
		})
	}
}

func TestContainerLifecycleReportsCleanupFailureAtEveryStartupStage(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer backend.Close()
	parsed, _ := url.Parse(backend.URL)
	port, _ := strconv.Atoi(parsed.Port())
	for _, stage := range []string{"run", "log-follower", "port", "readiness"} {
		t.Run(stage, func(t *testing.T) {
			cleanupErr := errors.New("fixture removal failed")
			startErr := errors.New("fixture startup failed")
			runner := &lifecycleRunner{port: port, removeError: cleanupErr}
			lifecycle := llamaLifecycleFixture(t, runner)
			var detail string
			switch stage {
			case "run":
				runner.runError, detail = startErr, startErr.Error()
			case "log-follower":
				if err := lifecycle.followBackendLogs(AllocationLlamaCPP); err != nil {
					t.Fatal(err)
				}
				detail = "already following"
			case "port":
				runner.portError, detail = startErr, startErr.Error()
			case "readiness":
				lifecycle.options.StartupTimeout = 5 * time.Millisecond
				detail = context.DeadlineExceeded.Error()
			}
			upstream, err := lifecycle.Start(context.Background(), AllocationLlamaCPP)
			var cleanupFailure *backendCleanupError
			if upstream != nil || !errors.As(err, &cleanupFailure) || !strings.Contains(err.Error(), detail) || !strings.Contains(err.Error(), cleanupErr.Error()) {
				t.Fatalf("upstream=%v err=%v", upstream, err)
			}
			if stage == "run" && !errors.Is(err, startErr) {
				t.Fatalf("cleanup error hid original start error: %v", err)
			}
			runner.mu.Lock()
			running, followers := runner.running, runner.logFollowers
			runner.removeError = nil
			runner.mu.Unlock()
			if !running || followers != 0 {
				t.Fatalf("failed cleanup residency: running=%t followers=%d", running, followers)
			}
			if err := lifecycle.Stop(context.Background(), AllocationLlamaCPP); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestContainerLifecycleFailedRunPreservesForeignContainer(t *testing.T) {
	for _, labels := range []string{
		`{}`,
		fmt.Sprintf(`{"%s":"true","%s":"llama-cpp","%s":"direct"}`, podman.ManagedContainerLabel, podman.ManagedApplicationLabel, podman.ManagedRoleLabel),
		fmt.Sprintf(`{"%s":"true","%s":"other","%s":"%s"}`, podman.ManagedContainerLabel, podman.ManagedApplicationLabel, podman.ManagedRoleLabel, GatewayBackendRole),
	} {
		t.Run(labels, func(t *testing.T) {
			runner := &lifecycleRunner{runResult: &process.Result{Status: -1}, labels: labels}
			lifecycle := llamaLifecycleFixture(t, runner)
			_, err := lifecycle.Start(context.Background(), AllocationLlamaCPP)
			var cleanupFailure *backendCleanupError
			if !errors.As(err, &cleanupFailure) || !strings.Contains(err.Error(), "unexpected ownership labels") || !runner.running {
				t.Fatalf("foreign container cleanup: running=%t err=%v", runner.running, err)
			}
			for _, command := range runner.commands {
				if command.Args[0] == "rm" {
					t.Fatalf("attempted foreign removal: %v", command.Args)
				}
			}
		})
	}
}

func TestSchedulerRetainsBackendWhenStartupCleanupFails(t *testing.T) {
	runner := &pendingStartRunner{
		lifecycleRunner: lifecycleRunner{removeError: errors.New("fixture removal failed")},
		started:         make(chan struct{}), finish: make(chan struct{}),
	}
	lifecycle := llamaLifecycleFixture(t, runner)
	scheduler, err := NewScheduler(lifecycle, 64, map[Allocation]int{testAllocation: 1})
	if err != nil {
		t.Fatal(err)
	}
	acquisitions := []<-chan acquireResult{acquireAsync(scheduler, AllocationLlamaCPP)}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("detached run did not start")
	}
	acquisitions = append(acquisitions, acquireAsync(scheduler, testAllocation), acquireAsync(scheduler, AllocationLlamaCPP))
	waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.QueuedRequests == 3 })
	close(runner.finish)
	var cleanupFailure *backendCleanupError
	for _, acquired := range acquisitions {
		select {
		case result := <-acquired:
			if result.lease != nil || !errors.As(result.err, &cleanupFailure) {
				t.Fatalf("queued startup cleanup result=%#v", result)
			}
		case <-time.After(time.Second):
			t.Fatal("failed cleanup left a queued acquisition waiting")
		}
	}
	for range 20 {
		status, err := scheduler.Status(context.Background())
		if err != nil || status.State != StateFailed || status.Allocation != AllocationLlamaCPP || status.ActiveRequests != 0 || status.QueuedRequests != 0 {
			t.Fatalf("uncertain startup residency=%#v err=%v", status, err)
		}
	}
	if _, err := scheduler.Acquire(context.Background(), testAllocation); !errors.As(err, &cleanupFailure) {
		t.Fatalf("new allocation ignored unresolved cleanup: %v", err)
	}
	runner.mu.Lock()
	removals := 0
	for _, command := range runner.commands {
		if command.Args[0] == "rm" {
			removals++
		}
	}
	running := runner.running
	runner.removeError = nil
	runner.mu.Unlock()
	if !running || removals != 1 {
		t.Fatalf("startup cleanup retries: running=%t removals=%d", running, removals)
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.running {
		t.Fatal("shutdown forgot failed-start backend")
	}
}

type pendingStartRunner struct {
	lifecycleRunner
	started chan struct{}
	finish  chan struct{}
}

func (runner *pendingStartRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	result, err := runner.lifecycleRunner.Run(ctx, command)
	if command.Args[0] == "run" && err == nil {
		close(runner.started)
		select {
		case <-ctx.Done():
		case <-runner.finish:
		}
		return process.Result{Status: -1}, nil
	}
	return result, err
}

func TestSchedulerShutdownCleansBackendAfterInterruptedDetachedRun(t *testing.T) {
	runner := &pendingStartRunner{started: make(chan struct{})}
	lifecycle := llamaLifecycleFixture(t, runner)
	scheduler, err := NewScheduler(lifecycle, 64, nil)
	if err != nil {
		t.Fatal(err)
	}
	acquired := acquireAsync(scheduler, AllocationLlamaCPP)
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("detached run did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if result := <-acquired; result.lease != nil || !errors.Is(result.err, ErrShuttingDown) {
		t.Fatalf("interrupted acquisition=%#v", result)
	}
	if runner.running {
		t.Fatal("interrupted Podman client left its detached backend running")
	}
}
