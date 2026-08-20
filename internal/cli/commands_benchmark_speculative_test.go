package cli

import (
	"encoding/json"
	"testing"

	"rocmplete/internal/benchmark"
)

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
