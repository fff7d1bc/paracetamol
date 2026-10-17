package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paracetamol/internal/hostdoctor"
)

func TestStatisticsDaysRejectsAmbiguousOrUnknownQueries(t *testing.T) {
	for _, test := range []struct {
		query string
		days  int
	}{{"", 7}, {"days=1", 1}, {"days=7", 7}, {"days=30", 30}} {
		if days, err := statisticsDays(test.query); err != nil || days != test.days {
			t.Fatalf("query=%q days=%d err=%v", test.query, days, err)
		}
	}
	for _, query := range []string{"days=", "days=0", "days=90", "days=01", "days=7&days=7", "days=7&other=1", "other=1", "days=%zz", "days=7;other=1"} {
		if _, err := statisticsDays(query); err == nil {
			t.Fatalf("accepted %q", query)
		}
	}
}

func TestStatisticsHTTPProtectedReadOnlyAndUnavailablePrivacy(t *testing.T) {
	var backendCalls, resourceCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { backendCalls.Add(1) }))
	defer upstream.Close()
	_, scheduler, handler := testGatewayServer(t, upstream)
	handler.resources = func() hostdoctor.ResourceSnapshot { resourceCalls.Add(1); return hostdoctor.ResourceSnapshot{} }
	const key = "statistics-fixture-key-at-least-32-characters"
	auth, err := newBearerAuth(key)
	if err != nil {
		t.Fatal(err)
	}
	handler.auth = auth
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/paracetamol/v1/statistics", nil))
	if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthenticated=%d %s", response.Code, response.Body)
	}
	request := httptest.NewRequest(http.MethodGet, "/paracetamol/v1/statistics?days=7&days=30", nil)
	request.Header.Set("Authorization", "Bearer "+key)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid query=%d %s", response.Code, response.Body)
	}
	request = httptest.NewRequest(http.MethodGet, "/paracetamol/v1/statistics", nil)
	request.Header.Set("Authorization", "Bearer "+key)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"disabled"`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("disabled=%d %s", response.Code, response.Body)
	}
	privatePath := filepath.Join(t.TempDir(), "host-private", "usage.sqlite")
	if err := os.WriteFile(filepath.Dir(privatePath), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler.stats = NewStatistics(StatisticsOptions{Enabled: true, Path: privatePath, RetentionDays: 90, Log: io.Discard})
	t.Cleanup(func() { _ = handler.stats.Close() })
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "statistics_unavailable") || strings.Contains(response.Body.String(), privatePath) || strings.Contains(response.Body.String(), "host-private") {
		t.Fatalf("unavailable=%d %s", response.Code, response.Body)
	}
	state, err := scheduler.Status(context.Background())
	if err != nil || state.State != StateUnloaded || backendCalls.Load() != 0 || resourceCalls.Load() != 0 || len(handler.requests.Recent(10)) != 0 {
		t.Fatalf("statistics read mutated service: state=%+v err=%v backend=%d resources=%d", state, err, backendCalls.Load(), resourceCalls.Load())
	}
}

func TestStatisticsHTTPPersistsOnlyKnownModelDailyTotalsAfterDrain(t *testing.T) {
	const privateText = "private-prompt-response-user-agent-session-peer"
	var backendCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		backendCalls.Add(1)
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), `"stream":true`) {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\""+privateText+"\"}}]}\n\ndata: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n")
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"`+privateText+`"}}],"usage":{"prompt_tokens":20,"completion_tokens":8}}`)
	}))
	defer upstream.Close()
	_, _, handler := testGatewayServer(t, upstream)
	path := filepath.Join(t.TempDir(), "statistics", "usage.sqlite")
	options := StatisticsOptions{Enabled: true, Path: path, RetentionDays: 90, Log: io.Discard}
	handler.stats = NewStatistics(options)
	t.Cleanup(func() { _ = handler.stats.Close() })
	for _, body := range []string{
		`{"model":"fixture","messages":[{"role":"user","content":"` + privateText + `"}]}`,
		`{"model":"fixture","stream":true,"messages":[]}`,
		`{"model":"arbitrary-model-` + privateText + `","messages":[]}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		request.Header.Set("User-Agent", privateText)
		request.Header.Set("X-Paracetamol-Session", privateText)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK && response.Code != http.StatusNotFound {
			t.Fatalf("inference=%d %s", response.Code, response.Body)
		}
	}
	if err := handler.stats.Close(); err != nil {
		t.Fatal(err)
	}
	handler.stats = NewStatistics(options)
	for _, query := range []string{"", "?days=1", "?days=7", "?days=30"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/paracetamol/v1/statistics"+query, nil))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("statistics=%d %s", response.Code, response.Body)
		}
		var snapshot struct {
			Overall struct {
				Requests uint64          `json:"requests"`
				Input    AggregateMetric `json:"input"`
				Output   AggregateMetric `json:"output"`
			} `json:"overall"`
			Models []struct {
				Model       string `json:"model"`
				Application string `json:"application"`
			} `json:"models"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.Overall.Requests != 2 || snapshot.Overall.Input.Total != 30 || snapshot.Overall.Input.Observations != 2 || snapshot.Overall.Output.Total != 12 || len(snapshot.Models) != 1 || snapshot.Models[0].Model != "fixture" || snapshot.Models[0].Application != "llama-cpp" {
			t.Fatalf("snapshot=%s", response.Body)
		}
		for _, forbidden := range []string{privateText, path, "arbitrary-model", `"sessions"`, `"peer"`, `"user_agent"`, `"recent_requests"`, `"controls"`, `"request_id"`} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatalf("statistics leaked %q: %s", forbidden, response.Body)
			}
		}
	}
	if backendCalls.Load() != 2 || len(handler.requests.Recent(10)) != 3 {
		t.Fatalf("backend=%d ledger=%d", backendCalls.Load(), len(handler.requests.Recent(10)))
	}
}

func TestStatisticsCloseAfterHTTPDrainIncludesFinalRequest(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-finish
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	}))
	defer upstream.Close()
	server, _, handler := testGatewayServer(t, upstream)
	options := StatisticsOptions{Enabled: true, Path: filepath.Join(t.TempDir(), "statistics", "usage.sqlite"), RetentionDays: 90, Log: io.Discard}
	handler.stats = NewStatistics(options)
	t.Cleanup(func() { _ = handler.stats.Close() })
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"fixture"}`))
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		requestDone <- err
	}()
	<-entered
	handler.BeginShutdown()
	shutdownDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { shutdownDone <- server.Config.Shutdown(ctx) }()
	close(finish)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
	if err := handler.stats.Close(); err != nil {
		t.Fatal(err)
	}
	handler.stats = NewStatistics(options)
	snapshot, err := handler.stats.Snapshot(context.Background(), 1)
	if err != nil || snapshot.Overall.Requests != 1 || snapshot.Overall.Output.Total != 2 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
