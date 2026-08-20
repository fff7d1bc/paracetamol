package benchmark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rocmplete/internal/process"
)

type resultRunner struct{ result process.Result }

func (runner resultRunner) Run(context.Context, process.Command) (process.Result, error) {
	return runner.result, nil
}

func (resultRunner) LookPath(string) (string, error) { return "", nil }

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func TestWriteJSONDoesNotReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := WriteJSON(path, map[string]any{"answer": 42}); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(path, map[string]any{"answer": 7}); err == nil {
		t.Fatal("second write replaced append-only result")
	}
	contents, _ := os.ReadFile(path)
	if string(contents) != "{\n  \"answer\": 42\n}\n" {
		t.Fatalf("unexpected result: %s", contents)
	}
}

func TestNewCheckpointDoesNotReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := WriteNewCheckpoint(path, map[string]int{"value": 1}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewCheckpoint(path, map[string]int{"value": 2}); err == nil {
		t.Fatal("new checkpoint replaced existing state")
	}
	if err := WriteCheckpoint(path, map[string]int{"value": 3}); err != nil {
		t.Fatal(err)
	}
}

func TestReadJSONStrictRejectsUnknownAndTrailingData(t *testing.T) {
	type document struct {
		Schema string `json:"schema"`
	}
	for name, contents := range map[string]string{
		"unknown":  `{"schema":"v1","extra":true}`,
		"trailing": `{"schema":"v1"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "result.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			var value document
			if err := ReadJSON(path, &value, true); err == nil {
				t.Fatal("invalid resumable state was accepted")
			}
		})
	}
}

func TestLlamaRatesRequireUniquePromptAndGenerationRows(t *testing.T) {
	result := LlamaResult{
		Parameters: LlamaParameters{PromptTokens: 32, GenerationTokens: 16},
		Results: []json.RawMessage{
			json.RawMessage(`{"n_prompt":32,"n_gen":0,"avg_ts":64}`),
			json.RawMessage(`{"n_prompt":0,"n_gen":16,"avg_ts":8}`),
		},
	}
	rates, err := Rates(result)
	if err != nil {
		t.Fatal(err)
	}
	if rates.PromptTokensPerSecond != 64 || rates.GenerationTokensPerSecond != 8 || rates.EstimatedInferenceSeconds != 2.5 {
		t.Fatalf("unexpected rates: %#v", rates)
	}
	result.Results = append(result.Results, json.RawMessage(`{"n_prompt":0,"n_gen":16,"avg_ts":9}`))
	if _, err := Rates(result); err == nil {
		t.Fatal("duplicate generation row was accepted")
	}
}

func TestRunLlamaRejectsTrailingJSON(t *testing.T) {
	runner := resultRunner{result: process.Result{Stdout: []byte(`[{"n_prompt":1}] {}`)}}
	if _, err := RunLlama(context.Background(), runner, []string{"llama-bench"}); err == nil {
		t.Fatal("trailing llama-bench JSON was accepted")
	}
}

func TestSpeculativeSummaryCountsOnlyCompleteTrials(t *testing.T) {
	trials := []SpeculativeTrial{
		{Depth: 4, Status: "complete", Metrics: &SpeculativeMetrics{CompletionTokens: 10, PredictedMS: 1000}},
		{Depth: 4, Status: "pending"},
		{Depth: 8, Status: "failed"},
	}
	summary := SpeculativeSummary(trials, 4)
	if summary.CompleteTrials != 1 || summary.FailedTrials != 1 || summary.TotalTrials != 3 || summary.WinnerDepth != 4 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestParseSpeculativeResponseRequiresChoices(t *testing.T) {
	response := map[string]any{
		"usage": map[string]any{"prompt_tokens": json.Number("1"), "completion_tokens": json.Number("1")},
		"timings": map[string]any{
			"draft_n": json.Number("1"), "draft_n_accepted": json.Number("1"),
			"predicted_per_second": json.Number("1"), "prompt_per_second": json.Number("1"),
			"predicted_ms": json.Number("1"), "prompt_ms": json.Number("1"),
		},
	}
	if _, err := ParseSpeculativeResponse(response, 1, 1); err == nil {
		t.Fatal("response without choices was accepted")
	}
	response["choices"] = []any{map[string]any{"message": map[string]any{"content": "ok"}}}
	if _, err := ParseSpeculativeResponse(response, 1, 1); err != nil {
		t.Fatal(err)
	}
}

func TestPostJSONUsesSubstitutableHTTPBoundary(t *testing.T) {
	client := doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.Header)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})
	value, err := PostJSONWithClient(context.Background(), client, "http://example.invalid/v1", map[string]int{"value": 1}, 1024)
	if err != nil || value["ok"] != true {
		t.Fatalf("value=%v err=%v", value, err)
	}
}
