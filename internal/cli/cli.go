// Package cli owns command parsing, help, and top-level error presentation.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"rocmplete/internal/controlerr"
	"rocmplete/internal/identity"
	"rocmplete/internal/process"
	"rocmplete/internal/project"
)

// Main executes the host control plane and returns a process exit status.
func Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	app := App{Context: ctx, Root: root, Environment: environment(), Stdin: stdin, Stdout: stdout, Stderr: stderr, Runner: process.OSRunner{}}
	err = app.Dispatch(args)
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var controlled *controlerr.Error
	if errors.As(err, &controlled) {
		fmt.Fprintf(stderr, "error: %s\n", controlled.Message)
		return controlled.Status
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "error: interrupted")
		return 130
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	return 1
}

func writeRootHelp(output io.Writer) {
	commands := []struct {
		name        string
		description string
	}{
		{"build", "build locally pinned application images"},
		{"guide", "show a focused application walkthrough"},
		{"agent", "install or run a managed coding agent"},
		{"images", "export or import locally built images"},
		{"doctor", "inspect host readiness and GPU policy"},
		{"acceptance", "run target-hardware smoke acceptance"},
		{"status", "show managed images, containers, devices, and data"},
		{"run", "run a managed application"},
		{"shell", "open a constrained application shell"},
		{"logs", "show managed container logs"},
		{"stop", "stop managed containers"},
		{"cleanup", "remove one explicit managed resource scope"},
		{"content", "list, install, import, and inspect managed content"},
		{"benchmark", "run or report managed performance evaluations"},
	}
	width := 0
	for _, command := range commands {
		if len(command.name) > width {
			width = len(command.name)
		}
	}
	fmt.Fprintf(output, "Usage: ./%s COMMAND [OPTIONS]\n\n", identity.CommandName)
	fmt.Fprintf(output, "%s builds and runs locally pinned AI applications in constrained containers.\n\n", identity.DisplayName)
	fmt.Fprintln(output, "Commands:")
	for _, command := range commands {
		fmt.Fprintf(output, "  %-*s  %s\n", width, command.name, command.description)
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "Run './%s COMMAND --help' for command-specific help.\n", strings.TrimSpace(identity.CommandName))
}
