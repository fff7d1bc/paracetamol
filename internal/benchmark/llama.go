package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"rocmplete/internal/controlerr"
	"rocmplete/internal/process"
)

const (
	LlamaSchema           = "rocmplete.llama-benchmark.v2"
	LlamaComparisonSchema = "rocmplete.llama-backend-comparison.v2"
)

type ImageIdentity struct {
	Reference string `json:"reference"`
	ID        string `json:"id,omitempty"`
}

type ModelIdentity struct {
	Kind       string `json:"kind,omitempty"`
	Preset     string `json:"preset,omitempty"`
	Path       string `json:"path"`
	Repository string `json:"repository,omitempty"`
	Revision   string `json:"revision,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
	Size       int64  `json:"size,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	MTimeNS    int64  `json:"mtime_ns,omitempty"`
}

type LlamaParameters struct {
	Repetitions      int    `json:"repetitions,omitempty"`
	PromptTokens     int    `json:"prompt_tokens"`
	GenerationTokens int    `json:"generation_tokens"`
	ContextDepth     int    `json:"context_depth,omitempty"`
	BatchSize        int    `json:"batch_size,omitempty"`
	UBatchSize       int    `json:"ubatch_size,omitempty"`
	CacheTypeK       string `json:"cache_type_k,omitempty"`
	CacheTypeV       string `json:"cache_type_v,omitempty"`
	FlashAttention   string `json:"flash_attention,omitempty"`
}

// LlamaResult owns the stable ROCmplete envelope. Results remain RawMessage
// because llama-bench's evidence object is an upstream format, not a control-
// plane state machine that ROCmplete should pretend to own.
type LlamaResult struct {
	Schema      string            `json:"schema"`
	CreatedAt   string            `json:"created_at"`
	Application string            `json:"application"`
	Image       ImageIdentity     `json:"image"`
	Profile     string            `json:"profile"`
	Backend     string            `json:"backend"`
	RenderNodes []string          `json:"render_nodes"`
	GPUSplit    string            `json:"gpu_split"`
	Model       ModelIdentity     `json:"model"`
	Parameters  LlamaParameters   `json:"parameters"`
	Results     []json.RawMessage `json:"results"`
}

type LlamaRun struct {
	Image       ImageIdentity
	Profile     string
	Backend     string
	RenderNodes []string
	Model       ModelIdentity
	Parameters  LlamaParameters
	Results     []json.RawMessage
}

type LlamaRates struct {
	PromptTokensPerSecond     float64 `json:"prompt_tokens_per_second"`
	GenerationTokensPerSecond float64 `json:"generation_tokens_per_second"`
	EstimatedInferenceSeconds float64 `json:"estimated_inference_seconds"`
}

type LlamaBackendResult struct {
	Status string     `json:"status"`
	Result string     `json:"result"`
	Rates  LlamaRates `json:"rates"`
}

type LlamaComparison struct {
	Schema      string                        `json:"schema"`
	CreatedAt   string                        `json:"created_at"`
	Image       ImageIdentity                 `json:"image"`
	Profile     string                        `json:"profile"`
	RenderNodes []string                      `json:"render_nodes"`
	Model       ModelIdentity                 `json:"model"`
	Parameters  LlamaParameters               `json:"parameters"`
	Backends    map[string]LlamaBackendResult `json:"backends"`
	Errors      map[string]string             `json:"errors"`
}

func RunLlama(ctx context.Context, runner process.Runner, command []string) ([]json.RawMessage, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("empty llama-bench command")
	}
	result, err := runner.Run(ctx, process.Command{Name: command[0], Args: command[1:]})
	if err != nil {
		return nil, err
	}
	if result.Status != 0 {
		detail := strings.TrimSpace(string(result.Stderr))
		if detail == "" {
			detail = fmt.Sprintf("exit status %d", result.Status)
		}
		return nil, controlerr.New("llama.cpp benchmark failed: %s", detail)
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Stdout))
	decoder.UseNumber()
	var rows []json.RawMessage
	if err := decoder.Decode(&rows); err != nil || len(rows) == 0 {
		return nil, controlerr.New("llama-bench returned invalid or empty JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, controlerr.New("llama-bench returned trailing JSON data")
	}
	for _, row := range rows {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(row, &object); err != nil || object == nil {
			return nil, controlerr.New("llama-bench returned a non-object result row")
		}
	}
	return rows, nil
}

func WriteLlama(path string, run LlamaRun) error {
	split := "none"
	if len(run.RenderNodes) > 1 {
		split = "layer"
	}
	return WriteJSON(path, LlamaResult{
		Schema: LlamaSchema, CreatedAt: Timestamp(), Application: "llama-cpp",
		Image: run.Image, Profile: run.Profile, Backend: run.Backend,
		RenderNodes: run.RenderNodes, GPUSplit: split, Model: run.Model,
		Parameters: run.Parameters, Results: run.Results,
	})
}

type llamaRateRow struct {
	Prompt int64       `json:"n_prompt"`
	Gen    int64       `json:"n_gen"`
	AvgTS  json.Number `json:"avg_ts"`
}

func Rates(value LlamaResult) (LlamaRates, error) {
	if value.Parameters.PromptTokens < 1 || value.Parameters.GenerationTokens < 1 {
		return LlamaRates{}, fmt.Errorf("llama benchmark has invalid token counts")
	}
	rate := func(wantPrompt, wantGeneration int64) (float64, error) {
		matches := 0
		found := 0.0
		for _, raw := range value.Results {
			var row llamaRateRow
			if err := json.Unmarshal(raw, &row); err != nil || row.Prompt != wantPrompt || row.Gen != wantGeneration {
				continue
			}
			matches++
			found, _ = row.AvgTS.Float64()
		}
		if matches != 1 || found <= 0 || math.IsNaN(found) || math.IsInf(found, 0) {
			return 0, fmt.Errorf("llama benchmark lacks one valid pp%d/tg%d row", wantPrompt, wantGeneration)
		}
		return found, nil
	}
	pp, err := rate(int64(value.Parameters.PromptTokens), 0)
	if err != nil {
		return LlamaRates{}, err
	}
	tg, err := rate(0, int64(value.Parameters.GenerationTokens))
	if err != nil {
		return LlamaRates{}, err
	}
	return LlamaRates{PromptTokensPerSecond: pp, GenerationTokensPerSecond: tg, EstimatedInferenceSeconds: float64(value.Parameters.PromptTokens)/pp + float64(value.Parameters.GenerationTokens)/tg}, nil
}

func integer(value any) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case float64:
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	default:
		return 0, fmt.Errorf("not an integer")
	}
}

func number(value any) (float64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Float64()
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	default:
		return 0, fmt.Errorf("not a number")
	}
}

func ReadLlama(path string) (LlamaResult, error) {
	var value LlamaResult
	if err := ReadJSON(path, &value, true); err != nil {
		return LlamaResult{}, err
	}
	if value.Schema != LlamaSchema || value.Application != "llama-cpp" {
		return LlamaResult{}, fmt.Errorf("unsupported llama.cpp benchmark result: %s", path)
	}
	return value, nil
}

func FileMetadata(path string) (ModelIdentity, error) {
	status, err := os.Stat(path)
	if err != nil {
		return ModelIdentity{}, err
	}
	return ModelIdentity{Kind: "local", Path: path, Size: status.Size(), MTimeNS: status.ModTime().UnixNano()}, nil
}
