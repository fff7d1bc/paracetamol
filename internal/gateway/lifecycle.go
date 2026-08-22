package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/identity"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/runtime"
)

const (
	GatewayBackendRole    = "gateway-backend"
	DefaultStartupTimeout = 30 * time.Minute
)

var gatewayContainerNames = map[Allocation]string{
	AllocationLlamaCPP:  identity.Container("gateway-llama-cpp"),
	AllocationDwarfStar: identity.Container("gateway-dwarfstar"),
}

type LifecycleOptions struct {
	Catalog           catalog.Catalog
	Registry          Registry
	DataRoot          string
	Profile           string
	RenderNodes       []string
	LlamaBackend      string
	LlamaModelsMax    int
	RouterPreset      string
	SourceRevision    string
	VolumeSuffix      string
	Unconfined        bool
	StartupTimeout    time.Duration
	Runner            process.Runner
	Log               io.Writer
	ReadinessInterval time.Duration
}

type ContainerLifecycle struct {
	options  LifecycleOptions
	podman   podman.Client
	client   *http.Client
	logMu    sync.Mutex
	follower *backendLogFollower
}

type backendLogFollower struct {
	allocation Allocation
	cancel     context.CancelFunc
	done       chan struct{}
}

func NewContainerLifecycle(options LifecycleOptions) (*ContainerLifecycle, error) {
	if options.Runner == nil || options.DataRoot == "" || options.Profile == "" || len(options.Registry.Models) == 0 {
		return nil, fmt.Errorf("invalid gateway backend lifecycle configuration")
	}
	if options.LlamaBackend != "rocm" && options.LlamaBackend != "vulkan" {
		return nil, fmt.Errorf("invalid gateway llama.cpp backend %q", options.LlamaBackend)
	}
	if options.LlamaModelsMax < 1 {
		return nil, fmt.Errorf("gateway llama.cpp models-max must be positive")
	}
	if options.StartupTimeout <= 0 {
		options.StartupTimeout = DefaultStartupTimeout
	}
	if options.ReadinessInterval <= 0 {
		options.ReadinessInterval = 500 * time.Millisecond
	}
	if options.Log == nil {
		options.Log = os.Stderr
	}
	options.Log = atomicLogWriter(options.Log)
	llamaModels, dwarfModels := 0, 0
	for _, model := range options.Registry.Models {
		switch model.Backend {
		case "llama-cpp":
			llamaModels++
		case "dwarfstar":
			dwarfModels++
		}
	}
	if llamaModels > 0 && options.RouterPreset == "" {
		return nil, fmt.Errorf("gateway llama.cpp inventory requires a router preset")
	}
	// DwarfStar serves one eagerly selected model per process. The catalog
	// deliberately contains one non-DSpark preset; reject accidental expansion
	// until allocations encode a DwarfStar model identity.
	if dwarfModels > 1 {
		return nil, fmt.Errorf("gateway supports exactly one DwarfStar preset")
	}
	return &ContainerLifecycle{
		options: options,
		podman:  podman.Client{Runner: options.Runner},
		client:  &http.Client{Timeout: 2 * time.Second},
	}, nil
}

// Preflight validates every selected backend before the public listener is
// opened. Only a stale container bearing the exact gateway ownership labels
// is reclaimed; direct, benchmark, and foreign containers are preserved.
func (lifecycle *ContainerLifecycle) Preflight(ctx context.Context, allocations []Allocation) error {
	if err := lifecycle.podman.RequireRootless(ctx); err != nil {
		return err
	}
	seen := map[Allocation]bool{}
	for _, allocation := range allocations {
		if _, ok := gatewayContainerNames[allocation]; !ok {
			return fmt.Errorf("unknown gateway allocation %q", allocation)
		}
		if seen[allocation] {
			continue
		}
		seen[allocation] = true
		application, _ := config.ApplicationByID(string(allocation))
		present, err := lifecycle.podman.Exists(ctx, "image", application.Image)
		if err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("gateway application image is not built: %s", application.Image)
		}
		if err := lifecycle.requireAvailable(ctx, allocation, true); err != nil {
			return err
		}
	}
	return nil
}

func (lifecycle *ContainerLifecycle) Start(ctx context.Context, allocation Allocation) (*url.URL, error) {
	if _, ok := gatewayContainerNames[allocation]; !ok {
		return nil, fmt.Errorf("unknown gateway allocation %q", allocation)
	}
	if err := lifecycle.requireAvailable(ctx, allocation, true); err != nil {
		return nil, err
	}
	command, internalPort, readinessPath, err := lifecycle.command(allocation)
	if err != nil {
		return nil, err
	}
	logLine(lifecycle.options.Log, "gateway | starting %s backend", allocation)
	result, err := lifecycle.options.Runner.Run(ctx, process.Command{Name: command[0], Args: command[1:]})
	if err != nil {
		return nil, fmt.Errorf("start gateway %s backend: %w", allocation, err)
	}
	if result.Status != 0 {
		detail := strings.TrimSpace(string(result.Stderr))
		if detail == "" {
			detail = fmt.Sprintf("podman exited with status %d", result.Status)
		}
		return nil, fmt.Errorf("start gateway %s backend: %s", allocation, detail)
	}
	if err := lifecycle.followBackendLogs(allocation); err != nil {
		_ = lifecycle.removeOwned(context.Background(), allocation)
		return nil, err
	}
	name := gatewayContainerNames[allocation]
	hostPort, err := lifecycle.podman.PublishedLoopbackPort(ctx, name, internalPort)
	if err != nil {
		lifecycle.stopFollowingBackendLogs(allocation)
		_ = lifecycle.removeOwned(context.Background(), allocation)
		return nil, err
	}
	upstream, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", hostPort))
	readyContext, cancel := context.WithTimeout(ctx, lifecycle.options.StartupTimeout)
	defer cancel()
	if err := lifecycle.waitReady(readyContext, allocation, upstream.ResolveReference(&url.URL{Path: readinessPath})); err != nil {
		lifecycle.stopFollowingBackendLogs(allocation)
		_ = lifecycle.removeOwned(context.Background(), allocation)
		return nil, err
	}
	logLine(lifecycle.options.Log, "gateway | %s backend ready on private port %d", allocation, hostPort)
	return upstream, nil
}

func (lifecycle *ContainerLifecycle) Stop(ctx context.Context, allocation Allocation) error {
	if _, ok := gatewayContainerNames[allocation]; !ok {
		return fmt.Errorf("unknown gateway allocation %q", allocation)
	}
	logLine(lifecycle.options.Log, "gateway | stopping %s backend", allocation)
	lifecycle.stopFollowingBackendLogs(allocation)
	return lifecycle.removeOwned(ctx, allocation)
}

func (lifecycle *ContainerLifecycle) followBackendLogs(allocation Allocation) error {
	lifecycle.logMu.Lock()
	if lifecycle.follower != nil {
		active := lifecycle.follower.allocation
		lifecycle.logMu.Unlock()
		return fmt.Errorf("gateway is already following %s backend logs", active)
	}
	ctx, cancel := context.WithCancel(context.Background())
	follower := &backendLogFollower{allocation: allocation, cancel: cancel, done: make(chan struct{})}
	lifecycle.follower = follower
	lifecycle.logMu.Unlock()

	logLine(lifecycle.options.Log, "gateway | following %s backend logs", allocation)
	output := newPrefixedLineWriter(lifecycle.options.Log, string(allocation)+" | ")
	go func() {
		defer close(follower.done)
		err := lifecycle.podman.Logs(ctx, podman.LogOptions{
			Container: gatewayContainerNames[allocation], Follow: true, All: true,
			Streams: podman.Streams{Stdout: output, Stderr: output},
		})
		output.finish()
		if err != nil && ctx.Err() == nil {
			logLine(lifecycle.options.Log, "gateway | %s backend log stream ended: %v", allocation, err)
		}
	}()
	return nil
}

func (lifecycle *ContainerLifecycle) stopFollowingBackendLogs(allocation Allocation) {
	lifecycle.logMu.Lock()
	follower := lifecycle.follower
	if follower == nil || follower.allocation != allocation {
		lifecycle.logMu.Unlock()
		return
	}
	lifecycle.follower = nil
	lifecycle.logMu.Unlock()
	follower.cancel()
	<-follower.done
}

func (lifecycle *ContainerLifecycle) command(allocation Allocation) ([]string, int, string, error) {
	application, _ := config.ApplicationByID(string(allocation))
	name := gatewayContainerNames[allocation]
	switch allocation {
	case AllocationLlamaCPP:
		command, err := runtime.LlamaCommand(runtime.LlamaOptions{
			Image: application.Image, Profile: lifecycle.options.Profile, Mode: "server",
			DataDir: lifecycle.options.DataRoot, Backend: lifecycle.options.LlamaBackend,
			SourceRevision: lifecycle.options.SourceRevision, RouterPreset: lifecycle.options.RouterPreset,
			ModelsMax: lifecycle.options.LlamaModelsMax, RenderNodes: lifecycle.options.RenderNodes,
			Listen: "127.0.0.1", Port: application.Port, Detach: true, Unconfined: lifecycle.options.Unconfined,
			ContainerName: name, ContainerRole: GatewayBackendRole, AutoRemove: true, DynamicHostPort: true,
		}, lifecycle.options.VolumeSuffix)
		return command, application.Port, "/health", err
	case AllocationDwarfStar:
		model, err := lifecycle.dwarfStarModel()
		if err != nil {
			return nil, 0, "", err
		}
		preset := lifecycle.options.Catalog.DwarfStarPresets[model.ID]
		bundle := lifecycle.options.Catalog.Bundles[model.Bundle]
		artifact := lifecycle.options.Catalog.Artifacts[bundle.Artifacts[0]]
		command, err := runtime.DwarfStarCommand(runtime.DwarfStarOptions{
			Image: application.Image, Mode: "server", DataDir: lifecycle.options.DataRoot,
			Model: content.ArtifactPath(lifecycle.options.DataRoot, artifact), RenderNodes: lifecycle.options.RenderNodes,
			Profile: lifecycle.options.Profile, Listen: "127.0.0.1", Port: application.Port,
			Context: preset.DefaultContext, OutputTokens: preset.MaxOutputTokens, Detach: true,
			Unconfined: lifecycle.options.Unconfined, ContainerName: name, ContainerRole: GatewayBackendRole,
			DynamicHostPort: true,
		}, lifecycle.options.VolumeSuffix)
		return command, application.Port, "/v1/models", err
	default:
		return nil, 0, "", fmt.Errorf("unknown gateway allocation %q", allocation)
	}
}

func (lifecycle *ContainerLifecycle) dwarfStarModel() (Model, error) {
	for _, model := range lifecycle.options.Registry.Models {
		if model.Application == string(AllocationDwarfStar) {
			return model, nil
		}
	}
	return Model{}, fmt.Errorf("gateway registry has no DwarfStar model")
}

func (lifecycle *ContainerLifecycle) waitReady(ctx context.Context, allocation Allocation, endpoint *url.URL) error {
	ticker := time.NewTicker(lifecycle.options.ReadinessInterval)
	defer ticker.Stop()
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		response, err := lifecycle.client.Do(request)
		if err == nil {
			_, _ = io.CopyN(io.Discard, response.Body, 4096)
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		present, inspectErr := lifecycle.podman.Exists(ctx, "container", gatewayContainerNames[allocation])
		if inspectErr != nil {
			return inspectErr
		}
		if !present {
			return fmt.Errorf("gateway %s backend exited before becoming ready", allocation)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("gateway %s backend did not become ready: %w", allocation, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (lifecycle *ContainerLifecycle) requireAvailable(ctx context.Context, allocation Allocation, reclaim bool) error {
	application := string(allocation)
	target := gatewayContainerNames[allocation]
	names, err := lifecycle.podman.ManagedContainerNames(ctx, application)
	if err != nil {
		return err
	}
	targetSeen := false
	for _, name := range names {
		if name != target {
			return fmt.Errorf("gateway cannot use %s while managed container %s exists", application, name)
		}
		targetSeen = true
	}
	exists, err := lifecycle.podman.Exists(ctx, "container", target)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !targetSeen {
		return fmt.Errorf("gateway container name is occupied by an unmanaged container: %s", target)
	}
	labels, err := lifecycle.podman.ContainerLabels(ctx, target)
	if err != nil {
		return err
	}
	if labels[podman.ManagedContainerLabel] != "true" || labels[podman.ManagedApplicationLabel] != application || labels[podman.ManagedRoleLabel] != GatewayBackendRole {
		return fmt.Errorf("gateway container name has unexpected ownership labels: %s", target)
	}
	if !reclaim {
		return fmt.Errorf("gateway backend container already exists: %s", target)
	}
	logLine(lifecycle.options.Log, "gateway | reclaiming stale backend container %s", target)
	return lifecycle.podman.RemoveContainer(ctx, target, 2, podman.Streams{})
}

func (lifecycle *ContainerLifecycle) removeOwned(ctx context.Context, allocation Allocation) error {
	name := gatewayContainerNames[allocation]
	exists, err := lifecycle.podman.Exists(ctx, "container", name)
	if err != nil || !exists {
		return err
	}
	labels, err := lifecycle.podman.ContainerLabels(ctx, name)
	if err != nil {
		return err
	}
	if labels[podman.ManagedContainerLabel] != "true" || labels[podman.ManagedApplicationLabel] != string(allocation) || labels[podman.ManagedRoleLabel] != GatewayBackendRole {
		return fmt.Errorf("refusing to remove container with unexpected ownership labels: %s", name)
	}
	// Cleanup must not depend on the foreground terminal still accepting output
	// (for example after an SSH connection drops). Capture Podman's small result
	// internally rather than wiring it to the possibly broken gateway log.
	return lifecycle.podman.RemoveContainer(ctx, name, 2, podman.Streams{})
}
