package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	listen := firstNonEmpty(*listenFlag, config.EnvironmentValue(app.Environment, "GATEWAY_LISTEN", config.DefaultListen))
	if err := config.ValidateListenAddress(listen); err != nil {
		return err
	}
	port, err := config.ValidatePort(firstNonEmpty(*portFlag, config.EnvironmentValue(app.Environment, "GATEWAY_PORT", "8080")))
	if err != nil {
		return err
	}
	profile := firstNonEmpty(*profileFlag, config.EnvironmentValue(app.Environment, "PROFILE", "auto"))
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
	lifecycle, err := gateway.NewContainerLifecycle(gateway.LifecycleOptions{
		Catalog: managed, Registry: registry, DataRoot: dataRoot, Profile: profile,
		RenderNodes: selectedNodes, LlamaBackend: *backend, LlamaModelsMax: *modelsMax,
		RouterPreset: routerPath, SourceRevision: app.projectRevision(),
		VolumeSuffix: app.podman().SELinuxVolumeSuffix(app.Context), Unconfined: *unconfined,
		StartupTimeout: *startupTimeout, Runner: app.Runner, Log: app.Stderr,
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
	handler, err := gateway.NewServer(registry, scheduler, app.Stderr)
	if err != nil {
		return errors.Join(err, scheduler.Shutdown(contextWithoutCancel()))
	}
	server := &http.Server{
		Handler: handler, ErrorLog: log.New(app.Stderr, "gateway http: ", 0),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
	}
	if !isLoopback(listen) {
		fmt.Fprintf(app.Stderr, "%s gateway is published on %s:%d without authentication.\n", errorTerminal.Warning("WARNING:"), listen, port)
	}
	app.writeGatewayStartup(gatewayStartup{
		Endpoint:     "http://" + net.JoinHostPort(listen, fmt.Sprint(port)) + "/v1",
		Applications: applications, Profile: profile, RenderNodes: selectedNodes,
		Backend: *backend, ModelsMax: *modelsMax, Registry: registry,
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
	Endpoint     string
	Applications []string
	Profile      string
	RenderNodes  []string
	Backend      string
	ModelsMax    int
	Registry     gateway.Registry
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

func (app *App) gatewayStatus(rawURL string) error {
	base, err := parseGatewayURL(firstNonEmpty(rawURL, config.EnvironmentValue(app.Environment, "GATEWAY_URL", config.DefaultGatewayURL)))
	if err != nil {
		return err
	}
	statusURL := *base
	statusURL.Path = "/paracetamol/v1/status"
	statusURL.RawQuery = ""
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
		rows = append(rows, []string{terminal.State(string(model.State)), terminal.Label(model.Application), terminal.Command(model.ID)})
	}
	lines, _ := ui.ColumnLines(rows, nil, "  ")
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	return nil
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
