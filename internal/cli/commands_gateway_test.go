package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"paracetamol/internal/config"
	"paracetamol/internal/gateway"
	"paracetamol/internal/process"
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

func TestGatewayHelpShowsApplicationAliasAndSafeModelDefault(t *testing.T) {
	app, stdout, _ := testApp(t, &commandRunner{})
	if err := app.runGateway([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help error=%v", err)
	}
	output := stdout.String()
	for _, expected := range []string{"-a, --application APPLICATION", "default discovers runnable applications", "--models-max COUNT", "default: 1"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("gateway help lacks %q:\n%s", expected, output)
		}
	}
}

func TestGatewayApplicationFlagSupportsShortAndLongForms(t *testing.T) {
	app, _, _ := testApp(t, &commandRunner{})
	set := app.flags("run gateway", usage("run", "gateway", "[OPTIONS]"))
	var applications stringList
	set.VarWithShort(&applications, "application", "a", "fixture")
	if err := parseFlags(set, []string{"-a", "llama-cpp", "--application", "dwarfstar"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(applications, ","); got != "llama-cpp,dwarfstar" {
		t.Fatalf("applications=%q", got)
	}
}

func TestGatewayAutomaticSelectionConsidersOnlyBuiltImages(t *testing.T) {
	llama, _ := config.ApplicationByID("llama-cpp")
	runner := &commandRunner{run: func(command process.Command) process.Result {
		if strings.Join(command.Args, " ") == "image exists "+llama.Image {
			return process.Result{}
		}
		return process.Result{Status: 1}
	}}
	app, _, _ := testApp(t, runner)
	applications, err := app.builtGatewayApplications()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(applications, ","); got != "llama-cpp" {
		t.Fatalf("built applications=%q", got)
	}
}

func TestGatewayStartupIsCompactAndActionable(t *testing.T) {
	app, stdout, _ := testApp(t, &commandRunner{})
	app.writeGatewayStartup(gatewayStartup{
		Endpoint: "http://127.0.0.1:8080/v1", Applications: []string{"llama-cpp"},
		Profile: "strix-halo", RenderNodes: []string{"/dev/dri/renderD128"},
		Backend: "rocm", ModelsMax: 1,
		Registry: gateway.Registry{
			Fingerprint: "1234567890abcdef",
			Models:      []gateway.Model{{ID: "qwen-one"}, {ID: "qwen-two"}},
		},
	})
	output := stdout.String()
	for _, expected := range []string{
		"http://127.0.0.1:8080/v1", "strix-halo", "/dev/dri/renderD128",
		"2 verified models", "1234567890ab", "up to 1 loaded model",
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
