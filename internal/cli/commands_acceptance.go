package cli

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
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

const acceptanceSchema = "rocmplete.hardware-acceptance.v1"

type acceptanceCase struct {
	id, description, application, bundle string
	visual                               bool
}

func (app *App) commandAcceptance(args []string) error {
	set := app.flags("acceptance", "Usage: ./rocmplete acceptance [OPTIONS]")
	profileFlag := set.String("profile", "auto", "expected GPU profile or auto")
	var nodes, applications stringList
	set.Var(&nodes, "render-node", "exact GPU render node")
	set.Var(&applications, "application", "limit application workload; repeatable")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	portText := set.String("port", "8190", "private ComfyUI smoke port")
	output := set.String("output", "", "new acceptance JSON")
	resume := set.String("resume", "", "resume a compatible Go acceptance JSON")
	prepare := set.Bool("prepare", false, "build missing images and install smoke content")
	dryRun := set.Bool("dry-run", false, "show preparation and smoke cases")
	nonInteractive := set.Bool("non-interactive", false, "do not prompt for visual review")
	acceptLicense := set.Bool("accept-license", false, "accept model agreements")
	acknowledgeRisk := set.Bool("acknowledge-license-risk", false, "allow NOASSERTION smoke content")
	memory := set.String("memory-policy", "balanced", "balanced or conservative")
	kernel := set.String("kernel-policy", "default", "default or experimental")
	if err := set.Parse(args); err != nil {
		return err
	}
	if len(set.Args()) != 0 {
		return controlerr.Usage("acceptance takes no positional command; use './rocmplete acceptance [OPTIONS]'")
	}
	if *output != "" && *resume != "" {
		return controlerr.Usage("--output and --resume are mutually exclusive")
	}
	if err := platform.ValidateProfile(*profileFlag); err != nil || *profileFlag == "cpu" {
		return controlerr.Usage("acceptance requires auto or a supported GPU profile")
	}
	if err := requireChoice(*memory, "memory policy", "balanced", "conservative"); err != nil {
		return err
	}
	if err := requireChoice(*kernel, "kernel policy", "default", "experimental"); err != nil {
		return err
	}
	for _, application := range applications {
		if err := requireChoice(application, "application", "comfyui", "llama-cpp", "dwarfstar"); err != nil {
			return err
		}
	}
	selectedNodes, err := app.resolveDevices(*profileFlag, nodes, nodes != nil)
	if err != nil {
		return err
	}
	if len(selectedNodes) != 1 {
		return controlerr.Usage("acceptance requires exactly one render node")
	}
	port, err := config.ValidatePort(*portText)
	if err != nil {
		return err
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, !*dryRun)
	if err != nil {
		return err
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	cases := selectedAcceptanceCases(applications)
	if *dryRun {
		requiredBundles := acceptanceBundles(managed, cases)
		if err := requireBenchmarkAgreements(managed, requiredBundles, *acceptLicense, true); err != nil {
			return err
		}
		fmt.Fprintf(app.Stdout, "Hardware acceptance\n  Profile      %s\n  Render node  %s\n  Data         %s\n", *profileFlag, selectedNodes[0], dataRoot)
		for _, image := range acceptanceImages(cases) {
			present, _ := app.podman().Exists(app.Context, "image", image.image)
			fmt.Fprintf(app.Stdout, "  Image        %-10s %s (%s)\n", image.target, image.image, map[bool]string{true: "ready", false: "build required"}[present])
		}
		for _, bundle := range requiredBundles {
			state := "ready"
			if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
				state = "install required"
			}
			fmt.Fprintf(app.Stdout, "  Content      %-54s %s\n", bundle.ID, state)
		}
		fmt.Fprintln(app.Stdout, "\nCases:")
		for _, candidate := range cases {
			fmt.Fprintf(app.Stdout, "  %-16s %s\n", candidate.id, candidate.description)
		}
		if *prepare {
			fmt.Fprintln(app.Stdout, "\nPreparation was planned but no image was built and no content was installed.")
		} else {
			fmt.Fprintln(app.Stdout, "\nNo workload was started.")
		}
		return nil
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	basePresent, err := app.podman().Exists(app.Context, "image", config.ROCmBaseImage)
	if err != nil {
		return err
	}
	if !basePresent && *prepare {
		if err := app.commandBuild([]string{"base"}); err != nil {
			return err
		}
	} else if !basePresent {
		return controlerr.New("acceptance image is missing: %s (repeat with --prepare)", config.ROCmBaseImage)
	}
	baseImageID, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", config.ROCmBaseImage}, "inspect acceptance base image")
	if err != nil {
		return err
	}
	gpuResult, err := app.run(runtime.GPUDiagnosticCommand(config.ROCmBaseImage, selectedNodes), true)
	if err != nil {
		return err
	}
	hardware, err := runtime.ParseGPUDiagnostic(string(gpuResult.Stdout))
	if err != nil {
		return err
	}
	detected, ok := platform.ProfileForArchitecture(hardware["Architecture"])
	if !ok {
		return controlerr.New("acceptance does not support architecture %s", hardware["Architecture"])
	}
	if *profileFlag != "auto" && detected.ID != *profileFlag {
		return controlerr.New("detected profile %s does not match requested %s", detected.ID, *profileFlag)
	}
	if result, err := app.run(runtime.CPUIsolationDiagnosticCommand(config.ROCmBaseImage), true); err != nil || !strings.Contains(string(result.Stdout), "CPU device isolation: passed") {
		if err != nil {
			return err
		}
		return controlerr.New("CPU device-isolation probe returned incomplete output")
	}
	explicitDwarf := containsString(applications, "dwarfstar")
	if detected.ID != "strix-halo" && !explicitDwarf {
		filtered := cases[:0]
		for _, candidate := range cases {
			if candidate.application != "dwarfstar" {
				filtered = append(filtered, candidate)
			}
		}
		cases = filtered
	}
	requiredBundles := acceptanceBundles(managed, cases)
	if err := requireBenchmarkAgreements(managed, requiredBundles, *acceptLicense, false); err != nil {
		return err
	}
	images := acceptanceImages(cases)
	if *prepare {
		for _, image := range images[1:] {
			present, err := app.podman().Exists(app.Context, "image", image.image)
			if err != nil {
				return err
			}
			if !present {
				if err := app.commandBuild([]string{image.target}); err != nil {
					return err
				}
			}
		}
		for _, bundle := range requiredBundles {
			if _, err := content.RequireBundle(managed, bundle, dataRoot); err == nil {
				continue
			}
			arguments := []string{bundle.ID, "--data-dir", dataRoot, "--non-interactive"}
			if *acceptLicense {
				arguments = append(arguments, "--accept-license")
			}
			if *acknowledgeRisk {
				arguments = append(arguments, "--acknowledge-license-risk")
			}
			if err := app.contentInstall(arguments); err != nil {
				return err
			}
		}
	}
	for _, image := range images {
		present, err := app.podman().Exists(app.Context, "image", image.image)
		if err != nil {
			return err
		}
		if !present {
			return controlerr.New("acceptance image is missing: %s (repeat with --prepare)", image.image)
		}
	}
	for _, bundle := range requiredBundles {
		if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
			return controlerr.New("acceptance content is not ready: %s (repeat with --prepare): %v", bundle.ID, err)
		}
	}
	definition := map[string]any{"profile": detected.ID, "architecture": hardware["Architecture"], "render_node": selectedNodes[0], "memory_policy": *memory, "kernel_policy": *kernel, "base_image": map[string]any{"reference": config.ROCmBaseImage, "id": baseImageID}, "cases": acceptanceCaseIDs(cases)}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		return err
	}
	resultPath := benchmark.DefaultPath((storage.Layout{Root: dataRoot}).AcceptanceResults(), ".json")
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
		if result["schema"] != acceptanceSchema || result["fingerprint"] != fingerprint {
			return controlerr.New("acceptance checkpoint does not match this hardware and case selection")
		}
	} else {
		if *output != "" {
			resultPath, err = absoluteNewPath(*output)
			if err != nil {
				return err
			}
		}
		entries := []any{}
		for _, candidate := range cases {
			entries = append(entries, map[string]any{"identifier": candidate.id, "description": candidate.description, "application": candidate.application, "bundle": candidate.bundle, "visual": candidate.visual, "status": "pending", "attempts": 0, "artifacts": []any{}})
		}
		result = map[string]any{"schema": acceptanceSchema, "suite_id": time.Now().UTC().Format("20060102T150405Z") + "-" + benchmark.Identifier(), "fingerprint": fingerprint, "definition": definition, "status": "running", "started_at": benchmark.Timestamp(), "hardware": hardware, "cases": entries}
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
	}
	entries, ok := result["cases"].([]any)
	if !ok {
		return controlerr.New("acceptance checkpoint has no cases")
	}
	failed, blocked := false, false
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok || entry["status"] == "pass" {
			continue
		}
		identifier := fmt.Sprint(entry["identifier"])
		candidate := findAcceptanceCase(cases, identifier)
		entry["status"], entry["attempts"], entry["started_at"] = "running", numberAsInt64(entry["attempts"])+1, benchmark.Timestamp()
		_ = benchmark.WriteCheckpoint(resultPath, result)
		started := time.Now()
		artifacts, caseErr := app.runAcceptanceCase(managed, candidate, dataRoot, detected.ID, selectedNodes[0], port, fmt.Sprint(result["suite_id"]), *memory, *kernel)
		entry["wall_seconds"], entry["finished_at"], entry["artifacts"] = time.Since(started).Seconds(), benchmark.Timestamp(), artifacts
		if caseErr != nil {
			entry["status"], entry["reason"] = "fail", caseErr.Error()
			failed = true
		} else if candidate.visual && (*nonInteractive || !app.confirmVisual(candidate, artifacts)) {
			entry["status"], entry["reason"] = "blocked", "generated artifact requires successful visual review"
			blocked = true
		} else {
			entry["status"] = "pass"
		}
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
	}
	result["status"], result["finished_at"] = "pass", benchmark.Timestamp()
	if failed {
		result["status"] = "fail"
	} else if blocked {
		result["status"] = "blocked"
	}
	if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
		return err
	}
	fmt.Fprintf(app.Stdout, "Acceptance complete: %s (%s)\n", resultPath, result["status"])
	if failed || blocked {
		return &controlerr.Error{Message: fmt.Sprintf("acceptance status: %s", result["status"]), Status: 1}
	}
	return nil
}

func selectedAcceptanceCases(applications []string) []acceptanceCase {
	all := []acceptanceCase{{"host-gpu", "GPU operation and exact device isolation", "", "", false}, {"comfyui-image", "ComfyUI Qwen Image FP8 Lightning generation", "comfyui", "qwen-image-2512-fp8-lightning", true}, {"comfyui-video", "ComfyUI Wan 2.2 FP8 Lightning five-frame generation", "comfyui", "wan-2.2-t2v-14b-fp8-lightning", true}, {"llama-cpp", "llama.cpp Qwen3 0.6B GPU offload benchmark", "llama-cpp", "llama-qwen3-0.6b-q8-0", false}, {"dwarfstar", "DwarfStar direct-answer generation", "dwarfstar", "dwarfstar-deepseek-v4-flash-0731-q2-imatrix", false}}
	if len(applications) == 0 {
		return all
	}
	selected := []acceptanceCase{all[0]}
	for _, candidate := range all[1:] {
		if containsString(applications, candidate.application) {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func acceptanceImages(cases []acceptanceCase) []struct{ target, image string } {
	images := []struct{ target, image string }{{"base", config.ROCmBaseImage}}
	seen := map[string]bool{config.ROCmBaseImage: true}
	for _, candidate := range cases {
		if candidate.application == "" {
			continue
		}
		application, _ := config.ApplicationByID(candidate.application)
		if !seen[application.Image] {
			seen[application.Image] = true
			images = append(images, struct{ target, image string }{application.ID, application.Image})
		}
	}
	return images
}

func acceptanceBundles(managed catalog.Catalog, cases []acceptanceCase) []catalog.Bundle {
	seen := map[string]bool{}
	var bundles []catalog.Bundle
	for _, candidate := range cases {
		if candidate.bundle != "" && !seen[candidate.bundle] {
			seen[candidate.bundle] = true
			bundles = append(bundles, managed.Bundles[candidate.bundle])
		}
	}
	return bundles
}

func acceptanceCaseIDs(cases []acceptanceCase) []string {
	result := make([]string, len(cases))
	for index, candidate := range cases {
		result[index] = candidate.id
	}
	return result
}

func findAcceptanceCase(cases []acceptanceCase, identifier string) acceptanceCase {
	for _, candidate := range cases {
		if candidate.id == identifier {
			return candidate
		}
	}
	return acceptanceCase{id: identifier}
}

func (app *App) runAcceptanceCase(managed catalog.Catalog, candidate acceptanceCase, dataRoot, profile, renderNode string, port int, suiteID, memory, kernel string) ([]any, error) {
	switch candidate.id {
	case "host-gpu":
		return []any{}, nil
	case "comfyui-image", "comfyui-video":
		transform := benchmark.SmokeImagePrompt
		extension := ".png"
		if candidate.id == "comfyui-video" {
			transform, extension = benchmark.SmokeVideoPrompt, ".mp4"
		}
		runID := "acceptance-" + suiteID + "-" + candidate.id
		path, _, err := app.executeComfyBenchmark(managed, managed.Bundles[candidate.bundle], comfyBenchmarkOptions{profile: profile, dataRoot: dataRoot, image: configApplicationImage("comfyui"), renderNode: renderNode, port: port, runs: 1, seed: 10, memoryPolicy: memory, kernelPolicy: kernel, cacheMode: "persistent", acceptLicense: true, transform: transform}, runID)
		if err != nil {
			return nil, err
		}
		outputRoot := filepath.Join((storage.Layout{Root: dataRoot}).Application("comfyui"), "output", "rocmplete-benchmarks", runID)
		var outputs []string
		_ = filepath.WalkDir(outputRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr == nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(path), extension) {
				outputs = append(outputs, path)
			}
			return walkErr
		})
		if len(outputs) != 1 {
			return nil, fmt.Errorf("%s expected one %s output, found %d", candidate.id, extension, len(outputs))
		}
		if err := validateMedia(outputs[0], extension); err != nil {
			return nil, err
		}
		return []any{outputs[0], path}, nil
	case "llama-cpp":
		preset := managed.LlamaPresets["qwen3-0.6b-q8-0"]
		artifact := managed.Artifacts[preset.Artifact]
		command := runtime.LlamaBenchmarkCommand(runtime.LlamaBenchmarkOptions{Image: configApplicationImage("llama-cpp"), Profile: profile, DataDir: dataRoot, Backend: "rocm", ManagedModel: artifact.Destination, RenderNodes: []string{renderNode}, Repetitions: 1, PromptTokens: 32, GenerationTokens: 16, BatchSize: 2048, UBatchSize: 512, CacheTypeK: "f16", CacheTypeV: "f16", FlashAttention: "auto"}, app.podman().SELinuxVolumeSuffix(app.Context))
		rows, err := benchmark.RunLlama(app.Context, app.Runner, command)
		if err != nil {
			return nil, err
		}
		path := filepath.Join((storage.Layout{Root: dataRoot}).AcceptanceResults(), "cases", suiteID+"-llama.json")
		if err := benchmark.WriteLlama(path, benchmark.LlamaRun{Image: map[string]any{"reference": configApplicationImage("llama-cpp")}, Profile: profile, Backend: "rocm", RenderNodes: []string{renderNode}, Model: map[string]any{"preset": preset.ID, "path": content.ArtifactPath(dataRoot, artifact)}, Parameters: map[string]any{"prompt_tokens": 32, "generation_tokens": 16}, Results: rows}); err != nil {
			return nil, err
		}
		return []any{path}, nil
	case "dwarfstar":
		bundle := managed.Bundles[candidate.bundle]
		artifact := managed.Artifacts[bundle.Artifacts[0]]
		prompt := "Reply with exactly: DwarfStar acceptance passed"
		command, err := runtime.DwarfStarCommand(runtime.DwarfStarOptions{Image: configApplicationImage("dwarfstar"), Mode: "cli", DataDir: dataRoot, Model: content.ArtifactPath(dataRoot, artifact), RenderNodes: []string{renderNode}, Profile: profile, Context: 4096, OutputTokens: 64, Prompt: &prompt, NoThinking: true}, app.podman().SELinuxVolumeSuffix(app.Context))
		if err != nil {
			return nil, err
		}
		_, err = app.run(command, false)
		return []any{}, err
	default:
		return nil, fmt.Errorf("unknown acceptance case %q", candidate.id)
	}
}

func validateMedia(path, extension string) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer handle.Close()
	header := make([]byte, 64)
	read, _ := handle.Read(header)
	status, err := handle.Stat()
	if err != nil || status.Size() < 1024 {
		return fmt.Errorf("generated media is unexpectedly small: %s", path)
	}
	if extension == ".png" {
		if read < 24 || string(header[:8]) != "\x89PNG\r\n\x1a\n" || binary.BigEndian.Uint32(header[16:20]) < 64 || binary.BigEndian.Uint32(header[20:24]) < 64 {
			return fmt.Errorf("generated output is not a valid PNG: %s", path)
		}
	} else if !strings.Contains(string(header[:read]), "ftyp") {
		return fmt.Errorf("generated output is not a valid MP4: %s", path)
	}
	return nil
}

func (app *App) confirmVisual(candidate acceptanceCase, artifacts []any) bool {
	fmt.Fprintf(app.Stdout, "Visual review required for %s:\n", candidate.id)
	for _, artifact := range artifacts {
		fmt.Fprintf(app.Stdout, "  %v\n", artifact)
	}
	fmt.Fprint(app.Stdout, "Does the generated artifact pass the documented smoke criteria? [y/N] ")
	scanner := bufio.NewScanner(app.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}
