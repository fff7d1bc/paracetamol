package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/content"
	"paracetamol/internal/storage"
)

func workflowPreflightFixture(t *testing.T) (*App, *commandRunner, string) {
	t.Helper()
	base := t.TempDir()
	runner := &commandRunner{run: gatewayOwnershipPodman}
	app, _, _ := testApp(t, runner)
	app.Root = filepath.Join(base, "checkout")
	app.Environment = map[string]string{"HOME": filepath.Join(base, "home"), "XDG_RUNTIME_DIR": filepath.Join(base, "runtime")}
	app.ConfigSelection = config.Selection{Disabled: true}
	payload := []byte("model fixture\n")
	artifact := catalog.Artifact{ID: "model", Target: "models", Destination: "model.bin", Size: int64(len(payload)), SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Source: catalog.Source{Path: "model.bin"}, License: catalog.License{Status: "verified"}}
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{artifact.ID: artifact},
		Bundles:   map[string]catalog.Bundle{},
		Workflows: map[string]catalog.Workflow{},
	}
	for _, id := range []string{"first", "last"} {
		contents := []byte(fmt.Sprintf("{\"fixture\":%q}\n", id))
		workflow := catalog.Workflow{ID: id, Destination: id + ".json", RenderedSHA256: fmt.Sprintf("%x", sha256.Sum256(contents))}
		managed.Workflows[id] = workflow
		managed.Bundles[id] = catalog.Bundle{ID: id, Application: "comfyui", Artifacts: []string{artifact.ID}, Workflow: id, Groups: []string{"all"}}
		file := filepath.Join(app.Root, "catalog", "workflows", id+".json")
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app.catalog = &managed
	return app, runner, filepath.Join(base, "data")
}

func assertNoWorkflowInstallMutation(t *testing.T, app *App, runner *commandRunner, data string) {
	t.Helper()
	if len(runner.commands) != 0 {
		t.Fatalf("rejected preflight ran subprocesses: %+v", runner.commands)
	}
	layout := storage.Layout{Root: data}
	for _, path := range []string{layout.ComfyModels(), layout.Staging(), layout.VerificationReceipt(), workflowPath(data, app.catalog.Workflows["first"]), app.Environment["XDG_RUNTIME_DIR"]} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("rejected preflight mutated %s: %v", path, err)
		}
	}
}

func prepareWorkflowOfflineInstall(t *testing.T, app *App, data string) string {
	t.Helper()
	mirror := filepath.Join(filepath.Dir(data), "mirror")
	if err := os.Mkdir(mirror, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mirror, "model.bin"), []byte("model fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(app.Environment["XDG_RUNTIME_DIR"], 0o700); err != nil {
		t.Fatal(err)
	}
	return mirror
}

func TestContentInstallPreflightsAllWorkflowsBeforeApprovalAndMutation(t *testing.T) {
	for _, shape := range []string{"modified", "file-symlink", "directory", "parent-symlink", "hardlink", "missing-resource", "changed-resource"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", shape, dryRun), func(t *testing.T) {
				app, runner, data := workflowPreflightFixture(t)
				// Missing approval would fail later. The existing workflow
				// conflict must win before any interactive or mutation boundary.
				artifact := app.catalog.Artifacts["model"]
				artifact.License.Status = "unverified"
				app.catalog.Artifacts["model"] = artifact
				workflow := app.catalog.Workflows["last"]
				destination := workflowPath(data, workflow)
				outside := filepath.Join(filepath.Dir(data), "unrelated")
				writeOwnershipSentinel(t, filepath.Join(outside, "sentinel"))
				args := []string{"all", "--data-dir", data, "--non-interactive"}
				if dryRun {
					args = append(args, "--dry-run")
				}
				want := "workflow"
				if shape == "missing-resource" || shape == "changed-resource" {
					resource := filepath.Join(app.Root, "catalog", "workflows", workflow.ID+".json")
					if shape == "missing-resource" {
						if err := os.Remove(resource); err != nil {
							t.Fatal(err)
						}
					} else if err := os.WriteFile(resource, []byte("changed source"), 0o644); err != nil {
						t.Fatal(err)
					}
				} else if shape == "parent-symlink" {
					curated := (storage.Layout{Root: data}).CuratedWorkflows()
					if err := os.MkdirAll(filepath.Dir(curated), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, curated); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
						t.Fatal(err)
					}
					switch shape {
					case "modified":
						writeOwnershipSentinel(t, destination)
					case "file-symlink":
						if err := os.Symlink(filepath.Join(outside, "sentinel"), destination); err != nil {
							t.Fatal(err)
						}
					case "directory":
						if err := os.Mkdir(destination, 0o755); err != nil {
							t.Fatal(err)
						}
					case "hardlink":
						if err := os.Link(filepath.Join(outside, "sentinel"), destination); err != nil {
							t.Fatal(err)
						}
						args = append(args, "--force-workflow")
						want = "singly linked"
					}
				}
				if err := app.contentInstall(args); err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("preflight error=%v, want %q", err, want)
				}
				assertNoWorkflowInstallMutation(t, app, runner, data)
				if got, err := os.ReadFile(filepath.Join(outside, "sentinel")); err != nil || string(got) != "must survive\n" {
					t.Fatalf("changed unrelated bytes: %q %v", got, err)
				}
				if shape == "modified" || shape == "hardlink" {
					if got, err := os.ReadFile(destination); err != nil || string(got) != "must survive\n" {
						t.Fatalf("changed user workflow: %q %v", got, err)
					}
				}
			})
		}
	}
}

func TestContentInstallWorkflowPreflightPreservesDryRunAndExplicitForce(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%t", force), func(t *testing.T) {
			app, runner, data := workflowPreflightFixture(t)
			last := workflowPath(data, app.catalog.Workflows["last"])
			if force {
				writeOwnershipSentinel(t, last)
			}
			args := []string{"all", "--data-dir", data, "--non-interactive"}
			if force {
				args = append(args, "--force-workflow")
			}
			if err := app.contentInstall(append(append([]string(nil), args...), "--dry-run")); err != nil {
				t.Fatal(err)
			}
			assertNoWorkflowInstallMutation(t, app, runner, data)
			if !force {
				if _, err := os.Lstat(data); !os.IsNotExist(err) {
					t.Fatalf("valid dry-run created data root: %v", err)
				}
			} else if got, err := os.ReadFile(last); err != nil || string(got) != "must survive\n" {
				t.Fatalf("dry-run replaced user workflow: %q %v", got, err)
			}
			mirror := prepareWorkflowOfflineInstall(t, app, data)
			if err := app.contentInstall(append(args, "--local-mirror", mirror)); err != nil {
				t.Fatal(err)
			}
			for _, workflow := range app.catalog.Workflows {
				if !workflowReady(data, workflow) {
					t.Fatalf("valid install left workflow %s unready", workflow.ID)
				}
			}
			if _, err := content.RequireBundle(*app.catalog, app.catalog.Bundles["first"], data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestContentInstallRechecksWorkflowsAfterApproval(t *testing.T) {
	app, runner, data := workflowPreflightFixture(t)
	artifact := app.catalog.Artifacts["model"]
	artifact.License.Status = "unverified"
	app.catalog.Artifacts["model"] = artifact
	app.Stdin = preflightTerminalReader{Reader: strings.NewReader("yes\n")}
	prompted := false
	var output bytes.Buffer
	app.Stdout = ownershipHookWriter{Writer: &output, hook: func(value string) {
		if !prompted && strings.Contains(value, "Acknowledge the unresolved licensing") {
			prompted = true
			writeOwnershipSentinel(t, workflowPath(data, app.catalog.Workflows["last"]))
		}
	}}
	if err := app.contentInstall([]string{"all", "--data-dir", data}); err == nil || !strings.Contains(err.Error(), "workflow differs") {
		t.Fatalf("late conflict error=%v", err)
	}
	if !prompted {
		t.Fatal("test did not reach approval")
	}
	assertNoWorkflowInstallMutation(t, app, runner, data)
}

func TestContentInstallRechecksWorkflowsAfterModelInstallation(t *testing.T) {
	app, _, data := workflowPreflightFixture(t)
	mirror := prepareWorkflowOfflineInstall(t, app, data)
	changed := false
	var output bytes.Buffer
	destination := workflowPath(data, app.catalog.Workflows["first"])
	app.Stdout = ownershipHookWriter{Writer: &output, hook: func(value string) {
		if !changed && strings.Contains(value, "Installed ") && strings.Contains(value, "model.bin") {
			changed = true
			writeOwnershipSentinel(t, destination)
		}
	}}
	err := app.contentInstall([]string{"all", "--data-dir", data, "--non-interactive", "--local-mirror", mirror})
	if !changed || err == nil || !strings.Contains(err.Error(), "workflow differs") {
		t.Fatalf("late conflict changed=%t err=%v output=%s", changed, err, output.String())
	}
	// A concurrent change after transfer may leave verified models installed,
	// but must not overwrite the newly changed user workflow.
	if _, err := content.RequireBundle(*app.catalog, app.catalog.Bundles["first"], data); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "must survive\n" {
		t.Fatalf("late user workflow changed: %q %v", got, err)
	}
	if _, err := os.Lstat(workflowPath(data, app.catalog.Workflows["last"])); !os.IsNotExist(err) {
		t.Fatalf("continued workflow publication after conflict: %v", err)
	}
}

func TestWorkflowOnlyValidationDoesNotCreateDataRoot(t *testing.T) {
	app, runner, data := workflowPreflightFixture(t)
	resource := filepath.Join(app.Root, "catalog", "workflows", "first.json")
	if err := os.Remove(resource); err != nil {
		t.Fatal(err)
	}
	if err := app.contentWorkflows([]string{"install", "first", "--data-dir", data}); err == nil {
		t.Fatal("installed workflow with missing resource")
	}
	assertNoWorkflowInstallMutation(t, app, runner, data)
	if _, err := os.Lstat(data); !os.IsNotExist(err) {
		t.Fatalf("failed workflow-only install created data: %v", err)
	}
}
