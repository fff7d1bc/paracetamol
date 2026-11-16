package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
    {"path":"/dev/null","major":1},
    {"path":"/dev/kfd","major":235},
    {"path":"/dev/dri/renderD130","major":226}
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

func TestExposedGPUDevicesUsesExplicitHostConfigWithoutFallback(t *testing.T) {
	// PathOnHost, not PathInContainer or an inferred render-node number, owns
	// this observation when the Docker-compatible inspect field is populated.
	var container inspectedContainer
	if err := json.Unmarshal([]byte(`{"OCIConfigPath":"/missing/not-read.json","HostConfig":{"Devices":[{"PathOnHost":"/dev/kfd","PathInContainer":"/ignored/kfd"},{"PathOnHost":"/dev/dri/renderD131","PathInContainer":"/ignored/renderD128"}]}}`), &container); err != nil {
		t.Fatal(err)
	}
	devices, err := exposedGPUDevices(container)
	if err != nil || !reflect.DeepEqual(devices, []string{"/dev/kfd", "/dev/dri/renderD131"}) {
		t.Fatalf("devices=%#v, error=%v", devices, err)
	}
}

func TestExposedGPUDevicesLinuxDeviceFallbackAndMountDeduplication(t *testing.T) {
	for name, raw := range map[string]string{
		"linux-only": `{"linux":{"devices":[{"path":"/dev/null","major":1},{"path":"/dev/kfd","major":235},{"path":"/dev/dri/renderD130","major":226}]}}`,
		"mixed":      `{"mounts":[{"destination":"/dev/kfd","source":"/dev/kfd","type":"bind"}],"linux":{"devices":[{"path":"/dev/kfd","major":235},{"path":"/dev/dri/renderD130","major":226}]}}`,
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

func TestExposedGPUDevicesRejectsUnavailableOrInvalidOCIConfig(t *testing.T) {
	for name, path := range map[string]string{
		"absent path":   "",
		"relative path": "config.json",
		"missing file":  filepath.Join(t.TempDir(), "missing.json"),
		"directory":     t.TempDir(),
		"invalid json":  writeDeviceConfig(t, `{"mounts":`),
		"oversized":     writeDeviceConfig(t, strings.Repeat(" ", 4*1024*1024+1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := exposedGPUDevices(inspectedContainer{OCIConfigPath: path}); err == nil {
				t.Fatal("unavailable or invalid effective OCI configuration accepted")
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
