package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"paracetamol/internal/identity"
	"paracetamol/internal/textmodel"
)

type fixedLifecycle struct {
	url *url.URL
}

func (fixed fixedLifecycle) Start(context.Context, Allocation) (*url.URL, error) {
	return fixed.url, nil
}
func (fixed fixedLifecycle) Stop(context.Context, Allocation) error { return nil }

func testGatewayServer(t *testing.T, upstream *httptest.Server) (*httptest.Server, *Scheduler, *Server) {
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
	return testServer, scheduler, server
}

func requireGatewayIdentity(t *testing.T, response *http.Response) {
	t.Helper()
	if values := response.Header.Values("Server"); len(values) != 1 || values[0] != identity.CommandName+"/"+identity.Version {
		t.Fatalf("Server headers=%q", values)
	}
	if values := response.Header.Values(IdentityHeader); len(values) != 1 || values[0] != IdentityValue {
		t.Fatalf("%s headers=%q", IdentityHeader, values)
	}
}

func TestServerListsExactRegistryAndRejectsUnknownModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer upstream.Close()
	server, _, _ := testGatewayServer(t, upstream)
	response, err := http.Get(server.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	requireGatewayIdentity(t, response)
	contents, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !bytes.Contains(contents, []byte(`"id":"fixture"`)) {
		t.Fatalf("status=%d body=%s", response.StatusCode, contents)
	}
	response, err = http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"unknown","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	requireGatewayIdentity(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", response.StatusCode)
	}
	response, err = http.Get(server.URL + "/unknown")
	if err != nil {
		t.Fatal(err)
	}
	requireGatewayIdentity(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route status=%d", response.StatusCode)
	}
}

func TestServerPreservesUnknownRequestFields(t *testing.T) {
	seen := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		seen <- body
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Server", "llama.cpp")
		writer.Header().Set(IdentityHeader, "foreign.gateway")
		_, _ = writer.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()
	server, _, _ := testGatewayServer(t, upstream)
	body := []byte(`{"model":"fixture","custom":{"number":1.2300},"messages":[{"role":"user","content":"hello"}]}`)
	response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	requireGatewayIdentity(t, response)
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
	server, _, _ := testGatewayServer(t, upstream)
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
	server, scheduler, handler := testGatewayServer(t, upstream)
	var logs bytes.Buffer
	handler.log = NewAtomicLogWriter(&logs)
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
	deadline := time.Now().Add(2 * time.Second)
	for len(handler.requests.Recent(1)) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	recent := handler.requests.Recent(1)
	if len(recent) != 1 || recent[0].Outcome != OutcomeCanceled {
		status, _ := scheduler.Status(context.Background())
		t.Fatalf("recent=%#v status=%#v logs=%s", recent, status, logs.String())
	}
}

func TestStatusRedactsLifecycleErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hijacker := writer.(http.Hijacker)
		connection, _, _ := hijacker.Hijack()
		connection.Close()
	}))
	defer upstream.Close()
	server, _, handler := testGatewayServer(t, upstream)
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
	recent := handler.requests.Recent(1)
	if len(recent) != 1 || recent[0].Outcome != OutcomeUpstreamError || recent[0].HTTPStatus != http.StatusBadGateway {
		t.Fatalf("recent=%#v", recent)
	}
}

func TestServerCorrelatesRequestsAndExposesPrivacySafeRecentMetadata(t *testing.T) {
	responseBody := []byte(`{"id":"chatcmpl-fixture","secret_response":"not retained","usage":{"prompt_tokens":19,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":11},"completion_tokens_details":{"reasoning_tokens":3}}}`)
	seenBody := make(chan []byte, 1)
	seenSessionHeader := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/models":
			_, _ = io.WriteString(writer, `{"data":[{"id":"fixture","path":"/secret/model.gguf","status":{"value":"loaded","args":["--secret"]}}]}`)
		case "/v1/chat/completions":
			body, _ := io.ReadAll(request.Body)
			seenBody <- body
			seenSessionHeader <- request.Header.Get(SessionIDHeader)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(responseBody)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	scheduler, err := NewScheduler(fixedLifecycle{url: parsed}, QueueLimit, nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := Registry{
		Models:      []Model{{ID: "fixture", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP}},
		Fingerprint: "fixture",
		byID:        map[string]Model{"fixture": {ID: "fixture", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP}},
	}
	var logs bytes.Buffer
	handler, err := NewServer(registry, scheduler, &logs)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer func() {
		server.Close()
		_ = scheduler.Shutdown(context.Background())
	}()
	payload := []byte(`{"model":"fixture","stream":false,"reasoning_effort":"medium","temperature":0.7,"messages":[{"role":"user","content":"secret prompt"}],"tools":[{"secret":"tool schema"}]}`)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", bytes.NewReader(payload))
	request.Header.Set("User-Agent", "pi/fixture")
	request.Header.Set(SessionIDHeader, "019fe5cc-5cad-7a92-aead-f0838931fb95")
	request.Header.Set("Authorization", "Bearer secret-token")
	request.Header.Set("X-Forwarded-For", "203.0.113.77")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	returned, _ := io.ReadAll(response.Body)
	response.Body.Close()
	requestID := response.Header.Get(RequestIDHeader)
	if requestID == "" || len(response.Header.Values(RequestIDHeader)) != 1 || !bytes.Equal(returned, responseBody) {
		t.Fatalf("request-id=%q response=%s", requestID, returned)
	}
	if got := <-seenBody; !bytes.Equal(got, payload) {
		t.Fatalf("request body changed:\n got %s\nwant %s", got, payload)
	}
	if got := <-seenSessionHeader; got != "" {
		t.Fatalf("internal session header reached upstream: %q", got)
	}

	statusResponse, err := http.Get(server.URL + "/paracetamol/v1/status?requests=1")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResponse.Body.Close()
	var status Status
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Schema != StatusSchema || len(status.RecentRequests) != 1 || status.RecentRequests[0].ID != requestID {
		t.Fatalf("status=%#v", status)
	}
	record := status.RecentRequests[0]
	if record.Outcome != OutcomeSucceeded || record.HTTPStatus != http.StatusOK || record.Application != "llama-cpp" || record.UserAgent != "pi/fixture" || record.SessionID != "019fe5cc-5cad-7a92-aead-f0838931fb95" {
		t.Fatalf("record=%#v", record)
	}
	if record.Peer != "127.0.0.1" {
		t.Fatalf("gateway trusted a forwarded peer instead of the TCP peer: %q", record.Peer)
	}
	assertTokens(t, record.Tokens, 19, 4, 11)
	if record.Tokens.Reasoning == nil || *record.Tokens.Reasoning != 3 {
		t.Fatalf("reasoning tokens=%#v", record.Tokens)
	}
	if status.Usage.Overall.Requests != 1 || status.Usage.Overall.Tokens.Reasoning.Total != 3 || len(status.Usage.Models) != 1 || len(status.Usage.Sessions) != 1 {
		t.Fatalf("usage=%#v", status.Usage)
	}
	if len(status.Models) != 1 || status.Models[0].State != ModelLoaded || status.Models[0].Diagnostic != "" {
		t.Fatalf("model status=%#v", status.Models)
	}
	encoded, _ := json.Marshal(status)
	for _, secret := range []string{"secret prompt", "tool schema", "not retained", "/secret/model.gguf", "--secret", "secret-token", "203.0.113.77"} {
		if bytes.Contains(encoded, []byte(secret)) || strings.Contains(logs.String(), secret) {
			t.Fatalf("observability leaked %q:\nstatus=%s\nlogs=%s", secret, encoded, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "gateway | request "+requestID+" start") || !strings.Contains(logs.String(), "gateway | request "+requestID+" finish") {
		t.Fatalf("correlated logs missing:\n%s", logs.String())
	}
}

func TestServerOmitsRecentRequestsUnlessRequestedAndValidatesQuery(t *testing.T) {
	const sessionID = "019fe5cc-5cad-7a92-aead-f0838931fb95"
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"choices":[]}`)
	}))
	defer upstream.Close()
	server, _, _ := testGatewayServer(t, upstream)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"private-model-name","messages":[{"content":"private"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(SessionIDHeader, sessionID)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	requestID := response.Header.Get(RequestIDHeader)
	response.Body.Close()
	if requestID == "" {
		t.Fatal("rejected request lacked correlation header")
	}

	response, err = http.Get(server.URL + "/paracetamol/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if bytes.Contains(contents, []byte("recent_requests")) || bytes.Contains(contents, []byte("private")) || bytes.Contains(contents, []byte(sessionID)) {
		t.Fatalf("concise status exposed request history: %s", contents)
	}

	response, err = http.Get(server.URL + "/paracetamol/v1/status?requests=1")
	if err != nil {
		t.Fatal(err)
	}
	var status Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(status.RecentRequests) != 1 || status.RecentRequests[0].ID != requestID || status.RecentRequests[0].Outcome != OutcomeRejected {
		t.Fatalf("recent requests=%#v", status.RecentRequests)
	}
	if len(status.Usage.Sessions) != 1 || status.Usage.Sessions[0].ID != sessionID {
		t.Fatalf("session usage=%#v", status.Usage.Sessions)
	}
	if status.RecentRequests[0].Model != "" {
		t.Fatalf("rejected unknown model was retained: %#v", status.RecentRequests[0])
	}
	for _, query := range []string{"requests=0", "requests=65", "requests=1&requests=2", "other=1"} {
		response, err = http.Get(server.URL + "/paracetamol/v1/status?" + query)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("query=%q status=%d", query, response.StatusCode)
		}
	}
}

func TestServerRecordsBackendStartFailure(t *testing.T) {
	lifecycle := &fakeLifecycle{startErr: map[Allocation]error{AllocationLlamaCPP: errors.New("private start detail")}}
	scheduler, err := NewScheduler(lifecycle, QueueLimit, nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := Registry{
		Models: []Model{{ID: "fixture", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP}},
		byID:   map[string]Model{"fixture": {ID: "fixture", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP}},
	}
	var logs bytes.Buffer
	handler, _ := NewServer(registry, scheduler, &logs)
	server := httptest.NewServer(handler)
	defer func() {
		server.Close()
		_ = scheduler.Shutdown(context.Background())
	}()
	response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"fixture","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.StatusCode)
	}
	recent := handler.requests.Recent(1)
	if len(recent) != 1 || recent[0].Outcome != OutcomeBackendStartFailed || recent[0].HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("recent=%#v", recent)
	}
	if !strings.Contains(logs.String(), "backend start") || !strings.Contains(logs.String(), "finish outcome=backend-start-failed") {
		t.Fatalf("logs=%s", logs.String())
	}
}

func TestServerRecordsQueueFullAndQueuedCancellation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"choices":[]}`)
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	scheduler, err := NewScheduler(fixedLifecycle{url: parsed}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	active, err := scheduler.Acquire(context.Background(), AllocationLlamaCPP)
	if err != nil {
		t.Fatal(err)
	}
	registry := Registry{
		Models: []Model{
			{ID: "llama", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP},
			{ID: "dwarf", Application: "dwarfstar", Backend: textmodel.BackendDwarfStar},
		},
		byID: map[string]Model{
			"llama": {ID: "llama", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP},
			"dwarf": {ID: "dwarf", Application: "dwarfstar", Backend: textmodel.BackendDwarfStar},
		},
	}
	handler, _ := NewServer(registry, scheduler, io.Discard)
	server := httptest.NewServer(handler)
	defer func() {
		server.Close()
		active.Release()
		_ = scheduler.Shutdown(context.Background())
	}()

	queuedContext, cancelQueued := context.WithCancel(context.Background())
	queuedRequest, _ := http.NewRequestWithContext(queuedContext, http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"dwarf","messages":[]}`))
	queuedResult := make(chan error, 1)
	go func() {
		response, requestErr := http.DefaultClient.Do(queuedRequest)
		if response != nil {
			response.Body.Close()
		}
		queuedResult <- requestErr
	}()
	waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.QueuedRequests == 1 })

	response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"dwarf","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("queue-full status=%d", response.StatusCode)
	}
	recent := handler.requests.Recent(1)
	if len(recent) != 1 || recent[0].Outcome != OutcomeQueueFull {
		t.Fatalf("recent=%#v", recent)
	}

	cancelQueued()
	select {
	case <-queuedResult:
	case <-time.After(time.Second):
		t.Fatal("queued request did not observe cancellation")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		recent = handler.requests.Recent(2)
		if len(recent) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(recent) != 2 || recent[0].Outcome != OutcomeCanceled {
		t.Fatalf("recent after cancellation=%#v", recent)
	}
}
