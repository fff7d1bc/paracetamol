package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/project"
)

func TestInstallWorkflowPreservesAndReplacesUserChangesExplicitly(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	identifiers := make([]string, 0, len(managed.Workflows))
	for identifier := range managed.Workflows {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	workflow := managed.Workflows[identifiers[0]]
	dataRoot := t.TempDir()
	var output bytes.Buffer
	app := App{Root: root, Stdout: &output}

	if err := app.installWorkflow(workflow, dataRoot, false); err != nil {
		t.Fatal(err)
	}
	if !workflowReady(dataRoot, workflow) {
		t.Fatal("freshly installed workflow is not ready")
	}
	destination := workflowPath(dataRoot, workflow)
	if err := os.WriteFile(destination, []byte("user modification\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.installWorkflow(workflow, dataRoot, false); err == nil {
		t.Fatal("differing user workflow was replaced without --force")
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "user modification\n" {
		t.Fatalf("user workflow changed after refused install: %q", contents)
	}
	if err := app.installWorkflow(workflow, dataRoot, true); err != nil {
		t.Fatal(err)
	}
	if !workflowReady(dataRoot, workflow) {
		t.Fatal("forced workflow replacement is not ready")
	}
}
