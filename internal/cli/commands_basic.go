package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"rocmplete/internal/buildplan"
	"rocmplete/internal/config"
	"rocmplete/internal/content"
	"rocmplete/internal/controlerr"
	"rocmplete/internal/platform"
	"rocmplete/internal/process"
	"rocmplete/internal/runtime"
	"rocmplete/internal/storage"
)

func (app *App) commandBuild(args []string) error {
	set := app.flags("build", "Usage: ./rocmplete build TARGET [--no-layer-cache | --no-cache] [--image TAG]")
	noLayerCache := set.Bool("no-layer-cache", false, "rebuild selected image layers")
	noCache := set.Bool("no-cache", false, "cold-build selected images and prerequisites")
	image := set.String("image", "", "override the selected image tag")
	target, args := leadingPositional(args)
	if err := set.Parse(args); err != nil {
		return err
	}
	if target == "" && len(set.Args()) > 0 {
		target = set.Args()[0]
	}
	if target == "" {
		return controlerr.Usage("choose an image target")
	}
	if err := requireChoice(target, "build target", "all", "base", "content-tools", "comfyui", "llama-cpp", "dwarfstar"); err != nil {
		return err
	}
	if *noLayerCache && *noCache {
		return controlerr.Usage("--no-layer-cache and --no-cache are mutually exclusive")
	}
	if *image != "" && target == "all" {
		return controlerr.Usage("--image requires one build target, not all")
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	volumeSuffix := ":rw"
	pipCache := ""
	if !*noCache {
		var err error
		pipCache, err = buildplan.PreparePipCache(app.Environment)
		if err != nil {
			return err
		}
		volumeSuffix = app.podman().SELinuxVolumeSuffix(app.Context)
	}
	selectedNoCache := *noLayerCache || *noCache
	build := func(label, target, tag, base, runtimeImage string, discardLayers bool) error {
		command, err := buildplan.Command(buildplan.Options{ProjectRoot: app.Root, Image: tag, Target: target, BaseImage: base, RuntimeImage: runtimeImage, PipCache: pipCache, VolumeSuffix: volumeSuffix, NoLayerCache: discardLayers})
		if err != nil {
			return err
		}
		fmt.Fprintf(app.Stdout, "Building %s (%s)\n", tag, label)
		_, err = app.run(command, false)
		return err
	}
	if target == "content-tools" {
		tag := config.ContentToolsImage
		if *image != "" {
			tag = *image
		}
		if err := build("content-tools", config.ContentToolsTarget, tag, "", "", selectedNoCache); err != nil {
			return err
		}
		fmt.Fprintf(app.Stdout, "Built content-tools: %s\n", tag)
		return nil
	}
	if target == "base" {
		baseTag := config.ROCmBaseImage
		if *image != "" {
			baseTag = *image
		}
		if err := build("runtime", config.ROCmRuntimeBuildTarget, config.ROCmRuntimeImage, "", "", selectedNoCache); err != nil {
			return err
		}
		if err := build("base", config.ROCmBaseBuildTarget, baseTag, "", config.ROCmRuntimeImage, selectedNoCache); err != nil {
			return err
		}
		fmt.Fprintf(app.Stdout, "Built runtime: %s\nBuilt base: %s\n", config.ROCmRuntimeImage, baseTag)
		return nil
	}
	prerequisiteNoCache := *noCache
	if err := build("content", config.ContentToolsTarget, config.ContentToolsImage, "", "", prerequisiteNoCache); err != nil {
		return err
	}
	if err := build("runtime", config.ROCmRuntimeBuildTarget, config.ROCmRuntimeImage, "", "", prerequisiteNoCache); err != nil {
		return err
	}
	targets := []string{target}
	if target == "all" {
		targets = []string{"comfyui", "llama-cpp", "dwarfstar"}
	}
	needsPyTorch := false
	for _, identifier := range targets {
		spec, _ := config.ApplicationByID(identifier)
		needsPyTorch = needsPyTorch || spec.SharedPyTorchBase
	}
	if needsPyTorch {
		if err := build("base", config.ROCmBaseBuildTarget, config.ROCmBaseImage, "", config.ROCmRuntimeImage, prerequisiteNoCache); err != nil {
			return err
		}
	}
	for _, identifier := range targets {
		spec, _ := config.ApplicationByID(identifier)
		tag := spec.Image
		if *image != "" {
			tag = *image
		}
		base := ""
		runtimeImage := config.ROCmRuntimeImage
		if spec.SharedPyTorchBase {
			base = config.ROCmBaseImage
			runtimeImage = ""
		}
		if err := build(identifier, spec.BuildTarget, tag, base, runtimeImage, selectedNoCache); err != nil {
			return err
		}
		fmt.Fprintf(app.Stdout, "Built %s: %s\n", identifier, tag)
	}
	return nil
}

func (app *App) commandGuide(args []string) error {
	set := app.flags("guide", "Usage: ./rocmplete guide [comfyui|llama-cpp|dwarfstar]")
	application, args := leadingPositional(args)
	if err := set.Parse(args); err != nil {
		return err
	}
	if application == "" && len(set.Args()) > 0 {
		application = set.Args()[0]
	}
	if application == "" {
		fmt.Fprintln(app.Stdout, "Applications:\n  comfyui     image and video workflows\n  llama-cpp   local GGUF inference and router\n  dwarfstar   experimental DeepSeek V4 Flash inference")
		return nil
	}
	if err := requireChoice(application, "application", "comfyui", "llama-cpp", "dwarfstar"); err != nil {
		return err
	}
	spec, _ := config.ApplicationByID(application)
	fmt.Fprintf(app.Stdout, "%s\n\n  Build:   ./rocmplete build %s\n  Content: %s\n  Run:     %s\n  Logs:    ./rocmplete logs %s\n  Stop:    ./rocmplete stop %s\n", spec.ID, spec.ID, spec.AfterBuild, spec.AfterContent, spec.ID, spec.ID)
	return nil
}

func (app *App) commandStatus(args []string) error {
	set := app.flags("status", "Usage: ./rocmplete status [llama-cpp] [--model PRESET] [--data-dir PATH]")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	model := set.String("model", "", "managed llama.cpp preset")
	application, args := leadingPositional(args)
	if err := set.Parse(args); err != nil {
		return err
	}
	if application == "" && len(set.Args()) > 0 {
		application = set.Args()[0]
	}
	if application != "" && application != "llama-cpp" {
		return controlerr.Usage("status application must be llama-cpp")
	}
	if *model != "" && application != "llama-cpp" {
		return controlerr.Usage("--model requires 'status llama-cpp'")
	}
	if application == "llama-cpp" && *dataFlag != "" {
		return controlerr.Usage("--data-dir is only valid for the normal status view")
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	if application == "llama-cpp" {
		return app.llamaStatus(*model)
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	free := uint64(0)
	probe := nearestExisting(dataRoot)
	var stat syscall.Statfs_t
	if probe != "" && syscall.Statfs(probe, &stat) == nil {
		free = stat.Bavail * uint64(stat.Bsize)
	}
	state := "missing"
	if status, statErr := os.Stat(dataRoot); statErr == nil && status.IsDir() {
		state = "ready"
	}
	fmt.Fprintf(app.Stdout, "Persistent data\n  %-12s %s (%s free)\n\n", state, dataRoot, humanSize(int64(free)))
	fmt.Fprintln(app.Stdout, "GPU devices")
	devices := []string{"/dev/kfd"}
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	sort.Strings(nodes)
	devices = append(devices, nodes...)
	for _, device := range devices {
		deviceState := "read/write"
		if err := platform.CheckDeviceAccess(device); err != nil {
			if os.IsNotExist(errors.Unwrap(err)) || strings.Contains(err.Error(), "does not exist") {
				deviceState = "missing"
			} else {
				deviceState = "no access"
			}
		}
		fmt.Fprintf(app.Stdout, "  %-12s %s\n", deviceState, device)
	}
	fmt.Fprintln(app.Stdout, "\nImages")
	images := []struct{ label, image string }{{"content", config.ContentToolsImage}, {"runtime", config.ROCmRuntimeImage}, {"base", config.ROCmBaseImage}}
	for _, application := range config.Applications() {
		images = append(images, struct{ label, image string }{application.ID, application.Image})
	}
	for _, item := range images {
		present, existsErr := app.podman().Exists(app.Context, "image", item.image)
		if existsErr != nil {
			return existsErr
		}
		imageState := "missing"
		if present {
			imageState = "ready"
		}
		fmt.Fprintf(app.Stdout, "  %-12s %-12s %s\n", imageState, item.label, item.image)
	}
	fmt.Fprintln(app.Stdout, "\nManaged containers")
	for _, application := range config.Applications() {
		present, existsErr := app.podman().Exists(app.Context, "container", application.ContainerName)
		if existsErr != nil {
			return existsErr
		}
		containerState := "absent"
		if present {
			containerState, existsErr = app.podman().Capture(app.Context, []string{"inspect", "--format", "{{.State.Status}}", application.ContainerName}, "cannot inspect container "+application.ContainerName)
			if existsErr != nil {
				return existsErr
			}
		}
		fmt.Fprintf(app.Stdout, "  %-12s %-12s %s\n", containerState, application.ID, application.ContainerName)
	}
	return nil
}

func (app *App) commandRun(args []string) error {
	if groupHelpRequested(args) {
		writeGroupHelp(app.Stdout, "Usage: ./rocmplete run APPLICATION [MODE] [OPTIONS]",
			[2]string{"comfyui", "run the ComfyUI web application"},
			[2]string{"llama-cpp", "run llama.cpp server or cli mode"},
			[2]string{"dwarfstar", "run DwarfStar server or cli mode"})
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return controlerr.Usage("choose an application")
	}
	application := args[0]
	switch application {
	case "comfyui":
		return app.runComfyUI(args[1:])
	case "llama-cpp":
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return controlerr.Usage("choose llama.cpp mode server or cli")
		}
		return app.runLlama(args[1], args[2:])
	case "dwarfstar":
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return controlerr.Usage("choose DwarfStar mode server or cli")
		}
		return app.runDwarfStar(args[1], args[2:])
	default:
		return controlerr.Usage("unknown application %q", application)
	}
}

func (app *App) runComfyUI(args []string) error {
	set := app.flags("run comfyui", "Usage: ./rocmplete run comfyui [OPTIONS] [-- COMFYUI-ARGS]")
	profile := set.String("profile", "", "execution profile")
	listen := set.String("listen", "", "host publication address")
	portText := set.String("port", "", "host port")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact GPU render node; repeatable")
	detach := set.Bool("detach", false, "run in background")
	dryRun := set.Bool("dry-run", false, "print resolved command")
	unconfined := set.Bool("unconfined", false, "disable seccomp")
	disableExtensions := set.Bool("disable-bundled-extensions", false, "disable bundled extensions")
	memoryPolicy := set.String("memory-policy", "", "balanced or conservative")
	image := set.String("image", "", "override image")
	kernelPolicy := set.String("kernel-policy", "", "default or experimental")
	if err := set.Parse(args); err != nil {
		return err
	}
	profileValue := firstNonEmpty(*profile, config.EnvironmentValue(app.Environment, "PROFILE", "auto"))
	if err := platform.ValidateProfile(profileValue); err != nil {
		return controlerr.Usage("%v", err)
	}
	listenValue := firstNonEmpty(*listen, config.EnvironmentValue(app.Environment, "LISTEN", config.DefaultListen))
	if err := config.ValidateListenAddress(listenValue); err != nil {
		return err
	}
	application, _ := config.ApplicationByID("comfyui")
	portValue, err := config.ValidatePort(firstNonEmpty(*portText, config.EnvironmentValue(app.Environment, "PORT", fmt.Sprint(application.Port))))
	if err != nil {
		return err
	}
	memoryValue := firstNonEmpty(*memoryPolicy, config.EnvironmentValue(app.Environment, "MEMORY_POLICY", "balanced"))
	if err := requireChoice(memoryValue, "memory policy", "balanced", "conservative"); err != nil {
		return err
	}
	kernelValue := firstNonEmpty(*kernelPolicy, config.EnvironmentValue(app.Environment, "KERNEL_POLICY", "default"))
	if err := requireChoice(kernelValue, "kernel policy", "default", "experimental"); err != nil {
		return err
	}
	selected, err := app.resolveDevices(profileValue, nodes, nodes != nil)
	if err != nil {
		return err
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, !*dryRun)
	if err != nil {
		return err
	}
	if !*dryRun {
		if err := (storage.Layout{Root: dataRoot}).PrepareRuntime("comfyui"); err != nil {
			return err
		}
	}
	for _, argument := range set.Args() {
		for _, managed := range []string{"--listen", "--port", "--base-directory", "--models-directory", "--input-directory", "--output-directory", "--temp-directory", "--user-directory", "--database-url", "--cpu"} {
			if argument == managed || strings.HasPrefix(argument, managed+"=") {
				return controlerr.Usage("ComfyUI argument %s is managed by the launcher", managed)
			}
		}
	}
	imageValue := firstNonEmpty(*image, config.EnvironmentValue(app.Environment, "IMAGE", application.Image))
	command := runtime.WebCommand(runtime.WebOptions{Image: imageValue, Profile: profileValue, Listen: listenValue, Port: portValue, DataDir: dataRoot, RenderNodes: selected, Detach: *detach, Unconfined: *unconfined, DisableBundledExtensions: *disableExtensions, Arguments: set.Args(), ContainerName: application.ContainerName, Application: "comfyui", MemoryPolicy: memoryValue, KernelPolicy: kernelValue, Publish: true}, app.podman().SELinuxVolumeSuffix(app.Context))
	if !isLoopback(listenValue) {
		fmt.Fprintf(app.Stderr, "WARNING: ComfyUI is published on %s:%d without authentication.\n", listenValue, portValue)
	}
	fmt.Fprintf(app.Stdout, "Application data: %s\n", (storage.Layout{Root: dataRoot}).Application("comfyui"))
	if *dryRun {
		fmt.Fprintf(app.Stdout, "Resolved command:\n  %s\n", shellJoin(command))
		return nil
	}
	return app.startManaged(application, imageValue, command, *detach)
}

func (app *App) runLlama(mode string, args []string) error {
	if err := requireChoice(mode, "llama.cpp mode", "server", "cli"); err != nil {
		return err
	}
	set := app.flags("run llama-cpp "+mode, "Usage: ./rocmplete run llama-cpp "+mode+" (--model FILE | --preset NAME | --router) [OPTIONS]")
	model := set.String("model", "", "exact local GGUF")
	presetID := set.String("preset", "", "installed catalog preset")
	routerMode := set.Bool("router", false, "serve installed presets")
	profileFlag := set.String("profile", "", "execution profile")
	backend := set.String("backend", "rocm", "rocm or vulkan")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact GPU render node; repeatable")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override image")
	contextSize := set.Int64("context", -1, "context size")
	unconfined := set.Bool("unconfined", false, "disable seccomp")
	dryRun := set.Bool("dry-run", false, "print resolved command")
	listen := set.String("listen", "", "host publication address")
	portText := set.String("port", "", "host port")
	detach := set.Bool("detach", false, "run in background")
	apiKey := set.String("api-key-file", "", "read-only API key file")
	modelsMax := set.Int("models-max", 0, "router simultaneous models")
	prompt := optionalString{}
	set.Var(&prompt, "prompt", "single prompt")
	if err := set.Parse(args); err != nil {
		return err
	}
	if mode != "server" && *routerMode {
		return controlerr.Usage("--router is only valid for server mode")
	}
	selectedSources := boolCount(*model != "", *presetID != "", *routerMode)
	if selectedSources != 1 {
		return controlerr.Usage("choose exactly one of --model, --preset, or --router")
	}
	if err := requireChoice(*backend, "backend", "rocm", "vulkan"); err != nil {
		return err
	}
	if *contextSize < -1 {
		return controlerr.Usage("--context must be zero or positive")
	}
	if *modelsMax != 0 && !*routerMode {
		return controlerr.Usage("--models-max is only valid with --router")
	}
	modelsMaxValue := *modelsMax
	if modelsMaxValue == 0 {
		modelsMaxValue = 2
	}
	if modelsMaxValue < 1 {
		return controlerr.Usage("--models-max must be at least 1")
	}
	profileValue := firstNonEmpty(*profileFlag, config.EnvironmentValue(app.Environment, "PROFILE", "auto"))
	if err := platform.ValidateProfile(profileValue); err != nil {
		return controlerr.Usage("%v", err)
	}
	selectedNodes, err := app.resolveDevices(profileValue, nodes, nodes != nil)
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
	options := runtime.LlamaOptions{Profile: profileValue, Mode: mode, DataDir: dataRoot, Backend: *backend, ModelsMax: modelsMaxValue, RenderNodes: selectedNodes, Listen: firstNonEmpty(*listen, config.EnvironmentValue(app.Environment, "LISTEN", config.DefaultListen)), AutoRemove: true, Unconfined: *unconfined, Detach: *detach}
	if err := config.ValidateListenAddress(options.Listen); err != nil {
		return err
	}
	application, _ := config.ApplicationByID("llama-cpp")
	options.Port, err = config.ValidatePort(firstNonEmpty(*portText, config.EnvironmentValue(app.Environment, "PORT", fmt.Sprint(application.Port))))
	if err != nil {
		return err
	}
	options.Image = firstNonEmpty(*imageFlag, application.Image)
	options.SourceRevision = app.projectRevision()
	if *contextSize >= 0 {
		options.Context = *contextSize
	}
	displayModel := ""
	if *model != "" {
		resolved, resolveErr := regularFile(*model, "GGUF model")
		if resolveErr != nil {
			return resolveErr
		}
		if strings.ToLower(filepath.Ext(resolved)) != ".gguf" {
			return controlerr.Usage("--model must name a .gguf file")
		}
		options.Model = resolved
		displayModel = resolved
	}
	if *presetID != "" {
		preset, ok := managed.LlamaPresets[*presetID]
		if !ok {
			return controlerr.Usage("unknown llama.cpp preset %q", *presetID)
		}
		bundle := managed.Bundles[preset.Bundle]
		if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
			return controlerr.New("preset %q is not installed: %v\n  Install content: ./rocmplete content install %s", *presetID, err, preset.Bundle)
		}
		artifact := managed.Artifacts[preset.Artifact]
		options.ManagedModel = artifact.Destination
		if preset.DraftArtifact != "" {
			options.ManagedDraft = managed.Artifacts[preset.DraftArtifact].Destination
		}
		options.SpeculativeType = preset.SpeculativeType
		options.DraftTokens = preset.DraftTokensForBackend(*backend)
		options.ContextOverrideArchitectures = preset.ContextOverrideArchitectures
		options.Jinja = preset.Jinja
		options.ReasoningPreserve = preset.ReasoningPreserve
		options.ChatTemplate = preset.ChatTemplate
		options.ProfileFlashAttention = preset.FlashAttention
		options.ProfileKVCache = preset.KVCache
		if preset.SamplingPolicy != "" {
			policy := managed.SamplingPolicies[preset.SamplingPolicy]
			options.SamplingDefaults = map[string]any{"thinking": policy.Thinking, "non_thinking": policy.NonThinking}
		}
		if *contextSize == -1 {
			options.Context = preset.DefaultContext
		}
		displayModel = fmt.Sprintf("%s (%s)", *presetID, content.ArtifactPath(dataRoot, artifact))
	}
	if *routerMode {
		contents, installed, renderErr := runtime.RenderRouter(managed, dataRoot, *backend)
		if renderErr != nil {
			return renderErr
		}
		if *dryRun {
			options.RouterPreset = filepath.Join((storage.Layout{Root: dataRoot}).Application("llama-cpp"), "models.ini")
		} else {
			options.RouterPreset, err = runtime.WriteRouter(dataRoot, contents)
			if err != nil {
				return err
			}
		}
		displayModel = "router: " + strings.Join(installed, ", ")
	}
	if *apiKey != "" {
		options.APIKeyFile, err = regularFile(*apiKey, "API-key file")
		if err != nil {
			return err
		}
	}
	if prompt.set {
		options.Prompt = &prompt.value
	}
	options.Interactive = mode == "cli" && !prompt.set
	if options.Interactive && !*dryRun && !terminalReader(app.Stdin) {
		return controlerr.New("interactive llama.cpp CLI requires a terminal; pass --prompt")
	}
	command, err := runtime.LlamaCommand(options, app.podman().SELinuxVolumeSuffix(app.Context))
	if err != nil {
		return err
	}
	if mode == "server" && !isLoopback(options.Listen) && options.APIKeyFile == "" {
		fmt.Fprintf(app.Stderr, "WARNING: llama.cpp is published on %s:%d without authentication.\n", options.Listen, options.Port)
	}
	fmt.Fprintf(app.Stdout, "Application data: %s\nBackend: %s\nModel: %s\n", (storage.Layout{Root: dataRoot}).Application("llama-cpp"), options.Backend, displayModel)
	if *dryRun {
		fmt.Fprintf(app.Stdout, "Resolved command:\n  %s\n", shellJoin(command))
		return nil
	}
	return app.startManaged(application, options.Image, command, *detach)
}

func (app *App) runDwarfStar(mode string, args []string) error {
	if err := requireChoice(mode, "DwarfStar mode", "server", "cli"); err != nil {
		return err
	}
	set := app.flags("run dwarfstar "+mode, "Usage: ./rocmplete run dwarfstar "+mode+" [OPTIONS]")
	modelFlag := set.String("model", "", "exact local GGUF")
	profileFlag := set.String("profile", "", "execution profile")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact render node")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override image")
	contextSize := set.Int64("context", 131072, "context tokens")
	outputTokens := set.Int64("output-tokens", 16000, "maximum output tokens")
	dspark := set.Bool("dspark", false, "enable managed DSpark pair")
	unconfined := set.Bool("unconfined", false, "disable seccomp")
	dryRun := set.Bool("dry-run", false, "print resolved command")
	listen := set.String("listen", "", "host publication address")
	portText := set.String("port", "", "host port")
	detach := set.Bool("detach", false, "run in background")
	prompt := optionalString{}
	set.Var(&prompt, "prompt", "single prompt")
	noThinking := set.Bool("no-thinking", false, "disable thinking")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *contextSize < 4096 || *contextSize > 1048576 {
		return controlerr.Usage("--context must be between 4096 and 1048576")
	}
	if *outputTokens < 1 || *outputTokens >= *contextSize {
		return controlerr.Usage("--output-tokens must be positive and smaller than --context")
	}
	profileValue := firstNonEmpty(*profileFlag, config.EnvironmentValue(app.Environment, "PROFILE", "auto"))
	if profileValue == "cpu" {
		return controlerr.Usage("DwarfStar requires a supported GPU profile")
	}
	if err := platform.ValidateProfile(profileValue); err != nil {
		return err
	}
	selectedNodes, err := app.resolveDevices(profileValue, nodes, nodes != nil)
	if err != nil {
		return err
	}
	if len(selectedNodes) != 1 {
		return controlerr.Usage("DwarfStar requires exactly one render node")
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, !*dryRun)
	if err != nil {
		return err
	}
	if !*dryRun {
		if err := (storage.Layout{Root: dataRoot}).PrepareRuntime("dwarfstar"); err != nil {
			return err
		}
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	model := ""
	support := ""
	if *dspark && *modelFlag != "" {
		return controlerr.Usage("--dspark cannot be combined with --model")
	}
	if *modelFlag != "" {
		model, err = regularFile(*modelFlag, "DwarfStar GGUF model")
		if err != nil {
			return err
		}
	} else {
		bundleID := "dwarfstar-deepseek-v4-flash-0731-q2-imatrix"
		if *dspark {
			bundleID = "dwarfstar-deepseek-v4-flash-0731-q2-imatrix-dspark"
		}
		bundle, ok := managed.Bundles[bundleID]
		if !ok {
			return fmt.Errorf("catalog lacks DwarfStar bundle %s", bundleID)
		}
		if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
			return controlerr.New("DwarfStar model is not installed: %v", err)
		}
		model = content.ArtifactPath(dataRoot, managed.Artifacts[bundle.Artifacts[0]])
		if *dspark && len(bundle.Artifacts) == 2 {
			support = content.ArtifactPath(dataRoot, managed.Artifacts[bundle.Artifacts[1]])
		}
	}
	application, _ := config.ApplicationByID("dwarfstar")
	options := runtime.DwarfStarOptions{Image: firstNonEmpty(*imageFlag, application.Image), Mode: mode, DataDir: dataRoot, Model: model, SupportModel: support, DSpark: *dspark, RenderNodes: selectedNodes, Profile: profileValue, Listen: firstNonEmpty(*listen, config.EnvironmentValue(app.Environment, "LISTEN", config.DefaultListen)), Context: *contextSize, OutputTokens: *outputTokens, NoThinking: *noThinking, Detach: *detach, Unconfined: *unconfined, Interactive: mode == "cli" && !prompt.set}
	if prompt.set {
		options.Prompt = &prompt.value
	}
	if err := config.ValidateListenAddress(options.Listen); err != nil {
		return err
	}
	options.Port, err = config.ValidatePort(firstNonEmpty(*portText, config.EnvironmentValue(app.Environment, "PORT", fmt.Sprint(application.Port))))
	if err != nil {
		return err
	}
	if options.Interactive && !*dryRun && !terminalReader(app.Stdin) {
		return controlerr.New("interactive DwarfStar CLI requires a terminal; pass --prompt")
	}
	command, err := runtime.DwarfStarCommand(options, app.podman().SELinuxVolumeSuffix(app.Context))
	if err != nil {
		return err
	}
	if mode == "server" && !isLoopback(options.Listen) {
		fmt.Fprintf(app.Stderr, "WARNING: DwarfStar is published on %s:%d without authentication.\n", options.Listen, options.Port)
	}
	fmt.Fprintf(app.Stdout, "Application data: %s\nModel: %s\n", (storage.Layout{Root: dataRoot}).Application("dwarfstar"), model)
	if *dryRun {
		fmt.Fprintf(app.Stdout, "Resolved command:\n  %s\n", shellJoin(command))
		return nil
	}
	return app.startManaged(application, options.Image, command, *detach)
}

func (app *App) commandShell(args []string) error {
	set := app.flags("shell", "Usage: ./rocmplete shell APPLICATION [--data-dir PATH] [--image TAG]")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override image")
	applicationID, args := leadingPositional(args)
	if err := set.Parse(args); err != nil {
		return err
	}
	if applicationID == "" {
		return controlerr.Usage("choose an application")
	}
	application, ok := config.ApplicationByID(applicationID)
	if !ok || !application.Shell {
		return controlerr.Usage("unknown shell application %q", applicationID)
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, true)
	if err != nil {
		return err
	}
	if err := (storage.Layout{Root: dataRoot}).PrepareRuntime(applicationID); err != nil {
		return err
	}
	image := firstNonEmpty(*imageFlag, config.EnvironmentValue(app.Environment, "IMAGE", application.Image))
	present, err := app.podman().Exists(app.Context, "image", image)
	if err != nil {
		return err
	}
	if !present {
		return controlerr.New("image not found: %s", image)
	}
	_, err = app.run(runtime.ShellCommand(image, dataRoot, app.podman().SELinuxVolumeSuffix(app.Context), applicationID), false)
	return err
}

func (app *App) commandLogs(args []string) error {
	set := app.flags("logs", "Usage: ./rocmplete logs APPLICATION [--follow] [--tail N | --all]")
	follow := set.Bool("follow", false, "follow output")
	tail := set.Int("tail", 200, "recent lines")
	all := set.Bool("all", false, "show complete logs")
	applicationID, args := leadingPositional(args)
	if err := set.Parse(args); err != nil {
		return err
	}
	if applicationID == "" {
		return controlerr.Usage("choose an application")
	}
	application, ok := config.ApplicationByID(applicationID)
	if !ok || !application.Logs {
		return controlerr.Usage("unknown log application %q", applicationID)
	}
	if *all && setWasSet(set, "tail") {
		return controlerr.Usage("--all and --tail are mutually exclusive")
	}
	if !*all && *tail < 1 {
		return controlerr.Usage("--tail must be at least 1")
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	present, err := app.podman().Exists(app.Context, "container", application.ContainerName)
	if err != nil {
		return err
	}
	if !present {
		return controlerr.New("container %q does not exist", application.ContainerName)
	}
	command := []string{"podman", "logs"}
	if *follow {
		command = append(command, "--follow")
	}
	if !*all {
		command = append(command, "--tail", fmt.Sprint(*tail))
	}
	command = append(command, application.ContainerName)
	_, err = app.run(command, false)
	return err
}

func (app *App) commandStop(args []string) error {
	set := app.flags("stop", "Usage: ./rocmplete stop APPLICATION|all")
	applicationID, args := leadingPositional(args)
	if err := set.Parse(args); err != nil {
		return err
	}
	if applicationID == "" {
		return controlerr.Usage("choose an application")
	}
	if err := requireChoice(applicationID, "application", "comfyui", "llama-cpp", "dwarfstar", "all"); err != nil {
		return err
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	targets := []string{applicationID}
	if applicationID == "all" {
		targets = []string{"comfyui", "llama-cpp", "dwarfstar"}
	}
	for _, identifier := range targets {
		application, _ := config.ApplicationByID(identifier)
		present, err := app.podman().Exists(app.Context, "container", application.ContainerName)
		if err != nil {
			return err
		}
		if !present {
			fmt.Fprintf(app.Stdout, "Container not present: %s\n", application.ContainerName)
			continue
		}
		if _, err := app.run([]string{"podman", "rm", "--force", "--time", "2", "--ignore", application.ContainerName}, false); err != nil {
			return err
		}
		fmt.Fprintf(app.Stdout, "Removed container: %s\n", application.ContainerName)
	}
	return nil
}

func (app *App) startManaged(application config.Application, image string, command []string, detach bool) error {
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	present, err := app.podman().Exists(app.Context, "image", image)
	if err != nil {
		return err
	}
	if !present {
		return controlerr.New("image not found: %s\n  Build image: ./rocmplete build %s", image, application.ID)
	}
	exists, err := app.podman().Exists(app.Context, "container", application.ContainerName)
	if err != nil {
		return err
	}
	if exists {
		return controlerr.New("container %q already exists; use logs or stop", application.ContainerName)
	}
	if application.Port != 0 {
		fmt.Fprintf(app.Stdout, "Logs: ./rocmplete logs %s\nStop: ./rocmplete stop %s\n", application.ID, application.ID)
	}
	_, runErr := app.run(command, false)
	if runErr != nil && !detach {
		_, _ = app.Runner.Run(contextWithoutCancel(), processCommand("podman", "rm", "--force", "--time", "1", "--ignore", application.ContainerName))
		if app.Context.Err() != nil {
			return app.Context.Err()
		}
	}
	return runErr
}

func (app *App) resolveDevices(profile string, requested []string, explicit bool) ([]string, error) {
	if profile == "cpu" {
		return nil, nil
	}
	values, err := platform.RequestedRenderNodes(requested, explicit, app.Environment)
	if err != nil {
		return nil, err
	}
	selected, err := platform.SelectRenderNodes(values)
	if err != nil {
		return nil, err
	}
	if err := platform.CheckAMDDeviceAccess(selected); err != nil {
		return nil, err
	}
	if err := app.podman().RequireContainerDeviceAccess(app.Context); err != nil {
		return nil, err
	}
	return selected, nil
}

func (app *App) resolveDataDir(value string, prepare bool) (string, error) {
	selected, err := config.SelectDataDir(value, app.Environment)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(selected, "~/") {
		selected = filepath.Join(app.Environment["HOME"], selected[2:])
	}
	selected, err = filepath.Abs(selected)
	if err != nil {
		return "", err
	}
	if prepare {
		if err := os.MkdirAll(selected, 0o755); err != nil {
			return "", fmt.Errorf("prepare data directory %s: %w", selected, err)
		}
		return filepath.EvalSymlinks(selected)
	}
	return filepath.Clean(selected), nil
}

func (app *App) projectRevision() string {
	result, err := app.Runner.Run(app.Context, processCommand("git", "-C", app.Root, "rev-parse", "HEAD"))
	if err != nil || result.Status != 0 {
		return "unavailable"
	}
	revision := strings.TrimSpace(string(result.Stdout))
	if len(revision) != 40 || strings.Trim(revision, "0123456789abcdef") != "" {
		return "unavailable"
	}
	changes, changeErr := app.Runner.Run(app.Context, processCommand("git", "-C", app.Root, "status", "--porcelain"))
	if changeErr == nil && changes.Status == 0 && strings.TrimSpace(string(changes.Stdout)) != "" {
		return revision + " (dirty)"
	}
	return revision
}

func leadingPositional(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

type optionalString struct {
	value string
	set   bool
}

func (value *optionalString) String() string { return value.value }
func (value *optionalString) Set(raw string) error {
	value.value, value.set = raw, true
	return nil
}

func terminalReader(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	status, err := file.Stat()
	return err == nil && status.Mode()&os.ModeCharDevice != 0
}

func regularFile(value, description string) (string, error) {
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			value = filepath.Join(home, value[2:])
		}
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", controlerr.New("cannot resolve %s: %v", description, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	status, err := os.Stat(resolved)
	if err != nil || !status.Mode().IsRegular() {
		return "", controlerr.New("%s is not a regular file: %s", description, resolved)
	}
	return resolved, nil
}

func isLoopback(address string) bool {
	ip := net.ParseIP(address)
	return ip != nil && ip.IsLoopback()
}

var safeShellWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellJoin(command []string) string {
	quoted := make([]string, len(command))
	for index, argument := range command {
		if argument != "" && safeShellWord.MatchString(argument) {
			quoted[index] = argument
		} else {
			quoted[index] = "'" + strings.ReplaceAll(argument, "'", "'\\''") + "'"
		}
	}
	return strings.Join(quoted, " ")
}

func humanSize(value int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	number := float64(value)
	unit := 0
	for number >= 1024 && unit < len(units)-1 {
		number /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", number, units[unit])
}

func nearestExisting(value string) string {
	current := value
	for {
		if _, err := os.Stat(current); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func setWasSet(set *flag.FlagSet, name string) bool {
	found := false
	set.Visit(func(item *flag.Flag) {
		if item.Name == name {
			found = true
		}
	})
	return found
}

func processCommand(name string, arguments ...string) process.Command {
	return process.Command{Name: name, Args: arguments}
}

func contextWithoutCancel() context.Context { return context.Background() }
