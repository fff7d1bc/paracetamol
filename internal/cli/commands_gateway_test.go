package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paracetamol/internal/config"
	"paracetamol/internal/gateway"
	"paracetamol/internal/hostdoctor"
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
	for _, expected := range []string{"-a, --application APPLICATION", "default discovers runnable applications", "--models-max COUNT", "default: 1", "loopback remains available", "Using --port alone selects loopback", "default: 7455", "run gateway --port 18080"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("gateway help lacks %q:\n%s", expected, output)
		}
	}
}

func TestGatewayListenBindingsAlwaysPreserveIPv4Loopback(t *testing.T) {
	for _, test := range []struct {
		name, requested, expected string
	}{
		{"default loopback", "127.0.0.1", "tcp4/127.0.0.1"},
		{"exact LAN", "192.168.249.225", "tcp4/127.0.0.1,tcp4/192.168.249.225"},
		{"IPv4 wildcard", "0.0.0.0", "tcp4/0.0.0.0"},
		{"IPv6 loopback", "::1", "tcp4/127.0.0.1,tcp6/::1"},
		{"IPv6 wildcard", "::", "tcp4/127.0.0.1,tcp6/::"},
	} {
		t.Run(test.name, func(t *testing.T) {
			bindings := gatewayListenBindings(test.requested)
			values := make([]string, 0, len(bindings))
			for _, binding := range bindings {
				values = append(values, binding.network+"/"+binding.host)
			}
			if got := strings.Join(values, ","); got != test.expected {
				t.Fatalf("bindings=%q", got)
			}
		})
	}
}

func TestOpenGatewayListenersClosesEarlierBindingsOnFailure(t *testing.T) {
	first := &trackedGatewayListener{}
	calls := []string{}
	listeners, err := openGatewayListeners("192.168.249.225", 8080, func(network, address string) (net.Listener, error) {
		calls = append(calls, network+"/"+address)
		if len(calls) == 1 {
			return first, nil
		}
		return nil, errors.New("fixture bind failure")
	})
	if err == nil || !strings.Contains(err.Error(), "192.168.249.225:8080") || listeners != nil {
		t.Fatalf("listeners=%v err=%v", listeners, err)
	}
	if !first.closed || strings.Join(calls, ",") != "tcp4/127.0.0.1:8080,tcp4/192.168.249.225:8080" {
		t.Fatalf("closed=%t calls=%v", first.closed, calls)
	}
}

func TestGatewayAdditionalEndpointPresentation(t *testing.T) {
	for _, test := range []struct {
		requested string
		label     string
		value     string
	}{
		{"127.0.0.1", "", ""},
		{"127.0.0.2", "Additional endpoint", "http://127.0.0.2:8080/v1"},
		{"192.168.249.225", "Published endpoint", "http://192.168.249.225:8080/v1"},
		{"0.0.0.0", "Published on", "0.0.0.0:8080 · all IPv4 interfaces"},
		{"::", "Published on", "[::]:8080 · all IPv6 interfaces"},
	} {
		label, value := gatewayAdditionalEndpoint(test.requested, 8080)
		if label != test.label || value != test.value {
			t.Fatalf("requested=%s label=%q value=%q", test.requested, label, value)
		}
	}
}

func TestCollectGatewayServeErrorsIgnoresNormalShutdown(t *testing.T) {
	results := make(chan error, 2)
	results <- http.ErrServerClosed
	results <- errors.New("fixture serve failure")
	err := collectGatewayServeErrors(results, 2, nil)
	if err == nil || !strings.Contains(err.Error(), "fixture serve failure") || strings.Contains(err.Error(), http.ErrServerClosed.Error()) {
		t.Fatalf("serve error=%v", err)
	}
}

type trackedGatewayListener struct{ closed bool }

func (*trackedGatewayListener) Accept() (net.Conn, error) { return nil, errors.New("unused") }
func (listener *trackedGatewayListener) Close() error     { listener.closed = true; return nil }
func (*trackedGatewayListener) Addr() net.Addr            { return &net.TCPAddr{} }

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
	if got := gatewayIntSetting("", map[string]string{}, "GATEWAY_PORT", &configuredPort, config.DefaultGatewayPort); got != "18080" {
		t.Fatalf("configured port=%q", got)
	}
}

func TestGatewayExplicitPortWithoutListenForcesLoopback(t *testing.T) {
	configuredListen := "192.168.1.50"
	environment := map[string]string{
		"PARACETAMOL_GATEWAY_LISTEN": "192.168.1.60",
		"PARACETAMOL_GATEWAY_PORT":   "17070",
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "resolved settings remain published", want: "192.168.1.60"},
		{name: "port only", args: []string{"--port", "18080"}, want: config.DefaultListen},
		{name: "listen only", args: []string{"--listen", "192.168.1.70"}, want: "192.168.1.70"},
		{name: "listen and port", args: []string{"--port", "18080", "--listen", "192.168.1.70"}, want: "192.168.1.70"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, _, _ := testApp(t, &commandRunner{})
			set := app.flags("run gateway", usage("run", "gateway", "[OPTIONS]"))
			listen := set.String("listen", "", "fixture")
			_ = set.String("port", "", "fixture")
			if err := parseFlags(set, test.args); err != nil {
				t.Fatal(err)
			}
			if got := gatewayListenSetting(set, *listen, environment, &configuredListen); got != test.want {
				t.Fatalf("listen=%q, want %q", got, test.want)
			}
		})
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
		LocalEndpoint: config.DefaultGatewayURL, Applications: []string{"llama-cpp"},
		Profile: "strix-halo", RenderNodes: []string{"/dev/dri/renderD128"},
		Backend: "rocm", ModelsMax: 1, Configuration: "/home/test/.config/paracetamol/config.toml",
		Registry: gateway.Registry{
			Fingerprint: "1234567890abcdef",
			Models:      []gateway.Model{{ID: "qwen-one"}, {ID: "qwen-two"}},
		},
	})
	output := stdout.String()
	for _, expected := range []string{
		config.DefaultGatewayURL, "strix-halo", "/dev/dri/renderD128",
		"2 verified models", "1234567890ab", "up to 1 loaded model",
		"unloaded", "status gateway --gateway-url " + config.DefaultGatewayURL,
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

func TestGatewayStartupDistinguishesLocalAndPublishedEndpoints(t *testing.T) {
	app, stdout, _ := testApp(t, &commandRunner{})
	label, published := gatewayAdditionalEndpoint("192.168.249.225", config.DefaultGatewayPort)
	app.writeGatewayStartup(gatewayStartup{
		LocalEndpoint: gatewayEndpoint(gatewayLoopbackAddress, config.DefaultGatewayPort), AdditionalLabel: label, AdditionalEndpoint: published,
		Applications: []string{"llama-cpp"}, Profile: "strix-halo", Backend: "rocm", ModelsMax: 1,
		Registry: gateway.Registry{Fingerprint: "1234567890abcdef", Models: []gateway.Model{{ID: "qwen"}}},
	})
	output := stdout.String()
	for _, expected := range []string{
		"Local endpoint", config.DefaultGatewayURL, "Published endpoint", "http://192.168.249.225:7455/v1",
		"status gateway --gateway-url " + config.DefaultGatewayURL,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("startup output lacks %q:\n%s", expected, output)
		}
	}
}

func TestStatusGatewayUsesVersionedEndpoint(t *testing.T) {
	requested := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
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

func TestStatusGatewayUsesConfiguredClientURL(t *testing.T) {
	requested := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
		requested = request.URL.Path
		_ = json.NewEncoder(writer).Encode(gateway.Status{
			Schema: gateway.StatusSchema, Gateway: "ready", StartedAt: "2026-01-01T00:00:00Z",
			Scheduler: gateway.SchedulerStatus{State: gateway.StateUnloaded},
		})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[gateway.client]\nurl = \""+server.URL+"/v1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, _ := testApp(t, &commandRunner{})
	app.ConfigSelection = config.Selection{Path: path}
	if err := app.commandStatus([]string{"gateway"}); err != nil {
		t.Fatal(err)
	}
	if requested != "/paracetamol/v1/status" {
		t.Fatalf("path=%q", requested)
	}
}

func TestGatewayURLRejectsCredentialsAndArbitraryPaths(t *testing.T) {
	for _, value := range []string{"ftp://example.test/v1", "http://user@example.test/v1", "http://example.test/other"} {
		if _, err := config.NormalizeGatewayURL(value); err == nil {
			t.Fatalf("URL %q was accepted", value)
		}
	}
}

func TestStatusGatewayAuthenticationAndResourcePresentation(t *testing.T) {
	const key = "client-key-0123456789abcdef0123456789abcdef"
	keyFile := filepath.Join(t.TempDir(), "client.key")
	if err := os.WriteFile(keyFile, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	total, available := uint64(128<<30), uint64(32<<30)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(gateway.Status{Schema: gateway.StatusSchema, Gateway: "ready", AuthenticationEnabled: true,
			Resources: &hostdoctor.ResourceSnapshot{CollectedAt: "sample", MemoryTotalBytes: &total, MemoryAvailableBytes: &available, GPUs: []hostdoctor.GPUResources{{Device: "renderD128", BusyPercent: floatPointer(90), TemperatureCelsius: floatPointer(65), SoCPowerWatts: floatPointer(87), PowerSample: "average"}}}})
	}))
	defer server.Close()
	app, stdout, _ := testApp(t, &commandRunner{})
	if err := app.commandStatus([]string{"gateway", "--gateway-url", server.URL + "/v1"}); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("401=%v", err)
	}
	if err := app.commandStatus([]string{"gateway", "--gateway-url", server.URL + "/v1", "--gateway-api-key-file", keyFile}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bearer key required", "Host resources", "32.0 GiB available / 128.0 GiB total", "90% busy", "65.0 °C", "87.0 W SoC (average)", "not model memory"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("output lacks %q", want)
		}
	}
	if strings.Contains(stdout.String(), key) || strings.Contains(stdout.String(), keyFile) {
		t.Fatal("status leaked credentials")
	}
}

func TestGatewayCredentialFlagsRejectEmptyOrWrongScope(t *testing.T) {
	app, _, _ := testApp(t, &commandRunner{})
	for _, args := range [][]string{{"gateway", "--gateway-api-key-file="}, {"llama-cpp", "--gateway-api-key-file=/unused/key"}} {
		if err := app.commandStatus(args); err == nil || !strings.Contains(err.Error(), "--gateway-api-key-file") {
			t.Fatalf("status %v: %v", args, err)
		}
	}
	if err := app.runGateway([]string{"--api-key-file="}); err == nil || !strings.Contains(err.Error(), "--api-key-file") {
		t.Fatalf("run gateway: %v", err)
	}
}

func TestRemoteGatewayWarningSeparatesAuthenticationFromEncryption(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		for _, scheme := range []string{"http", "https"} {
			app, _, stderr := testApp(t, &commandRunner{})
			app.writeAgentRemoteGateway("Pi", scheme+"://gateway.example.test/v1", authenticated)
			want := "no key supplied"
			if authenticated {
				want = "Bearer key supplied"
			}
			if !strings.Contains(stderr.String(), want) || strings.Contains(stderr.String(), "HTTP does not encrypt") != (scheme == "http") {
				t.Fatalf("warning=%s", stderr)
			}
		}
	}
}

func TestStatusGatewayRequestsRecentObservability(t *testing.T) {
	requested := ""
	input, output, cached, reasoning := int64(20), int64(5), int64(12), int64(3)
	promptMS, generatedMS := 100.0, 250.0
	parameters, modelBytes := uint64(27_400_000_000), uint64(29_192_355_840)
	context, trainingContext := int64(262144), int64(32768)
	sessionID := "019fe5cc-5cad-7a92-aead-f0838931fb95"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
		requested = request.URL.RequestURI()
		_ = json.NewEncoder(writer).Encode(gateway.Status{
			Schema: gateway.StatusSchema, Gateway: "ready", StartedAt: "2026-01-01T00:00:00Z",
			Applications: []string{"llama-cpp"}, InventoryFingerprint: "fixture",
			Scheduler: gateway.SchedulerStatus{State: gateway.StateReady, Allocation: gateway.AllocationLlamaCPP},
			Models: []gateway.StatusModel{{
				ID: "qwen", Application: "llama-cpp", ConfiguredContext: 262144, State: gateway.ModelUnknown,
				Runtime:    &gateway.ModelRuntime{Context: &context, TrainingContext: &trainingContext, Parameters: &parameters, ModelBytes: &modelBytes, Quantization: "Q8_0"},
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
					BackendTimings: gateway.AggregateBackendTimings{
						PromptProcessing: gateway.AggregateThroughput{TimedTokens: 20, Milliseconds: 100, Observations: 1, TokensPerSecond: floatPointer(200)},
						TokenGeneration:  gateway.AggregateThroughput{TimedTokens: 4, Milliseconds: 250, Observations: 1, TokensPerSecond: floatPointer(16)},
						SpeculativeDraft: gateway.AggregateDraft{DraftTokens: 8, AcceptedTokens: 5, Observations: 1, AcceptanceRatio: floatPointer(0.625)},
					},
				},
				Models:   []gateway.ModelAggregate{{Model: "qwen", Application: "llama-cpp", Usage: gateway.RequestAggregate{Requests: 1, Tokens: gateway.AggregateTokens{Output: gateway.AggregateMetric{Total: 5, Observations: 1}}}}},
				Sessions: []gateway.SessionAggregate{{ID: sessionID, Usage: gateway.RequestAggregate{Requests: 1, Tokens: gateway.AggregateTokens{Output: gateway.AggregateMetric{Total: 5, Observations: 1}}}}},
			},
			RecentRequests: []gateway.RequestRecord{{
				ID: "r00000001", SessionID: sessionID, Model: "qwen", Application: "llama-cpp", Peer: "192.0.2.10", UserAgent: "pi/fixture",
				Stream: true, Outcome: gateway.OutcomeSucceeded, HTTPStatus: http.StatusOK,
				Timing:         gateway.RequestTiming{GatewayWaitMilliseconds: 3, UpstreamMilliseconds: 40, TotalMilliseconds: 44},
				Tokens:         gateway.TokenUsage{Input: &input, Output: &output, Cached: &cached, Reasoning: &reasoning},
				BackendTimings: gateway.BackendTimings{PromptTokens: &input, PromptMilliseconds: &promptMS, GeneratedTokens: &output, GeneratedMilliseconds: &generatedMS},
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
		"256K configured context", "32K trained", "27.4B params", "27.2 GiB", "Q8_0", "PP 200.00 tok/s", "TG 16.00 tok/s", "draft 5/8 accepted (62.5%)",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("status output lacks %q:\n%s", expected, stdout)
		}
	}
}

func floatPointer(value float64) *float64 { return &value }

func TestGatewayTimingAndCachePresentation(t *testing.T) {
	first, input, cached := int64(2300), int64(100), int64(75)
	metadata := gatewayRequestMetadata(gateway.RequestRecord{Stream: true, Timing: gateway.RequestTiming{FirstOutputMilliseconds: &first}, Tokens: gateway.TokenUsage{Input: &input, Cached: &cached}})
	for _, want := range []string{"2.3s first output", "75.0% cache reuse"} {
		if !strings.Contains(metadata, want) {
			t.Fatalf("metadata lacks %q: %s", want, metadata)
		}
	}
	if got := gatewayFirstOutputMean(gateway.AggregateMetric{Total: 4600, Observations: 2}, 4); got != "2.3s mean (2/4 requests observed)" {
		t.Fatal(got)
	}
	if got := gatewayCacheReuse(gateway.AggregateCacheReuse{InputTokens: 100, CachedTokens: 75, Observations: 1, Ratio: floatPointer(.75)}); got != "75.0% (75/100 input tokens, 1 paired request)" {
		t.Fatal(got)
	}
	if got := gatewayCacheReuse(gateway.AggregateCacheReuse{}); got != "unavailable" {
		t.Fatal(got)
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
