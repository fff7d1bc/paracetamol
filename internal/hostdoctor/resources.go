package hostdoctor

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ResourceSnapshot is a best-effort, on-demand host view, not model memory
// accounting. In particular, amdgpu VRAM/GTT counters omit some APU SVM usage.
type ResourceSnapshot struct {
	CollectedAt          string         `json:"collected_at"`
	MemoryTotalBytes     *uint64        `json:"memory_total_bytes"`
	MemoryAvailableBytes *uint64        `json:"memory_available_bytes"`
	GPUs                 []GPUResources `json:"gpus"`
}

type GPUResources struct {
	Device             string   `json:"device"`
	BusyPercent        *float64 `json:"busy_percent"`
	TemperatureCelsius *float64 `json:"temperature_celsius"`
	SoCPowerWatts      *float64 `json:"soc_power_watts"`
	PowerSample        string   `json:"power_sample,omitempty"`
}

// ReadResources samples only the selected render nodes. Missing, unreadable or
// malformed sensors remain null rather than looking idle or preventing status.
func ReadResources(renderNodes []string) ResourceSnapshot {
	return readResources("/proc", "/sys/class/drm", renderNodes)
}

func readResources(procRoot, drmRoot string, renderNodes []string) ResourceSnapshot {
	result := ResourceSnapshot{CollectedAt: time.Now().UTC().Format(time.RFC3339Nano), GPUs: []GPUResources{}}
	for _, line := range strings.Split(readResource(filepath.Join(procRoot, "meminfo"), 64*1024), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || value > math.MaxUint64/1024 {
			continue
		}
		value *= 1024
		switch fields[0] {
		case "MemTotal:":
			if value > 0 {
				result.MemoryTotalBytes = &value
			}
		case "MemAvailable:":
			result.MemoryAvailableBytes = &value
		}
	}
	if result.MemoryTotalBytes != nil && result.MemoryAvailableBytes != nil && *result.MemoryAvailableBytes > *result.MemoryTotalBytes {
		result.MemoryAvailableBytes = nil
	}
	seen := map[string]bool{}
	for _, node := range renderNodes {
		name := filepath.Base(node)
		// The caller supplies validated device selection. Still refuse traversal
		// or arbitrary sysfs names at this read-only boundary.
		if !strings.HasPrefix(name, "renderD") || seen[name] {
			continue
		}
		if _, err := strconv.ParseUint(strings.TrimPrefix(name, "renderD"), 10, 32); err != nil {
			continue
		}
		seen[name] = true
		device := filepath.Join(drmRoot, name, "device")
		gpu := GPUResources{Device: name}
		gpu.BusyPercent = resourceNumber(filepath.Join(device, "gpu_busy_percent"), 1, 0, 100)
		monitors, _ := filepath.Glob(filepath.Join(device, "hwmon", "hwmon*"))
		for _, monitor := range monitors {
			if strings.TrimSpace(readResource(filepath.Join(monitor, "name"), 256)) != "amdgpu" {
				continue
			}
			gpu.TemperatureCelsius = resourceNumber(filepath.Join(monitor, "temp1_input"), 1000, -273.15, 1000)
			// Kernel amdgpu power1 sensors describe the SoC and include CPU
			// power on APUs. Do not label this GPU-only or wall-socket power.
			for _, sensor := range []struct{ file, sample string }{{"power1_average", "average"}, {"power1_input", "instantaneous"}} {
				if value := resourceNumber(filepath.Join(monitor, sensor.file), 1e6, 0, 1e6); value != nil {
					gpu.SoCPowerWatts, gpu.PowerSample = value, sensor.sample
					break
				}
			}
			break
		}
		result.GPUs = append(result.GPUs, gpu)
	}
	return result
}

func readResource(path string, limit int64) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(contents)) > limit {
		return ""
	}
	return string(contents)
}

func resourceNumber(path string, scale, minimum, maximum float64) *float64 {
	// These kernel interfaces publish integer units, not arbitrary float text.
	raw, err := strconv.ParseInt(strings.TrimSpace(readResource(path, 256)), 10, 64)
	if err != nil {
		return nil
	}
	value := float64(raw) / scale
	if value < minimum || value > maximum {
		return nil
	}
	return &value
}
