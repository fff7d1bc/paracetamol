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
	"paracetamol/internal/modelinventory"
	"paracetamol/internal/recipes"
	"paracetamol/internal/remoteimport"
	"paracetamol/internal/storage"
	"paracetamol/internal/ui"
	"paracetamol/internal/verification"
)

func (app *App) commandContent(args []string) error {
	writeHelp := func() {
		app.writeGroupHelp(usage("content", "COMMAND", "[OPTIONS]"),
			[2]string{"list [VIEW]", "list recipes, bundles, families, or models"},
			[2]string{"status", "inspect managed-content readiness"},
			[2]string{"install", "install verified managed content"},
			[2]string{"import", "resolve a reviewed remote file into a local pack"},
			[2]string{"workflows", "list, inspect, or install curated workflows"})
	}
	if groupHelpRequested(args) {
		writeHelp()
		return nil
	}
	if len(args) == 0 {
		writeHelp()
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
		writeHelp()
		return controlerr.Usage("unknown content command %q", args[0])
	}
}

func (app *App) contentList(args []string) error {
	set := app.flags("content list", usage("content", "list", "[recipes|bundles|families|models]", "[OPTIONS]"))
	set.Argument("VIEW", "recipes (default), bundles, families, or runnable models")
	application := set.String("application", "", "filter bundles or models by consuming application")
	details := set.Bool("details", false, "show complete managed model runtime policy")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	var scans stringList
	set.Var(&scans, "scan", "additional llama.cpp GGUF file or directory; repeatable")
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
		if *application != "" {
			if err := requireChoice(*application, "model inventory application", "llama-cpp", "dwarfstar"); err != nil {
				return err
			}
		}
		if len(scans) > 0 && *application == "dwarfstar" {
			return controlerr.Usage("--scan applies only to llama.cpp GGUF models")
		}
		dataRoot, err := app.resolveDataDir(*dataFlag, false)
		if err != nil {
			return err
		}
		return app.printModelInventory(managed, dataRoot, scans, *details, *application)
	}
	if *application != "" && view != "bundles" {
		return controlerr.Usage("--application requires the bundles or models view")
	}
	if view == "bundles" {
		if *application != "" {
			if _, ok := config.ApplicationByID(*application); !ok {
				return controlerr.Usage("unknown application %q", *application)
			}
		}
		terminal := app.terminal(app.Stdout)
		identifiers := sortedBundleIDs(managed)
		fmt.Fprintln(app.Stdout, terminal.Heading("Exact bundles:"))
		rows := [][]string{{terminal.Label("Bundle"), terminal.Label("Application"), terminal.Label("Size"), terminal.Label("License"), terminal.Label("Description")}}
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
			rows = append(rows, []string{terminal.Command(identifier), bundle.Application, humanSize(managed.BundleSize(bundle)), terminal.State(licenseState), bundle.Description})
		}
		lines, _ := ui.ColumnLines(rows, []ui.Column{{}, {}, {Right: true}, {}, {}}, "  ")
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
		}
		return nil
	}
	if view == "families" {
		terminal := app.terminal(app.Stdout)
		fmt.Fprintln(app.Stdout, terminal.Heading("Model families:"))
		var rows [][]string
		for _, family := range []string{"qwen", "wan"} {
			selected, _ := selectBundles(managed, "family", family)
			rows = append(rows, []string{terminal.Command("family " + family), fmt.Sprintf("%d bundles", len(selected))})
		}
		lines, _ := ui.ColumnLines(rows, []ui.Column{{}, {Right: true}}, "  ")
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
		}
		fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Global:"))
		lines, _ = ui.ColumnLines([][]string{{terminal.Command("all"), fmt.Sprintf("%d bundles", len(managed.Bundles))}}, []ui.Column{{}, {Right: true}}, "  ")
		fmt.Fprintln(app.Stdout, lines[0])
		return nil
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("Applications:"))
	for applicationIndex, applicationID := range []string{"comfyui", "llama-cpp", "dwarfstar"} {
		values, _ := recipes.ForApplication(applicationID)
		if applicationIndex > 0 {
			fmt.Fprintln(app.Stdout)
		}
		spec, _ := config.ApplicationByID(applicationID)
		fmt.Fprintf(app.Stdout, "  %s\n", terminal.Label(spec.DisplayName))
		rows := make([][]string, 0, len(values))
		for _, recipe := range values {
			count := fmt.Sprintf("%d bundles", len(recipe.Bundles))
			if len(recipe.Bundles) == 1 {
				count = "1 bundle"
			}
			rows = append(rows, []string{terminal.Command(applicationID + " " + recipe.ID), count, recipe.Description})
		}
		lines, _ := ui.ColumnLines(rows, []ui.Column{{}, {Right: true}, {}}, "    ")
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
		}
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Muted("Use 'content list models' for runnable models, 'bundles' for exact content, or 'families' for aggregates."))
	return nil
}

func (app *App) contentStatus(args []string) error {
	set := app.flags("content status", usage("content", "status", "[TARGET [SELECTION]]", "[--details]", "[--verify]", "[--data-dir PATH]"))
	set.Argument("TARGET", "application, family, exact bundle, or all (default)")
	set.Argument("SELECTION", "recipe or family identifier required by an application or family target")
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
	fmt.Fprintln(app.Stdout, terminal.Heading("Content status"))
	fmt.Fprintf(app.Stdout, "  %s  %s\n\n", terminal.Label("Data"), dataRoot)
	var summaryRows [][]string
	if !*details && !*verifyHash {
		summaryRows = append(summaryRows, []string{terminal.Label("Status"), terminal.Label("Bundle"), terminal.Label("Ready")})
	}
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
			var detailRows [][]string
			for _, status := range statuses {
				detailRows = append(detailRows, []string{terminal.State(string(status.State)), status.Artifact.Destination})
			}
			if bundle.Workflow != "" {
				state := "missing"
				if workflowReady(dataRoot, managed.Workflows[bundle.Workflow]) {
					state = "installed"
				}
				detailRows = append(detailRows, []string{terminal.State(state), "workflow/" + managed.Workflows[bundle.Workflow].Destination})
			}
			lines, _ := ui.ColumnLines(detailRows, nil, "  ")
			for _, line := range lines {
				fmt.Fprintln(app.Stdout, line)
			}
		} else {
			itemCount := len(statuses)
			if bundle.Workflow != "" {
				itemCount++
				if workflowReady(dataRoot, managed.Workflows[bundle.Workflow]) {
					ready++
				}
			}
			summaryRows = append(summaryRows, []string{terminal.State(summary), terminal.Command(bundle.ID), fmt.Sprintf("%d/%d items", ready, itemCount)})
		}
	}
	if len(summaryRows) > 0 {
		lines, _ := ui.ColumnLines(summaryRows, nil, "  ")
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
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
	set := app.flagsWithExamples("content install", usage("content", "install", "[TARGET [SELECTION]]", "[OPTIONS]"), []string{
		identity.Command("content", "install"),
		identity.Command("content", "install", "llama-cpp", "qwen3.8"),
		identity.Command("content", "install", "llama-cpp", "all", "--dry-run"),
		identity.Command("content", "install", "llama-cpp", "all", "--local-mirror", "/path/to/old-data", "--local-mirror-move", "--accept-license"),
	})
	set.Argument("TARGET", "application, family, exact bundle, or all; omit for the guided installer")
	set.Argument("SELECTION", "recipe, application aggregate 'all', or family identifier")
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
	managed, err := app.managedCatalog()
	if err != nil {
		return err
	}
	if len(packFiles) == 0 && !*nonInteractive && terminalReader(app.Stdin) && (target == "" || selection == "" && recipes.IsApplication(target)) {
		target, selection, err = app.guidedContentSelection(managed, target)
		if err != nil {
			return err
		}
	}
	if target == "" && len(packFiles) == 0 {
		return set.usageError("content install requires a complete target; use 'content list recipes'")
	}
	if len(packFiles) > 0 && (target != "" || selection != "") {
		return controlerr.Usage("--from-file cannot be combined with an explicit target")
	}
	if *localMirrorMove && *localMirror == "" {
		return controlerr.Usage("--local-mirror-move requires --local-mirror")
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
		fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Muted("Dry run: no data directory was created and no content was downloaded, moved, or verified."))
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

func (app *App) guidedContentSelection(managed catalog.Catalog, application string) (string, string, error) {
	if !terminalReader(app.Stdin) {
		return "", "", controlerr.Usage("content install requires a target when standard input is not a terminal")
	}
	if application != "" {
		return app.guidedApplicationContent(managed, application)
	}
	type topChoice struct {
		application string
		exact       bool
	}
	var choices []topChoice
	var rows [][]string
	terminal := app.terminal(app.Stdout)
	for _, application := range []string{"comfyui", "llama-cpp", "dwarfstar"} {
		spec, _ := config.ApplicationByID(application)
		choices = append(choices, topChoice{application: application})
		rows = append(rows, []string{terminal.Command(application), spec.DisplayName + " reviewed recipes"})
	}
	choices = append(choices, topChoice{exact: true})
	rows = append(rows, []string{terminal.Command("exact bundles"), "browse every advanced bundle by category"})
	index, err := app.promptMenu("Install content for:", rows, "content selection")
	if err != nil {
		return "", "", err
	}
	choice := choices[index]
	if choice.exact {
		bundle, err := app.guidedExactBundle(managed, "")
		return bundle, "", err
	}
	return app.guidedApplicationContent(managed, choice.application)
}

func (app *App) guidedApplicationContent(managed catalog.Catalog, application string) (string, string, error) {
	values, err := recipes.ForApplication(application)
	if err != nil {
		return "", "", controlerr.Usage("%v", err)
	}
	spec, _ := config.ApplicationByID(application)
	terminal := app.terminal(app.Stdout)
	rows := make([][]string, 0, len(values)+1)
	for _, recipe := range values {
		rows = append(rows, []string{terminal.Command(recipe.ID), recipe.Description})
	}
	rows = append(rows, []string{terminal.Command("exact bundles"), "browse every exact " + spec.DisplayName + " bundle"})
	index, err := app.promptMenu(spec.DisplayName+" content:", rows, "content selection")
	if err != nil {
		return "", "", err
	}
	if index == len(values) {
		bundle, err := app.guidedExactBundle(managed, application)
		return bundle, "", err
	}
	return application, values[index].ID, nil
}

func (app *App) promptMenu(heading string, rows [][]string, subject string) (int, error) {
	return app.promptMenuColumns(heading, rows, nil, subject)
}

func (app *App) promptMenuColumns(heading string, rows [][]string, columns []ui.Column, subject string) (int, error) {
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading(heading))
	lines, _ := ui.NumberedLines(rows, columns)
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	answer, err := app.promptLine(fmt.Sprintf("Choose [1-%d] (or q to cancel): ", len(rows)), true)
	if err != nil {
		return 0, err
	}
	if strings.EqualFold(answer, "q") {
		return 0, controlerr.New("%s cancelled", subject)
	}
	index, err := strconv.Atoi(answer)
	if err != nil {
		return 0, controlerr.Usage("%s must be a menu number", subject)
	}
	if index < 1 || index > len(rows) {
		return 0, controlerr.Usage("%s must be a number from 1 through %d", subject, len(rows))
	}
	return index - 1, nil
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

func (app *App) printModelInventory(managed catalog.Catalog, dataRoot string, scans []string, details bool, selectedApplication string) error {
	applications := []string{"llama-cpp", "dwarfstar"}
	if selectedApplication != "" {
		applications = []string{selectedApplication}
	}
	for index, application := range applications {
		if index > 0 {
			fmt.Fprintln(app.Stdout)
		}
		if application == "llama-cpp" {
			if err := app.printLlamaModelInventory(managed, dataRoot, scans, details); err != nil {
				return err
			}
		} else if err := app.printDwarfStarModelInventory(managed, dataRoot, details); err != nil {
			return err
		}
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout)
	if !details {
		fmt.Fprintln(app.Stdout, terminal.Muted("Use --details to find install commands and managed runtime policy."))
	}
	if selectedApplication != "dwarfstar" {
		fmt.Fprintln(app.Stdout, terminal.Muted("Run a ready llama.cpp preset with --preset; use a local GGUF row's absolute path with --model."))
	}
	if selectedApplication != "llama-cpp" {
		fmt.Fprintln(app.Stdout, terminal.Muted("Run a ready DwarfStar model with 'run dwarfstar server'."))
	}
	return nil
}

func (app *App) printLlamaModelInventory(managed catalog.Catalog, dataRoot string, scans []string, details bool) error {
	models, err := modelinventory.Llama(managed, dataRoot, scans)
	if err != nil {
		return err
	}
	terminal := app.terminal(app.Stdout)
	root := (storage.Layout{Root: dataRoot}).LlamaModels()
	fmt.Fprintln(app.Stdout, terminal.Heading("llama.cpp models"))
	fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label("Model root"), root)
	for _, scan := range scans {
		fmt.Fprintf(app.Stdout, "  %s  %s\n", terminal.Label("Also scanned"), scan)
	}
	if len(models) == 0 {
		fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Muted("No managed or local GGUF models found."))
		return nil
	}
	rows := [][]string{{terminal.Label("Status"), terminal.Label("Size"), terminal.Label("Preset or model")}}
	for _, model := range models {
		label := strings.Join(model.Presets, ", ")
		if len(model.Presets) == 0 {
			label = "local"
			if model.ExpectedShards > 1 {
				label = fmt.Sprintf("local (%d/%d shards)", model.ShardCount, model.ExpectedShards)
			}
			label += "  " + model.Path
		} else {
			label = terminal.Command(label)
		}
		size := "—"
		if model.Size > 0 {
			size = humanSize(model.Size)
		}
		rows = append(rows, []string{terminal.State(model.State), size, label})
	}
	lines, _ := ui.ColumnLines(rows, []ui.Column{{}, {Right: true}, {}}, "  ")
	fmt.Fprintln(app.Stdout)
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	if details {
		app.printLlamaModelDetails(managed, models, root)
	}
	return nil
}

func (app *App) printLlamaModelDetails(managed catalog.Catalog, models []modelinventory.LlamaModel, root string) {
	terminal := app.terminal(app.Stdout)
	printed := false
	for _, model := range models {
		for _, id := range model.Presets {
			if !printed {
				fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Managed preset details"))
				printed = true
			}
			preset := managed.LlamaPresets[id]
			bundle := managed.Bundles[preset.Bundle]
			path := model.Path
			if relative, err := filepath.Rel(root, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				path = relative
			}
			files := 0
			for _, artifactID := range bundle.Artifacts {
				if managed.Artifacts[artifactID].Target == "llama-models" {
					files++
				}
			}
			fmt.Fprintf(app.Stdout, "\n  %s\n", terminal.Command(id))
			writeDetailRows(app.Stdout, terminal, [][2]string{
				{"Model", path}, {"Bundle", bundle.ID}, {"Catalog size", humanSize(managed.BundleSize(bundle))},
				{"Files", strconv.Itoa(files)}, {"Default context", fmt.Sprintf("%d tokens", preset.DefaultContext)},
				{"Backends", llamaBackendPolicy(preset)},
				{"Context policy", llamaContextPolicy(preset)}, {"Template", llamaTemplatePolicy(preset)},
				{"Speculation", llamaSpeculationPolicy(preset)}, {"Reasoning", llamaInventoryReasoningPolicy(preset)},
				{"Sampling", llamaSamplingPolicy(preset)}, {"Flash Attention", llamaProfilePolicy(preset.FlashAttention)},
				{"K/V cache", llamaProfilePolicy(preset.KVCache)}, {"Model load", llamaModelLoadInventoryPolicy(preset.ModelLoad)},
			})
		}
	}
}

func llamaBackendPolicy(preset catalog.LlamaPreset) string {
	backends := preset.Backends
	if len(backends) == 0 {
		backends = []string{"rocm", "vulkan"}
	}
	var descriptions []string
	for _, backend := range backends {
		description := backend
		if profiles := preset.BackendProfiles[backend]; len(profiles) > 0 {
			description += " (" + strings.Join(profiles, ", ") + " only)"
		}
		descriptions = append(descriptions, description)
	}
	return strings.Join(descriptions, ", ")
}

func llamaModelLoadInventoryPolicy(policy map[string]string) string {
	values := make(map[string]string, len(policy)+2)
	for profile, value := range policy {
		values[profile] = value
	}
	for _, profile := range []string{"strix-halo", "strix-point"} {
		if values[profile] == "" {
			values[profile] = catalog.LlamaModelLoadResident
		}
	}
	return llamaProfilePolicy(values)
}

func (app *App) printDwarfStarModelInventory(managed catalog.Catalog, dataRoot string, details bool) error {
	store, err := verification.Load(dataRoot)
	if err != nil {
		return err
	}
	var bundles []catalog.Bundle
	for _, bundle := range managed.Bundles {
		if bundle.Application == "dwarfstar" {
			bundles = append(bundles, bundle)
		}
	}
	sort.Slice(bundles, func(i, j int) bool { return bundles[i].ID < bundles[j].ID })
	terminal := app.terminal(app.Stdout)
	fmt.Fprintln(app.Stdout, terminal.Heading("DwarfStar models"))
	fmt.Fprintf(app.Stdout, "  %s  %s\n\n", terminal.Label("Model root"), (storage.Layout{Root: dataRoot}).DwarfStarModels())
	rows := [][]string{{terminal.Label("Status"), terminal.Label("Size"), terminal.Label("Bundle")}}
	for _, bundle := range bundles {
		statuses, inspectErr := content.InspectBundle(store, managed, bundle, dataRoot, false)
		if inspectErr != nil {
			return inspectErr
		}
		rows = append(rows, []string{terminal.State(bundleModelState(statuses)), humanSize(managed.BundleSize(bundle)), terminal.Command(bundle.ID)})
	}
	lines, _ := ui.ColumnLines(rows, []ui.Column{{}, {Right: true}, {}}, "  ")
	for _, line := range lines {
		fmt.Fprintln(app.Stdout, line)
	}
	if !details {
		return nil
	}
	fmt.Fprintf(app.Stdout, "\n%s\n", terminal.Heading("Managed DwarfStar model details"))
	for _, bundle := range bundles {
		fmt.Fprintf(app.Stdout, "\n  %s\n", terminal.Command(bundle.ID))
		rows := make([][2]string, 0, len(bundle.Artifacts)+4)
		for index, artifactID := range bundle.Artifacts {
			label := "Support"
			if index == 0 {
				label = "Model"
			}
			rows = append(rows, [2]string{label, managed.Artifacts[artifactID].Destination})
		}
		run := identity.Command("run", "dwarfstar", "server")
		if len(bundle.Artifacts) > 1 {
			run += " --dspark"
		}
		rows = append(rows,
			[2]string{"Catalog size", humanSize(managed.BundleSize(bundle))},
			[2]string{"Files", strconv.Itoa(len(bundle.Artifacts))},
			[2]string{"Install", identity.Command("content", "install", bundle.ID)},
			[2]string{"Run", run},
		)
		writeDetailRows(app.Stdout, terminal, rows)
	}
	return nil
}

func bundleModelState(statuses []content.ArtifactStatus) string {
	missing := 0
	for _, status := range statuses {
		switch status.State {
		case content.Unexpected:
			return "user-file"
		case content.HashMismatch, content.SizeMismatch:
			return string(status.State)
		case content.Missing:
			missing++
		}
	}
	if missing == len(statuses) {
		return "missing"
	}
	if missing > 0 {
		return "partial"
	}
	for _, status := range statuses {
		if status.State == content.Unverified {
			return "unverified"
		}
	}
	return "ready"
}

func writeDetailRows(output io.Writer, terminal ui.Terminal, rows [][2]string) {
	materialized := make([][]string, 0, len(rows))
	for _, row := range rows {
		materialized = append(materialized, []string{terminal.Label(row[0]), row[1]})
	}
	lines, _ := ui.ColumnLines(materialized, nil, "    ")
	for _, line := range lines {
		fmt.Fprintln(output, line)
	}
}

func llamaTemplatePolicy(preset catalog.LlamaPreset) string {
	if preset.ChatTemplate != "" {
		return "managed " + preset.ChatTemplate
	}
	if preset.Jinja {
		return "model metadata; Jinja enabled"
	}
	return "model metadata; llama.cpp automatic"
}

func llamaContextPolicy(preset catalog.LlamaPreset) string {
	if len(preset.ContextOverrideArchitectures) == 0 {
		return "model metadata"
	}
	return "forced for " + strings.Join(preset.ContextOverrideArchitectures, ", ") + "; automatic fitting disabled"
}

func llamaSpeculationPolicy(preset catalog.LlamaPreset) string {
	if preset.SpeculativeType == "" {
		return "off"
	}
	strategy := map[string]string{"draft-mtp": "MTP", "draft-dflash": "DFlash"}[preset.SpeculativeType]
	backend := ""
	if len(preset.DraftTokensByBackend) > 0 {
		var policies []string
		for _, name := range []string{"rocm", "vulkan"} {
			if value, ok := preset.DraftTokensByBackend[name]; ok {
				policies = append(policies, fmt.Sprintf("%s=%d", name, value))
			}
		}
		backend = " (" + strings.Join(policies, ", ") + ")"
	}
	if preset.DraftArtifact != "" {
		return fmt.Sprintf("%s, %d draft tokens%s; draft %s", strategy, preset.DraftTokens, backend, preset.DraftArtifact)
	}
	return fmt.Sprintf("%s, %d draft tokens%s from model heads", strategy, preset.DraftTokens, backend)
}

func llamaInventoryReasoningPolicy(preset catalog.LlamaPreset) string {
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

func llamaSamplingPolicy(preset catalog.LlamaPreset) string {
	if preset.SamplingPolicy == "" {
		return "request or llama.cpp default"
	}
	return "catalog " + preset.SamplingPolicy + " (thinking/non-thinking)"
}

func llamaProfilePolicy(policy map[string]string) string {
	var values []string
	for _, profile := range []string{"rdna4", "strix-halo", "strix-point"} {
		if value, ok := policy[profile]; ok {
			values = append(values, profile+"="+value)
		}
	}
	if len(values) == 0 {
		return "llama.cpp default"
	}
	return strings.Join(values, ", ") + "; otherwise llama.cpp default"
}

func (app *App) contentWorkflows(args []string) error {
	writeHelp := func() {
		app.writeGroupHelp(usage("content", "workflows", "COMMAND", "[OPTIONS]"),
			[2]string{"list", "list curated workflows"},
			[2]string{"status", "inspect installed curated workflows"},
			[2]string{"install", "install one exact curated workflow"})
	}
	if groupHelpRequested(args) {
		writeHelp()
		return nil
	}
	if len(args) == 0 {
		writeHelp()
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
		var rows [][]string
		for _, id := range ids {
			workflow := managed.Workflows[id]
			rows = append(rows, []string{terminal.Command(id), workflow.Description})
		}
		lines, _ := ui.ColumnLines(rows, nil, "  ")
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
		}
		return nil
	case "status":
		set := app.flags("content workflows status", usage("content", "workflows", "status", "[WORKFLOW]", "[--data-dir PATH]"))
		set.Argument("WORKFLOW", "exact curated workflow; omit to inspect all")
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
		rows := [][]string{{terminal.Label("Status"), terminal.Label("Workflow"), terminal.Label("Path")}}
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
			rows = append(rows, []string{terminal.State(state), terminal.Command(id), path})
		}
		lines, _ := ui.ColumnLines(rows, nil, "  ")
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
		}
		if !allReady {
			return &controlerr.Error{Message: "some workflows are not installed", Status: 1}
		}
		return nil
	case "install":
		set := app.flags("content workflows install", usage("content", "workflows", "install", "WORKFLOW", "[--force]", "[--data-dir PATH]"))
		set.Argument("WORKFLOW", "exact curated workflow identifier")
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
			if id == "" {
				set.renderHelp(app.Stdout)
			}
			return controlerr.Usage("choose an exact workflow")
		}
		dataRoot, err := app.resolveDataDir(*dataFlag, true)
		if err != nil {
			return err
		}
		return app.installWorkflow(workflow, dataRoot, *force)
	default:
		writeHelp()
		return controlerr.Usage("unknown workflows command %q", args[0])
	}
}

func (app *App) contentImport(args []string) error {
	set := app.flags("content import", usage("content", "import", "[URL]", "[OPTIONS]"))
	set.Argument("URL", "allowlisted Hugging Face or Civitai file or model URL; omit for a prompt")
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
	if setWasSet(set, "version") && *version <= 0 {
		return controlerr.Usage("--version must be positive")
	}
	if rawURL == "" {
		if *nonInteractive || !terminalReader(app.Stdin) {
			return set.usageError("content import requires URL in noninteractive use")
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
			rows := make([][]string, 0, len(choices))
			for _, choice := range choices {
				rows = append(rows, []string{terminal.Command(choice[0]), choice[1]})
			}
			lines, _ := ui.NumberedLines(rows, nil)
			for _, line := range lines {
				fmt.Fprintln(app.Stdout, line)
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
		rows := make([][]string, 0, len(discovery.Files))
		for _, file := range discovery.Files {
			rows = append(rows, []string{terminal.Command(file.Name), humanSize(file.Size), map[bool]string{true: "primary"}[file.Primary]})
		}
		lines, _ := ui.NumberedLines(rows, []ui.Column{{}, {Right: true}, {}})
		for _, line := range lines {
			fmt.Fprintln(app.Stdout, line)
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
			rows := make([][]string, 0, len(candidates))
			for _, candidate := range candidates {
				rows = append(rows, []string{terminal.Command(candidate.ID), candidate.Label})
			}
			lines, _ := ui.NumberedLines(rows, nil)
			for _, line := range lines {
				fmt.Fprintln(app.Stdout, line)
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
	packDisplay := packPath
	if *dryRun {
		packDisplay += " (not saved by dry run)"
	}
	writeDetailRows(app.Stdout, terminal, [][2]string{
		{"Provider", discovery.Provider}, {"Source", discovery.SourceURL}, {"Title", discovery.Title},
		{"File", selected.Name + " (" + humanSize(selected.Size) + ")"}, {"SHA-256", selected.SHA256},
		{"Install as", kind.Label}, {"Destination", plan.Artifact.Destination}, {"Local pack", packDisplay},
		{"License", terminal.Warning("NOASSERTION; hosted-file rights are unverified")},
	})
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
