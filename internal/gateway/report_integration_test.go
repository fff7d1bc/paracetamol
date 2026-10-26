package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"paracetamol/internal/hostdoctor"
	"paracetamol/internal/textmodel"
)

func completedReport(t *testing.T, endpoint, id string) RequestReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		report, err := FetchRequestReport(ctx, endpoint+"/v1", "", id)
		if err != nil {
			t.Fatal(err)
		}
		if report.State == "completed" {
			return report
		}
		select {
		case <-ctx.Done():
			t.Fatal("gateway never published completion")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestGatewayRequestReportsUseActualProductionSamplingAndStreaming(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "canceled"}[canceled], func(t *testing.T) {
			const role = "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"
			const tail = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"private-response\"},\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":8,\"prompt_tokens_details\":{\"cached_tokens\":12}},\"timings\":{\"prompt_n\":8,\"prompt_ms\":25,\"predicted_n\":8,\"predicted_ms\":100,\"draft_verif_steps\":5,\"private_stat\":\"private-native-stat\"}}\n\ndata: [DONE]\n\n"
			continueResponse := make(chan struct{})
			defer close(continueResponse)
			bodySeen := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				bodySeen <- body
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set(InstanceHeader, "untrusted-backend-instance")
				_, _ = io.WriteString(w, role)
				w.(http.Flusher).Flush()
				select {
				case <-continueResponse:
					split := strings.Index(tail, "draft_verif_steps") + 7
					_, _ = io.WriteString(w, tail[:split])
					w.(http.Flusher).Flush()
					_, _ = io.WriteString(w, tail[split:])
				case <-r.Context().Done():
				}
			}))
			defer upstream.Close()
			gateway, _, handler := testGatewayServer(t, upstream)
			model := samplingModel(t, textmodel.BackendLlamaCPP)
			handler.registry = Registry{Models: []Model{model}, Fingerprint: "production-policy-fixture", byID: map[string]Model{model.ID: model}}
			handler.resources = func() hostdoctor.ResourceSnapshot { panic("report sampled resources") }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			payload := `{"model":"` + model.ID + `","stream":true,"reasoning_effort":"none","temperature":0,"top_p":null,"messages":[{"role":"user","content":"private-prompt"}],"tools":[{"private":"private-tool"}]}`
			request, _ := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(payload))
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			id := response.Header.Get(RequestIDHeader)
			if !validRequestReportID(id) || id[1:33] != handler.instanceID || len(response.Header.Values(InstanceHeader)) != 1 || response.Header.Get(InstanceHeader) != handler.instanceID {
				t.Fatalf("invalid correlation headers: %v", response.Header)
			}
			report, err := FetchRequestReport(context.Background(), gateway.URL+"/v1", "", id)
			if err != nil || report.State != "active" || report.Phase != "upstream" || report.Active == nil {
				t.Fatalf("active=%+v err=%v", report, err)
			}
			controls := report.Active.Controls
			if c := findControl(t, controls.Sampling, "temperature"); c.Source != ControlClient || c.Value != "0" {
				t.Fatalf("explicit zero lost: %+v", c)
			}
			if c := findControl(t, controls.Sampling, "top_p"); c.Source != ControlGatewayDefault || !c.Provided {
				t.Fatalf("null provenance lost: %+v", c)
			}
			var forwarded map[string]json.RawMessage
			if err := json.Unmarshal(<-bodySeen, &forwarded); err != nil {
				t.Fatal(err)
			}
			for key, value := range model.samplingDefaults.Defaults(false) {
				if key == "temperature" {
					value = 0
				}
				var actual float64
				expectedJSON, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var expected float64
				if err := json.Unmarshal(expectedJSON, &expected); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(forwarded[key], &actual); err != nil || actual != expected {
					t.Fatalf("forwarded %s=%s want %s", key, forwarded[key], expectedJSON)
				}
			}
			if canceled {
				cancel()
			} else {
				continueResponse <- struct{}{}
				body, err := io.ReadAll(response.Body)
				if err != nil || string(body) != role+tail {
					t.Fatalf("proxy changed SSE bytes: %q err=%v", body, err)
				}
			}
			report = completedReport(t, gateway.URL, id)
			if canceled {
				if report.Record.Outcome != OutcomeCanceled || report.Record.Completion.Observation != CompletionPartial || report.Record.BackendTimings.DraftVerificationSteps != nil {
					t.Fatalf("cancellation=%+v", report.Record)
				}
			} else if record := report.Record; record.Outcome != OutcomeSucceeded || record.Completion.Observation != CompletionAvailable || len(record.Completion.FinishReasons) != 1 || record.Completion.FinishReasons[0].FinishReason != "length" || record.Completion.StreamDone == nil || !*record.Completion.StreamDone || record.BackendTimings.PromptTokens == nil || *record.BackendTimings.PromptTokens != 8 || record.BackendTimings.DraftVerificationSteps == nil || *record.BackendTimings.DraftVerificationSteps != 5 {
				t.Fatalf("completion=%+v", record)
			}
			if !canceled {
				recent := handler.requests.Recent(1)
				if len(recent) != 1 || recent[0].BackendTimings.DraftVerificationSteps == nil || *recent[0].BackendTimings.DraftVerificationSteps != 5 {
					t.Fatalf("live ledger lost SSE counter: %#v", recent)
				}
			}
			encoded, _ := json.Marshal(report)
			for _, private := range []string{"private-response", "private-prompt", "private-tool", "private-native-stat"} {
				if bytes.Contains(encoded, []byte(private)) {
					t.Fatalf("private content in report: %s", encoded)
				}
			}
		})
	}
}

func TestGatewayRequestIdentityDoesNotCollideAcrossRestarts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"index":0,"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	var previous string
	for range 2 {
		gateway, _, handler := testGatewayServer(t, upstream)
		response, err := http.Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"fixture"}`))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		id := response.Header.Get(RequestIDHeader)
		if id == previous || id[1:33] != handler.instanceID {
			t.Fatalf("request identity repeated: %q", id)
		}
		if previous != "" {
			probe := httptest.NewRecorder()
			handler.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, requestReportPath+previous, nil))
			if probe.Code != http.StatusNotFound {
				t.Fatal("old request resolved in another gateway instance")
			}
		}
		previous = id
	}
}

func TestCompletionObserverFramingLossAndRecovery(t *testing.T) {
	const terminal = `{"choices":[{"index":0,"finish_reason":"stop"}]}`
	for _, fixture := range []struct {
		name, contentType, body string
		observation             CompletionObservation
	}{
		{"json", "application/json", terminal, CompletionAvailable},
		{"fragmented", "text/event-stream", "data: " + terminal + "\n\ndata: [DONE]\n\n", CompletionAvailable},
		{"no done", "text/event-stream", "data: " + terminal + "\n\n", CompletionPartial},
		{"incomplete done", "text/event-stream", "data: " + terminal + "\n\ndata: [DONE]", CompletionPartial},
		{"malformed event", "text/event-stream", "data: invalid\n\ndata: " + terminal + "\n\ndata: [DONE]\n\n", CompletionPartial},
		{"oversized event", "text/event-stream", "data: " + strings.Repeat("x", maxObservedSSEEventBytes+1) + "\n\ndata: " + terminal + "\n\ndata: [DONE]\n\n", CompletionPartial},
		{"oversized json", "application/json", terminal + strings.Repeat(" ", maxObservedJSONBytes), CompletionPartial},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			observer := newResponseObserver(fixture.contentType)
			// Exercise arbitrary Read boundaries, not just complete JSON events.
			for offset := 0; offset < len(fixture.body); offset += 97 {
				observer.Observe([]byte(fixture.body[offset:min(offset+97, len(fixture.body))]))
			}
			if got := observer.Finish().Completion; got.Observation != fixture.observation {
				t.Fatalf("completion=%+v", got)
			}
		})
	}
}
