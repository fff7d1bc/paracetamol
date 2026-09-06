package hostdoctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resourceFixture(t *testing.T, root, path, value string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResourceSnapshotSamplesOnlySelectedGPUAndUsesKernelUnits(t *testing.T) {
	root := t.TempDir()
	resourceFixture(t, root, "proc/meminfo", "MemTotal: 134217728 kB\nMemAvailable: 33554432 kB\n")
	device := "drm/renderD128/device/"
	for path, value := range map[string]string{"gpu_busy_percent": "93\n", "hwmon/hwmon8/name": "amdgpu\n", "hwmon/hwmon8/temp1_input": "65500\n", "hwmon/hwmon8/power1_average": "87000000\n", "hwmon/hwmon8/power1_input": "99000000\n"} {
		resourceFixture(t, root, device+path, value)
	}
	resourceFixture(t, root, "drm/renderD129/device/gpu_busy_percent", "66")
	got := readResources(filepath.Join(root, "proc"), filepath.Join(root, "drm"), []string{"/dev/dri/renderD128", "/dev/dri/renderD128"})
	if got.MemoryTotalBytes == nil || *got.MemoryTotalBytes != 128<<30 || got.MemoryAvailableBytes == nil || *got.MemoryAvailableBytes != 32<<30 || len(got.GPUs) != 1 {
		t.Fatalf("snapshot=%+v", got)
	}
	gpu := got.GPUs[0]
	if gpu.Device != "renderD128" || gpu.BusyPercent == nil || *gpu.BusyPercent != 93 || gpu.TemperatureCelsius == nil || *gpu.TemperatureCelsius != 65.5 || gpu.SoCPowerWatts == nil || *gpu.SoCPowerWatts != 87 || gpu.PowerSample != "average" {
		t.Fatalf("gpu=%+v", gpu)
	}
	encoded, _ := json.Marshal(got)
	for _, forbidden := range []string{root, "hwmon", "vram", "gtt", "renderD129"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestResourceSnapshotMissingInvalidAndCPUReadingsRemainUnavailable(t *testing.T) {
	root := t.TempDir()
	resourceFixture(t, root, "proc/meminfo", "MemTotal: 1000 kB\nMemAvailable: 2000 kB\n")
	for path, value := range map[string]string{"gpu_busy_percent": "101", "hwmon/hwmon0/name": "amdgpu", "hwmon/hwmon0/temp1_input": "NaN", "hwmon/hwmon0/power1_average": "-1", "hwmon/hwmon0/power1_input": "32000000"} {
		resourceFixture(t, root, "drm/renderD128/device/"+path, value)
	}
	got := readResources(filepath.Join(root, "proc"), filepath.Join(root, "drm"), []string{"/dev/dri/renderD128", "/dev/dri/renderD130", "../../etc", "renderD12oops"})
	if got.MemoryAvailableBytes != nil || len(got.GPUs) != 2 {
		t.Fatalf("snapshot=%+v", got)
	}
	if gpu := got.GPUs[0]; gpu.BusyPercent != nil || gpu.TemperatureCelsius != nil || gpu.SoCPowerWatts == nil || *gpu.SoCPowerWatts != 32 || gpu.PowerSample != "instantaneous" {
		t.Fatalf("gpu=%+v", gpu)
	}
	if gpu := got.GPUs[1]; gpu.BusyPercent != nil || gpu.TemperatureCelsius != nil || gpu.SoCPowerWatts != nil {
		t.Fatalf("missing gpu=%+v", gpu)
	}
	cpu := readResources(filepath.Join(root, "missing"), filepath.Join(root, "drm"), nil)
	if cpu.MemoryTotalBytes != nil || cpu.MemoryAvailableBytes != nil || len(cpu.GPUs) != 0 {
		t.Fatalf("CPU=%+v", cpu)
	}
	if got := readResource(filepath.Join(root, "proc/meminfo"), 1); got != "" {
		t.Fatalf("oversized resource=%q", got)
	}
}
