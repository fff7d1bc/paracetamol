// Package cli owns command parsing, help, and top-level error presentation.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"rocmplete/internal/identity"
	"rocmplete/internal/project"
)

// Main executes the host control plane and returns a process exit status.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	_ = ctx
	parser := flag.NewFlagSet(identity.CommandName, flag.ContinueOnError)
	parser.SetOutput(stderr)
	parser.Usage = func() { writeRootHelp(stderr) }
	showVersion := parser.Bool("version", false, "show version and exit")
	if err := parser.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "%s %s\n", identity.DisplayName, identity.Version)
		return 0
	}
	remaining := parser.Args()
	if len(remaining) == 0 {
		writeRootHelp(stdout)
		return 0
	}
	if remaining[0] == "help" || remaining[0] == "-h" || remaining[0] == "--help" {
		writeRootHelp(stdout)
		return 0
	}
	if _, err := project.Root(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "error: command %q has not been ported to Go yet\n", remaining[0])
	return 2
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
	fmt.Fprintf(output, "Usage: ./%s [--version] COMMAND [OPTIONS]\n\n", identity.CommandName)
	fmt.Fprintf(output, "%s builds and runs locally pinned AI applications in constrained containers.\n\n", identity.DisplayName)
	fmt.Fprintln(output, "Commands:")
	for _, command := range commands {
		fmt.Fprintf(output, "  %-*s  %s\n", width, command.name, command.description)
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "Run './%s COMMAND --help' for command-specific help.\n", strings.TrimSpace(identity.CommandName))
}
