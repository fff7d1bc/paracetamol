package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/project"
)

func TestExactBundleBrowserCoversCatalogExactlyOnce(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	covered := make(map[string]int)
	for _, category := range exactBundleCategories {
		bundles, err := exactBundlesInCategory(managed, category.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, bundle := range bundles {
			covered[bundle.ID]++
		}
	}
	if len(covered) != len(managed.Bundles) {
		t.Fatalf("browser covers %d of %d bundles", len(covered), len(managed.Bundles))
	}
	for identifier, count := range covered {
		if count != 1 {
			t.Fatalf("bundle %s appears in %d categories", identifier, count)
		}
	}
}

func TestGuidedContentCanBrowseExactApplicationBundles(t *testing.T) {
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"model": {ID: "model", Size: 1024}},
		Bundles: map[string]catalog.Bundle{
			"llama-special": {ID: "llama-special", Application: "llama-cpp", Artifacts: []string{"model"}, Groups: []string{"all"}, Description: "Special quantization"},
		},
	}
	var output bytes.Buffer
	app := App{
		Context: context.Background(), Environment: map[string]string{},
		Stdin: terminalPromptReader{Reader: strings.NewReader("9\n1\n")}, Stdout: &output,
	}
	target, selection, err := app.guidedContentSelection(managed, "llama-cpp")
	if err != nil {
		t.Fatal(err)
	}
	if target != "llama-special" || selection != "" {
		t.Fatalf("selection = %q %q", target, selection)
	}
	for _, expected := range []string{"llama.cpp content:", "exact bundles", "Special quantization"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("guided browser lacks %q:\n%s", expected, output.String())
		}
	}
}

func TestContentModelInventoryIncludesAliasesDwarfStarAndDetailedPolicy(t *testing.T) {
	dataRoot := t.TempDir()
	llama := catalog.Artifact{ID: "llama", Destination: "qwen/model.gguf", Target: "llama-models", Size: 1024, SHA256: "0"}
	dwarf := catalog.Artifact{ID: "dwarf", Destination: "deepseek/model.gguf", Target: "dwarfstar-models", Size: 2048, SHA256: "0"}
	support := catalog.Artifact{ID: "support", Destination: "deepseek/support.gguf", Target: "dwarfstar-models", Size: 512, SHA256: "0"}
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"llama": llama, "dwarf": dwarf, "support": support},
		Bundles: map[string]catalog.Bundle{
			"qwen":         {ID: "qwen", Application: "llama-cpp", Artifacts: []string{"llama"}},
			"deepseek":     {ID: "deepseek", Application: "dwarfstar", Artifacts: []string{"dwarf"}},
			"deepseek-mtp": {ID: "deepseek-mtp", Application: "dwarfstar", Artifacts: []string{"dwarf", "support"}},
		},
		LlamaPresets: map[string]catalog.LlamaPreset{
			"qwen-base": {ID: "qwen-base", Bundle: "qwen", Artifact: "llama", DefaultContext: 65536, Jinja: true},
			"qwen-mtp":  {ID: "qwen-mtp", Bundle: "qwen", Artifact: "llama", DefaultContext: 131072, SpeculativeType: "draft-mtp", DraftTokens: 3, SamplingPolicy: "qwen-policy"},
		},
		DwarfStarPresets: map[string]catalog.DwarfStarPreset{
			"deepseek": {ID: "deepseek", Bundle: "deepseek", DSparkBundle: "deepseek-mtp"},
		},
	}
	var stdout, stderr bytes.Buffer
	app := App{Context: context.Background(), Environment: map[string]string{}, Stdin: bytes.NewReader(nil), Stdout: &stdout, Stderr: &stderr, catalog: &managed}
	if err := app.contentList([]string{"models", "--data-dir", dataRoot, "--details"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"llama.cpp models", "qwen-base, qwen-mtp", "DwarfStar models", "deepseek-mtp", "Managed preset details",
		"Default context", "131072 tokens", "Sampling", "catalog qwen-policy", "content install deepseek-mtp", "run dwarfstar server --dspark",
	} {
		if !bytes.Contains(stdout.Bytes(), []byte(expected)) {
			t.Fatalf("inventory lacks %q:\n%s", expected, stdout.String())
		}
	}
}

func TestLlamaModelLoadInventoryPolicyIncludesResidentStrixDefaults(t *testing.T) {
	got := llamaModelLoadInventoryPolicy(map[string]string{
		"strix-halo": catalog.LlamaModelLoadMMapLazyTokenEmbedding,
	})
	for _, expected := range []string{"strix-halo=mmap-lazy-token-embedding", "strix-point=resident"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("model-load policy %q lacks %q", got, expected)
		}
	}
}

func TestContentListValidatesApplicationFilters(t *testing.T) {
	managed := catalog.Catalog{Bundles: map[string]catalog.Bundle{}, LlamaPresets: map[string]catalog.LlamaPreset{}, DwarfStarPresets: map[string]catalog.DwarfStarPreset{}}
	for _, arguments := range [][]string{{"models", "--application", "comfyui", "--data-dir", t.TempDir()}, {"bundles", "--application", "unknown"}} {
		var stdout, stderr bytes.Buffer
		app := App{Context: context.Background(), Environment: map[string]string{}, Stdout: &stdout, Stderr: &stderr, catalog: &managed}
		if err := app.contentList(arguments); err == nil {
			t.Fatalf("%v succeeded", arguments)
		}
	}
}

func TestContentInstallDryRunStatesThatItDidNotMutateStorage(t *testing.T) {
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"model": {ID: "model", Destination: "model.gguf", Target: "llama-models", Size: 1, SHA256: "0"}},
		Bundles:   map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
	}
	dataRoot := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer
	app := App{Context: context.Background(), Environment: map[string]string{}, Stdin: bytes.NewReader(nil), Stdout: &stdout, Stderr: &stderr, catalog: &managed}
	if err := app.contentInstall([]string{"bundle", "--dry-run", "--data-dir", dataRoot}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Dry run: no data directory was created") {
		t.Fatalf("dry-run output lacks mutation summary:\n%s", stdout.String())
	}
	if _, err := os.Stat(dataRoot); !os.IsNotExist(err) {
		t.Fatalf("dry run created data root: %v", err)
	}
}

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
