package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"paracetamol/internal/buildplan"
	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
	"paracetamol/internal/podman"
	"paracetamol/internal/runtime"
)

type cleanupTarget struct {
	kind string
	path string
	size int64
}

func (app *App) commandCleanup(args []string) error {
	if groupHelpRequested(args) {
		writeGroupHelp(app.Stdout, usage("cleanup", "SCOPE", "[OPTIONS]"),
			[2]string{"containers", "remove " + identity.DisplayName + "-owned containers"},
			[2]string{"caches", "remove application runtime caches"},
			[2]string{"build-cache", "remove the host package-download cache"},
			[2]string{"downloads", "remove resumable download staging"},
			[2]string{"images", "remove managed images"},
			[2]string{"data", "remove one explicit persistent data root"})
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return controlerr.Usage("choose containers, caches, build-cache, downloads, images, or data")
	}
	scope := args[0]
	if err := requireChoice(scope, "cleanup scope", "containers", "caches", "build-cache", "downloads", "images", "data"); err != nil {
		return err
	}
	set := app.flags("cleanup "+scope, usage("cleanup", scope, "[OPTIONS]"))
	yes := set.Bool("yes", false, "confirm the complete plan")
	nonInteractive := set.Bool("non-interactive", false, "never prompt")
	application := set.String("application", "all", "application scope")
	dataFlag := set.String("data-dir", "", "persistent data directory")
	imageTag := set.String("image-tag", "", "one exact image tag")
	if err := parseFlags(set, args[1:]); err != nil {
		return err
	}
	if *application != "all" {
		if _, ok := config.ApplicationByID(*application); !ok {
			return controlerr.Usage("unknown application %q", *application)
		}
	}
	if scope == "containers" {
		return app.cleanupContainers(*application, *yes, *nonInteractive)
	}
	if scope == "images" {
		return app.cleanupImages(*application, *imageTag, *yes, *nonInteractive)
	}
	if *imageTag != "" {
		return controlerr.Usage("--image-tag is valid only for images cleanup")
	}
	if scope == "build-cache" {
		cache, err := buildplan.CacheDir(app.Environment)
		if err != nil {
			return err
		}
		return app.cleanupPaths(scope, []string{cache}, *yes, *nonInteractive, false)
	}
	dataRoot, err := app.cleanupDataRoot(*dataFlag)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dataRoot); os.IsNotExist(err) {
		fmt.Fprintf(app.Stdout, "Data not present: %s\n", dataRoot)
		return nil
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	containers, err := app.managedContainers("all")
	if err != nil {
		return err
	}
	if len(containers) > 0 {
		return controlerr.New("managed containers are still present: %s; clean up containers first", strings.Join(containers, ", "))
	}
	if scope == "data" {
		return app.cleanupPaths(scope, []string{dataRoot}, *yes, *nonInteractive, true)
	}
	var paths []string
	if scope == "downloads" {
		paths = []string{filepath.Join(dataRoot, "staging"), filepath.Join(dataRoot, "apps", "comfyui", "benchmarks", ".cache")}
	} else {
		for _, spec := range config.Applications() {
			base := filepath.Join(dataRoot, "apps", spec.ID)
			paths = append(paths, filepath.Join(base, "cache"), filepath.Join(base, "home", ".cache"), filepath.Join(base, "home", ".miopen"), filepath.Join(base, "home", ".triton"))
		}
	}
	return app.cleanupPaths(scope, paths, *yes, *nonInteractive, false)
}

func (app *App) cleanupContainers(application string, yes, nonInteractive bool) error {
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	names, err := app.managedContainers(application)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(app.Stdout, "No managed containers are present.")
		return nil
	}
	for _, name := range names {
		fmt.Fprintf(app.Stdout, "  container: %s\n", name)
	}
	if err := app.confirmCleanup("containers", yes, nonInteractive); err != nil {
		return err
	}
	for _, name := range names {
		if err := app.podman().RemoveContainer(app.Context, name, 0, podman.Streams{Stdin: app.Stdin, Stdout: app.Stdout, Stderr: app.Stderr}); err != nil {
			present, inspectErr := app.podman().Exists(app.Context, "container", name)
			if inspectErr != nil || present {
				return err
			}
		}
		fmt.Fprintf(app.Stdout, "Removed container: %s\n", name)
	}
	return nil
}

func (app *App) managedContainers(application string) ([]string, error) {
	filter := application
	if application == "all" {
		filter = ""
	}
	labelled, err := app.podman().ManagedContainerNames(app.Context, filter)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var result []string
	add := func(name string) error {
		if seen[name] {
			return nil
		}
		present, err := app.podman().Exists(app.Context, "container", name)
		if err != nil {
			return err
		}
		if present {
			seen[name] = true
			result = append(result, name)
		}
		return nil
	}
	for _, spec := range config.Applications() {
		if application == "all" || application == spec.ID {
			if err := add(spec.ContainerName); err != nil {
				return nil, err
			}
		}
	}
	transient := map[string]string{comfyBenchmarkContainer: "comfyui", runtime.LlamaBenchmarkContainer: "llama-cpp", speculativeBenchmarkContainer: "llama-cpp"}
	for name, owner := range transient {
		if application == "all" || application == owner {
			if err := add(name); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range labelled {
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	sortStrings(result)
	return result, nil
}

func (app *App) cleanupImages(application, exact string, yes, nonInteractive bool) error {
	if exact != "" && application != "all" {
		return controlerr.Usage("--image-tag cannot be combined with --application")
	}
	if err := app.podman().RequireRootless(app.Context); err != nil {
		return err
	}
	containers, err := app.managedContainers(application)
	if err != nil {
		return err
	}
	if len(containers) > 0 {
		return controlerr.New("managed containers are still present: %s", strings.Join(containers, ", "))
	}
	var images []string
	if exact != "" {
		images = []string{exact}
	} else {
		for _, spec := range config.Applications() {
			if application == "all" || application == spec.ID {
				images = append(images, spec.Image)
			}
		}
		if application == "all" {
			images = append(images, config.ROCmBaseImage, config.ROCmRuntimeImage, config.ContentToolsImage)
		}
	}
	var present []string
	for _, image := range images {
		exists, err := app.podman().Exists(app.Context, "image", image)
		if err != nil {
			return err
		}
		if exists {
			present = append(present, image)
		} else {
			fmt.Fprintf(app.Stdout, "Image not present: %s\n", image)
		}
	}
	if len(present) == 0 {
		return nil
	}
	for _, image := range present {
		fmt.Fprintf(app.Stdout, "  image: %s\n", image)
	}
	if err := app.confirmCleanup("images", yes, nonInteractive); err != nil {
		return err
	}
	for _, image := range present {
		if err := app.podman().RemoveImage(app.Context, image, podman.Streams{Stdin: app.Stdin, Stdout: app.Stdout, Stderr: app.Stderr}); err != nil {
			exists, inspectErr := app.podman().Exists(app.Context, "image", image)
			if inspectErr != nil || exists {
				return err
			}
		}
		fmt.Fprintf(app.Stdout, "Removed image: %s\n", image)
	}
	return nil
}

func (app *App) cleanupPaths(scope string, paths []string, yes, nonInteractive, removeDataRoot bool) error {
	var targets []cleanupTarget
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			fmt.Fprintf(app.Stdout, "Not present: %s\n", path)
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return controlerr.New("cleanup target is not a real directory: %s", path)
		}
		size, err := directorySize(path)
		if err != nil {
			return err
		}
		targets = append(targets, cleanupTarget{kind: scope, path: path, size: size})
	}
	if len(targets) == 0 {
		return nil
	}
	fmt.Fprintln(app.Stdout, "Cleanup plan:")
	for _, target := range targets {
		fmt.Fprintf(app.Stdout, "  %s: %s (%s)\n", target.kind, target.path, humanSize(target.size))
	}
	if err := app.confirmCleanup(scope, yes, nonInteractive); err != nil {
		return err
	}
	for _, target := range targets {
		if !removeDataRoot {
			if err := rejectSymlinkComponents(target.path); err != nil {
				return err
			}
		}
		if err := os.RemoveAll(target.path); err != nil {
			return fmt.Errorf("remove %s: %w", target.path, err)
		}
		fmt.Fprintf(app.Stdout, "Removed: %s (%s)\n", target.path, humanSize(target.size))
	}
	return nil
}

func (app *App) cleanupDataRoot(value string) (string, error) {
	root, err := app.resolveDataDir(value, false)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	home, _ := filepath.Abs(app.Environment["HOME"])
	protected := map[string]bool{"/": true, "/etc": true, "/home": true, "/opt": true, "/tmp": true, "/usr": true, "/var": true, home: true, filepath.Dir(home): true}
	if protected[root] || len(strings.Split(strings.Trim(root, string(filepath.Separator)), string(filepath.Separator))) < 2 {
		return "", controlerr.New("refusing broad data directory: %s", root)
	}
	if err := rejectSymlinkComponents(root); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return root, nil
}

func (app *App) confirmCleanup(scope string, yes, nonInteractive bool) error {
	if yes {
		return nil
	}
	if nonInteractive || !terminalReader(app.Stdin) {
		return controlerr.New("%s cleanup requires confirmation; repeat with --yes", scope)
	}
	fmt.Fprintf(app.Stdout, "Proceed with %s cleanup? [y/N] ", scope)
	line, _ := bufio.NewReader(app.Stdin).ReadString('\n')
	if normalized := strings.ToLower(strings.TrimSpace(line)); normalized != "y" && normalized != "yes" {
		return controlerr.New("%s cleanup declined", scope)
	}
	return nil
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func rejectSymlinkComponents(path string) error {
	path = filepath.Clean(path)
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return controlerr.New("refusing cleanup through symlinked path: %s", current)
		}
	}
	return nil
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}
