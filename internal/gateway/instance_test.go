package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"paracetamol/internal/podman"
)

func TestGatewayInstanceIdentity(t *testing.T) {
	first, err := NewInstanceID()
	if err != nil || !ValidInstanceID(first) {
		t.Fatalf("new identity=%q err=%v", first, err)
	}
	second, err := NewInstanceID()
	if err != nil || !ValidInstanceID(second) || first == second {
		t.Fatalf("second identity=%q err=%v", second, err)
	}
	for _, id := range []string{"", strings.Repeat("0", 32), strings.Repeat("A", 32), strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("g", 32)} {
		if ValidInstanceID(id) {
			t.Fatalf("accepted noncanonical identity %q", id)
		}
	}
}

func TestServerUsesExplicitLifecycleIdentity(t *testing.T) {
	upstream := httptest.NewServer(nil)
	defer upstream.Close()
	_, scheduler, original := testGatewayServer(t, upstream)
	id := strings.Repeat("b", 32)
	server, err := NewServer(original.registry, scheduler, io.Discard, ServerOptions{InstanceID: id})
	if err != nil {
		t.Fatal(err)
	}
	defer server.BeginShutdown()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest("GET", "/paracetamol/v1/status", nil))
	var status Status
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if response.Header().Get(InstanceHeader) != id || status.InstanceID != id {
		t.Fatalf("lifecycle identity differs from HTTP header/status: %s %s", response.Header().Get(InstanceHeader), status.InstanceID)
	}
	if _, err := NewServer(original.registry, scheduler, io.Discard, ServerOptions{InstanceID: "not-canonical"}); err == nil {
		t.Fatal("server accepted invalid lifecycle identity")
	}
}

func instanceLabels(id string) string {
	labels, _ := json.Marshal(map[string]string{
		podman.ManagedContainerLabel: "true", podman.ManagedApplicationLabel: "llama-cpp",
		podman.ManagedRoleLabel: GatewayBackendRole, GatewayInstanceLabel: id,
	})
	return string(labels)
}

func TestLifecycleBindsContainerAndProtectsAnotherInstance(t *testing.T) {
	runner := &lifecycleRunner{running: true, labels: instanceLabels(strings.Repeat("b", 32))}
	lifecycle := llamaLifecycleFixture(t, runner)
	command, _, _, err := lifecycle.command(AllocationLlamaCPP)
	if err != nil {
		t.Fatal(err)
	}
	if command[0] != "podman" || command[1] != "run" || command[2] != "--label" || command[3] != GatewayInstanceLabel+"="+lifecycle.options.InstanceID {
		t.Fatalf("gateway instance label is not a Podman argument: %v", command)
	}
	if err := lifecycle.Stop(context.Background(), AllocationLlamaCPP); err != nil || !runner.running {
		t.Fatalf("stop must preserve a different gateway instance: running=%v err=%v", runner.running, err)
	}
	// A well-formed foreign nonce does not prove its gateway has stopped.
	runner.conflict = gatewayContainerNames[AllocationLlamaCPP] + "\n"
	if err := lifecycle.Preflight(context.Background(), []Allocation{AllocationLlamaCPP}); err == nil || !runner.running {
		t.Fatalf("preflight must preserve another gateway instance: running=%v err=%v", runner.running, err)
	}
	if _, err := lifecycle.Start(context.Background(), AllocationLlamaCPP); err == nil || !runner.running {
		t.Fatalf("start must preserve another gateway instance: running=%v err=%v", runner.running, err)
	}
	for _, command := range runner.commands {
		if len(command.Args) > 0 && (command.Args[0] == "rm" || command.Args[0] == "run") {
			t.Fatalf("foreign instance was mutated: %v", command.Args)
		}
	}
}

func TestLifecycleRejectsMissingOrInvalidInstanceLabels(t *testing.T) {
	for _, id := range []string{"", "old-name", strings.Repeat("0", 32)} {
		runner := &lifecycleRunner{running: true, conflict: gatewayContainerNames[AllocationLlamaCPP] + "\n", labels: instanceLabels(id)}
		lifecycle := llamaLifecycleFixture(t, runner)
		if err := lifecycle.Preflight(context.Background(), []Allocation{AllocationLlamaCPP}); err == nil || !runner.running {
			t.Fatalf("invalid owner was reclaimed: identity=%q running=%v err=%v", id, runner.running, err)
		}
	}
}
