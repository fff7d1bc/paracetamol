package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"paracetamol/internal/clientconfig"
	"paracetamol/internal/hostdoctor"
	"paracetamol/internal/textmodel"
)

func webFixtureModels() []Model {
	return []Model{
		{ID: clientconfig.RecommendedModel, Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP, Context: 262144, MaxOutputTokens: 16384, AgentTools: true, ReasoningControl: "effort", ReasoningLevels: []string{"medium", "xhigh"}, ReasoningDefault: "medium", ReasoningOff: true},
		{ID: "unsloth-qwen3.8-27b-ud-q8-k-xl", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP, Context: 262144, MaxOutputTokens: 16384, AgentTools: true, ReasoningControl: "effort", ReasoningLevels: []string{"medium", "xhigh"}, ReasoningDefault: "medium", ReasoningOff: true},
		{ID: "local-<img src=x onerror=alert(1)>", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP, Context: 8192, MaxOutputTokens: 1024},
	}
}

func TestWebPublicSurfaceIsExactAndGeneric(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("page contacted backend") }))
	defer upstream.Close()
	_, scheduler, server := testGatewayServer(t, upstream)
	server.registry.Models = webFixtureModels()
	server.auth, _ = newBearerAuth(testAPIKey)
	server.resources = func() hostdoctor.ResourceSnapshot {
		t.Fatal("page sampled resources")
		return hostdoctor.ResourceSnapshot{}
	}
	for _, path := range []string{"/", "/assets/gateway.css", "/assets/gateway.js"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := httptest.NewRequest(method, path, nil)
			r.Host = "private-host.invalid"
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("%s %s: %d %v", method, path, w.Code, w.Header())
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatal("HEAD has body")
			}
			for _, secret := range []string{testAPIKey, clientconfig.RecommendedModel, "private-host.invalid"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatalf("public %s leaked %q", path, secret)
				}
			}
		}
	}
	for _, path := range []string{"/assets/", "/assets/other.js", "/assets/../gateway.js", "/index.html", "/paracetamol/v1/ui", "/paracetamol/v1/status", "/v1/models", makiConfigPath + "providers.toml", makiConfigPath + "init.lua", "/health"} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 401 {
			t.Fatalf("anonymous %s returned %d", path, w.Code)
		}
	}
	for _, path := range []string{"/", "/assets/gateway.js"} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, unreadableAuthBody{}))
		if w.Code != 401 {
			t.Fatal("unexpected public method")
		}
	}
	status, err := scheduler.Status(context.Background())
	if err != nil || status.State != StateUnloaded || len(server.requests.Recent(64)) != 0 {
		t.Fatalf("page changed scheduler: %+v %v", status, err)
	}
}

func TestWebSnapshotIsReadOnlyAndKeepsClientDefaultsSeparate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unloaded snapshot contacted backend") }))
	defer upstream.Close()
	_, _, server := testGatewayServer(t, upstream)
	server.registry.Models = webFixtureModels()
	server.auth, _ = newBearerAuth(testAPIKey)
	server.resources = func() hostdoctor.ResourceSnapshot {
		t.Fatal("snapshot sampled host resources")
		return hostdoctor.ResourceSnapshot{}
	}
	r := httptest.NewRequest(http.MethodGet, "/paracetamol/v1/ui", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	var snapshot webSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || snapshot.DefaultModel != clientconfig.RecommendedModel || len(snapshot.Models) != 3 {
		t.Fatalf("snapshot=%+v status=%d", snapshot, w.Code)
	}
	first := snapshot.Models[0]
	if first.RequestSlots != 4 {
		t.Fatalf("request slots=%d", first.RequestSlots)
	}
	if first.MakiReasoningDefault != "xhigh" || first.ReasoningDefault != "medium" || first.State != ModelUnloaded || first.Family != "Qwen 3.8 Flash-Next" || first.Publisher != "unsloth" {
		t.Fatalf("model=%+v", first)
	}
	if snapshot.Models[2].Name != snapshot.Models[2].ID || snapshot.Models[2].Family != "Other models" {
		t.Fatal("unknown model identity lost")
	}
	for _, forbidden := range []string{testAPIKey, "recent_requests", "resources", "fingerprint", "bundle", "backend"} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatalf("unexpected snapshot field %s", forbidden)
		}
	}
	r.URL.RawQuery = "requests=64"
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("accepted snapshot query")
	}
	server.registry.Models = nil
	r.URL.RawQuery = ""
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"models":[]`) || strings.Contains(w.Body.String(), `"default_model"`) {
		t.Fatalf("empty snapshot=%s", w.Body)
	}
}

func TestWebSnapshotUsesOnlyReadOnlyResidencyForRunningBackend(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/models" {
			t.Errorf("page used unexpected backend route %s %s", r.Method, r.URL)
			w.WriteHeader(500)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"fixture","status":{"value":"loaded"},"meta":{"n_ctx":4096,"model_path":"/private/model.gguf"}},{"id":"foreign","status":{"value":"loaded"}}]}`)
	}))
	defer upstream.Close()
	_, scheduler, server := testGatewayServer(t, upstream)
	lease, err := scheduler.Acquire(context.Background(), AllocationLlamaCPP)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/paracetamol/v1/ui", nil))
	var snapshot webSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(snapshot.Models) != 1 || snapshot.Models[0].State != ModelLoaded || snapshot.Models[0].Runtime == nil || snapshot.Models[0].Runtime.Context == nil || *snapshot.Models[0].Runtime.Context != 4096 {
		t.Fatalf("snapshot=%s", w.Body)
	}
	if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "foreign") {
		t.Fatal("private router metadata leaked")
	}
	if len(server.requests.Recent(64)) != 0 {
		t.Fatal("snapshot recorded as inference")
	}
}
