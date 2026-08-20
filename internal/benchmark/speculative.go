package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const SpeculativeSchema = "rocmplete.llama-speculative-depth-sweep.v1"

func SpeculativeMessages(target, seed int) []map[string]string {
	lineCount := target / 14
	if lineCount < 1 {
		lineCount = 1
	}
	var packet strings.Builder
	packet.Grow(lineCount * 70)
	for index := 0; index < lineCount; index++ {
		fmt.Fprintf(&packet, "queue_case_%06d_%08x: owner=worker cancellation=checked invariant=bounded\n", index, uint32(index*1103515245+seed))
	}
	return []map[string]string{
		{"role": "system", "content": "You are a senior Go engineer in a deterministic performance evaluation. Study the repository packet before answering."},
		{"role": "user", "content": packet.String() + "\nExplain a correct bounded work queue with context cancellation, idempotent Close, and focused race tests."},
	}
}

func PostJSON(ctx context.Context, endpoint string, payload any, limit int64) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(contents)) > limit {
		return nil, fmt.Errorf("model response is unreadable or exceeds %d bytes", limit)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("model server returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(contents)))
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("model server returned invalid JSON: %w", err)
	}
	return value, nil
}

func WaitForHealth(ctx context.Context, endpoint string) error {
	return WaitForURL(ctx, endpoint+"/health")
}

func WaitForURL(ctx context.Context, endpoint string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		response, err := client.Do(request)
		if err == nil {
			contents, _ := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 && len(contents) > 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func ParseSpeculativeResponse(value map[string]any, wall, startup float64) (map[string]any, error) {
	usage, ok := value["usage"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("llama.cpp response has no usage object")
	}
	timings, ok := value["timings"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("llama.cpp response has no timings object")
	}
	fields := map[string]string{"prompt_tokens": "prompt_tokens", "completion_tokens": "completion_tokens", "drafted_tokens": "draft_n", "accepted_draft_tokens": "draft_n_accepted"}
	metrics := map[string]any{"request_seconds": wall, "startup_seconds": startup}
	for output, input := range fields {
		source := usage
		if strings.HasSuffix(input, "_n") || strings.HasPrefix(input, "draft_") {
			source = timings
		}
		integer, err := integer(source[input])
		if err != nil || integer < 0 {
			return nil, fmt.Errorf("llama.cpp response has invalid %s", input)
		}
		metrics[output] = integer
	}
	for output, input := range map[string]string{"generation_tokens_per_second": "predicted_per_second", "prompt_tokens_per_second": "prompt_per_second", "predicted_ms": "predicted_ms", "prompt_ms": "prompt_ms"} {
		value, err := number(timings[input])
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("llama.cpp response has invalid %s", input)
		}
		metrics[output] = value
	}
	drafted := metrics["drafted_tokens"].(int64)
	accepted := metrics["accepted_draft_tokens"].(int64)
	if accepted > drafted {
		return nil, fmt.Errorf("llama.cpp accepted more draft tokens than it proposed")
	}
	acceptance := 0.0
	if drafted > 0 {
		acceptance = float64(accepted) * 100 / float64(drafted)
	}
	metrics["acceptance_percent"] = acceptance
	identity, _ := json.Marshal(map[string]any{"choices": value["choices"]})
	digest := sha256.Sum256(identity)
	metrics["response_sha256"] = hex.EncodeToString(digest[:])
	metrics["message"] = value["choices"]
	return metrics, nil
}

func SpeculativeSummary(trials []any, incumbent int) map[string]any {
	type totals struct {
		trials, generated, drafted, accepted int64
		predictedMS, promptTokens, promptMS  float64
	}
	byDepth := map[int]*totals{}
	failed := 0
	for _, raw := range trials {
		trial, ok := raw.(map[string]any)
		if !ok || trial["status"] != "complete" {
			if ok && trial["status"] == "failed" {
				failed++
			}
			continue
		}
		depth, _ := integer(trial["depth"])
		total := byDepth[int(depth)]
		if total == nil {
			total = &totals{}
			byDepth[int(depth)] = total
		}
		total.trials++
		generated, _ := integer(trial["completion_tokens"])
		drafted, _ := integer(trial["drafted_tokens"])
		accepted, _ := integer(trial["accepted_draft_tokens"])
		predictedMS, _ := number(trial["predicted_ms"])
		promptTokens, _ := integer(trial["prompt_tokens"])
		promptMS, _ := number(trial["prompt_ms"])
		total.generated += generated
		total.drafted += drafted
		total.accepted += accepted
		total.predictedMS += predictedMS
		total.promptTokens += float64(promptTokens)
		total.promptMS += promptMS
	}
	depths := map[string]any{}
	winner, winnerRate := 0, 0.0
	for depth, total := range byDepth {
		generationRate, promptRate, acceptance := 0.0, 0.0, 0.0
		if total.predictedMS > 0 {
			generationRate = float64(total.generated) / (total.predictedMS / 1000)
		}
		if total.promptMS > 0 {
			promptRate = total.promptTokens / (total.promptMS / 1000)
		}
		if total.drafted > 0 {
			acceptance = float64(total.accepted) * 100 / float64(total.drafted)
		}
		depths[fmt.Sprint(depth)] = map[string]any{"complete_trials": total.trials, "generation_tokens_per_second": generationRate, "prompt_tokens_per_second": promptRate, "drafted_tokens": total.drafted, "accepted_draft_tokens": total.accepted, "acceptance_percent": acceptance}
		if generationRate > winnerRate {
			winner, winnerRate = depth, generationRate
		}
	}
	return map[string]any{"complete_trials": len(trials) - failed, "total_trials": len(trials), "failed_trials": failed, "depths": depths, "incumbent_depth": incumbent, "winner_depth": winner}
}
