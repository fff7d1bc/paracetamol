// Package cli owns command parsing, help, and top-level error presentation.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
	"paracetamol/internal/process"
	"paracetamol/internal/project"
	"paracetamol/internal/ui"
)

// Main executes the host control plane and returns a process exit status.
func Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	environmentValues := environment()
	errorTerminal := ui.New(stderr, environmentValues)
	remaining, configurationSelection, selectionErr := extractConfigurationSelection(args)
	if selectionErr != nil {
		fmt.Fprintf(stderr, "%s %v\n", errorTerminal.Error("error:"), selectionErr)
		var controlled *controlerr.Error
		if errors.As(selectionErr, &controlled) {
			return controlled.Status
		}
		return 1
	}
	args = remaining
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintf(stdout, "%s %s\n", identity.DisplayName, identity.Version)
		return 0
	}
	if len(args) == 0 {
		writeRootHelp(stdout)
		return 2
	}
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		writeRootHelp(stdout)
		return 0
	}
	root, err := project.Root()
	if err != nil {
		fmt.Fprintf(stderr, "%s %v\n", errorTerminal.Error("error:"), err)
		return 1
	}
	app := App{Context: ctx, Root: root, Environment: environmentValues, ConfigSelection: configurationSelection, Stdin: stdin, Stdout: stdout, Stderr: stderr, Runner: process.OSRunner{}, Executor: process.OSExecutor{}}
	err = app.Dispatch(args)
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var controlled *controlerr.Error
	if errors.As(err, &controlled) {
		fmt.Fprintf(stderr, "%s %s\n", errorTerminal.Error("error:"), controlled.Message)
		return controlled.Status
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintf(stderr, "%s interrupted\n", errorTerminal.Error("error:"))
		return 130
	}
	fmt.Fprintf(stderr, "%s %v\n", errorTerminal.Error("error:"), err)
	return 1
}

func writeRootHelp(output io.Writer) {
	width := 0
	for _, command := range commandTree {
		if len(command.Name) > width {
			width = len(command.Name)
		}
	}
	fmt.Fprintf(output, "Usage: ./%s COMMAND [OPTIONS]\n\n", identity.CommandName)
	fmt.Fprintf(output, "%s builds and runs locally pinned AI applications in constrained containers.\n\n", identity.DisplayName)
	terminal := ui.New(output, environment())
	fmt.Fprintln(output, terminal.Heading("Commands:"))
	for _, command := range commandTree {
		fmt.Fprintf(output, "  %s  %s\n", terminal.Command(fmt.Sprintf("%-*s", width, command.Name)), command.Description)
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "Options:\n  -h, --help            show this help\n  --version             show the %s version\n  -c, --config PATH     select this configuration file\n  --no-config           ignore the optional XDG configuration\n\n", identity.DisplayName)
	fmt.Fprintln(output, terminal.Heading("Typical lifecycle:"))
	for _, arguments := range [][]string{{"config", "init"}, {"guide", "APPLICATION"}, {"build", "APPLICATION"}, {"content", "install", "APPLICATION", "RECIPE"}, {"run", "APPLICATION"}, {"status"}} {
		fmt.Fprintf(output, "  %s\n", terminal.Command(identity.Command(arguments...)))
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "Run './%s COMMAND --help' for command-specific help.\n", strings.TrimSpace(identity.CommandName))
}
