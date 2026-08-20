package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/contentpack"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
	"paracetamol/internal/recipes"
	"paracetamol/internal/remoteimport"
	"paracetamol/internal/storage"
	"paracetamol/internal/ui"
	"paracetamol/internal/verification"
)

func (app *App) commandContent(args []string) error {
	if groupHelpRequested(args) {
		app.writeGroupHelp(usage("content", "COMMAND", "[OPTIONS]"),
			[2]string{"list [VIEW]", "list recipes, bundles, families, or models"},
			[2]string{"status", "inspect managed-content readiness"},
			[2]string{"install", "install verified managed content"},
			[2]string{"import", "resolve a reviewed remote file into a local pack"},
			[2]string{"workflows", "list, inspect, or install curated workflows"})
		return nil
	}
	if len(args) == 0 {
		return controlerr.Usage("choose content list, status, install, import, or workflows")
	}
	switch args[0] {
	case "list":
		return app.contentList(args[1:])
	case "status":
		return app.contentStatus(args[1:])
	case "install":
		return app.contentInstall(args[1:])
	case "import":
		return app.contentImport(args[1:])
	case "workflows":
		return app.contentWorkflows(args[1:])
	default:
		return controlerr.Usage("unknown content command %q", args[0])
	}
}

func (app *App) contentList(args []string) error {
	set := app.flags("content list", usage("content", "list", "[recipes|bundles|families|models]", "[OPTIONS]"))
	application := set.String("application", "", "filter exact bundles or models")
	details := set.Bool("details", false, "show model policy")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	var scans stringList
	set.Var(&scans, "scan", "additional GGUF file or directory")
	view, remaining := leadingPositional(args)
	if err := parseFlags(set, remaining); err != nil {
		return err
	}
	extras := set.Args()
	if view == "" && len(extras) > 0 {
		view, extras = extras[0], extras[1:]
	}
	if len(extras) > 0 {
		return controlerr.Usage("content list accepts at most one view")
	}
	if view == "" {
		view = "recipes"
	}
	if err := requireChoice(view, "content list view", "recipes", "bundles", "families", "models"); err != nil {
		return err
	}
	if (*details || *dataFlag != "" || len(scans) > 0) && view != "models" {
		return controlerr.Usage("--details, --data-dir, and --scan require the models view")
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	if view == "models" {
		if *application != "" && *application != "llama-cpp" {
			return controlerr.Usage("--models supports only --application llama-cpp")
		}
		dataRoot, err := app.resolveDataDir(*dataFlag, false)
		if err != nil {
			return err
		}
		return app.printModelInventory(managed, dataRoot, scans, *details)
	}
	if *application != "" && view != "bundles" {
		return controlerr.Usage("--application requires the bundles or models view")
	}
	if view == "bundles" {
		terminal := app.terminal(app.Stdout)
		identifiers := sortedBundleIDs(managed)
		fmt.Fprintln(app.Stdout, terminal.Heading("Exact bundles:"))
		for _, identifier := range identifiers {
			bundle := managed.Bundles[identifier]
			if *application != "" && bundle.Application != *application {
				continue
			}
			licenseState := "verified"
			if len(managed.BundleAgreements(bundle)) > 0 {
				licenseState = "TERMS"
			}
			for _, artifactID := range bundle.Artifacts {
				if managed.Artifacts[artifactID].License.Status != "verified" {
					if licenseState == "TERMS" {
						licenseState += "+UNVERIFIED"
					} else {
						licenseState = "UNVERIFIED"
					}
					break
				}
			}
			fmt.Fprintf(app.Stdout, "  %s %-10s %10s  %s %s\n", terminal.Command(fmt.Sprintf("%-54s", identifier)), bundle.Application, humanSize(managed.BundleSize(bundle)), terminal.State(fmt.Sprintf("%-18s", licenseState)), bundle.Description)
		}
		return nil
	}
	if view == "families" {
		terminal := app.terminal(app.Stdout)
		fmt.Fprintln(app.Stdout, terminal.Heading("Model families:"))
		for _, family := range []string{"qwen", "wan"} {
			selected, _ := selectBundles(managed, "family", family)
			fmt.Fprintf(app.Stdout, "  %s %2d bundles\n", terminal.Command(fmt.Sprintf("family %-8s", family)), len(selected))
		}
		fmt.Fprintf(app.Stdout, "\n%s\n  %s %2d bundles\n", terminal.Heading("Global:"), terminal.Command(fmt.Sprintf("%-14s", "all")), len(managed.Bundles))
		return nil
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Applications:"))
	for _, applicationID := range []string{"comfyui", "llama-cpp", "dwarfstar"} {
		values, _ := recipes.ForApplication(applicationID)
		fmt.Fprintf(app.Stdout, "  %s\n", terminal.Label(applicationID))
		for _, recipe := range values {
			fmt.Fprintf(app.Stdout, "    %s %d bundle(s)  %s\n", terminal.Command(fmt.Sprintf("%-24s", applicationID+" "+recipe.ID)), len(recipe.Bundles), recipe.Description)
		}
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Muted("Use 'content list models' for runnable models, 'bundles' for exact content, or 'families' for aggregates."))
	return nil
}

func (app *App) contentStatus(args []string) error {
	set := app.flags("content status", usage("content", "status", "[TARGET [SELECTION]]", "[--details]", "[--verify]", "[--data-dir PATH]"))
	details := set.Bool("details", false, "show every artifact")
	verifyHash := set.Bool("verify", false, "hash installed artifacts")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	target, remaining := leadingPositional(args)
	selection, remaining := leadingPositional(remaining)
	if err := parseFlags(set, remaining); err != nil {
		return err
	}
	positionals := set.Args()
	if target == "" && len(positionals) > 0 {
		target, positionals = positionals[0], positionals[1:]
	}
	if selection == "" && len(positionals) > 0 {
		selection, positionals = positionals[0], positionals[1:]
	}
	if len(positionals) > 0 {
		return controlerr.Usage("content status accepts at most a target and selection")
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	var bundles []catalog.Bundle
	if target == "" {
		for _, identifier := range sortedBundleIDs(managed) {
			bundles = append(bundles, managed.Bundles[identifier])
		}
	} else {
		bundles, err = selectBundles(managed, target, selection)
		if err != nil {
			return err
		}
	}
	dataRoot, err := app.resolveDataDir(*dataFlag, false)
	if err != nil {
		return err
	}
	store, err := verification.Load(dataRoot)
	if err != nil {
		return err
	}
	complete := true
	terminal := app.terminal(app.Stdout)
	for _, bundle := range bundles {
		statuses, inspectErr := app.inspectContentBundle(store, managed, bundle, dataRoot, *verifyHash)
		if inspectErr != nil {
			return inspectErr
		}
		ready := 0
		for _, status := range statuses {
			if content.Ready(status.State) {
				ready++
			}
		}
		summary := "partial"
		if ready == len(statuses) && (bundle.Workflow == "" || workflowReady(dataRoot, managed.Workflows[bundle.Workflow])) {
			summary = "ready"
		} else if ready == 0 && allMissing(statuses) {
			summary = "missing"
		}
		complete = complete && summary == "ready"
		if *details || *verifyHash {
			fmt.Fprintf(app.Stdout, "%s: %s\n", terminal.Command(bundle.ID), terminal.State(summary))
			for _, status := range statuses {
				fmt.Fprintf(app.Stdout, "  %s %s\n", terminal.State(fmt.Sprintf("%-14s", status.State)), status.Artifact.Destination)
			}
			if bundle.Workflow != "" {
				state := "missing"
				if workflowReady(dataRoot, managed.Workflows[bundle.Workflow]) {
					state = "installed"
				}
				fmt.Fprintf(app.Stdout, "  %s workflow/%s\n", terminal.State(fmt.Sprintf("%-14s", state)), managed.Workflows[bundle.Workflow].Destination)
			}
		} else {
			itemCount := len(statuses)
			if bundle.Workflow != "" {
				itemCount++
				if workflowReady(dataRoot, managed.Workflows[bundle.Workflow]) {
					ready++
				}
			}
			fmt.Fprintf(app.Stdout, "%s %s %d/%d items\n", terminal.State(fmt.Sprintf("%-10s", summary)), terminal.Command(fmt.Sprintf("%-54s", bundle.ID)), ready, itemCount)
		}
	}
	if !complete {
		return &controlerr.Error{Message: "some selected content is not ready", Status: 1}
	}
	return nil
}

func (app *App) inspectContentBundle(store *verification.Store, managed catalog.Catalog, bundle catalog.Bundle, dataRoot string, verifyHash bool) ([]content.ArtifactStatus, error) {
	if !verifyHash {
		return content.InspectBundle(store, managed, bundle, dataRoot, false)
	}
	statuses := make([]content.ArtifactStatus, 0, len(bundle.Artifacts))
	for index, identifier := range bundle.Artifacts {
		artifact := managed.Artifacts[identifier]
		initial, err := content.InspectArtifact(store, dataRoot, artifact, false)
		if err != nil {
			return nil, err
		}
		if initial.State != content.Verified && initial.State != content.Unverified {
			statuses = append(statuses, initial)
			continue
		}
		terminal := app.terminal(app.Stdout)
		fmt.Fprintf(app.Stdout, "\n%s SHA-256 for %s (%s)\n", terminal.Heading("Verifying"), artifact.Destination, humanSize(artifact.Size))
		progress := ui.NewProgress(app.Stdout, app.Environment, artifact.Size)
		progress.Update(artifact.Destination, index+1, len(bundle.Artifacts), 0, false)
		// An explicit status audit reports live bytes but does not refresh the
		// installer's durable receipt; content install owns that mutation.
		status, verifyErr := content.VerifyArtifact(app.Context, nil, dataRoot, artifact, func(hashed int64) {
			progress.Update(artifact.Destination, index+1, len(bundle.Artifacts), hashed, false)
		})
		if verifyErr == nil {
			progress.Update(artifact.Destination, index+1, len(bundle.Artifacts), artifact.Size, true)
		} else {
			progress.Finish()
			return nil, verifyErr
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (app *App) contentInstall(args []string) error {
	set := app.flagsWithExamples("content install", usage("content", "install", "TARGET", "[SELECTION]", "[OPTIONS]"), []string{
		identity.Command("content", "install"),
		identity.Command("content", "install", "llama-cpp", "qwen3.8"),
		identity.Command("content", "install", "llama-cpp", "all", "--dry-run"),
		identity.Command("content", "install", "llama-cpp", "all", "--local-mirror", "/path/to/old-data", "--local-mirror-move", "--accept-license"),
	})
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "content-tools image")
	dryRun := set.Bool("dry-run", false, "print the validated plan")
	acceptLicense := set.Bool("accept-license", false, "accept named model agreements")
	acknowledgeRisk := set.Bool("acknowledge-license-risk", false, "acknowledge NOASSERTION content")
	forceWorkflow := set.Bool("force-workflow", false, "replace a differing curated workflow")
	localMirror := set.String("local-mirror", "", "reuse exact verified files")
	localMirrorMove := set.Bool("local-mirror-move", false, "move reused mirror files")
	nonInteractive := set.Bool("non-interactive", false, "refuse interactive choices")
	var packFiles stringList
	set.Var(&packFiles, "from-file", "ignored local content pack; repeatable")
	target, remaining := leadingPositional(args)
	selection, remaining := leadingPositional(remaining)
	if err := parseFlags(set, remaining); err != nil {
		return err
	}
	extras := set.Args()
	if target == "" && len(extras) > 0 {
		target, extras = extras[0], extras[1:]
	}
	if selection == "" && len(extras) > 0 {
		selection, extras = extras[0], extras[1:]
	}
	if len(extras) > 0 {
		return controlerr.Usage("content install accepts at most a target and selection")
	}
	if target == "" && len(packFiles) == 0 && !*nonInteractive && terminalReader(app.Stdin) {
		selected, err := app.guidedContentSelection()
		if err != nil {
			return err
		}
		target, selection = selected.Application, selected.ID
	}
	if target == "" && len(packFiles) == 0 {
		return controlerr.Usage("content install requires a complete target; use 'content list recipes'")
	}
	if len(packFiles) > 0 && (target != "" || selection != "") {
		return controlerr.Usage("--from-file cannot be combined with an explicit target")
	}
	if *localMirrorMove && *localMirror == "" {
		return controlerr.Usage("--local-mirror-move requires --local-mirror")
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	var bundles []catalog.Bundle
	if len(packFiles) > 0 {
		var ids []string
		managed, ids, err = contentpack.Load(managed, packFiles)
		if err != nil {
			return err
		}
		for _, id := range ids {
			bundles = append(bundles, managed.Bundles[id])
		}
		target = "local content packs"
	} else {
		bundles, err = selectBundles(managed, target, selection)
		if err != nil {
			return err
		}
	}
	agreements := uniqueAgreements(managed, bundles)
	dataRoot, err := app.resolveDataDir(*dataFlag, !*dryRun)
	if err != nil {
		return err
	}
	artifacts := content.UniqueArtifacts(managed, bundles)
	plan, err := content.Plan(dataRoot, artifacts)
	if err != nil {
		return err
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Content installation"))
	fmt.Fprintf(app.Stdout, "  %s %d bundles, %d unique artifacts\n", terminal.Label("Selected:"), len(bundles), len(artifacts))
	fmt.Fprintf(app.Stdout, "  %s %s\n", terminal.Label("Data:"), dataRoot)
	fmt.Fprintf(app.Stdout, "  %s %s\n", terminal.Label("Ready:"), terminal.State(fmt.Sprintf("%d/%d", plan.Ready, len(artifacts))))
	fmt.Fprintf(app.Stdout, "  %s %s\n", terminal.Label("Download:"), humanSize(plan.DownloadBytes))
	fmt.Fprintf(app.Stdout, "  %s %s\n", terminal.Label("Verify:"), humanSize(plan.VerifyBytes))
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Bundles:"))
	for _, bundle := range bundles {
		fmt.Fprintf(app.Stdout, "  %s — %s\n", terminal.Command(bundle.ID), bundle.Description)
	}
	app.printContentAgreements(agreements)
	for _, artifact := range plan.Risky {
		warningTerminal := app.terminal(app.Stderr)
		fmt.Fprintf(app.Stderr, "%s %s has NOASSERTION licensing: %s\n", warningTerminal.Warning("WARNING:"), artifact.ID, artifact.License.Warning)
	}
	if *dryRun {
		return nil
	}
	acknowledged, err := app.confirmContentApprovals(agreements, plan.Risky, *acceptLicense, *acknowledgeRisk, *nonInteractive)
	if err != nil {
		return err
	}
	image := firstNonEmpty(*imageFlag, config.EnvironmentValue(app.Environment, "IMAGE", config.ContentToolsImage))
	if err := content.Install(content.InstallOptions{Context: app.Context, DataRoot: dataRoot, Artifacts: artifacts, Image: image, AcceptRisk: acknowledged, LocalMirror: *localMirror, MoveFromMirror: *localMirrorMove, Environment: app.Environment, Runner: app.Runner, Output: app.Stdout}); err != nil {
		return err
	}
	for _, workflowID := range selectedWorkflowIDs(bundles) {
		if err := app.installWorkflow(managed.Workflows[workflowID], dataRoot, *forceWorkflow); err != nil {
			return err
		}
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Success("All selected content is verified and ready."))
	if target == "comfyui" || target == "llama-cpp" || target == "dwarfstar" {
		if recipe, recipeErr := recipes.Find(target, selection); recipeErr == nil {
			terminal.Next(recipe.NextCommand())
		}
	}
	return nil
}

func (app *App) confirmContentApprovals(agreements []catalog.Agreement, risky []catalog.Artifact, accepted, acknowledged, nonInteractive bool) (bool, error) {
	interactive := !nonInteractive && terminalReader(app.Stdin)
	if len(agreements) > 0 && !accepted {
		var names []string
		for _, agreement := range agreements {
			names = append(names, agreement.Name)
		}
		if !interactive {
			return acknowledged, controlerr.New("selection is governed by %s; review the URLs in the plan and repeat with --accept-license", strings.Join(names, ", "))
		}
		warningTerminal := app.terminal(app.Stderr)
		fmt.Fprintf(app.Stderr, "\n%s The selected content is governed by %s.\n", warningTerminal.Warning("Terms:"), strings.Join(names, ", "))
		answer, err := app.promptLine("Confirm that you accept these terms and are permitted to use the models in your location? [y/N] ", true)
		if err != nil {
			return acknowledged, err
		}
		if normalized := strings.ToLower(answer); normalized != "y" && normalized != "yes" {
			return acknowledged, controlerr.New("model license acceptance declined")
		}
	}
	if len(risky) > 0 && !acknowledged {
		if !interactive {
			return false, controlerr.New("selected missing content has unverified licensing; review the warnings in the plan and repeat with --acknowledge-license-risk")
		}
		answer, err := app.promptLine("Acknowledge the unresolved licensing and continue with these downloads? [y/N] ", true)
		if err != nil {
			return false, err
		}
		if normalized := strings.ToLower(answer); normalized != "y" && normalized != "yes" {
			return false, controlerr.New("unverified-license acknowledgment declined")
		}
		acknowledged = true
	}
	return acknowledged, nil
}

func (app *App) printContentAgreements(agreements []catalog.Agreement) {
	if len(agreements) == 0 {
		return
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Warning("License agreements:"))
	for _, agreement := range agreements {
		fmt.Fprintf(app.Stdout, "  %s — %s\n    %s\n", terminal.Label(agreement.Name), agreement.Summary, terminal.Command(agreement.URL))
	}
}

func (app *App) guidedContentSelection() (recipes.Recipe, error) {
	if !terminalReader(app.Stdin) {
		return recipes.Recipe{}, controlerr.Usage("content install requires a target when standard input is not a terminal")
	}
	var choices []recipes.Recipe
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Choose managed content:"))
	for _, application := range []string{"comfyui", "llama-cpp", "dwarfstar"} {
		values, _ := recipes.ForApplication(application)
		for _, recipe := range values {
			choices = append(choices, recipe)
			fmt.Fprintf(app.Stdout, "  %2d. %s %s %s\n", len(choices), terminal.Label(fmt.Sprintf("%-22s", application)), terminal.Command(fmt.Sprintf("%-26s", recipe.ID)), recipe.Description)
		}
	}
	answer, err := app.promptLine("Selection (or q to cancel): ", true)
	if err != nil {
		return recipes.Recipe{}, err
	}
	if strings.EqualFold(answer, "q") {
		return recipes.Recipe{}, controlerr.New("content selection cancelled")
	}
	index, err := strconv.Atoi(answer)
	if err != nil || index < 1 || index > len(choices) {
		return recipes.Recipe{}, controlerr.Usage("content selection must be a number from 1 through %d", len(choices))
	}
	return choices[index-1], nil
}

func selectBundles(managed catalog.Catalog, target, selection string) ([]catalog.Bundle, error) {
	ids := sortedBundleIDs(managed)
	if target == "all" {
		if selection != "" {
			return nil, controlerr.Usage("all does not accept a selection")
		}
		result := make([]catalog.Bundle, 0, len(ids))
		for _, id := range ids {
			result = append(result, managed.Bundles[id])
		}
		return result, nil
	}
	if target == "family" {
		if selection != "qwen" && selection != "wan" {
			return nil, controlerr.Usage("family requires qwen or wan")
		}
		var result []catalog.Bundle
		for _, id := range ids {
			bundle := managed.Bundles[id]
			if stringSliceContains(bundle.Groups, selection) {
				result = append(result, bundle)
			}
		}
		return result, nil
	}
	if target == "comfyui" || target == "llama-cpp" || target == "dwarfstar" {
		if selection == "" {
			return nil, controlerr.Usage("%s requires a recipe or all", target)
		}
		if selection == "all" {
			var result []catalog.Bundle
			for _, id := range ids {
				if bundle := managed.Bundles[id]; bundle.Application == target {
					result = append(result, bundle)
				}
			}
			return result, nil
		}
		recipe, err := recipes.Find(target, selection)
		if err != nil {
			return nil, controlerr.Usage("%v", err)
		}
		result := make([]catalog.Bundle, 0, len(recipe.Bundles))
		for _, id := range recipe.Bundles {
			bundle, ok := managed.Bundles[id]
			if !ok || bundle.Application != target {
				return nil, fmt.Errorf("recipe references invalid bundle %s", id)
			}
			result = append(result, bundle)
		}
		return result, nil
	}
	if selection != "" {
		return nil, controlerr.Usage("exact bundle %q does not accept a selection", target)
	}
	bundle, ok := managed.Bundles[target]
	if !ok {
		return nil, controlerr.Usage("unknown content bundle %q", target)
	}
	return []catalog.Bundle{bundle}, nil
}

func sortedBundleIDs(managed catalog.Catalog) []string {
	result := make([]string, 0, len(managed.Bundles))
	for identifier := range managed.Bundles {
		result = append(result, identifier)
	}
	sort.Strings(result)
	return result
}

func uniqueAgreements(managed catalog.Catalog, bundles []catalog.Bundle) []catalog.Agreement {
	seen := make(map[string]bool)
	var result []catalog.Agreement
	for _, bundle := range bundles {
		for _, agreement := range managed.BundleAgreements(bundle) {
			if !seen[agreement.ID] {
				seen[agreement.ID] = true
				result = append(result, agreement)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func selectedWorkflowIDs(bundles []catalog.Bundle) []string {
	seen := make(map[string]bool)
	var result []string
	for _, bundle := range bundles {
		if bundle.Workflow != "" && !seen[bundle.Workflow] {
			seen[bundle.Workflow] = true
			result = append(result, bundle.Workflow)
		}
	}
	return result
}

func workflowPath(dataRoot string, workflow catalog.Workflow) string {
	return filepath.Join((storage.Layout{Root: dataRoot}).CuratedWorkflows(), workflow.Destination)
}

func workflowReady(dataRoot string, workflow catalog.Workflow) bool {
	if workflow.ID == "" {
		return true
	}
	path := workflowPath(dataRoot, workflow)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	digest, err := sha256Regular(path)
	return err == nil && digest == workflow.RenderedSHA256
}

func sha256Regular(path string) (string, error) {
	handle, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, handle); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func allMissing(statuses []content.ArtifactStatus) bool {
	for _, status := range statuses {
		if status.State != content.Missing {
			return false
		}
	}
	return true
}

func stringSliceContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (app *App) printModelInventory(managed catalog.Catalog, dataRoot string, scans []string, details bool) error {
	store, err := verification.Load(dataRoot)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(managed.LlamaPresets))
	for id := range managed.LlamaPresets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Managed llama.cpp presets:"))
	for _, id := range ids {
		preset := managed.LlamaPresets[id]
		bundle := managed.Bundles[preset.Bundle]
		statuses, inspectErr := content.InspectBundle(store, managed, bundle, dataRoot, false)
		if inspectErr != nil {
			return inspectErr
		}
		state := "ready"
		for _, status := range statuses {
			if !content.Ready(status.State) {
				state = "missing"
				if status.State != content.Missing {
					state = string(status.State)
				}
				break
			}
		}
		fmt.Fprintf(app.Stdout, "  %s %s %s\n", terminal.State(fmt.Sprintf("%-12s", state)), terminal.Command(fmt.Sprintf("%-48s", id)), content.ArtifactPath(dataRoot, managed.Artifacts[preset.Artifact]))
		if details {
			fmt.Fprintf(app.Stdout, "    %s\n", terminal.Muted(fmt.Sprintf("context=%d tools=%t reasoning=%s default=%s levels=%s template=%s speculation=%s", preset.DefaultContext, preset.AgentTools, firstNonEmpty(preset.ReasoningControl, "none"), preset.ReasoningDefault, strings.Join(preset.ReasoningLevels, ","), firstNonEmpty(preset.ChatTemplate, "model metadata"), firstNonEmpty(preset.SpeculativeType, "off"))))
		}
	}
	if len(scans) == 0 {
		return nil
	}
	fmt.Fprintln(app.Stdout, terminal.Heading("Loose GGUF models:"))
	for _, raw := range scans {
		resolved, err := filepath.Abs(raw)
		if err != nil {
			return err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return controlerr.New("cannot scan %s: %v", resolved, err)
		}
		if info.Mode().IsRegular() {
			if strings.EqualFold(filepath.Ext(resolved), ".gguf") {
				fmt.Fprintf(app.Stdout, "  %s %s (%s)\n", terminal.State(fmt.Sprintf("%-12s", "ready")), resolved, humanSize(info.Size()))
			}
			continue
		}
		err = filepath.WalkDir(resolved, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 && entry.IsDir() {
				return filepath.SkipDir
			}
			if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".gguf") {
				fmt.Fprintf(app.Stdout, "  %s %s\n", terminal.State(fmt.Sprintf("%-12s", "ready")), path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (app *App) contentWorkflows(args []string) error {
	if groupHelpRequested(args) {
		app.writeGroupHelp(usage("content", "workflows", "COMMAND", "[OPTIONS]"),
			[2]string{"list", "list curated workflows"},
			[2]string{"status", "inspect installed curated workflows"},
			[2]string{"install", "install one exact curated workflow"})
		return nil
	}
	if len(args) == 0 {
		return controlerr.Usage("choose content workflows list, status, or install")
	}
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(managed.Workflows))
	for id := range managed.Workflows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	terminal := app.terminal(app.Stdout)
	switch args[0] {
	case "list":
		set := app.flags("content workflows list", usage("content", "workflows", "list"))
		if err := parseFlags(set, args[1:]); err != nil {
			return err
		}
		if len(set.Args()) > 0 {
			return controlerr.Usage("content workflows list accepts no positional arguments")
		}
		fmt.Fprintln(app.Stdout, terminal.Heading("Curated workflows:"))
		for _, id := range ids {
			workflow := managed.Workflows[id]
			fmt.Fprintf(app.Stdout, "  %s %s\n", terminal.Command(fmt.Sprintf("%-48s", id)), workflow.Description)
		}
		return nil
	case "status":
		set := app.flags("content workflows status", usage("content", "workflows", "status", "[WORKFLOW]", "[--data-dir PATH]"))
		dataFlag := set.String("data-dir", "", "persistent data directory")
		id, remaining := leadingPositional(args[1:])
		if err := parseFlags(set, remaining); err != nil {
			return err
		}
		if id == "" && len(set.Args()) > 0 {
			id = set.Args()[0]
			if len(set.Args()) > 1 {
				return controlerr.Usage("workflow status accepts at most one workflow")
			}
		} else if len(set.Args()) > 0 {
			return controlerr.Usage("workflow status accepts at most one workflow")
		}
		if id != "" {
			if _, ok := managed.Workflows[id]; !ok {
				return controlerr.Usage("unknown workflow %q", id)
			}
			ids = []string{id}
		}
		dataRoot, err := app.resolveDataDir(*dataFlag, false)
		if err != nil {
			return err
		}
		allReady := true
		for _, id := range ids {
			state := "missing"
			path := workflowPath(dataRoot, managed.Workflows[id])
			if info, statErr := os.Lstat(path); statErr == nil {
				if !info.Mode().IsRegular() {
					state = "unexpected"
				} else if workflowReady(dataRoot, managed.Workflows[id]) {
					state = "installed"
				} else {
					state = "modified"
				}
			}
			allReady = allReady && state == "installed"
			fmt.Fprintf(app.Stdout, "%s %s %s\n", terminal.State(fmt.Sprintf("%-12s", state)), terminal.Command(fmt.Sprintf("%-48s", id)), path)
		}
		if !allReady {
			return &controlerr.Error{Message: "some workflows are not installed", Status: 1}
		}
		return nil
	case "install":
		set := app.flags("content workflows install", usage("content", "workflows", "install", "WORKFLOW", "[--force]", "[--data-dir PATH]"))
		dataFlag := set.String("data-dir", "", "persistent data directory")
		force := set.Bool("force", false, "replace a differing workflow")
		id, remaining := leadingPositional(args[1:])
		if err := parseFlags(set, remaining); err != nil {
			return err
		}
		if id == "" && len(set.Args()) > 0 {
			id = set.Args()[0]
			if len(set.Args()) > 1 {
				return controlerr.Usage("workflow install accepts exactly one workflow")
			}
		} else if len(set.Args()) > 0 {
			return controlerr.Usage("workflow install accepts exactly one workflow")
		}
		workflow, ok := managed.Workflows[id]
		if id == "" || !ok {
			return controlerr.Usage("choose an exact workflow")
		}
		dataRoot, err := app.resolveDataDir(*dataFlag, true)
		if err != nil {
			return err
		}
		return app.installWorkflow(workflow, dataRoot, *force)
	default:
		return controlerr.Usage("unknown workflows command %q", args[0])
	}
}

func (app *App) contentImport(args []string) error {
	set := app.flags("content import", usage("content", "import", "URL", "[OPTIONS]"))
	version := set.Int64("version", 0, "exact Civitai model-version ID")
	fileSelector := set.String("file", "", "provider file ID, name, or path")
	kindSelector := set.String("as", "", "explicit destination type")
	savePack := set.String("save-pack", "", "local content pack path")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageFlag := set.String("image", "", "content-tools image")
	dryRun := set.Bool("dry-run", false, "resolve without saving or downloading")
	nonInteractive := set.Bool("non-interactive", false, "require explicit ambiguous choices")
	acknowledge := set.Bool("acknowledge-license-risk", false, "allow NOASSERTION content")
	rawURL, remaining := leadingPositional(args)
	if err := parseFlags(set, remaining); err != nil {
		return err
	}
	if rawURL == "" && len(set.Args()) > 0 {
		rawURL = set.Args()[0]
		if len(set.Args()) > 1 {
			return controlerr.Usage("content import accepts exactly one URL")
		}
	} else if len(set.Args()) > 0 {
		return controlerr.Usage("content import accepts exactly one URL")
	}
	if *version < 0 {
		return controlerr.Usage("--version must be positive")
	}
	if rawURL == "" {
		if *nonInteractive || !terminalReader(app.Stdin) {
			return controlerr.Usage("content import requires URL in noninteractive use")
		}
		entered, promptErr := app.promptLine("Civitai or Hugging Face URL: ", true)
		if promptErr != nil {
			return promptErr
		}
		rawURL = entered
		if rawURL == "" {
			return controlerr.Usage("remote content URL is required")
		}
	}
	provider, err := remoteimport.Provider(rawURL)
	if err != nil {
		return err
	}
	if provider != "civitai" && *version != 0 {
		return controlerr.Usage("--version is only valid for Civitai URLs")
	}
	if provider == "civitai" && *version == 0 {
		choices, err := remoteimport.CivitaiVersions(app.Context, rawURL, app.Environment["CIVITAI_TOKEN"])
		if err != nil {
			return err
		}
		if len(choices) == 1 {
			fmt.Sscan(choices[0][0], version)
		} else if len(choices) > 1 {
			if *nonInteractive || !terminalReader(app.Stdin) {
				return controlerr.New("Civitai page has several versions; repeat with --version ID")
			}
			terminal := app.terminal(app.Stdout)
			fmt.Fprintln(app.Stdout, terminal.Heading("Choose a Civitai model version:"))
			for index, choice := range choices {
				fmt.Fprintf(app.Stdout, "  %2d. %s — %s\n", index+1, terminal.Command(choice[0]), choice[1])
			}
			selected, err := promptIndex(app, "Civitai model version", len(choices))
			if err != nil {
				return err
			}
			fmt.Sscan(choices[selected][0], version)
		}
	}
	discovery, err := remoteimport.Discover(app.Context, rawURL, *version, app.Environment["HF_TOKEN"], app.Environment["CIVITAI_TOKEN"])
	if err != nil {
		return err
	}
	var selected remoteimport.File
	if *fileSelector != "" {
		selected, err = remoteimport.SelectFile(discovery, *fileSelector)
	} else if automatic, ok := remoteimport.AutomaticFile(discovery); ok {
		selected = automatic
	} else {
		if *nonInteractive || !terminalReader(app.Stdin) {
			var names []string
			for _, file := range discovery.Files {
				names = append(names, file.ID)
			}
			return controlerr.New("remote source has several files; repeat with --file (choose %s)", strings.Join(names, ", "))
		}
		terminal := app.terminal(app.Stdout)
		fmt.Fprintln(app.Stdout, terminal.Heading("Choose a remote file:"))
		for index, file := range discovery.Files {
			fmt.Fprintf(app.Stdout, "  %2d. %s (%s)%s\n", index+1, terminal.Command(file.Name), humanSize(file.Size), map[bool]string{true: " — primary"}[file.Primary])
		}
		index, promptErr := promptIndex(app, "Remote file", len(discovery.Files))
		if promptErr != nil {
			return promptErr
		}
		selected = discovery.Files[index]
	}
	if err != nil {
		return err
	}
	var kind remoteimport.Kind
	if *kindSelector != "" {
		kind, err = remoteimport.SelectKind(*kindSelector, selected)
	} else {
		candidates := remoteimport.CandidateKinds(discovery, selected)
		if len(candidates) == 1 {
			kind = candidates[0]
		} else {
			if len(candidates) == 0 {
				return controlerr.New("provider metadata does not map %q safely to a supported destination", selected.Name)
			}
			if *nonInteractive || !terminalReader(app.Stdin) {
				var ids []string
				for _, candidate := range candidates {
					ids = append(ids, candidate.ID)
				}
				return controlerr.New("cannot infer destination; repeat with --as (choose %s)", strings.Join(ids, ", "))
			}
			terminal := app.terminal(app.Stdout)
			fmt.Fprintln(app.Stdout, terminal.Heading("Choose an installation type:"))
			for index, candidate := range candidates {
				fmt.Fprintf(app.Stdout, "  %2d. %s — %s\n", index+1, terminal.Command(candidate.ID), candidate.Label)
			}
			index, promptErr := promptIndex(app, "Install as", len(candidates))
			if promptErr != nil {
				return promptErr
			}
			kind = candidates[index]
		}
	}
	if err != nil {
		return err
	}
	plan, err := remoteimport.BuildPlan(discovery, selected, kind)
	if err != nil {
		return err
	}
	packPath := *savePack
	if packPath == "" {
		packPath = filepath.Join(app.Root, "local-content", "imports", plan.Bundle.ID+".json")
	}
	packPath, err = filepath.Abs(packPath)
	if err != nil {
		return err
	}
	if strings.ToLower(filepath.Ext(packPath)) != ".json" {
		return controlerr.Usage("import pack path must end in .json")
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Remote import:"))
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-12s", "Source:")), discovery.SourceURL)
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-12s", "Title:")), discovery.Title)
	fmt.Fprintf(app.Stdout, "  %s  %s (%s)\n", terminal.Label(fmt.Sprintf("%-12s", "File:")), selected.Name, humanSize(selected.Size))
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-12s", "SHA-256:")), selected.SHA256)
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-12s", "Install as:")), kind.Label)
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-12s", "Destination:")), plan.Artifact.Destination)
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label(fmt.Sprintf("%-12s", "Local pack:")), packPath)
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Warning(fmt.Sprintf("%-12s", "License:")), "NOASSERTION; hosted-file rights are unverified")
	if *dryRun {
		return nil
	}
	if !*acknowledge {
		return controlerr.New("remote imports have NOASSERTION licensing; review the plan and repeat with --acknowledge-license-risk")
	}
	pack, err := remoteimport.PackBytes(plan)
	if err != nil {
		return err
	}
	if err := saveImportPack(packPath, pack); err != nil {
		return err
	}
	fmt.Fprintf(app.Stdout, "%s %s\n", terminal.Success("Saved local pack:"), packPath)
	dataRoot, err := app.resolveDataDir(*dataFlag, true)
	if err != nil {
		return err
	}
	image := firstNonEmpty(*imageFlag, config.EnvironmentValue(app.Environment, "IMAGE", config.ContentToolsImage))
	if err := content.Install(content.InstallOptions{Context: app.Context, DataRoot: dataRoot, Artifacts: []catalog.Artifact{plan.Artifact}, Image: image, AcceptRisk: true, Environment: app.Environment, Runner: app.Runner, Output: app.Stdout}); err != nil {
		return err
	}
	if kind.Application == "llama-cpp" {
		terminal.Next(identity.Command("run", "llama-cpp", "server", "--model", content.ArtifactPath(dataRoot, plan.Artifact)))
	} else {
		terminal.Next(identity.Command("run", kind.Application))
	}
	return nil
}

func promptIndex(app *App, label string, count int) (int, error) {
	line, err := app.promptLine(fmt.Sprintf("%s [1-%d]: ", label, count), true)
	if err != nil {
		return 0, err
	}
	value := 0
	if _, err := fmt.Sscan(line, &value); err != nil || value < 1 || value > count {
		return 0, controlerr.Usage("selection must be a number between 1 and %d", count)
	}
	return value - 1, nil
}

func saveImportPack(path string, contents []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return controlerr.New("refusing unexpected import pack path: %s", path)
		}
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(current) != string(contents) {
			return controlerr.New("import pack exists with different content: %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicfile.Write(path, contents, 0o600, atomicfile.Create)
}

func (app *App) installWorkflow(workflow catalog.Workflow, dataRoot string, force bool) error {
	destination := workflowPath(dataRoot, workflow)
	managedRoot := (storage.Layout{Root: dataRoot}).CuratedWorkflows()
	if err := storage.ValidateManagedParent(destination, managedRoot, dataRoot, "curated workflow"); err != nil {
		return err
	}
	if workflowReady(dataRoot, workflow) {
		fmt.Fprintf(app.Stdout, "%s %s\n", app.terminal(app.Stdout).Success("Workflow already installed:"), destination)
		return nil
	}
	replace := false
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return controlerr.New("workflow path is not a regular file: %s", destination)
		}
		if !force {
			return controlerr.New("workflow differs from the curated version: %s; use --force to replace it", destination)
		}
		replace = true
	} else if !os.IsNotExist(err) {
		return err
	}
	resource := filepath.Join(app.Root, "catalog", "workflows", workflow.ID+".json")
	contents, err := os.ReadFile(resource)
	if err != nil {
		return fmt.Errorf("read curated workflow resource: %w", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(contents))
	if digest != workflow.RenderedSHA256 {
		return controlerr.New("curated workflow resource does not match catalog: %s", workflow.ID)
	}
	if err := storage.ValidateManagedParent(destination, managedRoot, dataRoot, "curated workflow"); err != nil {
		return err
	}
	policy := atomicfile.Create
	if replace {
		policy = atomicfile.ReplaceRegular
	}
	if err := atomicfile.Write(destination, contents, 0o644, policy); err != nil {
		return err
	}
	fmt.Fprintf(app.Stdout, "%s %s\n", app.terminal(app.Stdout).Success("Installed workflow:"), destination)
	return nil
}
