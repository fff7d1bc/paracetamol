package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"rocmplete/internal/controlerr"
	"rocmplete/internal/process"
)

const LlamaSchema = "rocmplete.llama-benchmark.v1"

type LlamaRun struct {
	Image       map[string]any
	Profile     string
	Backend     string
	RenderNodes []string
	Model       map[string]any
	Parameters  map[string]any
	Results     []map[string]any
}

func RunLlama(ctx context.Context, runner process.Runner, command []string) ([]map[string]any, error) {
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
	var rows []map[string]any
	if err := decoder.Decode(&rows); err != nil || len(rows) == 0 {
		return nil, controlerr.New("llama-bench returned invalid or empty JSON")
	}
	return rows, nil
}

func WriteLlama(path string, run LlamaRun) error {
	value := map[string]any{
		"schema":       LlamaSchema,
		"created_at":   Timestamp(),
		"application":  "llama-cpp",
		"image":        run.Image,
		"profile":      run.Profile,
		"backend":      run.Backend,
		"render_nodes": run.RenderNodes,
		"gpu_split":    map[bool]string{true: "layer", false: "none"}[len(run.RenderNodes) > 1],
		"model":        run.Model,
		"parameters":   run.Parameters,
		"results":      run.Results,
	}
	return WriteJSON(path, value)
}

func Rates(value map[string]any) (map[string]float64, error) {
	parameters, ok := value["parameters"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("llama benchmark has no parameters")
	}
	prompt, err := integer(parameters["prompt_tokens"])
	if err != nil || prompt < 1 {
		return nil, fmt.Errorf("llama benchmark has invalid prompt token count")
	}
	generation, err := integer(parameters["generation_tokens"])
	if err != nil || generation < 1 {
		return nil, fmt.Errorf("llama benchmark has invalid generation token count")
	}
	rows, ok := value["results"].([]any)
	if !ok {
		return nil, fmt.Errorf("llama benchmark has no results")
	}
	rate := func(wantPrompt, wantGeneration int64) (float64, error) {
		matches := 0
		found := 0.0
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			p, _ := integer(row["n_prompt"])
			g, _ := integer(row["n_gen"])
			if p != wantPrompt || g != wantGeneration {
				continue
			}
			matches++
			found, _ = number(row["avg_ts"])
		}
		if matches != 1 || found <= 0 || math.IsNaN(found) || math.IsInf(found, 0) {
			return 0, fmt.Errorf("llama benchmark lacks one valid pp%d/tg%d row", wantPrompt, wantGeneration)
		}
		return found, nil
	}
	pp, err := rate(prompt, 0)
	if err != nil {
		return nil, err
	}
	tg, err := rate(0, generation)
	if err != nil {
		return nil, err
	}
	return map[string]float64{
		"prompt_tokens_per_second":     pp,
		"generation_tokens_per_second": tg,
		"estimated_inference_seconds":  float64(prompt)/pp + float64(generation)/tg,
	}, nil
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

func ReadLlama(path string) (map[string]any, error) {
	value, err := ReadObject(path)
	if err != nil {
		return nil, err
	}
	if value["schema"] != LlamaSchema || value["application"] != "llama-cpp" {
		return nil, fmt.Errorf("unsupported llama.cpp benchmark result: %s", path)
	}
	return value, nil
}

func FileMetadata(path string) (map[string]any, error) {
	status, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"kind": "local", "path": path, "size": status.Size(), "mtime_ns": status.ModTime().UnixNano()}, nil
}
