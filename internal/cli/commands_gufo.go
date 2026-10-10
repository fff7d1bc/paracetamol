package cli

import (
	"fmt"

	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/platform"
	"paracetamol/internal/runtime"
	"paracetamol/internal/storage"
)

const defaultGufoPreset = "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl-gufo"

func (app *App) runGufo(args []string) error {
	set := app.flags("run gufo server", usage("run", "gufo", "server", "[OPTIONS]"))
	presetID := set.String("preset", defaultGufoPreset, "reviewed Gufo model preset")
	profileFlag := set.String("profile", "", "auto or strix-halo (Gufo supports gfx1151 only)")
	var nodes stringList
	set.Var(&nodes, "render-node", "exact Strix Halo render node")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "override the managed local image tag")
	contextSize := set.Int64("context", 0, "context tokens, multiple of 1024, 4096..262144 (default: preset)")
	outputTokens := set.Int64("output-tokens", 0, "default maximum generated tokens (default: preset, bounded by context)")
	cacheBytes := set.Int64("cache-ram-bytes", -1, "RAM snapshot budget in bytes, 0 for auto (default: preset)")
	reasoning := set.String("reasoning", "", "default reasoning: off, medium, or xhigh (default: medium)")
	listen := set.String("listen", "", "IPv4 host publication address (default: 127.0.0.1); use gateway for IPv6")
	portText := set.String("port", "", "host port (default: 8080)")
	detach := set.Bool("detach", false, "run in background")
	unconfined := set.Bool("unconfined", false, "disable container seccomp")
	dryRun := set.Bool("dry-run", false, "validate and print the resolved Podman command without starting")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) != 0 {
		return controlerr.Usage("run gufo server does not accept positional or upstream arguments")
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	preset, ok := managed.GufoPresets[*presetID]
	if !ok {
		return controlerr.Usage("unknown Gufo preset %q", *presetID)
	}
	profile := firstNonEmpty(*profileFlag, config.EnvironmentValue(app.Environment, "PROFILE", "auto"))
	if err := requireChoice(profile, "Gufo profile", "auto", "strix-halo"); err != nil {
		return err
	}
	selectedNodes, err := app.resolveDevices(profile, nodes, nodes != nil)
	if err != nil {
		return err
	}
	if len(selectedNodes) != 1 {
		return controlerr.Usage("Gufo requires exactly one render node")
	}
	if detected := platform.ModelProfile(profile, selectedNodes); detected != "strix-halo" {
		return controlerr.Usage("Gufo requires Strix Halo; selected device profile is %s", detected)
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	bundle := managed.Bundles[preset.Bundle]
	if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
		return controlerr.New("Gufo model is not ready: %v", err)
	}
	if !set.changed("context") {
		*contextSize = preset.DefaultContext
	}
	if !set.changed("output-tokens") {
		*outputTokens = min(preset.MaxOutputTokens, *contextSize-1)
	}
	if !set.changed("cache-ram-bytes") {
		*cacheBytes = preset.CacheRAMBytes
	}
	application, _ := config.ApplicationByID("gufo")
	port, err := config.ValidatePort(firstNonEmpty(*portText, config.EnvironmentValue(app.Environment, "PORT", fmt.Sprint(application.Port))))
	if err != nil {
		return err
	}
	options := runtime.GufoOptions{
		Image: firstNonEmpty(*imageFlag, application.Image), DataDir: dataRoot,
		Model: content.ArtifactPath(dataRoot, managed.Artifacts[preset.Artifact]), ModelID: preset.ID,
		Profile: profile, RenderNodes: selectedNodes, Context: *contextSize, OutputTokens: *outputTokens,
		CacheRAMBytes: *cacheBytes, Reasoning: firstNonEmpty(*reasoning, preset.ReasoningDefault),
		Listen: firstNonEmpty(*listen, config.EnvironmentValue(app.Environment, "LISTEN", config.DefaultListen)),
		Port:   port, Detach: *detach, Unconfined: *unconfined,
	}
	for _, id := range bundle.Artifacts {
		options.ModelFiles = append(options.ModelFiles, content.ArtifactPath(dataRoot, managed.Artifacts[id]))
	}
	command, err := runtime.GufoCommand(options, app.podman().SELinuxVolumeSuffix(app.Context))
	if err != nil {
		return controlerr.Usage("%v", err)
	}
	if !isLoopback(options.Listen) {
		fmt.Fprintf(app.Stderr, "%s Gufo is published on %s:%d without authentication.\n", app.terminal(app.Stderr).Warning("WARNING:"), options.Listen, options.Port)
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Gufo on Strix Halo"))
	writeDetailRows(app.Stdout, terminal, [][2]string{{"Model", preset.ID}, {"Context", fmt.Sprint(options.Context)}, {"Reasoning", options.Reasoning}, {"RAM cache bytes", fmt.Sprint(options.CacheRAMBytes)}, {"Speculation", "off"}, {"Application data", (storage.Layout{Root: dataRoot}).Application("gufo")}})
	if *dryRun {
		fmt.Fprintf(app.Stdout, "%s\n  %s\n", terminal.Heading("Resolved command:"), terminal.Command(shellJoin(command)))
		return nil
	}
	if err := (storage.Layout{Root: dataRoot}).PrepareRuntime("gufo"); err != nil {
		return err
	}
	return app.startManaged(application, options.Image, command, *detach)
}
