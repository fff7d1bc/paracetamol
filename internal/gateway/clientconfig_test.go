package gateway

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"paracetamol/internal/clientconfig"
)

func TestClientConfigURL(t *testing.T) {
	for _, test := range []struct {
		host, query, expected string
		tls                   bool
	}{
		{"aion.local:7455", "", "http://aion.local:7455/v1", false},
		{"[::1]:7455", "", "http://[::1]:7455/v1", false},
		{"gateway.example", "", "https://gateway.example/v1", true},
		{"127.0.0.1:7455", "base_url=" + url.QueryEscape("https://public.example/v1"), "https://public.example/v1", false},
	} {
		r := httptest.NewRequest(http.MethodGet, makiConfigPath+"providers.toml?"+test.query, nil)
		r.Host = test.host
		r.Header.Set("X-Forwarded-Host", "untrusted.example")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Forwarded", "host=untrusted.example;proto=https")
		if test.tls {
			r.TLS = &tls.ConnectionState{}
		}
		got, err := clientConfigURL(r)
		if err != nil || got != test.expected {
			t.Fatalf("got=%s err=%v want=%s", got, err, test.expected)
		}
	}
	for _, query := range []string{"other=x", "base_url=", "base_url=x&base_url=y", "base_url=%zz", "base_url=" + url.QueryEscape("https://user:secret@host/v1"), "base_url=" + url.QueryEscape("https://host/v1?secret=1"), "base_url=" + url.QueryEscape("file:///v1"), "base_url=" + url.QueryEscape("http://0.0.0.0:7455/v1"), "base_url=" + url.QueryEscape("http://[::]:7455/v1"), "base_url=" + url.QueryEscape("https://host/other")} {
		r := httptest.NewRequest(http.MethodGet, makiConfigPath+"providers.toml", nil)
		r.URL.RawQuery = query
		if _, err := clientConfigURL(r); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	for _, host := range []string{"host/path", "user@host", "host?x", "host#x", "0.0.0.0:7455", "[::]:7455", "host\\evil", ""} {
		r := httptest.NewRequest(http.MethodGet, makiConfigPath+"providers.toml", nil)
		r.Host = host
		if _, err := clientConfigURL(r); err == nil {
			t.Fatalf("accepted host %q", host)
		}
	}
}

func TestMakiExportsAreAuthenticatedInventoryOnly(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("configuration contacted backend") }))
	defer upstream.Close()
	_, scheduler, handler := testGatewayServer(t, upstream)
	handler.registry.Models = []Model{
		{ID: clientconfig.RecommendedModel, Context: 262144, MaxOutputTokens: 16384, AgentTools: true, ReasoningControl: "effort", ReasoningLevels: []string{"low", "medium", "xhigh"}, ReasoningDefault: "medium", ReasoningOff: true},
		{ID: "not-an-agent-model"},
	}
	const secret = "fixture-secret-0123456789abcdef0123456789abcdef"
	for _, key := range []string{"", secret} {
		handler.auth, _ = newBearerAuth(key)
		for _, file := range []string{"providers.toml", "init.lua"} {
			r := httptest.NewRequest(http.MethodGet, makiConfigPath+file, nil)
			r.Host = "aion.local:7455"
			if key != "" {
				denied := httptest.NewRecorder()
				handler.ServeHTTP(denied, r)
				if denied.Code != http.StatusUnauthorized || strings.Contains(denied.Body.String(), clientconfig.RecommendedModel) {
					t.Fatal("unauthenticated export")
				}
				r.Header.Set("Authorization", "Bearer "+key)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			body := response.Body.String()
			if response.Code != http.StatusOK || !strings.Contains(body, clientconfig.RecommendedModel) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%d %s", response.Code, body)
			}
			for _, forbidden := range []string{secret, "not-an-agent-model", "X-Paracetamol-Session-ID", "temperature", "Authorization"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("leaked %s", forbidden)
				}
			}
			if file == "providers.toml" && (!strings.Contains(body, "http://aion.local:7455/v1") || (strings.Contains(body, `api_key =`) != (key == ""))) {
				t.Fatal("wrong URL or key policy")
			}
			requireGatewayIdentity(t, response.Result())
		}
	}
	status, err := scheduler.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateUnloaded || status.Allocation != "" || status.ActiveRequests != 0 || status.QueuedRequests != 0 {
		t.Fatalf("export scheduled a backend: %+v", status)
	}
	if len(handler.requests.Recent(10)) != 0 {
		t.Fatal("configuration counted as inference")
	}
}

func TestMakiExportFailures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("backend called") }))
	defer upstream.Close()
	_, _, handler := testGatewayServer(t, upstream)
	for _, test := range []struct {
		path   string
		status int
	}{
		{makiConfigPath + "providers.toml", http.StatusConflict},
		{makiConfigPath + "init.lua", http.StatusConflict},
		{makiConfigPath + "providers.toml?base_url=bad", http.StatusBadRequest},
		{makiConfigPath + "unknown", http.StatusNotFound},
		{"/paracetamol/v1/clients/pi/models.json", http.StatusNotFound},
	} {
		r := httptest.NewRequest(http.MethodGet, test.path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s: %d %s", test.path, w.Code, w.Body.String())
		}
	}
}
