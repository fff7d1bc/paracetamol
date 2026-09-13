package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paracetamol/internal/agent"
	"paracetamol/internal/atomicfile"
	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/evaluation"
	"paracetamol/internal/identity"
	"paracetamol/internal/platform"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/runtime"
	"paracetamol/internal/storage"
	"paracetamol/internal/textmodel"
	"paracetamol/internal/ui"
)

func (app *App) benchmarkAgent(args []string) (returned error) {
	set := app.flags("benchmark agent", usage("benchmark", "agent", "(--preset PRESET | --dwarfstar)", "[OPTIONS]"))
	presetID := set.String("preset", "", "installed agent-capable preset")
	dwarfstar := set.Bool("dwarfstar", false, "evaluate DwarfStar")
	var taskIDs stringList
	set.Var(&taskIDs, "task", "frozen task identifier; repeatable")
	listTasks := set.Bool("list-tasks", false, "list frozen tasks")
	repetitions := set.Int("repetitions", 1, "fresh attempts per task")
	contextSize := set.Int64("context", 131072, "server context")
	thinking := set.String("thinking", "", "explicit thinking level")
	profileFlag := set.String("profile", "auto", "GPU execution profile")
	backend := set.String("backend", "rocm", "rocm or vulkan")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact render node; repeatable")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	portText := set.String("port", "8187", "private model-server port")
	output := set.String("output", "", "new result JSON below evaluation storage")
	keepGoing := set.Bool("keep-going", false, "continue after infrastructure failure")
	dryRun := set.Bool("dry-run", false, "print the frozen suite")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("benchmark agent accepts no positional arguments")
	}
	suite, err := evaluation.Load(filepath.Join(app.Root, "evaluations", "coding", "tasks.json"))
	if err != nil {
		return err
	}
	selectedTasks, err := evaluation.Select(suite, taskIDs)
	if err != nil {
		return err
	}
	if *listTasks {
		terminal := app.terminal(app.Stdout)
		fmt.Fprintln(app.Stdout, terminal.Heading("Coding-agent evaluation tasks:"))
		rows := [][]string{{terminal.Label("Task"), terminal.Label("Kind"), terminal.Label("Difficulty"), terminal.Label("Repository")}}
		for _, task := range suite.Tasks {
			rows = append(rows, []string{terminal.Command(task.Identifier), task.Kind, task.Difficulty, task.Repository})
		}
		lines, _ := ui.ColumnLines(rows, nil, "  ")
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
		}
		return nil
	}
	if boolCount(*presetID != "", *dwarfstar) != 1 {
		if *presetID == "" && !*dwarfstar {
			set.renderHelp(app.Stdout)
		}
		return controlerr.Usage("choose exactly one of --preset or --dwarfstar")
	}
	if *repetitions < 1 || *contextSize < 4096 {
		return controlerr.Usage("repetitions must be positive and context must be at least 4096")
	}
	if err := requireChoice(*backend, "backend", "rocm", "vulkan"); err != nil {
		return err
	}
	if *dwarfstar && *backend != "rocm" {
		return controlerr.Usage("DwarfStar coding evaluation supports only ROCm")
	}
	profile := *profileFlag
	if err := platform.ValidateProfile(profile); err != nil || profile == "cpu" {
		return controlerr.Usage("coding evaluation requires a valid GPU profile")
	}
	selectedNodes, err := app.resolveDevices(profile, nodes, nodes != nil)
	if err != nil {
		return err
	}
	if *dwarfstar && len(selectedNodes) != 1 {
		return controlerr.Usage("DwarfStar coding evaluation requires exactly one render node")
	}
	port, err := config.ValidatePort(*portText)
	if err != nil {
		return err
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	model := ""
	var modelPolicy textmodel.Model
	level := *thinking
	if !*dwarfstar {
		preset, ok := managed.LlamaPresets[*presetID]
		if !ok || !preset.AgentTools {
			return controlerr.Usage("preset %q is not reviewed for agent tools", *presetID)
		}
		modelProfile := platform.ModelProfile(profile, selectedNodes)
		if supported := preset.RuntimeBackends(modelProfile); !setWasSet(set, "backend") && !preset.SupportsRuntime(*backend, modelProfile) && len(supported) > 0 {
			*backend = supported[0]
		}
		if !preset.SupportsRuntime(*backend, modelProfile) {
			return controlerr.Usage("llama.cpp preset %q does not support backend %s on profile %s", *presetID, *backend, modelProfile)
		}
		model = *presetID
		if level == "" {
			level = agent.ReasoningDefault(preset)
		}
		if !containsString(agent.ReasoningLevels(preset), level) {
			return controlerr.Usage("thinking level %q is not supported by %s", level, *presetID)
		}
	} else {
		modelPolicy, err = agent.DwarfStarModel(managed)
		if err != nil {
			return err
		}
		model = modelPolicy.ID
		if level == "" {
			level = textmodel.ReasoningDefault(modelPolicy)
		}
		if !containsString(textmodel.ReasoningLevels(modelPolicy), level) {
			return controlerr.Usage("thinking level %q is not supported by %s", level, model)
		}
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	server, containerName, err := app.agentEvaluationServer(managed, dataRoot, profile, *backend, selectedNodes, port, *contextSize, *presetID, *dwarfstar)
	if err != nil {
		return err
	}
	evaluationRoot := (storage.Layout{Root: dataRoot}).AgentEvaluations()
	runID := time.Now().UTC().Format("20060102T150405Z") + "-" + evaluation.Identifier()
	runRoot := filepath.Join(evaluationRoot, "runs", runID)
	resultPath := benchmark.DefaultPath(filepath.Join(evaluationRoot, "results"), "-"+model+".json")
	if *output != "" {
		resultPath, err = absoluteNewPath(*output)
		if err != nil {
			return err
		}
		if err := storage.ValidateManagedParent(resultPath, evaluationRoot, dataRoot, "coding evaluation result"); err != nil {
			return err
		}
	}
	if *dryRun {
		terminal := app.terminal(app.Stdout)
		fmt.Fprintln(app.Stdout, terminal.Heading("Coding-agent evaluation"))
		writeDetailRows(app.Stdout, terminal, [][2]string{
			{"Suite", suite.Identifier + " (" + suite.Fingerprint + ")"}, {"Model", model}, {"Harness", "Pi"},
			{"Context", fmt.Sprint(*contextSize)}, {"Thinking", level}, {"Tasks", joinTaskIDs(selectedTasks)},
			{"Repetitions", fmt.Sprint(*repetitions)}, {"Server", terminal.Command(shellJoin(server))},
		})
		return nil
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	image := configApplicationImage("llama-cpp")
	if *dwarfstar {
		image = configApplicationImage("dwarfstar")
	}
	present, err := app.podman().Exists(app.Context, "image", image)
	if err != nil {
		return err
	}
	if !present {
		return controlerr.New("image not found: %s", image)
	}
	if exists, err := app.podman().Exists(app.Context, "container", containerName); err != nil || exists {
		if err != nil {
			return err
		}
		return controlerr.New("coding evaluation requires stopped container %q", containerName)
	}
	if err := benchmark.PortAvailable(port); err != nil {
		return err
	}
	applicationID := "llama-cpp"
	if *dwarfstar {
		applicationID = "dwarfstar"
	}
	if err := (storage.Layout{Root: dataRoot}).PrepareRuntime(applicationID); err != nil {
		return err
	}
	runtimePi, err := agent.ResolvePiRuntime(app.Context, app.Runner, dataRoot, app.Root)
	if err != nil {
		return err
	}
	for _, path := range []string{runRoot, filepath.Join(evaluationRoot, "results"), filepath.Join(evaluationRoot, "cache", "go-mod"), filepath.Join(evaluationRoot, "cache", "go-build")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	environment := cloneEnvironmentMap(app.Environment)
	environment["GOMODCACHE"] = filepath.Join(evaluationRoot, "cache", "go-mod")
	environment["GOCACHE"] = filepath.Join(evaluationRoot, "cache", "go-build")
	environment["GOFLAGS"] = "-buildvcs=false"
	environment["PYTHONDONTWRITEBYTECODE"] = "1"
	environment["GIT_AUTHOR_NAME"], environment["GIT_COMMITTER_NAME"] = identity.DisplayName+" Evaluation", identity.DisplayName+" Evaluation"
	environment["GIT_AUTHOR_EMAIL"], environment["GIT_COMMITTER_EMAIL"] = "evaluation@invalid.local", "evaluation@invalid.local"
	prepared := map[string]evaluation.Attempt{}
	for _, task := range selectedTasks {
		for repetition := 1; repetition <= *repetitions; repetition++ {
			attempt, err := evaluation.Prepare(app.Context, app.Runner, suite, task, repetition, evaluationRoot, runRoot, mapEnvironment(environment))
			if err != nil {
				return err
			}
			prepared[fmt.Sprintf("%s-%d", task.Identifier, repetition)] = attempt
		}
	}
	result := evaluation.Result{Schema: evaluation.ResultSchema, Suite: suite.Identifier, SuiteFingerprint: suite.Fingerprint, RunID: runID, Status: "running", StartedAt: benchmark.Timestamp(), Model: evaluation.ModelResult{Identifier: model, Context: *contextSize, Thinking: level, Backend: *backend}, Harness: "Pi", Tasks: []evaluation.TaskResult{}}
	if err := benchmark.WriteNewCheckpoint(resultPath, result); err != nil {
		return err
	}
	defer func() {
		returned = withCleanupFailure(returned, "clean up coding-evaluation model container", app.podman().RemoveContainer(contextWithoutCancel(), containerName, 10, podman.Streams{}))
	}()
	if _, err := app.run(server, true); err != nil {
		result.Status, result.FinishedAt, result.Error = "infrastructure-failed", benchmark.Timestamp(), err.Error()
		return checkpointThenReturn(resultPath, result, err)
	}
	health := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	if *dwarfstar {
		health = fmt.Sprintf("http://127.0.0.1:%d/v1/models", port)
	}
	if err := benchmark.WaitForURL(app.Context, health); err != nil {
		result.Status, result.FinishedAt, result.Error = "infrastructure-failed", benchmark.Timestamp(), err.Error()
		return checkpointThenReturn(resultPath, result, err)
	}
	failed := false
	taskResults := []evaluation.TaskResult{}
	for _, task := range selectedTasks {
		attemptResults := []evaluation.AttemptResult{}
		for repetition := 1; repetition <= *repetitions; repetition++ {
			attempt := prepared[fmt.Sprintf("%s-%d", task.Identifier, repetition)]
			harness, runErr := app.runEvaluationPi(managed, runtimePi, dataRoot, port, task, attempt, model, level, environment, evaluationRoot)
			grade := evaluation.GradeResult{Outcome: "infrastructure-failed"}
			if runErr == nil {
				grade, runErr = evaluation.Grade(app.Context, app.Runner, app.Root, attempt, mapEnvironment(environment))
			}
			entry := evaluation.AttemptResult{Repetition: repetition, Harness: harness, Grade: grade}
			if runErr != nil {
				entry.Error = runErr.Error()
				failed = true
			}
			attemptResults = append(attemptResults, entry)
			if runErr != nil && !*keepGoing {
				taskResults = append(taskResults, evaluation.TaskResult{Identifier: task.Identifier, Kind: task.Kind, Difficulty: task.Difficulty, Attempts: attemptResults})
				result.Tasks, result.Status, result.Error = taskResults, "infrastructure-failed", runErr.Error()
				return checkpointThenReturn(resultPath, result, runErr)
			}
		}
		taskResults = append(taskResults, evaluation.TaskResult{Identifier: task.Identifier, Kind: task.Kind, Difficulty: task.Difficulty, Attempts: attemptResults})
		result.Tasks = taskResults
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
	}
	result.Status, result.FinishedAt = "complete", benchmark.Timestamp()
	if failed {
		result.Status = "completed-with-infrastructure-failures"
	}
	if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
		return err
	}
	report := strings.TrimSuffix(resultPath, filepath.Ext(resultPath)) + ".md"
	if err := atomicfile.Write(report, []byte(renderAgentReport(result)), 0o644, atomicfile.Create); err != nil {
		return err
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s %s\n%s %s\n", terminal.Success("Coding-agent evaluation complete:"), resultPath, terminal.Label("Report:"), report)
	if failed {
		return controlerr.New("coding evaluation completed with infrastructure failures")
	}
	return nil
}

func (app *App) agentEvaluationServer(managed catalog.Catalog, dataRoot, profile, backend string, nodes []string, port int, contextSize int64, presetID string, dwarfstar bool) ([]string, string, error) {
	volume := app.podman().SELinuxVolumeSuffix(app.Context)
	if dwarfstar {
		modelPolicy, err := agent.DwarfStarModel(managed)
		if err != nil {
			return nil, "", err
		}
		bundle := managed.Bundles[modelPolicy.Bundle]
		if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
			return nil, "", err
		}
		application, _ := config.ApplicationByID("dwarfstar")
		model := content.ArtifactPath(dataRoot, managed.Artifacts[bundle.Artifacts[0]])
		command, err := runtime.DwarfStarCommand(runtime.DwarfStarOptions{Image: application.Image, Mode: "server", DataDir: dataRoot, Model: model, RenderNodes: nodes, Profile: profile, Listen: "127.0.0.1", Port: port, Context: contextSize, OutputTokens: minInt64(16000, contextSize/4), Detach: true}, volume)
		return command, application.ContainerName, err
	}
	preset := managed.LlamaPresets[presetID]
	bundle := managed.Bundles[preset.Bundle]
	if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
		return nil, "", err
	}
	artifact := managed.Artifacts[preset.Artifact]
	options := runtime.LlamaOptions{Image: configApplicationImage("llama-cpp"), Profile: profile, Mode: "server", DataDir: dataRoot, Backend: backend, ManagedModel: artifact.Destination, SpeculativeType: preset.SpeculativeType, DraftTokens: preset.DraftTokensForBackend(backend), ContextOverrideArchitectures: preset.ContextOverrideArchitectures, Jinja: preset.Jinja, ReasoningPreserve: preset.ReasoningPreserve, ChatTemplate: preset.ChatTemplate, ProfileFlashAttention: preset.FlashAttention, ProfileKVCache: preset.KVCache, ProfileModelLoad: preset.ModelLoad, AllowedProfiles: preset.BackendProfiles[backend], RenderNodes: nodes, Listen: "127.0.0.1", Port: port, Context: contextSize, Detach: true, AutoRemove: true}
	if preset.DraftArtifact != "" {
		options.ManagedDraft = managed.Artifacts[preset.DraftArtifact].Destination
	}
	if preset.SamplingPolicy != "" {
		policy := managed.SamplingPolicies[preset.SamplingPolicy]
		options.SamplingDefaults = map[string]any{"thinking": policy.Thinking, "non_thinking": policy.NonThinking}
	}
	command, err := runtime.LlamaCommand(options, volume)
	application, _ := config.ApplicationByID("llama-cpp")
	return command, application.ContainerName, err
}

func (app *App) runEvaluationPi(managed catalog.Catalog, piRuntime agent.PiRuntime, dataRoot string, port int, task evaluation.Task, attempt evaluation.Attempt, model, thinking string, environment map[string]string, evaluationRoot string) (evaluation.HarnessResult, error) {
	arguments := []string{"--provider", agent.ProviderID, "--model", model, "--thinking", thinking, "--print", "--mode", "json", "--no-session", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes", "--tools", "read,bash,edit,write", task.Prompt}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	plan, err := agent.CreatePiPlanForModels(app.Context, managed, app.Root, endpoint, arguments, piRuntime, []string{model})
	if err != nil {
		return evaluation.HarnessResult{}, err
	}
	if _, err := agent.PreparePiState(plan, dataRoot); err != nil {
		return evaluation.HarnessResult{}, err
	}
	child := map[string]string{"PI_CODING_AGENT_DIR": filepath.Join(agent.SandboxHome, ".local", "share", "pi", "agent"), "PI_SKIP_VERSION_CHECK": "1", "PI_TELEMETRY": "0", "PI_OFFLINE": "1", "TERM": "dumb", "GIT_AUTHOR_NAME": identity.DisplayName + " Evaluation", "GIT_AUTHOR_EMAIL": "evaluation@invalid.local"}
	readOnly := []agent.Mount{{Source: piRuntime.Root, Destination: piRuntime.Root}}
	if task.Toolchain == "go" {
		readOnly = append(readOnly, agent.Mount{Source: filepath.Join(evaluationRoot, "cache", "go-mod"), Destination: filepath.Join(agent.SandboxRuntime, "go-mod")})
		child["GOMODCACHE"], child["GOCACHE"], child["GOPROXY"], child["GOSUMDB"], child["GOFLAGS"] = filepath.Join(agent.SandboxRuntime, "go-mod"), "/tmp/go-build", "off", "off", "-buildvcs=false"
	} else {
		child["PYTHONDONTWRITEBYTECODE"], child["PYTHONPATH"] = "1", filepath.Join(attempt.Fixture, "src")
	}
	sandbox, err := agent.CreateSandboxPlan(app.Context, app.Runner, plan.Command, dataRoot, attempt.Fixture, "pi", child, environment, readOnly)
	if err != nil {
		return evaluation.HarnessResult{}, err
	}
	stdoutPath, stderrPath := filepath.Join(attempt.Root, "pi.jsonl"), filepath.Join(attempt.Root, "pi.stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return evaluation.HarnessResult{}, err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return evaluation.HarnessResult{}, err
	}
	defer stderr.Close()
	started := time.Now()
	result, runErr := app.Runner.Run(app.Context, process.Command{Name: sandbox.Command[0], Args: sandbox.Command[1:], Dir: attempt.Fixture, Env: sandbox.Environment, Stdout: stdout, Stderr: stderr})
	harness := evaluation.HarnessResult{Exit: result.Status, WallSeconds: time.Since(started).Seconds(), Usage: transcriptUsage(stdoutPath)}
	if runErr != nil {
		return harness, runErr
	}
	if result.Status != 0 {
		return harness, fmt.Errorf("Pi evaluation exited with status %d", result.Status)
	}
	return harness, nil
}

func transcriptUsage(path string) evaluation.Usage {
	totals := evaluation.Usage{}
	handle, err := os.Open(path)
	if err != nil {
		return totals
	}
	defer handle.Close()
	scanner := bufio.NewScanner(handle)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 4*1024*1024)
	for scanner.Scan() {
		var value map[string]any
		if json.Unmarshal(scanner.Bytes(), &value) != nil {
			continue
		}
		message, _ := value["message"].(map[string]any)
		usage, _ := message["usage"].(map[string]any)
		for source, target := range map[string]*int64{"input": &totals.Input, "output": &totals.Output, "reasoning": &totals.Reasoning, "cacheRead": &totals.CacheRead, "cacheWrite": &totals.CacheWrite} {
			if amount, ok := usage[source].(float64); ok && amount >= 0 {
				*target += int64(amount)
			}
		}
	}
	return totals
}

func renderAgentReport(result evaluation.Result) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Coding-agent evaluation\n\n- Suite: `%s`\n- Status: `%s`\n\n| Task | Kind | Difficulty | Outcome | Wall time | Output tokens |\n|---|---|---:|---|---:|---:|\n", result.Suite, result.Status)
	for _, task := range result.Tasks {
		for _, attempt := range task.Attempts {
			fmt.Fprintf(&builder, "| `%s` | %s | %s | **%s** | %.1fs | %d |\n", task.Identifier, task.Kind, task.Difficulty, attempt.Grade.Outcome, attempt.Harness.WallSeconds, attempt.Harness.Usage.Output)
		}
	}
	return builder.String()
}

func joinTaskIDs(tasks []evaluation.Task) string {
	ids := make([]string, len(tasks))
	for index, task := range tasks {
		ids[index] = task.Identifier
	}
	return strings.Join(ids, ", ")
}

func cloneEnvironmentMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func configApplicationImage(identifier string) string {
	application, _ := config.ApplicationByID(identifier)
	return application.Image
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
