package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/hostdoctor"
	"paracetamol/internal/identity"
	runtimeplan "paracetamol/internal/runtime"
	"paracetamol/internal/ui"
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
	kernelRelease, err := hostdoctor.KernelRelease()
	if err != nil {
		return err
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Host"))
	writeDoctorField(app.Stdout, terminal, "Podman", strings.TrimPrefix(podmanVersion, "podman version ")+" ("+terminal.State("rootless")+")")
	writeDoctorField(app.Stdout, terminal, "Kernel", kernelRelease)
	writeDoctorField(app.Stdout, terminal, "Data", dataRoot+" ("+terminal.State(writableState(dataRoot))+")")
	reportAppArmorPolicy(app.Stdout, terminal)
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("GPU access"))
	reportDoctorDevices(app.Stdout, terminal)
	allowed, err := app.podman().SELinuxContainerDeviceAccess(app.Context)
	if err != nil {
		return err
	}
	if allowed != nil && !*allowed {
		writeDoctorField(app.Stdout, terminal, "SELinux", terminal.State("blocked")+"; container_use_devices is off")
		writeDoctorField(app.Stdout, terminal, "Host action", terminal.Command("sudo setsebool -P container_use_devices 1"))
		return controlerr.New("SELinux blocks GPU device access")
	} else if allowed != nil {
		writeDoctorField(app.Stdout, terminal, "SELinux", terminal.State("allowed"))
	}
	ttmState := hostdoctor.ReadTTMState("/sys/module")
	image := *imageFlag
	if image == "" {
		for _, candidate := range []string{config.ROCmBaseImage, mustApplication("comfyui").Image} {
			present, inspectErr := app.podman().Exists(app.Context, "image", candidate)
			if inspectErr != nil {
				return inspectErr
			}
			if present {
				image = candidate
				break
			}
		}
	}
	if image == "" {
		fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("GPU probe"))
		writeDoctorField(app.Stdout, terminal, "Image", terminal.State("not built"))
		writeDoctorField(app.Stdout, terminal, "Operation", terminal.State("skipped"))
		fmt.Fprintln(app.Stdout, "\nThe containerized GPU probe needs a managed PyTorch image.")
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
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("GPU probe"))
	writeDoctorField(app.Stdout, terminal, "Image", image)
	writeDoctorField(app.Stdout, terminal, "Render nodes", strings.Join(selected, ", "))
	for _, label := range []string{"PyTorch", "ROCm/HIP", "Device", "Architecture", "GPU operation", "GPU devices"} {
		value := fields[label]
		if label == "GPU operation" || label == "GPU devices" {
			value = terminal.State(value)
		}
		writeDoctorField(app.Stdout, terminal, label, value)
	}
	if fields["Architecture"] == "gfx1151" {
		if warning := hostdoctor.StrixHaloKFDWarning(kernelRelease); warning != "" {
			writeDoctorField(app.Stdout, terminal, "KFD baseline", terminal.Warning(warning))
		}
	}
	if fields["Architecture"] == "gfx1150" || fields["Architecture"] == "gfx1151" {
		name := "Strix Point"
		if fields["Architecture"] == "gfx1151" {
			name = "Strix Halo"
		}
		reportTTMMemory(app.Stdout, terminal, selected[0], ttmState, name)
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
			if probe == path {
				if syscall.Access(probe, 7) == nil {
					return "writable"
				}
				return "insufficient access"
			}
			if syscall.Access(probe, 3) == nil {
				return "not created; parent writable"
			}
			return "not created; parent not writable"
		}
		if !os.IsNotExist(err) {
			return "insufficient access"
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "unavailable"
		}
		probe = parent
	}
}

func writeDoctorField(output interface{ Write([]byte) (int, error) }, terminal ui.Terminal, label, value string) {
	fmt.Fprintf(output, "  %s %s\n", terminal.Label(fmt.Sprintf("%-14s", label)), value)
}

func reportDoctorDevices(output interface{ Write([]byte) (int, error) }, terminal ui.Terminal) {
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	sort.Strings(nodes)
	devices := append([]string{"/dev/kfd"}, nodes...)
	insufficient := false
	for _, device := range devices {
		if _, err := os.Stat(device); os.IsNotExist(err) {
			writeDoctorField(output, terminal, map[bool]string{true: "KFD", false: "Render node"}[device == "/dev/kfd"], device+" ("+terminal.State("missing")+")")
			continue
		}
		state := "read/write"
		if err := syscall.Access(device, 6); err != nil {
			state = "insufficient access"
			insufficient = true
		}
		label := "Render node"
		if device == "/dev/kfd" {
			label = "KFD"
		}
		writeDoctorField(output, terminal, label, device+" ("+terminal.State(state)+")")
	}
	if len(nodes) == 0 {
		writeDoctorField(output, terminal, "Render node", "/dev/dri/renderD* ("+terminal.State("missing")+")")
	}
	if insufficient {
		writeDoctorField(output, terminal, "Access scope", terminal.Warning("the persistent rule below permits every local user"))
		writeDoctorField(output, terminal, "Host action", terminal.Command(fmt.Sprintf("printf '%%s\\n' 'KERNEL==\"kfd\", MODE=\"0666\"' 'SUBSYSTEM==\"drm\", KERNEL==\"renderD*\", MODE=\"0666\"' | sudo tee /etc/udev/rules.d/70-%s-gpu.rules", identity.StateNamespace)))
		writeDoctorField(output, terminal, "Apply", terminal.Command("sudo udevadm control --reload-rules && sudo udevadm trigger"))
	}
}

func reportAppArmorPolicy(output interface{ Write([]byte) (int, error) }, terminal ui.Terminal) {
	path := "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	value := strings.TrimSpace(string(contents))
	if err != nil || value != "0" && value != "1" {
		writeDoctorField(output, terminal, "AppArmor", terminal.Warning("user namespace restriction state is unreadable"))
		return
	}
	if value == "0" {
		writeDoctorField(output, terminal, "AppArmor", terminal.State("user namespace restriction is off"))
		return
	}
	writeDoctorField(output, terminal, "AppArmor", terminal.Warning("restricts unprivileged user namespaces"))
	writeDoctorField(output, terminal, "Impact", terminal.Warning("bubblewrap without a matching AppArmor profile may be blocked"))
	writeDoctorField(output, terminal, "Host action", terminal.Command(fmt.Sprintf("printf '%%s\\n' 'kernel.apparmor_restrict_unprivileged_userns = 0' | sudo tee /etc/sysctl.d/70-%s-userns.conf", identity.StateNamespace)))
	writeDoctorField(output, terminal, "Apply", terminal.Command("sudo sysctl --system"))
	writeDoctorField(output, terminal, "Security", terminal.Warning("this disables the AppArmor restriction system-wide"))
}

func reportTTMMemory(output interface{ Write([]byte) (int, error) }, terminal ui.Terminal, renderNode string, state *hostdoctor.TTMState, platformName string) {
	systemBytes, systemKnown := hostdoctor.ReadSystemMemory("/proc/meminfo")
	gttBytes, gttKnown := hostdoctor.ReadGTTBytes(renderNode)
	fmt.Fprintf(output, "\n%s\n", terminal.Heading(platformName+" shared memory"))
	if systemKnown {
		writeDoctorField(output, terminal, "System RAM", fmt.Sprintf("%.2f GiB", float64(systemBytes)/(1<<30)))
	}
	if state != nil {
		writeDoctorField(output, terminal, "TTM ceiling", fmt.Sprintf("%.2f GiB (%s; %d pages)", float64(state.PagesLimit)/float64(hostdoctor.PagesPerGiB), state.Module, state.PagesLimit))
		if state.PagePoolKnown {
			writeDoctorField(output, terminal, "TTM pool", fmt.Sprintf("%.2f GiB (%d pages)", float64(state.PagePoolSize)/float64(hostdoctor.PagesPerGiB), state.PagePoolSize))
		}
	}
	if gttKnown {
		writeDoctorField(output, terminal, "GTT ready", fmt.Sprintf("%.2f GiB", float64(gttBytes)/(1<<30)))
	}
	if !systemKnown {
		writeDoctorField(output, terminal, "Status", terminal.Warning("could not read total system RAM; see the tuning guide"))
		return
	}
	target, ok := hostdoctor.TargetGiB(systemBytes)
	if !ok {
		writeDoctorField(output, terminal, "Status", terminal.Info("no automatic TTM starting point is defined for this RAM size; see the tuning guide"))
		return
	}
	if state == nil {
		writeDoctorField(output, terminal, "Status", terminal.Warning("could not identify the active TTM pages_limit parameter; see the tuning guide"))
		return
	}
	if hostdoctor.MemoryReady(state, gttBytes, gttKnown, target) {
		writeDoctorField(output, terminal, "Status", terminal.Success(fmt.Sprintf("meets the %d GiB starting point", target)))
		return
	}
	writeDoctorField(output, terminal, "Status", terminal.Warning(fmt.Sprintf("effective GTT or TTM pool is below the %d GiB starting point", target)))
	fmt.Fprintf(output, "\n%s administrator access and a reboot are required:\n", terminal.Heading("Host action:"))
	tools := detectBootTools()
	for _, command := range hostdoctor.Remediation(state, target, tools, identity.StateNamespace) {
		if strings.HasPrefix(command, "Rebuild ") {
			fmt.Fprintf(output, "  %s\n", terminal.Warning(command))
		} else {
			fmt.Fprintf(output, "  %s\n", terminal.Command(command))
		}
	}
	fmt.Fprintf(output, "\n%s These are dynamic GPU-mapping and page-pool ceilings, not reserved memory.\n", terminal.Label("Note:"))
}

func detectBootTools() hostdoctor.BootTools {
	exists := func(path string, directory bool) bool {
		status, err := os.Stat(path)
		return err == nil && status.IsDir() == directory
	}
	has := func(name string) bool { _, err := exec.LookPath(name); return err == nil }
	tools := hostdoctor.BootTools{
		OstreeBooted: exists("/run/ostree-booted", false), RPMOstree: has("rpm-ostree"),
		GRUBDropIn: exists("/etc/default/grub.d", true), UpdateGRUB: has("update-grub"), Grubby: has("grubby"),
	}
	for _, candidate := range [][2]string{{"update-initramfs", "sudo update-initramfs -u"}, {"dracut", "sudo dracut --force"}, {"mkinitcpio", "sudo mkinitcpio -P"}} {
		if has(candidate[0]) {
			tools.Initramfs = candidate[1]
			break
		}
	}
	return tools
}
