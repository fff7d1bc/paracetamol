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

func TestBackendPollingStopsOnDeathAndKeepsLiveLoadingUnbounded(t *testing.T) {
	for _, kind := range []string{"server", "health", "prompt"} {
		for _, outcome := range []string{"ready", "dead", "canceled"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				checks, requests := 0, 0
				dead := errors.New("captured backend exited")
				check := func(context.Context) error {
					checks++
					if checks == 4 && outcome == "dead" {
						return dead
					}
					if checks == 4 && outcome == "canceled" {
						cancel()
						return errors.New("metadata probe interrupted")
					}
					return nil
				}
				client := doerFunc(func(*http.Request) (*http.Response, error) {
					requests++
					status, body := http.StatusServiceUnavailable, `{}`
					if requests == 4 {
						status = http.StatusOK
						if kind == "prompt" {
							body = `{"queued":{"status":{"completed":true}}}`
						}
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
				})
				var err error
				switch kind {
				case "server":
					_, err = waitForServer(ctx, client, "http://example.invalid", time.Nanosecond, check)
				case "health":
					err = waitForURL(ctx, client, "http://example.invalid/health", time.Nanosecond, check)
				case "prompt":
					_, err = waitForPrompt(ctx, client, "http://example.invalid", "queued", time.Nanosecond, check)
				}
				if checks != 4 || (outcome == "ready" && (err != nil || requests != 4)) || (outcome == "dead" && (!errors.Is(err, dead) || requests != 3)) || (outcome == "canceled" && (!errors.Is(err, context.Canceled) || requests != 3)) {
					t.Fatalf("checks=%d requests=%d err=%v", checks, requests, err)
				}
			})
		}
	}
}
