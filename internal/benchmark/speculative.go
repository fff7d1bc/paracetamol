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

const SpeculativeSchema = "rocmplete.llama-speculative-depth-sweep.v2"

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type SpeculativeMetrics struct {
	RequestSeconds            float64         `json:"request_seconds"`
	StartupSeconds            float64         `json:"startup_seconds"`
	PromptTokens              int64           `json:"prompt_tokens"`
	CompletionTokens          int64           `json:"completion_tokens"`
	DraftedTokens             int64           `json:"drafted_tokens"`
	AcceptedDraftTokens       int64           `json:"accepted_draft_tokens"`
	GenerationTokensPerSecond float64         `json:"generation_tokens_per_second"`
	PromptTokensPerSecond     float64         `json:"prompt_tokens_per_second"`
	PredictedMS               float64         `json:"predicted_ms"`
	PromptMS                  float64         `json:"prompt_ms"`
	AcceptancePercent         float64         `json:"acceptance_percent"`
	ResponseSHA256            string          `json:"response_sha256"`
	Message                   json.RawMessage `json:"message"`
}

type SpeculativeTrial struct {
	Identifier   string              `json:"identifier"`
	Depth        int                 `json:"depth"`
	ContextDepth int                 `json:"context_depth"`
	Seed         int                 `json:"seed"`
	Repetition   int                 `json:"repetition"`
	Status       string              `json:"status"`
	StartedAt    string              `json:"started_at,omitempty"`
	FinishedAt   string              `json:"finished_at,omitempty"`
	Error        string              `json:"error,omitempty"`
	Metrics      *SpeculativeMetrics `json:"metrics,omitempty"`
}

type SpeculativeDepthSummary struct {
	CompleteTrials            int64   `json:"complete_trials"`
	GenerationTokensPerSecond float64 `json:"generation_tokens_per_second"`
	PromptTokensPerSecond     float64 `json:"prompt_tokens_per_second"`
	DraftedTokens             int64   `json:"drafted_tokens"`
	AcceptedDraftTokens       int64   `json:"accepted_draft_tokens"`
	AcceptancePercent         float64 `json:"acceptance_percent"`
}

type SpeculativeSummaryResult struct {
	CompleteTrials int                                `json:"complete_trials"`
	TotalTrials    int                                `json:"total_trials"`
	FailedTrials   int                                `json:"failed_trials"`
	Depths         map[string]SpeculativeDepthSummary `json:"depths"`
	IncumbentDepth int                                `json:"incumbent_depth"`
	WinnerDepth    int                                `json:"winner_depth"`
}

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
	return PostJSONWithClient(ctx, http.DefaultClient, endpoint, payload, limit)
}

func PostJSONWithClient(ctx context.Context, client HTTPDoer, endpoint string, payload any, limit int64) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
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
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("model server returned trailing JSON data")
	}
	return value, nil
}

func WaitForHealth(ctx context.Context, endpoint string) error {
	return WaitForURL(ctx, endpoint+"/health")
}

func WaitForURL(ctx context.Context, endpoint string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	return WaitForURLWithClient(ctx, client, endpoint, time.Second)
}

func WaitForURLWithClient(ctx context.Context, client HTTPDoer, endpoint string, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("poll interval must be positive")
	}
	ticker := time.NewTicker(interval)
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

func ParseSpeculativeResponse(value map[string]any, wall, startup float64) (SpeculativeMetrics, error) {
	choices, ok := value["choices"].([]any)
	if !ok || len(choices) == 0 {
		return SpeculativeMetrics{}, fmt.Errorf("llama.cpp response has no choices")
	}
	usage, ok := value["usage"].(map[string]any)
	if !ok {
		return SpeculativeMetrics{}, fmt.Errorf("llama.cpp response has no usage object")
	}
	timings, ok := value["timings"].(map[string]any)
	if !ok {
		return SpeculativeMetrics{}, fmt.Errorf("llama.cpp response has no timings object")
	}
	readInteger := func(source map[string]any, input string) (int64, error) {
		value, err := integer(source[input])
		if err != nil || value < 0 {
			return 0, fmt.Errorf("llama.cpp response has invalid %s", input)
		}
		return value, nil
	}
	readNumber := func(input string) (float64, error) {
		value, err := number(timings[input])
		if err != nil || value <= 0 {
			return 0, fmt.Errorf("llama.cpp response has invalid %s", input)
		}
		return value, nil
	}
	metrics := SpeculativeMetrics{RequestSeconds: wall, StartupSeconds: startup}
	var err error
	if metrics.PromptTokens, err = readInteger(usage, "prompt_tokens"); err != nil {
		return SpeculativeMetrics{}, err
	}
	if metrics.CompletionTokens, err = readInteger(usage, "completion_tokens"); err != nil {
		return SpeculativeMetrics{}, err
	}
	if metrics.DraftedTokens, err = readInteger(timings, "draft_n"); err != nil {
		return SpeculativeMetrics{}, err
	}
	if metrics.AcceptedDraftTokens, err = readInteger(timings, "draft_n_accepted"); err != nil {
		return SpeculativeMetrics{}, err
	}
	if metrics.GenerationTokensPerSecond, err = readNumber("predicted_per_second"); err != nil {
		return SpeculativeMetrics{}, err
	}
	if metrics.PromptTokensPerSecond, err = readNumber("prompt_per_second"); err != nil {
		return SpeculativeMetrics{}, err
	}
	if metrics.PredictedMS, err = readNumber("predicted_ms"); err != nil {
		return SpeculativeMetrics{}, err
	}
	if metrics.PromptMS, err = readNumber("prompt_ms"); err != nil {
		return SpeculativeMetrics{}, err
	}
	if metrics.AcceptedDraftTokens > metrics.DraftedTokens {
		return SpeculativeMetrics{}, fmt.Errorf("llama.cpp accepted more draft tokens than it proposed")
	}
	if metrics.DraftedTokens > 0 {
		metrics.AcceptancePercent = float64(metrics.AcceptedDraftTokens) * 100 / float64(metrics.DraftedTokens)
	}
	identity, _ := json.Marshal(map[string]any{"choices": choices})
	digest := sha256.Sum256(identity)
	metrics.ResponseSHA256 = hex.EncodeToString(digest[:])
	metrics.Message, _ = json.Marshal(choices)
	return metrics, nil
}

func SpeculativeSummary(trials []SpeculativeTrial, incumbent int) SpeculativeSummaryResult {
	type totals struct {
		trials, generated, drafted, accepted int64
		predictedMS, promptTokens, promptMS  float64
	}
	byDepth := map[int]*totals{}
	failed, complete := 0, 0
	for _, trial := range trials {
		if trial.Status != "complete" || trial.Metrics == nil {
			if trial.Status == "failed" {
				failed++
			}
			continue
		}
		complete++
		total := byDepth[trial.Depth]
		if total == nil {
			total = &totals{}
			byDepth[trial.Depth] = total
		}
		total.trials++
		total.generated += trial.Metrics.CompletionTokens
		total.drafted += trial.Metrics.DraftedTokens
		total.accepted += trial.Metrics.AcceptedDraftTokens
		total.predictedMS += trial.Metrics.PredictedMS
		total.promptTokens += float64(trial.Metrics.PromptTokens)
		total.promptMS += trial.Metrics.PromptMS
	}
	depths := map[string]SpeculativeDepthSummary{}
	winner, winnerRate, haveWinner := 0, 0.0, false
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
		depths[fmt.Sprint(depth)] = SpeculativeDepthSummary{CompleteTrials: total.trials, GenerationTokensPerSecond: generationRate, PromptTokensPerSecond: promptRate, DraftedTokens: total.drafted, AcceptedDraftTokens: total.accepted, AcceptancePercent: acceptance}
		if !haveWinner || generationRate > winnerRate || generationRate == winnerRate && depth < winner {
			winner, winnerRate, haveWinner = depth, generationRate, true
		}
	}
	return SpeculativeSummaryResult{CompleteTrials: complete, TotalTrials: len(trials), FailedTrials: failed, Depths: depths, IncumbentDepth: incumbent, WinnerDepth: winner}
}
