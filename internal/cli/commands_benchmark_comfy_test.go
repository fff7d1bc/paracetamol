package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/storage"
)

func TestValidateComfySuiteResumeRequiresCompletedEvidence(t *testing.T) {
	dataRoot := t.TempDir()
	artifact := catalog.Artifact{ID: "artifact", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 42}
	bundle := catalog.Bundle{ID: "bundle", Artifacts: []string{artifact.ID}}
	managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{artifact.ID: artifact}, Bundles: map[string]catalog.Bundle{bundle.ID: bundle}, Benchmarks: map[string]catalog.Benchmark{bundle.ID: {SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Renderer: "identity"}}}
	configuration := benchmark.ComfyConfiguration{Image: "image", ImageID: "sha256:image", Profile: "strix-halo", RenderNode: "/dev/dri/renderD128", Runs: 1, Seed: 10, MemoryPolicy: "balanced", KernelPolicy: "default", CacheMode: "persistent"}
	resultPath := filepath.Join((storage.Layout{Root: dataRoot}).ComfyBenchmarks(), "result.json")
	result := benchmark.ComfyResult{
		Schema: benchmark.ComfyResultSchema, Bundle: bundle.ID, Status: "complete", StartedAt: "start", FinishedAt: "finish",
		Profile: configuration.Profile, RenderNode: configuration.RenderNode, MemoryPolicy: configuration.MemoryPolicy, KernelPolicy: configuration.KernelPolicy, CacheMode: configuration.CacheMode,
		Image:     benchmark.ImageIdentity{Reference: configuration.Image, ID: configuration.ImageID},
		Workflow:  benchmark.ComfyWorkflowEvidence{SourceSHA256: managed.Benchmarks[bundle.ID].SHA256, Renderer: "identity"},
		Artifacts: comfyArtifactEvidence(managed, bundle),
		Runs:      []benchmark.ComfyRun{{Index: 0, Kind: "cold", Seed: 10, PromptID: "prompt", WallSeconds: 1, Outputs: json.RawMessage(`{}`)}},
	}
	if err := benchmark.WriteJSON(resultPath, result); err != nil {
		t.Fatal(err)
	}
	suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12", Status: "running", CreatedAt: "now", Configuration: configuration, Entries: []benchmark.ComfySuiteEntry{{Bundle: bundle.ID, Result: resultPath, Status: "pass"}}}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, dataRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resultPath); err != nil {
		t.Fatal(err)
	}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, dataRoot); err == nil {
		t.Fatal("missing completed result was accepted")
	}
}

func TestValidateComfySuiteResumeRejectsDuplicateEntries(t *testing.T) {
	suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12", Status: "failed", CreatedAt: "now", Entries: []benchmark.ComfySuiteEntry{
		{Bundle: "bundle", Status: "fail", Error: "one"},
		{Bundle: "bundle", Status: "fail", Error: "two"},
	}}
	managed := catalog.Catalog{Bundles: map[string]catalog.Bundle{"bundle": {ID: "bundle"}}}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{{ID: "bundle"}}, t.TempDir()); err == nil {
		t.Fatal("duplicate suite entries were accepted")
	}
}

func TestValidateComfySuiteResumeRequiresFailedEntryToHaveNoResult(t *testing.T) {
	bundle := catalog.Bundle{ID: "bundle"}
	managed := catalog.Catalog{Bundles: map[string]catalog.Bundle{bundle.ID: bundle}}
	suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12", Status: "failed", CreatedAt: "now", Entries: []benchmark.ComfySuiteEntry{{Bundle: bundle.ID, Status: "fail", Result: "/unexpected/result.json", Error: "failed"}}}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, t.TempDir()); err == nil {
		t.Fatal("failed suite entry retained a misleading result path")
	}
}

func TestUpsertComfySuiteEntryReplacesFailedAttempt(t *testing.T) {
	entries := []benchmark.ComfySuiteEntry{{Bundle: "bundle", Status: "fail", Error: "old"}}
	entries = upsertComfySuiteEntry(entries, benchmark.ComfySuiteEntry{Bundle: "bundle", Status: "pass", Result: "result.json"})
	if len(entries) != 1 || entries[0].Status != "pass" || entries[0].Error != "" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestComfySuiteSignatureBindsImageAndArtifactBytes(t *testing.T) {
	artifact := catalog.Artifact{ID: "artifact", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 42}
	bundle := catalog.Bundle{ID: "bundle", Artifacts: []string{artifact.ID}}
	managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{artifact.ID: artifact}, Benchmarks: map[string]catalog.Benchmark{bundle.ID: {SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Renderer: "identity"}}}
	options := comfyBenchmarkOptions{image: "image", imageID: "sha256:one", runs: 1}
	initial, err := comfySuiteSignature(managed, []catalog.Bundle{bundle}, options)
	if err != nil {
		t.Fatal(err)
	}
	options.imageID = "sha256:two"
	rebuilt, err := comfySuiteSignature(managed, []catalog.Bundle{bundle}, options)
	if err != nil {
		t.Fatal(err)
	}
	artifact.SHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	managed.Artifacts[artifact.ID] = artifact
	changedArtifact, err := comfySuiteSignature(managed, []catalog.Bundle{bundle}, options)
	if err != nil {
		t.Fatal(err)
	}
	if initial == rebuilt || rebuilt == changedArtifact {
		t.Fatal("suite signature did not bind rebuilt image and changed artifact")
	}
}
