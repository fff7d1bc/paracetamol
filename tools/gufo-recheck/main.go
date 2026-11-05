// gufo-recheck preserves a developer experiment, not a managed application.
// No normal Paracetamol command imports or invokes this package.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func usage(out io.Writer) {
	fmt.Fprint(out, `Gufo developer recheck kit (not a supported application)

Usage: gufo-recheck <prepare|proxy|check> [options]

  prepare  Compose a pinned source-build recipe from the current Containerfile.
  proxy    Apply real gateway policy to an already-running loopback backend.
  check    Run the three frozen Flash-Next quality regressions through a gateway.

proxy and check require --experimental. Neither starts, installs or stops
Gufo. Nothing is added to application discovery, model lists or build-all.
Results are private, create-once files below the checkout's ignored build/.
A passing small screen is not production or broad quality acceptance.

Use a subcommand with --help for its options.
`)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		usage(stdout)
		return nil
	}
	switch args[0] {
	case "prepare":
		return runPrepare(ctx, args[1:], stdout, stderr)
	case "proxy":
		return runProxy(ctx, args[1:], stdout, stderr)
	case "check":
		return runChecks(ctx, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown developer operation %q; use --help", args[0])
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		switch {
		case errors.Is(err, context.Canceled):
			os.Exit(130)
		case errors.Is(err, errQualityFailed):
			os.Exit(2)
		default:
			os.Exit(1)
		}
	}
}
