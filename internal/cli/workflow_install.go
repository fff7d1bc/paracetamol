package cli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/storage"
)

type workflowInstallPlan struct {
	destination string
	contents    []byte
	policy      atomicfile.Policy
	ready       bool
}

// Bundle preflight and workflow-only installation use the same validation.
// A plan creates nothing and is rebuilt after downloads rather than treating
// an earlier user approval as permission to replace a newly changed workflow.
func (app *App) planWorkflowInstall(workflow catalog.Workflow, dataRoot string, force bool) (workflowInstallPlan, error) {
	plan := workflowInstallPlan{destination: workflowPath(dataRoot, workflow), policy: atomicfile.Create}
	managedRoot := (storage.Layout{Root: dataRoot}).CuratedWorkflows()
	if err := storage.ValidateManagedParent(plan.destination, managedRoot, dataRoot, "curated workflow"); err != nil {
		return plan, err
	}
	if workflowReady(dataRoot, workflow) {
		plan.ready = true
		return plan, nil
	}
	if info, err := os.Lstat(plan.destination); err == nil {
		if !info.Mode().IsRegular() {
			return plan, controlerr.New("workflow path is not a regular file: %s", plan.destination)
		}
		if !force {
			return plan, controlerr.New("workflow differs from the curated version: %s; explicit replacement is required", plan.destination)
		}
		plan.policy = atomicfile.ReplaceRegular
	} else if !os.IsNotExist(err) {
		return plan, err
	}
	if err := atomicfile.ValidateDestination(plan.destination, plan.policy); err != nil {
		return plan, err
	}
	resource := filepath.Join(app.Root, "catalog", "workflows", workflow.ID+".json")
	contents, err := os.ReadFile(resource)
	if err != nil {
		return plan, fmt.Errorf("read curated workflow resource: %w", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(contents)) != workflow.RenderedSHA256 {
		return plan, controlerr.New("curated workflow resource does not match catalog: %s", workflow.ID)
	}
	plan.contents = contents
	return plan, nil
}

func (app *App) installWorkflow(workflow catalog.Workflow, dataRoot string, force bool) error {
	if _, err := app.planWorkflowInstall(workflow, dataRoot, force); err != nil {
		return err
	}
	return content.WithMutation(app.Context, dataRoot, func() error {
		return app.publishWorkflow(workflow, dataRoot, force)
	})
}

// publishWorkflow requires the caller's retained content mutation reservation.
// Bundle installation must not reacquire it between artifacts and workflows.
func (app *App) publishWorkflow(workflow catalog.Workflow, dataRoot string, force bool) error {
	if app.Context != nil {
		if err := app.Context.Err(); err != nil {
			return err
		}
	}
	plan, err := app.planWorkflowInstall(workflow, dataRoot, force)
	if err != nil {
		return err
	}
	if app.Context != nil {
		if err := app.Context.Err(); err != nil {
			return err
		}
	}
	if plan.ready {
		fmt.Fprintf(app.Stdout, "%s %s\n", app.terminal(app.Stdout).Success("Workflow already installed:"), plan.destination)
		return nil
	}
	managedRoot := (storage.Layout{Root: dataRoot}).CuratedWorkflows()
	if err := storage.ValidateManagedParent(plan.destination, managedRoot, dataRoot, "curated workflow"); err != nil {
		return err
	}
	if app.Context != nil {
		if err := app.Context.Err(); err != nil {
			return err
		}
	}
	if err := atomicfile.Write(plan.destination, plan.contents, 0o644, plan.policy); err != nil {
		return err
	}
	fmt.Fprintf(app.Stdout, "%s %s\n", app.terminal(app.Stdout).Success("Installed workflow:"), plan.destination)
	return nil
}
