// Package project locates checkout-owned runtime resources.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const internalRootEnvironment = "PARACETAMOL_INTERNAL_PROJECT_ROOT"

// Root returns the validated source checkout containing runtime resources.
// The private environment override is set by the eventual checkout launcher;
// executable-relative discovery keeps direct build-tree invocation useful.
func Root() (string, error) {
	if configured := os.Getenv(internalRootEnvironment); configured != "" {
		return validate(configured)
	}

	executable, err := os.Executable()
	if err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
			executable = resolved
		}
		if root, findErr := find(filepath.Dir(executable)); findErr == nil {
			return root, nil
		}
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determine working directory: %w", err)
	}
	return find(workingDirectory)
}

func find(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if root, validateErr := validate(current); validateErr == nil {
			return root, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("cannot find the source checkout containing Containerfile and catalog/catalog.json")
		}
		current = parent
	}
}

func validate(candidate string) (string, error) {
	root, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	for _, relative := range []string{"Containerfile", filepath.Join("catalog", "catalog.json")} {
		status, statErr := os.Stat(filepath.Join(root, relative))
		if statErr != nil {
			return "", fmt.Errorf("invalid project root %s: %s is unavailable: %w", root, relative, statErr)
		}
		if !status.Mode().IsRegular() {
			return "", fmt.Errorf("invalid project root %s: %s is not a regular file", root, relative)
		}
	}
	return root, nil
}
