package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/gateway"
	"paracetamol/internal/project"
	"paracetamol/internal/verification"
)

func proxyCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return managed
}

func proxyFixture(t *testing.T) (gateway.Registry, catalog.Catalog) {
	t.Helper()
	managed := proxyCatalog(t)
	root := t.TempDir()
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range managed.Bundles[managed.LlamaPresets[regular].Bundle].Artifacts {
		a := managed.Artifacts[id]
		a.Size, a.SHA256 = 4, "synthetic-"+id
		managed.Artifacts[id] = a
		path := content.ArtifactPath(root, a)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := store.Record(path, a.Size, a.SHA256); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := proxyRegistry(managed, root); err == nil {
		t.Fatal("unpublished receipts accepted")
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	r, err := proxyRegistry(managed, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Models) != 1 || r.Models[0].ID != regular {
		t.Fatal("proxy admitted another model")
	}
	return r, managed
}

func testProxy(t *testing.T, r gateway.Registry, upstream string) http.Handler {
	t.Helper()
	u, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	server, scheduler, handler, err := proxyHandler(r, u, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.BeginShutdown()
		if err := scheduler.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return handler
}

func TestProxyOriginsAreNumericLoopbackOnly(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:18080", "http://[::1]:18080"} {
		if _, err := origin(raw); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://localhost:18080", "http://example.invalid:18080", "http://192.0.2.1:18080", "https://127.0.0.1:18080", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://user:pass@127.0.0.1:18080", "http://127.0.0.1:18080/", "http://127.0.0.1:18080?q=1", "http://127.0.0.1:18080#fragment"} {
		if _, err := origin(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestProxyRequiresReceiptsAndExplicitAcknowledgment(t *testing.T) {
	if _, err := proxyRegistry(proxyCatalog(t), t.TempDir()); err == nil {
		t.Fatal("missing receipts accepted")
	}
	missing := filepath.Join(t.TempDir(), "absent")
	for _, args := range [][]string{
		{"--upstream", "http://127.0.0.1:18080", "--data-dir", missing},
		{"--experimental", "--upstream", "http://127.0.0.1:18080", "--data-dir", missing},
		{"--experimental", "--upstream", "http://localhost:18080", "--data-dir", missing},
		{"--experimental", "--upstream", "http://127.0.0.1:18080", "--listen", "0.0.0.0:18081", "--data-dir", missing},
		{"--experimental", "--upstream", "http://127.0.0.1:18080", "--listen", "127.0.0.1:18080", "--data-dir", missing},
		{"--experimental", "--upstream", "http://127.0.0.1:18080", "--data-dir", missing, "extra"},
	} {
		if err := runProxy(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("validation created the missing data root")
	}
}

func TestProxyUsesRealSamplerAndPreservesHistoryAndJSON(t *testing.T) {
	r, managed := proxyFixture(t)
	forwarded := make(chan []byte, 1)
	const payload = " {\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":1}}\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/chat/completions" {
			t.Errorf("path=%s", req.URL.Path)
		}
		body, _ := io.ReadAll(req.Body)
		forwarded <- body
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, payload)
	}))
	defer upstream.Close()
	handler := testProxy(t, r, upstream.URL)
	for _, effort := range []string{"xhigh", "medium", "none"} {
		for _, explicit := range []bool{false, true} {
			request := map[string]json.RawMessage{
				"model": json.RawMessage(`"` + regular + `"`), "reasoning_effort": json.RawMessage(`"` + effort + `"`),
				"messages": json.RawMessage(`[{"role":"user","content":"fixture"},{"role":"assistant","content":null,"reasoning_content":"retain exactly","tool_calls":[{"id":"call1","type":"function","function":{"name":"fixture","arguments":"{\"number\":9007199254740993}"}}]},{"role":"tool","tool_call_id":"call1","content":"typed fixture"}]`),
				"custom":   json.RawMessage(`{"number":9007199254740993}`), "top_p": json.RawMessage(`null`),
			}
			if explicit {
				request["temperature"] = json.RawMessage(`0.125`)
			}
			body, _ := json.Marshal(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)))
			if response.Code != 200 || response.Body.String() != payload {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(<-forwarded, &got); err != nil {
				t.Fatal(err)
			}
			for key, value := range managed.SamplingPolicies[managed.LlamaPresets[regular].SamplingPolicy].Defaults(effort != "none") {
				want, _ := json.Marshal(value)
				if key == "temperature" && explicit {
					want = request[key]
				}
				if !bytes.Equal(got[key], want) {
					t.Fatalf("%s %s=%s want=%s", effort, key, got[key], want)
				}
			}
			for _, key := range []string{"messages", "custom"} {
				var a, b any
				da, db := json.NewDecoder(bytes.NewReader(request[key])), json.NewDecoder(bytes.NewReader(got[key]))
				da.UseNumber()
				db.UseNumber()
				if da.Decode(&a) != nil || db.Decode(&b) != nil || !reflect.DeepEqual(a, b) {
					t.Fatalf("changed %s", key)
				}
			}
			reportResponse := httptest.NewRecorder()
			handler.ServeHTTP(reportResponse, httptest.NewRequest(http.MethodGet, "/paracetamol/v1/requests/"+response.Header().Get(gateway.RequestIDHeader), nil))
			var report gateway.RequestReport
			if err := json.Unmarshal(reportResponse.Body.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.State != "completed" || report.Record == nil || report.Record.Outcome != gateway.OutcomeSucceeded || len(report.Record.Controls.Sampling) != 6 {
				t.Fatalf("report=%s", reportResponse.Body.String())
			}
			for _, control := range report.Record.Controls.Sampling {
				want := gateway.ControlGatewayDefault
				if explicit && control.Name == "temperature" {
					want = gateway.ControlClient
				}
				if control.Source != want {
					t.Fatalf("control=%+v want source %s", control, want)
				}
			}
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+regular+`","reasoning_effort":"none","chat_template_kwargs":{"enable_thinking":true}}`)))
	if response.Code != 400 || len(forwarded) != 0 {
		t.Fatal("conflicting reasoning reached external backend")
	}
}

func TestProxyRouteFilterAndSSEBytesAndFlush(t *testing.T) {
	r, _ := proxyFixture(t)
	const payload = "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"keep\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, payload)
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	handler := testProxy(t, r, upstream.URL)
	for _, path := range []string{"/", "/health", "/paracetamol/v1/ui", "/paracetamol/v1/status", "/paracetamol/v1/statistics", "/paracetamol/v1/clients/maki/providers.toml", "/paracetamol/v1/clients/maki/init.lua", "/assets/gateway.js"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 404 {
			t.Errorf("exposed route %s: %d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), regular) {
		t.Fatalf("models=%s", response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/models", nil))
	if response.Code != 404 {
		t.Fatal("wrong method exposed")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+regular+`","reasoning_effort":"xhigh","stream":true}`)))
	if response.Code != 200 || response.Body.String() != payload || !response.Flushed {
		t.Fatalf("SSE bytes/flush changed: %q", response.Body.String())
	}
}

func TestProxyClientCancellationAndServerShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "client", true: "server"}[shutdown], func(t *testing.T) {
			r, _ := proxyFixture(t)
			started, canceled := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				io.Copy(io.Discard, req.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"started\"}}]}\n\n")
				w.(http.Flusher).Flush()
				close(started)
				<-req.Context().Done()
				close(canceled)
			}))
			defer upstream.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(upstream.URL)
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			done := make(chan error, 1)
			var stdout, stderr bytes.Buffer
			go func() { done <- serveProxy(ctx, listener, r, u, &stdout, &stderr) }()
			requestContext, cancelRequest := context.WithCancel(context.Background())
			defer cancelRequest()
			req, _ := http.NewRequestWithContext(requestContext, http.MethodPost, "http://"+listener.Addr().String()+"/v1/chat/completions", strings.NewReader(`{"model":"`+regular+`","stream":true}`))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("upstream did not start")
			}
			if shutdown {
				stop()
			} else {
				cancelRequest()
				resp.Body.Close()
			}
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("cancellation did not propagate upstream")
			}
			stop()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("proxy/scheduler did not stop")
			}
			if !strings.Contains(stderr.String(), "WARNING: EXPERIMENTAL") || !strings.Contains(stderr.String(), "NOT the actual experimental engine") || !strings.Contains(stdout.String(), r.Fingerprint) {
				t.Fatal("missing experimental/provenance warning")
			}
		})
	}
}

func TestProxyServeFailureClosesListener(t *testing.T) {
	r, _ := proxyFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	u, _ := url.Parse("http://127.0.0.1:18080")
	if err := serveProxy(context.Background(), listener, r, u, io.Discard, io.Discard); err == nil {
		t.Fatal("Serve failure was lost")
	}
}
