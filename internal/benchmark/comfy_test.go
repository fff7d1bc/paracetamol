package benchmark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"rocmplete/internal/catalog"
)

func TestEveryCatalogBenchmarkLoadsAndMatches(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for identifier, spec := range managed.Benchmarks {
		t.Run(identifier, func(t *testing.T) {
			if _, err := LoadPrompt(root, spec); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQueuePromptUsesSubstitutableHTTPBoundary(t *testing.T) {
	client := doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/prompt" || request.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"prompt_id":"queued"}`))}, nil
	})
	identifier, err := QueuePromptWithClient(context.Background(), client, "http://example.invalid", map[string]any{"node": true})
	if err != nil || identifier != "queued" {
		t.Fatalf("identifier=%q err=%v", identifier, err)
	}
}

func TestSuiteReportsReplaceOnlyWhenExplicitlyOwned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suite.json")
	suite := ComfySuite{SuiteID: "one", Status: "running"}
	if _, err := WriteSuiteReports(path, suite, "markdown", "", false); err != nil {
		t.Fatal(err)
	}
	suite.Status = "complete"
	if _, err := WriteSuiteReports(path, suite, "markdown", "", false); err == nil {
		t.Fatal("create-only report was replaced")
	}
	if _, err := WriteSuiteReports(path, suite, "markdown", "", true); err != nil {
		t.Fatal(err)
	}
}

func TestPreparePromptDoesNotMutateSource(t *testing.T) {
	source := map[string]any{"1": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"seed": json.Number("9"), "filename_prefix": "old"}}}
	prompt, input, err := PreparePrompt(source, 42, "new")
	if err != nil {
		t.Fatal(err)
	}
	if input {
		t.Fatal("unexpected input")
	}
	if source["1"].(map[string]any)["inputs"].(map[string]any)["filename_prefix"] != "old" {
		t.Fatal("source mutated")
	}
	inputs := prompt["1"].(map[string]any)["inputs"].(map[string]any)
	if inputs["filename_prefix"] != "new" || inputs["seed"] != int64(42) {
		t.Fatalf("prompt was not prepared: %#v", inputs)
	}
}
