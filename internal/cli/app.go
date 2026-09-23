package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"paracetamol/internal/application"
	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/ui"
)

type App struct {
	Context         context.Context
	Root            string
	Environment     map[string]string
	ConfigSelection config.Selection
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	Runner          process.Runner
	Executor        process.Executor
	catalog         *catalog.Catalog
	configuration   *config.Configuration
	promptInput     *bufio.Reader
}

func (app *App) hostConfiguration() (config.Configuration, error) {
	if app.configuration != nil {
		return *app.configuration, nil
	}
	loaded, err := config.Load(app.Environment, app.ConfigSelection)
	if err != nil {
		return config.Configuration{}, err
	}
	app.configuration = &loaded
	return loaded, nil
}

var managedRunID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

type commandDefinition struct {
	Name        string
	Description string
	Run         func(*App, []string) error
}

var commandTree = []commandDefinition{
	{Name: "config", Description: "initialize host configuration", Run: func(app *App, args []string) error { return app.commandConfig(args) }},
	{Name: "build", Description: "build locally pinned application images", Run: func(app *App, args []string) error { return app.commandBuild(args) }},
	{Name: "guide", Description: "show a focused application walkthrough", Run: func(app *App, args []string) error { return app.commandGuide(args) }},
	{Name: "doctor", Description: "inspect host readiness and GPU policy", Run: func(app *App, args []string) error { return app.commandDoctor(args) }},
	{Name: "acceptance", Description: "run target-hardware smoke acceptance", Run: func(app *App, args []string) error { return app.commandAcceptance(args) }},
	{Name: "status", Description: "show managed images, containers, devices, and data", Run: func(app *App, args []string) error { return app.commandStatus(args) }},
	{Name: "run", Description: "run a managed application", Run: func(app *App, args []string) error { return app.commandRun(args) }},
	{Name: "shell", Description: "open a constrained application shell", Run: func(app *App, args []string) error { return app.commandShell(args) }},
	{Name: "logs", Description: "show managed container logs", Run: func(app *App, args []string) error { return app.commandLogs(args) }},
	{Name: "stop", Description: "stop the host gateway or managed containers", Run: func(app *App, args []string) error { return app.commandStop(args) }},
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
	writeRootHelp(app.Stdout)
	return controlerr.Usage("unknown command %q", args[0])
}

type commandFlags struct {
	*flag.FlagSet
	app       *App
	synopsis  string
	examples  []string
	arguments [][2]string
	order     []*flag.Flag
	aliases   map[string]string
}

func (set *commandFlags) record(name string) {
	set.order = append(set.order, set.Lookup(name))
}

func (set *commandFlags) Bool(name string, value bool, usage string) *bool {
	result := set.FlagSet.Bool(name, value, usage)
	set.record(name)
	return result
}

func (set *commandFlags) BoolVar(target *bool, name string, value bool, usage string) {
	set.FlagSet.BoolVar(target, name, value, usage)
	set.record(name)
}

func (set *commandFlags) String(name, value, usage string) *string {
	result := set.FlagSet.String(name, value, usage)
	set.record(name)
	return result
}

func (set *commandFlags) StringVar(target *string, name, value, usage string) {
	set.FlagSet.StringVar(target, name, value, usage)
	set.record(name)
}

func (set *commandFlags) Int(name string, value int, usage string) *int {
	result := set.FlagSet.Int(name, value, usage)
	set.record(name)
	return result
}

func (set *commandFlags) IntVar(target *int, name string, value int, usage string) {
	set.FlagSet.IntVar(target, name, value, usage)
	set.record(name)
}

func (set *commandFlags) Int64(name string, value int64, usage string) *int64 {
	result := set.FlagSet.Int64(name, value, usage)
	set.record(name)
	return result
}

func (set *commandFlags) Float64(name string, value float64, usage string) *float64 {
	result := set.FlagSet.Float64(name, value, usage)
	set.record(name)
	return result
}

func (set *commandFlags) Duration(name string, value time.Duration, usage string) *time.Duration {
	result := set.FlagSet.Duration(name, value, usage)
	set.record(name)
	return result
}

func (set *commandFlags) Var(value flag.Value, name, usage string) {
	set.FlagSet.Var(value, name, usage)
	set.record(name)
}

func (set *commandFlags) VarWithShort(value flag.Value, name, short, usage string) {
	set.FlagSet.Var(value, name, usage)
	set.FlagSet.Var(value, short, usage)
	set.record(name)
	if set.aliases == nil {
		set.aliases = make(map[string]string)
	}
	set.aliases[name] = short
}

func (set *commandFlags) Argument(name, description string) {
	set.arguments = append(set.arguments, [2]string{name, description})
}

func (set *commandFlags) renderHelp(output io.Writer) {
	terminal := ui.New(output, set.app.Environment)
	fmt.Fprintln(output, terminal.Heading(set.synopsis))
	if len(set.arguments) > 0 {
		width := 0
		for _, argument := range set.arguments {
			width = max(width, ui.DisplayWidth(argument[0]))
		}
		fmt.Fprintf(output, "\n%s\n", terminal.Heading("Arguments:"))
		for _, argument := range set.arguments {
			fmt.Fprintf(output, "  %s  %s\n", terminal.Command(fmt.Sprintf("%-*s", width, argument[0])), argument[1])
		}
	}
	width := len("-h, --help")
	for _, global := range globalConfigurationHelp {
		if len(global[0]) > width {
			width = len(global[0])
		}
	}
	for _, option := range set.order {
		if candidate := len(set.flagSyntax(option)); candidate > width {
			width = candidate
		}
	}
	fmt.Fprintf(output, "\n%s\n", terminal.Heading("Options:"))
	fmt.Fprintf(output, "  %s  %s\n", terminal.Command(fmt.Sprintf("%-*s", width, "-h, --help")), "show this help")
	for _, global := range globalConfigurationHelp {
		fmt.Fprintf(output, "  %s  %s\n", terminal.Command(fmt.Sprintf("%-*s", width, global[0])), global[1])
	}
	for _, option := range set.order {
		syntax := set.flagSyntax(option)
		description := option.Usage
		if option.DefValue != "" && option.DefValue != "false" && option.DefValue != "0" && option.DefValue != "-1" {
			description += " (default: " + option.DefValue + ")"
		}
		fmt.Fprintf(output, "  %s  %s\n", terminal.Command(fmt.Sprintf("%-*s", width, syntax)), description)
	}
	if len(set.examples) > 0 {
		fmt.Fprintf(output, "\n%s\n", terminal.Heading("Examples:"))
		for _, example := range set.examples {
			fmt.Fprintf(output, "  %s\n", terminal.Command(example))
		}
	}
}

func (set *commandFlags) changed(name string) bool {
	changed := false
	short := set.aliases[name]
	set.Visit(func(option *flag.Flag) {
		if option.Name == name || option.Name == short {
			changed = true
		}
	})
	return changed
}

func (set *commandFlags) usageError(format string, arguments ...any) error {
	set.renderHelp(set.app.Stdout)
	return controlerr.Usage(format, arguments...)
}

func (app *App) flags(name, usage string) *commandFlags {
	return app.flagsWithExamples(name, usage, examplesFor(name))
}

func (app *App) flagsWithExamples(name, synopsis string, examples []string) *commandFlags {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	set := &commandFlags{FlagSet: flags, app: app, synopsis: synopsis, examples: examples}
	set.Usage = func() { set.renderHelp(app.Stderr) }
	return set
}

func (set *commandFlags) flagSyntax(option *flag.Flag) string {
	value := "--" + option.Name
	if short := set.aliases[option.Name]; short != "" {
		value = "-" + short + ", " + value
	}
	if !isBooleanFlag(option.Value) {
		value += " " + flagMetavar(option.Name)
	}
	return value
}

func (app *App) terminal(writer io.Writer) ui.Terminal {
	return ui.New(writer, app.Environment)
}

func (app *App) promptLine(message string, leadingBlank bool) (string, error) {
	fmt.Fprint(app.Stdout, app.terminal(app.Stdout).PromptText(message, leadingBlank))
	if app.promptInput == nil {
		app.promptInput = bufio.NewReader(app.Stdin)
	}
	type result struct {
		line string
		err  error
	}
	completed := make(chan result, 1)
	go func() {
		line, err := app.promptInput.ReadString('\n')
		completed <- result{line: line, err: err}
	}()
	ctx := app.Context
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		if terminalReader(app.Stdin) {
			fmt.Fprintln(app.Stdout)
		}
		return "", ctx.Err()
	case read := <-completed:
		if read.err != nil && (read.err != io.EOF || read.line == "") {
			if errors.Is(read.err, io.EOF) {
				if terminalReader(app.Stdin) {
					fmt.Fprintln(app.Stdout)
				}
				return "", controlerr.New("input closed; cancelled")
			}
			return "", read.err
		}
		return strings.TrimSpace(read.line), nil
	}
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

func (app *App) writeGroupHelp(synopsis string, commands ...[2]string) {
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading(synopsis))
	if len(commands) == 0 {
		return
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Commands:"))
	width := 0
	for _, command := range commands {
		if len(command[0]) > width {
			width = len(command[0])
		}
	}
	for _, command := range commands {
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Command(fmt.Sprintf("%-*s", width, command[0])), command[1])
	}
	optionWidth := len("-h, --help")
	for _, option := range globalConfigurationHelp {
		optionWidth = max(optionWidth, len(option[0]))
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Options:"))
	fmt.Fprintf(app.Stdout, "  %s  show this help\n", terminal.Command(fmt.Sprintf("%-*s", optionWidth, "-h, --help")))
	for _, option := range globalConfigurationHelp {
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Command(fmt.Sprintf("%-*s", optionWidth, option[0])), option[1])
	}
	if examples := examplesForSynopsis(synopsis); len(examples) > 0 {
		fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Examples:"))
		for _, example := range examples {
			fmt.Fprintf(app.Stdout, "  %s\n", terminal.Command(example))
		}
	}
}

func usage(arguments ...string) string { return "Usage: " + identity.Command(arguments...) }
