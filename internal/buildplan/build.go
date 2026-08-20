// Package buildplan constructs local image build commands.
package buildplan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const PipCacheContainerPath = "/var/cache/rocmplete/pip"

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
	return filepath.Join(filepath.Clean(root), "rocmplete", "build"), nil
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
		arguments = append(arguments, "--build-arg", "PIP_NO_CACHE_DIR=", "--build-arg", "PIP_CACHE_DIR="+PipCacheContainerPath, "--volume", options.PipCache+":"+PipCacheContainerPath+options.VolumeSuffix)
	}
	if options.NoLayerCache {
		arguments = append(arguments, "--no-cache")
	}
	return append(arguments, options.ProjectRoot), nil
}
