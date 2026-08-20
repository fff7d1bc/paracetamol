package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"rocmplete/internal/benchmark"
	"rocmplete/internal/catalog"
	"rocmplete/internal/config"
	"rocmplete/internal/content"
	"rocmplete/internal/controlerr"
	"rocmplete/internal/identity"
	"rocmplete/internal/platform"
	"rocmplete/internal/podman"
	"rocmplete/internal/runtime"
	"rocmplete/internal/storage"
)

type comfyBenchmarkOptions struct {
	profile, dataRoot, image, imageID, renderNode string
	port, runs, seed                              int
	unconfined, dryRun                            bool
	memoryPolicy, kernelPolicy                    string
	cacheMode                                     string
	acceptLicense                                 bool
	transform                                     func(map[string]any) error
}

var comfyBenchmarkContainer = identity.Container("comfyui-benchmark")

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
	memory := set.String("memory-policy", "balanced", "balanced or conservative")
	kernel := set.String("kernel-policy", "default", "default or experimental")
	cache := set.String("cache-mode", "persistent", "persistent or isolated")
	if err := parseFlags(set, args); err != nil {
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
	options, extras, err := app.comfyBenchmarkFlags("benchmark comfyui run", usage("benchmark", "comfyui", "run", "BUNDLE", "[OPTIONS]"), remaining)
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
	set := app.flags("benchmark comfyui suite", usage("benchmark", "comfyui", "suite", "[OPTIONS]"))
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
	memory := set.String("memory-policy", "balanced", "balanced or conservative")
	kernel := set.String("kernel-policy", "default", "default or experimental")
	cache := set.String("cache-mode", "persistent", "persistent or isolated")
	resume := set.String("resume", "", "resume a compatible Go suite JSON")
	keepGoing := set.Bool("keep-going", false, "continue after individual failure")
	reportFormat := set.String("report-format", "both", "markdown, html, both, or none")
	if err := parseFlags(set, args); err != nil {
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
	if !options.dryRun {
		if err := app.podman().RequireRootless(app.Context); err != nil {
			return err
		}
		present, err := app.podman().Exists(app.Context, "image", options.image)
		if err != nil {
			return err
		}
		if !present {
			return controlerr.New("image not found: %s", options.image)
		}
		options.imageID, err = app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", options.image}, "cannot inspect benchmark image")
		if err != nil {
			return err
		}
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
	createdAt := benchmark.Timestamp()
	entries := []benchmark.ComfySuiteEntry{}
	if *resume != "" {
		suitePath, err = filepath.Abs(*resume)
		if err != nil {
			return err
		}
		var previous benchmark.ComfySuite
		if err := benchmark.ReadJSON(suitePath, &previous, true); err != nil {
			return err
		}
		if previous.Schema != benchmark.ComfySuiteSchema || previous.Signature != signature {
			return controlerr.New("suite checkpoint is not compatible with this selection and configuration")
		}
		if err := validateComfySuiteResume(previous, managed, bundles, dataRoot); err != nil {
			return err
		}
		suiteID, createdAt, entries = previous.SuiteID, previous.CreatedAt, previous.Entries
	}
	done := map[string]bool{}
	for _, entry := range entries {
		if entry.Status == "pass" {
			done[entry.Bundle] = true
		}
	}
	suite := benchmark.ComfySuite{Schema: benchmark.ComfySuiteSchema, SuiteID: suiteID, Signature: signature, Status: "running", CreatedAt: createdAt, Configuration: comfyConfiguration(options), Entries: entries}
	writeInitial := benchmark.WriteNewCheckpoint
	if *resume != "" {
		writeInitial = benchmark.WriteCheckpoint
	}
	if err := writeInitial(suitePath, suite); err != nil {
		return err
	}
	failed := false
	for _, bundle := range bundles {
		if done[bundle.ID] {
			continue
		}
		path, summary, runErr := app.executeComfyBenchmark(managed, bundle, options, suiteID)
		entry := benchmark.ComfySuiteEntry{Bundle: bundle.ID, Result: path, Status: "pass", ColdSeconds: summary.cold, WarmMeanSeconds: summary.warmMean}
		if runErr != nil {
			entry.Status, entry.Result, entry.Error = "fail", "", runErr.Error()
			failed = true
		}
		entries = upsertComfySuiteEntry(entries, entry)
		suite.Entries = entries
		if err := benchmark.WriteCheckpoint(suitePath, suite); err != nil {
			return err
		}
		if runErr != nil && !*keepGoing {
			suite.Status = "failed"
			return checkpointThenReturn(suitePath, suite, runErr)
		}
	}
	suite.Status, suite.FinishedAt = "complete", benchmark.Timestamp()
	if failed {
		suite.Status = "completed-with-failures"
	}
	if err := benchmark.WriteCheckpoint(suitePath, suite); err != nil {
		return err
	}
	if *reportFormat != "none" {
		if _, err := benchmark.WriteSuiteReports(suitePath, suite, *reportFormat, "", *resume != ""); err != nil {
			return err
		}
	}
	fmt.Fprintf(app.Stdout, "Benchmark suite complete: %s\n", suitePath)
	if failed {
		return controlerr.New("benchmark suite completed with failures")
	}
	return nil
}

func validateComfySuiteResume(suite benchmark.ComfySuite, managed catalog.Catalog, bundles []catalog.Bundle, dataRoot string) error {
	if !managedRunID.MatchString(suite.SuiteID) || suite.CreatedAt == "" || !map[string]bool{"running": true, "failed": true, "complete": true, "completed-with-failures": true}[suite.Status] {
		return controlerr.New("ComfyUI suite checkpoint has invalid root metadata")
	}
	selected := make(map[string]bool, len(bundles))
	for _, bundle := range bundles {
		selected[bundle.ID] = true
	}
	seen := make(map[string]bool)
	resultsRoot := (storage.Layout{Root: dataRoot}).ComfyBenchmarks()
	for index, entry := range suite.Entries {
		if !selected[entry.Bundle] || seen[entry.Bundle] || entry.Status != "pass" && entry.Status != "fail" {
			return controlerr.New("ComfyUI suite checkpoint entry %d has invalid metadata", index+1)
		}
		seen[entry.Bundle] = true
		if entry.Status != "pass" {
			if entry.Error == "" || entry.Result != "" {
				return controlerr.New("failed ComfyUI suite entry %s has invalid evidence", entry.Bundle)
			}
			continue
		}
		if entry.Result == "" || entry.Error != "" {
			return controlerr.New("ComfyUI suite checkpoint has duplicate or empty completed evidence for %s", entry.Bundle)
		}
		if err := storage.ValidateManagedParent(entry.Result, resultsRoot, dataRoot, "ComfyUI benchmark result"); err != nil {
			return err
		}
		status, err := os.Lstat(entry.Result)
		if err != nil || !status.Mode().IsRegular() {
			return controlerr.New("completed ComfyUI suite result is missing or unexpected: %s", entry.Result)
		}
		var result benchmark.ComfyResult
		if err := benchmark.ReadJSON(entry.Result, &result, true); err != nil || result.Schema != benchmark.ComfyResultSchema || result.Status != "complete" || result.Bundle != entry.Bundle || !comfyResultMatches(result, suite.Configuration, managed, managed.Bundles[entry.Bundle]) {
			return controlerr.New("completed ComfyUI suite result is incompatible: %s", entry.Result)
		}
	}
	return nil
}

func upsertComfySuiteEntry(entries []benchmark.ComfySuiteEntry, replacement benchmark.ComfySuiteEntry) []benchmark.ComfySuiteEntry {
	for index := range entries {
		if entries[index].Bundle == replacement.Bundle {
			entries[index] = replacement
			return entries
		}
	}
	return append(entries, replacement)
}

type comfySummary struct{ cold, warmMean float64 }

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
	prefix := identity.StateNamespace + "-benchmarks/" + runID + "/" + bundle.ID
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
	command := runtime.WebCommand(runtime.WebOptions{Image: options.image, Profile: options.profile, Listen: "127.0.0.1", Port: options.port, DataDir: options.dataRoot, RenderNodes: []string{options.renderNode}, Detach: true, Unconfined: options.unconfined, DisableBundledExtensions: true, Arguments: []string{"--disable-all-custom-nodes"}, ContainerName: comfyBenchmarkContainer, Application: "comfyui", MemoryPolicy: options.memoryPolicy, KernelPolicy: options.kernelPolicy, Environment: environment, Publish: true, ContainerRole: "benchmark"}, app.podman().SELinuxVolumeSuffix(app.Context))
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
	application, _ := config.ApplicationByID("comfyui")
	for _, name := range []string{comfyBenchmarkContainer, application.ContainerName} {
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
		defer func() {
			returned = withCleanupFailure(returned, "remove isolated benchmark cache", os.RemoveAll(cacheRoot))
		}()
	}
	var inputMetadata *benchmark.ComfySyntheticInput
	if needsInput {
		inputPath, digest, err := benchmark.EnsureSyntheticInput(options.dataRoot)
		if err != nil {
			return "", summary, err
		}
		inputMetadata = &benchmark.ComfySyntheticInput{Path: inputPath, SHA256: digest, Width: 768, Height: 768}
	}
	imageID, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", options.image}, "cannot inspect benchmark image")
	if err != nil {
		return "", summary, err
	}
	if options.imageID != "" && imageID != options.imageID {
		return "", summary, controlerr.New("benchmark image changed after suite planning: %s", options.image)
	}
	startedAt := benchmark.Timestamp()
	defer func() {
		returned = withCleanupFailure(returned, "clean up ComfyUI benchmark container", app.podman().RemoveContainer(contextWithoutCancel(), comfyBenchmarkContainer, 2, podman.Streams{}))
	}()
	if _, err := app.run(command, true); err != nil {
		return "", summary, err
	}
	stats, err := benchmark.WaitForServer(app.Context, fmt.Sprintf("http://127.0.0.1:%d", options.port))
	if err != nil {
		return "", summary, err
	}
	runs := []benchmark.ComfyRun{}
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
		outputs, marshalErr := json.Marshal(history["outputs"])
		if marshalErr != nil {
			return "", summary, marshalErr
		}
		runs = append(runs, benchmark.ComfyRun{Index: index, Kind: kind, Seed: options.seed + index, PromptID: promptID, WallSeconds: seconds, Outputs: outputs})
	}
	if warmCount > 0 {
		summary.warmMean = warmTotal / float64(warmCount)
	}
	artifacts := comfyArtifactEvidence(managed, bundle)
	value := benchmark.ComfyResult{
		Schema: benchmark.ComfyResultSchema, RunID: runID, Bundle: bundle.ID, Status: "complete",
		StartedAt: startedAt, FinishedAt: benchmark.Timestamp(), Profile: options.profile, RenderNode: options.renderNode,
		MemoryPolicy: options.memoryPolicy, KernelPolicy: options.kernelPolicy, CacheMode: options.cacheMode, Unconfined: options.unconfined,
		Image:     benchmark.ImageIdentity{Reference: options.image, ID: imageID},
		Workflow:  benchmark.ComfyWorkflowEvidence{SourceSHA256: spec.SHA256, Renderer: spec.Renderer, RenderedSHA256: spec.RenderedSHA256},
		Artifacts: artifacts, SyntheticInput: inputMetadata, System: stats, Runs: runs,
		OutputDirectory: filepath.Join((storage.Layout{Root: options.dataRoot}).Application("comfyui"), "output", identity.StateNamespace+"-benchmarks", runID),
	}
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

func comfyConfiguration(options comfyBenchmarkOptions) benchmark.ComfyConfiguration {
	return benchmark.ComfyConfiguration{Image: options.image, ImageID: options.imageID, Profile: options.profile, RenderNode: options.renderNode, Port: options.port, Runs: options.runs, Seed: options.seed, MemoryPolicy: options.memoryPolicy, KernelPolicy: options.kernelPolicy, CacheMode: options.cacheMode, Unconfined: options.unconfined}
}

func comfySuiteSignature(managed catalog.Catalog, bundles []catalog.Bundle, options comfyBenchmarkOptions) (string, error) {
	type bundleIdentity struct {
		ID        string                            `json:"id"`
		Workflow  benchmark.ComfyWorkflowEvidence   `json:"workflow"`
		Artifacts []benchmark.ComfyArtifactEvidence `json:"artifacts"`
	}
	identities := make([]bundleIdentity, 0, len(bundles))
	for _, bundle := range bundles {
		spec := managed.Benchmarks[bundle.ID]
		identities = append(identities, bundleIdentity{
			ID: bundle.ID, Workflow: benchmark.ComfyWorkflowEvidence{SourceSHA256: spec.SHA256, Renderer: spec.Renderer, RenderedSHA256: spec.RenderedSHA256},
			Artifacts: comfyArtifactEvidence(managed, bundle),
		})
	}
	sort.Slice(identities, func(i, j int) bool { return identities[i].ID < identities[j].ID })
	encoded, err := json.Marshal(struct {
		Bundles       []bundleIdentity             `json:"bundles"`
		Configuration benchmark.ComfyConfiguration `json:"configuration"`
	}{Bundles: identities, Configuration: comfyConfiguration(options)})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func comfyArtifactEvidence(managed catalog.Catalog, bundle catalog.Bundle) []benchmark.ComfyArtifactEvidence {
	result := make([]benchmark.ComfyArtifactEvidence, 0, len(bundle.Artifacts))
	for _, artifactID := range bundle.Artifacts {
		artifact := managed.Artifacts[artifactID]
		result = append(result, benchmark.ComfyArtifactEvidence{Identifier: artifact.ID, SHA256: artifact.SHA256, Size: artifact.Size})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Identifier < result[j].Identifier })
	return result
}

func comfyResultMatches(result benchmark.ComfyResult, configuration benchmark.ComfyConfiguration, managed catalog.Catalog, bundle catalog.Bundle) bool {
	if result.Image.Reference != configuration.Image || result.Image.ID != configuration.ImageID || result.Profile != configuration.Profile || result.RenderNode != configuration.RenderNode ||
		result.MemoryPolicy != configuration.MemoryPolicy || result.KernelPolicy != configuration.KernelPolicy || result.CacheMode != configuration.CacheMode || result.Unconfined != configuration.Unconfined ||
		result.StartedAt == "" || result.FinishedAt == "" || len(result.Runs) != configuration.Runs {
		return false
	}
	spec := managed.Benchmarks[bundle.ID]
	if result.Workflow != (benchmark.ComfyWorkflowEvidence{SourceSHA256: spec.SHA256, Renderer: spec.Renderer, RenderedSHA256: spec.RenderedSHA256}) || !reflect.DeepEqual(result.Artifacts, comfyArtifactEvidence(managed, bundle)) {
		return false
	}
	for index, run := range result.Runs {
		kind := "warm"
		if index == 0 {
			kind = "cold"
		}
		if run.Index != index || run.Kind != kind || run.Seed != configuration.Seed+index || run.PromptID == "" || run.WallSeconds <= 0 || !json.Valid(run.Outputs) {
			return false
		}
	}
	return true
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
