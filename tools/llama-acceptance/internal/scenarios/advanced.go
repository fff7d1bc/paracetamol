package scenarios

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"time"

	"paracetamol/internal/gateway"
	"paracetamol/tools/llama-acceptance/internal/contract"
)

func slots(ctx context.Context, opts contract.Options, name string) ([]contract.Slot, contract.Result) {
	if opts.Slots == nil {
		return nil, blocked(name, "native slot probe unavailable")
	}
	s, err := opts.Slots(ctx)
	if err != nil || len(s) != 4 {
		return nil, blocked(name, "requires four observable native slots")
	}
	seen := map[int]bool{}
	sum := int64(0)
	for _, slot := range s {
		if slot.ID < 0 || slot.ID > 3 || seen[slot.ID] || slot.PromptTokens == nil || *slot.PromptTokens < 0 {
			return nil, blocked(name, "missing native slot occupancy")
		}
		seen[slot.ID] = true
		sum += *slot.PromptTokens
	}
	return s, contract.Result{Name: name, Status: contract.Pass, Measurements: map[string]float64{"sum_slot_prompt_tokens": float64(sum)}}
}

func idleHistory(ctx context.Context, opts contract.Options, call contract.Call, n int) []contract.Result {
	_, probe := slots(ctx, opts, "history-idle-preflight")
	if probe.Status != contract.Pass {
		return []contract.Result{probe}
	}
	// A smaller resident history leaves room for a second sequence, unlike the
	// near-limit single sequence above. A named slot is part of the test input.
	if n > 2048 {
		n = 2048
	}
	text, want := table(n, false)
	r := request(opts, []any{message("user", text)}, "none", true, false)
	r["id_slot"] = 0
	first, err := call(ctx, "history-idle-populate", r)
	if err == nil {
		err = exact(first, want)
	}
	results := []contract.Result{outcome("history-idle-populate", first, err)}
	if err != nil {
		return results
	}
	s, before := slots(ctx, opts, "history-idle-resident")
	results = append(results, before)
	if before.Status != contract.Pass {
		return results
	}
	var original int64
	for _, slot := range s {
		if slot.ID == 0 {
			original = *slot.PromptTokens
		}
	}
	if original < 1000 {
		return append(results, blocked("history-idle-resident", "resident sequence not observed in selected slot"))
	}
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("history-active-second-%d", i)
		r2 := request(opts, []any{message("user", fmt.Sprintf("Return exactly SECOND_%d.", i))}, "none", false, false)
		r2["id_slot"] = 1
		response, e := call(ctx, name, r2)
		if e == nil {
			e = exact(response, fmt.Sprintf("SECOND_%d", i))
		}
		results = append(results, outcome(name, response, e))
		if e != nil {
			return results
		}
	}
	s, after := slots(ctx, opts, "history-idle-after-second")
	results = append(results, after)
	if after.Status != contract.Pass {
		return results
	}
	for _, slot := range s {
		if slot.ID == 0 && *slot.PromptTokens < original {
			return append(results, outcome("history-idle-preserved", contract.Response{}, fmt.Errorf("idle resident history lost while second slot worked")))
		}
	}
	response, err := call(ctx, "history-idle-reactivate", r)
	if err == nil {
		err = exact(response, want)
	}
	result := outcome("history-idle-reactivate", response, err)
	if err == nil {
		input, cache, ok := tokenCounts(response)
		if !ok {
			result = blocked(result.Name, "missing reactivation cache counts")
		} else if cache < input-128 {
			result.Status = contract.Fail
			result.Detail = "resident idle prefix was not reused"
		}
	}
	return append(results, result)
}

func concurrent(ctx context.Context, opts contract.Options, call contract.Call) contract.Result {
	_, probe := slots(ctx, opts, "four-worker-isolation")
	if probe.Status != contract.Pass {
		return probe
	}
	type completed struct {
		response contract.Response
		err      error
	}
	done := make(chan completed, 4)
	for i := 0; i < 4; i++ {
		go func(i int) {
			r := request(opts, []any{message("user", fmt.Sprintf("Return exactly WORKER_%d, no other text.", i))}, "none", false, true)
			r["id_slot"] = i
			response, err := call(ctx, fmt.Sprintf("worker-%d", i), r)
			if err == nil {
				err = exact(response, fmt.Sprintf("WORKER_%d", i))
			}
			done <- completed{response, err}
		}(i)
	}
	result := contract.Result{Name: "four-worker-isolation", Status: contract.Pass}
	for i := 0; i < 4; i++ {
		item := <-done
		if item.response.RequestID != "" {
			result.RequestIDs = append(result.RequestIDs, item.response.RequestID)
		}
		if item.err != nil {
			result.Status = contract.Fail
			result.Detail = item.err.Error()
		}
	}
	// Fast markers can finish between probes. Do not equate four launched
	// clients with native overlapping generation or a throughput benchmark.
	if result.Status == contract.Pass {
		result.Detail = "four independent slot-directed markers passed; overlap and throughput are not inferred"
	}
	return result
}

func cancellation(ctx context.Context, opts contract.Options, call contract.Call, stream bool) contract.Result {
	name := fmt.Sprintf("cancel-stream-%t", stream)
	_, probe := slots(ctx, opts, name)
	if probe.Status != contract.Pass {
		return probe
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	type completed struct {
		response contract.Response
		err      error
	}
	done := make(chan completed, 1)
	r := request(opts, []any{message("user", "Write an extremely long numbered list of consecutive integers, one per line, continuing without a summary until the output limit.")}, "none", false, stream)
	r["max_tokens"] = 32768
	r["id_slot"] = 3
	go func() { response, err := call(child, name, r); done <- completed{response, err} }()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case item := <-done:
			return outcome(name, item.response, fmt.Errorf("request ended before native decoded-token cancellation precondition: %v", item.err))
		case <-ctx.Done():
			cancel()
			// Call still owns partial-response/report publication after its HTTP
			// context is canceled. Join it before the runner hashes final evidence.
			item := <-done
			result := outcome(name, item.response, nil)
			result.Status, result.Detail = contract.Blocked, "parent context canceled before cancellation precondition"
			return result
		case <-tick.C:
			s, err := opts.Slots(ctx)
			if err != nil {
				cancel()
				<-done
				return blocked(name, "native slot observation failed")
			}
			for _, slot := range s {
				if slot.ID == 3 && slot.Processing && slot.DecodedTokens != nil && *slot.DecodedTokens > 0 {
					start := time.Now()
					cancel()
					item := <-done
					if !errors.Is(item.err, context.Canceled) {
						return outcome(name, item.response, fmt.Errorf("request did not report expected client context cancellation: %v", item.err))
					}
					// Client cancellation alone also matches joined report/protocol
					// failures. Require the gateway's exact completed observation;
					// idle native state must not turn missing evidence into PASS.
					report := item.response.Report
					if item.response.RequestID == "" || report.Record == nil {
						result := outcome(name, item.response, nil)
						result.Status, result.Detail = contract.Blocked, "correlated canceled request report unavailable"
						return result
					}
					if report.State != "completed" || report.Record.ID != item.response.RequestID || report.Record.Outcome != gateway.OutcomeCanceled {
						return outcome(name, item.response, fmt.Errorf("gateway report does not establish the matching completed canceled request"))
					}
					if !item.response.CancellationVerified {
						return outcome(name, item.response, fmt.Errorf("transport could not verify the canceled request's policy, correlation and persisted evidence"))
					}
					// Only post-disconnect metadata waiting is bounded, never inference.
					wait, stop := context.WithTimeout(ctx, 10*time.Second)
					defer stop()
					for {
						observed, e := opts.Slots(wait)
						if e != nil {
							return blocked(name, "post-disconnect native slot probe failed")
						}
						for _, idle := range observed {
							if idle.ID == 3 && !idle.Processing {
								result := outcome(name, item.response, nil)
								result.Measurements = map[string]float64{"disconnect_to_idle_ms": float64(time.Since(start).Microseconds()) / 1000}
								r2 := request(opts, []any{message("user", "Return exactly CANCEL_RECOVERY.")}, "none", false, false)
								r2["id_slot"] = 3
								follow, e := call(ctx, name+"-recovery", r2)
								if e == nil {
									e = exact(follow, "CANCEL_RECOVERY")
								}
								if follow.RequestID != "" {
									result.RequestIDs = append(result.RequestIDs, follow.RequestID)
								}
								if e != nil {
									result.Status = contract.Fail
									result.Detail = e.Error()
								}
								return result
							}
						}
						select {
						case <-wait.Done():
							return outcome(name, item.response, fmt.Errorf("native slot remained busy after disconnect"))
						case <-tick.C:
						}
					}
				}
			}
		}
	}
}

func imageURL(red bool) string {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	c := color.RGBA{0, 0, 255, 255}
	if red {
		c = color.RGBA{255, 0, 0, 255}
	}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var data bytes.Buffer
	_ = png.Encode(&data, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
}

func media(ctx context.Context, opts contract.Options, call contract.Call) []contract.Result {
	var results []contract.Result
	var previous contract.Response
	var previousMessages []any
	for _, phase := range []string{"red", "same-red", "blue", "text"} {
		var messages []any
		want := "RED"
		content := []any{map[string]any{"type": "text", "text": "Return exactly RED or BLUE naming the solid image color."}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL(phase != "blue")}}}
		messages = []any{map[string]any{"role": "user", "content": content}}
		if phase == "blue" {
			want = "BLUE"
		}
		if phase == "text" {
			if previous.Message == nil {
				return append(results, blocked("media-text", "complete assistant history missing"))
			}
			messages = append(previousMessages, previous.Message, message("user", "Ignore prior colors. Return exactly TEXT_RECOVERY."))
			want = "TEXT_RECOVERY"
		}
		r := request(opts, messages, "none", true, false)
		r["id_slot"] = 2
		response, err := call(ctx, "media-"+phase, r)
		if err == nil {
			err = exact(response, want)
		}
		result := outcome("media-"+phase, response, err)
		if err == nil && phase == "same-red" {
			input, cache, ok := tokenCounts(response)
			if !ok {
				result = blocked(result.Name, "missing media cache counts")
			} else if cache < input/2 {
				result.Status = contract.Fail
				result.Detail = "identical-media prefix reuse missing"
			}
		}
		results = append(results, result)
		if result.Status != contract.Pass {
			return results
		}
		previous = response
		previousMessages = messages
	}
	return results
}

// conditioningRequest is newly authored. It retains only the public synthetic
// reproducer's family/slot/depth sequence, not its former runner implementation.
func conditioningRequest(ctx context.Context, opts contract.Options, call contract.Call, family string, slot, target int) (contract.Response, contract.Result) {
	if opts.Tokenize == nil {
		return contract.Response{}, blocked(family, "native tokenizer unavailable")
	}
	n := 128
	for attempt := 0; attempt < 5; attempt++ {
		text, want := table(n, false)
		text = "Independent fixture family " + family + ".\n" + text
		count, err := opts.Tokenize(ctx, text)
		if err != nil || count <= 0 {
			return contract.Response{}, blocked(family, "native tokenizer unavailable")
		}
		if count >= target && count < target+128 {
			r := request(opts, []any{message("user", text)}, "none", true, false)
			r["id_slot"] = slot
			response, e := call(ctx, "condition-"+family+fmt.Sprintf("-%d", target), r)
			if e == nil {
				e = exact(response, want)
			}
			result := outcome("condition-"+family+fmt.Sprintf("-%d", target), response, e)
			if e == nil {
				input, _, ok := tokenCounts(response)
				if !ok || input < int64(target) || input > int64(target+4096) {
					result = blocked(result.Name, "measured conditioning depth unavailable or outside envelope")
				}
			}
			return response, result
		}
		n = n * (target + 32) / count
		if n < 64 || n > 20000 {
			break
		}
	}
	return contract.Response{}, blocked(family, "conditioning calibration failed")
}

func conditionedPrefill(ctx context.Context, opts contract.Options, call contract.Call) []contract.Result {
	_, probe := slots(ctx, opts, "conditioning-preflight")
	if probe.Status != contract.Pass {
		return []contract.Result{probe}
	}
	var results []contract.Result
	// The public distributed oracle returns more tokens than the historical
	// single marker. Reserve 300 extra cells so its answer cannot evict another
	// resident sequence just before the near-pool observation.
	for _, step := range []struct {
		family       string
		slot, target int
	}{{"alpha", 0, 74000}, {"beta", 1, 74000}, {"gamma", 2, 74000}, {"delta", 3, 74000}, {"epsilon", 0, 74000}, {"epsilon", 0, 113600}} {
		response, result := conditioningRequest(ctx, opts, call, step.family, step.slot, step.target)
		if result.Status == contract.Pass {
			_, cache, present := tokenCounts(response)
			if !present {
				result.Status = contract.Blocked
				result.Detail = "conditioning cache work unobserved"
			} else if step.target == 113600 && cache < 70000 {
				result.Status = contract.Fail
				result.Detail = "epsilon extension did not retain its existing prefix"
			} else if step.target == 74000 && cache > 128 {
				result.Status = contract.Fail
				result.Detail = "distinct conditioning prefix unexpectedly reused prior family"
			}
		}
		results = append(results, result)
		if result.Status != contract.Pass {
			return results
		}
		observed, snapshot := slots(ctx, opts, result.Name+"-slots")
		if snapshot.Status == contract.Pass {
			for _, slot := range observed {
				if slot.ID == step.slot && *slot.PromptTokens < int64(step.target) {
					snapshot.Status = contract.Blocked
					snapshot.Detail = "selected slot did not retain measured conditioning prefix"
				}
			}
		}
		results = append(results, snapshot)
		if snapshot.Status != contract.Pass {
			return results
		}
	}
	_, boundary := slots(ctx, opts, "conditioning-near-pool")
	occupancy := boundary.Measurements["sum_slot_prompt_tokens"]
	if boundary.Status == contract.Pass && (occupancy < 261500 || occupancy > 262144) {
		boundary.Status = contract.Blocked
		boundary.Detail = "maintained reproducer's near-pool live occupancy was not established"
	}
	results = append(results, boundary)
	// Even an unestablished boundary retains the subsequent measured result,
	// but it cannot acquire a cache-conditioned acceptance claim.
	response, result := conditioningRequest(ctx, opts, call, "zeta", 2, 16000)
	if result.Status == contract.Pass {
		input, cache, ok := tokenCounts(response)
		if !ok || cache > 128 {
			result.Status = contract.Blocked
			result.Detail = "aged16k cold work not established"
		} else {
			native := response.Report.Record.BackendTimings
			if native.PromptTokens == nil || native.PromptMilliseconds == nil || *native.PromptMilliseconds <= 0 {
				result.Status = contract.Blocked
				result.Detail = "aged16k native work/time missing"
			} else {
				result.Measurements = map[string]float64{"input_tokens": float64(input), "cached_tokens": float64(cache), "prompt_tokens": float64(*native.PromptTokens), "prompt_ms": *native.PromptMilliseconds}
			}
		}
	}
	result.Name = "prefill-aged16k"
	if result.Status == contract.Pass {
		result.Detail = "conditioning matches versioned synthetic reproducer only when near-pool snapshot passes; physical span and fast-kernel activation require independent native trace"
	}
	results = append(results, result)
	return results
}
