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
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	MaxRequestBytes = 16 * 1024 * 1024
	QueueLimit      = 64
	StatusSchema    = "paracetamol.gateway-status.v1"
)

type Server struct {
	registry  Registry
	scheduler *Scheduler
	transport *http.Transport
	log       io.Writer
	started   time.Time
	accepting atomic.Bool
}

type Status struct {
	Schema               string          `json:"schema"`
	Gateway              string          `json:"gateway"`
	InventoryFingerprint string          `json:"inventory_fingerprint"`
	StartedAt            string          `json:"started_at"`
	Applications         []string        `json:"applications"`
	Models               []StatusModel   `json:"models"`
	Scheduler            SchedulerStatus `json:"scheduler"`
}

type StatusModel struct {
	ID          string          `json:"id"`
	Application string          `json:"application"`
	State       AllocationState `json:"state"`
}

func NewServer(registry Registry, scheduler *Scheduler, log io.Writer) (*Server, error) {
	if scheduler == nil || len(registry.Models) == 0 {
		return nil, fmt.Errorf("gateway server requires a scheduler and model inventory")
	}
	if log == nil {
		log = os.Stderr
	}
	server := &Server{
		registry: registry, scheduler: scheduler, log: log, started: time.Now().UTC(),
		transport: &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			DialContext:       (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2: false, MaxIdleConns: 32, MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second,
		},
	}
	server.accepting.Store(true)
	return server, nil
}

func (server *Server) BeginShutdown() {
	server.accepting.Store(false)
	server.transport.CloseIdleConnections()
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
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
	models := make([]StatusModel, 0, len(server.registry.Models))
	for _, model := range server.registry.Models {
		if !seen[model.Application] {
			seen[model.Application] = true
			applications = append(applications, model.Application)
		}
		state := StateUnloaded
		if Allocation(model.Backend) == schedulerStatus.Allocation {
			state = schedulerStatus.State
		}
		models = append(models, StatusModel{ID: model.ID, Application: model.Application, State: state})
	}
	gatewayState := "ready"
	if !server.accepting.Load() {
		gatewayState = "shutting_down"
	}
	writeJSON(writer, http.StatusOK, Status{
		Schema: StatusSchema, Gateway: gatewayState, InventoryFingerprint: server.registry.Fingerprint,
		StartedAt: server.started.Format(time.RFC3339), Applications: applications, Models: models, Scheduler: schedulerStatus,
	})
}

func (server *Server) chat(writer http.ResponseWriter, request *http.Request) {
	if !server.accepting.Load() {
		writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "gateway_shutting_down", "Gateway is shutting down")
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, MaxRequestBytes+1))
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request_error", "invalid_body", "Request body could not be read")
		return
	}
	if len(body) > MaxRequestBytes {
		writeAPIError(writer, http.StatusRequestEntityTooLarge, "invalid_request_error", "request_too_large", "Request body exceeds the gateway limit")
		return
	}
	identifier, err := requestModel(body)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request_error", "invalid_model", err.Error())
		return
	}
	model, ok := server.registry.Lookup(identifier)
	if !ok {
		writeAPIError(writer, http.StatusNotFound, "invalid_request_error", "model_not_found", "The requested model is not available")
		return
	}
	lease, err := server.scheduler.Acquire(request.Context(), Allocation(model.Backend))
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return
		case errors.Is(err, ErrQueueFull):
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "gateway_queue_full", "Gateway request queue is full")
		case errors.Is(err, ErrShuttingDown):
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "gateway_shutting_down", "Gateway is shutting down")
		default:
			fmt.Fprintf(server.log, "gateway: start %s for model %s: %v\n", model.Application, model.ID, err)
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "backend_start_failed", "The selected model backend could not start")
		}
		return
	}
	defer lease.Release()
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	proxy := httputil.NewSingleHostReverseProxy(lease.Upstream)
	proxy.Transport = server.transport
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(output http.ResponseWriter, incoming *http.Request, proxyErr error) {
		if incoming.Context().Err() != nil {
			return
		}
		lease.Fail(proxyErr)
		fmt.Fprintf(server.log, "gateway: proxy %s through %s: %v\n", model.ID, model.Application, proxyErr)
		writeAPIError(output, http.StatusBadGateway, "gateway_error", "upstream_error", "The selected model backend disconnected")
	}
	proxy.ServeHTTP(writer, request)
}

func requestModel(body []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil {
		return "", errors.New("Request body must be one JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", errors.New("Request body contains trailing data")
	}
	raw, ok := object["model"]
	if !ok {
		return "", errors.New("Request body requires a model string")
	}
	var identifier string
	if err := json.Unmarshal(raw, &identifier); err != nil || strings.TrimSpace(identifier) == "" {
		return "", errors.New("Request body requires a non-empty model string")
	}
	return identifier, nil
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
