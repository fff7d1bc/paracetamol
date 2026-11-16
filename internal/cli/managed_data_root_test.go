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
	"paracetamol/internal/content"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
	"paracetamol/internal/verification"
)

func TestManagedDataRootAliasesAgreeAcrossInventoryStatusAndDryRun(t *testing.T) {
	for _, shape := range []string{"root-alias", "ancestor-alias"} {
		for _, operation := range []string{"inventory", "content-status", "workflow-status", "application-status", "direct-dry-run"} {
			t.Run(shape+"/"+operation, func(t *testing.T) {
				root, selected, managed := managedAliasFixture(t, shape)
				receipt := (storage.Layout{Root: root}).VerificationReceipt()
				before, err := os.ReadFile(receipt)
				if err != nil {
					t.Fatal(err)
				}
				runner := &commandRunner{run: gatewayOwnershipPodman}
				app, stdout, _ := testApp(t, runner)
				app.catalog = &managed
				args := map[string][]string{
					"inventory":          {"content", "list", "models"},
					"content-status":     {"content", "status", "bundle"},
					"workflow-status":    {"content", "workflows", "status", "graph"},
					"application-status": {"status", "comfyui"},
					"direct-dry-run":     {"run", "llama-cpp", "server", "--preset", "fixture", "--profile", "cpu", "--dry-run"},
				}[operation]
				if err := app.Dispatch(append(args, "--data-dir", selected)); err != nil {
					t.Fatal(err)
				}
				output := stdout.String()
				want := "ready"
				if operation == "workflow-status" {
					want = "installed"
				} else if operation == "application-status" {
					want = "1/1 bundles ready"
				} else if operation == "direct-dry-run" {
					want = (storage.Layout{Root: root}).LlamaModels() + ":/content/models:ro"
				}
				if !strings.Contains(output, want) || !strings.Contains(output, root) || strings.Contains(output, selected) {
					t.Fatalf("alias did not use canonical ready state; want %q below %s:\n%s", want, root, output)
				}
				if after, err := os.ReadFile(receipt); err != nil || !bytes.Equal(before, after) {
					t.Fatalf("read-only operation changed receipt: %q, %v", after, err)
				}
				for _, path := range []string{(storage.Layout{Root: root}).Application("llama-cpp"), (storage.Layout{Root: root}).Staging()} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Fatalf("read-only operation created %s: %v", path, err)
					}
				}
				for _, command := range runner.commands {
					if len(command.Args) > 0 && command.Args[0] == "run" {
						t.Fatalf("read-only operation launched a workload: %v", command)
					}
				}
			})
		}
	}
}

func TestGatewayRootAliasesReachFakePreflightWithoutStartingService(t *testing.T) {
	for _, shape := range []string{"root-alias", "ancestor-alias"} {
		t.Run(shape, func(t *testing.T) {
			root, selected, managed := managedAliasFixture(t, shape)
			// Raw low-level inspection is intentionally strict; only selection of
			// the data root may resolve aliases, not paths inside its partitions.
			if shape == "root-alias" {
				if _, err := content.InspectArtifact(nil, selected, managed.Artifacts["model"], false); err == nil {
					t.Fatal("raw inspector accepted a symlinked data root")
				}
			}
			runner := &commandRunner{run: func(command process.Command) process.Result {
				return process.Result{Stdout: []byte("false\n")}
			}}
			app, _, stderr := testApp(t, runner)
			app.catalog = &managed
			// Explicit application selection is strict: reaching lifecycle's fake
			// rootless probe proves the aliased inventory passed receipt readiness.
			err := app.runGateway([]string{"--application", "llama-cpp", "--profile", "cpu", "--data-dir", selected})
			if err == nil || !strings.Contains(err.Error(), "rootful Podman is not supported") || strings.Contains(stderr.String(), "Unavailable:") {
				t.Fatalf("aliased gateway did not reach fake lifecycle preflight: %v, %s", err, stderr)
			}
			probes := 0
			for _, command := range runner.commands {
				if command.Name == "git" {
					continue // Read-only source identity probe, also faked.
				}
				if command.Name != "podman" || len(command.Args) == 0 || command.Args[0] != "info" {
					t.Fatalf("unexpected gateway host work: %v", runner.commands)
				}
				probes++
			}
			if probes != 1 {
				t.Fatalf("gateway did not reach its one fake rootless probe: %v", runner.commands)
			}
			if _, err := os.Lstat((storage.Layout{Root: root}).Application("llama-cpp")); !os.IsNotExist(err) {
				t.Fatalf("gateway preflight prepared persistent state: %v", err)
			}
			assertGatewayOwnershipReleased(t, app.Environment)
		})
	}
}

func TestManagedDataRootReadOnlyCommandsKeepMissingAliasSuffixAbsent(t *testing.T) {
	for _, operation := range []string{"inventory", "content-status", "workflow-status", "application-status", "gateway"} {
		t.Run(operation, func(t *testing.T) {
			root, selected, managed := managedAliasFixture(t, "ancestor-alias")
			root, selected = filepath.Join(root, "absent", "data"), filepath.Join(selected, "absent", "data")
			runner := &commandRunner{run: gatewayOwnershipPodman}
			app, stdout, _ := testApp(t, runner)
			app.catalog = &managed
			args := map[string][]string{
				"inventory":          {"content", "list", "models"},
				"content-status":     {"content", "status", "bundle"},
				"workflow-status":    {"content", "workflows", "status", "graph"},
				"application-status": {"status", "comfyui"},
				"gateway":            {"run", "gateway", "--application", "llama-cpp", "--profile", "cpu"},
			}[operation]
			err := app.Dispatch(append(args, "--data-dir", selected))
			wantError := operation == "content-status" || operation == "workflow-status" || operation == "gateway"
			if (err != nil) != wantError || err != nil && strings.Contains(err.Error(), "symlink") {
				t.Fatalf("missing suffix error = %v", err)
			}
			if operation != "gateway" && (!strings.Contains(stdout.String(), root) || strings.Contains(stdout.String(), selected)) {
				t.Fatalf("missing suffix not canonicalized: %s", stdout)
			}
			if _, err := os.Lstat(filepath.Dir(root)); !os.IsNotExist(err) {
				t.Fatalf("read-only resolution created missing suffix: %v", err)
			}
			for _, command := range runner.commands {
				if len(command.Args) > 0 && command.Args[0] == "run" {
					t.Fatalf("missing-root inspection launched workload: %v", command)
				}
			}
		})
	}
}

func TestManagedDataRootResolutionDoesNotResolvePartitionLinks(t *testing.T) {
	root, selected, managed := managedAliasFixture(t, "root-alias")
	partition := (storage.Layout{Root: root}).LlamaModels()
	if err := os.Rename(partition, partition+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(partition+"-original", partition); err != nil {
		t.Fatal(err)
	}
	if _, err := content.InspectArtifact(nil, root, managed.Artifacts["model"], false); err == nil || !strings.Contains(err.Error(), "symlinked content") {
		t.Fatalf("low-level inspection accepted linked partition: %v", err)
	}
	app, _, _ := testApp(t, &commandRunner{})
	app.catalog = &managed
	if err := app.contentList([]string{"models", "--data-dir", selected}); err == nil || !strings.Contains(err.Error(), "symlinked content") {
		t.Fatalf("canonicalizing selected root accepted linked partition: %v", err)
	}
}

func managedAliasFixture(t *testing.T, shape string) (string, string, catalog.Catalog) {
	t.Helper()
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	root := filepath.Join(physical, "data")
	managed := gatewayOwnershipCatalog(t, root)
	comfy := catalog.Artifact{ID: "comfy", Target: "models", Destination: "fixture/model.safetensors", Size: 1, SHA256: strings.Repeat("0", 64)}
	managed.Artifacts[comfy.ID] = comfy
	managed.Bundles["comfy"] = catalog.Bundle{ID: "comfy", Application: "comfyui", Artifacts: []string{comfy.ID}}
	file := content.ArtifactPath(root, comfy)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(file, comfy.Size, comfy.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	graph := []byte("{}\n")
	digest := sha256.Sum256(graph)
	workflow := catalog.Workflow{ID: "graph", Destination: "fixture.json", RenderedSHA256: fmt.Sprintf("%x", digest)}
	managed.Workflows = map[string]catalog.Workflow{workflow.ID: workflow}
	file = workflowPath(root, workflow)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, graph, 0o600); err != nil {
		t.Fatal(err)
	}
	alias, target, selected := filepath.Join(base, "alias"), root, filepath.Join(base, "alias")
	if shape == "ancestor-alias" {
		target, selected = physical, filepath.Join(alias, "data")
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	return root, selected, managed
}
