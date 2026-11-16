package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/gateway"
	"paracetamol/internal/identity"
	"paracetamol/internal/platform"
	"paracetamol/internal/podman"
)

var fullID = regexp.MustCompile(`^(sha256:)?[0-9a-f]{64}$`)
var sourceID = regexp.MustCompile(`^[0-9a-f]{40}$`)

type backendEvidence struct {
	Trace           string            `json:"diagnostic_trace,omitempty"`
	GatewayInstance string            `json:"gateway_instance"`
	BootSession     string            `json:"boot_session_sha256"`
	Control         string            `json:"diagnostic_control,omitempty"`
	ContainerID     string            `json:"container_id"`
	ImageID         string            `json:"image_id"`
	Source          string            `json:"upstream_commit"`
	SourceTree      string            `json:"patched_source_tree"`
	Patches         string            `json:"patches"`
	ROCm            string            `json:"rocm"`
	NativeURL       string            `json:"private_native_url"`
	Arguments       []string          `json:"native_arguments"`
	Policy          map[string]string `json:"native_policy"`
	Artifacts       map[string]string `json:"artifact_sha256"`
	Template        string            `json:"template_sha256"`
	Kernel          string            `json:"kernel,omitempty"`
	Firmware        string            `json:"firmware_profile,omitempty"`
	CPUProfile      string            `json:"cpu_epp,omitempty"`
	Profile         string            `json:"profile"`
	RenderNodes     []string          `json:"render_nodes"`
	Sampling        map[string]any    `json:"sampling_policy"`
	Environment     map[string]string `json:"native_environment"`
	MemoryGuard     int64             `json:"minimum_available_memory_bytes"`
}

type inspectedContainer struct {
	ID            string `json:"Id"`
	Image         string
	OCIConfigPath string
	Name          string
	State         struct{ Running bool }
	Config        struct{ Labels map[string]string }
	EffectiveCaps json.RawMessage
	BoundingCaps  json.RawMessage
	HostConfig    struct {
		ReadonlyRootfs bool
		NetworkMode    string
		CapDrop        []string
		CapAdd         []string
		SecurityOpt    []string
		Devices        []struct{ PathOnHost, PathInContainer string }
	}
	Mounts []struct {
		Source, Destination, Type string
		RW                        bool
	}
	NetworkSettings struct {
		Ports map[string][]struct{ HostIP, HostPort string }
	}
}

func commandJSON(ctx context.Context, destination any, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "podman", args...)
	var err error
	reader, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, 4*1024*1024+1))
	if readErr != nil || len(data) > 4*1024*1024 {
		cancel()
	}
	waitErr := cmd.Wait()
	if readErr != nil || len(data) > 4*1024*1024 || waitErr != nil {
		return fmt.Errorf("bounded Podman inspection failed")
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("invalid Podman inspection response: %w", err)
	}
	return nil
}

func captureBackend(ctx context.Context, root string, opts runOptions, managed catalog.Catalog) (backendEvidence, error) {
	evidence := backendEvidence{MemoryGuard: 8 << 30}
	if !fullID.MatchString(opts.containerID) || !fullID.MatchString(opts.imageID) {
		return evidence, fmt.Errorf("backend container and image must be full immutable IDs")
	}
	var info struct {
		Host struct{ Security struct{ Rootless bool } }
	}
	if err := commandJSON(ctx, &info, "info", "--format", "json"); err != nil || !info.Host.Security.Rootless {
		return evidence, fmt.Errorf("acceptance requires working rootless Podman")
	}
	var containers []inspectedContainer
	if err := commandJSON(ctx, &containers, "container", "inspect", opts.containerID); err != nil || len(containers) != 1 {
		return evidence, fmt.Errorf("cannot inspect the exact selected backend container")
	}
	c := containers[0]
	if err := validateContainer(c, opts); err != nil {
		return evidence, err
	}
	devices, err := exposedGPUDevices(c)
	if err != nil {
		return evidence, err
	}
	for _, device := range devices {
		if strings.HasPrefix(device, "/dev/dri/renderD") {
			evidence.RenderNodes = append(evidence.RenderNodes, device)
		} else if device != "/dev/kfd" {
			return evidence, fmt.Errorf("backend exposes an unexpected device")
		}
	}
	if len(devices) != 2 || len(evidence.RenderNodes) != 1 || platform.ModelProfile("auto", evidence.RenderNodes) != "strix-halo" {
		return evidence, fmt.Errorf("this acceptance policy requires exactly one independently detected Strix Halo render node and /dev/kfd")
	}
	if err := platform.CheckAMDDeviceAccess(evidence.RenderNodes); err != nil {
		return evidence, err
	}
	evidence.Profile = "strix-halo"
	environment, err := containerBytes(ctx, c.ID, "/proc/1/environ", 128*1024)
	if err != nil {
		return evidence, fmt.Errorf("cannot observe effective router environment: %w", err)
	}
	evidence.Environment = map[string]string{}
	for _, assignment := range strings.Split(string(environment), "\x00") {
		name, value, ok := strings.Cut(assignment, "=")
		if !ok || !(strings.HasPrefix(name, "GGML_") || strings.HasPrefix(name, "LLAMA_") || strings.HasPrefix(name, "PARACETAMOL_QWEN4EXP_") || strings.HasPrefix(name, "HSA_") || strings.HasPrefix(name, "HIP_") || strings.HasPrefix(name, "ROCR_")) {
			continue
		}
		if strings.Contains(name, "KEY") || strings.Contains(name, "TOKEN") || strings.Contains(name, "SECRET") {
			return evidence, fmt.Errorf("sensitive inference environment must not enter evidence")
		}
		evidence.Environment[name] = value
	}
	if evidence.Environment["PARACETAMOL_QWEN4EXP_QSA_KERNEL"] != "1" || evidence.Environment["PARACETAMOL_QWEN4EXP_QSA_GRAPH"] != "1" || evidence.Environment["GGML_CUDA_DISABLE_GRAPHS"] != "" || evidence.Environment["LLAMA_GRAPH_REUSE_DISABLE"] != "" {
		return evidence, fmt.Errorf("QSA or graph environment differs from the accepted normal-image policy")
	}
	var images []struct {
		Config struct{ Labels map[string]string }
	}
	if err := commandJSON(ctx, &images, "image", "inspect", opts.imageID); err != nil || len(images) != 1 {
		return evidence, fmt.Errorf("cannot inspect exact backend image")
	}
	labels := images[0].Config.Labels
	evidence.Trace = labels[identity.LabelNamespace+".llama-acceptance.trace"]
	if opts.trace && evidence.Trace != "qwen4exp.v1" || !opts.trace && evidence.Trace != "" {
		return evidence, fmt.Errorf("diagnostic trace label differs from explicit --trace; traced images never qualify a normal upgrade")
	}
	evidence.Control = labels[identity.LabelNamespace+".llama-acceptance.control"]
	if evidence.Control != opts.control {
		return evidence, fmt.Errorf("diagnostic control differs from explicit --control; negative images never qualify a normal upgrade")
	}
	evidence.ContainerID, evidence.ImageID = c.ID, canonicalID(c.Image)
	evidence.GatewayInstance = c.Config.Labels[gateway.GatewayInstanceLabel]
	evidence.Source = labels["org.opencontainers.image.revision"]
	evidence.SourceTree = labels[identity.LabelNamespace+".llama-cpp.source-tree"]
	evidence.Patches = labels[identity.LabelNamespace+".llama-cpp.patches"]
	evidence.ROCm = labels[identity.LabelNamespace+".rocm.version"]
	if !sourceID.MatchString(evidence.Source) || !sourceID.MatchString(evidence.SourceTree) || evidence.ROCm == "" || evidence.Patches == "" {
		return evidence, fmt.Errorf("backend image lacks reviewed source, tree, patchset or ROCm identity labels")
	}
	bindings := c.NetworkSettings.Ports["8080/tcp"]
	for name, ports := range c.NetworkSettings.Ports {
		if name != "8080/tcp" && len(ports) != 0 {
			return evidence, fmt.Errorf("backend has an unexpected additional publication")
		}
	}
	if len(bindings) != 1 || net.ParseIP(bindings[0].HostIP) == nil || !net.ParseIP(bindings[0].HostIP).IsLoopback() {
		return evidence, fmt.Errorf("backend must publish exactly one private loopback endpoint")
	}
	port, err := strconv.Atoi(bindings[0].HostPort)
	if err != nil || port < 1 || port > 65535 {
		return evidence, fmt.Errorf("invalid backend publication")
	}
	evidence.NativeURL = "http://" + net.JoinHostPort(bindings[0].HostIP, strconv.Itoa(port))
	var models struct {
		Data []struct {
			ID     string `json:"id"`
			Status struct {
				Value string   `json:"value"`
				Args  []string `json:"args"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := nativeRead(ctx, evidence.NativeURL+"/models", &models); err != nil {
		return evidence, err
	}
	found := false
	for _, model := range models.Data {
		if model.ID != opts.model {
			continue
		}
		if found || model.Status.Value != "loaded" || len(model.Status.Args) == 0 {
			return evidence, fmt.Errorf("selected model needs one loaded native child with observable arguments; warm it explicitly first")
		}
		found = true
		evidence.Arguments = model.Status.Args
	}
	if !found {
		return evidence, fmt.Errorf("selected model is not loaded in the inspected backend")
	}
	evidence.Policy, err = nativePolicy(evidence.Arguments, opts.model)
	if err != nil {
		return evidence, err
	}
	preset := managed.LlamaPresets[opts.model]
	policy, ok := managed.SamplingPolicies[preset.SamplingPolicy]
	if !ok {
		return evidence, fmt.Errorf("selected model lacks its managed sampling policy")
	}
	evidence.Sampling = map[string]any{"policy": preset.SamplingPolicy, "thinking": policy.Defaults(true), "non_thinking": policy.Defaults(false)}
	if _, err := content.RequireBundle(managed, managed.Bundles[preset.Bundle], opts.dataRoot); err != nil {
		return evidence, fmt.Errorf("current local model receipts required: %w", err)
	}
	evidence.Artifacts = map[string]string{}
	for _, id := range managed.Bundles[preset.Bundle].Artifacts {
		artifact := managed.Artifacts[id]
		evidence.Artifacts[id] = artifact.SHA256
	}
	for flag, id := range map[string]string{"model": preset.Artifact, "model-draft": preset.DraftArtifact, "mmproj": preset.MMProjArtifact} {
		if id == "" {
			continue
		}
		path := evidence.Policy[flag]
		mapped, err := mountedPath(c, path)
		if err != nil {
			return evidence, err
		}
		expected, err := filepath.EvalSymlinks(content.ArtifactPath(opts.dataRoot, managed.Artifacts[id]))
		if err != nil {
			return evidence, err
		}
		actual, err := filepath.EvalSymlinks(mapped)
		if err != nil || actual != expected {
			return evidence, fmt.Errorf("native %s is not the verified catalog file", flag)
		}
	}
	// Templates are small image-owned files, unlike model weights. Verify the
	// selected file against tracked bytes rather than trusting the model alias.
	template := filepath.Join(root, "applications", "llama-cpp", "chat-templates", preset.ChatTemplate+".jinja")
	if strings.HasSuffix(preset.ChatTemplate, ".jinja") {
		template = filepath.Join(root, "applications", "llama-cpp", "chat-templates", preset.ChatTemplate)
	}
	tracked, err := os.ReadFile(template)
	if err != nil {
		return evidence, fmt.Errorf("cannot read the tracked selected chat template: %w", err)
	}
	actualTemplate, err := containerBytes(ctx, c.ID, evidence.Policy["chat-template-file"], 1024*1024)
	if err != nil || hashBytes(actualTemplate) != hashBytes(tracked) {
		return evidence, fmt.Errorf("selected native chat template differs from tracked bytes")
	}
	evidence.Template = hashBytes(tracked)
	evidence.Kernel = readObservation("/proc/sys/kernel/osrelease")
	evidence.Firmware = readObservation("/sys/firmware/acpi/platform_profile")
	evidence.CPUProfile = readObservation("/sys/devices/system/cpu/cpufreq/policy0/energy_performance_preference")
	boot := readObservation("/proc/sys/kernel/random/boot_id")
	if boot == "unavailable" || len(boot) != 36 {
		return evidence, fmt.Errorf("same-boot comparison identity unavailable")
	}
	// This ephemeral boot identity remains private evidence, not a machine name
	// or an identifier to publish in an acceptance summary.
	evidence.BootSession = hashBytes([]byte(boot))
	return evidence, nil
}

func validateContainer(c inspectedContainer, opts runOptions) error {
	if canonicalID(c.ID) != canonicalID(opts.containerID) || canonicalID(c.Image) != canonicalID(opts.imageID) || !c.State.Running {
		return fmt.Errorf("backend container/image identity changed or is not running")
	}
	if strings.TrimPrefix(c.Name, "/") != identity.Container("gateway-llama-cpp") {
		return fmt.Errorf("selected container is not the ordinary gateway backend")
	}
	labels := c.Config.Labels
	if labels[podman.ManagedContainerLabel] != "true" || labels[podman.ManagedApplicationLabel] != "llama-cpp" || labels[podman.ManagedRoleLabel] != gateway.GatewayBackendRole {
		return fmt.Errorf("selected container is not an exact gateway-owned llama.cpp backend")
	}
	if !gateway.ValidInstanceID(labels[gateway.GatewayInstanceLabel]) {
		return fmt.Errorf("backend is missing a canonical owning gateway instance")
	}
	if !c.HostConfig.ReadonlyRootfs || c.HostConfig.NetworkMode == "host" || len(c.HostConfig.CapDrop) == 0 || len(c.HostConfig.CapAdd) != 0 || !emptyCaps(c.EffectiveCaps) || !emptyCaps(c.BoundingCaps) {
		return fmt.Errorf("backend confinement differs from the reviewed policy")
	}
	secured := false
	for _, value := range c.HostConfig.SecurityOpt {
		secured = secured || value == "no-new-privileges" || value == "no-new-privileges=true"
	}
	if !secured {
		return fmt.Errorf("backend is missing no-new-privileges")
	}
	return nil
}

func canonicalID(id string) string { return "sha256:" + strings.TrimPrefix(id, "sha256:") }

// Inventory agreement does not bind a gateway to a container: another product
// namespace can serve the same models on a different loopback port. Require
// the lifecycle label that the serving process wrote when creating this child.
func validateGatewayBackend(status gateway.Status, backend backendEvidence) error {
	if !gateway.ValidInstanceID(backend.GatewayInstance) || status.InstanceID != backend.GatewayInstance {
		return fmt.Errorf("gateway instance does not own the inspected backend container")
	}
	if status.Scheduler.Allocation != gateway.AllocationLlamaCPP || status.Scheduler.State != gateway.StateReady {
		return fmt.Errorf("gateway must report the inspected llama.cpp allocation ready")
	}
	return nil
}

func containerBytes(ctx context.Context, id, path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("container diagnostic path must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "podman", "exec", id, "cat", path)
	reader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
	if readErr != nil || int64(len(raw)) > limit {
		cancel()
	}
	if err := cmd.Wait(); err != nil || readErr != nil || int64(len(raw)) > limit {
		return nil, fmt.Errorf("bounded container diagnostic read failed")
	}
	return raw, nil
}

// Rootless --device can appear as a bind mount, an OCI device, or both. The
// requested HostConfig alone cannot rule out additional effective exposures.
// Correlate representations, but never collapse duplicates within one of them.
func exposedGPUDevices(c inspectedContainer) ([]string, error) {
	if !filepath.IsAbs(c.OCIConfigPath) {
		return nil, fmt.Errorf("effective OCI device configuration unavailable")
	}
	// Do not let an unexpected FIFO block before the regular-file check.
	file, err := os.OpenFile(c.OCIConfigPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4*1024*1024 {
		return nil, fmt.Errorf("effective OCI configuration is not a bounded regular file")
	}
	var config struct {
		Mounts []struct{ Destination, Source, Type string }
		Linux  struct {
			Devices []struct {
				Path, Type string
				Major      int64
				Minor      *int64
			}
		}
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4*1024*1024+1))
	if err != nil || len(raw) > 4*1024*1024 {
		return nil, fmt.Errorf("cannot read bounded effective OCI configuration")
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("invalid effective OCI configuration: %w", err)
	}
	var devices []string
	effective := map[string]bool{}
	add := func(path string) {
		if !effective[path] {
			devices = append(devices, path)
			effective[path] = true
		}
	}
	mounts := map[string]bool{}
	for _, mount := range config.Mounts {
		gpu, err := gpuMount(mount.Source, mount.Destination, mount.Type, mounts)
		if err != nil {
			return nil, err
		}
		if gpu {
			add(mount.Destination)
		}
	}
	seen := map[string]bool{}
	// This is the non-TTY managed backend, not a generic OCI device policy.
	// KFD's major is dynamic: ignoring unknown character devices could conceal
	// an aliased KFD node when rootless /dev/kfd is represented only as a bind.
	standard := map[string][2]int64{
		"/dev/null": {1, 3}, "/dev/zero": {1, 5}, "/dev/full": {1, 7},
		"/dev/random": {1, 8}, "/dev/urandom": {1, 9}, "/dev/tty": {5, 0},
	}
	for _, device := range config.Linux.Devices {
		if seen[device.Path] || device.Type != "c" || device.Minor == nil {
			return nil, fmt.Errorf("unexpected or ambiguous effective OCI device")
		}
		seen[device.Path] = true
		if exactGPUDevice(device.Path) {
			if device.Path == "/dev/kfd" {
				if device.Major <= 0 || device.Major == 1 || device.Major == 5 || device.Major == 226 || *device.Minor != 0 {
					return nil, fmt.Errorf("effective KFD device identity differs from its path")
				}
			} else {
				minor, _ := strconv.ParseInt(strings.TrimPrefix(device.Path, "/dev/dri/renderD"), 10, 64)
				if device.Major != 226 || *device.Minor != minor {
					return nil, fmt.Errorf("effective render device identity differs from its path")
				}
			}
			add(device.Path)
		} else if identity, ok := standard[device.Path]; !ok || device.Major != identity[0] || *device.Minor != identity[1] {
			return nil, fmt.Errorf("backend exposes an unexpected effective OCI device")
		}
	}
	// Inspection may omit the Docker-compatible list for rootless devices, but
	// any records it does report must agree with the effective OCI inventory.
	seen = map[string]bool{}
	for _, device := range c.HostConfig.Devices {
		if !exactGPUDevice(device.PathOnHost) || device.PathOnHost != device.PathInContainer || seen[device.PathOnHost] || !effective[device.PathOnHost] {
			return nil, fmt.Errorf("requested GPU devices disagree with effective OCI configuration")
		}
		seen[device.PathOnHost] = true
	}
	seen = map[string]bool{}
	for _, mount := range c.Mounts {
		gpu, err := gpuMount(mount.Source, mount.Destination, mount.Type, seen)
		if err != nil {
			return nil, err
		}
		if gpu && !mounts[mount.Destination] {
			return nil, fmt.Errorf("inspected GPU mounts disagree with effective OCI configuration")
		}
	}
	return devices, nil
}

func gpuDevicePath(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/dev/kfd" || strings.HasPrefix(clean, "/dev/kfd/") || clean == "/dev/dri" || strings.HasPrefix(clean, "/dev/dri/")
}

func exactGPUDevice(path string) bool {
	if path == "/dev/kfd" {
		return true
	}
	minor, err := strconv.ParseUint(strings.TrimPrefix(path, "/dev/dri/renderD"), 10, 63)
	return strings.HasPrefix(path, "/dev/dri/renderD") && err == nil && minor >= 128 && path == "/dev/dri/renderD"+strconv.FormatUint(minor, 10)
}

func gpuMount(source, destination, kind string, seen map[string]bool) (bool, error) {
	if kind == "bind" && (filepath.Clean(source) == "/" || filepath.Clean(source) == "/dev" || filepath.Clean(destination) == "/dev") {
		return false, fmt.Errorf("backend must not expose or replace the complete device tree")
	}
	if !gpuDevicePath(source) && !gpuDevicePath(destination) {
		return false, nil
	}
	if kind != "bind" || !exactGPUDevice(source) || source != destination || seen[destination] {
		return false, fmt.Errorf("unexpected or ambiguous effective GPU mount")
	}
	seen[destination] = true
	return true, nil
}

func emptyCaps(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var caps []string
	return json.Unmarshal(raw, &caps) == nil && len(caps) == 0
}

func nativeRead(ctx context.Context, endpoint string, destination any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(raw) > 4*1024*1024 || response.StatusCode != 200 {
		return fmt.Errorf("native readonly metadata probe failed")
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return fmt.Errorf("native metadata shape changed: %w", err)
	}
	return nil
}

func nativePolicy(args []string, model string) (map[string]string, error) {
	policy := map[string]string{}
	aliases := map[string]string{"-c": "ctx-size", "-np": "parallel", "-b": "batch-size", "-ub": "ubatch-size", "-ctk": "cache-type-k", "-ctv": "cache-type-v", "-m": "model", "-md": "model-draft", "-fa": "flash-attn", "-dev": "device"}
	selected := map[string]bool{}
	for _, name := range []string{"ctx-size", "ctx-checkpoints", "parallel", "batch-size", "ubatch-size", "cache-type-k", "cache-type-v", "kv-unified", "no-kv-unified", "spec-type", "spec-draft-n-max", "spec-draft-sampling", "model", "model-draft", "mmproj", "chat-template-file", "device", "flash-attn", "load-mode", "lazy-mode", "cache-ram", "fit", "reasoning-preserve", "no-reasoning-preserve"} {
		selected[name] = true
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		name, value, inline := strings.Cut(arg, "=")
		if replacement, ok := aliases[name]; ok {
			name = replacement
		} else {
			name = strings.TrimPrefix(name, "--")
		}
		if strings.Contains(name, "api-key") {
			return nil, fmt.Errorf("native authentication arguments must not enter captured acceptance evidence")
		}
		if !selected[name] {
			continue
		}
		if _, seen := policy[name]; seen {
			return nil, fmt.Errorf("ambiguous duplicate native option %s", name)
		}
		if !inline {
			if name == "kv-unified" || name == "no-kv-unified" || name == "reasoning-preserve" || name == "no-reasoning-preserve" {
				value = "true"
			} else {
				index++
				if index >= len(args) || strings.HasPrefix(args[index], "--") {
					return nil, fmt.Errorf("native option %s has no observable value", name)
				}
				value = args[index]
			}
		}
		policy[name] = value
	}
	for name, expected := range map[string]string{"ctx-size": "262144", "parallel": "4", "batch-size": "2048", "ubatch-size": "2048", "cache-type-k": "f16", "cache-type-v": "f16", "kv-unified": "true", "reasoning-preserve": "true", "load-mode": "none", "lazy-mode": "on-direct", "cache-ram": "0", "fit": "off"} {
		if policy[name] != expected {
			return nil, fmt.Errorf("native %s differs from frozen suite policy, expected %s", name, expected)
		}
	}
	if _, opposite := policy["no-kv-unified"]; opposite || policy["no-reasoning-preserve"] != "" {
		return nil, fmt.Errorf("native cache/history policy contradicts the suite")
	}
	if policy["ctx-checkpoints"] == "0" {
		return nil, fmt.Errorf("disabling context checkpoints changes the protected regression path")
	}
	if policy["device"] != "ROCm0" || policy["chat-template-file"] == "" || policy["model"] == "" || policy["mmproj"] == "" {
		return nil, fmt.Errorf("selected ROCm model/template/projector arguments are missing")
	}
	if policy["flash-attn"] != "on" {
		return nil, fmt.Errorf("Flash Attention must remain explicitly on")
	}
	if strings.HasSuffix(model, "-dflash2") {
		if policy["spec-type"] != "draft-dflash" || policy["spec-draft-n-max"] != "3" || policy["spec-draft-sampling"] != "greedy" || policy["model-draft"] == "" {
			return nil, fmt.Errorf("native DFlash2 differs from the reviewed greedy depth-three policy")
		}
	} else if policy["spec-type"] != "" && policy["spec-type"] != "none" {
		return nil, fmt.Errorf("plain control unexpectedly enables speculation")
	}
	return policy, nil
}

func mountedPath(c inspectedContainer, path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("native content path is not absolute")
	}
	longest, source := "", ""
	for _, mount := range c.Mounts {
		if path == mount.Destination || strings.HasPrefix(path, mount.Destination+"/") {
			if mount.RW {
				return "", fmt.Errorf("native content mount is writable")
			}
			if len(mount.Destination) > len(longest) {
				longest, source = mount.Destination, mount.Source
			}
		}
	}
	if source == "" {
		return "", fmt.Errorf("native content does not map to a verified readonly host mount")
	}
	return filepath.Join(source, strings.TrimPrefix(strings.TrimPrefix(path, longest), "/")), nil
}

func readObservation(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 512 {
		return "unavailable"
	}
	return strings.TrimSpace(string(raw))
}

func hashBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func gatewayOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/v1") {
		return "", fmt.Errorf("require an explicit numeric-loopback HTTP gateway origin, optionally /v1")
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	if ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("require an explicit numeric-loopback HTTP gateway origin")
	}
	u.Path = ""
	u.Host = net.JoinHostPort(ip.String(), strconv.Itoa(port))
	return u.String(), nil
}
