package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"paracetamol/internal/textmodel"
)

func residencyServer(t *testing.T, handler http.Handler) (*Server, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(handler)
	registry := Registry{
		Models: []Model{
			{ID: "unloaded", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP},
			{ID: "loading", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP},
			{ID: "loaded", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP},
			{ID: "sleeping", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP},
			{ID: "failed", Application: "llama-cpp", Backend: textmodel.BackendLlamaCPP},
			{ID: "dwarf", Application: "dwarfstar", Backend: textmodel.BackendDwarfStar},
		},
		byID: map[string]Model{},
	}
	for _, model := range registry.Models {
		registry.byID[model.ID] = model
	}
	return &Server{registry: registry, residencyClient: upstream.Client()}, upstream
}

func TestProbeLlamaResidencyMapsOnlyFrozenModelState(t *testing.T) {
	server, upstream := residencyServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/models" || request.URL.RawQuery != "" {
			t.Fatalf("probe target=%s", request.URL.String())
		}
		_, _ = io.WriteString(writer, `{"data":[
  {"id":"unloaded","path":"/secret/model.gguf","status":{"value":"unloaded","args":["--secret"]}},
  {"id":"loading","status":{"value":"downloading"}},
  {"id":"loaded","status":{"value":"loaded"}},
  {"id":"sleeping","status":{"value":"sleeping"}},
  {"id":"failed","status":{"value":"unloaded","failed":true,"exit_code":1}},
  {"id":"foreign","status":{"value":"loaded"}}
]}`)
	}))
	defer upstream.Close()
	states, diagnostics := server.probeLlamaResidency(context.Background(), upstream.URL)
	want := map[string]ModelState{
		"unloaded": ModelUnloaded, "loading": ModelLoading, "loaded": ModelLoaded,
		"sleeping": ModelSleeping, "failed": ModelFailed,
	}
	for identifier, expected := range want {
		if states[identifier] != expected || diagnostics[identifier] != "" {
			t.Fatalf("%s state=%s diagnostic=%q", identifier, states[identifier], diagnostics[identifier])
		}
	}
	if _, ok := states["foreign"]; ok {
		t.Fatal("private router exposed a model outside the frozen inventory")
	}
	if encoded := strings.Join(mapValues(diagnostics), " "); strings.Contains(encoded, "secret") {
		t.Fatalf("diagnostics leaked router detail: %q", encoded)
	}
}

func TestProbeLlamaResidencyReportsMissingDuplicateMalformedAndOversized(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{"missing and duplicate", `{"data":[{"id":"loaded","status":{"value":"loaded"}},{"id":"loaded","status":{"value":"unloaded"}}]}`},
		{"malformed", `{"data":`},
		{"oversized", strings.Repeat("x", maxResidencyResponseBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, upstream := residencyServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.WriteString(writer, test.body)
			}))
			defer upstream.Close()
			states, diagnostics := server.probeLlamaResidency(context.Background(), upstream.URL)
			if states["unloaded"] != ModelUnknown || diagnostics["unloaded"] == "" {
				t.Fatalf("states=%#v diagnostics=%#v", states, diagnostics)
			}
			if test.name == "missing and duplicate" && (states["loaded"] != ModelUnknown || !strings.Contains(diagnostics["loaded"], "more than once")) {
				t.Fatalf("duplicate state=%s diagnostic=%q", states["loaded"], diagnostics["loaded"])
			}
		})
	}
}

func TestModelStatusDerivesDwarfStarResidencyFromAllocation(t *testing.T) {
	server, upstream := residencyServer(t, http.NotFoundHandler())
	defer upstream.Close()
	for _, test := range []struct {
		allocation AllocationState
		want       ModelState
	}{
		{StateStarting, ModelLoading},
		{StateReady, ModelLoaded},
		{StateDraining, ModelLoaded},
		{StateStopping, ModelUnknown},
		{StateFailed, ModelFailed},
	} {
		models := server.modelStatus(context.Background(), SchedulerStatus{Allocation: AllocationDwarfStar, State: test.allocation})
		for _, model := range models {
			if model.ID == "dwarf" && model.State != test.want {
				t.Fatalf("allocation=%s state=%s want=%s", test.allocation, model.State, test.want)
			}
		}
	}
}

func TestDecodeRouterModelStatusCoversPinnedRouterStates(t *testing.T) {
	for _, test := range []struct {
		value  string
		failed bool
		want   ModelState
	}{
		{"unloaded", false, ModelUnloaded},
		{"downloading", false, ModelLoading},
		{"downloaded", false, ModelLoading},
		{"loading", false, ModelLoading},
		{"loaded", false, ModelLoaded},
		{"sleeping", false, ModelSleeping},
		{"unloaded", true, ModelFailed},
		{"unexpected", false, ModelUnknown},
	} {
		raw := []byte(`{"value":"` + test.value + `","failed":` + map[bool]string{true: "true", false: "false"}[test.failed] + `}`)
		state, diagnostic := decodeRouterModelStatus(raw)
		if state != test.want || (state == ModelUnknown) != (diagnostic != "") {
			t.Fatalf("value=%s failed=%t state=%s diagnostic=%q", test.value, test.failed, state, diagnostic)
		}
	}
}

func mapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
