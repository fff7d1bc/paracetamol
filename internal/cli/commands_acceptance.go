package cli

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
	"paracetamol/internal/platform"
	"paracetamol/internal/runtime"
	"paracetamol/internal/storage"
)

const acceptanceSchema = "paracetamol.hardware-acceptance.v2"

type acceptanceCase struct {
	id, description, application, bundle string
	visual                               bool
}

type acceptanceImageIdentity struct {
	Target    string `json:"target"`
	Reference string `json:"reference"`
	ID        string `json:"id"`
}

type acceptanceBundleIdentity struct {
	ID        string            `json:"id"`
	Artifacts map[string]string `json:"artifacts"`
	Workflow  string            `json:"workflow,omitempty"`
}

type acceptanceDefinition struct {
	Profile         string                     `json:"profile"`
	Architecture    string                     `json:"architecture"`
	RenderNode      string                     `json:"render_node"`
	MemoryPolicy    string                     `json:"memory_policy"`
	KernelPolicy    string                     `json:"kernel_policy"`
	Port            int                        `json:"port"`
	ProjectRevision string                     `json:"project_revision"`
	Images          []acceptanceImageIdentity  `json:"images"`
	Bundles         []acceptanceBundleIdentity `json:"bundles"`
	Cases           []string                   `json:"cases"`
}

type acceptanceEntry struct {
	Identifier  string               `json:"identifier"`
	Description string               `json:"description"`
	Application string               `json:"application,omitempty"`
	Bundle      string               `json:"bundle,omitempty"`
	Visual      bool                 `json:"visual"`
	Status      string               `json:"status"`
	Attempts    int                  `json:"attempts"`
	StartedAt   string               `json:"started_at,omitempty"`
	FinishedAt  string               `json:"finished_at,omitempty"`
	WallSeconds float64              `json:"wall_seconds,omitempty"`
	Artifacts   []acceptanceArtifact `json:"artifacts"`
	Reason      string               `json:"reason,omitempty"`
}

type acceptanceArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type acceptanceResult struct {
	Schema      string               `json:"schema"`
	SuiteID     string               `json:"suite_id"`
	Fingerprint string               `json:"fingerprint"`
	Definition  acceptanceDefinition `json:"definition"`
	Status      string               `json:"status"`
	StartedAt   string               `json:"started_at"`
	FinishedAt  string               `json:"finished_at,omitempty"`
	Hardware    map[string]string    `json:"hardware"`
	Cases       []acceptanceEntry    `json:"cases"`
}

func (app *App) commandAcceptance(args []string) error {
	if len(args) > 0 && args[0] == "run" {
		return controlerr.Usage("acceptance is a direct command; use %q", identity.Command("acceptance", "[OPTIONS]"))
	}
	set := app.flags("acceptance", usage("acceptance", "[OPTIONS]"))
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
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) != 0 {
		return controlerr.Usage("acceptance takes no positional command; use %q", identity.Command("acceptance", "[OPTIONS]"))
	}
	if *output != "" && *resume != "" {
		return controlerr.Usage("--output and --resume are mutually exclusive")
	}
	if *resume != "" && (*prepare || *dryRun) {
		return controlerr.Usage("--resume cannot be combined with --prepare or --dry-run")
	}
	resultPath := ""
	var result acceptanceResult
	var err error
	if *resume != "" {
		resultPath, err = filepath.Abs(*resume)
		if err != nil {
			return err
		}
		if err := readAcceptanceResult(resultPath, &result); err != nil {
			return err
		}
		if err := validateAcceptanceEnvelope(result); err != nil {
			return err
		}
	} else if *output != "" {
		resultPath, err = absoluteNewPath(*output)
		if err != nil {
			return err
		}
		reportPath := strings.TrimSuffix(resultPath, filepath.Ext(resultPath)) + ".md"
		if _, err := absoluteNewPath(reportPath); err != nil {
			return err
		}
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
	if *resume != "" {
		if result.Definition.RenderNode != selectedNodes[0] || result.Definition.MemoryPolicy != *memory || result.Definition.KernelPolicy != *kernel || result.Definition.Port != port {
			return controlerr.New("acceptance checkpoint does not match the requested render node or runtime policy")
		}
		if *profileFlag != "auto" && result.Definition.Profile != *profileFlag {
			return controlerr.New("acceptance checkpoint does not match the requested profile")
		}
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
		if err := app.requireBenchmarkAgreements(managed, requiredBundles, *acceptLicense, true, *nonInteractive); err != nil {
			return err
		}
		terminal := app.terminal(app.Stdout)
		fmt.Fprintf(app.Stdout, "%s\n  %s  %s\n  %s  %s\n  %s  %s\n", terminal.Heading("Hardware acceptance"), terminal.Label(fmt.Sprintf("%-12s", "Profile")), *profileFlag, terminal.Label(fmt.Sprintf("%-12s", "Render node")), selectedNodes[0], terminal.Label(fmt.Sprintf("%-12s", "Data")), dataRoot)
		for _, image := range acceptanceImages(cases) {
			present, _ := app.podman().Exists(app.Context, "image", image.image)
			fmt.Fprintf(app.Stdout, "  %s  %s %s (%s)\n", terminal.Label(fmt.Sprintf("%-12s", "Image")), terminal.Command(fmt.Sprintf("%-10s", image.target)), image.image, terminal.State(map[bool]string{true: "ready", false: "build required"}[present]))
		}
		for _, bundle := range requiredBundles {
			state := "ready"
			if _, err := content.RequireBundle(managed, bundle, dataRoot); err != nil {
				state = "install required"
			}
			fmt.Fprintf(app.Stdout, "  %s  %s %s\n", terminal.Label(fmt.Sprintf("%-12s", "Content")), terminal.Command(fmt.Sprintf("%-54s", bundle.ID)), terminal.State(state))
		}
		fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Cases:"))
		for _, candidate := range cases {
			fmt.Fprintf(app.Stdout, "  %s %s\n", terminal.Command(fmt.Sprintf("%-16s", candidate.id)), candidate.description)
		}
		if *prepare {
			fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Muted("Preparation was planned but no image was built and no content was installed."))
		} else {
			fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Muted("No workload was started."))
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
		if err := app.commandBuild([]string{"pytorch-base"}); err != nil {
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
	if err := app.requireBenchmarkAgreements(managed, requiredBundles, *acceptLicense, false, *nonInteractive); err != nil {
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
	imageIdentities := make([]acceptanceImageIdentity, 0, len(images))
	for _, image := range images {
		identifier, inspectErr := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", image.image}, "inspect acceptance image "+image.image)
		if inspectErr != nil {
			return inspectErr
		}
		imageIdentities = append(imageIdentities, acceptanceImageIdentity{Target: image.target, Reference: image.image, ID: identifier})
	}
	if len(imageIdentities) == 0 || imageIdentities[0].ID != baseImageID {
		return controlerr.New("acceptance base image identity changed during preparation")
	}
	bundleIdentities := make([]acceptanceBundleIdentity, 0, len(requiredBundles))
	for _, bundle := range requiredBundles {
		identity := acceptanceBundleIdentity{ID: bundle.ID, Artifacts: make(map[string]string), Workflow: bundle.Workflow}
		for _, artifactID := range bundle.Artifacts {
			identity.Artifacts[artifactID] = managed.Artifacts[artifactID].SHA256
		}
		bundleIdentities = append(bundleIdentities, identity)
	}
	sourceIdentity, err := app.projectSourceIdentity()
	if err != nil {
		return err
	}
	definition := acceptanceDefinition{
		Profile: detected.ID, Architecture: hardware["Architecture"], RenderNode: selectedNodes[0],
		MemoryPolicy: *memory, KernelPolicy: *kernel, Port: port, ProjectRevision: sourceIdentity,
		Images: imageIdentities, Bundles: bundleIdentities, Cases: acceptanceCaseIDs(cases),
	}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		return err
	}
	if resultPath == "" {
		resultPath = benchmark.DefaultPath((storage.Layout{Root: dataRoot}).AcceptanceResults(), ".json")
	}
	if *resume != "" {
		if result.Fingerprint != fingerprint {
			return controlerr.New("acceptance checkpoint does not match this hardware and case selection")
		}
		if err := validateAcceptanceResult(result, cases); err != nil {
			return err
		}
	} else {
		entries := make([]acceptanceEntry, 0, len(cases))
		for _, candidate := range cases {
			entries = append(entries, acceptanceEntry{Identifier: candidate.id, Description: candidate.description, Application: candidate.application, Bundle: candidate.bundle, Visual: candidate.visual, Status: "pending", Artifacts: []acceptanceArtifact{}})
		}
		result = acceptanceResult{Schema: acceptanceSchema, SuiteID: time.Now().UTC().Format("20060102T150405Z") + "-" + benchmark.Identifier(), Fingerprint: fingerprint, Definition: definition, Status: "running", StartedAt: benchmark.Timestamp(), Hardware: hardware, Cases: entries}
		if err := benchmark.WriteNewCheckpoint(resultPath, result); err != nil {
			return err
		}
	}
	for index := range result.Cases {
		entry := &result.Cases[index]
		candidate, ok := findAcceptanceCase(cases, entry.Identifier)
		if !ok {
			return controlerr.New("acceptance checkpoint contains unknown case %q", entry.Identifier)
		}
		if entry.Status == "blocked" && candidate.visual && artifactsAvailable(entry.Artifacts) {
			entry.Status = "review"
		}
		if entry.Status == "pass" || entry.Status == "review" {
			continue
		}
		entry.Status, entry.Attempts, entry.StartedAt, entry.Reason = "running", entry.Attempts+1, benchmark.Timestamp(), ""
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
		started := time.Now()
		paths, caseErr := app.runAcceptanceCase(managed, candidate, dataRoot, detected.ID, selectedNodes[0], port, result.SuiteID, *memory, *kernel)
		artifacts, evidenceErr := captureAcceptanceArtifacts(paths)
		if caseErr == nil && evidenceErr != nil {
			caseErr = evidenceErr
		}
		entry.WallSeconds, entry.FinishedAt, entry.Artifacts = time.Since(started).Seconds(), benchmark.Timestamp(), artifacts
		if caseErr != nil {
			entry.Status, entry.Reason = "fail", caseErr.Error()
		} else if candidate.visual {
			entry.Status = "review"
		} else {
			entry.Status = "pass"
		}
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
	}
	for index := range result.Cases {
		entry := &result.Cases[index]
		if entry.Status != "review" {
			continue
		}
		candidate, _ := findAcceptanceCase(cases, entry.Identifier)
		accepted := false
		if !*nonInteractive {
			var promptErr error
			accepted, promptErr = app.confirmVisual(candidate, entry.Artifacts)
			if promptErr != nil {
				return checkpointThenReturn(resultPath, result, promptErr)
			}
		}
		if !accepted {
			entry.Status, entry.Reason = "blocked", "generated artifact requires successful visual review"
		} else {
			entry.Status, entry.Reason = "pass", ""
		}
		if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
			return err
		}
	}
	result.Status, result.FinishedAt = "pass", benchmark.Timestamp()
	for _, entry := range result.Cases {
		if entry.Status == "fail" {
			result.Status = "fail"
			break
		}
		if entry.Status == "blocked" {
			result.Status = "blocked"
		}
	}
	if err := benchmark.WriteCheckpoint(resultPath, result); err != nil {
		return err
	}
	reportPath := strings.TrimSuffix(resultPath, filepath.Ext(resultPath)) + ".md"
	reportPolicy := atomicfile.Create
	if *resume != "" {
		reportPolicy = atomicfile.ReplaceRegular
	}
	if err := atomicfile.Write(reportPath, []byte(renderAcceptanceReport(result)), 0o644, reportPolicy); err != nil {
		return err
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s %s (%s)\n%s %s\n", terminal.Success("Acceptance complete:"), resultPath, terminal.State(result.Status), terminal.Label("Report:"), reportPath)
	if result.Status != "pass" {
		status := 1
		if result.Status == "blocked" {
			status = 2
		}
		return &controlerr.Error{Message: fmt.Sprintf("acceptance status: %s", result.Status), Status: status}
	}
	return nil
}

func validateAcceptanceResult(result acceptanceResult, cases []acceptanceCase) error {
	if err := validateAcceptanceEnvelope(result); err != nil {
		return err
	}
	if len(result.Cases) != len(cases) {
		return controlerr.New("acceptance checkpoint has invalid root metadata")
	}
	for index, entry := range result.Cases {
		candidate := cases[index]
		if entry.Identifier != candidate.id || entry.Description != candidate.description || entry.Application != candidate.application || entry.Bundle != candidate.bundle || entry.Visual != candidate.visual {
			return controlerr.New("acceptance checkpoint case %d has invalid metadata", index+1)
		}
		if entry.Status == "pass" || entry.Status == "review" || entry.Status == "blocked" {
			needsEvidence := candidate.id == "comfyui-image" || candidate.id == "comfyui-video" || candidate.id == "llama-cpp"
			if needsEvidence && len(entry.Artifacts) == 0 {
				return controlerr.New("acceptance checkpoint case %q lacks result evidence", entry.Identifier)
			}
			if len(entry.Artifacts) > 0 && !artifactsAvailable(entry.Artifacts) {
				return controlerr.New("acceptance checkpoint case %q has missing or changed result evidence", entry.Identifier)
			}
		}
	}
	return nil
}

func validateAcceptanceEnvelope(result acceptanceResult) error {
	if result.Schema != acceptanceSchema || !managedRunID.MatchString(result.SuiteID) || result.StartedAt == "" {
		return controlerr.New("acceptance checkpoint has invalid root metadata")
	}
	if !map[string]bool{"running": true, "pass": true, "fail": true, "blocked": true}[result.Status] {
		return controlerr.New("acceptance checkpoint has invalid status %q", result.Status)
	}
	digest, err := jsonDigest(result.Definition)
	if err != nil || digest != result.Fingerprint {
		return controlerr.New("acceptance checkpoint definition does not match its fingerprint")
	}
	allowedStatus := map[string]bool{"pending": true, "running": true, "pass": true, "fail": true, "review": true, "blocked": true}
	seen := make(map[string]bool, len(result.Cases))
	for _, entry := range result.Cases {
		if entry.Identifier == "" || seen[entry.Identifier] || !allowedStatus[entry.Status] || entry.Attempts < 0 || unsafeCheckpointText(entry.Reason) {
			return controlerr.New("acceptance checkpoint has invalid case metadata")
		}
		seen[entry.Identifier] = true
		for _, artifact := range entry.Artifacts {
			if !filepath.IsAbs(artifact.Path) || unsafeCheckpointText(artifact.Path) || len(artifact.SHA256) != 64 || strings.Trim(artifact.SHA256, "0123456789abcdef") != "" || artifact.Size < 1 {
				return controlerr.New("acceptance checkpoint case %q has an invalid artifact path", entry.Identifier)
			}
		}
	}
	return nil
}

func unsafeCheckpointText(value string) bool {
	return strings.IndexFunc(value, func(character rune) bool { return character < 0x20 || character == 0x7f }) >= 0
}

func artifactsAvailable(artifacts []acceptanceArtifact) bool {
	if len(artifacts) == 0 {
		return false
	}
	for _, artifact := range artifacts {
		captured, err := captureAcceptanceArtifact(artifact.Path)
		if err != nil || captured != artifact {
			return false
		}
	}
	return true
}

func captureAcceptanceArtifacts(paths []string) ([]acceptanceArtifact, error) {
	artifacts := make([]acceptanceArtifact, 0, len(paths))
	for _, path := range paths {
		artifact, err := captureAcceptanceArtifact(path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func captureAcceptanceArtifact(path string) (acceptanceArtifact, error) {
	if !filepath.IsAbs(path) {
		return acceptanceArtifact{}, fmt.Errorf("acceptance evidence path is not absolute: %s", path)
	}
	status, err := os.Lstat(path)
	if err != nil || !status.Mode().IsRegular() {
		return acceptanceArtifact{}, fmt.Errorf("acceptance evidence is not a regular file: %s", path)
	}
	handle, err := os.Open(path)
	if err != nil {
		return acceptanceArtifact{}, err
	}
	digest := sha256.New()
	_, copyErr := io.Copy(digest, handle)
	closeErr := handle.Close()
	if copyErr != nil {
		return acceptanceArtifact{}, copyErr
	}
	if closeErr != nil {
		return acceptanceArtifact{}, closeErr
	}
	return acceptanceArtifact{Path: path, SHA256: hex.EncodeToString(digest.Sum(nil)), Size: status.Size()}, nil
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
	images := []struct{ target, image string }{{"pytorch-base", config.ROCmBaseImage}}
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

func findAcceptanceCase(cases []acceptanceCase, identifier string) (acceptanceCase, bool) {
	for _, candidate := range cases {
		if candidate.id == identifier {
			return candidate, true
		}
	}
	return acceptanceCase{}, false
}

func (app *App) runAcceptanceCase(managed catalog.Catalog, candidate acceptanceCase, dataRoot, profile, renderNode string, port int, suiteID, memory, kernel string) ([]string, error) {
	switch candidate.id {
	case "host-gpu":
		return []string{}, nil
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
		outputRoot := filepath.Join((storage.Layout{Root: dataRoot}).Application("comfyui"), "output", identity.StateNamespace+"-benchmarks", runID)
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
		return []string{outputs[0], path}, nil
	case "llama-cpp":
		preset := managed.LlamaPresets["qwen3-0.6b-q8-0"]
		artifact := managed.Artifacts[preset.Artifact]
		command := runtime.LlamaBenchmarkCommand(runtime.LlamaBenchmarkOptions{Image: configApplicationImage("llama-cpp"), Profile: profile, DataDir: dataRoot, Backend: "rocm", ManagedModel: artifact.Destination, RenderNodes: []string{renderNode}, Repetitions: 1, PromptTokens: 32, GenerationTokens: 16, BatchSize: 2048, UBatchSize: 512, CacheTypeK: "f16", CacheTypeV: "f16", FlashAttention: "auto"}, app.podman().SELinuxVolumeSuffix(app.Context))
		rows, err := benchmark.RunLlama(app.Context, app.Runner, command)
		if err != nil {
			return nil, err
		}
		path := filepath.Join((storage.Layout{Root: dataRoot}).AcceptanceResults(), "cases", suiteID+"-llama.json")
		if err := benchmark.WriteLlama(path, benchmark.LlamaRun{Image: benchmark.ImageIdentity{Reference: configApplicationImage("llama-cpp")}, Profile: profile, Backend: "rocm", RenderNodes: []string{renderNode}, Model: benchmark.ModelIdentity{Preset: preset.ID, Path: content.ArtifactPath(dataRoot, artifact)}, Parameters: benchmark.LlamaParameters{PromptTokens: 32, GenerationTokens: 16}, Results: rows}); err != nil {
			return nil, err
		}
		return []string{path}, nil
	case "dwarfstar":
		bundle := managed.Bundles[candidate.bundle]
		artifact := managed.Artifacts[bundle.Artifacts[0]]
		prompt := "Reply with exactly: DwarfStar acceptance passed"
		command, err := runtime.DwarfStarCommand(runtime.DwarfStarOptions{Image: configApplicationImage("dwarfstar"), Mode: "cli", DataDir: dataRoot, Model: content.ArtifactPath(dataRoot, artifact), RenderNodes: []string{renderNode}, Profile: profile, Context: 4096, OutputTokens: 64, Prompt: &prompt, NoThinking: true}, app.podman().SELinuxVolumeSuffix(app.Context))
		if err != nil {
			return nil, err
		}
		_, err = app.run(command, false)
		return []string{}, err
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

func (app *App) confirmVisual(candidate acceptanceCase, artifacts []acceptanceArtifact) (bool, error) {
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s\n", terminal.Heading("Visual review required for "+candidate.id+":"))
	for _, artifact := range artifacts {
		fmt.Fprintf(app.Stdout, "  %s\n", artifact.Path)
	}
	answer, err := app.promptLine("Does the generated artifact pass the documented smoke criteria? [y/N] ", true)
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", nil
}

func readAcceptanceResult(path string, result *acceptanceResult) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer handle.Close()
	decoder := json.NewDecoder(handle)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return controlerr.New("decode acceptance checkpoint %s: %v", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return controlerr.New("acceptance checkpoint contains trailing data: %s", path)
	}
	return nil
}

func renderAcceptanceReport(result acceptanceResult) string {
	var output strings.Builder
	fmt.Fprintf(&output, "# Hardware acceptance\n\n")
	fmt.Fprintf(&output, "**Result: %s.** Profile `%s` on `%s` using `%s`.\n\n", strings.ToUpper(result.Status), result.Definition.Profile, result.Definition.Architecture, result.Definition.RenderNode)
	fmt.Fprintf(&output, "- Suite: `%s`\n- Started: %s\n- Finished: %s\n- Memory policy: `%s`\n- Kernel policy: `%s`\n\n", result.SuiteID, result.StartedAt, result.FinishedAt, result.Definition.MemoryPolicy, result.Definition.KernelPolicy)
	fmt.Fprintln(&output, "| Case | Status | Attempts | Time | Evidence |")
	fmt.Fprintln(&output, "| --- | --- | ---: | ---: | --- |")
	for _, entry := range result.Cases {
		paths := make([]string, 0, len(entry.Artifacts))
		for _, artifact := range entry.Artifacts {
			paths = append(paths, artifact.Path)
		}
		evidence := strings.Join(paths, "<br>")
		if entry.Reason != "" {
			if evidence != "" {
				evidence += "<br>"
			}
			evidence += entry.Reason
		}
		fmt.Fprintf(&output, "| %s | %s | %d | %.2fs | %s |\n", entry.Identifier, entry.Status, entry.Attempts, entry.WallSeconds, evidence)
	}
	fmt.Fprint(&output, "\n## Exact inputs\n\n")
	fmt.Fprintf(&output, "Fingerprint: `%s`\n\n", result.Fingerprint)
	for _, image := range result.Definition.Images {
		fmt.Fprintf(&output, "- Image `%s`: `%s` (`%s`)\n", image.Target, image.Reference, image.ID)
	}
	for _, bundle := range result.Definition.Bundles {
		fmt.Fprintf(&output, "- Bundle `%s`\n", bundle.ID)
	}
	return output.String()
}
