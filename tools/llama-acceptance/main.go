// llama-acceptance is an explicitly invoked maintainer check. Normal builds,
// catalog operations and unit tests never start inference through this tool.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"paracetamol/internal/config"
	"paracetamol/internal/project"
	"paracetamol/tools/llama-acceptance/internal/coding"
	"paracetamol/tools/llama-acceptance/internal/contract"
	"paracetamol/tools/llama-acceptance/internal/scenarios"
)

const regularModel = "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"

var errIncomplete = errors.New("acceptance is not PASS; inspect the saved case results")

func help(out io.Writer) {
	fmt.Fprint(out, `llama.cpp maintainer acceptance

Usage: llama-acceptance <preflight|check|compare|prepare-grader|prepare-native|prepare-control|self-test> [options]

  preflight       Validate tracked fixtures and readonly gateway/backend evidence.
  check           Run explicit production-gateway cases; never start/stop a backend.
  compare         Compare two saved runs without inference.
  prepare-grader  Write the pinned CPU grading build recipe, without building it.
  prepare-native  Write a matching-source native-probe build recipe, without building it.
  prepare-control Write an isolated source-level negative-control build recipe.
  self-test       Grade trusted solutions and planted defects, without inference.

Use each operation with --help. A fresh output directory below checkout build/
is required for writes. Results never overwrite an earlier attempt. Model
installation, grading-image builds and gateway lifecycle remain explicit.
Short cases alone do not qualify an upgrade. See the tool README for the full
source-update sequence, native gates, long-context cases and comparison rules.
`)
}

type runOptions struct {
	control                                     string
	endpoint, model, output, keyFile, suite     string
	containerID, imageID, dataRoot, graderImage string
	prefillTolerance                            float64
}

func parseRun(args []string, stderr io.Writer) (runOptions, bool, error) {
	var opts runOptions
	set := flag.NewFlagSet("llama-acceptance", flag.ContinueOnError)
	set.SetOutput(stderr)
	set.StringVar(&opts.endpoint, "gateway-url", "", "explicit numeric-loopback gateway URL, optionally ending in /v1")
	set.StringVar(&opts.model, "model", regularModel+"-dflash2", "regular Flash-Next model or its -dflash2 control")
	set.StringVar(&opts.output, "out", "", "new private directory below checkout build/")
	set.StringVar(&opts.keyFile, "gateway-api-key-file", "", "absolute private gateway Bearer-key file")
	set.StringVar(&opts.suite, "suite", "short", "short, lifecycle, coding, history, prefill or all")
	set.StringVar(&opts.containerID, "container-id", "", "full ID of the already-running gateway-owned llama.cpp container")
	set.StringVar(&opts.imageID, "image-id", "", "expected immutable backend image ID, not a tag")
	set.StringVar(&opts.dataRoot, "data-dir", "", "explicit existing verified Paracetamol data root")
	set.StringVar(&opts.graderImage, "grader-image", "", "locally built immutable CPU grading image ID (coding/all)")
	set.Float64Var(&opts.prefillTolerance, "prefill-regression-percent", -1, "predeclared prefill tolerance from baseline variability; required for prefill/all")
	set.StringVar(&opts.control, "control", "", "diagnostic image only: mmq-sum (short) or qsa-host-span (prefill)")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, true, nil
		}
		return opts, false, err
	}
	if set.NArg() != 0 {
		return opts, false, fmt.Errorf("unexpected positional arguments")
	}
	if opts.endpoint == "" || opts.containerID == "" || opts.imageID == "" || opts.dataRoot == "" {
		return opts, false, fmt.Errorf("require --gateway-url, --container-id, --image-id and --data-dir; start and warm the ordinary gateway explicitly first")
	}
	if opts.model != regularModel && opts.model != regularModel+"-dflash2" {
		return opts, false, fmt.Errorf("this suite qualifies only regular Flash-Next and its matching DFlash2 control")
	}
	if math.IsNaN(opts.prefillTolerance) || math.IsInf(opts.prefillTolerance, 0) {
		return opts, false, fmt.Errorf("prefill tolerance must be finite")
	}
	switch opts.suite {
	case "short", "lifecycle", "coding", "history", "prefill", "all":
	default:
		return opts, false, fmt.Errorf("unsupported suite %q", opts.suite)
	}
	if opts.suite == "prefill" || opts.suite == "all" {
		if math.IsNaN(opts.prefillTolerance) || math.IsInf(opts.prefillTolerance, 0) || opts.prefillTolerance < 0 || opts.prefillTolerance > 100 {
			return opts, false, fmt.Errorf("prefill/all requires --prefill-regression-percent in 0..100, chosen before running the candidate")
		}
	}
	if opts.control != "" && !(opts.control == "mmq-sum" && opts.suite == "short" || opts.control == "qsa-host-span" && opts.suite == "prefill") {
		return opts, false, fmt.Errorf("diagnostic controls require mmq-sum with short or qsa-host-span with prefill; they cannot qualify a normal upgrade")
	}
	return opts, false, nil
}

func fixtureManifest() (map[string]string, error) {
	if err := scenarios.ValidateFixtures(); err != nil {
		return nil, err
	}
	if err := coding.ValidateFixtures(); err != nil {
		return nil, err
	}
	manifest := map[string]string{}
	for key, value := range scenarios.FixtureManifest() {
		manifest["scenarios/"+key] = value
	}
	for key, value := range coding.Manifest() {
		manifest["coding/"+key] = value
	}
	return manifest, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		help(stdout)
		return nil
	}
	switch args[0] {
	case "preflight", "check":
		opts, done, err := parseRun(args[1:], stderr)
		if err != nil || done {
			return err
		}
		return runGatewayChecks(ctx, args[0], opts, stdout)
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "prepare-native":
		return runPrepareNative(ctx, args[1:], stdout, stderr)
	case "prepare-control":
		return runPrepareControl(ctx, args[1:], stdout, stderr)
	case "prepare-grader":
		set := flag.NewFlagSet(args[0], flag.ContinueOnError)
		set.SetOutput(stderr)
		out := set.String("out", "", "new private build directory for the grading recipe")
		if err := set.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if set.NArg() != 0 || *out == "" {
			return fmt.Errorf("prepare-grader requires only --out")
		}
		root, err := project.Root()
		if err != nil {
			return err
		}
		outPath, err := validateNewOutput(root, *out)
		if err != nil {
			return err
		}
		path, err := coding.Prepare(outPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Prepared %s\nNo image built and nothing downloaded.\n", path)
		return nil
	case "self-test":
		set := flag.NewFlagSet(args[0], flag.ContinueOnError)
		set.SetOutput(stderr)
		out := set.String("out", "", "new private build directory for sandbox self-tests")
		image := set.String("grader-image", "", "exact locally built grading image ID")
		sourceOnly := set.Bool("source-only", false, "validate public fixture identities/oracles only; no Podman or inference")
		if err := set.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if _, err := fixtureManifest(); err != nil {
			return err
		}
		if *sourceOnly {
			if set.NArg() != 0 || *out != "" || *image != "" {
				return fmt.Errorf("self-test --source-only takes no image, output or positional arguments")
			}
			fmt.Fprintln(stdout, "PASS  public fixture identities and source-level oracle controls\nNo container or inference run.")
			return nil
		}
		if set.NArg() != 0 || *out == "" || *image == "" {
			return fmt.Errorf("self-test requires --out and --grader-image, or --source-only")
		}
		root, err := project.Root()
		if err != nil {
			return err
		}
		dir, err := validateNewOutput(root, *out)
		if err != nil {
			return err
		}
		results, runErr := coding.SelfTest(ctx, coding.Options{ImageID: *image, OutputDir: dir})
		if err := savePrivateJSON(filepath.Join(dir, "summary.json"), results); err != nil {
			return errors.Join(runErr, err)
		}
		printResults(stdout, results)
		if runErr == nil && contract.Worst(results) != contract.Pass {
			runErr = errIncomplete
		}
		return runErr
	default:
		return fmt.Errorf("unknown operation %q; use --help", args[0])
	}
}

func readKey(path string) (string, error) {
	// Reuse the host's private-file validation without loading configuration or
	// inheriting unrelated gateway environment variables.
	return config.SelectGatewayClientKey(path, map[string]string{}, config.Configuration{})
}

func printResults(out io.Writer, results []contract.Result) {
	for _, result := range results {
		fmt.Fprintf(out, "%s  %s", result.Status, result.Name)
		if result.Detail != "" {
			fmt.Fprintf(out, "  %s", result.Detail)
		}
		fmt.Fprintln(out)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		if errors.Is(err, errIncomplete) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
