package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"rocmplete/internal/application"
	"rocmplete/internal/benchmark"
	"rocmplete/internal/catalog"
	"rocmplete/internal/controlerr"
	"rocmplete/internal/identity"
	"rocmplete/internal/podman"
	"rocmplete/internal/process"
)

type App struct {
	Context     context.Context
	Root        string
	Environment map[string]string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Runner      process.Runner
	Executor    process.Executor
	catalog     *catalog.Catalog
}

var managedRunID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

type commandDefinition struct {
	Name        string
	Description string
	Run         func(*App, []string) error
}

var commandTree = []commandDefinition{
	{Name: "build", Description: "build locally pinned application images", Run: func(app *App, args []string) error { return app.commandBuild(args) }},
	{Name: "guide", Description: "show a focused application walkthrough", Run: func(app *App, args []string) error { return app.commandGuide(args) }},
	{Name: "doctor", Description: "inspect host readiness and GPU policy", Run: func(app *App, args []string) error { return app.commandDoctor(args) }},
	{Name: "acceptance", Description: "run target-hardware smoke acceptance", Run: func(app *App, args []string) error { return app.commandAcceptance(args) }},
	{Name: "status", Description: "show managed images, containers, devices, and data", Run: func(app *App, args []string) error { return app.commandStatus(args) }},
	{Name: "run", Description: "run a managed application", Run: func(app *App, args []string) error { return app.commandRun(args) }},
	{Name: "shell", Description: "open a constrained application shell", Run: func(app *App, args []string) error { return app.commandShell(args) }},
	{Name: "logs", Description: "show managed container logs", Run: func(app *App, args []string) error { return app.commandLogs(args) }},
	{Name: "stop", Description: "stop managed containers", Run: func(app *App, args []string) error { return app.commandStop(args) }},
	{Name: "content", Description: "list, install, import, and inspect managed content", Run: func(app *App, args []string) error { return app.commandContent(args) }},
	{Name: "agent", Description: "install or run a managed coding agent", Run: func(app *App, args []string) error { return app.commandAgent(args) }},
	{Name: "benchmark", Description: "run or report managed performance evaluations", Run: func(app *App, args []string) error { return app.commandBenchmark(args) }},
	{Name: "images", Description: "export or import locally built images", Run: func(app *App, args []string) error { return app.commandImages(args) }},
	{Name: "cleanup", Description: "remove one explicit managed resource scope", Run: func(app *App, args []string) error { return app.commandCleanup(args) }},
}

func checkpointThenReturn(path string, value any, cause error) error {
	if err := benchmark.WriteCheckpoint(path, value); err != nil {
		return fmt.Errorf("%w; additionally could not save checkpoint: %v", cause, err)
	}
	return cause
}

func withCleanupFailure(cause error, description string, cleanup error) error {
	if cleanup == nil {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("%s: %w", description, cleanup))
}

func environment() map[string]string {
	result := make(map[string]string)
	for _, item := range os.Environ() {
		name, value, ok := strings.Cut(item, "=")
		if ok {
			result[name] = value
		}
	}
	return result
}

func (app *App) podman() podman.Client { return podman.Client{Runner: app.Runner} }

func (app *App) executor() process.Executor {
	if app.Executor == nil {
		return process.OSExecutor{}
	}
	return app.Executor
}

func (app *App) managedCatalog() (catalog.Catalog, error) {
	if app.catalog != nil {
		return *app.catalog, nil
	}
	loaded, err := catalog.Load(app.Root + "/catalog/catalog.json")
	if err != nil {
		return catalog.Catalog{}, err
	}
	app.catalog = &loaded
	return loaded, nil
}

func (app *App) Dispatch(args []string) error {
	if err := application.Validate(); err != nil {
		return fmt.Errorf("invalid application registry: %w", err)
	}
	if len(args) == 0 {
		return controlerr.Usage("choose a command")
	}
	for _, command := range commandTree {
		if command.Name == args[0] {
			return command.Run(app, args[1:])
		}
	}
	return controlerr.Usage("unknown command %q", args[0])
}

func (app *App) flags(name, usage string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(app.Stderr)
	set.Usage = func() { fmt.Fprintln(app.Stderr, usage) }
	return set
}

func (app *App) run(command []string, capture bool) (process.Result, error) {
	if len(command) == 0 {
		return process.Result{}, fmt.Errorf("empty command")
	}
	request := process.Command{Name: command[0], Args: command[1:]}
	if !capture {
		request.Stdin = app.Stdin
		request.Stdout = app.Stdout
		request.Stderr = app.Stderr
	}
	result, err := app.Runner.Run(app.Context, request)
	if err != nil {
		return result, err
	}
	if result.Status != 0 {
		return result, &controlerr.Error{Message: fmt.Sprintf("command failed with exit status %d", result.Status), Status: result.Status}
	}
	return result, nil
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func requireChoice(value, description string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return controlerr.Usage("%s must be one of %s", description, strings.Join(allowed, ", "))
}

func groupHelpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help")
}

func writeGroupHelp(output io.Writer, usage string, commands ...[2]string) {
	fmt.Fprintln(output, usage)
	if len(commands) == 0 {
		return
	}
	fmt.Fprintln(output, "\nCommands:")
	width := 0
	for _, command := range commands {
		if len(command[0]) > width {
			width = len(command[0])
		}
	}
	for _, command := range commands {
		fmt.Fprintf(output, "  %-*s  %s\n", width, command[0], command[1])
	}
}

func usage(arguments ...string) string { return "Usage: " + identity.Command(arguments...) }
