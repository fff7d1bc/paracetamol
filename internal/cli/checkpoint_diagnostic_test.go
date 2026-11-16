package cli

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
)

func TestCheckpointDiagnosticPreservesPrintableSingleLineCauses(t *testing.T) {
	if got := checkpointDiagnostic(nil); got != "" {
		t.Fatalf("nil error diagnostic = %q", got)
	}
	if got := checkpointDiagnostic(errors.New("")); got != `""` {
		t.Fatalf("empty error diagnostic = %q", got)
	}
	for name, failure := range checkpointDiagnosticFailures() {
		t.Run(name, func(t *testing.T) {
			assertCheckpointDiagnostic(t, checkpointDiagnostic(failure), failure)
		})
	}
}

func TestAcceptanceAttemptDiagnosticsRoundTripAndRetry(t *testing.T) {
	for name, failure := range checkpointDiagnosticFailures() {
		t.Run(name, func(t *testing.T) {
			definition := acceptanceDefinition{}
			fingerprint, err := jsonDigest(definition)
			if err != nil {
				t.Fatal(err)
			}
			candidate := acceptanceCase{id: "host-gpu"}
			result := acceptanceResult{
				Schema: acceptanceSchema, SuiteID: "20260820T010203Z-abcdef12", Definition: definition,
				Fingerprint: fingerprint, Status: "running", StartedAt: "now",
				Cases: []acceptanceEntry{{Identifier: candidate.id, Status: "pending"}},
			}
			path := filepath.Join(t.TempDir(), "acceptance.json")
			if err := benchmark.WriteNewCheckpoint(path, result); err != nil {
				t.Fatal(err)
			}
			var attemptIDs []string
			for attempt := range 2 {
				if err := runAcceptanceAttempt(path, &result, 0, func(id string) ([]string, error) {
					attemptIDs = append(attemptIDs, id)
					return nil, failure
				}); err != nil {
					t.Fatal(err)
				}
				if err := readAcceptanceResult(path, &result); err != nil {
					t.Fatal(err)
				}
				if err := validateAcceptanceResult(result, []acceptanceCase{candidate}); err != nil {
					t.Fatalf("attempt %d cannot resume: %v", attempt+1, err)
				}
				entry := result.Cases[0]
				if entry.Status != "fail" || entry.Attempts != attempt+1 || len(entry.PreviousAttempts) != attempt {
					t.Fatalf("unexpected failed attempt: %#v", entry)
				}
				assertCheckpointDiagnostic(t, entry.Reason, failure)
				for _, previous := range entry.PreviousAttempts {
					assertCheckpointDiagnostic(t, previous.Reason, failure)
				}
			}
			if attemptIDs[0] == attemptIDs[1] {
				t.Fatal("retry reused failed attempt identity")
			}
			if err := runAcceptanceAttempt(path, &result, 0, func(string) ([]string, error) { return nil, nil }); err != nil {
				t.Fatal(err)
			}
			if err := readAcceptanceResult(path, &result); err != nil {
				t.Fatal(err)
			}
			if err := validateAcceptanceResult(result, []acceptanceCase{candidate}); err != nil {
				t.Fatalf("passed retry has invalid history: %v", err)
			}
			if entry := result.Cases[0]; entry.Status != "pass" || entry.Attempts != 3 || len(entry.PreviousAttempts) != 2 {
				t.Fatalf("unexpected passed retry: %#v", entry)
			}
			// Existing invalid checkpoints are not silently repaired on read.
			result.Cases[0].Status, result.Cases[0].Reason = "fail", failure.Error()
			if err := validateAcceptanceResult(result, []acceptanceCase{candidate}); err == nil {
				t.Fatal("raw multiline diagnostic was accepted")
			}
		})
	}
}

func TestComfySuiteAttemptDiagnosticsRoundTripAndRetry(t *testing.T) {
	for name, failure := range checkpointDiagnosticFailures() {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "suite.json")
			bundle := catalog.Bundle{ID: "bundle"}
			managed := catalog.Catalog{Bundles: map[string]catalog.Bundle{bundle.ID: bundle}}
			suite := benchmark.ComfySuite{Schema: benchmark.ComfySuiteSchema, SuiteID: "20260820T010203Z-abcdef12", CreatedAt: "now", Status: "running"}
			if err := benchmark.WriteNewCheckpoint(path, suite); err != nil {
				t.Fatal(err)
			}
			var attemptIDs []string
			for attempt := range 2 {
				runErr, writeErr := runComfySuiteAttempt(path, &suite, bundle.ID, root, func(id string) (string, comfySummary, error) {
					attemptIDs = append(attemptIDs, id)
					return "", comfySummary{}, failure
				})
				if runErr != failure || writeErr != nil {
					t.Fatalf("original workload error changed or checkpoint failed: %v, %v", runErr, writeErr)
				}
				if err := benchmark.ReadJSON(path, &suite, true); err != nil {
					t.Fatal(err)
				}
				if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, root); err != nil {
					t.Fatalf("attempt %d cannot resume: %v", attempt+1, err)
				}
				entry := suite.Entries[0]
				if entry.Status != "fail" || entry.Attempts != attempt+1 || len(entry.PreviousAttempts) != attempt {
					t.Fatalf("unexpected failed attempt: %#v", entry)
				}
				assertCheckpointDiagnostic(t, entry.Error, failure)
				for _, previous := range entry.PreviousAttempts {
					assertCheckpointDiagnostic(t, previous.Error, failure)
				}
			}
			if attemptIDs[0] == attemptIDs[1] {
				t.Fatal("retry reused failed attempt identity")
			}
			runErr, writeErr := runComfySuiteAttempt(path, &suite, bundle.ID, root, func(id string) (string, comfySummary, error) {
				child := comfySuiteAttemptResult(root, id, bundle.ID)
				result := benchmark.ComfyResult{
					Schema: benchmark.ComfyResultSchema, RunID: id, Bundle: bundle.ID, Status: "complete",
					StartedAt: "start", FinishedAt: "finish", Artifacts: comfyArtifactEvidence(managed, bundle),
				}
				return child, comfySummary{}, benchmark.WriteJSON(child, result)
			})
			if runErr != nil || writeErr != nil {
				t.Fatalf("passed retry failed: %v, %v", runErr, writeErr)
			}
			if err := benchmark.ReadJSON(path, &suite, true); err != nil {
				t.Fatal(err)
			}
			if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, root); err != nil {
				t.Fatalf("passed retry has invalid history: %v", err)
			}
			if entry := suite.Entries[0]; entry.Status != "pass" || entry.Attempts != 3 || len(entry.PreviousAttempts) != 2 {
				t.Fatalf("unexpected passed retry: %#v", entry)
			}
			suite.Entries[0].Status, suite.Entries[0].Error = "fail", failure.Error()
			if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, root); err == nil {
				t.Fatal("raw multiline diagnostic was accepted")
			}
		})
	}
}

func TestAcceptanceAttemptDiagnosticRetainsEvidenceFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "suite.json")
	missingArtifact := filepath.Join(root, "missing.json")
	failure := withCleanupFailure(errors.New("fixture workload failure"), "fixture cleanup", errors.New("fixture removal failure"))
	_, evidenceErr := captureAcceptanceArtifacts([]string{missingArtifact})
	if evidenceErr == nil {
		t.Fatal("missing fixture artifact was accepted")
	}
	result := acceptanceResult{SuiteID: "20260820T010203Z-abcdef12", Cases: []acceptanceEntry{{Identifier: "host-gpu", Status: "pending"}}}
	if err := benchmark.WriteNewCheckpoint(path, result); err != nil {
		t.Fatal(err)
	}
	if err := runAcceptanceAttempt(path, &result, 0, func(string) ([]string, error) {
		return []string{missingArtifact}, failure
	}); err != nil {
		t.Fatal(err)
	}
	if err := readAcceptanceResult(path, &result); err != nil {
		t.Fatal(err)
	}
	assertCheckpointDiagnostic(t, result.Cases[0].Reason, errors.Join(failure, evidenceErr))
}

func checkpointDiagnosticFailures() map[string]error {
	return map[string]error{
		"joined-workload-cleanup": withCleanupFailure(errors.New("fixture workload failed"), "clean up ComfyUI benchmark container", errors.New("fixture removal failure")),
		"stderr-controls":         withCleanupFailure(errors.New("fixture stderr:\r\n\t\x1b[31mfailed\x1b[0m\nfile \\\"quoted\\\"\x7f\u0085\u2028\xff"), "clean up ComfyUI benchmark container", errors.New("fixture removal failure\r\nsecond cause")),
		"long-all-causes":         withCleanupFailure(errors.New(strings.Repeat("workload context ", 4096)), "clean up ComfyUI benchmark container", errors.New("final cleanup cause")),
	}
}

func assertCheckpointDiagnostic(t *testing.T, diagnostic string, failure error) {
	t.Helper()
	if unsafeCheckpointText(diagnostic) || strings.IndexFunc(diagnostic, func(character rune) bool { return !unicode.IsPrint(character) }) >= 0 {
		t.Fatalf("diagnostic is not a printable single line: %q", diagnostic)
	}
	decoded, err := strconv.Unquote(`"` + diagnostic + `"`)
	if err != nil || decoded != failure.Error() {
		t.Fatalf("diagnostic lost or changed error causes: %q, %v", diagnostic, err)
	}
}
