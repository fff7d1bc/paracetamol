package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/gateway"
	"paracetamol/internal/identity"
	"paracetamol/internal/platform"
	"paracetamol/internal/runtime"
	"paracetamol/internal/storage"
	"paracetamol/internal/textmodel"
	"paracetamol/internal/ui"
)

func (app *App) runGateway(args []string) error {
	set := app.flags("run gateway", usage("run", "gateway", "[OPTIONS]"))
	var applications stringList
	set.VarWithShort(&applications, "application", "a", "restrict to llama-cpp or dwarfstar; repeatable; default discovers runnable applications")
	profileFlag := set.String("profile", "", "auto, rdna4, strix-halo, or strix-point (default: auto)")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact GPU render node; repeatable")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	listenFlag := set.String("listen", "", "host IP on which to publish the gateway (default: 127.0.0.1)")
	portFlag := set.String("port", "", "gateway host port (default: 8080)")
	backend := set.String("backend", "rocm", "llama.cpp backend: rocm or vulkan")
	modelsMax := set.Int("models-max", 1, "llama.cpp router simultaneous models")
	startupTimeout := set.Duration("startup-timeout", gateway.DefaultStartupTimeout, "backend readiness timeout")
	unconfined := set.Bool("unconfined", false, "disable backend seccomp")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("run gateway does not accept positional arguments")
	}
	configuration, err := app.hostConfiguration()
	if err != nil {
		return err
	}
	gatewayConfiguration := configuration.Gateway
	if err := applyGatewayConfiguration(set, app.Environment, configuration, &applications, &nodes, backend, modelsMax, startupTimeout); err != nil {
		return err
	}
	for _, application := range applications {
		if err := requireChoice(application, "gateway application", "llama-cpp", "dwarfstar"); err != nil {
			return err
		}
	}
	if err := requireChoice(*backend, "llama.cpp backend", "rocm", "vulkan"); err != nil {
		return err
	}
	if *modelsMax < 1 {
		return controlerr.Usage("--models-max must be at least 1")
	}
	if *startupTimeout <= 0 {
		return controlerr.Usage("--startup-timeout must be positive")
	}
	listen := gatewayStringSetting(*listenFlag, app.Environment, "GATEWAY_LISTEN", gatewayConfiguration.Listen, config.DefaultListen)
	if err := config.ValidateListenAddress(listen); err != nil {
		return err
	}
	port, err := config.ValidatePort(gatewayIntSetting(*portFlag, app.Environment, "GATEWAY_PORT", gatewayConfiguration.Port, 8080))
	if err != nil {
		return err
	}
	profile := gatewayStringSetting(*profileFlag, app.Environment, "PROFILE", gatewayConfiguration.Profile, "auto")
	if err := platform.ValidateProfile(profile); err != nil {
		return controlerr.Usage("%v", err)
	}
	selectedNodes, err := app.resolveDevices(profile, nodes, nodes != nil)
	if err != nil {
		return err
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	var registry gateway.Registry
	var diagnostics []gateway.Diagnostic
	if len(applications) == 0 {
		registry, applications, diagnostics, err = app.discoverGatewayRegistry(managed, dataRoot, profile, selectedNodes)
	} else {
		registry, diagnostics, err = gateway.BuildRegistry(managed, dataRoot, applications, profile, selectedNodes)
	}
	errorTerminal := app.terminal(app.Stderr)
	if err != nil {
		for _, diagnostic := range diagnostics {
			fmt.Fprintf(app.Stderr, "%s %s/%s: %s\n", errorTerminal.Warning("Unavailable:"), diagnostic.Application, diagnostic.Model, diagnostic.Reason)
		}
		return err
	}
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(app.Stderr, "%s %s/%s: %s\n", errorTerminal.Warning("Unavailable:"), diagnostic.Application, diagnostic.Model, diagnostic.Reason)
	}
	allocations := make([]gateway.Allocation, 0, len(applications))
	seen := map[string]bool{}
	for _, application := range applications {
		if !seen[application] {
			allocations = append(allocations, gateway.Allocation(application))
			seen[application] = true
		}
	}
	routerPath := ""
	routerContents := ""
	if seen[string(textmodel.BackendLlamaCPP)] {
		routerContents, err = runtime.RenderRouterModels(managed, *backend, registry.IDs(string(textmodel.BackendLlamaCPP)))
		if err != nil {
			return err
		}
		routerPath = filepath.Join((storage.Layout{Root: dataRoot}).Application("llama-cpp"), "models.ini")
	}
	gatewayLog := gateway.NewAtomicLogWriter(app.Stderr)
	lifecycle, err := gateway.NewContainerLifecycle(gateway.LifecycleOptions{
		Catalog: managed, Registry: registry, DataRoot: dataRoot, Profile: profile,
		RenderNodes: selectedNodes, LlamaBackend: *backend, LlamaModelsMax: *modelsMax,
		RouterPreset: routerPath, SourceRevision: app.projectRevision(),
		VolumeSuffix: app.podman().SELinuxVolumeSuffix(app.Context), Unconfined: *unconfined,
		StartupTimeout: *startupTimeout, Runner: app.Runner, Log: gatewayLog,
	})
	if err != nil {
		return err
	}
	if err := lifecycle.Preflight(app.Context, allocations); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(listen, fmt.Sprint(port)))
	if err != nil {
		return fmt.Errorf("listen for gateway on %s:%d: %w", listen, port, err)
	}
	defer listener.Close()
	for _, application := range applications {
		if err := (storage.Layout{Root: dataRoot}).PrepareRuntime(application); err != nil {
			return err
		}
	}
	if routerContents != "" {
		written, writeErr := runtime.WriteRouter(dataRoot, routerContents)
		if writeErr != nil {
			return writeErr
		}
		if written != routerPath {
			return fmt.Errorf("llama.cpp router path changed during gateway preparation")
		}
	}
	scheduler, err := gateway.NewScheduler(lifecycle, gateway.QueueLimit, map[gateway.Allocation]int{gateway.AllocationDwarfStar: 1})
	if err != nil {
		return err
	}
	handler, err := gateway.NewServer(registry, scheduler, gatewayLog)
	if err != nil {
		return errors.Join(err, scheduler.Shutdown(contextWithoutCancel()))
	}
	server := &http.Server{
		Handler: handler, ErrorLog: log.New(gatewayLog, "gateway | http: ", 0),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
	}
	if !isLoopback(listen) {
		fmt.Fprintf(app.Stderr, "%s gateway is published on %s:%d without authentication.\n", errorTerminal.Warning("WARNING:"), listen, port)
	}
	app.writeGatewayStartup(gatewayStartup{
		Endpoint:     "http://" + net.JoinHostPort(listen, fmt.Sprint(port)) + "/v1",
		Applications: applications, Profile: profile, RenderNodes: selectedNodes,
		Backend: *backend, ModelsMax: *modelsMax, Registry: registry, Configuration: configuration.Path,
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	select {
	case serveErr := <-serveResult:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			handler.BeginShutdown()
			return errors.Join(serveErr, scheduler.Shutdown(contextWithoutCancel()))
		}
		return scheduler.Shutdown(contextWithoutCancel())
	case <-app.Context.Done():
		handler.BeginShutdown()
		shutdownErr := server.Shutdown(contextWithoutCancel())
		serveErr := <-serveResult
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		backendErr := scheduler.Shutdown(contextWithoutCancel())
		if err := errors.Join(shutdownErr, serveErr, backendErr); err != nil {
			return err
		}
		fmt.Fprintln(app.Stdout, app.terminal(app.Stdout).Success("Gateway stopped."))
		return nil
	}
}

type gatewayStartup struct {
	Endpoint      string
	Applications  []string
	Profile       string
	RenderNodes   []string
	Backend       string
	ModelsMax     int
	Registry      gateway.Registry
	Configuration string
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(*value)
}

func gatewayStringSetting(flagValue string, environment map[string]string, environmentName string, configured *string, fallback string) string {
	return firstNonEmpty(flagValue, config.EnvironmentValue(environment, environmentName, ""), stringValue(configured), fallback)
}

func gatewayIntSetting(flagValue string, environment map[string]string, environmentName string, configured *int, fallback int) string {
	return firstNonEmpty(flagValue, config.EnvironmentValue(environment, environmentName, ""), intValue(configured), fmt.Sprint(fallback))
}

func renderNodeEnvironmentConfigured(environment map[string]string) bool {
	prefix := identity.EnvironmentPrefix()
	_, plural := environment[prefix+"_RENDER_NODES"]
	return plural || environment[prefix+"_RENDER_NODE"] != ""
}

func applyGatewayConfiguration(set *commandFlags, environment map[string]string, configuration config.Configuration, applications, nodes *stringList, backend *string, modelsMax *int, startupTimeout *time.Duration) error {
	gatewayConfiguration := configuration.Gateway
	if !set.changed("application") {
		*applications = append(*applications, gatewayConfiguration.Applications...)
	}
	if !set.changed("backend") && gatewayConfiguration.LlamaCPP.Backend != nil {
		*backend = *gatewayConfiguration.LlamaCPP.Backend
	}
	if !set.changed("models-max") && gatewayConfiguration.LlamaCPP.ModelsMax != nil {
		*modelsMax = *gatewayConfiguration.LlamaCPP.ModelsMax
	}
	if !set.changed("startup-timeout") && gatewayConfiguration.StartupTimeout != nil {
		configuredTimeout, err := time.ParseDuration(*gatewayConfiguration.StartupTimeout)
		if err != nil {
			return controlerr.New("invalid [gateway].startup_timeout in %s: %v", configuration.Path, err)
		}
		*startupTimeout = configuredTimeout
	}
	if !set.changed("render-node") && !renderNodeEnvironmentConfigured(environment) && len(gatewayConfiguration.RenderNodes) > 0 {
		*nodes = append(*nodes, gatewayConfiguration.RenderNodes...)
	}
	return nil
}

func (app *App) discoverGatewayRegistry(managed catalog.Catalog, dataRoot, profile string, renderNodes []string) (gateway.Registry, []string, []gateway.Diagnostic, error) {
	candidates, err := app.builtGatewayApplications()
	if err != nil {
		return gateway.Registry{}, nil, nil, err
	}
	if len(candidates) == 0 {
		return gateway.Registry{}, nil, nil, fmt.Errorf("no gateway application image is built; build llama-cpp or dwarfstar first")
	}
	return gateway.DiscoverRegistry(managed, dataRoot, candidates, profile, renderNodes)
}

func (app *App) builtGatewayApplications() ([]string, error) {
	applications := make([]string, 0, 2)
	for _, identifier := range []string{string(textmodel.BackendLlamaCPP), string(textmodel.BackendDwarfStar)} {
		application, _ := config.ApplicationByID(identifier)
		present, err := app.podman().Exists(app.Context, "image", application.Image)
		if err != nil {
			return nil, err
		}
		if present {
			applications = append(applications, identifier)
		}
	}
	return applications, nil
}

func (app *App) writeGatewayStartup(summary gatewayStartup) {
	terminal := app.terminal(app.Stdout)
	modelCount := len(summary.Registry.Models)
	fingerprint := summary.Registry.Fingerprint
	if len(fingerprint) > 12 {
		fingerprint = fingerprint[:12]
	}
	profile := summary.Profile
	if len(summary.RenderNodes) > 0 {
		profile += " · " + strings.Join(summary.RenderNodes, ", ")
	}
	rows := [][2]string{
		{"Endpoint", summary.Endpoint},
		{"Profile", profile},
		{"Applications", strings.Join(summary.Applications, ", ")},
		{"Inventory", fmt.Sprintf("%d verified %s · %s", modelCount, plural(modelCount, "model", "models"), fingerprint)},
	}
	if summary.Configuration != "" {
		rows = append(rows, [2]string{"Configuration", summary.Configuration})
	}
	for _, application := range summary.Applications {
		if application == "llama-cpp" {
			rows = append(rows, [2]string{"llama.cpp", fmt.Sprintf("%s · up to %d loaded %s", summary.Backend, summary.ModelsMax, plural(summary.ModelsMax, "model", "models"))})
			break
		}
	}
	rows = append(rows,
		[2]string{"Backend", terminal.State("unloaded") + " — starts with the first request"},
		[2]string{"Inspect", terminal.Command(identity.Command("status", "gateway", "--gateway-url", summary.Endpoint))},
	)
	fmt.Fprintln(app.Stdout, terminal.Heading(identity.DisplayName+" gateway"))
	writeDetailRows(app.Stdout, terminal, rows)
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Muted("Waiting for requests. Press Ctrl-C to stop."))
}

func (app *App) gatewayStatus(rawURL string, recentRequests int) error {
	base, err := parseGatewayURL(firstNonEmpty(rawURL, config.EnvironmentValue(app.Environment, "GATEWAY_URL", config.DefaultGatewayURL)))
	if err != nil {
		return err
	}
	statusURL := *base
	statusURL.Path = "/paracetamol/v1/status"
	statusURL.RawQuery = ""
	if recentRequests > 0 {
		statusURL.RawQuery = url.Values{"requests": []string{fmt.Sprint(recentRequests)}}.Encode()
	}
	statusURL.Fragment = ""
	ctx, cancel := context.WithTimeout(app.Context, 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, statusURL.String(), nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return controlerr.New("cannot reach gateway at %s: %v", base, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return controlerr.New("gateway status returned HTTP %d", response.StatusCode)
	}
	var status gateway.Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return controlerr.New("cannot decode gateway status: %v", err)
	}
	if status.Schema != gateway.StatusSchema {
		return controlerr.New("gateway returned unsupported status schema %q", status.Schema)
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s\n", terminal.Heading(identity.DisplayName+" gateway"))
	writeStatusRows(app.Stdout, terminal, [][2]string{
		{"State", status.Gateway},
		{"Started", status.StartedAt},
		{"Applications", strings.Join(status.Applications, ", ")},
		{"Allocation", firstNonEmpty(string(status.Scheduler.Allocation), "none")},
		{"Backend state", string(status.Scheduler.State)},
		{"Requests", fmt.Sprintf("%d active, %d queued", status.Scheduler.ActiveRequests, status.Scheduler.QueuedRequests)},
		{"Inventory", status.InventoryFingerprint},
	})
	rows := make([][]string, 0, len(status.Models))
	for _, model := range status.Models {
		detail := terminal.Command(model.ID)
		if model.Diagnostic != "" {
			detail += " — " + terminal.Muted(model.Diagnostic)
		}
		rows = append(rows, []string{terminal.State(string(model.State)), terminal.Label(model.Application), detail})
	}
	lines, _ := ui.ColumnLines(rows, nil, "  ")
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	if status.Usage.Overall.Requests > 0 {
		fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Usage since start"))
		writeStatusRows(app.Stdout, terminal, [][2]string{
			{"Requests", gatewayAggregateOutcomes(status.Usage.Overall)},
			{"Tokens", gatewayAggregateTokens(status.Usage.Overall.Tokens, status.Usage.Overall.Requests)},
			{"Mean timing", gatewayAggregateTiming(status.Usage.Overall.Timing)},
		})
		if len(status.Usage.Models) > 0 {
			usageRows := make([][]string, 0, len(status.Usage.Models))
			for _, model := range status.Usage.Models {
				usageRows = append(usageRows, []string{
					terminal.Command(model.Model),
					fmt.Sprintf("%d requests", model.Usage.Requests),
					terminal.Muted(gatewayAggregateTokens(model.Usage.Tokens, model.Usage.Requests)),
				})
			}
			modelLines, _ := ui.ColumnLines(usageRows, nil, "  ")
			for _, line := range modelLines {
				fmt.Fprintln(app.Stdout, "  "+line)
			}
		}
	}
	if recentRequests > 0 {
		if len(status.Usage.Sessions) > 0 {
			fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Recent sessions"))
			for _, session := range status.Usage.Sessions {
				fmt.Fprintf(app.Stdout, "  %s  %d requests · %s\n", terminal.Command(session.ID), session.Usage.Requests,
					terminal.Muted(gatewayAggregateTokens(session.Usage.Tokens, session.Usage.Requests)))
			}
		}
		fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Recent requests"))
		if len(status.RecentRequests) == 0 {
			fmt.Fprintln(app.Stdout, terminal.Muted("No completed requests are recorded."))
			return nil
		}
		for _, record := range status.RecentRequests {
			statusText := "no HTTP response"
			if record.HTTPStatus != 0 {
				statusText = fmt.Sprintf("HTTP %d", record.HTTPStatus)
			}
			model := firstNonEmpty(record.Model, "unknown model")
			requestRows, _ := ui.ColumnLines([][]string{{
				terminal.State(string(record.Outcome)), terminal.Command(record.ID), terminal.Command(model),
				fmt.Sprintf("%s · %s total · %s wait · %s upstream", statusText,
					gatewayDuration(record.Timing.TotalMilliseconds), gatewayDuration(record.Timing.GatewayWaitMilliseconds), gatewayDuration(record.Timing.UpstreamMilliseconds)),
			}}, nil, "  ")
			fmt.Fprintln(app.Stdout, requestRows[0])
			fmt.Fprintf(app.Stdout, "    %s\n", terminal.Muted(gatewayRequestMetadata(record)))
		}
	}
	return nil
}

func gatewayDuration(milliseconds int64) string {
	if milliseconds < 0 {
		milliseconds = 0
	}
	maxMilliseconds := int64(math.MaxInt64 / int64(time.Millisecond))
	if milliseconds > maxMilliseconds {
		milliseconds = maxMilliseconds
	}
	return (time.Duration(milliseconds) * time.Millisecond).String()
}

func gatewayRequestMetadata(record gateway.RequestRecord) string {
	parts := []string{firstNonEmpty(record.Application, "no application"), firstNonEmpty(record.Peer, "unknown peer")}
	if record.SessionID != "" {
		parts = append(parts, "session "+record.SessionID)
	}
	if record.UserAgent != "" {
		parts = append(parts, record.UserAgent)
	}
	if record.Stream {
		parts = append(parts, "stream")
	} else {
		parts = append(parts, "non-stream")
	}
	tokens := []string{}
	if record.Tokens.Input != nil {
		tokens = append(tokens, fmt.Sprintf("%d input", *record.Tokens.Input))
	}
	if record.Tokens.Output != nil {
		tokens = append(tokens, fmt.Sprintf("%d output", *record.Tokens.Output))
	}
	if record.Tokens.Cached != nil {
		tokens = append(tokens, fmt.Sprintf("%d cached", *record.Tokens.Cached))
	}
	if record.Tokens.Reasoning != nil {
		tokens = append(tokens, fmt.Sprintf("%d reasoning", *record.Tokens.Reasoning))
	}
	if len(tokens) == 0 {
		parts = append(parts, "tokens unavailable")
	} else {
		parts = append(parts, strings.Join(tokens, ", "))
	}
	parts = append(parts, gatewayControlMetadata(record.Controls))
	return strings.Join(parts, " · ")
}

func gatewayAggregateOutcomes(usage gateway.RequestAggregate) string {
	parts := []string{fmt.Sprintf("%d completed", usage.Requests)}
	for _, outcome := range []struct {
		count uint64
		label string
	}{
		{usage.Outcomes.Succeeded, "succeeded"},
		{usage.Outcomes.Rejected, "rejected"},
		{usage.Outcomes.Canceled, "canceled"},
		{usage.Outcomes.QueueFull, "queue-full"},
		{usage.Outcomes.BackendStartFailed, "backend-start-failed"},
		{usage.Outcomes.UpstreamError, "upstream-error"},
		{usage.Outcomes.Other, "other"},
	} {
		if outcome.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", outcome.count, outcome.label))
		}
	}
	return strings.Join(parts, " · ")
}

func gatewayAggregateTokens(tokens gateway.AggregateTokens, requests uint64) string {
	parts := []string{}
	for _, token := range []struct {
		metric gateway.AggregateMetric
		label  string
	}{
		{tokens.Input, "input"},
		{tokens.Output, "output"},
		{tokens.Cached, "cached"},
		{tokens.Reasoning, "reasoning"},
	} {
		if token.metric.Observations == 0 {
			continue
		}
		value := fmt.Sprintf("%d %s", token.metric.Total, token.label)
		if token.metric.Observations != requests {
			value += fmt.Sprintf(" (%d/%d reported)", token.metric.Observations, requests)
		}
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return "unavailable"
	}
	return strings.Join(parts, " · ")
}

func gatewayAggregateTiming(timing gateway.AggregateTiming) string {
	return fmt.Sprintf("%s wait · %s upstream · %s total",
		gatewayMeanDuration(timing.GatewayWait), gatewayMeanDuration(timing.Upstream), gatewayMeanDuration(timing.Total))
}

func gatewayMeanDuration(metric gateway.AggregateMetric) string {
	if metric.Observations == 0 {
		return "unavailable"
	}
	mean := metric.Total / metric.Observations
	maxMilliseconds := uint64(math.MaxInt64 / int64(time.Millisecond))
	if mean > maxMilliseconds {
		mean = maxMilliseconds
	}
	return gatewayDuration(int64(mean))
}

func gatewayControlMetadata(controls gateway.RequestControls) string {
	reasoning := explicitGatewayControls(controls.Reasoning)
	samplers := explicitGatewayControls(controls.Sampling)
	parts := []string{}
	if len(reasoning) == 0 {
		parts = append(parts, "reasoning defaults")
	} else {
		parts = append(parts, strings.Join(reasoning, ", "))
	}
	if len(samplers) == 0 {
		parts = append(parts, "sampler defaults")
	} else {
		parts = append(parts, strings.Join(samplers, ", "))
	}
	if len(controls.Diagnostics) > 0 {
		parts = append(parts, fmt.Sprintf("%d control diagnostics", len(controls.Diagnostics)))
	}
	return strings.Join(parts, " · ")
}

func explicitGatewayControls(controls []gateway.ObservedControl) []string {
	result := []string{}
	for _, control := range controls {
		switch control.Source {
		case gateway.ControlClient:
			result = append(result, control.Name+"="+control.Value)
		case gateway.ControlInvalid:
			result = append(result, control.Name+"=invalid")
		}
	}
	return result
}

func parseGatewayURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, controlerr.Usage("--gateway-url must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" && parsed.Path != "/v1" && parsed.Path != "/v1/" {
		return nil, controlerr.Usage("--gateway-url path must be /v1")
	}
	parsed.Path = "/v1"
	return parsed, nil
}
