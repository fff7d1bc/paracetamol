// Package storage owns persistent layout and managed-path safety.
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rocmplete/internal/identity"
)

type Layout struct{ Root string }

func (layout Layout) Application(name string) string {
	return filepath.Join(layout.Root, "apps", name)
}
func (layout Layout) ComfyModels() string {
	return filepath.Join(layout.Root, "content", "comfyui", "models")
}
func (layout Layout) LlamaModels() string {
	return filepath.Join(layout.Root, "content", "llama-cpp", "models")
}
func (layout Layout) DwarfStarModels() string {
	return filepath.Join(layout.Root, "content", "dwarfstar", "models")
}
func (layout Layout) CuratedWorkflows() string {
	return filepath.Join(layout.Application("comfyui"), "user", "default", "workflows", "curated")
}
func (layout Layout) ImportedWorkflows() string {
	return filepath.Join(layout.Application("comfyui"), "user", "default", "workflows", "imported")
}
func (layout Layout) VerificationReceipt() string {
	return filepath.Join(layout.Root, "content", "."+identity.StateNamespace, "verification.json")
}
func (layout Layout) Staging() string { return filepath.Join(layout.Root, "staging") }
func (layout Layout) PiRuntime() string {
	return filepath.Join(layout.Application("pi"), "runtime")
}
func (layout Layout) LlamaBenchmarks() string {
	return filepath.Join(layout.Application("llama-cpp"), "benchmarks")
}
func (layout Layout) ComfyBenchmarks() string {
	return filepath.Join(layout.Application("comfyui"), "benchmarks")
}
func (layout Layout) AgentEvaluations() string {
	return layout.Application("agent-evaluation")
}
func (layout Layout) AcceptanceResults() string {
	return filepath.Join(layout.Application("acceptance"), "results")
}

func (layout Layout) PrepareRuntime(application string) error {
	paths := []string{layout.Application(application)}
	switch application {
	case "comfyui":
		paths = append(paths, layout.ComfyModels())
	case "llama-cpp":
		paths = append(paths, layout.LlamaModels())
	case "dwarfstar":
		paths = append(paths, layout.DwarfStarModels())
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("prepare %s: %w", path, err)
		}
	}
	return nil
}

func (layout Layout) PrepareDownloads() error {
	for _, path := range []string{filepath.Join(layout.Staging(), ".home"), filepath.Join(layout.Staging(), ".cache", "huggingface")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("prepare %s: %w", path, err)
		}
	}
	return nil
}

// ValidateManagedParent rejects redirects between persistent partitions. An
// ordinary filepath.Rel check is insufficient because an existing staging
// component could be a symlink into application-owned state.
func ValidateManagedParent(path, managedRoot, dataRoot, description string) error {
	data, err := filepath.Abs(dataRoot)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(managedRoot)
	if err != nil {
		return err
	}
	candidate, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !within(root, data) || !within(candidate, root) {
		return fmt.Errorf("%s path escapes its managed root: %s", description, path)
	}
	relative, err := filepath.Rel(data, filepath.Dir(candidate))
	if err != nil {
		return err
	}
	current := data
	components := []string{data}
	if relative != "." {
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			components = append(components, current)
		}
	}
	for _, component := range components {
		status, statErr := os.Lstat(component)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			return fmt.Errorf("cannot inspect %s path component %s: %w", description, component, statErr)
		}
		if status.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlinked %s path component: %s", description, component)
		}
		if !status.IsDir() {
			return fmt.Errorf("%s path component is not a directory: %s", description, component)
		}
	}
	return nil
}

func within(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
