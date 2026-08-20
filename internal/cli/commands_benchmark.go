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
	"rocmplete/internal/runtime"
	"rocmplete/internal/storage"
)

func (app *App) commandBenchmark(args []string) error {
	if groupHelpRequested(args) {
		writeGroupHelp(app.Stdout, "Usage: ./rocmplete benchmark COMMAND [OPTIONS]",
			[2]string{"comfyui", "run one managed ComfyUI benchmark"},
			[2]string{"suite", "run or resume an ordered ComfyUI suite"},
			[2]string{"agent", "evaluate a model on frozen coding tasks"},
			[2]string{"llama-cpp", "measure llama-bench or compare backends"},
			[2]string{"llama-cpp-speculative", "screen speculative-decoding depths"},
			[2]string{"report", "render a stored ComfyUI result"})
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return controlerr.Usage("choose benchmark comfyui, suite, agent, llama-cpp, llama-cpp-speculative, or report")
	}
	switch args[0] {
	case "comfyui":
		return app.benchmarkComfyUI(args[1:])
	case "suite":
		return app.benchmarkSuite(args[1:])
	case "agent":
		return app.benchmarkAgent(args[1:])
	case "llama-cpp":
		return app.benchmarkLlama(args[1:])
	case "llama-cpp-speculative":
		return app.benchmarkSpeculative(args[1:])
	case "report":
		return app.benchmarkReport(args[1:])
	default:
		return controlerr.Usage("unknown benchmark command %q", args[0])
	}
}

func (app *App) benchmarkLlama(args []string) error {
	set := app.flags("benchmark llama-cpp", "Usage: ./rocmplete benchmark llama-cpp (--model FILE | --preset NAME) [OPTIONS]")
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
	if err := set.Parse(args); err != nil {
		return err
	}
	if boolCount(*modelFlag != "", *presetFlag != "") != 1 {
		return controlerr.Usage("choose exactly one of --model or --preset")
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
	if !*dryRun {
		if err := (storage.Layout{Root: dataRoot}).PrepareRuntime("llama-cpp"); err != nil {
			return err
		}
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	model, managedModel := "", ""
	modelMetadata := map[string]any{}
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
		modelMetadata = map[string]any{"kind": "catalog", "preset": *presetFlag, "path": content.ArtifactPath(dataRoot, artifact), "repository": artifact.Source.Repository, "revision": artifact.Source.Revision, "source_path": artifact.Source.Path, "size": artifact.Size, "sha256": artifact.SHA256}
	}
	application, _ := config.ApplicationByID("llama-cpp")
	image := firstNonEmpty(*imageFlag, application.Image)
	backends := []string{*backend}
	if *compare {
		backends = []string{"rocm", "vulkan"}
	}
	parameters := map[string]any{"repetitions": *repetitions, "prompt_tokens": *promptTokens, "generation_tokens": *generationTokens, "context_depth": *contextDepth, "batch_size": *batch, "ubatch_size": *ubatch, "cache_type_k": *cacheK, "cache_type_v": *cacheV, "flash_attention": *flash}
	commands := make(map[string][]string)
	for _, candidate := range backends {
		commands[candidate] = runtime.LlamaBenchmarkCommand(runtime.LlamaBenchmarkOptions{Image: image, Profile: profile, DataDir: dataRoot, Backend: candidate, Model: model, ManagedModel: managedModel, RenderNodes: selectedNodes, Repetitions: *repetitions, PromptTokens: *promptTokens, GenerationTokens: *generationTokens, ContextDepth: *contextDepth, BatchSize: *batch, UBatchSize: *ubatch, CacheTypeK: *cacheK, CacheTypeV: *cacheV, FlashAttention: *flash, Unconfined: *unconfined}, app.podman().SELinuxVolumeSuffix(app.Context))
	}
	fmt.Fprintf(app.Stdout, "Model: %s\nParameters: depth %d, pp%d, tg%d, batch %d/%d, KV %s/%s, FA %s, %d repetitions\n", modelMetadata["path"], *contextDepth, *promptTokens, *generationTokens, *batch, *ubatch, *cacheK, *cacheV, *flash, *repetitions)
	if *dryRun {
		for _, candidate := range backends {
			fmt.Fprintf(app.Stdout, "\nBackend: %s\nResolved command:\n  %s\n", candidate, shellJoin(commands[candidate]))
		}
		fmt.Fprintln(app.Stdout, "No container was started.")
		return nil
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
	results := make(map[string]map[string]any)
	errorsByBackend := make(map[string]string)
	for _, candidate := range backends {
		rows, runErr := benchmark.RunLlama(app.Context, app.Runner, commands[candidate])
		_, _ = app.run([]string{"podman", "rm", "--force", "--time", "0", "--ignore", "rocmplete-llama-cpp-benchmark"}, true)
		if runErr != nil {
			errorsByBackend[candidate] = runErr.Error()
			if !*compare {
				return runErr
			}
			continue
		}
		path := benchmark.DefaultPath((storage.Layout{Root: dataRoot}).LlamaBenchmarks(), "-"+candidate+".json")
		if !*compare && *output != "" {
			path, err = absoluteNewPath(*output)
			if err != nil {
				return err
			}
		}
		run := benchmark.LlamaRun{Image: map[string]any{"reference": image, "id": imageID}, Profile: profile, Backend: candidate, RenderNodes: selectedNodes, Model: modelMetadata, Parameters: parameters, Results: rows}
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
		results[candidate] = map[string]any{"status": "pass", "result": path, "rates": rates}
		fmt.Fprintf(app.Stdout, "Benchmark complete (%s): %s\n", candidate, path)
	}
	if !*compare {
		return nil
	}
	comparisonPath := benchmark.DefaultPath((storage.Layout{Root: dataRoot}).LlamaBenchmarks(), "-backend-comparison.json")
	if *output != "" {
		comparisonPath, err = absoluteNewPath(*output)
		if err != nil {
			return err
		}
	}
	comparison := map[string]any{"schema": "rocmplete.llama-backend-comparison.v1", "created_at": benchmark.Timestamp(), "image": map[string]any{"reference": image, "id": imageID}, "profile": profile, "render_nodes": selectedNodes, "model": modelMetadata, "parameters": parameters, "backends": results, "errors": errorsByBackend}
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
	set := app.flags("benchmark report", "Usage: ./rocmplete benchmark report SUITE.json [--report-format markdown|html|both] [--output PATH]")
	format := set.String("report-format", "both", "markdown, html, or both")
	output := set.String("output", "", "single report output path")
	subject, remaining := leadingPositional(args)
	if err := set.Parse(remaining); err != nil {
		return err
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
	value, err := benchmark.ReadObject(subject)
	if err != nil {
		return err
	}
	if value["schema"] != benchmark.ComfySuiteSchema {
		return controlerr.New("unsupported benchmark suite: %s", subject)
	}
	paths, err := benchmark.WriteSuiteReports(subject, value, *format, *output)
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
