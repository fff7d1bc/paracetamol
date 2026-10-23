package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/controlerr"
)

func TestSpeculativeSweepRejectsStreamedPresetsBeforeHostResolution(t *testing.T) {
	managed, err := catalog.Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, presetID := range []string{
		"unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl-dflash2",
		"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl-dflash2",
	} {
		for name, extra := range map[string][]string{
			"default sweep":   nil,
			"ngram override":  {"--ngram-simple"},
			"reviewed depth":  {"--draft-depth", "3"},
			"different depth": {"--draft-depth", "4"},
		} {
			for _, dryRun := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry-run=%t", presetID, name, dryRun), func(t *testing.T) {
					runner := &commandRunner{}
					app, stdout, stderr := testApp(t, runner)
					app.catalog = &managed
					root := t.TempDir()
					args := []string{"benchmark", "llama-cpp", "speculative", "--preset", presetID,
						"--profile", "strix-halo", "--render-node", "/dev/dri/renderD999999",
						"--data-dir", filepath.Join(root, "data"), "--output", filepath.Join(root, "checkpoint.json")}
					args = append(args, extra...)
					if dryRun {
						args = append(args, "--dry-run")
					}
					err := app.Dispatch(args)
					var controlled *controlerr.Error
					if !errors.As(err, &controlled) || controlled.Status != 2 || !strings.Contains(controlled.Message, presetID) || !strings.Contains(controlled.Message, "reviewed depth-3 DFlash tuple") || !strings.Contains(controlled.Message, "use gateway-based probes instead") {
						t.Fatalf("missing early streamed-policy usage error: %v", err)
					}
					if len(runner.commands) != 0 || app.configuration != nil || stdout.Len() != 0 || stderr.Len() != 0 {
						t.Fatalf("rejected sweep reached host planning/execution: commands=%v configuration=%v stdout=%q stderr=%q", runner.commands, app.configuration, stdout.String(), stderr.String())
					}
					entries, err := os.ReadDir(root)
					if err != nil || len(entries) != 0 {
						t.Fatalf("rejected sweep created persistent data/checkpoint: entries=%v err=%v", entries, err)
					}
				})
			}
		}
	}
}

func TestSpeculativeSweepKeepsResidentDFlashEligible(t *testing.T) {
	// Generic resident decoding stays eligible independently of which model
	// families have passed hardware acceptance and remain in the real catalog.
	managed := catalog.Catalog{LlamaPresets: map[string]catalog.LlamaPreset{
		"resident-dflash-fixture": {ID: "resident-dflash-fixture", DefaultContext: 262144,
			SpeculativeType: "draft-dflash", DraftTokens: 3, AgentTools: true,
			DraftArtifact: "draft-fixture", ModelLoad: map[string]string{"strix-halo": catalog.LlamaModelLoadResident}},
	}}
	runner := &commandRunner{}
	app, _, _ := testApp(t, runner)
	app.catalog = &managed
	err := app.Dispatch([]string{"benchmark", "llama-cpp", "speculative", "--preset", "resident-dflash-fixture", "--profile", "invalid", "--dry-run"})
	var controlled *controlerr.Error
	if !errors.As(err, &controlled) || controlled.Status != 2 || controlled.Message != "speculative sweeps require a valid GPU profile" || len(runner.commands) != 0 {
		t.Fatalf("resident DFlash no longer reaches ordinary sweep validation: %v", err)
	}
}

func TestValidateSpeculativeResumeBindsDefinitionAndSchedule(t *testing.T) {
	planned := makeSpeculativeTrials([]int{1, 2}, []int{4096}, 1, 42)
	definition := speculativeDefinition{Preset: "preset", Depths: []int{1, 2}, ContextDepths: []int{4096}, Repetitions: 1, Seed: 42}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	result := speculativeResult{Schema: benchmark.SpeculativeSchema, SuiteID: "20260820T010203Z-abcdef12", Definition: definition, Fingerprint: fingerprint, Status: "preparing", StartedAt: "2026-08-20T01:02:03Z", Trials: append([]benchmark.SpeculativeTrial(nil), planned...)}
	if err := validateSpeculativeResume(result, planned); err != nil {
		t.Fatal(err)
	}
	result.Trials[0].Depth = 8
	if err := validateSpeculativeResume(result, planned); err == nil {
		t.Fatal("tampered trial was accepted")
	}
	result.Trials = append([]benchmark.SpeculativeTrial(nil), planned...)
	result.Definition.Preset = "other"
	if err := validateSpeculativeResume(result, planned); err == nil {
		t.Fatal("tampered definition was accepted")
	}
}

func TestIntListRejectsTrailingInput(t *testing.T) {
	var values intList
	if err := values.Set("12oops"); err == nil {
		t.Fatal("integer with trailing input was accepted")
	}
	if err := values.Set("12"); err != nil || len(values) != 1 || values[0] != 12 {
		t.Fatalf("valid integer was not accepted: %v, %v", values, err)
	}
}

func TestValidateSpeculativeEnvelopeRejectsInconsistentTrials(t *testing.T) {
	definition := speculativeDefinition{Preset: "preset"}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	base := speculativeResult{
		Schema: benchmark.SpeculativeSchema, SuiteID: "20260820T010203Z-abcdef12",
		Definition: definition, Fingerprint: fingerprint, Status: "running", StartedAt: "2026-08-20T01:02:03Z",
	}
	tests := []benchmark.SpeculativeTrial{
		{Identifier: "pending-with-metrics", Status: "pending", Metrics: &benchmark.SpeculativeMetrics{}},
		{Identifier: "complete-without-metrics", Status: "complete", StartedAt: "start", FinishedAt: "finish"},
		{Identifier: "failed-without-error", Status: "failed", StartedAt: "start", FinishedAt: "finish"},
	}
	for _, trial := range tests {
		result := base
		result.Trials = []benchmark.SpeculativeTrial{trial}
		if err := validateSpeculativeEnvelope(result); err == nil {
			t.Fatalf("inconsistent %q trial was accepted", trial.Identifier)
		}
	}
	base.Trials = []benchmark.SpeculativeTrial{{Identifier: "duplicate", Status: "pending"}, {Identifier: "duplicate", Status: "pending"}}
	if err := validateSpeculativeEnvelope(base); err == nil {
		t.Fatal("duplicate trial identifier was accepted")
	}
}

func TestValidSpeculativeMetrics(t *testing.T) {
	metrics := benchmark.SpeculativeMetrics{
		RequestSeconds: 1, StartupSeconds: 2, PromptTokens: 3, CompletionTokens: 4,
		DraftedTokens: 5, AcceptedDraftTokens: 4, GenerationTokensPerSecond: 6,
		PromptTokensPerSecond: 7, PredictedMS: 8, PromptMS: 9, AcceptancePercent: 80,
		ResponseSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Message:        json.RawMessage(`[{"message":{"content":"ok"}}]`),
	}
	if !validSpeculativeMetrics(metrics) {
		t.Fatal("valid speculative metrics were rejected")
	}
	metrics.AcceptedDraftTokens = 6
	if validSpeculativeMetrics(metrics) {
		t.Fatal("impossible speculative metrics were accepted")
	}
}
