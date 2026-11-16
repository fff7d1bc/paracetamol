package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"paracetamol/internal/process"
)

// A resource-only execution copy can inherit Git discovery from an unrelated
// ancestor. That revision must never identify an acceptance checkpoint. Ask
// Git for its actual worktree root rather than assuming .git is a directory:
// linked worktrees use a .git file, and checkout paths may contain symlinks.
func (app *App) requireSourceCheckout() error {
	result, err := app.Runner.Run(app.Context, process.Command{Name: "git", Args: []string{"-C", app.Root, "rev-parse", "--show-toplevel"}})
	if err != nil || result.Status != 0 {
		return fmt.Errorf("project source identity requires a Git checkout rooted at the project directory; use a matching clone or worktree for acceptance and speculative benchmarks")
	}
	canonical := func(path string) (string, error) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(absolute)
	}
	root, err := canonical(app.Root)
	if err != nil {
		return fmt.Errorf("cannot resolve project source root: %w", err)
	}
	gitRoot := strings.TrimSuffix(string(result.Stdout), "\n")
	if gitRoot == "" {
		return fmt.Errorf("cannot resolve project Git checkout root")
	}
	gitRoot, err = canonical(gitRoot)
	if err != nil {
		return fmt.Errorf("cannot resolve project Git checkout root: %w", err)
	}
	if gitRoot != root {
		return fmt.Errorf("project source identity requires its own Git checkout; the project directory is inside a different Git worktree; use a matching clone or worktree for acceptance and speculative benchmarks")
	}
	return nil
}
