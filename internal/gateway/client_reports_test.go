package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const clientReportInstance = "0123456789abcdef0123456789abcdef"
const clientReportID = "r" + clientReportInstance + "-0000000000000001"

func clientReportFixture(active bool) RequestReport {
	report := RequestReport{
		Schema: RequestReportSchema, InstanceID: clientReportInstance,
		InventoryFingerprint: strings.Repeat("a", 64),
		Retention:            RequestReportRetention{Completed: RecentRequestLimit, Active: ActiveRequestLimit},
	}
	if active {
		report.State, report.Phase = "active", "upstream"
		stream := true
		report.Active = &ActiveRequestReport{ID: clientReportID, Model: "reviewed-model", Application: "llama-cpp", Stream: &stream, StartedAt: "2026-10-20T10:00:00Z"}
	} else {
		report.State = "completed"
		report.Record = &RequestRecord{ID: clientReportID, Model: "reviewed-model", Application: "llama-cpp", Outcome: OutcomeSucceeded}
	}
	return report
}

func TestFetchRequestReportActiveAndCompleted(t *testing.T) {
	for _, active := range []bool{true, false} {
		name := "completed"
		if active {
			name = "active"
		}
		t.Run(name, func(t *testing.T) {
			want := clientReportFixture(active)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet || request.URL.Path != requestReportPath+clientReportID || request.URL.RawQuery != "" || request.URL.ForceQuery {
					t.Errorf("unexpected request: %s %s", request.Method, request.URL)
				}
				if request.Header.Get("Authorization") != "Bearer "+testAPIKey || request.Header.Get("Accept") != "application/json" {
					t.Errorf("unexpected metadata headers")
				}
				writer.Header().Set(IdentityHeader, IdentityValue)
				_ = json.NewEncoder(writer).Encode(want)
			}))
			defer server.Close()
			got, err := FetchRequestReport(context.Background(), server.URL+"/v1/", testAPIKey, clientReportID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != want.State || got.Schema != want.Schema || got.InstanceID != want.InstanceID || got.InventoryFingerprint != want.InventoryFingerprint || got.Retention != want.Retention {
				t.Fatalf("report=%#v", got)
			}
			if active && (got.Active == nil || got.Active.ID != clientReportID || got.Phase != "upstream" || got.Active.Stream == nil || !*got.Active.Stream) {
				t.Fatalf("active report=%#v", got)
			}
			if !active && (got.Record == nil || got.Record.ID != clientReportID || got.Record.Outcome != OutcomeSucceeded) {
				t.Fatalf("completed report=%#v", got)
			}
		})
	}
}

func TestFetchRequestReportRejectsInvalidSelectorBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, id := range []string{"", "r00000001", clientReportID + "/extra", clientReportID + "?requests=1", strings.ToUpper(clientReportID), "r" + clientReportInstance + "-0000000000000000"} {
		if _, err := FetchRequestReport(context.Background(), server.URL+"/v1", "", id); err == nil {
			t.Errorf("accepted selector %q", id)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid selectors made %d HTTP calls", calls.Load())
	}
}

func TestFetchRequestReportRejectsInconsistentResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		active bool
		change func(*RequestReport)
	}{
		{"schema", true, func(report *RequestReport) { report.Schema = "wrong" }},
		{"instance", true, func(report *RequestReport) { report.InstanceID = strings.Repeat("f", 32) }},
		{"active ID", true, func(report *RequestReport) { report.Active.ID = "r" + clientReportInstance + "-0000000000000002" }},
		{"completed ID", false, func(report *RequestReport) { report.Record.ID = "r" + clientReportInstance + "-0000000000000002" }},
		{"active missing", true, func(report *RequestReport) { report.Active = nil }},
		{"completed missing", false, func(report *RequestReport) { report.Record = nil }},
		{"active both", true, func(report *RequestReport) { report.Record = &RequestRecord{ID: clientReportID} }},
		{"completed both", false, func(report *RequestReport) { report.Active = &ActiveRequestReport{ID: clientReportID} }},
		{"active phase", true, func(report *RequestReport) { report.Phase = "generating" }},
		{"active phase missing", true, func(report *RequestReport) { report.Phase = "" }},
		{"completed phase", false, func(report *RequestReport) { report.Phase = "upstream" }},
		{"state", false, func(report *RequestReport) { report.State = "failed" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := clientReportFixture(test.active)
			test.change(&report)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set(IdentityHeader, IdentityValue)
				_ = json.NewEncoder(writer).Encode(report)
			}))
			defer server.Close()
			if _, err := FetchRequestReport(context.Background(), server.URL+"/v1", "", clientReportID); err == nil || errors.Is(err, ErrRequestNotRetained) {
				t.Fatalf("invalid report error=%v", err)
			}
		})
	}
}

func TestFetchRequestReportRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{`{"schema":`, `null`, `[]`, `{} {}`, strings.Repeat(" ", 2*1024*1024+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set(IdentityHeader, IdentityValue)
			_, _ = io.WriteString(writer, body)
		}))
		_, err := FetchRequestReport(context.Background(), server.URL+"/v1", "", clientReportID)
		server.Close()
		if err == nil {
			t.Fatal("accepted malformed or oversized response")
		}
	}
}

func TestFetchRequestReportNotRetainedAndAuthentication(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		marker string
		want   string
		absent bool
	}{
		{"not retained", http.StatusNotFound, IdentityValue, "request report is not retained", true},
		{"foreign 404", http.StatusNotFound, "", "not a", false},
		{"wrong marker", http.StatusOK, "different", "not a", false},
		{"unauthorized", http.StatusUnauthorized, IdentityValue, "authentication failed", false},
		{"backend error", http.StatusServiceUnavailable, IdentityValue, "gateway returned HTTP 503", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if test.marker != "" {
					writer.Header().Set(IdentityHeader, test.marker)
				}
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, `{"private":"do not echo server bodies"}`)
			}))
			defer server.Close()
			_, err := FetchRequestReport(context.Background(), server.URL+"/v1", testAPIKey, clientReportID)
			if err == nil || !strings.Contains(err.Error(), test.want) || errors.Is(err, ErrRequestNotRetained) != test.absent || strings.Contains(err.Error(), "private") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestFetchRequestReportDoesNotFollowRedirects(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	for _, sameHost := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/redirected" {
				forwarded.Add(1)
				return
			}
			writer.Header().Set(IdentityHeader, IdentityValue)
			location := target.URL + "/redirected"
			if sameHost {
				location = "/redirected"
			}
			http.Redirect(writer, request, location, http.StatusFound)
		}))
		_, err := FetchRequestReport(context.Background(), server.URL+"/v1", testAPIKey, clientReportID)
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
			t.Fatalf("redirect error=%v", err)
		}
	}
	if forwarded.Load() != 0 {
		t.Fatalf("redirect forwarded %d requests", forwarded.Load())
	}
}

func TestFetchRequestReportHonorsCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(IdentityHeader, IdentityValue)
		_ = json.NewEncoder(writer).Encode(clientReportFixture(false))
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchRequestReport(ctx, server.URL+"/v1", "", clientReportID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error=%v", err)
	}
}
