// Package buildplan constructs local image build commands.
package buildplan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rocmplete/internal/application"
	"rocmplete/internal/identity"
)

const pipCacheContainerPath = "/var/cache/rocmplete/pip"

func CacheDir(environment map[string]string) (string, error) {
	root := environment["XDG_CACHE_HOME"]
	if root == "" {
		home := environment["HOME"]
		if home == "" {
			return "", fmt.Errorf("cannot locate the build cache: HOME is not set")
		}
		if !filepath.IsAbs(home) {
			return "", fmt.Errorf("HOME must be an absolute path: %s", home)
		}
		root = filepath.Join(home, ".cache")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("XDG_CACHE_HOME must be an absolute path: %s", root)
	}
	return filepath.Join(filepath.Clean(root), identity.StateNamespace, "build"), nil
}

func PreparePipCache(environment map[string]string) (string, error) {
	root, err := CacheDir(environment)
	if err != nil {
		return "", err
	}
	cache := filepath.Join(root, "pip")
	for _, candidate := range []string{filepath.Dir(root), root, cache} {
		status, statErr := os.Lstat(candidate)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return "", fmt.Errorf("inspect build cache %s: %w", candidate, statErr)
		}
		if status.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing symlinked build cache path: %s", candidate)
		}
		if !status.IsDir() {
			return "", fmt.Errorf("build cache path is not a directory: %s", candidate)
		}
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return "", fmt.Errorf("create pip build cache %s: %w", cache, err)
	}
	return cache, nil
}

type Options struct {
	ProjectRoot  string
	Image        string
	Target       string
	BaseImage    string
	RuntimeImage string
	PipCache     string
	VolumeSuffix string
	NoLayerCache bool
}

type Request struct {
	ProjectRoot   string
	Targets       []application.BuildID
	ImageOverride string
	PipCache      string
	VolumeSuffix  string
	NoLayerCache  bool
	NoCache       bool
}

type Step struct {
	Unit    application.BuildUnit
	Image   string
	Command []string
	Cold    bool
}

// Plan resolves the complete build dependency graph before Podman is started.
// A target-specific image override never changes a prerequisite identity.
func Plan(request Request) ([]Step, error) {
	if len(request.Targets) == 0 {
		return nil, fmt.Errorf("build plan requires at least one target")
	}
	if request.NoLayerCache && request.NoCache {
		return nil, fmt.Errorf("no-layer-cache and no-cache are mutually exclusive")
	}
	if request.ImageOverride != "" && len(request.Targets) != 1 {
		return nil, fmt.Errorf("image override requires exactly one build target")
	}
	units, err := application.BuildClosure(request.Targets)
	if err != nil {
		return nil, err
	}
	selected := make(map[application.BuildID]bool, len(request.Targets))
	for _, target := range request.Targets {
		selected[target] = true
	}
	images := make(map[application.BuildID]string)
	for _, unit := range application.BuildUnits() {
		images[unit.ID] = unit.Image
	}
	if request.ImageOverride != "" {
		images[request.Targets[0]] = request.ImageOverride
	}
	steps := make([]Step, 0, len(units))
	for _, unit := range units {
		baseImage, runtimeImage := "", ""
		for _, prerequisite := range unit.Prerequisites {
			switch prerequisite {
			case application.BuildRuntime:
				runtimeImage = images[prerequisite]
			case application.BuildPyTorchBase:
				baseImage = images[prerequisite]
			}
		}
		cold := request.NoCache || request.NoLayerCache && selected[unit.ID]
		pipCache := request.PipCache
		if request.NoCache {
			pipCache = ""
		}
		command, err := Command(Options{
			ProjectRoot: request.ProjectRoot, Image: images[unit.ID], Target: unit.Target,
			BaseImage: baseImage, RuntimeImage: runtimeImage, PipCache: pipCache,
			VolumeSuffix: request.VolumeSuffix, NoLayerCache: cold,
		})
		if err != nil {
			return nil, fmt.Errorf("plan %s: %w", unit.ID, err)
		}
		steps = append(steps, Step{Unit: unit, Image: images[unit.ID], Command: command, Cold: cold})
	}
	return steps, nil
}

func Command(options Options) ([]string, error) {
	arguments := []string{"podman", "build", "--tag", options.Image, "--file", filepath.Join(options.ProjectRoot, "Containerfile"), "--target", options.Target}
	if options.BaseImage != "" {
		arguments = append(arguments, "--build-arg", "ROCM_BASE_IMAGE="+options.BaseImage, "--pull=never")
	}
	if options.RuntimeImage != "" {
		arguments = append(arguments, "--build-arg", "ROCM_RUNTIME_IMAGE="+options.RuntimeImage, "--pull=never")
	}
	if options.PipCache != "" {
		if !filepath.IsAbs(options.PipCache) {
			return nil, fmt.Errorf("pip build cache path must be absolute: %s", options.PipCache)
		}
		if strings.ContainsAny(options.PipCache, ":\n") {
			return nil, fmt.Errorf("pip build cache path cannot contain ':' or a newline: %s", options.PipCache)
		}
		if options.VolumeSuffix != ":rw" && options.VolumeSuffix != ":rw,Z" {
			return nil, fmt.Errorf("unsupported build-cache volume suffix: %s", options.VolumeSuffix)
		}
		arguments = append(arguments, "--build-arg", "PIP_NO_CACHE_DIR=", "--build-arg", "PIP_CACHE_DIR="+pipCacheContainerPath, "--volume", options.PipCache+":"+pipCacheContainerPath+options.VolumeSuffix)
	}
	if options.NoLayerCache {
		arguments = append(arguments, "--no-cache")
	}
	return append(arguments, options.ProjectRoot), nil
}
