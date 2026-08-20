package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAcceptanceResultRejectsUnknownFieldsAndTrailingData(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown":  `{"schema":"paracetamol.hardware-acceptance.v2","unknown":true}`,
		"trailing": `{"schema":"paracetamol.hardware-acceptance.v2"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "acceptance.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			var result acceptanceResult
			if err := readAcceptanceResult(path, &result); err == nil {
				t.Fatal("invalid acceptance checkpoint was accepted")
			}
		})
	}
}

func TestAcceptanceReportStartsWithOutcomeAndInputs(t *testing.T) {
	report := renderAcceptanceReport(acceptanceResult{
		Status: "pass", SuiteID: "suite", Fingerprint: "digest",
		Definition: acceptanceDefinition{Profile: "strix-halo", Architecture: "gfx1151", RenderNode: "/dev/dri/renderD128", MemoryPolicy: "balanced", KernelPolicy: "default", Images: []acceptanceImageIdentity{{Target: "llama-cpp", Reference: "image", ID: "sha256:id"}}},
		Cases:      []acceptanceEntry{{Identifier: "llama-cpp", Status: "pass", Attempts: 1}},
	})
	for _, expected := range []string{"**Result: PASS.**", "`strix-halo`", "| llama-cpp | pass |", "Fingerprint: `digest`", "`sha256:id`"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report lacks %q:\n%s", expected, report)
		}
	}
}

func TestValidateAcceptanceResultRejectsTamperedDefinitionAndCase(t *testing.T) {
	cases := []acceptanceCase{{id: "host-gpu", description: "probe"}}
	definition := acceptanceDefinition{Profile: "strix-halo"}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	result := acceptanceResult{Schema: acceptanceSchema, SuiteID: "20260820T010203Z-abcdef12", Fingerprint: fingerprint, Definition: definition, Status: "running", StartedAt: "now", Cases: []acceptanceEntry{{Identifier: "host-gpu", Description: "probe", Status: "pending"}}}
	if err := validateAcceptanceResult(result, cases); err != nil {
		t.Fatal(err)
	}
	result.Definition.Profile = "rdna4"
	if err := validateAcceptanceResult(result, cases); err == nil {
		t.Fatal("tampered definition was accepted")
	}
	result.Definition = definition
	result.Cases[0].Identifier = "../../escape"
	if err := validateAcceptanceResult(result, cases); err == nil {
		t.Fatal("tampered case was accepted")
	}
}

func TestAcceptanceEnvelopeRejectsInvalidRootAndControlText(t *testing.T) {
	definition := acceptanceDefinition{Profile: "strix-halo"}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	result := acceptanceResult{Schema: acceptanceSchema, SuiteID: "20260820T010203Z-abcdef12", Fingerprint: fingerprint, Definition: definition, Status: "running", StartedAt: "now", Cases: []acceptanceEntry{{Identifier: "host-gpu", Status: "pending"}}}
	if err := validateAcceptanceEnvelope(result); err != nil {
		t.Fatal(err)
	}
	result.Status = "invented"
	if err := validateAcceptanceEnvelope(result); err == nil {
		t.Fatal("invalid root status was accepted")
	}
	result.Status, result.Cases[0].Reason = "running", "terminal\x1bescape"
	if err := validateAcceptanceEnvelope(result); err == nil {
		t.Fatal("control characters were accepted")
	}
}

func TestArtifactsAvailableRequiresRegularFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.png")
	if err := os.WriteFile(path, []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := captureAcceptanceArtifact(path)
	if err != nil {
		t.Fatal(err)
	}
	if !artifactsAvailable([]acceptanceArtifact{artifact}) || artifactsAvailable([]acceptanceArtifact{{Path: filepath.Dir(path), SHA256: artifact.SHA256, Size: artifact.Size}}) {
		t.Fatal("artifact availability accepted an invalid path")
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if artifactsAvailable([]acceptanceArtifact{artifact}) {
		t.Fatal("changed artifact was accepted")
	}
}
