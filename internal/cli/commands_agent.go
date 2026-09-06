package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"paracetamol/internal/agent"
	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
)

func (app *App) commandAgent(args []string) error {
	writeHelp := func() {
		app.writeGroupHelp(usage("agent", "COMMAND", "[OPTIONS]"),
			[2]string{"install", "install the pinned managed Pi runtime"},
			[2]string{"run", "run managed Pi or Maki"})
	}
	if groupHelpRequested(args) {
		writeHelp()
		return nil
	}
	if len(args) == 0 {
		writeHelp()
		return controlerr.Usage("choose agent install or run")
	}
	switch args[0] {
	case "install":
		return app.agentInstall(args[1:])
	case "run":
		if groupHelpRequested(args[1:]) {
			app.writeGroupHelp(usage("agent", "run", "CLIENT", "[OPTIONS]", "[-- CLIENT-ARGS]"), [2]string{"pi", "run the managed Pi coding agent"}, [2]string{"maki", "run Maki with the managed provider"})
			return nil
		}
		if len(args) == 1 {
			app.writeGroupHelp(usage("agent", "run", "CLIENT", "[OPTIONS]", "[-- CLIENT-ARGS]"), [2]string{"pi", "run the managed Pi coding agent"}, [2]string{"maki", "run Maki with the managed provider"})
			return controlerr.Usage("choose agent run pi or maki")
		}
		if strings.HasPrefix(args[1], "-") {
			app.writeGroupHelp(usage("agent", "run", "CLIENT", "[OPTIONS]", "[-- CLIENT-ARGS]"), [2]string{"pi", "run the managed Pi coding agent"}, [2]string{"maki", "run Maki with the managed provider"})
			return controlerr.Usage("choose agent run pi or maki")
		}
		switch args[1] {
		case "pi":
			return app.agentPi(args[2:])
		case "maki":
			return app.agentMaki(args[2:])
		default:
			app.writeGroupHelp(usage("agent", "run", "CLIENT", "[OPTIONS]", "[-- CLIENT-ARGS]"), [2]string{"pi", "run the managed Pi coding agent"}, [2]string{"maki", "run Maki with the managed provider"})
			return controlerr.Usage("unknown agent client %q", args[1])
		}
	default:
		writeHelp()
		return controlerr.Usage("unknown agent command %q", args[0])
	}
}

func (app *App) agentInstall(args []string) error {
	set := app.flags("agent install", usage("agent", "install", "pi", "[--data-dir PATH]"))
	set.Argument("pi", "the pinned managed Pi npm runtime")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	client, remaining := leadingPositional(args)
	if err := parseFlags(set, remaining); err != nil {
		return err
	}
	if client == "" && len(set.Args()) > 0 {
		client = set.Args()[0]
		if len(set.Args()) > 1 {
			return controlerr.Usage("agent install accepts exactly one client")
		}
	} else if len(set.Args()) > 0 {
		return controlerr.Usage("agent install accepts exactly one client")
	}
	if client == "" {
		return set.usageError("agent install requires client pi")
	}
	if client != "pi" {
		return controlerr.Usage("agent install supports only pi")
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, true)
	if err != nil {
		return err
	}
	runtime, installed, err := agent.InstallPiRuntime(app.Context, app.Runner, dataRoot, app.Root, os.Environ(), app.Stdout, app.Stderr)
	if err != nil {
		return err
	}
	state := "Already installed"
	if installed {
		state = "Installed"
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s Pi %s\n%s %s\n%s %s (%s)\n", terminal.Success(state+":"), runtime.PackageVersion, terminal.Label("Runtime:"), runtime.Root, terminal.Label("System Node.js:"), runtime.NodeVersion, runtime.Node)
	return nil
}

func (app *App) agentPi(args []string) error {
	set := app.flags("agent run pi", usage("agent", "run", "pi", "[OPTIONS]", "[-- PI-ARGS]"))
	gatewayURL := set.String("gateway-url", "", "gateway base URL ending in /v1 (default: "+config.DefaultGatewayURL+")")
	keyFile := set.String("gateway-api-key-file", "", "private gateway client Bearer-key file")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	sandbox := set.Bool("sandbox", true, "confine Pi to the working directory")
	noSandbox := set.Bool("no-sandbox", false, "use the normal host filesystem")
	upstreamArgs, err := parseFlagsWithPassthrough(set, args)
	if err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("Pi arguments must follow --")
	}
	if *noSandbox {
		*sandbox = false
	}
	if setWasSet(set, "sandbox") && *noSandbox {
		return controlerr.Usage("--sandbox and --no-sandbox are mutually exclusive")
	}
	configuration, err := app.hostConfiguration()
	if err != nil {
		return err
	}
	endpoint, err := config.SelectGatewayClientURL(*gatewayURL, app.Environment, configuration)
	if err != nil {
		return controlerr.Usage("invalid gateway client URL: %v", err)
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	runtime, err := agent.ResolvePiRuntime(app.Context, app.Runner, dataRoot, app.Root)
	if err != nil {
		return err
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	if set.changed("gateway-api-key-file") && *keyFile == "" {
		return controlerr.Usage("--gateway-api-key-file must name a private key file")
	}
	apiKey := ""
	if agent.PiMode(upstreamArgs) == "session" {
		apiKey, err = config.SelectGatewayClientKey(*keyFile, app.Environment, configuration)
		if err != nil {
			return err
		}
	}
	plan, err := agent.CreatePiPlan(app.Context, managed, app.Root, endpoint, upstreamArgs, runtime, apiKey)
	if err != nil {
		return err
	}
	if plan.Mode == "passthrough" {
		return app.execProcess(plan.Command, app.Environment)
	}
	dataRoot, err = app.resolveDataDir(*dataFlag, true)
	if err != nil {
		return err
	}
	agentDir, err := agent.PreparePiState(plan, dataRoot)
	if err != nil {
		return err
	}
	command := plan.Command
	environment := agent.PiEnvironment(app.Environment, agentDir, plan.Mode == "session")
	if plan.Remote {
		app.writeAgentRemoteGateway("Pi", plan.Endpoint, plan.Authenticated)
	}
	if *sandbox {
		child := map[string]string{"PI_CODING_AGENT_DIR": filepath.Join(agent.SandboxHome, ".local", "share", "pi", "agent"), "PI_SKIP_VERSION_CHECK": "1", "PI_TELEMETRY": "0"}
		if plan.Mode == "session" {
			child["PI_OFFLINE"] = "1"
		}
		sandboxPlan, err := agent.CreateSandboxPlan(app.Context, app.Runner, command, dataRoot, mustWorkingDirectory(), "pi", child, app.Environment, []agent.Mount{{Source: plan.RuntimeRoot, Destination: plan.RuntimeRoot}})
		if err != nil {
			return err
		}
		terminal := app.terminal(app.Stderr)
		fmt.Fprintln(app.Stderr, terminal.Heading("Pi sandbox"))
		writeDetailRows(app.Stderr, terminal, [][2]string{{"Writable project", sandboxPlan.Workdir}, {"Private state", sandboxPlan.StateRoot}, {"Network", "host network retained for " + plan.Endpoint}})
		command = sandboxPlan.Command
		environment = envSliceMap(sandboxPlan.Environment)
	}
	return app.execProcess(command, environment)
}

func (app *App) agentMaki(args []string) error {
	set := app.flags("agent run maki", usage("agent", "run", "maki", "[OPTIONS]", "[-- MAKI-ARGS]"))
	gatewayURL := set.String("gateway-url", "", "gateway base URL ending in /v1 (default: "+config.DefaultGatewayURL+")")
	keyFile := set.String("gateway-api-key-file", "", "private gateway client Bearer-key file")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	sandbox := set.Bool("sandbox", true, "confine Maki to the working directory")
	noSandbox := set.Bool("no-sandbox", false, "use the normal host filesystem")
	upstreamArgs, err := parseFlagsWithPassthrough(set, args)
	if err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("Maki arguments must follow --")
	}
	if *noSandbox {
		*sandbox = false
	}
	if setWasSet(set, "sandbox") && *noSandbox {
		return controlerr.Usage("--sandbox and --no-sandbox are mutually exclusive")
	}
	configuration, err := app.hostConfiguration()
	if err != nil {
		return err
	}
	endpoint, err := config.SelectGatewayClientURL(*gatewayURL, app.Environment, configuration)
	if err != nil {
		return controlerr.Usage("invalid gateway client URL: %v", err)
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	if set.changed("gateway-api-key-file") && *keyFile == "" {
		return controlerr.Usage("--gateway-api-key-file must name a private key file")
	}
	apiKey := ""
	if agent.MakiMode(upstreamArgs) == "session" {
		apiKey, err = config.SelectGatewayClientKey(*keyFile, app.Environment, configuration)
		if err != nil {
			return err
		}
	}
	plan, err := agent.CreateMakiPlan(app.Context, managed, app.Root, endpoint, upstreamArgs, app.Environment, apiKey)
	if err != nil {
		return err
	}
	if plan.Mode == "passthrough" {
		return app.execProcess(plan.Command, app.Environment)
	}
	dataRoot, err = app.resolveDataDir(*dataFlag, true)
	if err != nil {
		return err
	}
	paths, err := agent.PrepareMakiState(plan, dataRoot)
	if err != nil {
		return err
	}
	command := plan.Command
	environment, err := agent.MakiEnvironment(app.Environment, paths)
	if err != nil {
		return err
	}
	if plan.Remote {
		app.writeAgentRemoteGateway("Maki", plan.Endpoint, plan.Authenticated)
	}
	if *sandbox {
		sandboxPlan, err := agent.CreateSandboxPlan(app.Context, app.Runner, command, dataRoot, mustWorkingDirectory(), "maki", map[string]string{}, app.Environment, nil)
		if err != nil {
			return err
		}
		terminal := app.terminal(app.Stderr)
		fmt.Fprintln(app.Stderr, terminal.Heading("Maki sandbox"))
		writeDetailRows(app.Stderr, terminal, [][2]string{{"Writable project", sandboxPlan.Workdir}, {"Private state", sandboxPlan.StateRoot}, {"Network", "host network retained for " + plan.Endpoint}})
		command = sandboxPlan.Command
		environment = envSliceMap(sandboxPlan.Environment)
	}
	return app.execProcess(command, environment)
}

func mustWorkingDirectory() string {
	value, err := os.Getwd()
	if err != nil {
		return "."
	}
	return value
}

func (app *App) writeAgentRemoteGateway(client, endpoint string, authenticated bool) {
	terminal := app.terminal(app.Stderr)
	authentication := "no key supplied"
	if authenticated {
		authentication = "Bearer key supplied"
	}
	fmt.Fprintln(app.Stderr, terminal.Heading(client+" remote gateway"))
	writeDetailRows(app.Stderr, terminal, [][2]string{{"Endpoint", endpoint}, {"Authentication", authentication}})
	parsed, _ := url.Parse(endpoint)
	if parsed.Scheme == "http" {
		fmt.Fprintf(app.Stderr, "%s HTTP does not encrypt credentials, prompts or tool results. Trust the remote host and network boundary.\n", terminal.Warning("WARNING:"))
	} else {
		fmt.Fprintln(app.Stderr, terminal.Muted("Prompts and tool results are sent to the remote host over HTTPS."))
	}
}

func (app *App) execProcess(command []string, environment map[string]string) error {
	if len(command) == 0 {
		return fmt.Errorf("empty agent command")
	}
	return app.executor().Exec(command[0], command, mapEnvironment(environment))
}

func mapEnvironment(environment map[string]string) []string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+environment[key])
	}
	return result
}

func envSliceMap(values []string) map[string]string {
	result := make(map[string]string)
	for _, item := range values {
		for index := 0; index < len(item); index++ {
			if item[index] == '=' {
				result[item[:index]] = item[index+1:]
				break
			}
		}
	}
	return result
}
