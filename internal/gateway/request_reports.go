package gateway

import (
	"net/http"
	"strings"
)

const (
	RequestReportSchema = "paracetamol.request-report.v1"
	ActiveRequestLimit  = 128
	requestReportPath   = "/paracetamol/v1/requests/"
)

// ActiveRequestReport contains only metadata known before a request completes.
// In particular, it does not turn unobserved outcomes or timings into zeros.
type ActiveRequestReport struct {
	ID          string          `json:"id"`
	Model       string          `json:"model,omitempty"`
	Application string          `json:"application,omitempty"`
	Stream      *bool           `json:"stream,omitempty"`
	StartedAt   string          `json:"started_at"`
	Controls    RequestControls `json:"controls"`
}

type activeRequestEntry struct {
	phase  string
	report ActiveRequestReport
}

type RequestReportRetention struct {
	Completed int `json:"completed"`
	Active    int `json:"active"`
}

type RequestReport struct {
	Schema               string                 `json:"schema"`
	InstanceID           string                 `json:"instance_id"`
	InventoryFingerprint string                 `json:"inventory_fingerprint"`
	State                string                 `json:"state"`
	Phase                string                 `json:"phase,omitempty"`
	Active               *ActiveRequestReport   `json:"active,omitempty"`
	Record               *RequestRecord         `json:"record,omitempty"`
	Retention            RequestReportRetention `json:"retention"`
}

func cloneRequestControls(controls RequestControls) RequestControls {
	controls.Sampling = append([]ObservedControl(nil), controls.Sampling...)
	controls.Reasoning = append([]ObservedControl(nil), controls.Reasoning...)
	controls.Diagnostics = append([]string(nil), controls.Diagnostics...)
	return controls
}

// Track is best-effort observability, never admission control. A full active
// cache must not reject, delay, or evict an otherwise runnable request.
func (ledger *requestLedger) Track(record RequestRecord, phase string) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if _, exists := ledger.active[record.ID]; !exists && len(ledger.active) >= ActiveRequestLimit {
		return
	}
	if ledger.active == nil {
		ledger.active = make(map[string]activeRequestEntry)
	}
	report := ActiveRequestReport{
		ID: record.ID, Model: record.Model, Application: record.Application,
		StartedAt: record.StartedAt, Controls: cloneRequestControls(record.Controls),
	}
	// The body has not been decoded in receiving. False would misreport an
	// unknown stream setting as an observed non-streaming request.
	if phase != "receiving" {
		stream := record.Stream
		report.Stream = &stream
	}
	ledger.active[record.ID] = activeRequestEntry{phase: phase, report: report}
}

func (ledger *requestLedger) Untrack(id string) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	delete(ledger.active, id)
}

// Lookup shares the completed ring rather than creating a second retention
// policy. Its snapshots own mutable slices so encoding cannot race updates.
func (ledger *requestLedger) Lookup(id string) (RequestReport, bool) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	for _, entry := range ledger.entries {
		if entry.sequence != 0 && entry.record.ID == id {
			record := entry.record
			record.Controls = cloneRequestControls(record.Controls)
			record.Completion.FinishReasons = append([]ChoiceFinishReason(nil), record.Completion.FinishReasons...)
			if record.Completion.StreamDone != nil {
				done := *record.Completion.StreamDone
				record.Completion.StreamDone = &done
			}
			return RequestReport{State: "completed", Record: &record}, true
		}
	}
	if entry, ok := ledger.active[id]; ok {
		active := entry.report
		active.Controls = cloneRequestControls(active.Controls)
		if active.Stream != nil {
			stream := *active.Stream
			active.Stream = &stream
		}
		return RequestReport{State: "active", Phase: entry.phase, Active: &active}, true
	}
	return RequestReport{}, false
}

func (server *Server) requestReport(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	id := strings.TrimPrefix(request.URL.Path, requestReportPath)
	if request.URL.RawQuery != "" || request.URL.ForceQuery || !validRequestReportID(id) {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request_error", "invalid_request_report", "Request reports require one canonical request ID and no query parameters")
		return
	}
	// Epoch-qualified IDs cannot accidentally resolve to a new process's request.
	if id[1:33] != server.instanceID {
		writeRequestNotRetained(writer)
		return
	}
	report, ok := server.requests.Lookup(id)
	if !ok {
		writeRequestNotRetained(writer)
		return
	}
	report.Schema = RequestReportSchema
	report.InstanceID = server.instanceID
	report.InventoryFingerprint = server.registry.Fingerprint
	report.Retention = RequestReportRetention{Completed: RecentRequestLimit, Active: ActiveRequestLimit}
	writeJSON(writer, http.StatusOK, report)
}

func writeRequestNotRetained(writer http.ResponseWriter) {
	writeAPIError(writer, http.StatusNotFound, "not_found_error", "request_not_retained", "Request report is not retained; it may be unknown, expired, from another gateway instance, or outside the bounded active cache")
}

func validRequestReportID(id string) bool {
	if len(id) != 50 || id[0] != 'r' || id[33] != '-' {
		return false
	}
	nonzero := false
	for index := 1; index < len(id); index++ {
		if index == 33 {
			continue
		}
		character := id[index]
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
		if index > 33 && character != '0' {
			nonzero = true
		}
	}
	return nonzero
}
