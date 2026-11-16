package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"paracetamol/tools/llama-acceptance/internal/coding"
	"paracetamol/tools/llama-acceptance/internal/contract"
)

func comparisonFixture(suite string, index int, candidate bool) summary {
	letter := "a"
	if candidate {
		letter = "b"
	}
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC).Add(time.Duration(index) * 20 * time.Second)
	s := summary{Schema: contract.Version, Suite: suite, Model: regularModel, StartedAt: start.Format(time.RFC3339Nano), FinishedAt: start.Add(10 * time.Second).Format(time.RFC3339Nano), Complete: true, Status: contract.Pass, Fixtures: map[string]string{"fixture": strings.Repeat("d", 64)}, ToolSHA256: strings.Repeat("c", 64), Instance: fmtHex(index+1, 32), Inventory: strings.Repeat("e", 64), PrefillTolerance: 5, Evidence: map[string]string{"manifest.json": fmtHex(index+1, 64)}}
	s.Backend = backendEvidence{ContainerID: fmtHex(index+100, 64), ImageID: "sha256:" + strings.Repeat(letter, 64), Source: strings.Repeat(letter, 40), SourceTree: strings.Repeat(letter, 40), Patches: "reviewed-patches", ROCm: "10.1", Policy: map[string]string{"ctx-size": "262144"}, Sampling: map[string]any{"thinking": map[string]any{"temperature": 1.0}, "non_thinking": map[string]any{"temperature": 0.7}}, Artifacts: map[string]string{"target": strings.Repeat("a", 64)}, Template: strings.Repeat("f", 64), Kernel: "test-kernel", Firmware: "performance", CPUProfile: "performance", Profile: "strix-halo", RenderNodes: []string{"/dev/dri/renderD128"}, MemoryGuard: 8 << 30, Arguments: []string{"llama-server", "--port", fmt.Sprint(18000 + index), "--ctx-checkpoints", "4"}, Environment: map[string]string{"QSA": "1"}}
	if suite == "coding" || suite == "all" {
		s.GraderImage = "sha256:" + strings.Repeat("9", 64)
	}
	s.Backend.BootSession = strings.Repeat("7", 64)
	s.Backend.GatewayInstance = s.Instance
	required, _, _ := caseCoverage(s)
	var names []string
	for name := range required {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		var ids []string
		for j := 0; j < minimumRequestIDs(name); j++ {
			ids = append(ids, "r"+s.Instance+"-"+fmtHex(i*8+j+1, 16))
		}
		s.Cases = append(s.Cases, contract.Result{Name: name, Status: contract.Pass, RequestIDs: ids})
	}
	if suite == "prefill" || suite == "all" {
		for name := range prefillInputs(s) {
			s.Evidence[name+".request.json"] = strings.Repeat("1", 64)
		}
		for i := range s.Cases {
			switch s.Cases[i].Name {
			case "prefill-32k-measurement":
				s.Cases[i].Measurements = map[string]float64{"prompt_tokens": 32768, "prompt_ms": 50000}
			case "prefill-aged16k":
				s.Cases[i].Measurements = map[string]float64{"prompt_tokens": 16045, "prompt_ms": 20000, "input_tokens": 16045, "cached_tokens": 0}
			}
			if candidate && s.Cases[i].Measurements != nil {
				s.Cases[i].Measurements["prompt_ms"] *= 0.9
			}
		}
	}
	return s
}

func fmtHex(n, width int) string {
	const digits = "0123456789abcdef"
	b := make([]byte, width)
	for i := len(b) - 1; i >= 0; i-- {
		b[i] = digits[n%16]
		n /= 16
	}
	return string(b)
}
func comparisonArms(suite string) ([]summary, []summary) {
	return []summary{comparisonFixture(suite, 0, false), comparisonFixture(suite, 3, false)}, []summary{comparisonFixture(suite, 1, true), comparisonFixture(suite, 2, true)}
}
func changeCase(s *summary, name string, fn func(*contract.Result)) {
	for i := range s.Cases {
		if s.Cases[i].Name == name {
			fn(&s.Cases[i])
			return
		}
	}
	panic("unknown test case " + name)
}

func TestComparisonMatchedCoverageAndCounterbalance(t *testing.T) {
	for _, suite := range []string{"short", "lifecycle", "coding", "history", "prefill", "all"} {
		t.Run(suite, func(t *testing.T) {
			old, next := comparisonArms(suite)
			status, messages := compareRuns(old, next)
			if status != contract.Pass {
				t.Fatalf("status=%s messages=%v", status, messages)
			}
		})
	}
}

func TestLifecycleCoverageIsIndependentAndStillRequiredByAll(t *testing.T) {
	history, _, err := caseCoverage(summary{Suite: "history"})
	if err != nil || history["cancel-stream-false"] || !history["history-append-2"] {
		t.Fatalf("history coverage is not limited to long-context cases: %v %v", history, err)
	}
	lifecycle, _, err := caseCoverage(summary{Suite: "lifecycle"})
	if err != nil || lifecycle["history-cold"] || !lifecycle["lifecycle-slot-init-0"] || !lifecycle["lifecycle-slot-init-3"] || !lifecycle["cancel-stream-false"] || !lifecycle["cancel-stream-true"] || !lifecycle["media-text"] || !lifecycle["four-worker-isolation"] {
		t.Fatalf("standalone lifecycle coverage incomplete: %v %v", lifecycle, err)
	}
	run := comparisonFixture("all", 0, false)
	for i, item := range run.Cases {
		if item.Name == "cancel-stream-true" {
			run.Cases = append(run.Cases[:i:i], run.Cases[i+1:]...)
			break
		}
	}
	if err := validateObservation(run); err == nil {
		t.Fatal("all accepted without independent lifecycle coverage")
	}
}

func TestInitializationCoverageAndPrefillWorkCannotBeOmitted(t *testing.T) {
	old, next := comparisonArms("lifecycle")
	for i, item := range next[0].Cases {
		if item.Name == "lifecycle-slot-init-1" {
			next[0].Cases = append(next[0].Cases[:i:i], next[0].Cases[i+1:]...)
			break
		}
	}
	if status, _ := compareRuns(old, next); status != contract.Blocked {
		t.Fatal("lifecycle accepted without observed initialization of every slot")
	}
	old, next = comparisonArms("prefill")
	next[0].Evidence["prefill-slot-init-2.request.json"] = strings.Repeat("9", 64)
	if status, _ := compareRuns(old, next); status != contract.Blocked {
		t.Fatal("prefill accepted unmatched initialization work")
	}
}

func TestDiagnosticControlNeverQualifiesNormalUpdate(t *testing.T) {
	old, next := comparisonArms("short")
	for i := range next {
		next[i].Control, next[i].Backend.Control = "mmq-sum", "mmq-sum"
	}
	status, messages := compareRuns(old, next)
	if status != contract.Review || !strings.Contains(strings.Join(messages, " "), "Diagnostic") {
		t.Fatalf("diagnostic image qualified normal update: %s %v", status, messages)
	}
	next[0].Backend.Control = ""
	if status, _ := compareRuns(old, next); status != contract.Blocked {
		t.Fatal("control label disagreement accepted")
	}
}

func TestDiagnosticTraceNeverQualifiesNormalUpdateOrHidesFailure(t *testing.T) {
	old, next := comparisonArms("short")
	for i := range next {
		next[i].Backend.Trace = "qwen4exp.v1"
		next[i].Cases = append(next[i].Cases, contract.Result{Name: "diagnostic-trace-only", Status: contract.Review})
		next[i].Status = contract.Review
	}
	if status, messages := compareRuns(old, next); status != contract.Review || !strings.Contains(strings.Join(messages, " "), "Diagnostic trace") {
		t.Fatalf("traced image qualified normal update: status=%s messages=%v", status, messages)
	}
	next[0].Cases[0].Status, next[0].Status = contract.Fail, contract.Fail
	if status, _ := compareRuns(old, next); status != contract.Fail {
		t.Fatalf("later diagnostic arm hid an earlier failure: %s", status)
	}
	next[0].Backend.Trace = "unknown"
	if status, _ := compareRuns(old, next); status != contract.Blocked {
		t.Fatal("unknown trace protocol accepted")
	}
}

func TestComparisonRequiresSamePrivateBoot(t *testing.T) {
	old, next := comparisonArms("short")
	next[0].Backend.BootSession = strings.Repeat("8", 64)
	if status, _ := compareRuns(old, next); status != contract.Blocked {
		t.Fatal("different physical boot session accepted as matched")
	}
}

func TestComparisonRequiresGatewayBackendBinding(t *testing.T) {
	for _, instance := range []string{"", strings.Repeat("0", 32), strings.Repeat("f", 32)} {
		old, next := comparisonArms("short")
		next[0].Backend.GatewayInstance = instance
		if status, _ := compareRuns(old, next); status != contract.Blocked {
			t.Fatalf("unbound backend accepted: %q status=%s", instance, status)
		}
	}
}

func TestComparisonRejectsUnmatchedOrReusedEvidence(t *testing.T) {
	for _, fault := range []string{"missing-case", "extra-case", "duplicate-case", "case-coverage-both-missing", "same-observation", "reused-request", "mixed-image", "mixed-source", "mixed-tree", "mixed-patches", "mixed-rocm", "samplers", "inventory", "profile", "render", "guard", "artifacts", "template", "tool", "fixtures", "kernel", "firmware", "epp", "request-bytes", "conditioning-bytes", "missing-request", "changed-work", "changed-cached", "missing-timing", "nonfinite", "fractional-work", "timestamp", "overlap", "wrong-epoch", "missing-source", "missing-samplers", "error-retained", "status-drift", "overflow-time", "underflow-time"} {
		t.Run(fault, func(t *testing.T) {
			old, next := comparisonArms("prefill")
			switch fault {
			case "missing-case":
				next[0].Cases = next[0].Cases[1:]
			case "extra-case":
				next[0].Cases = append(next[0].Cases, contract.Result{Name: "unreviewed", Status: contract.Pass})
			case "duplicate-case":
				next[0].Cases = append(next[0].Cases, next[0].Cases[0])
			case "case-coverage-both-missing":
				for i := range old {
					old[i].Cases = old[i].Cases[1:]
				}
				for i := range next {
					next[i].Cases = next[i].Cases[1:]
				}
			case "same-observation":
				old[1] = old[0]
			case "reused-request":
				next[0].Instance = old[0].Instance
				next[0].Cases = append([]contract.Result{}, old[0].Cases...)
			case "mixed-image":
				next[1].Backend.ImageID = "sha256:" + strings.Repeat("9", 64)
			case "mixed-source":
				next[1].Backend.Source = strings.Repeat("9", 40)
			case "mixed-tree":
				next[1].Backend.SourceTree = strings.Repeat("9", 40)
			case "mixed-patches":
				next[1].Backend.Patches = "other"
			case "mixed-rocm":
				next[1].Backend.ROCm = "other"
			case "samplers":
				next[0].Backend.Sampling = map[string]any{"temperature": 0.5}
			case "inventory":
				next[0].Inventory = strings.Repeat("9", 64)
			case "profile":
				next[0].Backend.Profile = "other"
			case "render":
				next[0].Backend.RenderNodes = []string{"/dev/dri/renderD129"}
			case "guard":
				next[0].Backend.MemoryGuard = 1
			case "artifacts":
				next[0].Backend.Artifacts = map[string]string{"target": strings.Repeat("9", 64)}
			case "template":
				next[0].Backend.Template = strings.Repeat("9", 64)
			case "tool":
				next[0].ToolSHA256 = strings.Repeat("9", 64)
			case "fixtures":
				next[0].Fixtures = map[string]string{"fixture": strings.Repeat("9", 64)}
			case "kernel":
				next[0].Backend.Kernel = "other"
			case "firmware":
				next[0].Backend.Firmware = "other"
			case "epp":
				next[0].Backend.CPUProfile = "other"
			case "request-bytes":
				next[0].Evidence["prefill-32k-cold.request.json"] = strings.Repeat("9", 64)
			case "conditioning-bytes":
				next[0].Evidence["condition-epsilon-113600.request.json"] = strings.Repeat("9", 64)
			case "missing-request":
				delete(next[0].Evidence, "condition-epsilon-113600.request.json")
			case "changed-work":
				changeCase(&next[0], "prefill-aged16k", func(r *contract.Result) { r.Measurements["prompt_tokens"]++ })
			case "changed-cached":
				changeCase(&next[0], "prefill-aged16k", func(r *contract.Result) { r.Measurements["cached_tokens"]++ })
			case "missing-timing":
				changeCase(&next[0], "prefill-aged16k", func(r *contract.Result) { delete(r.Measurements, "prompt_ms") })
			case "nonfinite":
				changeCase(&next[0], "prefill-aged16k", func(r *contract.Result) { r.Measurements["prompt_ms"] = math.NaN() })
			case "fractional-work":
				changeCase(&next[0], "prefill-aged16k", func(r *contract.Result) { r.Measurements["prompt_tokens"] += 0.1 })
			case "timestamp":
				next[0].StartedAt = "invalid"
			case "overlap":
				next[0].StartedAt = old[0].StartedAt
			case "wrong-epoch":
				next[0].Cases[0].RequestIDs = []string{"r" + strings.Repeat("1", 32) + "-" + strings.Repeat("2", 16)}
			case "missing-source":
				next[0].Backend.Source = ""
			case "missing-samplers":
				next[0].Backend.Sampling = nil
			case "error-retained":
				next[0].Error = "metadata failed"
			case "status-drift":
				next[0].Status = contract.Review
			case "overflow-time":
				for i := range old {
					changeCase(&old[i], "prefill-32k-measurement", func(r *contract.Result) { r.Measurements["prompt_ms"] = math.MaxFloat64 })
				}
			case "underflow-time":
				for i := range old {
					changeCase(&old[i], "prefill-32k-measurement", func(r *contract.Result) { r.Measurements["prompt_ms"] = math.SmallestNonzeroFloat64 })
				}
			}
			status, messages := compareRuns(old, next)
			if status != contract.Blocked {
				t.Fatalf("fault=%s status=%s messages=%v", fault, status, messages)
			}
		})
	}
}

func TestComparisonRequiresCounterbalancedIndependentTiming(t *testing.T) {
	old, next := comparisonArms("prefill")
	if status, _ := compareRuns(old[:1], next[:1]); status != contract.Review {
		t.Fatal("one observation accepted as timing gate")
	}
	old, next = comparisonArms("prefill")
	old[1].StartedAt = time.Date(2026, 11, 15, 12, 0, 20, 0, time.UTC).Format(time.RFC3339Nano)
	old[1].FinishedAt = time.Date(2026, 11, 15, 12, 0, 30, 0, time.UTC).Format(time.RFC3339Nano)
	next[0].StartedAt = time.Date(2026, 11, 15, 12, 0, 40, 0, time.UTC).Format(time.RFC3339Nano)
	next[0].FinishedAt = time.Date(2026, 11, 15, 12, 0, 50, 0, time.UTC).Format(time.RFC3339Nano)
	next[1].StartedAt = time.Date(2026, 11, 15, 12, 1, 0, 0, time.UTC).Format(time.RFC3339Nano)
	next[1].FinishedAt = time.Date(2026, 11, 15, 12, 1, 10, 0, time.UTC).Format(time.RFC3339Nano)
	if status, _ := compareRuns(old, next); status != contract.Review {
		t.Fatal("AABB accepted as counterbalanced")
	}
}

func TestComparisonRetainsRegressionVariabilityAndMisses(t *testing.T) {
	for _, fault := range []string{"regression", "variability", "opposite-direction", "model-miss", "first-miss-followup-pass", "tolerance-drift", "tolerance-nan"} {
		t.Run(fault, func(t *testing.T) {
			suite := "prefill"
			if fault == "first-miss-followup-pass" {
				suite = "coding"
			}
			old, next := comparisonArms(suite)
			want := contract.Review
			switch fault {
			case "regression":
				for i := range next {
					changeCase(&next[i], "prefill-32k-measurement", func(r *contract.Result) { r.Measurements["prompt_ms"] = 60000 })
				}
			case "variability":
				changeCase(&next[1], "prefill-32k-measurement", func(r *contract.Result) { r.Measurements["prompt_ms"] = 30000 })
			case "opposite-direction":
				changeCase(&next[1], "prefill-32k-measurement", func(r *contract.Result) { r.Measurements["prompt_ms"] = 51000 })
			case "model-miss":
				next[0].Cases[0].Status = contract.Review
				next[0].Status = contract.Review
			case "first-miss-followup-pass":
				task := coding.Tasks()[0]
				changeCase(&next[0], task.ID+"-first", func(r *contract.Result) { r.Status = contract.Review })
				for i, name := range []string{task.ID + "-repair", task.ID + "-fresh-1234", task.ID + "-fresh-2026"} {
					next[0].Cases = append(next[0].Cases, contract.Result{Name: name, Status: contract.Pass, RequestIDs: []string{"r" + next[0].Instance + "-" + fmtHex(10000+i, 16)}})
				}
				next[0].Status = contract.Review
			case "tolerance-drift":
				next[0].PrefillTolerance = 20
				want = contract.Blocked
			case "tolerance-nan":
				for i := range old {
					old[i].PrefillTolerance = math.NaN()
				}
				for i := range next {
					next[i].PrefillTolerance = math.NaN()
				}
				want = contract.Blocked
			}
			if status, messages := compareRuns(old, next); status != want {
				t.Fatalf("want=%s got=%s messages=%v", want, status, messages)
			}
		})
	}
}

func TestComparisonCodingExpansionRequiresOriginalMiss(t *testing.T) {
	old, next := comparisonArms("coding")
	next[0].Cases = append(next[0].Cases, contract.Result{Name: coding.Tasks()[0].ID + "-repair", Status: contract.Pass})
	if status, _ := compareRuns(old, next); status != contract.Blocked {
		t.Fatal("repair after original passing attempt accepted")
	}
	old, next = comparisonArms("coding")
	changeCase(&next[0], coding.Tasks()[0].ID+"-first", func(r *contract.Result) { r.Status = contract.Review })
	next[0].Status = contract.Review
	if status, _ := compareRuns(old, next); status != contract.Blocked {
		t.Fatal("dropped predeclared fresh attempts accepted")
	}
}

func TestComparisonSameImageIsExplicitDiagnostic(t *testing.T) {
	old, next := comparisonArms("prefill")
	for i := range next {
		next[i].Backend.ImageID = old[0].Backend.ImageID
		next[i].Backend.Source = old[0].Backend.Source
		next[i].Backend.SourceTree = old[0].Backend.SourceTree
	}
	status, messages := compareRuns(old, next)
	if status != contract.Pass || !strings.Contains(strings.Join(messages, "\n"), "A/A diagnostic only") {
		t.Fatalf("status=%s messages=%v", status, messages)
	}
}

func TestComparisonPreservesUnknownNativeControlsAndEnvironment(t *testing.T) {
	for _, fault := range []string{"unknown-argument", "checkpoint", "environment", "tool-call-observation"} {
		t.Run(fault, func(t *testing.T) {
			old, next := comparisonArms("short")
			switch fault {
			case "unknown-argument":
				next[0].Backend.Arguments = append(next[0].Backend.Arguments, "--new-experimental-control", "1")
			case "checkpoint":
				next[0].Backend.Arguments[len(next[0].Backend.Arguments)-1] = "0"
			case "environment":
				next[0].Backend.Environment = map[string]string{"QSA": "0"}
			case "tool-call-observation":
				changeCase(&next[0], "nested-tool-stream-false", func(r *contract.Result) { r.RequestIDs = []string{r.RequestIDs[0], r.RequestIDs[0]} })
			}
			if status, _ := compareRuns(old, next); status != contract.Blocked {
				t.Fatalf("unmatched %s accepted", fault)
			}
		})
	}
}

func TestNormalizeNativeListenerOnly(t *testing.T) {
	a, err := normalizedNativeArguments([]string{"server", "--port", "18556", "--ctx-checkpoints", "4"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := normalizedNativeArguments([]string{"server", "--port=18557", "--ctx-checkpoints", "4"})
	if err != nil || !equalJSON(a, b) {
		t.Fatalf("listener normalization: %v", err)
	}
	for _, args := range [][]string{nil, {"--port"}, {"--port=invalid"}, {"--port", "1", "--port=2"}} {
		if _, err := normalizedNativeArguments(args); err == nil {
			t.Fatalf("ambiguous native listener accepted: %v", args)
		}
	}
}

func writeComparisonRun(t *testing.T, dir string, s summary) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "artifact.json"), []byte(`{"synthetic":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	planned := s
	planned.FinishedAt = ""
	planned.Complete = false
	planned.Status = contract.Blocked
	planned.Cases = nil
	planned.Evidence = nil
	plan, err := json.Marshal(planned)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), plan, 0600); err != nil {
		t.Fatal(err)
	}
	evidence, err := evidenceFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Evidence = evidence
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCompareRejectsDuplicatePathAndAliases(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run")
	writeComparisonRun(t, dir, comparisonFixture("short", 0, false))
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	for _, second := range []string{dir, alias} {
		var output bytes.Buffer
		if err := runCompare([]string{"--baseline", dir, "--baseline", second, "--candidate", dir}, &output, &output); err == nil || !strings.Contains(err.Error(), "multiple observations") {
			t.Fatalf("duplicate path accepted: %v", err)
		}
	}
}

func TestComparisonEvidenceTamperAndReadOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	writeComparisonRun(t, dir, comparisonFixture("short", 0, false))
	before, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadSummary(dir); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "summary.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("read-only load rewrote summary")
	}
	if err := os.WriteFile(filepath.Join(dir, "artifact.json"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSummary(dir); err == nil {
		t.Fatal("changed evidence accepted")
	}
}

func TestFinalSummaryCannotRelabelPredeclaredInputs(t *testing.T) {
	for _, field := range []string{"tolerance", "suite", "source", "started", "sampling"} {
		t.Run(field, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "run")
			s := comparisonFixture("short", 0, false)
			writeComparisonRun(t, dir, s)
			raw, err := os.ReadFile(filepath.Join(dir, "summary.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "tolerance":
				s.PrefillTolerance = 100
			case "suite":
				s.Suite = "all"
			case "source":
				s.Backend.Source = strings.Repeat("9", 40)
			case "started":
				s.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
			case "sampling":
				s.Backend.Sampling = map[string]any{"temperature": 0.5}
			}
			raw, _ = json.Marshal(s)
			if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadSummary(dir); err == nil {
				t.Fatal("post-hoc identity change accepted")
			}
		})
	}
}
