package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"paracetamol/internal/textmodel"
)

type fixedLifecycle struct {
	url *url.URL
}

func (fixed fixedLifecycle) Start(context.Context, Allocation) (*url.URL, error) {
	return fixed.url, nil
}
func (fixed fixedLifecycle) Stop(context.Context, Allocation) error { return nil }

func testGatewayServer(t *testing.T, upstream *httptest.Server) (*httptest.Server, *Scheduler) {
	t.Helper()
	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := NewScheduler(fixedLifecycle{url: parsed}, QueueLimit, map[Allocation]int{AllocationDwarfStar: 1})
	if err != nil {
		t.Fatal(err)
	}
	registry := Registry{Models: []Model{{ID: "fixture", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP}}, Fingerprint: "fixture", byID: map[string]Model{"fixture": {ID: "fixture", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP}}}
	server, err := NewServer(registry, scheduler, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server)
	t.Cleanup(func() {
		testServer.Close()
		_ = scheduler.Shutdown(context.Background())
	})
	return testServer, scheduler
}

func TestServerListsExactRegistryAndRejectsUnknownModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer upstream.Close()
	server, _ := testGatewayServer(t, upstream)
	response, err := http.Get(server.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !bytes.Contains(contents, []byte(`"id":"fixture"`)) {
		t.Fatalf("status=%d body=%s", response.StatusCode, contents)
	}
	response, err = http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"unknown","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestServerPreservesUnknownRequestFields(t *testing.T) {
	seen := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		seen <- body
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()
	server, _ := testGatewayServer(t, upstream)
	body := []byte(`{"model":"fixture","custom":{"number":1.2300},"messages":[{"role":"user","content":"hello"}]}`)
	response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := <-seen; !bytes.Equal(got, body) {
		t.Fatalf("body changed:\n got %s\nwant %s", got, body)
	}
}

func TestServerFlushesStreamingResponses(t *testing.T) {
	finish := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: first\n\n")
		writer.(http.Flusher).Flush()
		<-finish
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	server, _ := testGatewayServer(t, upstream)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"fixture","stream":true,"messages":[]}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	line := make(chan string, 1)
	go func() { value, _ := reader.ReadString('\n'); line <- value }()
	select {
	case value := <-line:
		if value != "data: first\n" {
			t.Fatalf("line=%q", value)
		}
	case <-time.After(time.Second):
		t.Fatal("gateway buffered the first SSE event")
	}
	close(finish)
	response.Body.Close()
}

func TestServerPropagatesCancellationAndReleasesLease(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		once.Do(func() { close(started) })
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: started\n\n")
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()
	server, scheduler := testGatewayServer(t, upstream)
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"fixture","messages":[]}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	response.Body.Close()
	<-cancelled
	waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.ActiveRequests == 0 })
}

func TestStatusRedactsLifecycleErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hijacker := writer.(http.Hijacker)
		connection, _, _ := hijacker.Hijack()
		connection.Close()
	}))
	defer upstream.Close()
	server, _ := testGatewayServer(t, upstream)
	response, _ := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"fixture","messages":[]}`))
	if response != nil {
		response.Body.Close()
	}
	statusResponse, err := http.Get(server.URL + "/paracetamol/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResponse.Body.Close()
	var status Status
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(status.Scheduler.LastError, "EOF") || strings.Contains(status.Scheduler.LastError, upstream.URL) {
		t.Fatalf("status leaked lifecycle detail: %#v", status.Scheduler)
	}
}
