package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"paracetamol/internal/agent"
	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
	"paracetamol/internal/platform"
	"paracetamol/internal/podman"
	"paracetamol/internal/runtime"
	"paracetamol/internal/storage"
)

type intList []int

type speculativeThinking struct {
	Client string `json:"client"`
	Native string `json:"native"`
}

type speculativeProfileSetting struct {
	Profile string `json:"profile"`
	Value   string `json:"value"`
}

type speculativeDefinition struct {
	SourceIdentity       string                      `json:"source_identity"`
	Image                benchmark.ImageIdentity     `json:"image"`
	Model                benchmark.ModelIdentity     `json:"model"`
	DraftModel           *benchmark.ModelIdentity    `json:"draft_model,omitempty"`
	Profile              string                      `json:"profile"`
	Backend              string                      `json:"backend"`
	RenderNodes          []string                    `json:"render_nodes"`
	Preset               string                      `json:"preset"`
	SpeculativeType      string                      `json:"speculative_type"`
	IncumbentDepth       int64                       `json:"incumbent_depth"`
	Depths               []int                       `json:"depths"`
	ContextDepths        []int                       `json:"context_depths"`
	Repetitions          int                         `json:"repetitions"`
	GenerationTokens     int                         `json:"generation_tokens"`
	Seed                 int                         `json:"seed"`
	ServerContext        int                         `json:"server_context"`
	Thinking             speculativeThinking         `json:"thinking"`
	Jinja                bool                        `json:"jinja"`
	ReasoningPreserve    bool                        `json:"reasoning_preserve"`
	ReasoningControl     string                      `json:"reasoning_control"`
	ChatTemplate         string                      `json:"chat_template"`
	SamplingPolicy       string                      `json:"sampling_policy"`
	SamplingDefaults     json.RawMessage             `json:"sampling_defaults,omitempty"`
	FlashPolicy          []speculativeProfileSetting `json:"flash_policy"`
	KVCachePolicy        []speculativeProfileSetting `json:"kv_cache_policy"`
	DraftProbabilityMin  float64                     `json:"draft_probability_min"`
	DraftBackendSampling string                      `json:"draft_backend_sampling"`
	NgramSimple          bool                        `json:"ngram_simple"`
	GraphOptimization    bool                        `json:"graph_optimization"`
	DisableGraphs        bool                        `json:"disable_graphs"`
	Poll                 int                         `json:"poll"`
	NoHost               bool                        `json:"no_host"`
	FlashAttention       string                      `json:"flash_attention"`
	CacheTypeK           string                      `json:"cache_type_k"`
	CacheTypeV           string                      `json:"cache_type_v"`
	BatchSize            int                         `json:"batch_size"`
	UBatchSize           int                         `json:"ubatch_size"`
}

type speculativeResult struct {
	Schema      string                             `json:"schema"`
	SuiteID     string                             `json:"suite_id"`
	Fingerprint string                             `json:"fingerprint"`
	Definition  speculativeDefinition              `json:"definition"`
	Status      string                             `json:"status"`
	StartedAt   string                             `json:"started_at"`
	FinishedAt  string                             `json:"finished_at,omitempty"`
	Trials      []benchmark.SpeculativeTrial       `json:"trials"`
	Summary     benchmark.SpeculativeSummaryResult `json:"summary"`
}

var speculativeBenchmarkContainer = identity.Container("llama-cpp-speculative-benchmark")

func (values *intList) String() string {
	parts := make([]string, len(*values))
	for index, value := range *values {
		parts[index] = fmt.Sprint(value)
	}
	return strings.Join(parts, ",")
}
func (values *intList) Set(value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	*values = append(*values, parsed)
	return nil
}

func (app *App) benchmarkSpeculative(args []string) error {
	set := app.flags("benchmark llama-cpp speculative", usage("benchmark", "llama-cpp", "speculative", "--preset PRESET", "[OPTIONS]"))
	presetID := set.String("preset", "", "installed speculative preset")
	var depths, contexts intList
	set.Var(&depths, "draft-depth", "draft depth; repeatable")
	set.Var(&contexts, "context-depth", "target prompt depth; repeatable")
	repetitions := set.Int("repetitions", 3, "fresh repetitions per condition")
	generation := set.Int("generation-tokens", 512, "maximum generated tokens")
	seed := set.Int("seed", 42, "first request seed")
	contextSize := set.Int("context", 131072, "server context")
	thinking := set.String("thinking", "", "off, minimal, low, medium, high, xhigh, or max")
	draftProbability := set.Float64("draft-probability-min", 0, "minimum draft probability")
	draftSampling := set.String("draft-backend-sampling", "on", "on or off")
	ngram := set.Bool("ngram-simple", false, "try draftless n-gram matching first")
	graphOptimization := set.Bool("graph-optimization", false, "enable graph optimizer")
	disableGraphs := set.Bool("disable-graphs", false, "disable graph capture")
	poll := set.Int("poll", -1, "worker polling level 0 through 100")
	noHost := set.Bool("no-host", false, "bypass host buffers")
	flash := set.String("flash-attn", "preset", "preset, on, off, or auto")
	cacheK := set.String("cache-type-k", "preset", "preset, f16, q8_0, or q4_0")
	cacheV := set.String("cache-type-v", "preset", "preset, f16, q8_0, or q4_0")
	batch := set.Int("batch-size", 2048, "logical batch size")
	ubatch := set.Int("ubatch-size", 512, "physical microbatch size")
	profileFlag := set.String("profile", "auto", "GPU execution profile")
	backend := set.String("backend", "rocm", "rocm or vulkan")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact GPU render node; repeatable")
	portText := set.String("port", "8190", "private benchmark server port")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override llama.cpp image")
	output := set.String("output", "", "new checkpoint JSON")
	resume := set.String("resume", "", "resume a compatible Go checkpoint")
	keepGoing := set.Bool("keep-going", false, "continue after failed trial")
	unconfined := set.Bool("unconfined", false, "disable seccomp")
	dryRun := set.Bool("dry-run", false, "print the complete sweep")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("speculative benchmark accepts no positional arguments")
	}
	if *presetID == "" {
		return controlerr.Usage("--preset is required")
	}
	if *output != "" && *resume != "" {
		return controlerr.Usage("--output and --resume are mutually exclusive")
	}
	if *dryRun && *resume != "" {
		return controlerr.Usage("--resume cannot be combined with --dry-run")
	}
	var err error
	var result speculativeResult
	resultPath := ""
	if *resume != "" {
		resultPath, err = filepath.Abs(*resume)
		if err != nil {
			return err
		}
		if err := benchmark.ReadJSON(resultPath, &result, true); err != nil {
			return err
		}
		if err := validateSpeculativeEnvelope(result); err != nil {
			return err
		}
	} else if *output != "" {
		resultPath, err = absoluteNewPath(*output)
		if err != nil {
			return err
		}
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	preset, ok := managed.LlamaPresets[*presetID]
	if !ok {
		return controlerr.Usage("unknown llama.cpp preset %q", *presetID)
	}
	if preset.SpeculativeType == "" {
		return controlerr.Usage("preset %q does not enable speculative decoding", *presetID)
	}
	if !preset.AgentTools {
		return controlerr.Usage("preset %q has no reviewed Chat Completions policy", *presetID)
	}
	maximumDepth := 15
	if preset.SpeculativeType == "draft-mtp" {
		maximumDepth = 8
	}
	if len(depths) == 0 {
		for depth := 1; depth <= maximumDepth; depth++ {
			depths = append(depths, depth)
		}
	}
	if err := validateUniqueRange(depths, 1, maximumDepth, "--draft-depth"); err != nil {
		return err
	}
	if len(contexts) == 0 {
		contexts = intList{4096, 32768, 65536, 122880}
	}
	if err := validateUniqueRange(contexts, 1, math.MaxInt, "--context-depth"); err != nil {
		return err
	}
	sort.Ints(depths)
	sort.Ints(contexts)
	if *repetitions < 1 || *generation < 1 || *batch < 1 || *ubatch < 1 || *ubatch > *batch {
		return controlerr.Usage("repetitions, generation, and batch sizes must be positive; ubatch must not exceed batch")
	}
	if *seed < 0 || int64(*seed)+int64(*repetitions)-1 > math.MaxInt32 {
		return controlerr.Usage("seed range must fit a signed 32-bit integer")
	}
	if *draftProbability < 0 || *draftProbability > 1 {
		return controlerr.Usage("--draft-probability-min must be between 0 and 1")
	}
	if *poll < -1 || *poll > 100 {
		return controlerr.Usage("--poll must be between 0 and 100")
	}
	if *contextSize < 1 || int64(*contextSize) > preset.DefaultContext || contexts[len(contexts)-1]+*generation+1024 > *contextSize {
		return controlerr.Usage("server context must fit the largest prompt, generation allowance, and 1024-token margin within the preset limit")
	}
	for value, choices := range map[string][]string{*draftSampling: {"on", "off"}, *flash: {"preset", "on", "off", "auto"}, *cacheK: {"preset", "f16", "q8_0", "q4_0"}, *cacheV: {"preset", "f16", "q8_0", "q4_0"}, *backend: {"rocm", "vulkan"}} {
		if err := requireChoice(value, "speculative benchmark setting", choices...); err != nil {
			return err
		}
	}
	if (*cacheK == "preset") != (*cacheV == "preset") || *cacheK != "preset" && *cacheK != *cacheV {
		return controlerr.Usage("key and value caches must both use preset or the same explicit type")
	}
	if *cacheV != "preset" && *cacheV != "f16" && *flash != "on" {
		return controlerr.Usage("a quantized value cache requires --flash-attn on")
	}
	profile := *profileFlag
	if err := platform.ValidateProfile(profile); err != nil || profile == "cpu" {
		return controlerr.Usage("speculative sweeps require a valid GPU profile")
	}
	selectedNodes, err := app.resolveDevices(profile, nodes, nodes != nil)
	if err != nil {
		return err
	}
	port, err := config.ValidatePort(*portText)
	if err != nil {
		return err
	}
	level := *thinking
	if level == "" {
		level = agent.ReasoningDefault(preset)
	}
	if !containsString(agent.ReasoningLevels(preset), level) {
		return controlerr.Usage("thinking level %q is not supported by %s", level, *presetID)
	}
	nativeReasoning := level
	if level == "off" {
		nativeReasoning = "off"
	} else if preset.ReasoningControl == "toggle" {
		nativeReasoning = "on"
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, !*dryRun)
	if err != nil {
		return err
	}
	bundle := managed.Bundles[preset.Bundle]
	if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
		return controlerr.New("preset %q is not installed: %v", *presetID, err)
	}
	artifact := managed.Artifacts[preset.Artifact]
	modelIdentity := managedModelIdentity(dataRoot, *presetID, artifact)
	managedDraft := ""
	var draftIdentity *benchmark.ModelIdentity
	if preset.DraftArtifact != "" {
		draftArtifact := managed.Artifacts[preset.DraftArtifact]
		managedDraft = draftArtifact.Destination
		identity := managedModelIdentity(dataRoot, "", draftArtifact)
		draftIdentity = &identity
	}
	application, _ := config.ApplicationByID("llama-cpp")
	image := firstNonEmpty(*imageFlag, application.Image)
	flashPolicy := preset.FlashAttention
	if *flash != "preset" {
		flashPolicy = map[string]string{}
		for _, candidate := range platform.ProfileIDs() {
			flashPolicy[candidate] = *flash
		}
	}
	kvPolicy := preset.KVCache
	if *cacheK != "preset" {
		kvPolicy = map[string]string{}
		for _, candidate := range platform.ProfileIDs() {
			kvPolicy[candidate] = *cacheK
		}
	}
	samplingDefaults := map[string]any{}
	var samplingJSON json.RawMessage
	if preset.SamplingPolicy != "" {
		policy := managed.SamplingPolicies[preset.SamplingPolicy]
		samplingDefaults = map[string]any{"thinking": policy.Thinking, "non_thinking": policy.NonThinking}
		encoded, err := json.Marshal(samplingDefaults)
		if err != nil {
			return fmt.Errorf("encode sampling policy: %w", err)
		}
		samplingJSON = encoded
	}
	commands := map[int][]string{}
	for _, depth := range depths {
		extra := []string{"--parallel", "1", "--spec-draft-p-min", fmt.Sprint(*draftProbability), "--batch-size", fmt.Sprint(*batch), "--ubatch-size", fmt.Sprint(*ubatch)}
		if *draftSampling == "off" {
			extra = append(extra, "--no-spec-draft-backend-sampling")
		}
		if *ngram {
			extra = append(extra, "--spec-type", "ngram-simple")
		}
		if *poll >= 0 {
			extra = append(extra, "--poll", fmt.Sprint(*poll))
		}
		if *noHost {
			extra = append(extra, "--no-host")
		}
		environment := []string{}
		if *graphOptimization {
			environment = append(environment, "GGML_CUDA_GRAPH_OPT=1")
		}
		if *disableGraphs {
			environment = append(environment, "GGML_CUDA_DISABLE_GRAPHS=1")
		}
		command, err := runtime.LlamaCommand(runtime.LlamaOptions{Image: image, Profile: profile, Mode: "server", DataDir: dataRoot, Backend: *backend, ManagedModel: artifact.Destination, ManagedDraft: managedDraft, SpeculativeType: preset.SpeculativeType, DraftTokens: int64(depth), ContextOverrideArchitectures: preset.ContextOverrideArchitectures, Jinja: preset.Jinja, ReasoningPreserve: preset.ReasoningPreserve, ChatTemplate: preset.ChatTemplate, SamplingDefaults: samplingDefaults, ProfileFlashAttention: flashPolicy, ProfileKVCache: kvPolicy, RenderNodes: selectedNodes, Listen: "127.0.0.1", Port: port, Context: int64(*contextSize), Detach: true, Unconfined: *unconfined, ContainerName: speculativeBenchmarkContainer, ContainerRole: "benchmark", AutoRemove: false, Arguments: extra, Environment: environment}, app.podman().SELinuxVolumeSuffix(app.Context))
		if err != nil {
			return err
		}
		commands[depth] = command
	}
	requestCount := len(depths) * len(contexts) * *repetitions
	fmt.Fprintf(app.Stdout, "Model: %s\nParameters: %s, depths %s, targets %s, %d repetitions (%d requests)\nCondition: %s, seed %d, %d generated tokens, context %d, parallel 1\n", *presetID, preset.SpeculativeType, depths.String(), contexts.String(), *repetitions, requestCount, level, *seed, *generation, *contextSize)
	if *dryRun {
		for _, depth := range depths {
			fmt.Fprintf(app.Stdout, "\nDraft depth: %d\n  %s\n", depth, shellJoin(commands[depth]))
		}
		fmt.Fprintln(app.Stdout, "No container was started and no checkpoint was written.")
		return nil
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	present, err := app.podman().Exists(app.Context, "image", image)
	if err != nil || !present {
		if err != nil {
			return err
		}
		return controlerr.New("image not found: %s", image)
	}
	if err := benchmark.PortAvailable(port); err != nil {
		return err
	}
	imageID, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", image}, "cannot inspect benchmark image")
	if err != nil {
		return err
	}
	sourceIdentity, err := app.projectSourceIdentity()
	if err != nil {
		return err
	}
	definition := speculativeDefinition{
		SourceIdentity: sourceIdentity, Image: benchmark.ImageIdentity{Reference: image, ID: imageID},
		Model: modelIdentity, DraftModel: draftIdentity, Profile: profile, Backend: *backend,
		RenderNodes: selectedNodes, Preset: *presetID, SpeculativeType: preset.SpeculativeType,
		IncumbentDepth: preset.DraftTokensForBackend(*backend), Depths: []int(depths), ContextDepths: []int(contexts),
		Repetitions: *repetitions, GenerationTokens: *generation, Seed: *seed, ServerContext: *contextSize,
		Thinking: speculativeThinking{Client: level, Native: nativeReasoning}, Jinja: preset.Jinja,
		ReasoningPreserve: preset.ReasoningPreserve, ReasoningControl: preset.ReasoningControl,
		ChatTemplate: preset.ChatTemplate, SamplingPolicy: preset.SamplingPolicy, SamplingDefaults: samplingJSON,
		FlashPolicy: profileSettings(flashPolicy), KVCachePolicy: profileSettings(kvPolicy), DraftProbabilityMin: *draftProbability,
		DraftBackendSampling: *draftSampling, NgramSimple: *ngram, GraphOptimization: *graphOptimization,
		DisableGraphs: *disableGraphs, Poll: *poll, NoHost: *noHost, FlashAttention: *flash,
		CacheTypeK: *cacheK, CacheTypeV: *cacheV, BatchSize: *batch, UBatchSize: *ubatch,
	}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		return err
	}
	plannedTrials := makeSpeculativeTrials(depths, contexts, *repetitions, *seed)
	if *resume != "" {
		if result.Fingerprint != fingerprint {
			return controlerr.New("speculative benchmark checkpoint does not match this definition")
		}
		if err := validateSpeculativeResume(result, plannedTrials); err != nil {
			return err
		}
	}
	if err := (storage.Layout{Root: dataRoot}).PrepareRuntime("llama-cpp"); err != nil {
		return err
	}
	if *resume == "" {
		if resultPath == "" {
			resultPath = benchmark.DefaultPath((storage.Layout{Root: dataRoot}).LlamaBenchmarks(), "-speculative-depth-sweep.json")
		}
		result = speculativeResult{Schema: benchmark.SpeculativeSchema, SuiteID: time.Now().UTC().Format("20060102T150405Z") + "-" + benchmark.Identifier(), Fingerprint: fingerprint, Definition: definition, Status: "preparing", StartedAt: benchmark.Timestamp(), Trials: plannedTrials, Summary: benchmark.SpeculativeSummaryResult{Depths: map[string]benchmark.SpeculativeDepthSummary{}}}
		if err := benchmark.WriteNewCheckpoint(resultPath, result); err != nil {
			return err
		}
	}
	result.Summary = benchmark.SpeculativeSummary(result.Trials, int(preset.DraftTokensForBackend(*backend)))
	result.Status = "running"
	if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
		return err
	}
	failed := false
	for index := range result.Trials {
		trial := &result.Trials[index]
		if trial.Status == "complete" {
			continue
		}
		depth, contextDepth, requestSeed := trial.Depth, trial.ContextDepth, trial.Seed
		fmt.Fprintf(app.Stdout, "[%d/%d] depth %d, target %d, seed %d\n", index+1, len(result.Trials), depth, contextDepth, requestSeed)
		trial.Status, trial.StartedAt, trial.Error, trial.Metrics = "running", benchmark.Timestamp(), "", nil
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
		startup := time.Now()
		_, startErr := app.run(commands[depth], true)
		if startErr == nil {
			startErr = benchmark.WaitForHealth(app.Context, fmt.Sprintf("http://127.0.0.1:%d", port))
		}
		startupSeconds := time.Since(startup).Seconds()
		var metrics benchmark.SpeculativeMetrics
		if startErr == nil {
			payload := map[string]any{"model": *presetID, "messages": benchmark.SpeculativeMessages(contextDepth, requestSeed), "max_tokens": *generation, "seed": requestSeed, "stream": false}
			if nativeReasoning == "off" {
				payload["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
			} else if nativeReasoning != "on" {
				payload["reasoning_effort"] = nativeReasoning
			}
			started := time.Now()
			response, requestErr := benchmark.PostJSON(app.Context, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port), payload, 32*1024*1024)
			if requestErr != nil {
				startErr = requestErr
			} else {
				metrics, startErr = benchmark.ParseSpeculativeResponse(response, time.Since(started).Seconds(), startupSeconds)
			}
		}
		startErr = withCleanupFailure(startErr, "clean up speculative benchmark container", app.podman().RemoveContainer(contextWithoutCancel(), speculativeBenchmarkContainer, 10, podman.Streams{}))
		if startErr != nil {
			trial.Status, trial.Error, trial.FinishedAt = "failed", startErr.Error(), benchmark.Timestamp()
			failed = true
		} else {
			trial.Status, trial.FinishedAt, trial.Metrics = "complete", benchmark.Timestamp(), &metrics
			fmt.Fprintf(app.Stdout, "  %.2f t/s, accepted %d/%d (%.1f%%)\n", metrics.GenerationTokensPerSecond, metrics.AcceptedDraftTokens, metrics.DraftedTokens, metrics.AcceptancePercent)
		}
		result.Summary = benchmark.SpeculativeSummary(result.Trials, int(preset.DraftTokensForBackend(*backend)))
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
		if startErr != nil && !*keepGoing {
			result.Status = "failed"
			return checkpointThenReturn(resultPath, result, startErr)
		}
	}
	result.Status, result.FinishedAt = "complete", benchmark.Timestamp()
	if failed {
		result.Status = "failed"
	}
	if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
		return err
	}
	fmt.Fprintf(app.Stdout, "Speculative benchmark complete: %s\n", resultPath)
	if failed {
		return controlerr.New("speculative benchmark completed with failed trials")
	}
	return nil
}

func makeSpeculativeTrials(depths, contexts []int, repetitions, seed int) []benchmark.SpeculativeTrial {
	trials := make([]benchmark.SpeculativeTrial, 0, len(depths)*len(contexts)*repetitions)
	for repetition := 1; repetition <= repetitions; repetition++ {
		for _, contextDepth := range contexts {
			for _, depth := range depths {
				trials = append(trials, benchmark.SpeculativeTrial{Identifier: fmt.Sprintf("d%d-c%d-s%d-r%d", depth, contextDepth, seed+repetition-1, repetition), Depth: depth, ContextDepth: contextDepth, Seed: seed + repetition - 1, Repetition: repetition, Status: "pending"})
			}
		}
	}
	sort.Slice(trials, func(i, j int) bool {
		left := sha256.Sum256([]byte("schedule-v1:" + trials[i].Identifier))
		right := sha256.Sum256([]byte("schedule-v1:" + trials[j].Identifier))
		return strings.Compare(string(left[:]), string(right[:])) < 0
	})
	return trials
}

func validateSpeculativeResume(result speculativeResult, planned []benchmark.SpeculativeTrial) error {
	if err := validateSpeculativeEnvelope(result); err != nil {
		return err
	}
	if len(result.Trials) != len(planned) {
		return controlerr.New("speculative benchmark checkpoint has an unexpected trial count")
	}
	for index, trial := range result.Trials {
		expected := planned[index]
		if trial.Identifier != expected.Identifier || trial.Depth != expected.Depth || trial.ContextDepth != expected.ContextDepth || trial.Seed != expected.Seed || trial.Repetition != expected.Repetition {
			return controlerr.New("speculative benchmark trial %d has invalid metadata", index+1)
		}
	}
	return nil
}

func validateSpeculativeEnvelope(result speculativeResult) error {
	if result.Schema != benchmark.SpeculativeSchema || !managedRunID.MatchString(result.SuiteID) || result.StartedAt == "" {
		return controlerr.New("speculative benchmark checkpoint has invalid root metadata")
	}
	if !map[string]bool{"preparing": true, "running": true, "failed": true, "complete": true}[result.Status] {
		return controlerr.New("speculative benchmark checkpoint has invalid status %q", result.Status)
	}
	if (result.Status == "complete" || result.Status == "failed") && result.FinishedAt == "" {
		return controlerr.New("finished speculative benchmark checkpoint has no completion timestamp")
	}
	digest, err := jsonDigest(result.Definition)
	if err != nil || digest != result.Fingerprint {
		return controlerr.New("speculative benchmark definition does not match its fingerprint")
	}
	seen := map[string]bool{}
	for index, trial := range result.Trials {
		if trial.Identifier == "" || seen[trial.Identifier] {
			return controlerr.New("speculative benchmark trial %d has an invalid identifier", index+1)
		}
		seen[trial.Identifier] = true
		switch trial.Status {
		case "pending":
			if trial.StartedAt != "" || trial.FinishedAt != "" || trial.Error != "" || trial.Metrics != nil {
				return controlerr.New("pending speculative benchmark trial %q contains result data", trial.Identifier)
			}
		case "running":
			if trial.StartedAt == "" || trial.FinishedAt != "" || trial.Error != "" || trial.Metrics != nil {
				return controlerr.New("running speculative benchmark trial %q has inconsistent state", trial.Identifier)
			}
		case "complete":
			if trial.StartedAt == "" || trial.FinishedAt == "" || trial.Error != "" || trial.Metrics == nil || !validSpeculativeMetrics(*trial.Metrics) {
				return controlerr.New("completed speculative benchmark trial %q has invalid metrics", trial.Identifier)
			}
		case "failed":
			if trial.StartedAt == "" || trial.FinishedAt == "" || trial.Error == "" || trial.Metrics != nil {
				return controlerr.New("failed speculative benchmark trial %q has inconsistent state", trial.Identifier)
			}
		default:
			return controlerr.New("speculative benchmark trial %q has invalid status %q", trial.Identifier, trial.Status)
		}
	}
	return nil
}

func validSpeculativeMetrics(metrics benchmark.SpeculativeMetrics) bool {
	finite := func(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
	if !finite(metrics.RequestSeconds) || metrics.RequestSeconds <= 0 || !finite(metrics.StartupSeconds) || metrics.StartupSeconds < 0 ||
		metrics.PromptTokens < 0 || metrics.CompletionTokens < 0 || metrics.DraftedTokens < 0 || metrics.AcceptedDraftTokens < 0 || metrics.AcceptedDraftTokens > metrics.DraftedTokens ||
		!finite(metrics.GenerationTokensPerSecond) || metrics.GenerationTokensPerSecond <= 0 || !finite(metrics.PromptTokensPerSecond) || metrics.PromptTokensPerSecond <= 0 ||
		!finite(metrics.PredictedMS) || metrics.PredictedMS <= 0 || !finite(metrics.PromptMS) || metrics.PromptMS <= 0 || !finite(metrics.AcceptancePercent) || metrics.AcceptancePercent < 0 || metrics.AcceptancePercent > 100 ||
		!json.Valid(metrics.Message) {
		return false
	}
	decoded, err := hex.DecodeString(metrics.ResponseSHA256)
	return err == nil && len(decoded) == sha256.Size
}

func managedModelIdentity(dataRoot, preset string, artifact catalog.Artifact) benchmark.ModelIdentity {
	return benchmark.ModelIdentity{Kind: "catalog", Preset: preset, Path: content.ArtifactPath(dataRoot, artifact), Repository: artifact.Source.Repository, Revision: artifact.Source.Revision, SourcePath: artifact.Source.Path, Size: artifact.Size, SHA256: artifact.SHA256}
}

func profileSettings(values map[string]string) []speculativeProfileSetting {
	settings := make([]speculativeProfileSetting, 0, len(values))
	for _, profile := range sortedMapKeys(values) {
		settings = append(settings, speculativeProfileSetting{Profile: profile, Value: values[profile]})
	}
	return settings
}

func validateUniqueRange(values []int, minimum, maximum int, name string) error {
	seen := map[int]bool{}
	for _, value := range values {
		if value < minimum || value > maximum {
			return controlerr.Usage("%s must be between %d and %d", name, minimum, maximum)
		}
		if seen[value] {
			return controlerr.Usage("%s values must be unique", name)
		}
		seen[value] = true
	}
	return nil
}

func jsonDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
