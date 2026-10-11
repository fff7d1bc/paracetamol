package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/textmodel"
)

func samplingModels(t *testing.T) []Model {
	t.Helper()
	managed, err := catalog.Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := textmodel.All(managed)
	if err != nil {
		t.Fatal(err)
	}
	var models []Model
	for _, candidate := range candidates {
		models = append(models, Model{ID: candidate.ID, Application: string(candidate.Backend), Backend: candidate.Backend,
			ReasoningControl: candidate.ReasoningControl, ReasoningLevels: candidate.ReasoningLevels,
			ReasoningDefault: candidate.ReasoningDefault, ReasoningOff: candidate.ReasoningOff,
			SamplingPolicy: candidate.SamplingPolicy, samplingDefaults: managed.SamplingPolicies[candidate.SamplingPolicy]})
	}
	return models
}

func samplingModel(t *testing.T, backend textmodel.Backend) Model {
	t.Helper()
	for _, model := range samplingModels(t) {
		if model.Backend == backend && (strings.Contains(model.ID, "qwen3.8-27b-mtp-ud-q8") || backend == textmodel.BackendGufo) {
			return model
		}
	}
	t.Fatal("missing sampling fixture")
	return Model{}
}

func normalizedSampling(t *testing.T, body string, model Model) ([]byte, RequestControls, error) {
	t.Helper()
	observation, err := inspectRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	result, err := applySamplingDefaults([]byte(body), model, &observation.Controls)
	return result, observation.Controls, err
}

func TestSamplingDefaultsEveryManagedModelAndMode(t *testing.T) {
	policies := map[string]bool{}
	for _, model := range samplingModels(t) {
		t.Run(model.ID, func(t *testing.T) {
			if model.SamplingPolicy == "" {
				body := ` { "model":"fixture", "temperature":null,"chat_template_kwargs":{"enable_thinking":"invalid"} } `
				result, controls, err := normalizedSampling(t, body, model)
				if err != nil || string(result) != body || controls.SamplingPolicy != "" {
					t.Fatalf("model without a policy was changed: %s %+v %v", result, controls, err)
				}
				return
			}
			policies[model.SamplingPolicy] = true
			selections := []string{"", "none"}
			if model.ReasoningControl == "toggle" {
				selections = append(selections, "high")
			} else {
				selections = append(selections, model.ReasoningLevels...)
			}
			for _, selection := range selections {
				body := `{"model":"fixture"}`
				if selection != "" {
					body = `{"model":"fixture","reasoning_effort":"` + selection + `"}`
				}
				result, controls, err := normalizedSampling(t, body, model)
				if err != nil {
					t.Fatalf("%s: %v", selection, err)
				}
				object, err := decodeJSONObject(result)
				if err != nil {
					t.Fatal(err)
				}
				thinking := selection != "none"
				for key, value := range model.samplingDefaults.Defaults(thinking) {
					want, _ := json.Marshal(value)
					if !bytes.Equal(object[key], want) {
						t.Fatalf("%s: %s=%s want %s", selection, key, object[key], want)
					}
					control := findControl(t, controls.Sampling, key)
					if control.Source != ControlGatewayDefault || control.Provided || control.Value != string(want) {
						t.Fatalf("%s control=%+v", selection, control)
					}
				}
				if controls.SamplingPolicy != model.SamplingPolicy || (controls.SamplingMode == "thinking") != thinking {
					t.Fatalf("mode attribution=%+v", controls)
				}
			}
		})
	}
	if len(policies) != 5 {
		t.Fatalf("tested policies=%v", policies)
	}
}

func TestSamplingExplicitValuesNullsAndUnrelatedPayload(t *testing.T) {
	model := samplingModel(t, textmodel.BackendLlamaCPP)
	body := `{"model":"fixture","temperature":0,"top_p":null,"min_p":0,"presence_penalty":-0.5,"repeat_penalty":"invalid-client-value","seed":9007199254740993,"messages":[{"role":"user","content":"<keep> \u263a"}],"tools":[{"type":"function","function":{"name":"untouched","parameters":{"type":"object"}}}],"custom":{"n":9007199254740995}}`
	result, controls, err := normalizedSampling(t, body, model)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := decodeJSONObject([]byte(body))
	after, _ := decodeJSONObject(result)
	for _, key := range []string{"temperature", "min_p", "presence_penalty", "repeat_penalty", "seed", "messages", "tools", "custom"} {
		var a, b bytes.Buffer
		_ = json.Compact(&a, before[key])
		_ = json.Compact(&b, after[key])
		// JSON can escape HTML while preserving exact numbers and strings.
		var oldValue, newValue any
		oldDecoder, newDecoder := json.NewDecoder(&a), json.NewDecoder(&b)
		oldDecoder.UseNumber()
		newDecoder.UseNumber()
		_ = oldDecoder.Decode(&oldValue)
		_ = newDecoder.Decode(&newValue)
		if !reflect.DeepEqual(oldValue, newValue) {
			t.Fatalf("changed client field %s: %s -> %s", key, before[key], after[key])
		}
	}
	if control := findControl(t, controls.Sampling, "temperature"); control.Source != ControlClient || control.Value != "0" {
		t.Fatalf("explicit zero=%+v", control)
	}
	if control := findControl(t, controls.Sampling, "top_p"); control.Source != ControlGatewayDefault || !control.Provided || control.Value != "0.95" {
		t.Fatalf("explicit null=%+v", control)
	}
	if control := findControl(t, controls.Sampling, "repeat_penalty"); control.Source != ControlInvalid {
		t.Fatalf("invalid explicit value was repaired: %+v", control)
	}
	if summary := controlsLogSummary(controls); !strings.Contains(summary, "temperature=0") || !strings.Contains(summary, "top_p=0.95(gateway)") || strings.Contains(summary, "untouched") {
		t.Fatalf("summary=%s", summary)
	}
	fullyExplicit := ` { "model":"fixture","temperature":0,"top_p":0,"top_k":0,"min_p":0,"presence_penalty":0,"repeat_penalty":0 } `
	result, _, err = normalizedSampling(t, fullyExplicit, model)
	if err != nil || string(result) != fullyExplicit {
		t.Fatalf("fully explicit request changed: %s %v", result, err)
	}
}

func TestSamplingThinkingControls(t *testing.T) {
	for _, backend := range []textmodel.Backend{textmodel.BackendLlamaCPP, textmodel.BackendGufo} {
		model := samplingModel(t, backend)
		for _, test := range []struct {
			fields string
			on     bool
		}{
			{`"chat_template_kwargs":{"enable_thinking":false}`, false},
			{`"chat_template_kwargs":{"enable_thinking":true}`, true},
			{`"reasoning_effort":"none","chat_template_kwargs":{"enable_thinking":false}`, false},
			{`"chat_template_kwargs":{"reasoning_effort":"xhigh","preserve_thinking":true}`, true},
			{`"reasoning_effort":"medium","chat_template_kwargs":{"reasoning_effort":"medium","enable_thinking":true}`, true},
		} {
			body := `{"model":"fixture",` + test.fields + `}`
			_, controls, err := normalizedSampling(t, body, model)
			if err != nil || (controls.SamplingMode == "thinking") != test.on {
				t.Fatalf("%s %s: %+v %v", backend, test.fields, controls, err)
			}
		}
	}
	gufo := samplingModel(t, textmodel.BackendGufo)
	for _, field := range []string{`"thinking":{"type":"disabled"}`, `"reasoning_effort":"off"`, `"chat_template_kwargs":{"thinking_mode":"chat"}`} {
		_, controls, err := normalizedSampling(t, `{"model":"fixture",`+field+`}`, gufo)
		if err != nil || controls.SamplingMode != "non_thinking" {
			t.Fatalf("Gufo %s: %+v %v", field, controls, err)
		}
	}
}

func TestSamplingRejectsAmbiguousOrInvalidReasoning(t *testing.T) {
	for _, backend := range []textmodel.Backend{textmodel.BackendLlamaCPP, textmodel.BackendGufo} {
		model := samplingModel(t, backend)
		for _, fields := range []string{
			`"reasoning_effort":"none","chat_template_kwargs":{"enable_thinking":true}`,
			`"reasoning_effort":"medium","chat_template_kwargs":{"enable_thinking":false}`,
			`"reasoning_effort":"xhigh","chat_template_kwargs":{"reasoning_effort":"medium"}`,
			`"reasoning_effort":"secret-invalid-effort"`,
			`"reasoning_effort":null`,
			`"reasoning_effort":false`,
			`"chat_template_kwargs":{"enable_thinking":"false"}`,
			`"chat_template_kwargs":{"enable_thinking":null}`,
			`"chat_template_kwargs":{"reasoning_effort":"none"}`,
			`"chat_template_kwargs":[]`,
			`"chat_template_kwargs":null`,
			`"reasoning_effort":"none","reasoning_effort":"medium"`,
			`"chat_template_kwargs":{"enable_thinking":false,"enable_thinking":true}`,
			`"temperature":0,"temperature":1`,
		} {
			_, _, err := normalizedSampling(t, `{"model":"fixture",`+fields+`}`, model)
			if err == nil || strings.Contains(err.Error(), "secret-invalid-effort") {
				t.Fatalf("%s %s: error=%v", backend, fields, err)
			}
		}
	}
	gufo := samplingModel(t, textmodel.BackendGufo)
	for _, fields := range []string{
		`"thinking":{"type":"disabled"},"reasoning_effort":"medium"`,
		`"chat_template_kwargs":{"thinking_mode":"chat","enable_thinking":true}`,
		`"chat_template_kwargs":{"thinking_mode":"secret-invalid-mode"}`,
		`"thinking":{"type":"adaptive"}`,
	} {
		if _, _, err := normalizedSampling(t, `{"model":"fixture",`+fields+`}`, gufo); err == nil {
			t.Fatalf("Gufo accepted %s", fields)
		}
	}
}

func TestDwarfStarNativeThinkingControlsWhenPolicyIsDeclared(t *testing.T) {
	// The shipped DeepSeek preset deliberately has no policy. This fixture
	// checks the shared mechanism without inventing new DeepSeek defaults.
	model := samplingModel(t, textmodel.BackendLlamaCPP)
	model.Backend = textmodel.BackendDwarfStar
	model.ReasoningLevels, model.ReasoningDefault = []string{"high"}, "high"
	for _, field := range []string{
		`"think":false`, `"thinking":false`, `"thinking":{"type":"disabled"}`,
		`"reasoning_effort":"none"`, `"chat_template_kwargs":{"reasoning_effort":"none"}`,
	} {
		_, controls, err := normalizedSampling(t, `{"model":"fixture",`+field+`}`, model)
		if err != nil || controls.SamplingMode != "non_thinking" {
			t.Fatalf("%s: %+v %v", field, controls, err)
		}
	}
}

func TestGatewayForwardsDefaultsAndRecordsTheirProvenance(t *testing.T) {
	for _, backend := range []textmodel.Backend{textmodel.BackendLlamaCPP, textmodel.BackendGufo} {
		t.Run(string(backend), func(t *testing.T) {
			requests := make(chan []byte, 2)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.ContentLength != int64(len(body)) {
					t.Errorf("wrong rewritten content length %d != %d", r.ContentLength, len(body))
				}
				requests <- body
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer upstream.Close()
			endpoint, _, server := testGatewayServer(t, upstream)
			model := samplingModel(t, backend)
			server.registry = Registry{byID: map[string]Model{model.ID: model}}
			body := `{"model":"` + model.ID + `","reasoning_effort":"none","stream":true,"temperature":0,"messages":[{"role":"user","content":"private"}]}`
			response, err := http.Post(endpoint.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			output, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != 200 || !bytes.Contains(output, []byte("[DONE]")) {
				t.Fatalf("response %d: %s", response.StatusCode, output)
			}
			forwarded, _ := decodeJSONObject(<-requests)
			if string(forwarded["temperature"]) != "0" || string(forwarded["top_p"]) != "0.8" || string(forwarded["presence_penalty"]) != "1.5" {
				t.Fatalf("forwarded=%s", forwarded)
			}
			records := server.requests.Recent(1)
			if len(records) != 1 || records[0].Controls.SamplingMode != "non_thinking" || records[0].Outcome != OutcomeSucceeded {
				t.Fatalf("records=%+v", records)
			}
			if encoded, _ := json.Marshal(records); bytes.Contains(encoded, []byte("private")) {
				t.Fatal("request ledger retained prompt text")
			}
			response, err = http.Post(endpoint.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"`+model.ID+`","reasoning_effort":"none","chat_template_kwargs":{"enable_thinking":true}}`))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 400 || len(requests) != 0 {
				t.Fatalf("invalid request reached backend, status=%d", response.StatusCode)
			}
		})
	}
}

func TestGatewayRejectsReasoningConflictBeforeAllocation(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	scheduler, err := NewScheduler(lifecycle, QueueLimit, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Shutdown(context.Background())
	model := samplingModel(t, textmodel.BackendLlamaCPP)
	registry := Registry{Models: []Model{model}, byID: map[string]Model{model.ID: model}}
	server, err := NewServer(registry, scheduler, io.Discard, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+model.ID+`","reasoning_effort":"none","chat_template_kwargs":{"enable_thinking":true}}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 400 || len(lifecycle.Events()) != 0 {
		t.Fatalf("status=%d events=%v", response.Code, lifecycle.Events())
	}
	if records := server.requests.Recent(1); len(records) != 1 || records[0].Outcome != OutcomeRejected {
		t.Fatalf("rejection not recorded: %+v", records)
	}
}
