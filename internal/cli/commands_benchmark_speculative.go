package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rocmplete/internal/agent"
	"rocmplete/internal/benchmark"
	"rocmplete/internal/config"
	"rocmplete/internal/content"
	"rocmplete/internal/controlerr"
	"rocmplete/internal/platform"
	"rocmplete/internal/runtime"
	"rocmplete/internal/storage"
)

type intList []int

func (values *intList) String() string {
	parts := make([]string, len(*values))
	for index, value := range *values {
		parts[index] = fmt.Sprint(value)
	}
	return strings.Join(parts, ",")
}
func (values *intList) Set(value string) error {
	var parsed int
	if _, err := fmt.Sscan(value, &parsed); err != nil {
		return err
	}
	*values = append(*values, parsed)
	return nil
}

func (app *App) benchmarkSpeculative(args []string) error {
	set := app.flags("benchmark llama-cpp-speculative", "Usage: ./rocmplete benchmark llama-cpp-speculative --preset PRESET [OPTIONS]")
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
	if err := set.Parse(args); err != nil {
		return err
	}
	if *presetID == "" {
		return controlerr.Usage("--preset is required")
	}
	if *output != "" && *resume != "" {
		return controlerr.Usage("--output and --resume are mutually exclusive")
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
	managedDraft := ""
	if preset.DraftArtifact != "" {
		managedDraft = managed.Artifacts[preset.DraftArtifact].Destination
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
	if preset.SamplingPolicy != "" {
		policy := managed.SamplingPolicies[preset.SamplingPolicy]
		samplingDefaults = map[string]any{"thinking": policy.Thinking, "non_thinking": policy.NonThinking}
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
		command, err := runtime.LlamaCommand(runtime.LlamaOptions{Image: image, Profile: profile, Mode: "server", DataDir: dataRoot, Backend: *backend, ManagedModel: artifact.Destination, ManagedDraft: managedDraft, SpeculativeType: preset.SpeculativeType, DraftTokens: int64(depth), ContextOverrideArchitectures: preset.ContextOverrideArchitectures, Jinja: preset.Jinja, ReasoningPreserve: preset.ReasoningPreserve, ChatTemplate: preset.ChatTemplate, SamplingDefaults: samplingDefaults, ProfileFlashAttention: flashPolicy, ProfileKVCache: kvPolicy, RenderNodes: selectedNodes, Listen: "127.0.0.1", Port: port, Context: int64(*contextSize), Detach: true, Unconfined: *unconfined, ContainerName: "rocmplete-llama-cpp-speculative-benchmark", ContainerRole: "benchmark", AutoRemove: false, Arguments: extra, Environment: environment}, app.podman().SELinuxVolumeSuffix(app.Context))
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
	if err := (storage.Layout{Root: dataRoot}).PrepareRuntime("llama-cpp"); err != nil {
		return err
	}
	imageID, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", image}, "cannot inspect benchmark image")
	if err != nil {
		return err
	}
	definition := map[string]any{"image": map[string]any{"reference": image, "id": imageID}, "profile": profile, "backend": *backend, "render_nodes": selectedNodes, "preset": *presetID, "speculative_type": preset.SpeculativeType, "incumbent_depth": preset.DraftTokensForBackend(*backend), "depths": []int(depths), "context_depths": []int(contexts), "repetitions": *repetitions, "generation_tokens": *generation, "seed": *seed, "server_context": *contextSize, "thinking": map[string]any{"client": level, "native": nativeReasoning}, "draft_probability_min": *draftProbability, "draft_backend_sampling": *draftSampling, "ngram_simple": *ngram, "graph_optimization": *graphOptimization, "disable_graphs": *disableGraphs, "poll": *poll, "no_host": *noHost, "flash_attention": *flash, "cache_type_k": *cacheK, "cache_type_v": *cacheV, "batch_size": *batch, "ubatch_size": *ubatch}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		return err
	}
	resultPath := benchmark.DefaultPath((storage.Layout{Root: dataRoot}).LlamaBenchmarks(), "-speculative-depth-sweep.json")
	var result map[string]any
	if *resume != "" {
		resultPath, err = filepath.Abs(*resume)
		if err != nil {
			return err
		}
		result, err = benchmark.ReadObject(resultPath)
		if err != nil {
			return err
		}
		if result["schema"] != benchmark.SpeculativeSchema || result["fingerprint"] != fingerprint {
			return controlerr.New("speculative benchmark checkpoint does not match this definition")
		}
	} else {
		if *output != "" {
			resultPath, err = absoluteNewPath(*output)
			if err != nil {
				return err
			}
		}
		trials := []any{}
		for repetition := 1; repetition <= *repetitions; repetition++ {
			for _, contextDepth := range contexts {
				for _, depth := range depths {
					trials = append(trials, map[string]any{"identifier": fmt.Sprintf("d%d-c%d-s%d-r%d", depth, contextDepth, *seed+repetition-1, repetition), "depth": depth, "context_depth": contextDepth, "seed": *seed + repetition - 1, "repetition": repetition, "status": "pending"})
				}
			}
		}
		sort.Slice(trials, func(i, j int) bool {
			left := sha256.Sum256([]byte("schedule-v1:" + fmt.Sprint(trials[i].(map[string]any)["identifier"])))
			right := sha256.Sum256([]byte("schedule-v1:" + fmt.Sprint(trials[j].(map[string]any)["identifier"])))
			return strings.Compare(string(left[:]), string(right[:])) < 0
		})
		result = map[string]any{"schema": benchmark.SpeculativeSchema, "suite_id": time.Now().UTC().Format("20060102T150405Z") + "-" + benchmark.Identifier(), "fingerprint": fingerprint, "definition": definition, "status": "preparing", "started_at": benchmark.Timestamp(), "trials": trials, "summary": map[string]any{}}
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
	}
	trials, ok := result["trials"].([]any)
	if !ok {
		return controlerr.New("speculative benchmark checkpoint has no trial list")
	}
	result["status"] = "running"
	if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
		return err
	}
	failed := false
	for index, raw := range trials {
		trial, ok := raw.(map[string]any)
		if !ok {
			return controlerr.New("speculative benchmark has an invalid trial")
		}
		if trial["status"] == "complete" {
			continue
		}
		depth := int(numberAsInt64(trial["depth"]))
		contextDepth := int(numberAsInt64(trial["context_depth"]))
		requestSeed := int(numberAsInt64(trial["seed"]))
		fmt.Fprintf(app.Stdout, "[%d/%d] depth %d, target %d, seed %d\n", index+1, len(trials), depth, contextDepth, requestSeed)
		trial["status"], trial["started_at"] = "running", benchmark.Timestamp()
		_ = benchmark.WriteCheckpoint(resultPath, result)
		startup := time.Now()
		_, startErr := app.run(commands[depth], true)
		if startErr == nil {
			startErr = benchmark.WaitForHealth(app.Context, fmt.Sprintf("http://127.0.0.1:%d", port))
		}
		startupSeconds := time.Since(startup).Seconds()
		var metrics map[string]any
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
		_, _ = app.run([]string{"podman", "rm", "--force", "--time", "10", "--ignore", "rocmplete-llama-cpp-speculative-benchmark"}, true)
		if startErr != nil {
			trial["status"], trial["error"], trial["finished_at"] = "failed", startErr.Error(), benchmark.Timestamp()
			failed = true
		} else {
			for key, value := range metrics {
				trial[key] = value
			}
			trial["status"], trial["finished_at"] = "complete", benchmark.Timestamp()
			fmt.Fprintf(app.Stdout, "  %.2f t/s, accepted %v/%v (%.1f%%)\n", metrics["generation_tokens_per_second"], metrics["accepted_draft_tokens"], metrics["drafted_tokens"], metrics["acceptance_percent"])
		}
		result["summary"] = benchmark.SpeculativeSummary(trials, int(preset.DraftTokensForBackend(*backend)))
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
		if startErr != nil && !*keepGoing {
			result["status"] = "failed"
			_ = benchmark.WriteCheckpoint(resultPath, result)
			return startErr
		}
	}
	result["status"], result["finished_at"] = "complete", benchmark.Timestamp()
	if failed {
		result["status"] = "failed"
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

func numberAsInt64(value any) int64 {
	switch typed := value.(type) {
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	}
	return 0
}
