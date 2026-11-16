package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerStripsNativeReplayHeaderAndCancelsBackendWork(t *testing.T) {
	for _, header := range []string{"", "X-Conversation-ID", "x-conversation-id", "x-CoNvErSaTiOn-Id"} {
		name := header
		if name == "" {
			name = "absent"
		}
		t.Run(name, func(t *testing.T) {
			const sessionID = "00000000-0000-4000-8000-00000000000a"
			const body = `{"model":"fixture","stream":true,"messages":[{"role":"user","content":"fixture"}],"cache_prompt":true,"custom":{"number":1.2300}}`
			type forwardedRequest struct {
				header http.Header
				body   []byte
				err    error
			}
			seen := make(chan forwardedRequest, 1)
			stopped := make(chan struct{})
			release := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				contents, err := io.ReadAll(request.Body)
				seen <- forwardedRequest{request.Header.Clone(), contents, err}
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"fixture\"}}]}\n\n")
				writer.(http.Flusher).Flush()
				// Model the pinned native replay lifecycle: a conversation ID
				// opts out of disconnect cancellation until explicitly stopped.
				if request.Header.Get("X-Conversation-ID") != "" {
					<-release
				} else {
					select {
					case <-request.Context().Done():
					case <-release:
					}
				}
				close(stopped)
			}))
			defer func() {
				close(release)
				upstream.Close()
			}()
			server, scheduler, handler := testGatewayServer(t, upstream)
			handler.auth, _ = newBearerAuth(testAPIKey)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if header != "" {
				// Preserve the supplied spelling on the actual HTTP request;
				// Header.Set would canonicalize before the gateway receives it.
				request.Header[header] = []string{"native-replay-session"}
			}
			request.Header.Set(SessionIDHeader, sessionID)
			request.Header.Set("X-Session-Affinity", sessionID)
			request.Header.Set("X-Session-ID", sessionID)
			request.Header.Set("Authorization", "Bearer "+testAPIKey)
			request.Header.Set("Proxy-Authorization", "must-not-forward")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", response.StatusCode)
			}
			forwarded := <-seen
			if forwarded.err != nil || !bytes.Equal(forwarded.body, []byte(body)) {
				t.Fatalf("forwarded body=%s err=%v", forwarded.body, forwarded.err)
			}
			for _, consumed := range []string{"X-Conversation-ID", SessionIDHeader, "Authorization", "Proxy-Authorization"} {
				if values := forwarded.header.Values(consumed); len(values) != 0 {
					t.Errorf("consumed header %s reached backend: %q", consumed, values)
				}
			}
			for _, preserved := range []string{"X-Session-Affinity", "X-Session-ID"} {
				if got := forwarded.header.Get(preserved); got != sessionID {
					t.Errorf("session header %s=%q", preserved, got)
				}
			}
			cancel()
			response.Body.Close()
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("backend work survived downstream cancellation")
			}
			waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.ActiveRequests == 0 })
			deadline := time.Now().Add(2 * time.Second)
			for len(handler.requests.Recent(1)) == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			recent := handler.requests.Recent(1)
			if len(recent) != 1 || recent[0].Outcome != OutcomeCanceled || recent[0].SessionID != sessionID {
				t.Fatalf("recent=%+v", recent)
			}
		})
	}
}

func TestServerDoesNotExposeNativeReplayLifecycle(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("native replay route reached backend")
	}))
	defer upstream.Close()
	server, _, _ := testGatewayServer(t, upstream)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		request, err := http.NewRequest(method, server.URL+"/v1/stream?conversation_id=fixture", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Conversation-ID", "fixture")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s /v1/stream status=%d", method, response.StatusCode)
		}
	}
}
