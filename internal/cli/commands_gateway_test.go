package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"paracetamol/internal/gateway"
)

func TestRunHelpIncludesGateway(t *testing.T) {
	app, stdout, _ := testApp(t, &commandRunner{})
	if err := app.commandRun([]string{"--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "gateway") || !strings.Contains(stdout.String(), "one-port") {
		t.Fatalf("gateway missing from run help:\n%s", stdout)
	}
}

func TestGatewayApplicationSelectionDefaultsToLlamaCPP(t *testing.T) {
	if got := strings.Join(selectedGatewayApplications(nil), ","); got != "llama-cpp" {
		t.Fatalf("default applications=%q", got)
	}
	explicit := []string{"dwarfstar"}
	selected := selectedGatewayApplications(explicit)
	if got := strings.Join(selected, ","); got != "dwarfstar" {
		t.Fatalf("explicit applications=%q", got)
	}
	selected[0] = "llama-cpp"
	if explicit[0] != "dwarfstar" {
		t.Fatal("application selection aliases caller storage")
	}
}

func TestGatewayStartupIsCompactAndActionable(t *testing.T) {
	app, stdout, _ := testApp(t, &commandRunner{})
	app.writeGatewayStartup(gatewayStartup{
		Endpoint: "http://127.0.0.1:8080/v1", Applications: []string{"llama-cpp"},
		Profile: "strix-halo", RenderNodes: []string{"/dev/dri/renderD128"},
		Backend: "rocm", ModelsMax: 2,
		Registry: gateway.Registry{
			Fingerprint: "1234567890abcdef",
			Models:      []gateway.Model{{ID: "qwen-one"}, {ID: "qwen-two"}},
		},
	})
	output := stdout.String()
	for _, expected := range []string{
		"http://127.0.0.1:8080/v1", "strix-halo", "/dev/dri/renderD128",
		"2 verified models", "1234567890ab", "up to 2 loaded models",
		"unloaded", "status gateway --gateway-url http://127.0.0.1:8080/v1",
		"Waiting for requests",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("startup output lacks %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "qwen-one") || strings.Contains(output, "qwen-two") {
		t.Fatalf("startup output dumps the model inventory:\n%s", output)
	}
}

func TestStatusGatewayUsesVersionedEndpoint(t *testing.T) {
	requested := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requested = request.URL.Path
		_ = json.NewEncoder(writer).Encode(gateway.Status{
			Schema: gateway.StatusSchema, Gateway: "ready", StartedAt: "2026-01-01T00:00:00Z",
			Applications: []string{"llama-cpp"}, InventoryFingerprint: "fixture",
			Scheduler: gateway.SchedulerStatus{State: gateway.StateUnloaded},
			Models:    []gateway.StatusModel{{ID: "fixture", Application: "llama-cpp", State: gateway.StateUnloaded}},
		})
	}))
	defer server.Close()
	app, stdout, _ := testApp(t, &commandRunner{})
	if err := app.commandStatus([]string{"gateway", "--gateway-url", server.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if requested != "/paracetamol/v1/status" || !strings.Contains(stdout.String(), "fixture") {
		t.Fatalf("path=%q output=%s", requested, stdout)
	}
}

func TestGatewayURLRejectsCredentialsAndArbitraryPaths(t *testing.T) {
	for _, value := range []string{"ftp://example.test/v1", "http://user@example.test/v1", "http://example.test/other"} {
		if _, err := parseGatewayURL(value); err == nil {
			t.Fatalf("URL %q was accepted", value)
		}
	}
}
