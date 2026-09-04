package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"paracetamol/internal/identity"
)

const (
	MaxRequestBytes = 16 * 1024 * 1024
	QueueLimit      = 64
	StatusSchema    = "paracetamol.gateway-status.v4"
	IdentityHeader  = "X-Paracetamol-Gateway"
	IdentityValue   = "paracetamol.gateway.v1"
)

type Server struct {
	registry        Registry
	scheduler       *Scheduler
	transport       *http.Transport
	residencyClient *http.Client
	log             io.Writer
	started         time.Time
	accepting       atomic.Bool
	nextRequestID   atomic.Uint64
	requests        requestLedger
}

type Status struct {
	Schema               string          `json:"schema"`
	Gateway              string          `json:"gateway"`
	InventoryFingerprint string          `json:"inventory_fingerprint"`
	StartedAt            string          `json:"started_at"`
	Applications         []string        `json:"applications"`
	Models               []StatusModel   `json:"models"`
	Scheduler            SchedulerStatus `json:"scheduler"`
	Usage                UsageSummary    `json:"usage"`
	RecentRequests       []RequestRecord `json:"recent_requests,omitempty"`
}

type StatusModel struct {
	ID                string        `json:"id"`
	Application       string        `json:"application"`
	ConfiguredContext int64         `json:"configured_context"`
	State             ModelState    `json:"state"`
	Runtime           *ModelRuntime `json:"runtime,omitempty"`
	Diagnostic        string        `json:"diagnostic,omitempty"`
}

type ModelRuntime struct {
	Context         *int64  `json:"context,omitempty"`
	TrainingContext *int64  `json:"training_context,omitempty"`
	Parameters      *uint64 `json:"parameters,omitempty"`
	ModelBytes      *uint64 `json:"model_bytes,omitempty"`
	Quantization    string  `json:"quantization,omitempty"`
}

func NewServer(registry Registry, scheduler *Scheduler, log io.Writer) (*Server, error) {
	if scheduler == nil || len(registry.Models) == 0 {
		return nil, fmt.Errorf("gateway server requires a scheduler and model inventory")
	}
	if log == nil {
		log = os.Stderr
	}
	sharedLog := atomicLogWriter(log)
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: false, MaxIdleConns: 32, MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second,
	}
	residencyTransport := transport.Clone()
	residencyTransport.Proxy = nil
	server := &Server{
		registry: registry, scheduler: scheduler, log: sharedLog, started: time.Now().UTC(), transport: transport,
		residencyClient: &http.Client{Transport: residencyTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	server.accepting.Store(true)
	return server, nil
}

func (server *Server) BeginShutdown() {
	server.accepting.Store(false)
	server.transport.CloseIdleConnections()
	if transport, ok := server.residencyClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Server", identity.CommandName+"/"+identity.Version)
	writer.Header().Set(IdentityHeader, IdentityValue)
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/health":
		server.health(writer)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/models":
		server.models(writer)
	case request.Method == http.MethodGet && request.URL.Path == "/paracetamol/v1/status":
		server.status(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/chat/completions":
		server.chat(writer, request)
	default:
		writeAPIError(writer, http.StatusNotFound, "not_found_error", "not_found", "File Not Found")
	}
}

func (server *Server) health(writer http.ResponseWriter) {
	if !server.accepting.Load() {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "shutting_down"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) models(writer http.ResponseWriter) {
	data := make([]map[string]any, 0, len(server.registry.Models))
	for _, model := range server.registry.Models {
		data = append(data, map[string]any{"id": model.ID, "object": "model", "created": 0, "owned_by": "paracetamol"})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (server *Server) status(writer http.ResponseWriter, request *http.Request) {
	recentLimit, err := recentRequestLimit(request.URL.RawQuery)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request_error", "invalid_requests", err.Error())
		return
	}
	schedulerStatus, err := server.scheduler.Status(request.Context())
	if err != nil && !errors.Is(err, ErrShuttingDown) {
		writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "status_unavailable", "Gateway status is unavailable")
		return
	}
	if schedulerStatus.LastError != "" {
		schedulerStatus.LastError = "backend operation failed; inspect gateway stderr"
	}
	applications := []string{}
	seen := map[string]bool{}
	for _, model := range server.registry.Models {
		if !seen[model.Application] {
			seen[model.Application] = true
			applications = append(applications, model.Application)
		}
	}
	gatewayState := "ready"
	if !server.accepting.Load() {
		gatewayState = "shutting_down"
	}
	recentRequests, usage := server.requests.Snapshot(recentLimit)
	writeJSON(writer, http.StatusOK, Status{
		Schema: StatusSchema, Gateway: gatewayState, InventoryFingerprint: server.registry.Fingerprint,
		StartedAt: server.started.Format(time.RFC3339), Applications: applications,
		Models: server.modelStatus(request.Context(), schedulerStatus), Scheduler: schedulerStatus, Usage: usage,
		RecentRequests: recentRequests,
	})
}

func (server *Server) chat(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	record := RequestRecord{
		ID: fmt.Sprintf("r%08x", server.nextRequestID.Add(1)), SessionID: requestSessionID(request), Peer: requestPeer(request),
		UserAgent: boundedText(request.UserAgent(), maxObservedTextRunes), StartedAt: started.UTC().Format(time.RFC3339Nano),
	}
	writer.Header().Set(RequestIDHeader, record.ID)
	reject := func(status int, outcome RequestOutcome, kind, code, message string) {
		writeAPIError(writer, status, kind, code, message)
		record.Outcome, record.HTTPStatus = outcome, status
		server.completeRequest(&record, started, time.Time{}, time.Time{})
		logLine(server.log, "gateway | request %s reject outcome=%s session=%s status=%d model=%s peer=%s total=%dms",
			record.ID, outcome, firstNonEmptyLog(record.SessionID, "none"), status, firstNonEmptyLog(record.Model, "unknown"), firstNonEmptyLog(record.Peer, "unknown"), record.Timing.TotalMilliseconds)
	}
	if !server.accepting.Load() {
		reject(http.StatusServiceUnavailable, OutcomeRejected, "gateway_error", "gateway_shutting_down", "Gateway is shutting down")
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, MaxRequestBytes+1))
	if err != nil {
		if request.Context().Err() != nil {
			record.Outcome = OutcomeCanceled
			server.completeRequest(&record, started, time.Time{}, time.Time{})
			logLine(server.log, "gateway | request %s finish outcome=%s status=0 total=%dms", record.ID, record.Outcome, record.Timing.TotalMilliseconds)
			return
		}
		reject(http.StatusBadRequest, OutcomeRejected, "invalid_request_error", "invalid_body", "Request body could not be read")
		return
	}
	if len(body) > MaxRequestBytes {
		reject(http.StatusRequestEntityTooLarge, OutcomeRejected, "invalid_request_error", "request_too_large", "Request body exceeds the gateway limit")
		return
	}
	observation, err := inspectRequest(body)
	if err != nil {
		reject(http.StatusBadRequest, OutcomeRejected, "invalid_request_error", "invalid_model", err.Error())
		return
	}
	record.Stream = observation.Stream
	record.Controls = observation.Controls
	model, ok := server.registry.Lookup(observation.Model)
	if !ok {
		reject(http.StatusNotFound, OutcomeRejected, "invalid_request_error", "model_not_found", "The requested model is not available")
		return
	}
	record.Model, record.Application = model.ID, model.Application
	logLine(server.log, "gateway | request %s start session=%s model=%s application=%s peer=%s stream=%t %s",
		record.ID, firstNonEmptyLog(record.SessionID, "none"), model.ID, model.Application, firstNonEmptyLog(record.Peer, "unknown"), record.Stream, controlsLogSummary(record.Controls))
	waitStarted := time.Now()
	lease, err := server.scheduler.Acquire(request.Context(), Allocation(model.Backend))
	waitFinished := time.Now()
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			record.Outcome = OutcomeCanceled
		case errors.Is(err, ErrQueueFull):
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "gateway_queue_full", "Gateway request queue is full")
			record.Outcome, record.HTTPStatus = OutcomeQueueFull, http.StatusServiceUnavailable
		case errors.Is(err, ErrShuttingDown):
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "gateway_shutting_down", "Gateway is shutting down")
			record.Outcome, record.HTTPStatus = OutcomeRejected, http.StatusServiceUnavailable
		default:
			logLine(server.log, "gateway | request %s backend start %s: %v", record.ID, model.Application, err)
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "backend_start_failed", "The selected model backend could not start")
			record.Outcome, record.HTTPStatus = OutcomeBackendStartFailed, http.StatusServiceUnavailable
		}
		server.completeRequest(&record, started, waitStarted, waitFinished)
		server.logRequestFinish(record)
		return
	}
	defer lease.Release()
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	// Early gateway errors use the header installed before body parsing. A
	// proxied response receives it through ModifyResponse instead; remove the
	// early copy so ReverseProxy's additive header transfer cannot duplicate it.
	writer.Header().Del(RequestIDHeader)
	observedWriter := &statusResponseWriter{ResponseWriter: writer}
	var observer *responseObserver
	proxyFailed := false
	proxy := httputil.NewSingleHostReverseProxy(lease.Upstream)
	originalDirector := proxy.Director
	proxy.Director = func(outgoing *http.Request) {
		originalDirector(outgoing)
		outgoing.Header.Del(SessionIDHeader)
	}
	proxy.Transport = server.transport
	proxy.FlushInterval = -1
	proxy.ModifyResponse = func(response *http.Response) error {
		// The public endpoint identifies the gateway, not whichever private
		// backend served this request.
		response.Header.Del("Server")
		response.Header.Del(IdentityHeader)
		response.Header.Set(RequestIDHeader, record.ID)
		if isEventStream(response.Header.Get("Content-Type")) {
			response.Header.Set("X-Accel-Buffering", "no")
		}
		observer = newResponseObserver(response.Header.Get("Content-Type"))
		response.Body = &observedBody{ReadCloser: response.Body, observer: observer}
		return nil
	}
	proxy.ErrorHandler = func(output http.ResponseWriter, incoming *http.Request, proxyErr error) {
		if incoming.Context().Err() != nil {
			return
		}
		proxyFailed = true
		lease.Fail(proxyErr)
		logLine(server.log, "gateway | request %s proxy %s through %s: %v", record.ID, model.ID, model.Application, proxyErr)
		output.Header().Set(RequestIDHeader, record.ID)
		writeAPIError(output, http.StatusBadGateway, "gateway_error", "upstream_error", "The selected model backend disconnected")
	}
	upstreamStarted := time.Now()
	proxyAborted := serveReverseProxy(proxy, observedWriter, request)
	upstreamFinished := time.Now()
	if proxyAborted && request.Context().Err() == nil {
		proxyFailed = true
		lease.Fail(errors.New("upstream response stream aborted"))
		logLine(server.log, "gateway | request %s proxy %s through %s: response stream aborted", record.ID, model.ID, model.Application)
	}
	if observer != nil {
		observation := observer.Finish()
		record.Tokens, record.BackendTimings = observation.Tokens, observation.Timings
	}
	record.HTTPStatus = observedWriter.status
	switch {
	case request.Context().Err() != nil:
		record.Outcome = OutcomeCanceled
	case proxyFailed || record.HTTPStatus >= http.StatusBadRequest:
		record.Outcome = OutcomeUpstreamError
	default:
		record.Outcome = OutcomeSucceeded
	}
	record.Timing.UpstreamMilliseconds = elapsedMilliseconds(upstreamStarted, upstreamFinished)
	server.completeRequest(&record, started, waitStarted, waitFinished)
	server.logRequestFinish(record)
}

// ReverseProxy uses http.ErrAbortHandler to terminate a response whose stream
// breaks after headers were committed. Contain that sentinel so the gateway
// can release the lease normally and still publish its privacy-safe finish
// record; any unrelated panic remains a programming error.
func serveReverseProxy(proxy *httputil.ReverseProxy, writer http.ResponseWriter, request *http.Request) (aborted bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				aborted = true
				return
			}
			panic(recovered)
		}
	}()
	proxy.ServeHTTP(writer, request)
	return false
}

func recentRequestLimit(rawQuery string) (int, error) {
	if rawQuery == "" {
		return 0, nil
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil || len(values) != 1 || len(values["requests"]) != 1 {
		return 0, fmt.Errorf("status accepts only one requests query parameter")
	}
	limit, err := strconv.Atoi(values.Get("requests"))
	if err != nil || limit < 1 || limit > RecentRequestLimit {
		return 0, fmt.Errorf("requests must be from 1 through %d", RecentRequestLimit)
	}
	return limit, nil
}

func (server *Server) completeRequest(record *RequestRecord, started, waitStarted, waitFinished time.Time) {
	finished := time.Now()
	record.FinishedAt = finished.UTC().Format(time.RFC3339Nano)
	record.Timing.TotalMilliseconds = elapsedMilliseconds(started, finished)
	if !waitStarted.IsZero() && !waitFinished.IsZero() {
		record.Timing.GatewayWaitMilliseconds = elapsedMilliseconds(waitStarted, waitFinished)
	}
	server.requests.Add(*record)
}

func (server *Server) logRequestFinish(record RequestRecord) {
	tokens := "unavailable"
	if record.Tokens.Input != nil || record.Tokens.Output != nil || record.Tokens.Cached != nil || record.Tokens.Reasoning != nil {
		tokens = fmt.Sprintf("in:%s,out:%s,cached:%s,reasoning:%s", tokenLogValue(record.Tokens.Input), tokenLogValue(record.Tokens.Output), tokenLogValue(record.Tokens.Cached), tokenLogValue(record.Tokens.Reasoning))
	}
	logLine(server.log, "gateway | request %s finish outcome=%s session=%s status=%d wait=%dms upstream=%dms total=%dms tokens=%s",
		record.ID, record.Outcome, firstNonEmptyLog(record.SessionID, "none"), record.HTTPStatus, record.Timing.GatewayWaitMilliseconds,
		record.Timing.UpstreamMilliseconds, record.Timing.TotalMilliseconds, tokens)
}

func tokenLogValue(value *int64) string {
	if value == nil {
		return "?"
	}
	return strconv.FormatInt(*value, 10)
}

func elapsedMilliseconds(start, finish time.Time) int64 {
	if start.IsZero() || finish.IsZero() || finish.Before(start) {
		return 0
	}
	return finish.Sub(start).Milliseconds()
}

func firstNonEmptyLog(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func writeAPIError(writer http.ResponseWriter, status int, kind, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"message": message, "type": kind, "code": code}})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		http.Error(writer, "internal error", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(append(encoded, '\n'))
}
