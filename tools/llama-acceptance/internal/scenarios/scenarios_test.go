package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paracetamol/internal/gateway"
	"paracetamol/tools/llama-acceptance/internal/contract"
)

func ptr[T any](v T) *T { return &v }

func fakeResponse(text string, input, cache int64) contract.Response {
	return contract.Response{RequestID: "synthetic-request", Text: text, Finish: "stop", Message: map[string]any{"role": "assistant", "content": text, "reasoning_content": "preserve this opaque reasoning", "opaque": map[string]any{"nested": "preserve"}}, Report: gateway.RequestReport{Record: &gateway.RequestRecord{Tokens: gateway.TokenUsage{Input: ptr(input), Cached: ptr(cache)}, BackendTimings: gateway.BackendTimings{PromptTokens: ptr(input - cache), PromptMilliseconds: ptr(float64(input-cache) / 2)}}}}
}

func fakeTokens(_ context.Context, text string) (int, error) {
	return strings.Count(text, "checksum-label synthetic-only.")*24 + 64, nil
}

func TestFixtureSelfTestAndManifest(t *testing.T) {
	if err := ValidateFixtures(); err != nil {
		t.Fatalf("%v; manifest=%v", err, FixtureManifest())
	}
	first := FixtureManifest()
	second := FixtureManifest()
	if !reflect.DeepEqual(first, second) || len(first) != 13 {
		t.Fatalf("unstable/incomplete manifest: %v", first)
	}
	text, want := table(128, false)
	edited, changed := table(128, true)
	if text == edited || want == changed || !strings.HasSuffix(changed, "VALUE_998877665") {
		t.Fatal("late edit generator/oracle missing")
	}
	if strings.Contains(want, "998877665") {
		t.Fatal("edited value leaked into baseline oracle")
	}
}

func TestHistoryAppendQueriesUseDifferentOriginalRecords(t *testing.T) {
	for _, n := range []int{64, 128, 2048, 10000, 20000} {
		seen := map[int]bool{0: true, n / 4: true, n / 2: true, 3 * n / 4: true, n - 1: true}
		for turn, keys := range [2][4]int{{1, n / 3, 2 * n / 3, n - 2}, {2, n / 5, 4 * n / 5, n - 3}} {
			prompt, want := historyAppend(n, turn+1)
			parts := strings.Split(want, ",")
			if len(parts) != 5 || parts[4] != fmt.Sprintf("APPEND_%d_314159265", turn+1) {
				t.Fatalf("missing current-turn marker: %s", want)
			}
			for i, key := range keys {
				if key < 0 || key >= n || seen[key] {
					t.Fatalf("n=%d turn=%d repeats/unavailable key=%d", n, turn+1, key)
				}
				seen[key] = true
				if !strings.Contains(prompt, fmt.Sprintf("record-%06d", key)) || parts[i] != fmt.Sprintf("VALUE_%09d", 100000003+key*7919) || strings.Contains(prompt, parts[i]) {
					t.Fatalf("prompt/oracle mismatch or leaked value: %s => %s", prompt, want)
				}
			}
		}
	}
}

func TestShortPositiveAndKnownBadReplay(t *testing.T) {
	for _, bad := range []string{"", "ALDER_937261\n</think>\nALDER_937261", "WRONG"} {
		t.Run(bad, func(t *testing.T) {
			requests := map[string]map[string]any{}
			call := func(_ context.Context, name string, r map[string]any) (contract.Response, error) {
				requests[name] = r
				if strings.HasSuffix(name, "-call") {
					return contract.Response{RequestID: name, Finish: "tool_calls", Message: map[string]any{"role": "assistant", "reasoning_content": "keep-me", "tool_calls": []any{map[string]any{"id": "tool-1"}}}, Tools: []contract.ToolCall{{ID: "tool-1", Name: "lookup_record", Arguments: map[string]any{"query": map[string]any{"key": "ALDER"}}}}}, nil
				}
				if strings.HasSuffix(name, "-result") {
					messages := r["messages"].([]any)
					history := messages[len(messages)-2].(map[string]any)
					if history["reasoning_content"] != "keep-me" || history["tool_calls"] == nil {
						t.Error("assistant tool/reasoning history lost")
					}
				}
				want := "ALDER_937261"
				if strings.HasPrefix(name, "effort-") {
					want = "EFFORT_271828"
				}
				if name == "omitted-effort" {
					want = "DEFAULT_161803"
					if _, exists := r["reasoning_effort"]; exists {
						t.Error("omitted-effort case sends an effort")
					}
				}
				if bad != "" && strings.HasPrefix(name, "off-") {
					want = bad
				}
				return fakeResponse(want, 400, 0), nil
			}
			results := Short(context.Background(), contract.Options{Model: "runtime-model"}, call)
			if len(results) != 12 {
				t.Fatalf("got %d cases", len(results))
			}
			for _, r := range results {
				if strings.HasPrefix(r.Name, "off-") && bad != "" {
					if r.Status != contract.Fail {
						t.Fatalf("known bad replay accepted: %v", r)
					}
				} else if r.Status != contract.Pass {
					t.Fatalf("positive failed: %v", r)
				}
			}
			for name, r := range requests {
				if r["model"] != "runtime-model" {
					t.Fatal("model hardcoded")
				}
				for _, key := range []string{"top_p", "top_k", "min_p", "presence_penalty", "repeat_penalty"} {
					if _, ok := r[key]; ok {
						t.Fatalf("%s overrides sampler %s", name, key)
					}
				}
				_, explicit := r["temperature"]
				if explicit != (name == "off-replay-temperature-zero") {
					t.Fatal("unexpected temperature override")
				}
			}
		})
	}
}

func TestShortTruncationAndMalformedToolFail(t *testing.T) {
	results := Short(context.Background(), contract.Options{}, func(_ context.Context, name string, _ map[string]any) (contract.Response, error) {
		r := fakeResponse("ALDER_937261", 400, 0)
		r.Finish = "length"
		return r, nil
	})
	for _, r := range results {
		if r.Status != contract.Fail {
			t.Fatalf("truncation/malformed tool passed: %v", r)
		}
	}
}

func TestHistoryMissingDepthAndCacheBlock(t *testing.T) {
	if r := History(context.Background(), contract.Options{}, nil); contract.Worst(r) != contract.Blocked {
		t.Fatal(r)
	}
	r := History(context.Background(), contract.Options{Tokenize: fakeTokens}, func(_ context.Context, _ string, r map[string]any) (contract.Response, error) {
		text := r["messages"].([]any)[0].(map[string]any)["content"].(string)
		n := strings.Count(text, "checksum-label synthetic-only.")
		_, want := table(n, false)
		response := fakeResponse(want, int64(n*24+64), 0)
		response.Report.Record.Tokens.Cached = nil
		return response, nil
	})
	if contract.Worst(r) != contract.Blocked {
		t.Fatal(r)
	}
}

func TestHistoryAppendPreservesCompleteAssistantAndWrongValueFails(t *testing.T) {
	for _, bad := range []string{"", "wrong-value", "length", "missing-cache", "constant-only-append-1", "constant-only-append-2", "stale-original-append-1", "stale-previous-append-2", "wrong-original-value-append-2", "initial-reserve", "over-budget"} {
		t.Run(bad, func(t *testing.T) {
			appendCount := 0
			previousAppend := ""
			call := func(_ context.Context, name string, r map[string]any) (contract.Response, error) {
				if strings.HasPrefix(name, "media-") {
					want := "RED"
					if name == "media-blue" {
						want = "BLUE"
					}
					if name == "media-text" {
						want = "TEXT_RECOVERY"
					}
					return fakeResponse(want, 1000, 990), nil
				}
				if r["reasoning_effort"] != "xhigh" {
					t.Fatal("history changed reasoning policy")
				}
				messages := r["messages"].([]any)
				text := messages[0].(map[string]any)["content"].(string)
				n := strings.Count(text, "checksum-label synthetic-only.")
				_, want := table(n, name == "history-edit" || strings.Contains(name, "append"))
				input := int64(n*24 + 96)
				if strings.Contains(name, "append") {
					appendCount++
					if len(messages) != 1+2*appendCount || r["cache_prompt"] != true {
						t.Fatal("append lost prior turns or cache policy")
					}
					originalWant := want
					keys := [2][4]int{{1, n / 3, 2 * n / 3, n - 2}, {2, n / 5, 4 * n / 5, n - 3}}[appendCount-1]
					values := []string{}
					for _, key := range keys {
						values = append(values, fmt.Sprintf("VALUE_%09d", 100000003+key*7919))
					}
					marker := fmt.Sprintf("APPEND_%d_314159265", appendCount)
					want = strings.Join(append(values, marker), ",")
					if bad == "constant-only-"+strings.TrimPrefix(name, "history-") {
						want = marker
					}
					if bad == "stale-original-append-1" && appendCount == 1 {
						want = originalWant + "," + marker
					}
					if bad == "stale-previous-append-2" && appendCount == 2 {
						want = strings.TrimSuffix(previousAppend, ",APPEND_1_314159265") + "," + marker
					}
					if bad == "wrong-original-value-append-2" && appendCount == 2 {
						want = strings.Replace(want, values[0], "VALUE_000000000", 1)
					}
					previousAppend = want
					input += int64(appendCount * 100)
					found := false
					for _, value := range messages {
						m := value.(map[string]any)
						if m["role"] == "assistant" {
							found = true
							if m["reasoning_content"] != "preserve this opaque reasoning" || m["opaque"] == nil {
								t.Fatal("full assistant history was reconstructed/lost")
							}
						}
					}
					if !found {
						t.Fatal("append lacks assistant history")
					}
					if bad == "over-budget" && appendCount == 2 {
						input = 262144 - 4096 + 1
					}
				}
				cache := input - 32
				if strings.HasSuffix(name, "-cold") {
					cache = 0
					if bad == "initial-reserve" {
						input = 247000 + 1024 + 1
					}
				}
				if name == "history-edit" && bad == "wrong-value" {
					want = "VALUE_WRONG"
				}
				response := fakeResponse(want, input, cache)
				if name == "history-append-1" && bad == "length" {
					response.Finish = "length"
				}
				if name == "history-repeat" && bad == "missing-cache" {
					response.Report.Record.Tokens.Cached = nil
				}
				return response, nil
			}
			results := History(context.Background(), contract.Options{Tokenize: fakeTokens}, call)
			if bad == "" {
				if appendCount != 2 {
					t.Fatalf("appends=%d results=%v", appendCount, results)
				}
				for _, r := range results {
					if r.Status == contract.Fail {
						t.Fatal(r)
					}
					switch r.Name {
					case "history-cold", "history-populate", "history-repeat", "history-edit", "history-append-1", "history-append-2":
						if r.Status != contract.Pass {
							t.Fatalf("positive near-limit history was not established: %+v", r)
						}
					}
				}
			} else if bad == "missing-cache" || bad == "initial-reserve" || bad == "over-budget" {
				if contract.Worst(results) != contract.Blocked {
					t.Fatal(results)
				}
			} else if contract.Worst(results) != contract.Fail {
				t.Fatalf("bad answer accepted: %v", results)
			}
		})
	}
}

func TestPrefillMeasuredWorkAndMissingTiming(t *testing.T) {
	for _, missing := range []bool{false, true} {
		call := func(_ context.Context, _ string, r map[string]any) (contract.Response, error) {
			text := r["messages"].([]any)[0].(map[string]any)["content"].(string)
			n := strings.Count(text, "checksum-label synthetic-only.")
			_, want := table(n, false)
			response := fakeResponse(want, int64(n*24+96), 0)
			if missing {
				response.Report.Record.BackendTimings.PromptMilliseconds = nil
			}
			return response, nil
		}
		results := Prefill(context.Background(), contract.Options{Tokenize: fakeTokens}, call)
		found := false
		for _, r := range results {
			if r.Name == "prefill-32k-measurement" {
				found = true
				if missing && r.Status != contract.Blocked {
					t.Fatal(r)
				}
				if !missing && (r.Status != contract.Pass || r.Measurements["prompt_tokens_per_second"] != 2000) {
					t.Fatal(r)
				}
			}
		}
		if !found {
			t.Fatal(results)
		}
	}
}

func TestConditioningRejectsUnobservedBoundaryAndWrongValue(t *testing.T) {
	for _, bad := range []bool{false, true} {
		opts := contract.Options{Tokenize: fakeTokens, Slots: func(context.Context) ([]contract.Slot, error) { return testSlots(114000, false), nil }}
		call := func(_ context.Context, _ string, r map[string]any) (contract.Response, error) {
			text := r["messages"].([]any)[0].(map[string]any)["content"].(string)
			n := strings.Count(text, "checksum-label synthetic-only.")
			_, want := table(n, false)
			if bad {
				want = "WRONG"
			}
			cache := int64(0)
			if strings.Contains(text, "epsilon") && n*24 > 100000 {
				cache = 74000
			}
			return fakeResponse(want, int64(n*24+96), cache), nil
		}
		results := conditionedPrefill(context.Background(), opts, call)
		if bad && contract.Worst(results) != contract.Fail {
			t.Fatal(results)
		}
		if !bad && contract.Worst(results) != contract.Blocked {
			t.Fatalf("unobserved boundary accepted: %v", results)
		}
	}
}

func TestConditioningPositiveAndMissingExtensionReuse(t *testing.T) {
	for _, badCache := range []bool{false, true} {
		occupancy := [4]int64{}
		order := []int{}
		opts := contract.Options{Tokenize: fakeTokens, Slots: func(context.Context) ([]contract.Slot, error) {
			var observed []contract.Slot
			for i, n := range occupancy {
				observed = append(observed, contract.Slot{ID: i, PromptTokens: ptr(n), DecodedTokens: ptr(int64(10))})
			}
			return observed, nil
		}}
		call := func(_ context.Context, _ string, r map[string]any) (contract.Response, error) {
			text := r["messages"].([]any)[0].(map[string]any)["content"].(string)
			n := strings.Count(text, "checksum-label synthetic-only.")
			_, want := table(n, false)
			input := int64(n*24 + 96)
			cache := int64(0)
			if strings.Contains(text, "epsilon") && input > 100000 && !badCache {
				cache = 74000
			}
			slot := r["id_slot"].(int)
			occupancy[slot] = input + 10
			for i, id := range order {
				if id == slot {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
			order = append(order, slot)
			for {
				sum := int64(0)
				for _, v := range occupancy {
					sum += v
				}
				if sum <= 262144 {
					break
				}
				oldest := order[0]
				order = order[1:]
				occupancy[oldest] = 0
			}
			return fakeResponse(want, input, cache), nil
		}
		results := conditionedPrefill(context.Background(), opts, call)
		if badCache && contract.Worst(results) != contract.Fail {
			t.Fatal(results)
		}
		if !badCache && contract.Worst(results) != contract.Pass {
			t.Fatal(results)
		}
	}
}

func testSlots(tokens int64, busy bool) []contract.Slot {
	var slots []contract.Slot
	for i := 0; i < 4; i++ {
		slots = append(slots, contract.Slot{ID: i, PromptTokens: ptr(tokens), DecodedTokens: ptr(int64(1)), Processing: busy && i == 3})
	}
	return slots
}

func canceledResponse() contract.Response {
	return contract.Response{RequestID: "synthetic-canceled-request", CancellationVerified: true, Report: gateway.RequestReport{State: "completed", Record: &gateway.RequestRecord{ID: "synthetic-canceled-request", Outcome: gateway.OutcomeCanceled}}}
}

func TestCancellationRequiresDecodedNativeSlotThenIdleAndRecovery(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var active atomic.Bool
		var recovery atomic.Bool
		opts := contract.Options{Slots: func(context.Context) ([]contract.Slot, error) { return testSlots(2000, active.Load()), nil }}
		call := func(ctx context.Context, name string, _ map[string]any) (contract.Response, error) {
			if strings.HasSuffix(name, "-recovery") {
				recovery.Store(true)
				return fakeResponse("CANCEL_RECOVERY", 20, 0), nil
			}
			active.Store(true)
			<-ctx.Done()
			active.Store(false)
			return canceledResponse(), ctx.Err()
		}
		result := cancellation(context.Background(), opts, call, stream)
		if result.Status != contract.Pass || !recovery.Load() {
			t.Fatal(result)
		}
	}
}

func TestCancellationRequiresMatchingCompletedCanceledReport(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, fault := range []string{"request-id", "record", "state", "record-id", "outcome", "unverified"} {
			t.Run(fmt.Sprintf("stream-%t/%s", stream, fault), func(t *testing.T) {
				var active atomic.Bool
				var recovery atomic.Bool
				opts := contract.Options{Slots: func(context.Context) ([]contract.Slot, error) { return testSlots(2000, active.Load()), nil }}
				call := func(ctx context.Context, name string, _ map[string]any) (contract.Response, error) {
					if strings.HasSuffix(name, "-recovery") {
						recovery.Store(true)
						return fakeResponse("CANCEL_RECOVERY", 20, 0), nil
					}
					active.Store(true)
					<-ctx.Done()
					active.Store(false)
					response := canceledResponse()
					switch fault {
					case "request-id":
						response.RequestID = ""
					case "record":
						response.Report.Record = nil
					case "state":
						response.Report.State = "active"
					case "record-id":
						response.Report.Record.ID = "another-request"
					case "outcome":
						response.Report.Record.Outcome = gateway.OutcomeSucceeded
					case "unverified":
						response.CancellationVerified = false
					}
					// errors.Is still matches cancellation when transport also
					// reports a failed correlation/protocol observation.
					return response, errors.Join(ctx.Err(), errors.New("request report validation failed"))
				}
				result := cancellation(context.Background(), opts, call, stream)
				if result.Status == contract.Pass || recovery.Load() {
					t.Fatalf("invalid canceled report accepted: %+v", result)
				}
			})
		}
	}
}

func TestParentCancellationWaitsForEvidencePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, recovering, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var published atomic.Bool
	opts := contract.Options{Slots: func(context.Context) ([]contract.Slot, error) { return testSlots(0, false), nil }}
	done := make(chan contract.Result, 1)
	go func() {
		done <- cancellation(ctx, opts, func(child context.Context, _ string, _ map[string]any) (contract.Response, error) {
			close(started)
			<-child.Done()
			close(recovering)
			<-release
			published.Store(true)
			return canceledResponse(), child.Err()
		}, false)
	}()
	<-started
	cancel()
	<-recovering
	select {
	case result := <-done:
		close(release)
		t.Fatalf("returned before evidence publication: %+v", result)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case result := <-done:
		if !published.Load() || result.Status != contract.Blocked || len(result.RequestIDs) != 1 || result.RequestIDs[0] != "synthetic-canceled-request" {
			t.Fatalf("lost interrupted request evidence: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("did not join completed evidence publisher")
	}
}

func TestFourWorkersDoNotMixMarkers(t *testing.T) {
	opts := contract.Options{Slots: func(context.Context) ([]contract.Slot, error) { return testSlots(0, false), nil }}
	for _, wrong := range []bool{false, true} {
		r := concurrent(context.Background(), opts, func(_ context.Context, name string, request map[string]any) (contract.Response, error) {
			slot := request["id_slot"].(int)
			if wrong {
				slot = 9
			}
			return fakeResponse(fmt.Sprintf("WORKER_%d", slot), 40, 0), nil
		})
		if wrong && r.Status != contract.Fail {
			t.Fatal(r)
		}
		if !wrong && r.Status != contract.Pass {
			t.Fatal(r)
		}
	}
}

func TestMediaUsesGeneratedImagesAndPreservesChangedHistory(t *testing.T) {
	results := media(context.Background(), contract.Options{}, func(_ context.Context, name string, r map[string]any) (contract.Response, error) {
		raw, _ := json.Marshal(r)
		if !strings.Contains(string(raw), "data:image/png;base64,") {
			t.Fatal("missing inline synthetic PNG")
		}
		want := "RED"
		if name == "media-blue" {
			want = "BLUE"
		}
		if name == "media-text" {
			want = "TEXT_RECOVERY"
			if !strings.Contains(string(raw), imageURL(false)) {
				t.Fatal("changed blue history lost")
			}
		}
		return fakeResponse(want, 1000, 990), nil
	})
	if contract.Worst(results) != contract.Pass {
		t.Fatal(results)
	}
}
