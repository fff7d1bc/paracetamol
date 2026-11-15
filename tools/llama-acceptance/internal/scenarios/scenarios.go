// Package scenarios contains versioned public synthetic model acceptance cases.
// It owns request definitions and independent answer predicates, not transport,
// backend lifecycle, sampler policy or execution of generated programs.
package scenarios

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"paracetamol/tools/llama-acceptance/internal/contract"
)

//go:embed fixtures/*.json PROVENANCE.md scenarios.go advanced.go
var fixtures embed.FS

const generatorVersion = "public-distributed-records.v1; row=record-%06d value=VALUE_%09d; value=100000003+index*7919; keys=0,n/4,n/2,3*n/4,n-1; edited-last=VALUE_998877665; append-1-keys=1,n/3,2*n/3,n-2; append-2-keys=2,n/5,4*n/5,n-3; append-marker=APPEND_%d_314159265; append-oracle=ordered-original-record-values,turn-marker; history-input=247000..258048; history-initial-input-max=248024; history-output-reserve=4096"

func FixtureManifest() map[string]string {
	manifest := map[string]string{}
	// Source hashes also identify the policies, calibration and predicates around
	// generated fixtures; changing code cannot silently retain the same manifest.
	for _, name := range []string{"fixtures/off-replay.json", "PROVENANCE.md", "scenarios.go", "advanced.go"} {
		data, _ := fixtures.ReadFile(name)
		sum := sha256.Sum256(data)
		manifest[name] = hex.EncodeToString(sum[:])
	}
	for _, n := range []int{128, 2048, 10000} {
		text, want := table(n, false)
		sum := sha256.Sum256([]byte(text + "\nORACLE=" + want))
		manifest[fmt.Sprintf("distributed-records-%d", n)] = hex.EncodeToString(sum[:])
		var appends strings.Builder
		for turn := 1; turn <= 2; turn++ {
			prompt, expected := historyAppend(n, turn)
			fmt.Fprintf(&appends, "%s\nORACLE=%s\n", prompt, expected)
		}
		sum = sha256.Sum256([]byte(appends.String()))
		manifest[fmt.Sprintf("history-append-queries-%d", n)] = hex.EncodeToString(sum[:])
	}
	for _, red := range []bool{false, true} {
		sum := sha256.Sum256([]byte(imageURL(red)))
		manifest[fmt.Sprintf("solid-media-red-%t", red)] = hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256([]byte(generatorVersion))
	manifest["distributed-record-generator.v1"] = hex.EncodeToString(sum[:])
	return manifest
}

func ValidateFixtures() error {
	manifest := FixtureManifest()
	for name, want := range map[string]string{
		"fixtures/off-replay.json":        "e0b925e0158984c68aa50205816c104c8ccaed5be91f7eef83d4856c80707f25",
		"distributed-record-generator.v1": "3b1fb6f2ff72e6bac10284efe9a08e8b947c32b828379924b067d248cad29a81",
		"distributed-records-128":         "724627f64d24ee6769b010e194fa4b1594064ca2e91341499735d37b752cd7e8",
		"distributed-records-2048":        "dd665852b908b89167033335c079d1ca7450e2544dd42fc338d582de5d9479f9",
		"distributed-records-10000":       "31e60f445691c36f5d088a4d1f9fb2df937bd398848c679211f8c295f496b1c2",
		"history-append-queries-128":      "841e683eee4e7bf587e5bbddcb802ea2fc5a94350c31a67766e48a445da4cee3",
		"history-append-queries-2048":     "b397926b6d472a3757cd1237bdfdc66723bba06f5155400cbfbc7ec596276349",
		"history-append-queries-10000":    "64a3a44ee6aad050aee38486198379777f2605d69f54f28c8eca5c04996312c5",
		"solid-media-red-false":           "68b752d6b19b2824ff729dc29d59054e3da0721c7b2e5980c63217ac4abe762e",
		"solid-media-red-true":            "1734858fdd52aaecacf3a56421ae6daf12fe559eee7970bbcf66e65320079561",
	} {
		if manifest[name] != want {
			return fmt.Errorf("frozen synthetic fixture %s changed; review a new fixture identity", name)
		}
	}
	r, err := replay()
	if err != nil {
		return err
	}
	if r["reasoning_effort"] != "none" || r["cache_prompt"] != false || r["max_tokens"] != float64(4096) {
		return fmt.Errorf("off replay policy changed")
	}
	if err := exact(contract.Response{Text: "ALDER_937261\n", Finish: "stop"}, "ALDER_937261"); err != nil {
		return err
	}
	for _, bad := range []string{"ALDER_937261\n</think>\nALDER_937261", "ALDER_937261 explanation", "WRONG"} {
		if exact(contract.Response{Text: bad, Finish: "stop"}, "ALDER_937261") == nil {
			return fmt.Errorf("broken replay oracle")
		}
	}
	return nil
}

func replay() (map[string]any, error) {
	data, err := fixtures.ReadFile("fixtures/off-replay.json")
	if err != nil {
		return nil, err
	}
	var request map[string]any
	err = json.Unmarshal(data, &request)
	return request, err
}

func exact(response contract.Response, expected string) error {
	if response.Finish != "stop" {
		return fmt.Errorf("expected natural stop, got %q", response.Finish)
	}
	if len(response.Tools) != 0 || strings.TrimSpace(response.Text) != expected {
		return fmt.Errorf("exact answer oracle rejected response")
	}
	return nil
}

func outcome(name string, response contract.Response, err error) contract.Result {
	r := contract.Result{Name: name, Status: contract.Pass}
	if response.RequestID != "" {
		r.RequestIDs = []string{response.RequestID}
	}
	if err != nil {
		r.Status, r.Detail = contract.Fail, err.Error()
	}
	return r
}

func blocked(name, detail string) contract.Result {
	return contract.Result{Name: name, Status: contract.Blocked, Detail: detail}
}

func request(opts contract.Options, messages []any, effort string, cache, stream bool) map[string]any {
	return map[string]any{"model": opts.Model, "messages": messages, "reasoning_effort": effort, "max_tokens": 4096, "seed": 42, "cache_prompt": cache, "stream": stream}
}

func message(role, text string) map[string]any { return map[string]any{"role": role, "content": text} }

// Short keeps the historically discriminating replay before more expensive work.
func Short(ctx context.Context, opts contract.Options, call contract.Call) []contract.Result {
	var results []contract.Result
	for _, seed := range []int{42, 1234, 2026, 7, 99, 0} {
		r, err := replay()
		name := fmt.Sprintf("off-replay-seed-%d", seed)
		if err != nil {
			results = append(results, outcome(name, contract.Response{}, err))
			continue
		}
		r["model"] = opts.Model
		r["seed"] = seed
		if seed == 0 {
			name = "off-replay-temperature-zero"
			r["seed"] = 42
			r["temperature"] = 0
		}
		response, err := call(ctx, name, r)
		if err == nil {
			err = exact(response, "ALDER_937261")
		}
		if err == nil && strings.TrimSpace(response.Reasoning) != "" {
			err = fmt.Errorf("off mode returned reasoning")
		}
		results = append(results, outcome(name, response, err))
	}
	for _, effort := range []string{"low", "medium", "xhigh"} {
		name := "effort-" + effort
		r := request(opts, []any{message("developer", "Reply with exactly the requested marker and no explanation."), message("user", "Return EFFORT_271828.")}, effort, false, false)
		response, err := call(ctx, name, r)
		if err == nil {
			err = exact(response, "EFFORT_271828")
		}
		results = append(results, outcome(name, response, err))
	}
	// Raw API omission is distinct from explicit xhigh client preferences. The
	// transport verifies gateway default provenance without sending samplers.
	omitted := request(opts, []any{message("user", "Reply exactly DEFAULT_161803.")}, "medium", false, false)
	delete(omitted, "reasoning_effort")
	defaultResponse, defaultErr := call(ctx, "omitted-effort", omitted)
	if defaultErr == nil {
		defaultErr = exact(defaultResponse, "DEFAULT_161803")
	}
	results = append(results, outcome("omitted-effort", defaultResponse, defaultErr))
	for _, stream := range []bool{false, true} {
		results = append(results, toolRoundTrip(ctx, opts, call, stream))
	}
	return results
}

func toolRoundTrip(ctx context.Context, opts contract.Options, call contract.Call, stream bool) contract.Result {
	name := fmt.Sprintf("nested-tool-stream-%t", stream)
	messages := []any{message("developer", "Use lookup_record to retrieve the requested key. After receiving the tool result, return only its exact value."), message("user", "Look up ALDER using query.key.")}
	r := request(opts, messages, "xhigh", false, stream)
	base, _ := replay()
	r["tools"] = base["tools"]
	r["tool_choice"] = "required"
	first, err := call(ctx, name+"-call", r)
	if err != nil {
		return outcome(name, first, err)
	}
	if first.Finish != "tool_calls" || len(first.Tools) != 1 || first.Message == nil {
		return outcome(name, first, fmt.Errorf("expected one complete structured tool-call assistant"))
	}
	t := first.Tools[0]
	query, ok := t.Arguments["query"].(map[string]any)
	if !ok || len(t.Arguments) != 1 || len(query) != 1 || query["key"] != "ALDER" || t.Name != "lookup_record" || t.ID == "" {
		return outcome(name, first, fmt.Errorf("nested tool argument oracle rejected response"))
	}
	// Keep the entire assistant object, including reasoning and tool serialization.
	messages = append(messages, first.Message, map[string]any{"role": "tool", "tool_call_id": t.ID, "content": "ALDER_937261"})
	r = request(opts, messages, "xhigh", true, stream)
	r["tools"] = base["tools"]
	r["tool_choice"] = "none"
	second, err := call(ctx, name+"-result", r)
	if err == nil {
		err = exact(second, "ALDER_937261")
	}
	result := outcome(name, second, err)
	result.RequestIDs = append([]string{first.RequestID}, result.RequestIDs...)
	return result
}

func table(n int, edit bool) (string, string) {
	keys := []int{0, n / 4, n / 2, 3 * n / 4, n - 1}
	var text strings.Builder
	text.WriteString("Synthetic immutable records follow. Retrieve only the requested keys, in the requested order. Do not infer values from keys.\n")
	for i := 0; i < n; i++ {
		value := fmt.Sprintf("VALUE_%09d", 100000003+i*7919)
		if edit && i == n-1 {
			value = "VALUE_998877665"
		}
		fmt.Fprintf(&text, "record-%06d = %s; checksum-label synthetic-only.\n", i, value)
	}
	values := make([]string, len(keys))
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = fmt.Sprintf("record-%06d", k)
		values[i] = fmt.Sprintf("VALUE_%09d", 100000003+k*7919)
		if edit && k == n-1 {
			values[i] = "VALUE_998877665"
		}
	}
	fmt.Fprintf(&text, "Return exactly the values for %s, joined by a single comma, without spaces or explanation.", strings.Join(names, ", "))
	return text.String(), strings.Join(values, ",")
}

func historyAppend(n, turn int) (string, string) {
	// Neither turn re-queries the original five keys or the preceding append's
	// keys. Replaying an assistant answer cannot substitute for older context.
	keys := [2][4]int{{1, n / 3, 2 * n / 3, n - 2}, {2, n / 5, 4 * n / 5, n - 3}}[turn-1]
	names := make([]string, len(keys))
	values := make([]string, len(keys))
	for i, key := range keys {
		names[i] = fmt.Sprintf("record-%06d", key)
		values[i] = fmt.Sprintf("VALUE_%09d", 100000003+key*7919)
	}
	marker := fmt.Sprintf("APPEND_%d_314159265", turn)
	prompt := fmt.Sprintf("Retrieve these different keys from the original record table in the first user message: %s. Return exactly their values in that order, followed by %s, all joined by a single comma, without spaces or explanation.", strings.Join(names, ", "), marker)
	return prompt, strings.Join(append(values, marker), ",")
}

func tokenCounts(response contract.Response) (input, cached int64, ok bool) {
	r := response.Report.Record
	if r == nil || r.Tokens.Input == nil || r.Tokens.Cached == nil {
		return 0, 0, false
	}
	input, cached = *r.Tokens.Input, *r.Tokens.Cached
	return input, cached, input > 0 && cached >= 0 && cached <= input
}

// calibratedTable has a finite three-probe budget. A measured depth is mandatory;
// generated string size or configured context capacity is never coverage proof.
func calibratedTable(ctx context.Context, opts contract.Options, call contract.Call, name string, target int64, effort string) (int, contract.Response, []contract.Result, bool) {
	n := 128
	var results []contract.Result
	var response contract.Response
	if opts.Tokenize == nil {
		return n, response, []contract.Result{blocked(name+"-depth", "native tokenizer unavailable")}, false
	}
	for attempt := 0; attempt < 3; attempt++ {
		text, want := table(n, false)
		caseName := fmt.Sprintf("%s-calibration-%d", name, attempt)
		count, err := opts.Tokenize(ctx, text)
		if err != nil || count <= 0 {
			return n, response, append(results, blocked(caseName, "native tokenizer failed or returned invalid count")), false
		}
		input := int64(count)
		if input >= target && input <= target+512 {
			response, err = call(ctx, name+"-cold", request(opts, []any{message("user", text)}, effort, false, false))
			if err == nil {
				err = exact(response, want)
			}
			if err != nil {
				return n, response, append(results, outcome(name+"-cold", response, err)), false
			}
			observed, cache, present := tokenCounts(response)
			if !present || observed < target || observed > target+4096 || cache != 0 {
				return n, response, append(results, blocked(name+"-cold", "missing cold work counts or measured depth outside envelope")), false
			}
			return n, response, append(results, outcome(name+"-cold", response, nil)), true
		}
		// Leave a measured 512-token margin to avoid falling just below the gate.
		next := int(int64(n) * (target + 128) / input)
		if next < 64 || next > 20000 {
			break
		}
		n = next
	}
	return n, response, append(results, blocked(name+"-depth", "finite calibration did not establish target token depth")), false
}

func History(ctx context.Context, opts contract.Options, call contract.Call) []contract.Result {
	const target = 247000
	// Reserve two complete 4096-token assistant histories plus the next answer.
	// A measured cold ceiling limits rendered overhead, rather than assuming
	// the bare corpus tokenizer includes chat framing. Each append's actual
	// measured input must still leave the full output budget below the pool.
	n, cold, results, ok := calibratedTable(ctx, opts, call, "history", target, "xhigh")
	if !ok {
		return results
	}
	if input, _, present := tokenCounts(cold); !present || input > target+1024 {
		return append(results, blocked("history-initial-reserve", "reported cold depth leaves insufficient declared history/framing reserve"))
	}
	text, want := table(n, false)
	messages := []any{message("user", text)}
	r := request(opts, messages, "xhigh", true, false)
	// First cache-enabled population is separate from the cold calibration.
	population, err := call(ctx, "history-populate", r)
	if err == nil {
		err = exact(population, want)
	}
	results = append(results, outcome("history-populate", population, err))
	if err != nil {
		return results
	}
	previous := population
	appendTurn := 0
	for _, phase := range []string{"repeat", "edit", "append-1", "append-2"} {
		if phase == "edit" {
			text, want = table(n, true)
			messages = []any{message("user", text)}
		}
		if strings.HasPrefix(phase, "append") {
			if previous.Message == nil {
				return append(results, blocked("history-"+phase, "complete assistant object unavailable; cannot preserve reasoning history"))
			}
			appendTurn++
			prompt, expected := historyAppend(n, appendTurn)
			messages = append(messages, previous.Message, message("user", prompt))
			want = expected
		}
		response, callErr := call(ctx, "history-"+phase, request(opts, messages, "xhigh", true, false))
		if callErr == nil {
			callErr = exact(response, want)
		}
		result := outcome("history-"+phase, response, callErr)
		if callErr == nil {
			input, cache, present := tokenCounts(response)
			if !present {
				result = blocked(result.Name, "missing or invalid measured input/cache counts")
			} else if input < target || input > 262144-4096 {
				result = blocked(result.Name, "measured history depth outside declared near-limit envelope")
			} else if cache < input/2 {
				result.Status = contract.Fail
				result.Detail = "expected substantial live prefix reuse"
			}
			if phase == "repeat" && present && cache < input-128 {
				result.Status = contract.Fail
				result.Detail = "exact repeat failed near-total prefix-reuse oracle"
			}
		}
		results = append(results, result)
		if result.Status != contract.Pass {
			return results
		}
		previous = response
	}
	results = append(results, idleHistory(ctx, opts, call, n)...)
	results = append(results, concurrent(ctx, opts, call))
	for _, stream := range []bool{false, true} {
		results = append(results, cancellation(ctx, opts, call, stream))
	}
	results = append(results, media(ctx, opts, call)...)
	return results
}

func Prefill(ctx context.Context, opts contract.Options, call contract.Call) []contract.Result {
	_, response, results, ok := calibratedTable(ctx, opts, call, "prefill-32k", 32768, "none")
	if !ok {
		return results
	}
	r := response.Report.Record
	measurement := outcome("prefill-32k-measurement", response, nil)
	if r == nil || r.BackendTimings.PromptTokens == nil || r.BackendTimings.PromptMilliseconds == nil || *r.BackendTimings.PromptMilliseconds <= 0 || *r.BackendTimings.PromptTokens < 32768 {
		measurement = blocked(measurement.Name, "missing paired native prompt-work and duration observations")
	} else {
		measurement.Measurements = map[string]float64{"prompt_tokens": float64(*r.BackendTimings.PromptTokens), "prompt_ms": *r.BackendTimings.PromptMilliseconds, "prompt_tokens_per_second": float64(*r.BackendTimings.PromptTokens) * 1000 / *r.BackendTimings.PromptMilliseconds}
	}
	results = append(results, measurement)
	return append(results, conditionedPrefill(ctx, opts, call)...)
}
