package cli

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/runtime"
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

func TestAcceptanceResumeAfterChildPublicationKeepsFirstAttempt(t *testing.T) {
	root := t.TempDir()
	launch := ""
	registered := false
	starts := 0
	runner := &commandRunner{run: func(command process.Command) process.Result {
		switch command.Args[0] {
		case "info":
			return process.Result{}
		case "create":
			registered = true
			launch = strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
			return process.Result{Stdout: []byte(fixtureContainerID)}
		case "container":
			if !registered {
				return process.Result{Status: 1}
			}
			if command.Args[1] == "inspect" {
				return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, runtime.LlamaBenchmarkContainer, fixtureContainerLabels("llama-cpp", "benchmark", launch))}
			}
			return process.Result{}
		case "start":
			starts++
			registered = false
			return process.Result{Stdout: []byte(`[{"n_prompt":32,"n_gen":0,"avg_ts":100},{"n_prompt":0,"n_gen":16,"avg_ts":10}]`)}
		default:
			t.Fatalf("unexpected fake subprocess %v", command.Args)
			return process.Result{}
		}
	}}
	app, _, _ := testApp(t, runner)
	managed := catalog.Catalog{
		Artifacts:    map[string]catalog.Artifact{"model": {ID: "model", Target: "llama-cpp", Destination: "fixture.gguf"}},
		LlamaPresets: map[string]catalog.LlamaPreset{"qwen-qwen3-0.6b-q8-0": {ID: "qwen-qwen3-0.6b-q8-0", Artifact: "model"}},
	}
	candidate := acceptanceCase{id: "llama-cpp"}
	definition := acceptanceDefinition{Cases: []string{candidate.id}}
	fingerprint, _ := jsonDigest(definition)
	result := acceptanceResult{Schema: acceptanceSchema, SuiteID: "20260820T010203Z-abcdef12", Definition: definition, Fingerprint: fingerprint, Status: "running", StartedAt: "now", Cases: []acceptanceEntry{{Identifier: candidate.id, Status: "pending"}}}
	path := filepath.Join(root, "suite.json")
	if err := benchmark.WriteNewCheckpoint(path, result); err != nil {
		t.Fatal(err)
	}
	var firstPath string
	err := runAcceptanceAttempt(path, &result, 0, func(attemptID string) ([]string, error) {
		var durable acceptanceResult
		if err := readAcceptanceResult(path, &durable); err != nil || durable.Cases[0].Attempts != 1 || durable.Cases[0].Status != "running" {
			t.Fatalf("workload began without durable identity: %#v %v", durable, err)
		}
		paths, err := app.runAcceptanceCase(managed, candidate, root, "cpu", "/dev/dri/renderD128", 8190, attemptID, "balanced", "default")
		if err != nil {
			t.Fatal(err)
		}
		firstPath = paths[0]
		if err := os.Rename(path, path+".saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return paths, nil
	})
	if err == nil {
		t.Fatal("expected parent checkpoint failure")
	}
	first, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	path += ".saved"
	if err := readAcceptanceResult(path, &result); err != nil {
		t.Fatal(err)
	}
	if err := validateAcceptanceResult(result, []acceptanceCase{candidate}); err != nil {
		t.Fatal(err)
	}
	if err := runAcceptanceAttempt(path, &result, 0, func(attemptID string) ([]string, error) {
		return app.runAcceptanceCase(managed, candidate, root, "cpu", "/dev/dri/renderD128", 8190, attemptID, "balanced", "default")
	}); err != nil {
		t.Fatal(err)
	}
	entry := result.Cases[0]
	if starts != 2 || entry.Status != "pass" || entry.Attempts != 2 || len(entry.PreviousAttempts) != 1 || entry.PreviousAttempts[0].Status != "running" || entry.Artifacts[0].Path == firstPath {
		t.Fatalf("unexpected retry: starts=%d entry=%#v", starts, entry)
	}
	current, err := os.ReadFile(firstPath)
	if err != nil || string(current) != string(first) {
		t.Fatalf("first attempt evidence changed: %v", err)
	}
}

func TestAcceptanceRejectsAttemptCountersAndOverflowBeforeWorkload(t *testing.T) {
	definition := acceptanceDefinition{}
	fingerprint, _ := jsonDigest(definition)
	for _, attempts := range []int{-1, 0, math.MaxInt} {
		result := acceptanceResult{Schema: acceptanceSchema, SuiteID: "20260820T010203Z-abcdef12", Definition: definition, Fingerprint: fingerprint, Status: "running", StartedAt: "now", Cases: []acceptanceEntry{{Identifier: "llama-cpp", Status: "running", Attempts: attempts}}}
		if err := validateAcceptanceEnvelope(result); err == nil {
			t.Fatalf("accepted invalid counter %d", attempts)
		}
		if attempts == math.MaxInt {
			if err := runAcceptanceAttempt(filepath.Join(t.TempDir(), "absent.json"), &result, 0, func(string) ([]string, error) {
				t.Fatal("overflow started workload")
				return nil, nil
			}); err == nil {
				t.Fatal("overflow was accepted")
			}
		}
	}
}

func TestAcceptanceRejectsOldSchemaAndMismatchedAttemptHistory(t *testing.T) {
	definition := acceptanceDefinition{}
	fingerprint, _ := jsonDigest(definition)
	result := acceptanceResult{Schema: acceptanceSchema, SuiteID: "20260820T010203Z-abcdef12", Definition: definition, Fingerprint: fingerprint, Status: "running", StartedAt: "now", Cases: []acceptanceEntry{{Identifier: "host-gpu", Status: "running", Attempts: 1}}}
	if err := validateAcceptanceEnvelope(result); err != nil {
		t.Fatal(err)
	}
	result.Schema = "paracetamol.hardware-acceptance.v2"
	if err := validateAcceptanceEnvelope(result); err == nil {
		t.Fatal("old checkpoint schema was accepted")
	}
	result.Schema, result.Cases[0].Attempts = acceptanceSchema, 2
	if err := validateAcceptanceEnvelope(result); err == nil {
		t.Fatal("missing previous attempt was accepted")
	}
}

func TestAcceptanceInitialAttemptCheckpointFailureStartsNoWorkload(t *testing.T) {
	result := acceptanceResult{SuiteID: "20260820T010203Z-abcdef12", Cases: []acceptanceEntry{{Identifier: "host-gpu", Status: "pending"}}}
	if err := runAcceptanceAttempt(t.TempDir(), &result, 0, func(string) ([]string, error) {
		t.Fatal("initial checkpoint failure started workload")
		return nil, nil
	}); err == nil {
		t.Fatal("initial checkpoint unexpectedly succeeded")
	}
}
