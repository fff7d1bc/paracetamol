package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"rocmplete/internal/catalog"
	"rocmplete/internal/config"
	"rocmplete/internal/controlerr"
	"rocmplete/internal/identity"
	"rocmplete/internal/platform"
	"rocmplete/internal/process"
)

const llamaRuntimeReportPath = "/tmp/rocmplete-llama-runtime"

var llamaRuntimeKeys = map[string]bool{
	"schema": true, "mode": true, "profile": true, "backend": true,
	"device": true, "backend_devices": true, "architecture": true,
	"gpu_count": true, "router": true, "models_max": true, "context": true,
	"listen": true, "host_listen": true, "port": true, "api_key": true,
	"unified_memory": true, "vulkan_f16_kv_contiguize": true,
}

var routerModelIdentifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func parseLlamaRuntimeReport(contents string) (map[string]string, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(contents, "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		_, duplicate := values[key]
		if !ok || key == "" || duplicate {
			return nil, controlerr.New("invalid llama.cpp runtime report")
		}
		values[key] = value
	}
	if len(values) != len(llamaRuntimeKeys) || values["schema"] != "1" || values["mode"] != "server" {
		return nil, controlerr.New("unsupported llama.cpp runtime report")
	}
	for key := range values {
		if !llamaRuntimeKeys[key] {
			return nil, controlerr.New("unsupported llama.cpp runtime report")
		}
	}
	if values["backend"] != "rocm" && values["backend"] != "vulkan" {
		return nil, controlerr.New("invalid backend in llama.cpp runtime report")
	}
	if values["profile"] == "auto" || platform.ValidateProfile(values["profile"]) != nil {
		return nil, controlerr.New("invalid profile in llama.cpp runtime report")
	}
	for _, key := range []string{"router", "api_key", "unified_memory", "vulkan_f16_kv_contiguize"} {
		if values[key] != "0" && values[key] != "1" {
			return nil, controlerr.New("invalid %s in llama.cpp runtime report", key)
		}
	}
	for _, key := range []string{"gpu_count", "models_max"} {
		value, err := strconv.Atoi(values[key])
		if err != nil || value < 0 || key == "models_max" && value < 1 {
			return nil, controlerr.New("invalid %s in llama.cpp runtime report", key)
		}
	}
	if values["context"] != "" {
		if value, err := strconv.ParseInt(values["context"], 10, 64); err != nil || value < 1 {
			return nil, controlerr.New("invalid context in llama.cpp runtime report")
		}
	}
	if err := config.ValidateListenAddress(values["listen"]); err != nil {
		return nil, controlerr.New("invalid container listen address in llama.cpp runtime report")
	}
	if err := config.ValidateListenAddress(values["host_listen"]); err != nil {
		return nil, controlerr.New("invalid host listen address in llama.cpp runtime report")
	}
	if _, err := config.ValidatePort(values["port"]); err != nil {
		return nil, controlerr.New("invalid port in llama.cpp runtime report")
	}
	return values, nil
}

func parseContainerEnvironment(contents string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(contents, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = value
		}
	}
	return values
}

func parseRouterSnapshot(contents string) (map[string]map[string]string, error) {
	lines := strings.Split(contents, "\n")
	if len(lines) == 0 || lines[0] != "version = 1" {
		return nil, controlerr.New("running llama.cpp router has an unsupported preset")
	}
	sections := make(map[string]map[string]string)
	var current map[string]string
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			identifier := line[1 : len(line)-1]
			if !routerModelIdentifier.MatchString(identifier) || sections[identifier] != nil {
				return nil, controlerr.New("running llama.cpp router preset is invalid")
			}
			current = make(map[string]string)
			sections[identifier] = current
			continue
		}
		key, value, ok := strings.Cut(line, " = ")
		if !ok || key == "" || current == nil {
			return nil, controlerr.New("running llama.cpp router preset is invalid")
		}
		if _, duplicate := current[key]; duplicate {
			return nil, controlerr.New("running llama.cpp router preset is invalid")
		}
		current[key] = value
	}
	if len(sections) == 0 {
		return nil, controlerr.New("running llama.cpp router has no models")
	}
	return sections, nil
}

func (app *App) capturePodman(arguments []string, description string) ([]byte, error) {
	result, err := app.Runner.Run(app.Context, process.Command{Name: "podman", Args: arguments})
	if err != nil {
		return nil, controlerr.New("%s: %v", description, err)
	}
	if result.Status != 0 {
		detail := strings.TrimSpace(string(result.Stderr))
		if detail != "" {
			return nil, controlerr.New("%s: %s", description, detail)
		}
		return nil, controlerr.New("%s", description)
	}
	return result.Stdout, nil
}

func (app *App) llamaStatus(requested string) error {
	application, _ := config.ApplicationByID("llama-cpp")
	present, err := app.podman().Exists(app.Context, "container", application.ContainerName)
	if err != nil {
		return err
	}
	if !present {
		return controlerr.New("container %q does not exist", application.ContainerName)
	}
	state, err := app.podman().Capture(app.Context, []string{"inspect", "--format", "{{.State.Status}}", application.ContainerName}, "cannot inspect llama.cpp container")
	if err != nil {
		return err
	}
	if state != "running" {
		return controlerr.New("container %q is %s", application.ContainerName, state)
	}
	rawRuntime, err := app.capturePodman([]string{"exec", application.ContainerName, "cat", llamaRuntimeReportPath}, "cannot read the live llama.cpp runtime report; retry after startup")
	if err != nil {
		return err
	}
	runtimeReport, err := parseLlamaRuntimeReport(string(rawRuntime))
	if err != nil {
		return err
	}
	rawEnvironment, err := app.capturePodman([]string{"inspect", "--format", "{{range .Config.Env}}{{println .}}{{end}}", application.ContainerName}, "cannot inspect llama.cpp container environment")
	if err != nil {
		return err
	}
	environment := parseContainerEnvironment(string(rawEnvironment))
	image, err := app.podman().Capture(app.Context, []string{"inspect", "--format", "{{.Config.Image}}", application.ContainerName}, "cannot inspect llama.cpp image")
	if err != nil || image == "" || strings.ContainsAny(image, "\r\n") {
		return firstError(err, controlerr.New("Podman returned an invalid container image name"))
	}
	imageID, err := app.podman().Capture(app.Context, []string{"inspect", "--format", "{{.Image}}", application.ContainerName}, "cannot inspect llama.cpp image identity")
	if err != nil || imageID == "" || strings.ContainsAny(imageID, "\r\n") {
		return firstError(err, controlerr.New("Podman returned an invalid container image ID"))
	}
	labelsText, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{json .Labels}}", imageID}, "cannot inspect llama.cpp image labels")
	if err != nil {
		return err
	}
	labels := map[string]string{}
	if labelsText != "null" {
		if err := json.Unmarshal([]byte(labelsText), &labels); err != nil {
			return controlerr.New("Podman returned invalid image labels")
		}
	}
	rawCommand, err := app.capturePodman([]string{"exec", application.ContainerName, "cat", "/proc/1/cmdline"}, "cannot inspect the running llama.cpp command")
	if err != nil {
		return err
	}
	processCommand, err := parseLlamaProcessCommand(rawCommand)
	if err != nil {
		return err
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}

	var preset *catalog.LlamaPreset
	var selected map[string]string
	var router map[string]map[string]string
	if runtimeReport["router"] == "1" {
		rawRouter, captureErr := app.capturePodman([]string{"exec", application.ContainerName, "cat", "/run/rocmplete/models.ini"}, "cannot read the running llama.cpp router preset")
		if captureErr != nil {
			return captureErr
		}
		router, err = parseRouterSnapshot(string(rawRouter))
		if err != nil {
			return err
		}
		if requested != "" {
			section, ok := router[requested]
			candidate, catalogOK := managed.LlamaPresets[requested]
			if !ok || !catalogOK {
				return controlerr.New("running router does not expose model %q", requested)
			}
			selected, preset = section, &candidate
		}
	} else {
		preset, err = directLlamaPreset(managed, environment, runtimeReport["backend"], requested)
		if err != nil {
			return err
		}
	}

	fmt.Fprintf(app.Stdout, "%s llama.cpp runtime\n", identity.DisplayName)
	writeStatusRows(app.Stdout, [][2]string{
		{"State", state},
		{identity.DisplayName, firstNonEmpty(environment["ROCMLETE_SOURCE_REVISION"], app.projectRevision())},
		{"Image", image}, {"Image ID", imageID},
		{"llama.cpp", firstNonEmpty(labels["org.opencontainers.image.revision"], "unknown")},
		{"ROCm", firstNonEmpty(labels["io.github.fff7d1bc.rocmplete.rocm.version"], "unknown")},
	})

	profile := runtimeReport["profile"]
	requestedProfile := environment["ROCMLETE_PROFILE"]
	profileDisplay := profile
	if requestedProfile != "" && requestedProfile != profile {
		profileDisplay += " (requested " + requestedProfile + ")"
	}
	writeStatusSection(app.Stdout, "Hardware", [][2]string{
		{"Backend", runtimeReport["backend"]}, {"Profile", profileDisplay},
		{"Architecture", runtimeReport["architecture"]}, {"Device", runtimeReport["device"]},
		{"Render nodes", firstNonEmpty(environment["ROCMLETE_RENDER_NODES"], "none")},
	})
	mode := "direct model"
	if runtimeReport["router"] == "1" {
		mode = "router"
	} else if preset != nil {
		mode = "direct preset"
	}
	serverRows := [][2]string{
		{"Mode", mode}, {"Container bind", runtimeReport["listen"] + ":" + runtimeReport["port"]},
		{"Host publish", runtimeReport["host_listen"] + ":" + runtimeReport["port"]},
		{"Authentication", map[bool]string{true: "configured (value redacted)", false: "none"}[runtimeReport["api_key"] == "1"]},
		{"Unified memory", onOff(runtimeReport["unified_memory"])},
		{"Vulkan F16 KV fix", onOff(runtimeReport["vulkan_f16_kv_contiguize"])},
	}
	if router != nil {
		serverRows = append(serverRows, [2]string{"Loaded-model limit", runtimeReport["models_max"]}, [2]string{"Configured models", strconv.Itoa(len(router))})
	}
	writeStatusSection(app.Stdout, "Server", serverRows)

	if router != nil && preset == nil {
		identifiers := make([]string, 0, len(router))
		for identifier := range router {
			identifiers = append(identifiers, identifier)
		}
		sort.Strings(identifiers)
		fmt.Fprintln(app.Stdout, "\nRouter models")
		for _, identifier := range identifiers {
			fmt.Fprintf(app.Stdout, "  %s\n", identifier)
		}
		fmt.Fprintf(app.Stdout, "\nDetails: %s\n", identity.Command("status", "llama-cpp", "--model", "MODEL"))
	} else {
		modelRows, rowErr := llamaModelStatusRows(managed, preset, selected, environment, runtimeReport)
		if rowErr != nil {
			return rowErr
		}
		writeStatusSection(app.Stdout, "Model policy", modelRows)
	}
	writeStatusSection(app.Stdout, identity.DisplayName+" behavior", [][2]string{
		{"Downstream patches", firstNonEmpty(labels["io.github.fff7d1bc.rocmplete.llama-cpp.patches"], "unknown")},
		{"Policy boundary", "catalog defaults are selected after reasoning mode resolution"},
		{"Secrets", "API-key values and host secret paths are never printed"},
	})
	fmt.Fprintf(app.Stdout, "\nReproduce effective %s launch\n  %s\n", identity.DisplayName, shellJoin(llamaReproductionCommand(environment, runtimeReport, preset)))
	fmt.Fprintf(app.Stdout, "\nExact running llama.cpp command\n  %s\n", shellJoin(processCommand))
	return nil
}

func firstError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

func parseLlamaProcessCommand(raw []byte) ([]string, error) {
	if !utf8.Valid(raw) {
		return nil, controlerr.New("running llama.cpp command is not valid UTF-8")
	}
	value := strings.TrimRight(string(raw), "\x00")
	if value == "" {
		return nil, controlerr.New("running llama.cpp command is empty")
	}
	parts := strings.Split(value, "\x00")
	if filepath.Base(parts[0]) != "llama-server" {
		return nil, controlerr.New("running container PID 1 is not llama-server")
	}
	for index := range parts {
		if (parts[index] == "--api-key-file" || parts[index] == "--api-key") && index+1 < len(parts) {
			parts[index+1] = "API_KEY_FILE"
		} else if strings.HasPrefix(parts[index], "--api-key-file=") || strings.HasPrefix(parts[index], "--api-key=") {
			name, _, _ := strings.Cut(parts[index], "=")
			parts[index] = name + "=API_KEY_FILE"
		}
	}
	return parts, nil
}

func directLlamaPreset(managed catalog.Catalog, environment map[string]string, backend, requested string) (*catalog.LlamaPreset, error) {
	var candidates []catalog.LlamaPreset
	for _, candidate := range managed.LlamaPresets {
		artifact := managed.Artifacts[candidate.Artifact]
		draft := ""
		if candidate.DraftArtifact != "" {
			draft = "/content/models/" + managed.Artifacts[candidate.DraftArtifact].Destination
		}
		if environment["ROCMLETE_LLAMA_MODEL"] == "/content/models/"+artifact.Destination &&
			environment["ROCMLETE_LLAMA_DRAFT_MODEL"] == draft &&
			environment["ROCMLETE_LLAMA_SPECULATIVE_TYPE"] == candidate.SpeculativeType &&
			environment["ROCMLETE_LLAMA_DRAFT_TOKENS"] == strconv.FormatInt(candidate.DraftTokensForBackend(backend), 10) {
			candidates = append(candidates, candidate)
		}
	}
	if requested != "" {
		for _, candidate := range candidates {
			if candidate.ID == requested {
				selected := candidate
				return &selected, nil
			}
		}
		if _, ok := managed.LlamaPresets[requested]; !ok {
			return nil, controlerr.Usage("unknown llama.cpp preset %q", requested)
		}
		return nil, controlerr.New("running llama.cpp server is not using preset %q", requested)
	}
	if len(candidates) == 1 {
		return &candidates[0], nil
	}
	if len(candidates) > 1 {
		return nil, controlerr.New("running model matches multiple presets; select one with --model")
	}
	return nil, nil
}

func llamaModelStatusRows(managed catalog.Catalog, preset *catalog.LlamaPreset, section, environment, runtimeReport map[string]string) ([][2]string, error) {
	profile := runtimeReport["profile"]
	modelPath, contextValue, template, reasoningOutput, speculativeType, draftTokens, flashAttention, kvCache, sampling := "", runtimeReport["context"], "", "", "", "0", "", "", ""
	if section != nil {
		modelPath = section["model"]
		if contextValue == "" {
			contextValue = section["c"]
		}
		if value := section["chat-template-file"]; value != "" {
			template = "managed " + strings.TrimSuffix(filepath.Base(value), filepath.Ext(value))
		}
		if section["reasoning-preserve"] == "true" {
			reasoningOutput = "preserved in the API response"
		}
		speculativeType, draftTokens = section["spec-type"], firstNonEmpty(section["spec-draft-n-max"], "0")
		flashAttention, kvCache = section["rocmplete-flash-attn-"+profile], section["rocmplete-kv-cache-"+profile]
		sampling = section["sampling-defaults-by-reasoning"]
	} else {
		modelPath = environment["ROCMLETE_LLAMA_MODEL"]
		if value := environment["ROCMLETE_LLAMA_CHAT_TEMPLATE"]; value != "" {
			template = "managed " + value
		}
		if environment["ROCMLETE_LLAMA_REASONING_PRESERVE"] == "1" {
			reasoningOutput = "preserved in the API response"
		}
		speculativeType, draftTokens = environment["ROCMLETE_LLAMA_SPECULATIVE_TYPE"], firstNonEmpty(environment["ROCMLETE_LLAMA_DRAFT_TOKENS"], "0")
		key := strings.ToUpper(strings.ReplaceAll(profile, "-", "_"))
		flashAttention, kvCache = environment["ROCMLETE_LLAMA_FLASH_ATTN_"+key], environment["ROCMLETE_LLAMA_KV_CACHE_"+key]
		sampling = environment["ROCMLETE_LLAMA_SAMPLING_DEFAULTS"]
	}
	rows := make([][2]string, 0, 20)
	contextPolicy, reasoning := "model metadata", "not catalog-managed"
	if preset == nil {
		rows = append(rows, [2]string{"Preset", "local GGUF"}, [2]string{"GGUF", firstNonEmpty(modelPath, "unknown")})
	} else {
		artifact := managed.Artifacts[preset.Artifact]
		rows = append(rows,
			[2]string{"Preset", preset.ID}, [2]string{"Description", artifact.Description},
			[2]string{"GGUF", firstNonEmpty(modelPath, "unknown")},
			[2]string{"Source", fmt.Sprintf("%s @ %s / %s", artifact.Source.Repository, artifact.Source.Revision, artifact.Source.Path)},
			[2]string{"SHA-256", artifact.SHA256})
		if len(preset.ContextOverrideArchitectures) > 0 {
			contextPolicy = "forced for " + strings.Join(preset.ContextOverrideArchitectures, ", ") + "; automatic fitting disabled"
		}
		reasoning = llamaReasoningPolicy(*preset)
	}
	if contextValue == "" {
		contextValue = "model metadata"
	} else {
		contextValue += " tokens"
	}
	if template == "" {
		template = "model metadata"
	}
	if reasoningOutput == "" {
		reasoningOutput = "llama.cpp default"
	}
	speculation := "off"
	if speculativeType != "" {
		name := map[string]string{"draft-mtp": "MTP", "draft-dflash": "DFlash"}[speculativeType]
		if name == "" {
			name = speculativeType
		}
		speculation = name + ", " + draftTokens + " draft tokens"
	}
	rows = append(rows,
		[2]string{"Context", contextValue}, [2]string{"Context policy", contextPolicy},
		[2]string{"Template", template}, [2]string{"Reasoning", reasoning},
		[2]string{"Reasoning output", reasoningOutput}, [2]string{"Speculation", speculation},
		[2]string{"Flash Attention", firstNonEmpty(flashAttention, "llama.cpp default")},
		[2]string{"K/V cache", firstNonEmpty(kvCache, "llama.cpp default")})
	samplingRows, err := llamaSamplingRows(managed, preset, sampling)
	return append(rows, samplingRows...), err
}

func llamaReasoningPolicy(preset catalog.LlamaPreset) string {
	if preset.ReasoningControl == "" {
		return "not exposed"
	}
	levels := append([]string(nil), preset.ReasoningLevels...)
	if preset.ReasoningControl == "toggle" {
		levels = []string{"on"}
	}
	if preset.ReasoningOff {
		levels = append([]string{"off"}, levels...)
	}
	return fmt.Sprintf("%s; %s; default %s", preset.ReasoningControl, strings.Join(levels, ", "), preset.ReasoningDefault)
}

func llamaSamplingRows(managed catalog.Catalog, preset *catalog.LlamaPreset, raw string) ([][2]string, error) {
	var modes map[string]map[string]any
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &modes); err != nil {
			return nil, controlerr.New("running llama.cpp sampling policy is invalid")
		}
	} else if preset != nil && preset.SamplingPolicy != "" {
		policy := managed.SamplingPolicies[preset.SamplingPolicy]
		modes = map[string]map[string]any{"thinking": policy.Thinking, "non_thinking": policy.NonThinking}
	} else {
		return [][2]string{{"Sampling", "request values or llama.cpp defaults"}}, nil
	}
	fields := [][2]string{{"temperature", "temp"}, {"top_p", "top-p"}, {"top_k", "top-k"}, {"min_p", "min-p"}, {"presence_penalty", "presence"}, {"repeat_penalty", "repeat"}}
	rows := make([][2]string, 0, 3)
	for _, mode := range [][2]string{{"thinking", "Thinking sampling"}, {"non_thinking", "Off sampling"}} {
		values := modes[mode[0]]
		parts := make([]string, 0, len(fields))
		for _, field := range fields {
			value, ok := values[field[0]]
			if !ok {
				return nil, controlerr.New("running llama.cpp sampling policy is invalid")
			}
			parts = append(parts, fmt.Sprintf("%s %v", field[1], value))
		}
		rows = append(rows, [2]string{mode[1], strings.Join(parts, ", ")})
	}
	return append(rows, [2]string{"Precedence", "explicit request values override these defaults"}), nil
}

func llamaReproductionCommand(environment, runtimeReport map[string]string, preset *catalog.LlamaPreset) []string {
	command := []string{"./" + identity.CommandName, "run", "llama-cpp", "server"}
	if runtimeReport["router"] == "1" {
		command = append(command, "--router", "--models-max", runtimeReport["models_max"])
	} else if preset != nil {
		command = append(command, "--preset", preset.ID)
	} else {
		command = append(command, "--model", "GGUF_PATH")
	}
	command = append(command, "--backend", runtimeReport["backend"], "--profile", runtimeReport["profile"])
	for _, node := range strings.Split(environment["ROCMLETE_RENDER_NODES"], ",") {
		if node != "" {
			command = append(command, "--render-node", node)
		}
	}
	command = append(command, "--listen", runtimeReport["host_listen"], "--port", runtimeReport["port"])
	if runtimeReport["context"] != "" {
		command = append(command, "--context", runtimeReport["context"])
	} else if preset != nil && runtimeReport["router"] == "0" {
		command = append(command, "--context", strconv.FormatInt(preset.DefaultContext, 10))
	}
	if runtimeReport["api_key"] == "1" {
		command = append(command, "--api-key-file", "API_KEY_FILE")
	}
	return command
}

func writeStatusSection(output io.Writer, title string, rows [][2]string) {
	fmt.Fprintf(output, "\n%s\n", title)
	writeStatusRows(output, rows)
}

func writeStatusRows(output io.Writer, rows [][2]string) {
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	for _, row := range rows {
		fmt.Fprintf(output, "  %-*s  %s\n", width, row[0], row[1])
	}
}

func onOff(value string) string {
	if value == "1" {
		return "on"
	}
	return "off"
}
