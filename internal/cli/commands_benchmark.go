package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rocmplete/internal/benchmark"
	"rocmplete/internal/config"
	"rocmplete/internal/content"
	"rocmplete/internal/controlerr"
	"rocmplete/internal/platform"
	"rocmplete/internal/podman"
	"rocmplete/internal/runtime"
	"rocmplete/internal/storage"
)

func (app *App) commandBenchmark(args []string) error {
	if groupHelpRequested(args) {
		writeGroupHelp(app.Stdout, usage("benchmark", "COMMAND", "[OPTIONS]"),
			[2]string{"comfyui run", "run one managed ComfyUI benchmark"},
			[2]string{"comfyui suite", "run or resume an ordered ComfyUI suite"},
			[2]string{"agent", "evaluate a model on frozen coding tasks"},
			[2]string{"llama-cpp throughput", "measure llama-bench or compare backends"},
			[2]string{"llama-cpp speculative", "screen speculative-decoding depths"},
			[2]string{"report", "render a stored ComfyUI result"})
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return controlerr.Usage("choose benchmark comfyui, llama-cpp, agent, or report")
	}
	switch args[0] {
	case "comfyui":
		if len(args) == 1 || groupHelpRequested(args[1:]) {
			writeGroupHelp(app.Stdout, usage("benchmark", "comfyui", "COMMAND", "[OPTIONS]"), [2]string{"run", "run one exact bundle"}, [2]string{"suite", "run or resume an ordered suite"})
			return nil
		}
		switch args[1] {
		case "run":
			return app.benchmarkComfyUI(args[2:])
		case "suite":
			return app.benchmarkSuite(args[2:])
		default:
			return controlerr.Usage("unknown ComfyUI benchmark command %q", args[1])
		}
	case "agent":
		return app.benchmarkAgent(args[1:])
	case "llama-cpp":
		if len(args) == 1 || groupHelpRequested(args[1:]) {
			writeGroupHelp(app.Stdout, usage("benchmark", "llama-cpp", "COMMAND", "[OPTIONS]"), [2]string{"throughput", "run llama-bench"}, [2]string{"speculative", "screen speculative-decoding depths"})
			return nil
		}
		switch args[1] {
		case "throughput":
			return app.benchmarkLlama(args[2:])
		case "speculative":
			return app.benchmarkSpeculative(args[2:])
		default:
			return controlerr.Usage("unknown llama.cpp benchmark command %q", args[1])
		}
	case "report":
		return app.benchmarkReport(args[1:])
	default:
		return controlerr.Usage("unknown benchmark command %q", args[0])
	}
}

func (app *App) benchmarkLlama(args []string) error {
	set := app.flags("benchmark llama-cpp throughput", usage("benchmark", "llama-cpp", "throughput", "(--model FILE | --preset NAME)", "[OPTIONS]"))
	modelFlag := set.String("model", "", "exact local GGUF file")
	presetFlag := set.String("preset", "", "installed catalog preset")
	profileFlag := set.String("profile", "", "execution profile")
	backend := set.String("backend", "rocm", "rocm or vulkan")
	compare := set.Bool("compare-backends", false, "run ROCm and Vulkan")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact GPU render node; repeatable")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override image")
	repetitions := set.Int("repetitions", 5, "repetitions per test")
	promptTokens := set.Int("prompt-tokens", 512, "prompt-processing tokens")
	generationTokens := set.Int("generation-tokens", 128, "generation tokens")
	contextDepth := set.Int("context-depth", 0, "tokens already present")
	batch := set.Int("batch-size", 2048, "logical batch size")
	ubatch := set.Int("ubatch-size", 512, "physical microbatch size")
	cacheK := set.String("cache-type-k", "f16", "f16, q8_0, or q4_0")
	cacheV := set.String("cache-type-v", "f16", "f16, q8_0, or q4_0")
	flash := set.String("flash-attn", "auto", "on, off, or auto")
	output := set.String("output", "", "new result or comparison JSON")
	unconfined := set.Bool("unconfined", false, "disable seccomp")
	dryRun := set.Bool("dry-run", false, "print resolved command")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("llama.cpp throughput accepts no positional arguments")
	}
	if boolCount(*modelFlag != "", *presetFlag != "") != 1 {
		return controlerr.Usage("choose exactly one of --model or --preset")
	}
	selectedOutput := ""
	if *output != "" {
		var err error
		selectedOutput, err = absoluteNewPath(*output)
		if err != nil {
			return err
		}
	}
	for name, value := range map[string]int{"--repetitions": *repetitions, "--prompt-tokens": *promptTokens, "--generation-tokens": *generationTokens, "--batch-size": *batch, "--ubatch-size": *ubatch} {
		if value < 1 {
			return controlerr.Usage("%s must be at least 1", name)
		}
	}
	if *contextDepth < 0 || *ubatch > *batch {
		return controlerr.Usage("context depth must be non-negative and ubatch must not exceed batch")
	}
	if err := requireChoice(*backend, "backend", "rocm", "vulkan"); err != nil {
		return err
	}
	if err := requireChoice(*cacheK, "key cache", "f16", "q8_0", "q4_0"); err != nil {
		return err
	}
	if err := requireChoice(*cacheV, "value cache", "f16", "q8_0", "q4_0"); err != nil {
		return err
	}
	if err := requireChoice(*flash, "Flash Attention", "on", "off", "auto"); err != nil {
		return err
	}
	if *cacheV != "f16" && *flash != "on" {
		return controlerr.Usage("a quantized value cache requires --flash-attn on")
	}
	profile := firstNonEmpty(*profileFlag, config.EnvironmentValue(app.Environment, "PROFILE", "auto"))
	if err := platform.ValidateProfile(profile); err != nil {
		return controlerr.Usage("%v", err)
	}
	if *compare && profile == "cpu" {
		return controlerr.Usage("--compare-backends requires a GPU profile")
	}
	selectedNodes, err := app.resolveDevices(profile, nodes, nodes != nil)
	if err != nil {
		return err
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, !*dryRun)
	if err != nil {
		return err
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	model, managedModel := "", ""
	modelMetadata := benchmark.ModelIdentity{}
	if *modelFlag != "" {
		model, err = regularFile(*modelFlag, "GGUF model")
		if err != nil {
			return err
		}
		if strings.ToLower(filepath.Ext(model)) != ".gguf" {
			return controlerr.Usage("--model must name a .gguf file")
		}
		modelMetadata, err = benchmark.FileMetadata(model)
		if err != nil {
			return err
		}
	} else {
		preset, ok := managed.LlamaPresets[*presetFlag]
		if !ok {
			return controlerr.Usage("unknown llama.cpp preset %q", *presetFlag)
		}
		if preset.SpeculativeType != "" {
			return controlerr.Usage("preset %q enables %s; use a non-speculative preset with llama-bench", *presetFlag, preset.SpeculativeType)
		}
		bundle := managed.Bundles[preset.Bundle]
		if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
			return controlerr.New("preset %q is not installed: %v", *presetFlag, err)
		}
		artifact := managed.Artifacts[preset.Artifact]
		managedModel = artifact.Destination
		modelMetadata = benchmark.ModelIdentity{Kind: "catalog", Preset: *presetFlag, Path: content.ArtifactPath(dataRoot, artifact), Repository: artifact.Source.Repository, Revision: artifact.Source.Revision, SourcePath: artifact.Source.Path, Size: artifact.Size, SHA256: artifact.SHA256}
	}
	application, _ := config.ApplicationByID("llama-cpp")
	image := firstNonEmpty(*imageFlag, application.Image)
	backends := []string{*backend}
	if *compare {
		backends = []string{"rocm", "vulkan"}
	}
	parameters := benchmark.LlamaParameters{Repetitions: *repetitions, PromptTokens: *promptTokens, GenerationTokens: *generationTokens, ContextDepth: *contextDepth, BatchSize: *batch, UBatchSize: *ubatch, CacheTypeK: *cacheK, CacheTypeV: *cacheV, FlashAttention: *flash}
	commands := make(map[string][]string)
	for _, candidate := range backends {
		commands[candidate] = runtime.LlamaBenchmarkCommand(runtime.LlamaBenchmarkOptions{Image: image, Profile: profile, DataDir: dataRoot, Backend: candidate, Model: model, ManagedModel: managedModel, RenderNodes: selectedNodes, Repetitions: *repetitions, PromptTokens: *promptTokens, GenerationTokens: *generationTokens, ContextDepth: *contextDepth, BatchSize: *batch, UBatchSize: *ubatch, CacheTypeK: *cacheK, CacheTypeV: *cacheV, FlashAttention: *flash, Unconfined: *unconfined}, app.podman().SELinuxVolumeSuffix(app.Context))
	}
	fmt.Fprintf(app.Stdout, "Model: %s\nParameters: depth %d, pp%d, tg%d, batch %d/%d, KV %s/%s, FA %s, %d repetitions\n", modelMetadata.Path, *contextDepth, *promptTokens, *generationTokens, *batch, *ubatch, *cacheK, *cacheV, *flash, *repetitions)
	if *dryRun {
		for _, candidate := range backends {
			fmt.Fprintf(app.Stdout, "\nBackend: %s\nResolved command:\n  %s\n", candidate, shellJoin(commands[candidate]))
		}
		fmt.Fprintln(app.Stdout, "No container was started.")
		return nil
	}
	if err := (storage.Layout{Root: dataRoot}).PrepareRuntime("llama-cpp"); err != nil {
		return err
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	present, err := app.podman().Exists(app.Context, "image", image)
	if err != nil {
		return err
	}
	if !present {
		return controlerr.New("image not found: %s", image)
	}
	imageID, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", image}, "cannot inspect benchmark image")
	if err != nil {
		return err
	}
	results := make(map[string]benchmark.LlamaBackendResult)
	errorsByBackend := make(map[string]string)
	for _, candidate := range backends {
		rows, runErr := benchmark.RunLlama(app.Context, app.Runner, commands[candidate])
		runErr = withCleanupFailure(runErr, "clean up llama.cpp benchmark container", app.podman().RemoveContainer(contextWithoutCancel(), runtime.LlamaBenchmarkContainer, 0, podman.Streams{}))
		if runErr != nil {
			errorsByBackend[candidate] = runErr.Error()
			if !*compare {
				return runErr
			}
			continue
		}
		path := benchmark.DefaultPath((storage.Layout{Root: dataRoot}).LlamaBenchmarks(), "-"+candidate+".json")
		if !*compare && selectedOutput != "" {
			path = selectedOutput
		}
		run := benchmark.LlamaRun{Image: benchmark.ImageIdentity{Reference: image, ID: imageID}, Profile: profile, Backend: candidate, RenderNodes: selectedNodes, Model: modelMetadata, Parameters: parameters, Results: rows}
		if err := benchmark.WriteLlama(path, run); err != nil {
			return err
		}
		value, err := benchmark.ReadLlama(path)
		if err != nil {
			return err
		}
		rates, err := benchmark.Rates(value)
		if err != nil {
			return err
		}
		results[candidate] = benchmark.LlamaBackendResult{Status: "pass", Result: path, Rates: rates}
		fmt.Fprintf(app.Stdout, "Benchmark complete (%s): %s\n", candidate, path)
	}
	if !*compare {
		return nil
	}
	comparisonPath := benchmark.DefaultPath((storage.Layout{Root: dataRoot}).LlamaBenchmarks(), "-backend-comparison.json")
	if selectedOutput != "" {
		comparisonPath = selectedOutput
	}
	comparison := benchmark.LlamaComparison{Schema: benchmark.LlamaComparisonSchema, CreatedAt: benchmark.Timestamp(), Image: benchmark.ImageIdentity{Reference: image, ID: imageID}, Profile: profile, RenderNodes: selectedNodes, Model: modelMetadata, Parameters: parameters, Backends: results, Errors: errorsByBackend}
	if err := benchmark.WriteJSON(comparisonPath, comparison); err != nil {
		return err
	}
	fmt.Fprintf(app.Stdout, "Backend comparison: %s\n", comparisonPath)
	if len(results) != len(backends) {
		return controlerr.New("one or more backend benchmarks failed; comparison preserved at %s", comparisonPath)
	}
	return nil
}

func (app *App) benchmarkReport(args []string) error {
	set := app.flags("benchmark report", usage("benchmark", "report", "SUITE.json", "[--report-format markdown|html|both]", "[--output PATH]"))
	format := set.String("report-format", "both", "markdown, html, or both")
	output := set.String("output", "", "single report output path")
	subject, remaining := leadingPositional(args)
	if err := parseFlags(set, remaining); err != nil {
		return err
	}
	if subject == "" && len(set.Args()) > 0 {
		subject = set.Args()[0]
		if len(set.Args()) > 1 {
			return controlerr.Usage("benchmark report accepts exactly one suite")
		}
	} else if len(set.Args()) > 0 {
		return controlerr.Usage("benchmark report accepts exactly one suite")
	}
	if subject == "" {
		return controlerr.Usage("choose a suite JSON")
	}
	if err := requireChoice(*format, "report format", "markdown", "html", "both"); err != nil {
		return err
	}
	if *output != "" && *format == "both" {
		return controlerr.Usage("--output requires one report format")
	}
	var value benchmark.ComfySuite
	if err := benchmark.ReadJSON(subject, &value, true); err != nil {
		return err
	}
	if value.Schema != benchmark.ComfySuiteSchema {
		return controlerr.New("unsupported benchmark suite: %s", subject)
	}
	paths, err := benchmark.WriteSuiteReports(subject, value, *format, *output, false)
	if err != nil {
		return err
	}
	for _, path := range paths {
		fmt.Fprintf(app.Stdout, "Wrote report: %s\n", path)
	}
	return nil
}

func absoluteNewPath(value string) (string, error) {
	path, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if status, err := os.Lstat(path); err == nil {
		return "", controlerr.New("refusing to replace existing result %s (%s)", path, status.Mode())
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
