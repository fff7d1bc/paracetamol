package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
	"paracetamol/internal/platform"
	runtimeplan "paracetamol/internal/runtime"
)

func (app *App) commandDoctor(args []string) error {
	set := app.flags("doctor", usage("doctor", "[--render-node PATH]", "[--data-dir PATH]", "[--image TAG]"))
	var nodes stringList
	set.Var(&nodes, "render-node", "exact render node; repeatable")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "managed PyTorch diagnostic image")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("doctor accepts no positional arguments")
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	podmanVersion, err := app.podman().Capture(app.Context, []string{"--version"}, "cannot query Podman")
	if err != nil {
		return err
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Host"))
	fmt.Fprintf(app.Stdout, "  %s  %s (rootless)\n", terminal.Label(fmt.Sprintf("%-14s", "Podman")), strings.TrimPrefix(podmanVersion, "podman version "))
	fmt.Fprintf(app.Stdout, "  %s  %s/%s\n", terminal.Label(fmt.Sprintf("%-14s", "Kernel")), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(app.Stdout, "  %s  %s (%s)\n", terminal.Label(fmt.Sprintf("%-14s", "Data")), dataRoot, terminal.State(writableState(dataRoot)))
	if restriction, readErr := os.ReadFile("/proc/sys/kernel/apparmor_restrict_unprivileged_userns"); readErr == nil && strings.TrimSpace(string(restriction)) != "0" {
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Warning(fmt.Sprintf("%-14s", "AppArmor")), "restricts unprivileged user namespaces; bubblewrap may need host policy")
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("GPU access"))
	reportDevice := func(path string) {
		state := "read/write"
		if err := platform.CheckDeviceAccess(path); err != nil {
			state = err.Error()
		}
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-14s", path)), terminal.State(state))
	}
	reportDevice("/dev/kfd")
	discovered, _ := filepath.Glob("/dev/dri/renderD*")
	sort.Strings(discovered)
	if len(discovered) == 0 {
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-14s", "/dev/dri/renderD*")), terminal.State("missing"))
	}
	for _, node := range discovered {
		reportDevice(node)
	}
	allowed, err := app.podman().SELinuxContainerDeviceAccess(app.Context)
	if err != nil {
		return err
	}
	if allowed != nil && !*allowed {
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-14s", "SELinux")), terminal.State("blocked")+" (container_use_devices is off)")
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-14s", "Host action")), terminal.Command("sudo setsebool -P container_use_devices 1"))
		return controlerr.New("SELinux blocks GPU device access")
	}
	image := *imageFlag
	if image == "" {
		for _, candidate := range []string{config.ROCmBaseImage, mustApplication("comfyui").Image} {
			present, _ := app.podman().Exists(app.Context, "image", candidate)
			if present {
				image = candidate
				break
			}
		}
	}
	if image == "" {
		fmt.Fprintf(app.Stdout, "\n%s\n  %s  %s\n  %s  %s\n", terminal.Heading("GPU probe"), terminal.Label(fmt.Sprintf("%-14s", "Image")), terminal.State("not built"), terminal.Label(fmt.Sprintf("%-14s", "Operation")), terminal.State("skipped"))
		terminal.Next(identity.Command("build", "pytorch-base"))
		return nil
	}
	present, err := app.podman().Exists(app.Context, "image", image)
	if err != nil {
		return err
	}
	if !present {
		return controlerr.New("GPU diagnostic image is not built: %s", image)
	}
	selected, err := app.resolveDevices("auto", nodes, nodes != nil)
	if err != nil {
		return err
	}
	result, err := app.run(runtimeplan.GPUDiagnosticCommand(image, selected), true)
	if err != nil {
		return controlerr.New("GPU diagnostics failed: %v", err)
	}
	fields, err := runtimeplan.ParseGPUDiagnostic(string(result.Stdout))
	if err != nil {
		return err
	}
	fmt.Fprintf(app.Stdout, "\n%s\n  %s  %s\n  %s  %s\n", terminal.Heading("GPU probe"), terminal.Label(fmt.Sprintf("%-14s", "Image")), image, terminal.Label(fmt.Sprintf("%-14s", "Render nodes")), strings.Join(selected, ", "))
	for _, label := range []string{"PyTorch", "ROCm/HIP", "Device", "Architecture", "GPU operation", "GPU devices"} {
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-14s", label)), fields[label])
	}
	if fields["Architecture"] == "gfx1150" || fields["Architecture"] == "gfx1151" {
		reportTTMMemory(app.Stdout, selected[0])
	}
	return nil
}

func mustApplication(id string) config.Application {
	value, _ := config.ApplicationByID(id)
	return value
}

func writableState(path string) string {
	probe := path
	for {
		info, err := os.Stat(probe)
		if err == nil {
			if !info.IsDir() {
				return "not a directory"
			}
			if syscallAccess(probe) {
				if probe == path {
					return "writable"
				}
				return "not created; parent writable"
			}
			return "insufficient access"
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "unavailable"
		}
		probe = parent
	}
}

func syscallAccess(path string) bool {
	// access(2) uses bit 2 for write and bit 1 for search/execute.
	return syscall.Access(path, 3) == nil
}

func reportTTMMemory(output interface{ Write([]byte) (int, error) }, renderNode string) {
	memTotal := int64(0)
	if handle, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(handle)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[0] == "MemTotal:" {
				memTotal, _ = strconv.ParseInt(fields[1], 10, 64)
				memTotal *= 1024
			}
		}
		_ = handle.Close()
	}
	gtt, _ := os.ReadFile(filepath.Join("/sys/class/drm", filepath.Base(renderNode), "device", "mem_info_gtt_total"))
	gttValue, _ := strconv.ParseInt(strings.TrimSpace(string(gtt)), 10, 64)
	fmt.Fprintf(output, "\nShared memory\n  System RAM     %.2f GiB\n  GTT ready      %.2f GiB\n", float64(memTotal)/(1<<30), float64(gttValue)/(1<<30))
}
