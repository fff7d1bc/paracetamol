package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"rocmplete/internal/catalog"
	"rocmplete/internal/controlerr"
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
	catalog     *catalog.Catalog
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
	if len(args) == 0 {
		return controlerr.Usage("choose a command")
	}
	switch args[0] {
	case "build":
		return app.commandBuild(args[1:])
	case "guide":
		return app.commandGuide(args[1:])
	case "status":
		return app.commandStatus(args[1:])
	case "run":
		return app.commandRun(args[1:])
	case "shell":
		return app.commandShell(args[1:])
	case "logs":
		return app.commandLogs(args[1:])
	case "stop":
		return app.commandStop(args[1:])
	case "cleanup":
		return app.commandCleanup(args[1:])
	case "content":
		return app.commandContent(args[1:])
	case "agent":
		return app.commandAgent(args[1:])
	case "images":
		return app.commandImages(args[1:])
	case "doctor":
		return app.commandDoctor(args[1:])
	case "benchmark":
		return app.commandBenchmark(args[1:])
	case "acceptance":
		return app.commandAcceptance(args[1:])
	default:
		return controlerr.Usage("unknown command %q", args[0])
	}
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
