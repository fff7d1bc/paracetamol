package benchmark

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestComfyTerminalPromptFailuresReturnWithoutPollingAgain(t *testing.T) {
	for _, test := range []struct {
		name, status, want string
	}{
		{"execution error", `{"status_str":"error","completed":false,"messages":[["execution_error",{"exception_type":"OutOfMemoryError","exception_message":"allocation failed\nretry elsewhere\u001b","traceback":["private traceback"],"current_inputs":{"private":"input"}}]]}`, "OutOfMemoryError: allocation failed retry elsewhere"},
		{"interrupted", `{"status_str":"error","completed":false,"messages":[["execution_interrupted",{}]]}`, "execution interrupted"},
		{"missing details", `{"status_str":"error","completed":false}`, "failed"},
		{"malformed details", `{"status_str":"error","completed":false,"messages":[{},["execution_error",[]]]}`, "failed"},
		{"contradictory completion", `{"status_str":"error","completed":true}`, "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			client := doerFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					cancel() // Bound the old infinite-poll failure without a deadline.
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"queued":{"status":` + test.status + `}}`))}, nil
			})
			result, err := WaitForPromptWithClient(ctx, client, "http://example.invalid", "queued", time.Nanosecond)
			if result != nil || err == nil || errors.Is(err, context.Canceled) || calls != 1 || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("result=%v err=%v calls=%d", result, err, calls)
			}
			for _, private := range []string{"private traceback", "private", "\n", "\x1b"} {
				if strings.Contains(err.Error(), private) {
					t.Fatalf("failure leaked private/unprintable detail: %q", err)
				}
			}
		})
	}
}

func TestComfyPromptPollingWaitsForExplicitCompletion(t *testing.T) {
	for _, pending := range []string{`{}`, `{"queued":{}}`, `{"queued":{"status":{"completed":false,"status_str":"running"}}}`} {
		t.Run(pending, func(t *testing.T) {
			calls := 0
			client := doerFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				body := pending
				if calls > 1 {
					body = `{"queued":{"status":{"completed":true,"status_str":"success"},"outputs":{"answer":"ready"}}}`
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			result, err := WaitForPromptWithClient(context.Background(), client, "http://example.invalid", "queued", time.Nanosecond)
			if err != nil || calls != 2 || result["outputs"] == nil {
				t.Fatalf("result=%v err=%v calls=%d", result, err, calls)
			}
		})
	}
}

func TestComfyPendingPromptRemainsCancellable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := doerFunc(func(request *http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	if _, err := WaitForPromptWithClient(ctx, client, "http://example.invalid", "queued", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("pending prompt lost cancellation: %v", err)
	}
}

func TestComfyFailureDiagnosticIsBounded(t *testing.T) {
	status := map[string]any{"messages": []any{[]any{"execution_error", map[string]any{"exception_type": "Error", "exception_message": strings.Repeat("界", 2000)}}}}
	if detail := comfyPromptFailureDetail(status); len(detail) > 1030 || !strings.HasSuffix(detail, "...") {
		t.Fatalf("unbounded diagnostic: %d bytes", len(detail))
	}
}
