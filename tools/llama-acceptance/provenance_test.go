package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"paracetamol/internal/gateway"
)

// These fixtures describe effective OCI metadata only. The named device paths
// are never opened and need not exist on the test host.
func writeDeviceConfig(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGatewayBackendInstanceBinding(t *testing.T) {
	instance := strings.Repeat("a", 32)
	backend := backendEvidence{GatewayInstance: instance}
	status := gateway.Status{InstanceID: instance, InventoryFingerprint: strings.Repeat("b", 64), Scheduler: gateway.SchedulerStatus{Allocation: gateway.AllocationLlamaCPP, State: gateway.StateReady}}
	if err := validateGatewayBackend(status, backend); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"another-gateway", "missing-owner", "invalid-owner", "unloaded", "starting", "another-allocation"} {
		t.Run(fault, func(t *testing.T) {
			actualStatus, actualBackend := status, backend
			switch fault {
			case "another-gateway":
				// Both instances can have the same catalog and valid independent
				// metadata. Their nonce, not inventory equality, binds the child.
				actualStatus.InstanceID = strings.Repeat("c", 32)
			case "missing-owner":
				actualBackend.GatewayInstance = ""
			case "invalid-owner":
				actualStatus.InstanceID, actualBackend.GatewayInstance = strings.Repeat("0", 32), strings.Repeat("0", 32)
			case "unloaded":
				actualStatus.Scheduler.State = gateway.StateUnloaded
			case "starting":
				actualStatus.Scheduler.State = gateway.StateStarting
			case "another-allocation":
				actualStatus.Scheduler.Allocation = "different"
			}
			if err := validateGatewayBackend(actualStatus, actualBackend); err == nil {
				t.Fatal("gateway/backend mismatch passed")
			}
		})
	}
}

func TestExposedGPUDevicesRootlessBindMountFallback(t *testing.T) {
	raw := `{
  "mounts": [
    {"destination":"/proc","source":"proc","type":"proc"},
    {"destination":"/dev","source":"tmpfs","type":"tmpfs"},
    {"destination":"/dev/kfd","source":"/dev/kfd","type":"bind"},
    {"destination":"/dev/dri/renderD130","source":"/dev/dri/renderD130","type":"bind"}
  ],
  "linux": {"devices": [
    {"path":"/dev/null","type":"c","major":1,"minor":3},
    {"path":"/dev/zero","type":"c","major":1,"minor":5},
    {"path":"/dev/full","type":"c","major":1,"minor":7},
    {"path":"/dev/random","type":"c","major":1,"minor":8},
    {"path":"/dev/urandom","type":"c","major":1,"minor":9},
    {"path":"/dev/tty","type":"c","major":5,"minor":0},
    {"path":"/dev/kfd","type":"c","major":235,"minor":0},
    {"path":"/dev/dri/renderD130","type":"c","major":226,"minor":130}
  ]}
}`
	container := inspectedContainer{OCIConfigPath: writeDeviceConfig(t, raw)}
	devices, err := exposedGPUDevices(container)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/dev/kfd", "/dev/dri/renderD130"}
	if !reflect.DeepEqual(devices, want) {
		t.Fatalf("effective GPU devices = %#v, want %#v", devices, want)
	}
}

func TestExposedGPUDevicesCorrelatesAllRepresentations(t *testing.T) {
	// A rootless --device may be reported in all four places. Equivalent
	// representations describe one exposure, not four independent devices.
	var container inspectedContainer
	if err := json.Unmarshal([]byte(`{"HostConfig":{"Devices":[{"PathOnHost":"/dev/kfd","PathInContainer":"/dev/kfd"},{"PathOnHost":"/dev/dri/renderD131","PathInContainer":"/dev/dri/renderD131"}]},"Mounts":[{"Source":"/dev/kfd","Destination":"/dev/kfd","Type":"bind"},{"Source":"/dev/dri/renderD131","Destination":"/dev/dri/renderD131","Type":"bind"}]}`), &container); err != nil {
		t.Fatal(err)
	}
	container.OCIConfigPath = writeDeviceConfig(t, `{"mounts":[{"source":"/dev/kfd","destination":"/dev/kfd","type":"bind"},{"source":"/dev/dri/renderD131","destination":"/dev/dri/renderD131","type":"bind"}],"linux":{"devices":[{"path":"/dev/kfd","type":"c","major":235,"minor":0},{"path":"/dev/dri/renderD131","type":"c","major":226,"minor":131}]}}`)
	devices, err := exposedGPUDevices(container)
	if err != nil || !reflect.DeepEqual(devices, []string{"/dev/kfd", "/dev/dri/renderD131"}) {
		t.Fatalf("devices=%#v, error=%v", devices, err)
	}
}

func TestExposedGPUDevicesReconcilesInspectWithEffectiveOCI(t *testing.T) {
	normal := `{"mounts":[{"source":"/dev/kfd","destination":"/dev/kfd","type":"bind"},{"source":"/dev/dri/renderD130","destination":"/dev/dri/renderD130","type":"bind"}]}`
	for name, inspected := range map[string]string{
		"host alias":             `{"HostConfig":{"Devices":[{"PathOnHost":"/dev/dri/renderD130","PathInContainer":"/extra-render"}]}}`,
		"host missing target":    `{"HostConfig":{"Devices":[{"PathOnHost":"/dev/dri/renderD130"}]}}`,
		"host unobserved device": `{"HostConfig":{"Devices":[{"PathOnHost":"/dev/dri/renderD129","PathInContainer":"/dev/dri/renderD129"}]}}`,
		"host duplicate":         `{"HostConfig":{"Devices":[{"PathOnHost":"/dev/kfd","PathInContainer":"/dev/kfd"},{"PathOnHost":"/dev/kfd","PathInContainer":"/dev/kfd"}]}}`,
		"host tree":              `{"HostConfig":{"Devices":[{"PathOnHost":"/dev/dri","PathInContainer":"/dev/dri"}]}}`,
		"inspected alias":        `{"Mounts":[{"Source":"/dev/dri/renderD129","Destination":"/extra-render","Type":"bind"}]}`,
		"inspected tree alias":   `{"Mounts":[{"Source":"/dev/dri","Destination":"/extra-gpus","Type":"bind"}]}`,
		"inspected duplicate":    `{"Mounts":[{"Source":"/dev/kfd","Destination":"/dev/kfd","Type":"bind"},{"Source":"/dev/kfd","Destination":"/dev/kfd","Type":"bind"}]}`,
		"inspected unobserved":   `{"Mounts":[{"Source":"/dev/dri/renderD129","Destination":"/dev/dri/renderD129","Type":"bind"}]}`,
		"inspected wrong source": `{"Mounts":[{"Source":"/elsewhere","Destination":"/dev/dri/renderD130","Type":"bind"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var container inspectedContainer
			if err := json.Unmarshal([]byte(inspected), &container); err != nil {
				t.Fatal(err)
			}
			container.OCIConfigPath = writeDeviceConfig(t, normal)
			if _, err := exposedGPUDevices(container); err == nil {
				t.Fatal("inconsistent or ambiguous inspected exposure accepted")
			}
		})
	}
	// A populated HostConfig must not bypass an effective OCI-only exposure.
	var container inspectedContainer
	if err := json.Unmarshal([]byte(`{"HostConfig":{"Devices":[{"PathOnHost":"/dev/kfd","PathInContainer":"/dev/kfd"},{"PathOnHost":"/dev/dri/renderD130","PathInContainer":"/dev/dri/renderD130"}]}}`), &container); err != nil {
		t.Fatal(err)
	}
	container.OCIConfigPath = writeDeviceConfig(t, strings.TrimSuffix(normal, `]}`)+`,{"source":"/dev/dri/renderD129","destination":"/extra-render","type":"bind"}]}`)
	if _, err := exposedGPUDevices(container); err == nil {
		t.Fatal("HostConfig concealed an additional aliased effective GPU")
	}
}

func TestExposedGPUDevicesLinuxDeviceFallbackAndMountDeduplication(t *testing.T) {
	for name, raw := range map[string]string{
		"linux-only": `{"linux":{"devices":[{"path":"/dev/null","type":"c","major":1,"minor":3},{"path":"/dev/kfd","type":"c","major":235,"minor":0},{"path":"/dev/dri/renderD130","type":"c","major":226,"minor":130}]}}`,
		"mixed":      `{"mounts":[{"destination":"/dev/kfd","source":"/dev/kfd","type":"bind"}],"linux":{"devices":[{"path":"/dev/kfd","type":"c","major":235,"minor":0},{"path":"/dev/dri/renderD130","type":"c","major":226,"minor":130}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			devices, err := exposedGPUDevices(inspectedContainer{OCIConfigPath: writeDeviceConfig(t, raw)})
			if err != nil || !reflect.DeepEqual(devices, []string{"/dev/kfd", "/dev/dri/renderD130"}) {
				t.Fatalf("devices=%#v, error=%v", devices, err)
			}
		})
	}
}

func TestExposedGPUDevicesRejectsUnsafeOrAmbiguousMounts(t *testing.T) {
	for name, mounts := range map[string]string{
		"whole device tree":         `[{"destination":"/dev","source":"/dev","type":"bind"}]`,
		"whole tree elsewhere":      `[{"destination":"/host-devices","source":"/dev","type":"bind"}]`,
		"whole tree trailing slash": `[{"destination":"/host-devices","source":"/dev/","type":"bind"}]`,
		"whole tree dot component":  `[{"destination":"/host-devices","source":"/dev/.","type":"bind"}]`,
		"whole host tree":           `[{"destination":"/host","source":"/","type":"bind"}]`,
		"GPU tree alias":            `[{"destination":"/extra-gpus","source":"/dev/dri","type":"bind"}]`,
		"GPU subtree alias":         `[{"destination":"/extra-gpus","source":"/dev/dri/by-path","type":"bind"}]`,
		"extra render alias":        `[{"destination":"/extra-render","source":"/dev/dri/renderD129","type":"bind"}]`,
		"kfd alias":                 `[{"destination":"/extra-kfd","source":"/dev/kfd","type":"bind"}]`,
		"GPU tree destination":      `[{"destination":"/dev/dri","source":"/other","type":"bind"}]`,
		"render dot component":      `[{"destination":"/dev/dri/renderD130","source":"/dev/dri/../dri/renderD130","type":"bind"}]`,
		"wrong kfd source":          `[{"destination":"/dev/kfd","source":"/other/kfd","type":"bind"}]`,
		"wrong render source":       `[{"destination":"/dev/dri/renderD130","source":"/dev/dri/renderD131","type":"bind"}]`,
		"missing source":            `[{"destination":"/dev/kfd","type":"bind"}]`,
		"nonbind kfd":               `[{"destination":"/dev/kfd","source":"/dev/kfd","type":"tmpfs"}]`,
		"nonbind render":            `[{"destination":"/dev/dri/renderD130","source":"/dev/dri/renderD130","type":"tmpfs"}]`,
		"duplicate kfd":             `[{"destination":"/dev/kfd","source":"/dev/kfd","type":"bind"},{"destination":"/dev/kfd","source":"/dev/kfd","type":"bind"}]`,
		"duplicate render":          `[{"destination":"/dev/dri/renderD130","source":"/dev/dri/renderD130","type":"bind"},{"destination":"/dev/dri/renderD130","source":"/dev/dri/renderD130","type":"bind"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := exposedGPUDevices(inspectedContainer{OCIConfigPath: writeDeviceConfig(t, `{"mounts":`+mounts+`}`)})
			if err == nil {
				t.Fatal("unsafe effective GPU mount accepted")
			}
		})
	}
}

func TestExposedGPUDevicesRejectsAmbiguousOCIDevices(t *testing.T) {
	for name, devices := range map[string]string{
		"render alias":        `[{"path":"/extra-render","type":"c","major":226,"minor":129}]`,
		"kfd alias":           `[{"path":"/dev/kfd","type":"c","major":235,"minor":0},{"path":"/extra-kfd","type":"c","major":235,"minor":0}]`,
		"unknown KFD major":   `[{"path":"/extra-kfd","type":"c","major":242,"minor":0}]`,
		"duplicate":           `[{"path":"/dev/dri/renderD130","type":"c","major":226,"minor":130},{"path":"/dev/dri/renderD130","type":"c","major":226,"minor":130}]`,
		"tree":                `[{"path":"/dev/dri","type":"c","major":226,"minor":130}]`,
		"card node":           `[{"path":"/dev/dri/card0","type":"c","major":226,"minor":0}]`,
		"wrong render major":  `[{"path":"/dev/dri/renderD130","type":"c","major":235,"minor":130}]`,
		"wrong render minor":  `[{"path":"/dev/dri/renderD130","type":"c","major":226,"minor":129}]`,
		"wrong render type":   `[{"path":"/dev/dri/renderD130","type":"b","major":226,"minor":130}]`,
		"wrong kfd major":     `[{"path":"/dev/kfd","type":"c","major":226,"minor":0}]`,
		"wrong kfd minor":     `[{"path":"/dev/kfd","type":"c","major":235,"minor":1}]`,
		"missing type":        `[{"path":"/dev/kfd","major":235,"minor":0}]`,
		"missing minor":       `[{"path":"/dev/kfd","type":"c","major":235}]`,
		"default GPU alias":   `[{"path":"/dev/null","type":"c","major":226,"minor":130}]`,
		"default KFD alias":   `[{"path":"/dev/null","type":"c","major":242,"minor":0}]`,
		"default wrong minor": `[{"path":"/dev/null","type":"c","major":1,"minor":5}]`,
		"default wrong type":  `[{"path":"/dev/null","type":"b","major":1,"minor":3}]`,
		"default duplicate":   `[{"path":"/dev/null","type":"c","major":1,"minor":3},{"path":"/dev/null","type":"c","major":1,"minor":3}]`,
	} {
		t.Run(name, func(t *testing.T) {
			// Rootless binds need not provide a Linux /dev/kfd device record
			// from which to learn its dynamic major number.
			if _, err := exposedGPUDevices(inspectedContainer{OCIConfigPath: writeDeviceConfig(t, `{"mounts":[{"source":"/dev/kfd","destination":"/dev/kfd","type":"bind"}],"linux":{"devices":`+devices+`}}`)}); err == nil {
				t.Fatal("ambiguous effective OCI device accepted")
			}
		})
	}
}

func TestExposedGPUDevicesRejectsUnavailableOrInvalidOCIConfig(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "config.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"absent path":   "",
		"relative path": "config.json",
		"missing file":  filepath.Join(t.TempDir(), "missing.json"),
		"directory":     t.TempDir(),
		"FIFO":          fifo,
		"invalid json":  writeDeviceConfig(t, `{"mounts":`),
		"trailing json": writeDeviceConfig(t, `{"mounts":[]} {"mounts":[]}`),
		"oversized":     writeDeviceConfig(t, strings.Repeat(" ", 4*1024*1024+1)),
	} {
		t.Run(name, func(t *testing.T) {
			for _, requested := range []string{`{}`, `{"HostConfig":{"Devices":[{"PathOnHost":"/dev/kfd","PathInContainer":"/dev/kfd"},{"PathOnHost":"/dev/dri/renderD130","PathInContainer":"/dev/dri/renderD130"}]}}`} {
				var container inspectedContainer
				if err := json.Unmarshal([]byte(requested), &container); err != nil {
					t.Fatal(err)
				}
				container.OCIConfigPath = path
				if _, err := exposedGPUDevices(container); err == nil {
					t.Fatal("unavailable or invalid effective OCI configuration accepted")
				}
			}
		})
	}
}

func TestNativePolicyKeyGuardsAndNoSecretDisclosure(t *testing.T) {
	for _, extra := range [][]string{
		{"--api-key", "PRIVATE_SENTINEL"},
		{"--api-key=PRIVATE_SENTINEL"},
		{"--api-key-file", "/private/PRIVATE_SENTINEL"},
		{"--ctx-checkpoints", "0"},
		{"-c", "262144"},
		{"--cache-type-k=f16"},
		{"--no-reasoning-preserve"},
		{"--no-kv-unified"},
	} {
		args := append(validNativeArgs(regularModel), extra...)
		_, err := nativePolicy(args, regularModel)
		if err == nil {
			t.Fatalf("native policy accepted forbidden or duplicate option %q", extra[0])
		}
		if strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("native policy diagnostic disclosed a credential value")
		}
	}
	for _, extra := range [][]string{{"--model-draft"}, {"--ctx-checkpoints"}, {"--ctx-checkpoints", "--other-option"}} {
		if _, err := nativePolicy(append(validNativeArgs(regularModel), extra...), regularModel); err == nil {
			t.Fatalf("native policy accepted missing value for %q", extra[0])
		}
	}
}
