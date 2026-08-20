package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rocmplete/internal/benchmark"
	"rocmplete/internal/catalog"
	"rocmplete/internal/config"
	"rocmplete/internal/content"
	"rocmplete/internal/controlerr"
	"rocmplete/internal/platform"
	"rocmplete/internal/runtime"
	"rocmplete/internal/storage"
)

type comfyBenchmarkOptions struct {
	profile, dataRoot, image, renderNode string
	port, runs, seed                     int
	unconfined, dryRun                   bool
	memoryPolicy, kernelPolicy           string
	cacheMode                            string
	acceptLicense                        bool
	transform                            func(map[string]any) error
}

func (app *App) comfyBenchmarkFlags(name, usage string, args []string) (*comfyBenchmarkOptions, []string, error) {
	set := app.flags(name, usage)
	profileFlag := set.String("profile", "", "execution profile")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact GPU render node")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override image")
	portText := set.String("port", "8190", "local benchmark server port")
	runs := set.Int("runs", 2, "cold plus warm runs")
	seed := set.Int("seed", 10, "first deterministic seed")
	unconfined := set.Bool("unconfined", false, "disable seccomp")
	dryRun := set.Bool("dry-run", false, "print the validated workload")
	acceptLicense := set.Bool("accept-license", false, "accept catalog model agreements")
	_ = set.Bool("non-interactive", false, "never prompt")
	memory := set.String("memory-policy", "balanced", "balanced or conservative")
	kernel := set.String("kernel-policy", "default", "default or experimental")
	cache := set.String("cache-mode", "persistent", "persistent or isolated")
	if err := set.Parse(args); err != nil {
		return nil, nil, err
	}
	if *runs < 1 {
		return nil, nil, controlerr.Usage("--runs must be at least 1")
	}
	port, err := config.ValidatePort(*portText)
	if err != nil {
		return nil, nil, err
	}
	if err := requireChoice(*memory, "memory policy", "balanced", "conservative"); err != nil {
		return nil, nil, err
	}
	if err := requireChoice(*kernel, "kernel policy", "default", "experimental"); err != nil {
		return nil, nil, err
	}
	if err := requireChoice(*cache, "cache mode", "persistent", "isolated"); err != nil {
		return nil, nil, err
	}
	profile := firstNonEmpty(*profileFlag, config.EnvironmentValue(app.Environment, "PROFILE", "auto"))
	if profile == "cpu" {
		return nil, nil, controlerr.Usage("ComfyUI benchmark requires a GPU profile")
	}
	if err := platform.ValidateProfile(profile); err != nil {
		return nil, nil, controlerr.Usage("%v", err)
	}
	selected, err := app.resolveDevices(profile, nodes, nodes != nil)
	if err != nil {
		return nil, nil, err
	}
	if len(selected) != 1 {
		return nil, nil, controlerr.Usage("ComfyUI benchmark requires exactly one render node")
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, !*dryRun)
	if err != nil {
		return nil, nil, err
	}
	application, _ := config.ApplicationByID("comfyui")
	return &comfyBenchmarkOptions{profile: profile, dataRoot: dataRoot, image: firstNonEmpty(*imageFlag, application.Image), renderNode: selected[0], port: port, runs: *runs, seed: *seed, unconfined: *unconfined, dryRun: *dryRun, memoryPolicy: *memory, kernelPolicy: *kernel, cacheMode: *cache, acceptLicense: *acceptLicense}, set.Args(), nil
}

func (app *App) benchmarkComfyUI(args []string) error {
	bundleID, remaining := leadingPositional(args)
	options, extras, err := app.comfyBenchmarkFlags("benchmark comfyui", "Usage: ./rocmplete benchmark comfyui BUNDLE [OPTIONS]", remaining)
	if err != nil {
		return err
	}
	if bundleID == "" && len(extras) > 0 {
		bundleID, extras = extras[0], extras[1:]
	}
	if bundleID == "" || len(extras) != 0 {
		return controlerr.Usage("choose one exact benchmark bundle")
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	bundle, ok := managed.Bundles[bundleID]
	if !ok || bundle.Application != "comfyui" {
		return controlerr.Usage("unknown ComfyUI bundle %q", bundleID)
	}
	if _, ok := managed.Benchmarks[bundleID]; !ok {
		return controlerr.Usage("bundle %q has no managed benchmark", bundleID)
	}
	if err := requireBenchmarkAgreements(managed, []catalog.Bundle{bundle}, options.acceptLicense, options.dryRun); err != nil {
		return err
	}
	path, _, err := app.executeComfyBenchmark(managed, bundle, *options, "")
	if err != nil {
		return err
	}
	if options.dryRun {
		fmt.Fprintln(app.Stdout, "No container was started.")
	} else {
		fmt.Fprintf(app.Stdout, "Benchmark complete: %s\n", path)
	}
	return nil
}

func (app *App) benchmarkSuite(args []string) error {
	return app.benchmarkSuiteParsed(args)
}

func (app *App) benchmarkSuiteParsed(args []string) error {
	// Re-parse one explicit flag set because Go's flag package intentionally
	// has no argparse-style parent parser. Keeping this local makes the public
	// suite surface obvious and testable.
	set := app.flags("benchmark suite", "Usage: ./rocmplete benchmark suite [OPTIONS]")
	profileFlag := set.String("profile", "", "execution profile")
	var nodes, includes stringList
	set.Var(&nodes, "render-node", "exact GPU render node")
	set.Var(&includes, "include", "explicit bundle; repeatable")
	family := set.String("family", "", "comfyui, qwen, wan, ltx, or hunyuan")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override image")
	portText := set.String("port", "8190", "local benchmark server port")
	runs := set.Int("runs", 2, "cold plus warm runs")
	seed := set.Int("seed", 10, "first deterministic seed")
	unconfined := set.Bool("unconfined", false, "disable seccomp")
	dryRun := set.Bool("dry-run", false, "print the validated workload")
	acceptLicense := set.Bool("accept-license", false, "accept catalog model agreements")
	_ = set.Bool("non-interactive", false, "never prompt")
	memory := set.String("memory-policy", "balanced", "balanced or conservative")
	kernel := set.String("kernel-policy", "default", "default or experimental")
	cache := set.String("cache-mode", "persistent", "persistent or isolated")
	resume := set.String("resume", "", "resume a compatible Go suite JSON")
	keepGoing := set.Bool("keep-going", false, "continue after individual failure")
	reportFormat := set.String("report-format", "both", "markdown, html, both, or none")
	if err := set.Parse(args); err != nil {
		return err
	}
	if len(set.Args()) != 0 {
		return controlerr.Usage("benchmark suite takes no positional arguments")
	}
	if *family != "" {
		if err := requireChoice(*family, "family", "comfyui", "qwen", "wan", "ltx", "hunyuan"); err != nil {
			return err
		}
	}
	if err := requireChoice(*reportFormat, "report format", "markdown", "html", "both", "none"); err != nil {
		return err
	}
	if *runs < 1 {
		return controlerr.Usage("--runs must be at least 1")
	}
	port, err := config.ValidatePort(*portText)
	if err != nil {
		return err
	}
	for value, choices := range map[string][]string{*memory: {"balanced", "conservative"}, *kernel: {"default", "experimental"}, *cache: {"persistent", "isolated"}} {
		if err := requireChoice(value, "suite policy", choices...); err != nil {
			return err
		}
	}
	profile := firstNonEmpty(*profileFlag, config.EnvironmentValue(app.Environment, "PROFILE", "auto"))
	if profile == "cpu" {
		return controlerr.Usage("ComfyUI benchmark requires a GPU profile")
	}
	if err := platform.ValidateProfile(profile); err != nil {
		return err
	}
	selectedNodes, err := app.resolveDevices(profile, nodes, nodes != nil)
	if err != nil {
		return err
	}
	if len(selectedNodes) != 1 {
		return controlerr.Usage("ComfyUI benchmark requires exactly one render node")
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, !*dryRun)
	if err != nil {
		return err
	}
	application, _ := config.ApplicationByID("comfyui")
	options := comfyBenchmarkOptions{profile: profile, dataRoot: dataRoot, image: firstNonEmpty(*imageFlag, application.Image), renderNode: selectedNodes[0], port: port, runs: *runs, seed: *seed, unconfined: *unconfined, dryRun: *dryRun, memoryPolicy: *memory, kernelPolicy: *kernel, cacheMode: *cache, acceptLicense: *acceptLicense}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	includeSet := map[string]bool{}
	for _, id := range includes {
		if _, ok := managed.Benchmarks[id]; !ok {
			return controlerr.Usage("unknown benchmark bundle %q", id)
		}
		includeSet[id] = true
	}
	var bundles []catalog.Bundle
	for _, id := range sortedMapKeys(managed.Benchmarks) {
		bundle := managed.Bundles[id]
		familyMatch := *family == "" || *family == "comfyui" || containsString(bundle.Groups, *family)
		wanted := familyMatch
		if *family == "" && len(includeSet) > 0 {
			wanted = includeSet[id]
		} else if *family != "" && len(includeSet) > 0 {
			wanted = familyMatch || includeSet[id]
		}
		if wanted {
			bundles = append(bundles, bundle)
		}
	}
	if len(bundles) == 0 {
		return controlerr.Usage("benchmark suite selection is empty")
	}
	if err := requireBenchmarkAgreements(managed, bundles, options.acceptLicense, options.dryRun); err != nil {
		return err
	}
	signature, err := comfySuiteSignature(managed, bundles, options)
	if err != nil {
		return err
	}
	if options.dryRun {
		fmt.Fprintf(app.Stdout, "Suite bundles: %d\nSuite signature: %s\n", len(bundles), signature)
		for _, bundle := range bundles {
			fmt.Fprintf(app.Stdout, "\n== %s ==\n", bundle.ID)
			if _, _, err := app.executeComfyBenchmark(managed, bundle, options, "dry-run"); err != nil {
				return err
			}
		}
		fmt.Fprintln(app.Stdout, "No container was started.")
		return nil
	}
	suiteID := time.Now().UTC().Format("20060102T150405Z") + "-" + benchmark.Identifier()
	suitePath := benchmark.DefaultPath(filepath.Join((storage.Layout{Root: dataRoot}).ComfyBenchmarks(), "suites"), ".json")
	entries := []any{}
	if *resume != "" {
		suitePath, err = filepath.Abs(*resume)
		if err != nil {
			return err
		}
		previous, err := benchmark.ReadObject(suitePath)
		if err != nil {
			return err
		}
		if previous["schema"] != benchmark.ComfySuiteSchema || previous["signature"] != signature {
			return controlerr.New("suite checkpoint is not compatible with this selection and configuration")
		}
		suiteID, _ = previous["suite_id"].(string)
		entries, _ = previous["entries"].([]any)
	}
	done := map[string]bool{}
	for _, raw := range entries {
		if entry, ok := raw.(map[string]any); ok && entry["status"] == "pass" {
			done[fmt.Sprint(entry["bundle"])] = true
		}
	}
	suite := map[string]any{"schema": benchmark.ComfySuiteSchema, "suite_id": suiteID, "signature": signature, "status": "running", "created_at": benchmark.Timestamp(), "configuration": comfyConfiguration(options), "entries": entries}
	if err := benchmark.WriteCheckpoint(suitePath, suite); err != nil {
		return err
	}
	failed := false
	for _, bundle := range bundles {
		if done[bundle.ID] {
			continue
		}
		path, summary, runErr := app.executeComfyBenchmark(managed, bundle, options, suiteID)
		entry := map[string]any{"bundle": bundle.ID, "result": path, "status": "pass", "cold_seconds": summary.cold, "warm_mean_seconds": summary.warmMean}
		if runErr != nil {
			entry["status"], entry["error"] = "fail", runErr.Error()
			failed = true
		}
		entries = append(entries, entry)
		suite["entries"] = entries
		if err := benchmark.WriteCheckpoint(suitePath, suite); err != nil {
			return err
		}
		if runErr != nil && !*keepGoing {
			suite["status"] = "failed"
			_ = benchmark.WriteCheckpoint(suitePath, suite)
			return runErr
		}
	}
	suite["status"], suite["finished_at"] = "complete", benchmark.Timestamp()
	if failed {
		suite["status"] = "completed-with-failures"
	}
	if err := benchmark.WriteCheckpoint(suitePath, suite); err != nil {
		return err
	}
	if *reportFormat != "none" {
		if _, err := benchmark.WriteSuiteReports(suitePath, suite, *reportFormat, ""); err != nil {
			return err
		}
	}
	fmt.Fprintf(app.Stdout, "Benchmark suite complete: %s\n", suitePath)
	if failed {
		return controlerr.New("benchmark suite completed with failures")
	}
	return nil
}

type comfySummary struct{ cold, warmMean any }

func (app *App) executeComfyBenchmark(managed catalog.Catalog, bundle catalog.Bundle, options comfyBenchmarkOptions, runID string) (path string, summary comfySummary, returned error) {
	if _, err := content.RequireBundle(managed, bundle, options.dataRoot); err != nil {
		if !options.dryRun {
			return "", summary, controlerr.New("bundle %q is not verified and ready: %v", bundle.ID, err)
		}
		fmt.Fprintf(app.Stderr, "WARNING: bundle %s is not ready: %v\n", bundle.ID, err)
	}
	spec := managed.Benchmarks[bundle.ID]
	source, err := benchmark.LoadPrompt(app.Root, spec)
	if err != nil {
		return "", summary, err
	}
	if options.transform != nil {
		if err := options.transform(source); err != nil {
			return "", summary, err
		}
	}
	if runID == "" {
		runID = time.Now().UTC().Format("20060102T150405Z") + "-" + benchmark.Identifier()
	}
	prefix := "rocmplete-benchmarks/" + runID + "/" + bundle.ID
	_, needsInput, err := benchmark.PreparePrompt(source, int64(options.seed), prefix)
	if err != nil {
		return "", summary, err
	}
	cacheRoot := ""
	environment := []string{}
	if options.cacheMode == "isolated" {
		cacheRoot = filepath.Join((storage.Layout{Root: options.dataRoot}).ComfyBenchmarks(), ".cache", runID+"-"+bundle.ID)
		containerRoot := "/data/benchmarks/.cache/" + runID + "-" + bundle.ID
		environment = []string{"HOME=" + containerRoot + "/home", "XDG_CACHE_HOME=" + containerRoot + "/xdg", "HF_HOME=" + containerRoot + "/huggingface", "TORCH_HOME=" + containerRoot + "/torch", "TRITON_CACHE_DIR=" + containerRoot + "/triton"}
	}
	command := runtime.WebCommand(runtime.WebOptions{Image: options.image, Profile: options.profile, Listen: "127.0.0.1", Port: options.port, DataDir: options.dataRoot, RenderNodes: []string{options.renderNode}, Detach: true, Unconfined: options.unconfined, DisableBundledExtensions: true, Arguments: []string{"--disable-all-custom-nodes"}, ContainerName: "rocmplete-comfyui-benchmark", Application: "comfyui", MemoryPolicy: options.memoryPolicy, KernelPolicy: options.kernelPolicy, Environment: environment, Publish: true, ContainerRole: "benchmark"}, app.podman().SELinuxVolumeSuffix(app.Context))
	path = filepath.Join((storage.Layout{Root: options.dataRoot}).ComfyBenchmarks(), runID+"-"+bundle.ID+".json")
	if options.dryRun {
		fmt.Fprintf(app.Stdout, "Benchmark source SHA-256: %s\nRuns: %d (cold + %d warm)\nCache mode: %s\nSynthetic input: %t\nResolved command:\n  %s\n", spec.SHA256, options.runs, options.runs-1, options.cacheMode, needsInput, shellJoin(command))
		return path, summary, nil
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return "", summary, err
	}
	present, err := app.podman().Exists(app.Context, "image", options.image)
	if err != nil || !present {
		if err != nil {
			return "", summary, err
		}
		return "", summary, controlerr.New("image not found: %s", options.image)
	}
	for _, name := range []string{"rocmplete-comfyui-benchmark", "rocmplete-comfyui"} {
		exists, err := app.podman().Exists(app.Context, "container", name)
		if err != nil {
			return "", summary, err
		}
		if exists {
			return "", summary, controlerr.New("container %q must be stopped before benchmarking", name)
		}
	}
	if err := benchmark.PortAvailable(options.port); err != nil {
		return "", summary, err
	}
	if err := (storage.Layout{Root: options.dataRoot}).PrepareRuntime("comfyui"); err != nil {
		return "", summary, err
	}
	if cacheRoot != "" {
		if _, err := os.Lstat(cacheRoot); err == nil {
			return "", summary, controlerr.New("isolated benchmark cache already exists: %s", cacheRoot)
		} else if !os.IsNotExist(err) {
			return "", summary, err
		}
		if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
			return "", summary, err
		}
		defer os.RemoveAll(cacheRoot)
	}
	inputMetadata := any(nil)
	if needsInput {
		inputPath, digest, err := benchmark.EnsureSyntheticInput(options.dataRoot)
		if err != nil {
			return "", summary, err
		}
		inputMetadata = map[string]any{"path": inputPath, "sha256": digest, "width": 768, "height": 768}
	}
	imageID, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", options.image}, "cannot inspect benchmark image")
	if err != nil {
		return "", summary, err
	}
	startedAt := benchmark.Timestamp()
	cleanup := func() {
		_, _ = app.run([]string{"podman", "rm", "--force", "--time", "2", "--ignore", "rocmplete-comfyui-benchmark"}, true)
	}
	defer cleanup()
	if _, err := app.run(command, true); err != nil {
		return "", summary, err
	}
	stats, err := benchmark.WaitForServer(app.Context, fmt.Sprintf("http://127.0.0.1:%d", options.port))
	if err != nil {
		return "", summary, err
	}
	runs := []any{}
	warmTotal, warmCount := 0.0, 0
	for index := 0; index < options.runs; index++ {
		prompt, _, err := benchmark.PreparePrompt(source, int64(options.seed+index), prefix)
		if err != nil {
			return "", summary, err
		}
		started := time.Now()
		promptID, err := benchmark.QueuePrompt(app.Context, fmt.Sprintf("http://127.0.0.1:%d", options.port), prompt)
		if err != nil {
			return "", summary, err
		}
		history, err := benchmark.WaitForPrompt(app.Context, fmt.Sprintf("http://127.0.0.1:%d", options.port), promptID)
		if err != nil {
			return "", summary, err
		}
		seconds := time.Since(started).Seconds()
		kind := "warm"
		if index == 0 {
			kind, summary.cold = "cold", seconds
		} else {
			warmTotal, warmCount = warmTotal+seconds, warmCount+1
		}
		runs = append(runs, map[string]any{"index": index, "kind": kind, "seed": options.seed + index, "prompt_id": promptID, "wall_seconds": seconds, "outputs": history["outputs"]})
	}
	if warmCount > 0 {
		summary.warmMean = warmTotal / float64(warmCount)
	}
	artifacts := []any{}
	for _, artifactID := range bundle.Artifacts {
		artifact := managed.Artifacts[artifactID]
		artifacts = append(artifacts, map[string]any{"identifier": artifact.ID, "sha256": artifact.SHA256, "size": artifact.Size})
	}
	value := map[string]any{"schema": benchmark.ComfyResultSchema, "run_id": runID, "bundle": bundle.ID, "status": "complete", "started_at": startedAt, "finished_at": benchmark.Timestamp(), "profile": options.profile, "render_node": options.renderNode, "memory_policy": options.memoryPolicy, "kernel_policy": options.kernelPolicy, "cache_mode": options.cacheMode, "unconfined": options.unconfined, "image": map[string]any{"reference": options.image, "id": imageID}, "workflow": map[string]any{"benchmark_source_sha256": spec.SHA256, "benchmark_renderer": spec.Renderer, "benchmark_rendered_sha256": spec.RenderedSHA256}, "artifacts": artifacts, "synthetic_input": inputMetadata, "system": stats, "runs": runs, "output_directory": filepath.Join((storage.Layout{Root: options.dataRoot}).Application("comfyui"), "output", "rocmplete-benchmarks", runID)}
	if err := benchmark.WriteJSON(path, value); err != nil {
		return "", summary, err
	}
	return path, summary, nil
}

func requireBenchmarkAgreements(managed catalog.Catalog, bundles []catalog.Bundle, accepted, dryRun bool) error {
	agreements := uniqueAgreements(managed, bundles)
	if len(agreements) > 0 && !accepted && !dryRun {
		names := make([]string, 0, len(agreements))
		for _, agreement := range agreements {
			names = append(names, agreement.Name)
		}
		return controlerr.New("benchmark content is governed by %s; review the catalog URLs and repeat with --accept-license", strings.Join(names, ", "))
	}
	return nil
}

func comfyConfiguration(options comfyBenchmarkOptions) map[string]any {
	return map[string]any{"image": options.image, "profile": options.profile, "render_node": options.renderNode, "port": options.port, "runs": options.runs, "seed": options.seed, "memory_policy": options.memoryPolicy, "kernel_policy": options.kernelPolicy, "cache_mode": options.cacheMode, "unconfined": options.unconfined}
}

func comfySuiteSignature(managed catalog.Catalog, bundles []catalog.Bundle, options comfyBenchmarkOptions) (string, error) {
	ids := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		spec := managed.Benchmarks[bundle.ID]
		ids = append(ids, strings.Join([]string{bundle.ID, spec.SHA256, spec.Renderer, spec.RenderedSHA256}, ":"))
	}
	sort.Strings(ids)
	encoded, err := json.Marshal(map[string]any{"bundles": ids, "configuration": comfyConfiguration(options)})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
