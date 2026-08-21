// Package hostdoctor inspects Linux host state and derives non-mutating
// remediation guidance for GPU access and RDNA 3.5 shared memory.
package hostdoctor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const PagesPerGiB int64 = (1 << 30) / 4096

var TTMModules = []string{"amdttm", "amd_ttm", "ttm"}

type TTMState struct {
	Module        string
	PagesLimit    int64
	PagePoolSize  int64
	PagePoolKnown bool
}

type BootTools struct {
	OstreeBooted bool
	RPMOstree    bool
	GRUBDropIn   bool
	UpdateGRUB   bool
	Grubby       bool
	Initramfs    string
}

func KernelRelease() (string, error) {
	var name syscall.Utsname
	if err := syscall.Uname(&name); err != nil {
		return "", fmt.Errorf("query kernel release: %w", err)
	}
	bytes := make([]byte, 0, len(name.Release))
	for _, character := range name.Release {
		if character == 0 {
			break
		}
		bytes = append(bytes, byte(character))
	}
	if len(bytes) == 0 {
		return "", fmt.Errorf("kernel release is empty")
	}
	return string(bytes), nil
}

func ReadTTMState(moduleRoot string) *TTMState {
	for _, module := range TTMModules {
		parameters := filepath.Join(moduleRoot, module, "parameters")
		pages, ok := readPositive(filepath.Join(parameters, "pages_limit"))
		if !ok {
			continue
		}
		pool, poolKnown := readNonNegative(filepath.Join(parameters, "page_pool_size"))
		return &TTMState{Module: module, PagesLimit: pages, PagePoolSize: pool, PagePoolKnown: poolKnown}
	}
	return nil
}

func ReadSystemMemory(path string) (int64, bool) {
	handle, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer handle.Close()
	scanner := bufio.NewScanner(handle)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == "MemTotal:" && fields[2] == "kB" {
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil && value > 0 {
				return value * 1024, true
			}
		}
	}
	return 0, false
}

func ReadGTTBytes(renderNode string) (int64, bool) {
	return readPositive(filepath.Join("/sys/class/drm", filepath.Base(renderNode), "device", "mem_info_gtt_total"))
}

func TargetGiB(systemBytes int64) (int64, bool) {
	systemGiB := float64(systemBytes) / (1 << 30)
	for _, tier := range [][2]int64{{120, 112}, {112, 100}, {56, 48}, {40, 32}} {
		if systemGiB >= float64(tier[0]) {
			return tier[1], true
		}
	}
	return 0, false
}

func KernelParameters(module string, targetGiB int64) []string {
	pages := targetGiB * PagesPerGiB
	var result []string
	if targetGiB >= 112 {
		result = append(result, fmt.Sprintf("amdgpu.gttsize=%d", targetGiB*1024))
	}
	result = append(result, fmt.Sprintf("%s.pages_limit=%d", module, pages))
	if targetGiB >= 112 {
		result = append(result, fmt.Sprintf("%s.page_pool_size=%d", module, pages))
	}
	return result
}

func ModuleOptions(module string, targetGiB int64) []string {
	pages := targetGiB * PagesPerGiB
	var result []string
	if targetGiB >= 112 {
		result = append(result, fmt.Sprintf("options amdgpu gttsize=%d", targetGiB*1024))
	}
	line := fmt.Sprintf("options %s pages_limit=%d", module, pages)
	if targetGiB >= 112 {
		line += fmt.Sprintf(" page_pool_size=%d", pages)
	}
	return append(result, line)
}

func MemoryReady(state *TTMState, gttBytes int64, gttKnown bool, targetGiB int64) bool {
	effective := float64(state.PagesLimit) / float64(PagesPerGiB)
	if gttKnown {
		effective = float64(gttBytes) / (1 << 30)
	}
	poolReady := targetGiB < 112 || state.PagePoolKnown && state.PagePoolSize >= targetGiB*PagesPerGiB
	return effective >= float64(targetGiB) && poolReady
}

func Remediation(state *TTMState, targetGiB int64, tools BootTools, namespace string) []string {
	parameters := KernelParameters(state.Module, targetGiB)
	if tools.OstreeBooted && tools.RPMOstree {
		operations := []string{fmt.Sprintf("--delete-if-present '%s.pages_limit=%d'", state.Module, state.PagesLimit)}
		if state.PagePoolKnown {
			operations = append(operations, fmt.Sprintf("--delete-if-present '%s.page_pool_size=%d'", state.Module, state.PagePoolSize))
		}
		for _, parameter := range parameters {
			operations = append(operations, fmt.Sprintf("--append-if-missing '%s'", parameter))
		}
		return []string{"sudo rpm-ostree kargs \\\n      " + strings.Join(operations, " \\\n      "), "sudo reboot"}
	}
	if tools.GRUBDropIn && tools.UpdateGRUB {
		configuration := `GRUB_CMDLINE_LINUX_DEFAULT="${GRUB_CMDLINE_LINUX_DEFAULT} ` + strings.Join(parameters, " ") + `"`
		return []string{fmt.Sprintf("printf '%%s\\n' '%s' | sudo tee /etc/default/grub.d/70-%s-ttm.cfg", configuration, namespace), "sudo update-grub", "sudo reboot"}
	}
	if tools.Grubby {
		names := make([]string, 0, len(parameters))
		for _, parameter := range parameters {
			name, _, _ := strings.Cut(parameter, "=")
			names = append(names, name)
		}
		return []string{fmt.Sprintf("sudo grubby --update-kernel=ALL \\\n    --remove-args='%s' \\\n    --args='%s'", strings.Join(names, " "), strings.Join(parameters, " ")), "sudo reboot"}
	}
	options := ModuleOptions(state.Module, targetGiB)
	quoted := make([]string, 0, len(options))
	for _, option := range options {
		quoted = append(quoted, "'"+option+"'")
	}
	result := []string{fmt.Sprintf("printf '%%s\\n' %s | sudo tee /etc/modprobe.d/%s-ttm.conf", strings.Join(quoted, " "), namespace)}
	if tools.Initramfs != "" {
		result = append(result, tools.Initramfs)
	} else {
		result = append(result, "Rebuild the initramfs with the host distribution's tool.")
	}
	return append(result, "sudo reboot")
}

func StrixHaloKFDWarning(release string) string {
	version, _, _ := strings.Cut(release, "-")
	if versionAtLeast(version, []int{6, 18, 4}) {
		return ""
	}
	return fmt.Sprintf("kernel %s; verify gfx1151 queue/context-save backports (upstream 6.18.4+)", version)
}

func versionAtLeast(value string, required []int) bool {
	parts := strings.Split(value, ".")
	for index, minimum := range required {
		actual := 0
		if index < len(parts) {
			digits := strings.TrimLeftFunc(parts[index], func(character rune) bool { return character < '0' || character > '9' })
			digits = strings.TrimRightFunc(digits, func(character rune) bool { return character < '0' || character > '9' })
			actual, _ = strconv.Atoi(digits)
		}
		if actual != minimum {
			return actual > minimum
		}
	}
	return true
}

func readPositive(path string) (int64, bool) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(contents)), 10, 64)
	return value, err == nil && value > 0
}

func readNonNegative(path string) (int64, bool) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(contents)), 10, 64)
	return value, err == nil && value >= 0
}
