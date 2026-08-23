package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestGatewayConfigurationProvidesOptionalDefaultsAndFlagsWin(t *testing.T) {
	app, _, _ := testApp(t, &commandRunner{})
	set := app.flags("run gateway", usage("run", "gateway", "[OPTIONS]"))
	var applications, nodes stringList
	set.VarWithShort(&applications, "application", "a", "fixture")
	set.Var(&nodes, "render-node", "fixture")
	backend := set.String("backend", "rocm", "fixture")
	modelsMax := set.Int("models-max", 1, "fixture")
	startupTimeout := set.Duration("startup-timeout", gateway.DefaultStartupTimeout, "fixture")
	if err := parseFlags(set, []string{"-a", "llama-cpp", "--models-max", "3"}); err != nil {
		t.Fatal(err)
	}
	configuredBackend := "vulkan"
	configuredModels := 2
	configuredTimeout := "45m"
	configuration := config.Configuration{Path: "/profiles/aion.toml", Gateway: config.GatewayConfiguration{
		Applications: []string{"dwarfstar"}, RenderNodes: []string{"/dev/dri/renderD128"}, StartupTimeout: &configuredTimeout,
		LlamaCPP: config.GatewayLlamaConfiguration{Backend: &configuredBackend, ModelsMax: &configuredModels},
	}}
	if err := applyGatewayConfiguration(set, map[string]string{}, configuration, &applications, &nodes, backend, modelsMax, startupTimeout); err != nil {
		t.Fatal(err)
	}
	if strings.Join(applications, ",") != "llama-cpp" || strings.Join(nodes, ",") != "/dev/dri/renderD128" || *backend != "vulkan" || *modelsMax != 3 || *startupTimeout != 45*time.Minute {
		t.Fatalf("applications=%v nodes=%v backend=%s models=%d timeout=%s", applications, nodes, *backend, *modelsMax, *startupTimeout)
	}
}

func TestGatewayScalarPrecedenceIsFlagEnvironmentConfigurationDefault(t *testing.T) {
	configuredListen := "192.168.1.50"
	environment := map[string]string{"PARACETAMOL_GATEWAY_LISTEN": "192.168.1.60"}
	if got := gatewayStringSetting("192.168.1.70", environment, "GATEWAY_LISTEN", &configuredListen, "127.0.0.1"); got != "192.168.1.70" {
		t.Fatalf("flag precedence=%q", got)
	}
	if got := gatewayStringSetting("", environment, "GATEWAY_LISTEN", &configuredListen, "127.0.0.1"); got != "192.168.1.60" {
		t.Fatalf("environment precedence=%q", got)
	}
	if got := gatewayStringSetting("", map[string]string{}, "GATEWAY_LISTEN", &configuredListen, "127.0.0.1"); got != "192.168.1.50" {
		t.Fatalf("configuration precedence=%q", got)
	}
	configuredPort := 18080
	if got := gatewayIntSetting("", map[string]string{}, "GATEWAY_PORT", &configuredPort, 8080); got != "18080" {
		t.Fatalf("configured port=%q", got)
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
		Backend: "rocm", ModelsMax: 1, Configuration: "/home/test/.config/paracetamol/config.toml",
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
		"/home/test/.config/paracetamol/config.toml",
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
			Models:    []gateway.StatusModel{{ID: "fixture", Application: "llama-cpp", State: gateway.ModelUnloaded}},
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

func TestStatusGatewayRequestsRecentObservability(t *testing.T) {
	requested := ""
	input, output, cached, reasoning := int64(20), int64(5), int64(12), int64(3)
	sessionID := "019fe5cc-5cad-7a92-aead-f0838931fb95"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requested = request.URL.RequestURI()
		_ = json.NewEncoder(writer).Encode(gateway.Status{
			Schema: gateway.StatusSchema, Gateway: "ready", StartedAt: "2026-01-01T00:00:00Z",
			Applications: []string{"llama-cpp"}, InventoryFingerprint: "fixture",
			Scheduler: gateway.SchedulerStatus{State: gateway.StateReady, Allocation: gateway.AllocationLlamaCPP},
			Models: []gateway.StatusModel{{
				ID: "qwen", Application: "llama-cpp", State: gateway.ModelUnknown,
				Diagnostic: "llama.cpp router model status is unavailable",
			}},
			Usage: gateway.UsageSummary{
				Overall: gateway.RequestAggregate{
					Requests: 1, Outcomes: gateway.AggregateOutcomes{Succeeded: 1},
					Tokens: gateway.AggregateTokens{
						Input: gateway.AggregateMetric{Total: 20, Observations: 1}, Output: gateway.AggregateMetric{Total: 5, Observations: 1},
						Cached: gateway.AggregateMetric{Total: 12, Observations: 1}, Reasoning: gateway.AggregateMetric{Total: 3, Observations: 1},
					},
					Timing: gateway.AggregateTiming{
						GatewayWait: gateway.AggregateMetric{Total: 3, Observations: 1}, Upstream: gateway.AggregateMetric{Total: 40, Observations: 1}, Total: gateway.AggregateMetric{Total: 44, Observations: 1},
					},
				},
				Models:   []gateway.ModelAggregate{{Model: "qwen", Application: "llama-cpp", Usage: gateway.RequestAggregate{Requests: 1, Tokens: gateway.AggregateTokens{Output: gateway.AggregateMetric{Total: 5, Observations: 1}}}}},
				Sessions: []gateway.SessionAggregate{{ID: sessionID, Usage: gateway.RequestAggregate{Requests: 1, Tokens: gateway.AggregateTokens{Output: gateway.AggregateMetric{Total: 5, Observations: 1}}}}},
			},
			RecentRequests: []gateway.RequestRecord{{
				ID: "r00000001", SessionID: sessionID, Model: "qwen", Application: "llama-cpp", Peer: "192.0.2.10", UserAgent: "pi/fixture",
				Stream: true, Outcome: gateway.OutcomeSucceeded, HTTPStatus: http.StatusOK,
				Timing: gateway.RequestTiming{GatewayWaitMilliseconds: 3, UpstreamMilliseconds: 40, TotalMilliseconds: 44},
				Tokens: gateway.TokenUsage{Input: &input, Output: &output, Cached: &cached, Reasoning: &reasoning},
				Controls: gateway.RequestControls{
					Reasoning: []gateway.ObservedControl{{Name: "reasoning_effort", Source: gateway.ControlClient, Provided: true, Value: "medium"}},
					Sampling:  []gateway.ObservedControl{{Name: "temperature", Source: gateway.ControlDefault}},
				},
			}},
		})
	}))
	defer server.Close()
	app, stdout, _ := testApp(t, &commandRunner{})
	if err := app.commandStatus([]string{"gateway", "--gateway-url", server.URL + "/v1", "--requests", "1"}); err != nil {
		t.Fatal(err)
	}
	if requested != "/paracetamol/v1/status?requests=1" {
		t.Fatalf("request=%q", requested)
	}
	for _, expected := range []string{
		"r00000001", "qwen", "succeeded", "44ms total", "20 input", "5 output", "12 cached",
		"3 reasoning", sessionID, "Usage since start", "Recent sessions", "reasoning_effort=medium", "sampler defaults", "pi/fixture", "router model status is unavailable",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("status output lacks %q:\n%s", expected, stdout)
		}
	}
}

func TestStatusGatewayValidatesRecentRequestCountAndScope(t *testing.T) {
	for _, arguments := range [][]string{
		{"gateway", "--requests", "0"},
		{"gateway", "--requests", "65"},
		{"llama-cpp", "--requests", "1"},
	} {
		app, _, _ := testApp(t, &commandRunner{})
		if err := app.commandStatus(arguments); err == nil || !strings.Contains(err.Error(), "--requests") {
			t.Fatalf("arguments=%v err=%v", arguments, err)
		}
	}
}
