package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"paracetamol/internal/agent"
	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
)

func (app *App) commandAgent(args []string) error {
	if groupHelpRequested(args) {
		writeGroupHelp(app.Stdout, usage("agent", "COMMAND", "[OPTIONS]"),
			[2]string{"install", "install the pinned managed Pi runtime"},
			[2]string{"run", "run managed Pi or Maki"})
		return nil
	}
	if len(args) == 0 {
		return controlerr.Usage("choose agent install or run")
	}
	switch args[0] {
	case "install":
		return app.agentInstall(args[1:])
	case "run":
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return controlerr.Usage("choose agent run pi or maki")
		}
		switch args[1] {
		case "pi":
			return app.agentPi(args[2:])
		case "maki":
			return app.agentMaki(args[2:])
		default:
			return controlerr.Usage("unknown agent client %q", args[1])
		}
	default:
		return controlerr.Usage("unknown agent client %q", args[0])
	}
}

func (app *App) agentInstall(args []string) error {
	set := app.flags("agent install", usage("agent", "install", "pi", "[--data-dir PATH]"))
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
	fmt.Fprintf(app.Stdout, "%s: Pi %s\nRuntime: %s\nSystem Node.js: %s (%s)\n", state, runtime.PackageVersion, runtime.Root, runtime.NodeVersion, runtime.Node)
	return nil
}

func (app *App) agentPi(args []string) error {
	set := app.flags("agent run pi", usage("agent", "run", "pi", "[OPTIONS]", "[-- PI-ARGS]"))
	gatewayURL := set.String("gateway-url", "", "gateway base URL ending in /v1")
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
	endpoint := firstNonEmpty(*gatewayURL, config.EnvironmentValue(app.Environment, "GATEWAY_URL", config.DefaultGatewayURL))
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
	plan, err := agent.CreatePiPlan(app.Context, managed, app.Root, endpoint, upstreamArgs, runtime)
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
		fmt.Fprintf(app.Stderr, "Pi remote gateway\n  endpoint          %s\nWARNING: prompts and tool results cross this unauthenticated connection. Trust the remote host and network boundary.\n", plan.Endpoint)
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
		fmt.Fprintf(app.Stderr, "Pi sandbox\n  Writable project  %s\n  Private state     %s\n  Network           host network retained for %s\n", sandboxPlan.Workdir, sandboxPlan.StateRoot, plan.Endpoint)
		command = sandboxPlan.Command
		environment = envSliceMap(sandboxPlan.Environment)
	}
	return app.execProcess(command, environment)
}

func (app *App) agentMaki(args []string) error {
	set := app.flags("agent run maki", usage("agent", "run", "maki", "[OPTIONS]", "[-- MAKI-ARGS]"))
	gatewayURL := set.String("gateway-url", "", "gateway base URL ending in /v1")
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
	endpoint := firstNonEmpty(*gatewayURL, config.EnvironmentValue(app.Environment, "GATEWAY_URL", config.DefaultGatewayURL))
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	plan, err := agent.CreateMakiPlan(app.Context, managed, app.Root, endpoint, upstreamArgs, app.Environment)
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
		fmt.Fprintf(app.Stderr, "Maki remote gateway\n  endpoint          %s\nWARNING: prompts and tool results cross this unauthenticated connection. Trust the remote host and network boundary.\n", plan.Endpoint)
	}
	if *sandbox {
		sandboxPlan, err := agent.CreateSandboxPlan(app.Context, app.Runner, command, dataRoot, mustWorkingDirectory(), "maki", map[string]string{}, app.Environment, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(app.Stderr, "Maki sandbox\n  Writable project  %s\n  Private state     %s\n  Network           host network retained for %s\n", sandboxPlan.Workdir, sandboxPlan.StateRoot, plan.Endpoint)
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
