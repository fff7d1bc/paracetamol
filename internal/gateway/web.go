package gateway

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"strings"
	"time"

	"paracetamol/internal/clientconfig"
	"paracetamol/internal/identity"
	"paracetamol/internal/textmodel"
)

// Assets are compiled with the controller. Never serve a host directory or
// consult the catalog on a browser request. Make tracks these embedded inputs.
//
//go:embed assets/index.html assets/gateway.css assets/gateway.js
var webFiles embed.FS

var webTemplate = template.Must(template.ParseFS(webFiles, "assets/index.html"))

// serveWeb is the complete anonymous surface, deliberately before Bearer auth.
// The document contains no inventory, configuration, residency or host data.
func (server *Server) serveWeb(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	var body []byte
	var contentType string
	switch r.URL.Path {
	case "/":
		var rendered bytes.Buffer
		if err := webTemplate.Execute(&rendered, identity.DisplayName); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "gateway_error", "page_unavailable", "Gateway page is unavailable")
			return true
		}
		body, contentType = rendered.Bytes(), "text/html; charset=utf-8"
	case "/assets/gateway.css":
		body, _ = webFiles.ReadFile("assets/gateway.css")
		contentType = "text/css; charset=utf-8"
	case "/assets/gateway.js":
		body, _ = webFiles.ReadFile("assets/gateway.js")
		contentType = "text/javascript; charset=utf-8"
	default:
		return false
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
	return true
}

type webModel struct {
	Model
	Name                 string        `json:"name"`
	Family               string        `json:"family"`
	Publisher            string        `json:"publisher"`
	State                ModelState    `json:"state"`
	Runtime              *ModelRuntime `json:"runtime,omitempty"`
	MakiReasoningDefault string        `json:"maki_reasoning_default,omitempty"`
	RequestSlots         int           `json:"request_slots"`
}

type webSnapshot struct {
	CollectedAt  string     `json:"collected_at"`
	Gateway      string     `json:"gateway"`
	DefaultModel string     `json:"default_model,omitempty"`
	Models       []webModel `json:"models"`
}

func (server *Server) webData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "invalid_query", "Page data does not accept query parameters")
		return
	}
	status, err := server.scheduler.Status(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "gateway_error", "status_unavailable", "Gateway snapshot is unavailable")
		return
	}
	snapshot := webSnapshot{CollectedAt: time.Now().UTC().Format(time.RFC3339), Gateway: "ready", Models: []webModel{}}
	if !server.accepting.Load() {
		snapshot.Gateway = "shutting_down"
	}
	var eligible []textmodel.Model
	for _, model := range server.clientModels() {
		if model.AgentTools {
			eligible = append(eligible, model)
		}
	}
	if preferred, err := clientconfig.DefaultModel(eligible); err == nil {
		snapshot.DefaultModel = preferred.ID
	}
	residency := server.modelStatus(r.Context(), status)
	for index, model := range server.registry.Models {
		name, family, publisher := modelPresentation(model.ID)
		item := webModel{Model: model, Name: name, Family: family, Publisher: publisher, State: residency[index].State, Runtime: residency[index].Runtime}
		item.RequestSlots = 1
		if model.Backend == "llama-cpp" {
			item.RequestSlots = server.llamaSlots
		}
		if model.AgentTools {
			item.MakiReasoningDefault = clientconfig.ReasoningDefault(server.clientModel(model))
		}
		snapshot.Models = append(snapshot.Models, item)
	}
	writeJSON(w, http.StatusOK, snapshot)
}

// Presentation only. Unknown identities keep their exact spelling, and these
// labels never influence eligibility, routing, licenses or reasoning policy.
func modelPresentation(id string) (name, family, publisher string) {
	name, family, publisher = id, "Other models", "Unspecified"
	for _, prefix := range []string{"huihui-ai", "ggml-org", "mradermacher", "unsloth", "bartowski", "tencent", "qwen"} {
		if strings.HasPrefix(id, prefix+"-") {
			publisher = prefix
			name = strings.TrimPrefix(id, prefix+"-")
			break
		}
	}
	for _, group := range [][2]string{
		{"qwen3.8-flash-next-", "Qwen 3.8 Flash-Next"},
		{"qwen3.8-27b-", "Qwen 3.8 27B"},
		{"gemma4-", "Gemma 4"},
		{"qwen3-", "Qwen 3"},
	} {
		if strings.HasPrefix(name, group[0]) {
			family = group[1]
			name = family + " · " + strings.ReplaceAll(strings.TrimPrefix(name, group[0]), "-", " ")
			break
		}
	}
	if family == "Other models" {
		name = id
	}
	return
}
