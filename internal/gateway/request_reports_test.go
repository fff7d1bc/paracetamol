package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"paracetamol/internal/hostdoctor"
)

const reportTestInstance = "0123456789abcdef0123456789abcdef"

func reportTestID(counter int) string {
	return fmt.Sprintf("r%s-%016x", reportTestInstance, counter)
}

func TestRequestReportLedgerTracksPhasesAndOwnsSnapshots(t *testing.T) {
	var ledger requestLedger
	record := RequestRecord{
		ID: reportTestID(1), Model: "fixture", Application: "llama-cpp", Stream: true, StartedAt: "start",
		Peer: "private-peer", UserAgent: "private-agent", SessionID: "private-session",
		Controls: RequestControls{
			Sampling:  []ObservedControl{{Name: "temperature", Source: ControlClient, Value: "0"}},
			Reasoning: []ObservedControl{{Name: "reasoning_effort", Value: "medium"}}, Diagnostics: []string{"diagnostic"},
		},
	}
	ledger.Track(record, "receiving")
	record.Controls.Sampling[0].Value = "changed"
	record.Controls.Reasoning[0].Value = "changed"
	record.Controls.Diagnostics[0] = "changed"
	report, ok := ledger.Lookup(record.ID)
	if !ok || report.State != "active" || report.Phase != "receiving" || report.Record != nil || report.Active.ID != record.ID {
		t.Fatalf("active report=%+v retained=%t", report, ok)
	}
	if controls := report.Active.Controls; controls.Sampling[0].Value != "0" || controls.Reasoning[0].Value != "medium" || controls.Diagnostics[0] != "diagnostic" {
		t.Fatalf("track retained caller-owned slices: %+v", controls)
	}
	report.Active.Controls.Sampling[0].Value = "snapshot changed"
	again, _ := ledger.Lookup(record.ID)
	if again.Active.Controls.Sampling[0].Value != "0" {
		t.Fatal("lookup returned ledger-owned controls")
	}
	encoded, _ := json.Marshal(again)
	for _, forbidden := range []string{"private-peer", "private-agent", "private-session", "http_status", "finished_at", "tokens", "timing", "\"stream\""} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("active report contains unavailable or private field %q: %s", forbidden, encoded)
		}
	}
	for _, phase := range []string{"waiting", "upstream"} {
		ledger.Track(record, phase)
		current, _ := ledger.Lookup(record.ID)
		if current.Phase != phase {
			t.Fatalf("phase=%q want %q", current.Phase, phase)
		}
		if current.Active.Stream == nil || !*current.Active.Stream {
			t.Fatal("decoded stream setting missing")
		}
		*current.Active.Stream = false
		again, _ := ledger.Lookup(record.ID)
		if again.Active.Stream == nil || !*again.Active.Stream {
			t.Fatal("lookup returned ledger-owned stream setting")
		}
	}
	done := true
	record.Completion = CompletionMetadata{Observation: CompletionAvailable, FinishReasons: []ChoiceFinishReason{{Index: 0, FinishReason: "stop"}}, StreamDone: &done}
	ledger.Add(record)
	completed, ok := ledger.Lookup(record.ID)
	if !ok || completed.State != "completed" || completed.Record == nil || completed.Active != nil || completed.Phase != "" {
		t.Fatalf("completed report=%+v retained=%t", completed, ok)
	}
	if len(ledger.active) != 0 {
		t.Fatal("completion retained active entry")
	}
	completed.Record.Controls.Reasoning[0].Value = "snapshot changed"
	completed.Record.Completion.FinishReasons[0].FinishReason = "length"
	*completed.Record.Completion.StreamDone = false
	completedAgain, _ := ledger.Lookup(record.ID)
	if completedAgain.Record.Controls.Reasoning[0].Value != "changed" {
		t.Fatal("completed lookup returned ledger-owned controls")
	}
	if completedAgain.Record.Completion.FinishReasons[0].FinishReason != "stop" || !*completedAgain.Record.Completion.StreamDone {
		t.Fatal("completed lookup returned ledger-owned completion metadata")
	}
}

func TestRequestReportActiveNonStreamingIsKnownAfterReceiving(t *testing.T) {
	var ledger requestLedger
	record := RequestRecord{ID: reportTestID(1)}
	ledger.Track(record, "receiving")
	receiving, _ := ledger.Lookup(record.ID)
	if receiving.Active.Stream != nil {
		t.Fatal("undecoded stream setting was invented")
	}
	ledger.Track(record, "waiting")
	waiting, _ := ledger.Lookup(record.ID)
	encoded, _ := json.Marshal(waiting)
	if waiting.Active.Stream == nil || *waiting.Active.Stream || !strings.Contains(string(encoded), `"stream":false`) {
		t.Fatalf("known non-streaming setting omitted: %s", encoded)
	}
}

func TestRequestReportLedgerCapacityEvictionAndOutOfOrderCompletion(t *testing.T) {
	var ledger requestLedger
	for index := 1; index <= ActiveRequestLimit; index++ {
		ledger.Track(RequestRecord{ID: reportTestID(index)}, "receiving")
	}
	untracked := RequestRecord{ID: reportTestID(ActiveRequestLimit + 1)}
	ledger.Track(untracked, "receiving")
	if len(ledger.active) != ActiveRequestLimit {
		t.Fatalf("active count=%d", len(ledger.active))
	}
	if _, ok := ledger.Lookup(untracked.ID); ok {
		t.Fatal("full cache retained another active request")
	}
	ledger.Track(RequestRecord{ID: reportTestID(1)}, "upstream")
	updated, _ := ledger.Lookup(reportTestID(1))
	if updated.Phase != "upstream" {
		t.Fatal("full cache prevented existing entry update")
	}
	// Even an untracked request still publishes an ordinary completion.
	ledger.Add(untracked)
	if completed, ok := ledger.Lookup(untracked.ID); !ok || completed.State != "completed" {
		t.Fatal("active cache capacity suppressed completion")
	}
	ledger.Untrack(reportTestID(1))
	if _, ok := ledger.Lookup(reportTestID(1)); ok {
		t.Fatal("untrack retained active entry")
	}
	// Ring retention follows completion order, not request-ID order.
	for index := RecentRequestLimit; index >= 1; index-- {
		ledger.Add(RequestRecord{ID: reportTestID(1000 + index)})
	}
	if _, ok := ledger.Lookup(untracked.ID); ok {
		t.Fatal("overwritten completion remained queryable")
	}
	if _, ok := ledger.Lookup(reportTestID(1000 + RecentRequestLimit)); !ok {
		t.Fatal("oldest retained out-of-order completion missing")
	}
	ledger.Add(RequestRecord{ID: reportTestID(2000)})
	if _, ok := ledger.Lookup(reportTestID(1000 + RecentRequestLimit)); ok {
		t.Fatal("evicted out-of-order completion remained queryable")
	}
	if retained, _ := ledger.Lookup(reportTestID(1001)); retained.State != "completed" {
		t.Fatal("newer completion lost")
	}
}

func TestRequestReportLedgerConcurrentSnapshots(t *testing.T) {
	var ledger requestLedger
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Go(func() {
			for index := 1; index <= 100; index++ {
				record := RequestRecord{ID: reportTestID(worker*100 + index), Controls: RequestControls{Sampling: []ObservedControl{{Name: "temperature", Value: "0"}}}}
				ledger.Track(record, "waiting")
				record.Controls.Sampling[0].Value = "1"
				if report, ok := ledger.Lookup(record.ID); ok && report.Active != nil {
					report.Active.Controls.Sampling[0].Value = "2"
				}
				ledger.Add(record)
				if report, ok := ledger.Lookup(record.ID); ok && report.Record != nil {
					report.Record.Controls.Sampling[0].Value = "3"
				}
				ledger.Untrack(record.ID)
			}
		})
	}
	workers.Wait()
	if len(ledger.active) != 0 || len(ledger.Recent(RecentRequestLimit)) != RecentRequestLimit {
		t.Fatal("concurrent completion changed retention bounds")
	}
}

func TestRequestReportHandlerUsesProtectedMetadataOnly(t *testing.T) {
	server := &Server{instanceID: reportTestInstance, registry: Registry{Fingerprint: "frozen-inventory"}}
	server.auth, _ = newBearerAuth(testAPIKey)
	server.resources = func() hostdoctor.ResourceSnapshot { panic("report sampled host resources") }
	record := RequestRecord{ID: reportTestID(1), Model: "fixture"}
	server.requests.Track(record, "waiting")
	request := httptest.NewRequest(http.MethodGet, requestReportPath+record.ID, unreadableAuthBody{})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated report status=%d", response.Code)
	}
	for _, completed := range []bool{false, true} {
		if completed {
			server.requests.Add(record)
		}
		request := httptest.NewRequest(http.MethodGet, requestReportPath+record.ID, unreadableAuthBody{})
		request.Header.Set("Authorization", "Bearer "+testAPIKey)
		response := httptest.NewRecorder()
		// No scheduler is installed: querying it would panic.
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("report status=%d headers=%v body=%s", response.Code, response.Header(), response.Body)
		}
		requireGatewayIdentity(t, response.Result())
		var report RequestReport
		if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Schema != RequestReportSchema || report.InstanceID != reportTestInstance || report.InventoryFingerprint != "frozen-inventory" || report.Retention.Completed != RecentRequestLimit || report.Retention.Active != ActiveRequestLimit {
			t.Fatalf("report envelope=%+v", report)
		}
		if completed && (report.State != "completed" || report.Record == nil) || !completed && (report.State != "active" || report.Phase != "waiting" || report.Active == nil) {
			t.Fatalf("report state=%+v", report)
		}
	}
	if len(server.requests.Recent(RecentRequestLimit)) != 1 {
		t.Fatal("report reads entered the completed ledger")
	}
}

func TestRequestReportHandlerValidatesSelectorsAndUnknownInstances(t *testing.T) {
	server := &Server{instanceID: reportTestInstance}
	valid := reportTestID(1)
	foreign := "rffffffffffffffffffffffffffffffff-0000000000000001"
	for _, test := range []struct {
		suffix string
		status int
		code   string
	}{
		{valid, http.StatusNotFound, "request_not_retained"},
		{foreign, http.StatusNotFound, "request_not_retained"},
		{"", http.StatusBadRequest, "invalid_request_report"},
		{"r00000001", http.StatusBadRequest, "invalid_request_report"},
		{reportTestID(0), http.StatusBadRequest, "invalid_request_report"},
		{strings.ToUpper(valid), http.StatusBadRequest, "invalid_request_report"},
		{valid + "/extra", http.StatusBadRequest, "invalid_request_report"},
		{valid + "?requests=1", http.StatusBadRequest, "invalid_request_report"},
		{valid + "?", http.StatusBadRequest, "invalid_request_report"},
		{valid + "?secret=private", http.StatusBadRequest, "invalid_request_report"},
	} {
		t.Run(test.suffix, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, requestReportPath+test.suffix, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d body=%s headers=%v", response.Code, response.Body, response.Header())
			}
			if strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), valid) || strings.Contains(response.Body.String(), foreign) {
				t.Fatal("controlled error echoed client selectors")
			}
		})
	}
}
