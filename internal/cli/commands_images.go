package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/imagearchive"
)

func (app *App) commandImages(args []string) error {
	if groupHelpRequested(args) {
		writeGroupHelp(app.Stdout, usage("images", "COMMAND", "[OPTIONS]"),
			[2]string{"export", "save exact managed images to an archive"},
			[2]string{"import", "validate and load a managed image archive"})
		return nil
	}
	if len(args) == 0 {
		return controlerr.Usage("choose images export or import")
	}
	switch args[0] {
	case "export":
		return app.imagesExport(args[1:])
	case "import":
		return app.imagesImport(args[1:])
	default:
		return controlerr.Usage("unknown images command %q", args[0])
	}
}

func (app *App) imagesExport(args []string) error {
	set := app.flags("images export", usage("images", "export", "TARGET", "--output ARCHIVE", "[--dry-run]"))
	outputFlag := set.String("output", "", "new Docker archive path")
	dryRun := set.Bool("dry-run", false, "validate and print the command")
	target, remaining := leadingPositional(args)
	if err := parseFlags(set, remaining); err != nil {
		return err
	}
	if target == "" && len(set.Args()) > 0 {
		target = set.Args()[0]
		if len(set.Args()) > 1 {
			return controlerr.Usage("images export accepts exactly one target")
		}
	} else if len(set.Args()) > 0 {
		return controlerr.Usage("images export accepts exactly one target")
	}
	if target == "" || *outputFlag == "" {
		return controlerr.Usage("images export requires TARGET and --output")
	}
	references, err := imagearchive.SelectedReferences(target)
	if err != nil {
		return controlerr.Usage("%v", err)
	}
	output, err := filepath.Abs(*outputFlag)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(output); err == nil || info != nil {
		return controlerr.New("image archive output already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent, err := os.Stat(filepath.Dir(output))
	if err != nil || !parent.IsDir() {
		return controlerr.New("image archive parent is not a directory: %s", filepath.Dir(output))
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	localIDs := make(map[string]string)
	for _, reference := range references {
		present, err := app.podman().Exists(app.Context, "image", reference)
		if err != nil {
			return err
		}
		if !present {
			return controlerr.New("cannot export missing image %s", reference)
		}
		localIDs[reference], err = app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", reference}, "cannot inspect image "+reference)
		if err != nil {
			return err
		}
		fmt.Fprintf(app.Stdout, "  ready  %s\n", reference)
	}
	command := []string{"podman", "save", "--format", "docker-archive", "--output", "<temporary-archive>"}
	if len(references) > 1 {
		command = append(command, "--multi-image-archive")
	}
	command = append(command, references...)
	if *dryRun {
		fmt.Fprintf(app.Stdout, "Archive: %s\nCommand: %s\n", output, shellJoin(command))
		return nil
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), "."+filepath.Base(output)+".*.partial")
	if err != nil {
		return err
	}
	partial := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(partial); err != nil {
		return err
	}
	defer os.Remove(partial)
	for index, value := range command {
		if value == "<temporary-archive>" {
			command[index] = partial
		}
	}
	if _, err := app.run(command, false); err != nil {
		return err
	}
	archive, err := imagearchive.Inspect(partial)
	if err != nil {
		return err
	}
	if err := imagearchive.ValidateManaged(archive, references); err != nil {
		return err
	}
	for _, image := range archive.Images {
		if localIDs[image.Reference] != image.ID {
			return controlerr.New("image identity changed while exporting: %s", image.Reference)
		}
	}
	if err := atomicfile.Publish(output, partial, 0o600, atomicfile.Create); err != nil {
		return controlerr.New("publish image archive: %v", err)
	}
	fmt.Fprintf(app.Stdout, "Exported: %s (%s)\n", output, humanSize(archive.Size))
	return nil
}

func (app *App) imagesImport(args []string) error {
	set := app.flags("images import", usage("images", "import", "ARCHIVE", "[--dry-run]"))
	dryRun := set.Bool("dry-run", false, "validate without loading")
	file, remaining := leadingPositional(args)
	if err := parseFlags(set, remaining); err != nil {
		return err
	}
	if file == "" && len(set.Args()) > 0 {
		file = set.Args()[0]
		if len(set.Args()) > 1 {
			return controlerr.Usage("images import accepts exactly one archive")
		}
	} else if len(set.Args()) > 0 {
		return controlerr.Usage("images import accepts exactly one archive")
	}
	if file == "" {
		return controlerr.Usage("images import requires an archive")
	}
	resolved, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	archive, err := imagearchive.Inspect(resolved)
	if err != nil {
		return err
	}
	if err := imagearchive.ValidateManaged(archive, nil); err != nil {
		return err
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	missing := 0
	for _, image := range archive.Images {
		present, err := app.podman().Exists(app.Context, "image", image.Reference)
		if err != nil {
			return err
		}
		state := "missing"
		if present {
			localID, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", image.Reference}, "cannot inspect image "+image.Reference)
			if err != nil {
				return err
			}
			if localID != image.ID {
				return controlerr.New("current tag refers to a different image: %s; remove that exact tag before importing", image.Reference)
			}
			state = "identical"
		} else {
			missing++
		}
		fmt.Fprintf(app.Stdout, "  %-10s %s\n", state, image.Reference)
	}
	fmt.Fprintf(app.Stdout, "Archive: %s (%s)\n", resolved, humanSize(archive.Size))
	if missing == 0 {
		fmt.Fprintln(app.Stdout, "All archived images are already present.")
		return nil
	}
	command := []string{"podman", "load", "--input", resolved}
	if *dryRun {
		fmt.Fprintf(app.Stdout, "Command: %s\n", shellJoin(command))
		return nil
	}
	if _, err := app.run(command, false); err != nil {
		return err
	}
	var invalid []string
	for _, image := range archive.Images {
		localID, err := app.podman().Capture(app.Context, []string{"image", "inspect", "--format", "{{.Id}}", image.Reference}, "cannot verify image "+image.Reference)
		if err != nil || strings.TrimSpace(localID) != image.ID {
			invalid = append(invalid, image.Reference)
		}
	}
	if len(invalid) > 0 {
		return controlerr.New("Podman load completed but verification failed for: %s", strings.Join(invalid, ", "))
	}
	fmt.Fprintf(app.Stdout, "Imported %d managed images.\n", len(archive.Images))
	return nil
}
