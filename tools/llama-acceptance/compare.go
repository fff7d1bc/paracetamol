package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"paracetamol/internal/benchmark"
	"paracetamol/tools/llama-acceptance/internal/coding"
	"paracetamol/tools/llama-acceptance/internal/contract"
)

type paths []string

func (p *paths) String() string { return strings.Join(*p, ", ") }
func (p *paths) Set(value string) error {
	if value == "" {
		return fmt.Errorf("empty run path")
	}
	*p = append(*p, value)
	return nil
}

func evidenceFiles(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular evidence path")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "summary.json" {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		n, copyErr := io.Copy(hash, io.LimitReader(file, 32*1024*1024+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || n > 32*1024*1024 {
			return fmt.Errorf("cannot hash bounded evidence file")
		}
		files[filepath.ToSlash(rel)] = hex.EncodeToString(hash.Sum(nil))
		return nil
	})
	return files, err
}

func loadSummary(dir string) (summary, error) {
	var result summary
	path := filepath.Join(dir, "summary.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4*1024*1024 {
		return result, fmt.Errorf("run requires a bounded regular summary.json")
	}
	if err := benchmark.ReadJSON(path, &result, true); err != nil {
		return result, err
	}
	if result.Schema != contract.Version || result.Model == "" || len(result.Cases) == 0 || len(result.Fixtures) == 0 || len(result.Evidence) == 0 || !fullID.MatchString(result.ToolSHA256) {
		return result, fmt.Errorf("run lacks required suite/fixture/tool/evidence identities")
	}
	// The initial manifest precedes inference. In particular the tolerated
	// regression cannot be relabelled by editing only the final summary after
	// seeing candidate timings. Evidence hashing alone does not compare these
	// two semantic records, and is not an authenticity/signature mechanism.
	planPath := filepath.Join(dir, "manifest.json")
	planInfo, err := os.Lstat(planPath)
	if err != nil || !planInfo.Mode().IsRegular() || planInfo.Size() > 4*1024*1024 {
		return result, fmt.Errorf("run requires its bounded regular initial manifest.json")
	}
	var planned summary
	if err := benchmark.ReadJSON(planPath, &planned, true); err != nil {
		return result, err
	}
	if !matchesPlan(result, planned) {
		return result, fmt.Errorf("final summary differs from the predeclared manifest inputs")
	}
	if result.Status != contract.Worst(result.Cases) && !(result.Status == contract.Blocked && !result.Complete) {
		return result, fmt.Errorf("summary disagrees with individual case outcomes")
	}
	seen := map[string]bool{}
	for _, item := range result.Cases {
		if item.Name == "" || seen[item.Name] {
			return result, fmt.Errorf("duplicate or empty case identity")
		}
		seen[item.Name] = true
	}
	files, err := evidenceFiles(dir)
	if err != nil || !equalJSON(files, result.Evidence) {
		return result, fmt.Errorf("run evidence is missing, changed or added since completion")
	}
	return result, nil
}

func matchesPlan(final, planned summary) bool {
	return !planned.Complete && planned.FinishedAt == "" && planned.Status == contract.Blocked && len(planned.Cases) == 0 && len(planned.Evidence) == 0 && planned.Error == "" && final.Schema == planned.Schema && final.Control == planned.Control && final.Suite == planned.Suite && final.Model == planned.Model && final.StartedAt == planned.StartedAt && final.ToolSHA256 == planned.ToolSHA256 && final.Instance == planned.Instance && final.Inventory == planned.Inventory && final.GraderImage == planned.GraderImage && final.PrefillTolerance == planned.PrefillTolerance && equalJSON(final.Fixtures, planned.Fixtures) && equalJSON(final.Backend, planned.Backend)
}

func runCompare(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("llama-acceptance compare", flag.ContinueOnError)
	set.SetOutput(stderr)
	var baseline, candidate paths
	set.Var(&baseline, "baseline", "completed baseline run directory; repeat for counterbalanced measurements")
	set.Var(&candidate, "candidate", "completed candidate run directory; repeat for counterbalanced measurements")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if set.NArg() != 0 || len(baseline) == 0 || len(candidate) == 0 {
		return fmt.Errorf("compare requires --baseline and --candidate run directories")
	}
	var old, next []summary
	seenDirectories := map[string]bool{}
	for _, group := range []struct {
		dirs paths
		dest *[]summary
	}{{baseline, &old}, {candidate, &next}} {
		for _, dir := range group.dirs {
			canonical, err := filepath.EvalSymlinks(dir)
			if err != nil {
				return fmt.Errorf("cannot resolve comparison directory: %w", err)
			}
			canonical, err = filepath.Abs(canonical)
			if err != nil {
				return err
			}
			if seenDirectories[canonical] {
				return fmt.Errorf("one saved run cannot count as multiple observations")
			}
			seenDirectories[canonical] = true
			run, err := loadSummary(dir)
			if err != nil {
				return fmt.Errorf("cannot compare %s: %w", dir, err)
			}
			*group.dest = append(*group.dest, run)
		}
	}
	status, messages := compareRuns(old, next)
	for _, message := range messages {
		fmt.Fprintln(stdout, message)
	}
	fmt.Fprintf(stdout, "%s  matched comparison (not universal model-quality acceptance)\n", status)
	if status != contract.Pass {
		return errIncomplete
	}
	return nil
}

func compareRuns(baseline, candidate []summary) (contract.Status, []string) {
	if len(baseline) == 0 || len(candidate) == 0 {
		return contract.Blocked, []string{"Missing comparison arm."}
	}
	anchor := baseline[0]
	anchorArguments, err := normalizedNativeArguments(anchor.Backend.Arguments)
	if err != nil {
		return contract.Blocked, []string{err.Error()}
	}
	result := contract.Pass
	var messages []string
	all := append(append([]summary{}, baseline...), candidate...)
	var windows []observation
	seenRequests := map[string]bool{}
	imageSources := map[string]sourceIdentity{}
	for index, run := range all {
		if run.Control != run.Backend.Control || run.Control != "" && run.Control != "mmq-sum" && run.Control != "qsa-host-span" {
			return contract.Blocked, []string{"Invalid diagnostic control identity."}
		}
		if run.Control != "" {
			result = contract.Review
			messages = append(messages, "Diagnostic negative-control arm only; it cannot qualify a normal source update.")
		}
		if !run.Complete || run.Status == contract.Blocked {
			return contract.Blocked, []string{"A required arm is incomplete or blocked."}
		}
		if err := validateObservation(run); err != nil {
			return contract.Blocked, []string{err.Error()}
		}
		arguments, err := normalizedNativeArguments(run.Backend.Arguments)
		if err != nil {
			return contract.Blocked, []string{err.Error()}
		}
		if run.Schema != anchor.Schema || run.Suite != anchor.Suite || run.Model != anchor.Model || run.ToolSHA256 != anchor.ToolSHA256 || run.GraderImage != anchor.GraderImage || run.PrefillTolerance != anchor.PrefillTolerance || run.Inventory != anchor.Inventory || !equalJSON(run.Fixtures, anchor.Fixtures) || !equalJSON(run.Backend.Policy, anchor.Backend.Policy) || !equalJSON(run.Backend.Sampling, anchor.Backend.Sampling) || !equalJSON(run.Backend.Environment, anchor.Backend.Environment) || !equalJSON(arguments, anchorArguments) || !equalJSON(run.Backend.Artifacts, anchor.Backend.Artifacts) || run.Backend.Template != anchor.Backend.Template {
			return contract.Blocked, []string{"Fixture, tool, model, sampling, inventory, runtime policy or grading identity differs. These are not matched arms."}
		}
		if run.Backend.BootSession != anchor.Backend.BootSession || run.Backend.Kernel != anchor.Backend.Kernel || run.Backend.Firmware != anchor.Backend.Firmware || run.Backend.CPUProfile != anchor.Backend.CPUProfile || run.Backend.Profile != anchor.Backend.Profile || !equalJSON(run.Backend.RenderNodes, anchor.Backend.RenderNodes) || run.Backend.MemoryGuard != anchor.Backend.MemoryGuard {
			return contract.Blocked, []string{"Host profile, selected render nodes, kernel, power or guard policy changed between arms."}
		}
		identity := sourceOf(run)
		if previous, exists := imageSources[canonicalID(run.Backend.ImageID)]; exists && previous != identity {
			return contract.Blocked, []string{"One immutable image has conflicting source or patchset identities."}
		}
		imageSources[canonicalID(run.Backend.ImageID)] = identity
		groupAnchor := baseline[0]
		candidateArm := index >= len(baseline)
		if candidateArm {
			groupAnchor = candidate[0]
		}
		if canonicalID(run.Backend.ImageID) != canonicalID(groupAnchor.Backend.ImageID) || identity != sourceOf(groupAnchor) {
			return contract.Blocked, []string{"An arm mixes different images, sources, patchsets or ROCm versions."}
		}
		// A report can belong to two case predicates within one observation, but
		// cannot count again in a copied/relabelled second observation.
		thisRun := map[string]bool{}
		for _, item := range run.Cases {
			for _, id := range item.RequestIDs {
				thisRun[id] = true
			}
		}
		for id := range thisRun {
			if seenRequests[id] {
				return contract.Blocked, []string{"A request observation was reused in multiple comparison arms."}
			}
			seenRequests[id] = true
		}
		start, _ := time.Parse(time.RFC3339Nano, run.StartedAt)
		end, _ := time.Parse(time.RFC3339Nano, run.FinishedAt)
		windows = append(windows, observation{run: run, candidate: candidateArm, start: start, end: end})
		if run.Status == contract.Fail {
			result = contract.Fail
		} else if run.Status == contract.Review && result == contract.Pass {
			result = contract.Review
		}
	}
	slices.SortFunc(windows, func(a, b observation) int { return a.start.Compare(b.start) })
	for i := 1; i < len(windows); i++ {
		if windows[i].start.Before(windows[i-1].end) {
			return contract.Blocked, []string{"Comparison observations overlap or reuse the same time window."}
		}
	}
	if canonicalID(baseline[0].Backend.ImageID) == canonicalID(candidate[0].Backend.ImageID) {
		messages = append(messages, "Same-image A/A diagnostic only; no source-update gain or acceptance is inferred.")
	}
	if result != contract.Pass {
		messages = append(messages, "Retained case failures/misses need review; later attempts do not replace them.")
	}
	names := []string{}
	if anchor.Suite == "prefill" || anchor.Suite == "all" {
		names = []string{"prefill-32k-measurement", "prefill-aged16k"}
		for _, run := range all {
			if !equalJSON(prefillInputs(run), prefillInputs(anchor)) {
				return contract.Blocked, append(messages, "Prefill or cache-conditioning request bytes differ between observations.")
			}
		}
		if len(baseline) != len(candidate) || len(windows)%4 != 0 || len(baseline) < 2 {
			return comparisonReview(result), append(messages, "Timing requires distinct observations in one or more complete ABBA or BAAB blocks.")
		}
		for i := 0; i < len(windows); i += 4 {
			if windows[i].candidate != windows[i+3].candidate || windows[i+1].candidate != windows[i+2].candidate || windows[i].candidate == windows[i+1].candidate {
				return comparisonReview(result), append(messages, "Timing order is not counterbalanced ABBA/BAAB; no paired acceptance claim.")
			}
		}
	}
	for _, name := range names {
		if len(baseline) < 2 || len(candidate) < 2 || anchor.PrefillTolerance < 0 || math.IsNaN(anchor.PrefillTolerance) || math.IsInf(anchor.PrefillTolerance, 0) {
			if result == contract.Pass {
				result = contract.Review
			}
			messages = append(messages, name+": requires at least two observations per arm and a tolerance declared before the candidate.")
			continue
		}
		anchorWork := caseByName(anchor, name).Measurements
		for _, run := range all {
			m := caseByName(run, name).Measurements
			for _, field := range []string{"prompt_tokens", "input_tokens", "cached_tokens"} {
				value, exists := anchorWork[field]
				other, present := m[field]
				if exists != present || exists && other != value {
					return contract.Blocked, append(messages, name+": paired prompt/cache work counts differ.")
				}
			}
		}
		rate := func(runs []summary) (float64, bool) {
			var tokens, milliseconds float64
			for _, run := range runs {
				found := false
				for _, item := range run.Cases {
					if item.Name != name {
						continue
					}
					t, m := item.Measurements["prompt_tokens"], item.Measurements["prompt_ms"]
					if found || item.Status != contract.Pass || t <= 0 || m <= 0 || math.IsNaN(t) || math.IsNaN(m) || math.IsInf(t, 0) || math.IsInf(m, 0) {
						return 0, false
					}
					found = true
					tokens, milliseconds = tokens+t, milliseconds+m
				}
				if !found {
					return 0, false
				}
				if !finitePositive(tokens) || !finitePositive(milliseconds) {
					return 0, false
				}
			}
			value := tokens / milliseconds * 1000
			return value, finitePositive(value)
		}
		oldRate, oldOK := rate(baseline)
		newRate, newOK := rate(candidate)
		if !oldOK || !newOK {
			return contract.Blocked, append(messages, "Missing or invalid paired native work/timing observations.")
		}
		percent := (oldRate/newRate - 1) * 100
		if math.IsNaN(percent) || math.IsInf(percent, 0) {
			return contract.Blocked, append(messages, "Paired native timing aggregation is nonfinite.")
		}
		messages = append(messages, fmt.Sprintf("%s: %.2f -> %.2f newly evaluated tokens/s, %.2f%% change in time/token", name, oldRate, newRate, percent))
		if percent > anchor.PrefillTolerance && result == contract.Pass {
			result = contract.Review
			messages = append(messages, "Prefill regression exceeds the predeclared tolerance. Investigate before accepting this update.")
		}
		spread := max(relativeSpread(baseline, name), relativeSpread(candidate, name))
		if math.IsNaN(spread) || math.IsInf(spread, 0) {
			return contract.Blocked, append(messages, "Timing variability calculation is nonfinite.")
		}
		if spread > anchor.PrefillTolerance || spread > 0 && math.Abs(percent) <= spread {
			result = comparisonReview(result)
			messages = append(messages, fmt.Sprintf("%s: within-image timing spread %.2f%% makes the pooled effect equivocal under the declared tolerance.", name, spread))
		}
		for i := 0; i < len(windows); i += 4 {
			first := pairEffect(windows[i], windows[i+1], name)
			second := pairEffect(windows[i+2], windows[i+3], name)
			if math.IsNaN(first) || math.IsInf(first, 0) || math.IsNaN(second) || math.IsInf(second, 0) {
				return contract.Blocked, append(messages, "Paired timing effect is nonfinite.")
			}
			if first*second < 0 {
				result = comparisonReview(result)
				messages = append(messages, name+": the two order directions disagree; retain the observations for review.")
			}
		}
	}
	return result, messages
}

type sourceIdentity struct{ Source, Tree, Patches, ROCm string }

func sourceOf(run summary) sourceIdentity {
	return sourceIdentity{run.Backend.Source, run.Backend.SourceTree, run.Backend.Patches, run.Backend.ROCm}
}

type observation struct {
	run        summary
	candidate  bool
	start, end time.Time
}

func comparisonReview(status contract.Status) contract.Status {
	if status == contract.Pass {
		return contract.Review
	}
	return status
}
func caseByName(run summary, name string) contract.Result {
	for _, item := range run.Cases {
		if item.Name == name {
			return item
		}
	}
	return contract.Result{}
}
func relativeSpread(runs []summary, name string) float64 {
	lo, hi := math.Inf(1), 0.0
	for _, run := range runs {
		m := caseByName(run, name).Measurements
		value := m["prompt_ms"] / m["prompt_tokens"]
		lo = min(lo, value)
		hi = max(hi, value)
	}
	return (hi/lo - 1) * 100
}
func pairEffect(a, b observation, name string) float64 {
	if a.candidate {
		a, b = b, a
	}
	old, next := caseByName(a.run, name).Measurements, caseByName(b.run, name).Measurements
	return ((next["prompt_ms"]/next["prompt_tokens"])/(old["prompt_ms"]/old["prompt_tokens"]) - 1) * 100
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// Child listener ports are ephemeral. All other arguments, including unknown
// knobs introduced by an upstream refresh, remain comparison inputs. Do not
// silently normalize checkpoint, precision, graph, host or loading controls.
func normalizedNativeArguments(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("native argument evidence is missing")
	}
	result := make([]string, 0, len(args))
	seenPort := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := ""
		isPort := arg == "--port" || strings.HasPrefix(arg, "--port=")
		if !isPort {
			result = append(result, arg)
			continue
		}
		if seenPort {
			return nil, fmt.Errorf("duplicate native listener option")
		}
		seenPort = true
		if arg == "--port" {
			i++
			if i == len(args) {
				return nil, fmt.Errorf("native port has no value")
			}
			value = args[i]
		} else {
			value = strings.TrimPrefix(arg, "--port=")
		}
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("native listener port is invalid")
		}
	}
	return result, nil
}

// Only these requests define timed work and its cache-conditioning procedure.
// Assistant-dependent coding repairs/history appends may legitimately differ;
// they are separately retained and never pooled as fixed-work measurements.
func prefillInputs(run summary) map[string]string {
	inputs := map[string]string{}
	for slot := 0; slot < 4; slot++ {
		name := fmt.Sprintf("prefill-slot-init-%d", slot)
		inputs[name] = run.Evidence[name+".request.json"]
	}
	for _, name := range []string{"prefill-32k-cold", "condition-alpha-74000", "condition-beta-74000", "condition-gamma-74000", "condition-delta-74000", "condition-epsilon-74000", "condition-epsilon-113600", "condition-zeta-16000"} {
		inputs[name] = run.Evidence[name+".request.json"]
	}
	return inputs
}

func validateObservation(run summary) error {
	if !fullID.MatchString(run.Backend.BootSession) {
		return fmt.Errorf("run lacks its private same-boot comparison identity")
	}
	if run.Schema != contract.Version || run.Error != "" || len(run.Fixtures) == 0 || !fullID.MatchString(run.ToolSHA256) || len(run.Backend.Policy) == 0 || len(run.Backend.Sampling) == 0 || len(run.Backend.Artifacts) == 0 || !fullID.MatchString(run.Backend.ImageID) || !fullID.MatchString(run.Backend.ContainerID) || !sourceID.MatchString(run.Backend.Source) || !sourceID.MatchString(run.Backend.SourceTree) || run.Backend.Patches == "" || run.Backend.ROCm == "" || !fullID.MatchString(run.Backend.Template) || run.Backend.Profile == "" || len(run.Backend.RenderNodes) == 0 || run.Backend.MemoryGuard <= 0 || !fullID.MatchString(run.Inventory) || !validHexIdentity(run.Instance, 32) || !fullID.MatchString(run.Evidence["manifest.json"]) {
		return fmt.Errorf("run lacks required model, source, sampler, hardware or evidence identities")
	}
	start, err := time.Parse(time.RFC3339Nano, run.StartedAt)
	if err != nil {
		return fmt.Errorf("invalid observation start time")
	}
	end, err := time.Parse(time.RFC3339Nano, run.FinishedAt)
	if err != nil || !end.After(start) {
		return fmt.Errorf("invalid observation interval")
	}
	if run.Status != contract.Worst(run.Cases) {
		return fmt.Errorf("run outcome disagrees with retained cases")
	}
	required, optional, err := caseCoverage(run)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	requestCount := 0
	for _, item := range run.Cases {
		if seen[item.Name] || !required[item.Name] && !optional[item.Name] {
			return fmt.Errorf("unknown or duplicate case coverage: %s", item.Name)
		}
		seen[item.Name] = true
		switch item.Status {
		case contract.Pass, contract.Fail, contract.Blocked, contract.Review:
		default:
			return fmt.Errorf("invalid case outcome")
		}
		caseRequests := map[string]bool{}
		for _, id := range item.RequestIDs {
			if len(id) != 50 || !strings.HasPrefix(id, "r"+run.Instance+"-") || !validHexIdentity(id[34:], 16) {
				return fmt.Errorf("request does not belong to observation gateway epoch")
			}
			if caseRequests[id] {
				return fmt.Errorf("case repeats one request as multiple observations")
			}
			caseRequests[id] = true
			requestCount++
		}
		if len(item.RequestIDs) < minimumRequestIDs(item.Name) {
			return fmt.Errorf("case lacks its correlated request coverage: %s", item.Name)
		}
		for field, value := range item.Measurements {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("nonfinite comparison measurement")
			}
			if strings.HasSuffix(field, "_tokens") && (value < 0 || value > 262144 || math.Trunc(value) != value) {
				return fmt.Errorf("invalid comparison token count")
			}
		}
	}
	for name := range required {
		if !seen[name] {
			return fmt.Errorf("required case missing: %s", name)
		}
	}
	if requestCount == 0 {
		return fmt.Errorf("observation has no correlated request identities")
	}
	if run.Suite == "prefill" || run.Suite == "all" {
		if math.IsNaN(run.PrefillTolerance) || math.IsInf(run.PrefillTolerance, 0) || run.PrefillTolerance < 0 || run.PrefillTolerance > 100 {
			return fmt.Errorf("missing finite predeclared prefill tolerance")
		}
		for name, hash := range prefillInputs(run) {
			if !fullID.MatchString(hash) {
				return fmt.Errorf("timed request or cache-conditioning evidence missing: %s", name)
			}
		}
		for _, name := range []string{"prefill-32k-measurement", "prefill-aged16k"} {
			m := caseByName(run, name).Measurements
			t, ms := m["prompt_tokens"], m["prompt_ms"]
			if t <= 0 || t > 262144 || math.Trunc(t) != t || ms <= 0 {
				return fmt.Errorf("missing valid fixed-work native timing: %s", name)
			}
		}
	}
	return nil
}

func validHexIdentity(value string, n int) bool {
	return len(value) == n && strings.Trim(value, "0123456789abcdef") == "" && strings.Trim(value, "0") != ""
}

// Successful complete runs have fixed mandatory coverage. The only optional
// branch is the predeclared coding repair/fresh-seed expansion, and its first
// miss must remain visible. Unknown additions do not silently widen the suite.
func caseCoverage(run summary) (map[string]bool, map[string]bool, error) {
	required, optional := map[string]bool{}, map[string]bool{}
	add := func(names ...string) {
		for _, name := range names {
			required[name] = true
		}
	}
	switch run.Suite {
	case "short", "lifecycle", "coding", "history", "prefill", "all":
	default:
		return nil, nil, fmt.Errorf("unknown comparison suite")
	}
	if run.Suite == "short" || run.Suite == "all" {
		add("off-replay-seed-42", "off-replay-seed-1234", "off-replay-seed-2026", "off-replay-seed-7", "off-replay-seed-99", "off-replay-temperature-zero", "effort-low", "effort-medium", "effort-xhigh", "omitted-effort", "nested-tool-stream-false", "nested-tool-stream-true")
	}
	if run.Suite == "coding" || run.Suite == "all" {
		if !fullID.MatchString(run.GraderImage) {
			return nil, nil, fmt.Errorf("coding comparison lacks immutable grader identity")
		}
		for _, task := range coding.Tasks() {
			for n := 0; n < 4; n++ {
				add(fmt.Sprintf("coding-selftest/%s/%d", task.ID, n))
			}
			add(task.ID + "-first")
			first := caseByName(run, task.ID+"-first")
			if first.Status == contract.Review {
				optional[task.ID+"-repair"] = true
				add(task.ID+"-fresh-1234", task.ID+"-fresh-2026")
			}
		}
	}
	if run.Suite == "history" || run.Suite == "all" {
		add("history-cold", "history-populate", "history-repeat", "history-edit", "history-append-1", "history-append-2")
	}
	if run.Suite == "lifecycle" || run.Suite == "all" {
		for slot := 0; slot < 4; slot++ {
			add(fmt.Sprintf("lifecycle-slot-init-%d", slot))
		}
		add("history-idle-populate", "history-idle-resident", "history-active-second-0", "history-active-second-1", "history-active-second-2", "history-idle-after-second", "history-idle-reactivate", "four-worker-isolation", "cancel-stream-false", "cancel-stream-true", "media-red", "media-same-red", "media-blue", "media-text")
	}
	if run.Suite == "prefill" || run.Suite == "all" {
		for slot := 0; slot < 4; slot++ {
			add(fmt.Sprintf("prefill-slot-init-%d", slot))
		}
		add("prefill-32k-cold", "prefill-32k-measurement", "conditioning-near-pool", "prefill-aged16k")
		for _, name := range []string{"condition-alpha-74000", "condition-beta-74000", "condition-gamma-74000", "condition-delta-74000", "condition-epsilon-74000", "condition-epsilon-113600"} {
			add(name, name+"-slots")
		}
	}
	return required, optional, nil
}

func minimumRequestIDs(name string) int {
	if strings.HasPrefix(name, "coding-selftest/") || strings.HasSuffix(name, "-slots") || name == "conditioning-near-pool" || name == "history-idle-resident" || name == "history-idle-after-second" {
		return 0
	}
	if strings.HasPrefix(name, "nested-tool-stream-") || strings.HasPrefix(name, "cancel-stream-") {
		return 2
	}
	if name == "four-worker-isolation" {
		return 4
	}
	return 1
}
