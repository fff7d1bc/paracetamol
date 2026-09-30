package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"paracetamol/internal/application"
	"paracetamol/internal/buildplan"
	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/gateway"
	"paracetamol/internal/identity"
	"paracetamol/internal/platform"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/runtime"
	"paracetamol/internal/storage"
	"paracetamol/internal/ui"
)

func (app *App) commandBuild(args []string) error {
	set := app.flags("build", "Usage: "+identity.Command("build", "[TARGET]", "[--no-layer-cache | --no-cache]", "[--image TAG]"))
	set.Argument("TARGET", "all, runtime, pytorch-base, content-tools, comfyui, llama-cpp, or dwarfstar")
	noLayerCache := set.Bool("no-layer-cache", false, "rebuild selected image layers")
	noCache := set.Bool("no-cache", false, "cold-build selected images and prerequisites")
	image := set.String("image", "", "override the selected image tag")
	target, args := leadingPositional(args)
	if err := parseFlags(set, args); err != nil {
		return err
	}
	positionals := set.Args()
	if target == "" && len(positionals) > 0 {
		target, positionals = positionals[0], positionals[1:]
	}
	if len(positionals) > 0 {
		return controlerr.Usage("build accepts exactly one target")
	}
	if target == "" {
		if !terminalReader(app.Stdin) {
			return set.usageError("choose an image target")
		}
		var err error
		target, err = app.guidedBuildTarget()
		if err != nil {
			return err
		}
	}
	targets := []application.BuildID{application.BuildID(target)}
	if target == "all" {
		targets = []application.BuildID{
			application.BuildContentTools,
			application.BuildComfyUI,
			application.BuildLlamaCPP,
			application.BuildDwarfStar,
		}
	}
	if *image != "" && target == "all" {
		return controlerr.Usage("--image requires one build target, not all")
	}
	if *noLayerCache && *noCache {
		return controlerr.Usage("--no-layer-cache and --no-cache are mutually exclusive")
	}
	if _, err := application.BuildClosure(targets); err != nil {
		return controlerr.Usage("%v", err)
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
	steps, err := buildplan.Plan(buildplan.Request{
		ProjectRoot: app.Root, Targets: targets, ImageOverride: *image,
		PipCache: pipCache, VolumeSuffix: volumeSuffix,
		NoLayerCache: *noLayerCache, NoCache: *noCache,
	})
	if err != nil {
		return controlerr.Usage("%v", err)
	}
	type outcome struct {
		step   buildplan.Step
		state  string
		reason string
	}
	outcomes := make([]outcome, 0, len(steps))
	states := make(map[application.BuildID]string, len(steps))
	failed := false
	terminal := app.terminal(app.Stdout)
	for _, step := range steps {
		blockedBy := ""
		for _, prerequisite := range step.Unit.Prerequisites {
			if states[prerequisite] != "built" {
				blockedBy = string(prerequisite)
				break
			}
		}
		if blockedBy != "" {
			states[step.Unit.ID] = "skipped"
			outcomes = append(outcomes, outcome{step: step, state: "skipped", reason: "prerequisite " + blockedBy + " did not build"})
			continue
		}
		fmt.Fprintf(app.Stdout, "%s %s (%s)\n", terminal.Heading("Building"), step.Unit.DisplayName, step.Image)
		if _, runErr := app.run(step.Command, false); runErr != nil {
			failed = true
			states[step.Unit.ID] = "failed"
			outcomes = append(outcomes, outcome{step: step, state: "failed", reason: runErr.Error()})
			continue
		}
		states[step.Unit.ID] = "built"
		outcomes = append(outcomes, outcome{step: step, state: "built"})
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Build summary"))
	rows := make([][]string, 0, len(outcomes))
	for _, result := range outcomes {
		detail := result.step.Image
		if result.reason != "" {
			detail += " (" + result.reason + ")"
		}
		rows = append(rows, []string{terminal.State(result.state), terminal.Command(string(result.step.Unit.ID)), detail})
	}
	lines, _ := ui.ColumnLines(rows, nil, "  ")
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	if failed {
		return &controlerr.Error{Message: "one or more image builds failed", Status: 1}
	}
	return nil
}

func (app *App) guidedBuildTarget() (string, error) {
	choices := []string{"all", string(application.BuildRuntime), string(application.BuildPyTorchBase), string(application.BuildContentTools)}
	for _, spec := range application.All() {
		choices = append(choices, spec.ID)
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Choose an image target:"))
	rows := make([][]string, 0, len(choices))
	for _, choice := range choices {
		rows = append(rows, []string{terminal.Command(choice)})
	}
	lines, _ := ui.NumberedLines(rows, nil)
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	answer, err := app.promptLine("Selection (or q to cancel): ", true)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(answer, "q") {
		return "", controlerr.New("build selection cancelled")
	}
	selected, err := strconv.Atoi(answer)
	if err != nil || selected < 1 || selected > len(choices) {
		return "", controlerr.Usage("build selection must be a number from 1 through %d", len(choices))
	}
	return choices[selected-1], nil
}

func (app *App) commandGuide(args []string) error {
	set := app.flags("guide", usage("guide", "[APPLICATION]"))
	set.Argument("APPLICATION", "comfyui, llama-cpp, or dwarfstar; omit to list all")
	application, args := leadingPositional(args)
	if err := parseFlags(set, args); err != nil {
		return err
	}
	positionals := set.Args()
	if application == "" && len(positionals) > 0 {
		application, positionals = positionals[0], positionals[1:]
	}
	if len(positionals) > 0 {
		return controlerr.Usage("guide accepts at most one application")
	}
	if application == "" {
		terminal := app.terminal(app.Stdout)
		fmt.Fprintln(app.Stdout, terminal.Heading("Applications:"))
		var rows [][]string
		for _, spec := range config.Applications() {
			rows = append(rows, []string{terminal.Command(spec.ID), spec.Summary})
		}
		lines, _ := ui.ColumnLines(rows, nil, "  ")
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
		}
		terminal.Next(identity.Command("guide", "APPLICATION"))
		return nil
	}
	spec, ok := config.ApplicationByID(application)
	if !ok {
		return controlerr.Usage("unknown application %q", application)
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s\n%s\n\n%s %s\n%s %d\n%s %s\n\n", terminal.Heading(spec.DisplayName), spec.Summary, terminal.Label("Managed image:"), spec.Image, terminal.Label("Default port:"), spec.Port, terminal.Label("Modes:"), strings.Join(spec.Modes, ", "))
	fmt.Fprintf(app.Stdout, "%s\n  %s   %s\n", terminal.Heading("Walkthrough"), terminal.Label("Build:"), terminal.Command(identity.Command("build", spec.ID)))
	for _, action := range spec.AfterBuild {
		fmt.Fprintf(app.Stdout, "  %s %s\n           %s\n", terminal.Label("Content:"), terminal.Command(action.Command()), terminal.Muted(action.Description))
	}
	for _, action := range spec.AfterContent {
		fmt.Fprintf(app.Stdout, "  %s     %s\n           %s\n", terminal.Label("Run:"), terminal.Command(action.Command()), terminal.Muted(action.Description))
	}
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label("Status:"), terminal.Command(identity.Command("status", spec.ID)))
	if spec.Logs {
		fmt.Fprintf(app.Stdout, "  %s    %s\n", terminal.Label("Logs:"), terminal.Command(identity.Command("logs", spec.ID)))
	}
	if spec.Shell {
		fmt.Fprintf(app.Stdout, "  %s   %s\n", terminal.Label("Shell:"), terminal.Command(identity.Command("shell", spec.ID)))
	}
	fmt.Fprintf(app.Stdout, "  %s    %s\n", terminal.Label("Stop:"), terminal.Command(identity.Command("stop", spec.ID)))
	return nil
}

func (app *App) commandStatus(args []string) error {
	set := app.flags("status", usage("status", "[APPLICATION]", "[OPTIONS]"))
	set.Argument("APPLICATION", "comfyui, llama-cpp, dwarfstar, or gateway; omit for the host dashboard")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	model := set.String("model", "", "managed llama.cpp preset")
	gatewayURL := set.String("gateway-url", "", "gateway OpenAI-compatible base URL")
	keyFile := set.String("gateway-api-key-file", "", "private gateway client Bearer-key file")
	requests := set.Int("requests", 0, "include 1-64 recent gateway requests")
	application, args := leadingPositional(args)
	if err := parseFlags(set, args); err != nil {
		return err
	}
	positionals := set.Args()
	if application == "" && len(positionals) > 0 {
		application, positionals = positionals[0], positionals[1:]
	}
	if len(positionals) > 0 {
		return controlerr.Usage("status accepts at most one application")
	}
	if application != "" && application != "gateway" {
		if _, ok := config.ApplicationByID(application); !ok {
			return controlerr.Usage("unknown status application %q", application)
		}
	}
	if application == "gateway" {
		if set.changed("gateway-api-key-file") && *keyFile == "" {
			return controlerr.Usage("--gateway-api-key-file must name a private key file")
		}
		if *model != "" || *dataFlag != "" {
			return controlerr.Usage("status gateway does not accept --model or --data-dir")
		}
		if set.changed("requests") && (*requests < 1 || *requests > gateway.RecentRequestLimit) {
			return controlerr.Usage("--requests must be from 1 through %d", gateway.RecentRequestLimit)
		}
		return app.gatewayStatus(*gatewayURL, *keyFile, *requests)
	}
	if *gatewayURL != "" {
		return controlerr.Usage("--gateway-url requires 'status gateway'")
	}
	if set.changed("gateway-api-key-file") {
		return controlerr.Usage("--gateway-api-key-file requires 'status gateway'")
	}
	if set.changed("requests") {
		return controlerr.Usage("--requests requires 'status gateway'")
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
	if application != "" {
		spec, _ := config.ApplicationByID(application)
		return app.applicationStatus(spec, *dataFlag)
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
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Persistent data"))
	dataRows, _ := ui.ColumnLines([][]string{{terminal.State(state), dataRoot, humanSize(int64(free)) + " free"}}, nil, "  ")
	fmt.Fprintln(app.Stdout, dataRows[0])
	fmt.Fprintln(app.Stdout, terminal.Heading("GPU devices"))
	devices := []string{"/dev/kfd"}
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	sort.Strings(nodes)
	devices = append(devices, nodes...)
	var deviceRows [][]string
	for _, device := range devices {
		deviceState := "read/write"
		if err := platform.CheckDeviceAccess(device); err != nil {
			if os.IsNotExist(errors.Unwrap(err)) || strings.Contains(err.Error(), "does not exist") {
				deviceState = "missing"
			} else {
				deviceState = "no access"
			}
		}
		deviceRows = append(deviceRows, []string{terminal.State(deviceState), device})
	}
	lines, _ := ui.ColumnLines(deviceRows, nil, "  ")
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Images"))
	images := []struct{ label, image string }{{"content", config.ContentToolsImage}, {"runtime", config.ROCmRuntimeImage}, {"base", config.ROCmBaseImage}}
	for _, application := range config.Applications() {
		images = append(images, struct{ label, image string }{application.ID, application.Image})
	}
	var imageRows [][]string
	for _, item := range images {
		present, existsErr := app.podman().Exists(app.Context, "image", item.image)
		if existsErr != nil {
			return existsErr
		}
		imageState := "missing"
		if present {
			imageState = "ready"
		}
		imageRows = append(imageRows, []string{terminal.State(imageState), terminal.Label(item.label), item.image})
	}
	lines, _ = ui.ColumnLines(imageRows, nil, "  ")
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Managed containers"))
	var containerRows [][]string
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
		containerRows = append(containerRows, []string{terminal.State(containerState), terminal.Command(application.ID), application.ContainerName})
	}
	lines, _ = ui.ColumnLines(containerRows, nil, "  ")
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	return nil
}

func (app *App) applicationStatus(spec config.Application, dataFlag string) error {
	dataRoot, err := app.resolveDataDir(dataFlag, false)
	if err != nil {
		return err
	}
	imageState := "missing"
	if present, inspectErr := app.podman().Exists(app.Context, "image", spec.Image); inspectErr != nil {
		return inspectErr
	} else if present {
		imageState = "ready"
	}
	containerState := "absent"
	if present, inspectErr := app.podman().Exists(app.Context, "container", spec.ContainerName); inspectErr != nil {
		return inspectErr
	} else if present {
		containerState, err = app.podman().Capture(app.Context, []string{"inspect", "--format", "{{.State.Status}}", spec.ContainerName}, "cannot inspect container "+spec.ContainerName)
		if err != nil {
			return err
		}
	}
	applicationData := (storage.Layout{Root: dataRoot}).Application(spec.ID)
	dataState := "missing"
	if status, statErr := os.Stat(applicationData); statErr == nil && status.IsDir() {
		dataState = "ready"
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	ready, total := 0, 0
	for _, bundle := range managed.Bundles {
		if bundle.Application != spec.ID {
			continue
		}
		total++
		if _, requireErr := content.RequireBundle(managed, bundle, dataRoot); requireErr == nil {
			ready++
		}
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s\n", terminal.Heading(spec.DisplayName))
	writeStatusRows(app.Stdout, terminal, [][2]string{
		{"Image", imageState + " — " + spec.Image},
		{"Container", containerState + " — " + spec.ContainerName},
		{"Data", dataState + " — " + applicationData},
		{"Content", fmt.Sprintf("%d/%d bundles ready", ready, total)},
	})
	return nil
}

func (app *App) commandRun(args []string) error {
	writeHelp := func() {
		commands := make([][2]string, 0, len(config.Applications())+1)
		commands = append(commands, [2]string{"gateway", "lazy one-port text inference across selected backends"})
		for _, spec := range config.Applications() {
			commands = append(commands, [2]string{spec.ID, spec.Summary})
		}
		app.writeGroupHelp(usage("run", "APPLICATION", "[MODE]", "[OPTIONS]"), commands...)
	}
	if groupHelpRequested(args) {
		writeHelp()
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		writeHelp()
		return controlerr.Usage("choose an application")
	}
	application := args[0]
	switch application {
	case "gateway":
		return app.runGateway(args[1:])
	case "comfyui":
		return app.runComfyUI(args[1:])
	case "llama-cpp":
		if groupHelpRequested(args[1:]) {
			app.writeGroupHelp(usage("run", "llama-cpp", "MODE", "[OPTIONS]"), [2]string{"server", "start the OpenAI-compatible server"}, [2]string{"cli", "run an interactive or one-prompt conversation"})
			return nil
		}
		if len(args) == 1 {
			app.writeGroupHelp(usage("run", "llama-cpp", "MODE", "[OPTIONS]"), [2]string{"server", "start the OpenAI-compatible server"}, [2]string{"cli", "run an interactive or one-prompt conversation"})
			return controlerr.Usage("choose llama.cpp mode server or cli")
		}
		if strings.HasPrefix(args[1], "-") {
			app.writeGroupHelp(usage("run", "llama-cpp", "MODE", "[OPTIONS]"), [2]string{"server", "start the OpenAI-compatible server"}, [2]string{"cli", "run an interactive or one-prompt conversation"})
			return controlerr.Usage("choose llama.cpp mode server or cli")
		}
		return app.runLlama(args[1], args[2:])
	case "dwarfstar":
		if groupHelpRequested(args[1:]) {
			app.writeGroupHelp(usage("run", "dwarfstar", "MODE", "[OPTIONS]"), [2]string{"server", "start the OpenAI-compatible server"}, [2]string{"cli", "run an interactive or one-prompt conversation"})
			return nil
		}
		if len(args) == 1 {
			app.writeGroupHelp(usage("run", "dwarfstar", "MODE", "[OPTIONS]"), [2]string{"server", "start the OpenAI-compatible server"}, [2]string{"cli", "run an interactive or one-prompt conversation"})
			return controlerr.Usage("choose DwarfStar mode server or cli")
		}
		if strings.HasPrefix(args[1], "-") {
			app.writeGroupHelp(usage("run", "dwarfstar", "MODE", "[OPTIONS]"), [2]string{"server", "start the OpenAI-compatible server"}, [2]string{"cli", "run an interactive or one-prompt conversation"})
			return controlerr.Usage("choose DwarfStar mode server or cli")
		}
		return app.runDwarfStar(args[1], args[2:])
	default:
		writeHelp()
		return controlerr.Usage("unknown application %q", application)
	}
}

func (app *App) runComfyUI(args []string) error {
	set := app.flags("run comfyui", usage("run", "comfyui", "[OPTIONS]", "[-- COMFYUI-ARGS]"))
	profile := set.String("profile", "", "auto, cpu, rdna4, strix-halo, or strix-point (default: auto)")
	listen := set.String("listen", "", "host IP on which to publish ComfyUI (default: 127.0.0.1)")
	portText := set.String("port", "", "host port (default: 8188)")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact GPU render node; repeatable")
	detach := set.Bool("detach", false, "run in background")
	dryRun := set.Bool("dry-run", false, "print resolved command")
	unconfined := set.Bool("unconfined", false, "disable seccomp")
	disableExtensions := set.Bool("disable-bundled-extensions", false, "disable bundled extensions")
	memoryPolicy := set.String("memory-policy", "", "balanced or conservative (default: balanced)")
	image := set.String("image", "", "override image")
	kernelPolicy := set.String("kernel-policy", "", "default or experimental (default: default)")
	upstreamArgs, err := parseFlagsWithPassthrough(set, args)
	if err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("ComfyUI arguments must follow --")
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
	for _, argument := range upstreamArgs {
		for _, managed := range []string{"--listen", "--port", "--base-directory", "--models-directory", "--input-directory", "--output-directory", "--temp-directory", "--user-directory", "--database-url", "--cpu"} {
			if argument == managed || strings.HasPrefix(argument, managed+"=") {
				return controlerr.Usage("ComfyUI argument %s is managed by the launcher", managed)
			}
		}
	}
	imageValue := firstNonEmpty(*image, config.EnvironmentValue(app.Environment, "IMAGE", application.Image))
	command := runtime.WebCommand(runtime.WebOptions{Image: imageValue, Profile: profileValue, Listen: listenValue, Port: portValue, DataDir: dataRoot, RenderNodes: selected, Detach: *detach, Unconfined: *unconfined, DisableBundledExtensions: *disableExtensions, Arguments: upstreamArgs, ContainerName: application.ContainerName, Application: "comfyui", MemoryPolicy: memoryValue, KernelPolicy: kernelValue, Publish: true}, app.podman().SELinuxVolumeSuffix(app.Context))
	if !isLoopback(listenValue) {
		fmt.Fprintf(app.Stderr, "%s ComfyUI is published on %s:%d without authentication.\n", app.terminal(app.Stderr).Warning("WARNING:"), listenValue, portValue)
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s %s\n", terminal.Label("Application data:"), (storage.Layout{Root: dataRoot}).Application("comfyui"))
	if *dryRun {
		fmt.Fprintf(app.Stdout, "%s\n  %s\n", terminal.Heading("Resolved command:"), terminal.Command(shellJoin(command)))
		return nil
	}
	return app.startManaged(application, imageValue, command, *detach)
}

func (app *App) runLlama(mode string, args []string) error {
	if err := requireChoice(mode, "llama.cpp mode", "server", "cli"); err != nil {
		return err
	}
	sourceSynopsis := "(--model FILE | --preset NAME)"
	if mode == "server" {
		sourceSynopsis = "(--model FILE | --preset NAME | --router)"
	}
	set := app.flags("run llama-cpp "+mode, usage("run", "llama-cpp", mode, sourceSynopsis, "[OPTIONS]"))
	model := set.String("model", "", "exact local GGUF")
	presetID := set.String("preset", "", "installed catalog preset")
	routerMode := false
	if mode == "server" {
		set.BoolVar(&routerMode, "router", false, "serve installed presets")
	}
	profileFlag := set.String("profile", "", "execution profile: auto, cpu, rdna4, strix-halo, or strix-point (default: auto)")
	backend := set.String("backend", "rocm", "GPU inference backend: rocm or vulkan")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact GPU render node; repeatable")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override the managed local image tag")
	contextSize := set.Int64("context", -1, "context size; overrides the preset or model default")
	unconfined := set.Bool("unconfined", false, "disable the container seccomp filter")
	dryRun := set.Bool("dry-run", false, "validate and print the resolved Podman command")
	listen, portText, apiKey := "", "", ""
	detach := false
	modelsMax := 0
	if mode == "server" {
		set.StringVar(&listen, "listen", "", "host IP on which to publish the server (default: 127.0.0.1)")
		set.StringVar(&portText, "port", "", "host server port (default: 8080)")
		set.BoolVar(&detach, "detach", false, "run in background")
		set.StringVar(&apiKey, "api-key-file", "", "exact local API-key file mounted read-only")
		set.IntVar(&modelsMax, "models-max", 0, "router models loaded simultaneously (default: 2)")
	}
	prompt := optionalString{}
	if mode == "cli" {
		set.Var(&prompt, "prompt", "single prompt")
	}
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("run llama-cpp %s does not accept positional arguments", mode)
	}
	contextExplicit := setWasSet(set, "context")
	backendExplicit := setWasSet(set, "backend")
	modelsMaxExplicit := setWasSet(set, "models-max")
	selectedSources := boolCount(*model != "", *presetID != "", routerMode)
	if selectedSources != 1 {
		if selectedSources == 0 {
			set.renderHelp(app.Stdout)
		}
		if mode == "server" {
			return controlerr.Usage("choose exactly one of --model, --preset, or --router")
		}
		return controlerr.Usage("choose exactly one of --model or --preset")
	}
	if err := requireChoice(*backend, "backend", "rocm", "vulkan"); err != nil {
		return err
	}
	if contextExplicit && *contextSize < 0 {
		return controlerr.Usage("--context must be zero or positive")
	}
	if modelsMaxExplicit && !routerMode {
		return controlerr.Usage("--models-max is only valid with --router")
	}
	modelsMaxValue := modelsMax
	if !modelsMaxExplicit {
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
	options := runtime.LlamaOptions{Profile: profileValue, Mode: mode, DataDir: dataRoot, Backend: *backend, ModelsMax: modelsMaxValue, RenderNodes: selectedNodes, AutoRemove: true, Unconfined: *unconfined, Detach: detach}
	application, _ := config.ApplicationByID("llama-cpp")
	if mode == "server" {
		options.Listen = firstNonEmpty(listen, config.EnvironmentValue(app.Environment, "LISTEN", config.DefaultListen))
		if err := config.ValidateListenAddress(options.Listen); err != nil {
			return err
		}
		options.Port, err = config.ValidatePort(firstNonEmpty(portText, config.EnvironmentValue(app.Environment, "PORT", fmt.Sprint(application.Port))))
		if err != nil {
			return err
		}
	}
	options.Image = firstNonEmpty(*imageFlag, application.Image)
	options.SourceRevision = app.projectRevision()
	if *contextSize >= 0 {
		options.Context = *contextSize
	}
	displayModel := ""
	backendSelectedByPreset := false
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
		modelProfile := platform.ModelProfile(profileValue, selectedNodes)
		if !preset.SupportsRuntime(*backend, modelProfile) {
			supported := preset.RuntimeBackends(modelProfile)
			if backendExplicit || len(supported) == 0 {
				return controlerr.Usage("llama.cpp preset %q does not support backend %s on profile %s; available backends: %s", *presetID, *backend, modelProfile, strings.Join(supported, ", "))
			}
			*backend = supported[0]
			options.Backend = *backend
			backendSelectedByPreset = true
		}
		bundle := managed.Bundles[preset.Bundle]
		if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
			return controlerr.New("preset %q is not installed: %v\n  Install content: %s", *presetID, err, identity.Command("content", "install", preset.Bundle))
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
		options.ProfileModelLoad = preset.ModelLoad
		options.AllowedProfiles = preset.BackendProfiles[*backend]
		if preset.SamplingPolicy != "" {
			policy := managed.SamplingPolicies[preset.SamplingPolicy]
			options.SamplingDefaults = map[string]any{"thinking": policy.Thinking, "non_thinking": policy.NonThinking}
		}
		if *contextSize == -1 {
			options.Context = preset.DefaultContext
		}
		displayModel = fmt.Sprintf("%s (%s)", *presetID, content.ArtifactPath(dataRoot, artifact))
	}
	if routerMode {
		contents, installed, renderErr := runtime.RenderRouter(managed, dataRoot, *backend, platform.ModelProfile(profileValue, selectedNodes))
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
	if apiKey != "" {
		options.APIKeyFile, err = regularFile(apiKey, "API-key file")
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
		fmt.Fprintf(app.Stderr, "%s llama.cpp is published on %s:%d without authentication.\n", app.terminal(app.Stderr).Warning("WARNING:"), options.Listen, options.Port)
	}
	terminal := app.terminal(app.Stdout)
	backendDisplay := options.Backend
	if backendSelectedByPreset {
		backendDisplay += " (required by preset)"
	}
	fmt.Fprintf(app.Stdout, "%s %s\n%s %s\n%s %s\n", terminal.Label("Application data:"), (storage.Layout{Root: dataRoot}).Application("llama-cpp"), terminal.Label("Backend:"), backendDisplay, terminal.Label("Model:"), displayModel)
	if mode == "server" {
		status := []string{"status", "llama-cpp"}
		if *presetID != "" {
			status = append(status, "--model", *presetID)
		}
		fmt.Fprintf(app.Stdout, "%s %s\n", terminal.Label("Configuration:"), terminal.Command(identity.Command(status...)))
	}
	if *dryRun {
		fmt.Fprintf(app.Stdout, "%s\n  %s\n", terminal.Heading("Resolved command:"), terminal.Command(shellJoin(command)))
		return nil
	}
	return app.startManaged(application, options.Image, command, detach)
}

func (app *App) runDwarfStar(mode string, args []string) error {
	if err := requireChoice(mode, "DwarfStar mode", "server", "cli"); err != nil {
		return err
	}
	set := app.flags("run dwarfstar "+mode, usage("run", "dwarfstar", mode, "[OPTIONS]"))
	modelFlag := set.String("model", "", "exact local DwarfStar-compatible GGUF; defaults to the managed model")
	profileFlag := set.String("profile", "", "execution profile: auto, rdna4, strix-halo, or strix-point (default: auto)")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact render node")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override the managed local image tag")
	contextSize := set.Int64("context", -1, "allocated context tokens (default: managed preset policy)")
	outputTokens := set.Int64("output-tokens", -1, "default or CLI maximum output tokens (default: managed preset policy)")
	dspark := set.Bool("dspark", false, "enable the exact managed DSpark support GGUF")
	unconfined := set.Bool("unconfined", false, "disable the container seccomp filter")
	dryRun := set.Bool("dry-run", false, "validate and print the resolved Podman command")
	listen, portText := "", ""
	detach := false
	if mode == "server" {
		set.StringVar(&listen, "listen", "", "host IP on which to publish the server (default: 127.0.0.1)")
		set.StringVar(&portText, "port", "", "host server port (default: 8000)")
		set.BoolVar(&detach, "detach", false, "run in background")
	}
	prompt := optionalString{}
	noThinking := false
	if mode == "cli" {
		set.Var(&prompt, "prompt", "run one prompt; omit for an interactive conversation")
		set.BoolVar(&noThinking, "no-thinking", false, "disable thinking and request a direct answer")
	}
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("run dwarfstar %s does not accept positional arguments", mode)
	}
	contextExplicit := setWasSet(set, "context")
	outputExplicit := setWasSet(set, "output-tokens")
	if contextExplicit && (*contextSize < 4096 || *contextSize > 1048576) {
		return controlerr.Usage("--context must be between 4096 and 1048576")
	}
	if outputExplicit && *outputTokens < 1 {
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
	if len(managed.DwarfStarPresets) != 1 {
		return fmt.Errorf("catalog must define exactly one default DwarfStar preset")
	}
	var preset catalog.DwarfStarPreset
	for _, candidate := range managed.DwarfStarPresets {
		preset = candidate
	}
	if !contextExplicit {
		*contextSize = preset.DefaultContext
	}
	if !outputExplicit {
		*outputTokens = preset.MaxOutputTokens
	}
	if *contextSize < 4096 || *contextSize > 1048576 {
		return controlerr.Usage("--context must be between 4096 and 1048576")
	}
	if *outputTokens < 1 || *outputTokens >= *contextSize {
		return controlerr.Usage("--output-tokens must be positive and smaller than --context")
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
		bundleID := preset.Bundle
		if *dspark {
			bundleID = preset.DSparkBundle
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
	options := runtime.DwarfStarOptions{Image: firstNonEmpty(*imageFlag, application.Image), Mode: mode, DataDir: dataRoot, Model: model, SupportModel: support, DSpark: *dspark, RenderNodes: selectedNodes, Profile: profileValue, Context: *contextSize, OutputTokens: *outputTokens, NoThinking: noThinking, Detach: detach, Unconfined: *unconfined, Interactive: mode == "cli" && !prompt.set}
	if prompt.set {
		options.Prompt = &prompt.value
	}
	if mode == "server" {
		options.Listen = firstNonEmpty(listen, config.EnvironmentValue(app.Environment, "LISTEN", config.DefaultListen))
		if err := config.ValidateListenAddress(options.Listen); err != nil {
			return err
		}
		options.Port, err = config.ValidatePort(firstNonEmpty(portText, config.EnvironmentValue(app.Environment, "PORT", fmt.Sprint(application.Port))))
		if err != nil {
			return err
		}
	}
	if options.Interactive && !*dryRun && !terminalReader(app.Stdin) {
		return controlerr.New("interactive DwarfStar CLI requires a terminal; pass --prompt")
	}
	command, err := runtime.DwarfStarCommand(options, app.podman().SELinuxVolumeSuffix(app.Context))
	if err != nil {
		return err
	}
	if mode == "server" && !isLoopback(options.Listen) {
		fmt.Fprintf(app.Stderr, "%s DwarfStar is published on %s:%d without authentication.\n", app.terminal(app.Stderr).Warning("WARNING:"), options.Listen, options.Port)
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s %s\n%s %s\n", terminal.Label("Application data:"), (storage.Layout{Root: dataRoot}).Application("dwarfstar"), terminal.Label("Model:"), model)
	if *dryRun {
		fmt.Fprintf(app.Stdout, "%s\n  %s\n", terminal.Heading("Resolved command:"), terminal.Command(shellJoin(command)))
		return nil
	}
	return app.startManaged(application, options.Image, command, detach)
}

func (app *App) commandShell(args []string) error {
	set := app.flags("shell", usage("shell", "APPLICATION", "[--data-dir PATH]", "[--image TAG]"))
	set.Argument("APPLICATION", "application whose constrained image shell to open")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override image")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) != 1 {
		if len(set.Args()) == 0 {
			set.renderHelp(app.Stdout)
		}
		return controlerr.Usage("shell accepts exactly one application")
	}
	applicationID := set.Args()[0]
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
	set := app.flags("logs", usage("logs", "APPLICATION", "[--follow]", "[--tail N | --all]"))
	set.Argument("APPLICATION", "managed application container")
	follow := set.Bool("follow", false, "follow output")
	tail := set.Int("tail", 200, "recent lines")
	all := set.Bool("all", false, "show complete logs")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) != 1 {
		if len(set.Args()) == 0 {
			set.renderHelp(app.Stdout)
		}
		return controlerr.Usage("logs accepts exactly one application")
	}
	applicationID := set.Args()[0]
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
	return app.podman().Logs(app.Context, podman.LogOptions{Container: application.ContainerName, Follow: *follow, All: *all, Tail: *tail, Streams: podman.Streams{Stdin: app.Stdin, Stdout: app.Stdout, Stderr: app.Stderr}})
}

func (app *App) commandStop(args []string) error {
	set := app.flags("stop", usage("stop", "APPLICATION|gateway|all"))
	set.Argument("APPLICATION|gateway|all", "one managed application, the host gateway, or all of them")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) != 1 {
		if len(set.Args()) == 0 {
			set.renderHelp(app.Stdout)
		}
		return controlerr.Usage("stop accepts exactly one application")
	}
	applicationID := set.Args()[0]
	if applicationID != "all" && applicationID != "gateway" {
		if _, ok := config.ApplicationByID(applicationID); !ok {
			return controlerr.Usage("unknown application %q", applicationID)
		}
	}
	if applicationID == "gateway" || applicationID == "all" {
		stopped, err := gateway.StopLocalGateway(app.Context, app.Environment)
		if err != nil {
			return err
		}
		if stopped {
			fmt.Fprintln(app.Stdout, app.terminal(app.Stdout).Success("Gateway stopped."))
		} else {
			fmt.Fprintln(app.Stdout, app.terminal(app.Stdout).Muted("Gateway control socket not present."))
		}
		if applicationID == "gateway" {
			return nil
		}
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	targets := []string{applicationID}
	if applicationID == "all" {
		targets = nil
		for _, spec := range config.Applications() {
			targets = append(targets, spec.ID)
		}
	}
	for _, identifier := range targets {
		application, _ := config.ApplicationByID(identifier)
		present, err := app.podman().Exists(app.Context, "container", application.ContainerName)
		if err != nil {
			return err
		}
		if !present {
			fmt.Fprintf(app.Stdout, "%s %s\n", app.terminal(app.Stdout).Muted("Container not present:"), application.ContainerName)
			continue
		}
		if err := app.podman().RemoveContainer(app.Context, application.ContainerName, 2, podman.Streams{Stdin: app.Stdin, Stdout: app.Stdout, Stderr: app.Stderr}); err != nil {
			return err
		}
		fmt.Fprintf(app.Stdout, "%s %s\n", app.terminal(app.Stdout).Success("Removed container:"), application.ContainerName)
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
		message := fmt.Sprintf("image not found: %s\n  Build image: %s", image, identity.Command("build", application.ID))
		if application.ID == "comfyui" && image == application.Image {
			message += "\n  Install content: " + identity.Command("content", "install", "comfyui", "image")
		}
		return controlerr.New("%s", message)
	}
	managed, err := app.podman().ManagedContainerNames(app.Context, application.ID)
	if err != nil {
		return err
	}
	if len(managed) > 0 {
		return controlerr.New("managed %s container already exists: %s; stop its owning command first", application.ID, strings.Join(managed, ", "))
	}
	exists, err := app.podman().Exists(app.Context, "container", application.ContainerName)
	if err != nil {
		return err
	}
	if exists {
		return controlerr.New("container name is already occupied: %s", application.ContainerName)
	}
	if application.Port != 0 {
		terminal := app.terminal(app.Stdout)
		fmt.Fprintf(app.Stdout, "%s %s\n%s %s\n", terminal.Label("Logs:"), terminal.Command(identity.Command("logs", application.ID)), terminal.Label("Stop:"), terminal.Command(identity.Command("stop", application.ID)))
	}
	_, runErr := app.run(command, false)
	if runErr != nil && !detach {
		cleanupErr := app.podman().RemoveContainer(contextWithoutCancel(), application.ContainerName, 1, podman.Streams{})
		if app.Context.Err() != nil {
			return withCleanupFailure(app.Context.Err(), "remove interrupted application container", cleanupErr)
		}
		return withCleanupFailure(runErr, "remove failed application container", cleanupErr)
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
	configuration, err := app.hostConfiguration()
	if err != nil {
		return "", err
	}
	selected, err := config.SelectDataDir(value, app.Environment, configuration)
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
	result, err := app.Runner.Run(app.Context, process.Command{Name: "git", Args: []string{"-C", app.Root, "rev-parse", "HEAD"}})
	if err != nil || result.Status != 0 {
		return "unavailable"
	}
	revision := strings.TrimSpace(string(result.Stdout))
	if len(revision) != 40 || strings.Trim(revision, "0123456789abcdef") != "" {
		return "unavailable"
	}
	changes, changeErr := app.Runner.Run(app.Context, process.Command{Name: "git", Args: []string{"-C", app.Root, "status", "--porcelain"}})
	if changeErr == nil && changes.Status == 0 && strings.TrimSpace(string(changes.Stdout)) != "" {
		return revision + " (dirty)"
	}
	return revision
}

func (app *App) projectSourceIdentity() (string, error) {
	result, err := app.Runner.Run(app.Context, process.Command{Name: "git", Args: []string{"-C", app.Root, "rev-parse", "HEAD"}})
	if err != nil || result.Status != 0 {
		return "", fmt.Errorf("cannot resolve project revision")
	}
	revision := strings.TrimSpace(string(result.Stdout))
	if len(revision) != 40 || strings.Trim(revision, "0123456789abcdef") != "" {
		return "", fmt.Errorf("project revision is invalid")
	}
	difference, err := app.Runner.Run(app.Context, process.Command{Name: "git", Args: []string{"-C", app.Root, "diff", "--binary", "HEAD", "--"}})
	if err != nil || difference.Status != 0 {
		return "", fmt.Errorf("cannot fingerprint tracked project changes")
	}
	untracked, err := app.Runner.Run(app.Context, process.Command{Name: "git", Args: []string{"-C", app.Root, "ls-files", "--others", "--exclude-standard", "-z"}})
	if err != nil || untracked.Status != 0 {
		return "", fmt.Errorf("cannot fingerprint untracked project inputs")
	}
	if len(difference.Stdout) == 0 && len(untracked.Stdout) == 0 {
		return revision, nil
	}
	hash := sha256.New()
	hash.Write([]byte("paracetamol-source-v1\x00" + revision + "\x00"))
	hash.Write(difference.Stdout)
	paths := []string{}
	if len(untracked.Stdout) != 0 {
		paths = strings.Split(strings.TrimSuffix(string(untracked.Stdout), "\x00"), "\x00")
	}
	sort.Strings(paths)
	for _, relative := range paths {
		if relative == "" || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("git returned unsafe untracked path %q", relative)
		}
		path := filepath.Join(app.Root, filepath.FromSlash(relative))
		status, inspectErr := os.Lstat(path)
		if inspectErr != nil {
			return "", fmt.Errorf("inspect untracked project input %s: %w", relative, inspectErr)
		}
		fmt.Fprintf(hash, "\x00%s\x00%s\x00", relative, status.Mode())
		if status.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return "", readErr
			}
			hash.Write([]byte(target))
			continue
		}
		if !status.Mode().IsRegular() {
			return "", fmt.Errorf("untracked project input is not a regular file: %s", relative)
		}
		handle, openErr := os.Open(path)
		if openErr != nil {
			return "", openErr
		}
		_, copyErr := io.Copy(hash, handle)
		closeErr := handle.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return fmt.Sprintf("%s+dirty.%x", revision, hash.Sum(nil)), nil
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
	return ui.IsTerminal(reader)
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

func setWasSet(set interface{ Visit(func(*flag.Flag)) }, name string) bool {
	found := false
	set.Visit(func(item *flag.Flag) {
		if item.Name == name {
			found = true
		}
	})
	return found
}

func contextWithoutCancel() context.Context { return context.Background() }
