package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"paracetamol/internal/hostdoctor"
)

const testAPIKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type unreadableAuthBody struct{}

func (unreadableAuthBody) Read([]byte) (int, error) { panic("unauthenticated request body read") }
func (unreadableAuthBody) Close() error             { return nil }

func TestBearerGateProtectsAllRoutesBeforeWork(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unauthenticated backend reached") }))
	defer upstream.Close()
	_, scheduler, server := testGatewayServer(t, upstream)
	server.auth, _ = newBearerAuth(testAPIKey)
	server.resources = func() hostdoctor.ResourceSnapshot {
		t.Error("unauthenticated resources sampled")
		return hostdoctor.ResourceSnapshot{}
	}
	for _, path := range []string{"/health", "/v1/models", "/paracetamol/v1/status?requests=64", "/v1/chat/completions", "/unknown"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			for _, authorization := range []string{"", "Bearer wrong", "Basic " + testAPIKey, "Bearer " + testAPIKey + ", Bearer wrong"} {
				request := httptest.NewRequest(method, path, unreadableAuthBody{})
				request.Header.Set("Authorization", authorization)
				response := httptest.NewRecorder()
				server.ServeHTTP(response, request)
				if response.Code != 401 || response.Header().Get(IdentityHeader) != IdentityValue || response.Header().Get("WWW-Authenticate") == "" || strings.Contains(response.Body.String(), testAPIKey) {
					t.Fatalf("unsafe response: %d %s", response.Code, response.Body)
				}
			}
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Add("Authorization", "Bearer "+testAPIKey)
	request.Header.Add("Authorization", "Bearer "+testAPIKey)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal("accepted duplicate Authorization")
	}
	status, err := scheduler.Status(context.Background())
	if err != nil || status.State != StateUnloaded || status.ActiveRequests != 0 || status.QueuedRequests != 0 {
		t.Fatalf("scheduler=%+v %v", status, err)
	}
	if len(server.requests.Recent(64)) != 0 {
		t.Fatal("unauthenticated payload entered request ledger")
	}
}

func TestBearerKeyIsConsumedNotForwardedAndMetadataClientsAuthenticate(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("gateway credentials reached backend")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer upstream.Close()
	server, _, handler := testGatewayServer(t, upstream)
	handler.auth, _ = newBearerAuth(testAPIKey)
	if _, err := FetchModelIDs(context.Background(), server.URL+"/v1", ""); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("missing-key error=%v", err)
	}
	models, err := FetchModelIDs(context.Background(), server.URL+"/v1", testAPIKey)
	if err != nil || len(models) != 1 || models[0] != "fixture" {
		t.Fatalf("models=%v %v", models, err)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"fixture"}`))
	request.Header.Set("Authorization", "bEaReR "+testAPIKey)
	request.Header.Set("Proxy-Authorization", "must-not-forward")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("response=%d calls=%d", response.StatusCode, calls.Load())
	}
	status, err := FetchStatus(context.Background(), server.URL+"/v1", testAPIKey, 1)
	if err != nil || len(status.RecentRequests) != 1 {
		t.Fatalf("status=%+v %v", status, err)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), testAPIKey) {
		t.Fatal("credential leaked into status")
	}
}

func TestGatewayMetadataClientNeverFollowsRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(IdentityHeader, IdentityValue)
		http.Redirect(w, r, target.URL+"/v1/models", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	if _, err := FetchModelIDs(context.Background(), server.URL+"/v1", testAPIKey); err == nil {
		t.Fatal("accepted redirect")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("redirect target received a request")
	}
}

func TestGatewayKeyValidationAndNoAuthDefault(t *testing.T) {
	if _, err := newBearerAuth("short"); err == nil {
		t.Fatal("accepted weak key")
	}
	auth, err := newBearerAuth("")
	if err != nil || !auth.allow(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil)) {
		t.Fatal("changed unauthenticated default")
	}
}

func TestMetadataResponsesAreBoundedAndErrorsDoNotEchoBodies(t *testing.T) {
	for _, test := range []struct {
		name, marker, body, want string
		status                   int
	}{
		{"wrong service", "other-service", testAPIKey, "is not a Paracetamol gateway", 200},
		{"unavailable", IdentityValue, testAPIKey, "HTTP 503", 503},
		{"oversized", IdentityValue, strings.Repeat(" ", 2*1024*1024+1), "too large", 200},
		{"invalid JSON", IdentityValue, testAPIKey, "", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(IdentityHeader, test.marker)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			_, modelsErr := FetchModelIDs(context.Background(), server.URL+"/v1", testAPIKey)
			_, statusErr := FetchStatus(context.Background(), server.URL+"/v1", testAPIKey, 0)
			for _, err := range []error{modelsErr, statusErr} {
				if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), testAPIKey) {
					t.Fatalf("unsafe metadata error: %v", err)
				}
			}
		})
	}
}

func TestMetadataRejectsUnknownStatusSchemaAndHonorsCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set(IdentityHeader, IdentityValue)
		_, _ = io.WriteString(w, `{"schema":"unknown"}`)
	}))
	defer server.Close()
	if _, err := FetchStatus(context.Background(), server.URL+"/v1", "", 0); err == nil || !strings.Contains(err.Error(), "unsupported status schema") {
		t.Fatalf("schema error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchModelIDs(ctx, server.URL+"/v1", testAPIKey); err == nil {
		t.Fatal("ignored cancellation")
	}
	if calls.Load() != 1 {
		t.Fatal("canceled probe reached server")
	}
}
