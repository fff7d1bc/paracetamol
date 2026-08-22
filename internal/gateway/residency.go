package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"paracetamol/internal/textmodel"
)

const maxResidencyResponseBytes = 2 * 1024 * 1024

type ModelState string

const (
	ModelUnloaded ModelState = "unloaded"
	ModelLoading  ModelState = "loading"
	ModelLoaded   ModelState = "loaded"
	ModelSleeping ModelState = "sleeping"
	ModelFailed   ModelState = "failed"
	ModelUnknown  ModelState = "unknown"
)

type routerModelList struct {
	Data []struct {
		ID     string          `json:"id"`
		Status json.RawMessage `json:"status"`
	} `json:"data"`
}

type routerModelStatus struct {
	Value  string `json:"value"`
	Failed bool   `json:"failed"`
}

func (server *Server) modelStatus(ctx context.Context, scheduler SchedulerStatus) []StatusModel {
	models := make([]StatusModel, 0, len(server.registry.Models))
	for _, model := range server.registry.Models {
		models = append(models, StatusModel{ID: model.ID, Application: model.Application, State: ModelUnloaded})
	}
	if scheduler.Allocation == "" {
		return models
	}
	switch scheduler.Allocation {
	case AllocationDwarfStar:
		state, diagnostic := dwarfStarModelState(scheduler.State)
		for index := range models {
			if server.registry.Models[index].Backend == textmodel.BackendDwarfStar {
				models[index].State = state
				models[index].Diagnostic = diagnostic
			}
		}
	case AllocationLlamaCPP:
		if scheduler.Upstream == "" {
			for index := range models {
				if server.registry.Models[index].Backend == textmodel.BackendLlamaCPP {
					models[index].State = ModelUnknown
					models[index].Diagnostic = "llama.cpp router residency is unavailable while the backend changes state"
				}
			}
			return models
		}
		states, diagnostics := server.probeLlamaResidency(ctx, scheduler.Upstream)
		for index := range models {
			if server.registry.Models[index].Backend != textmodel.BackendLlamaCPP {
				continue
			}
			identifier := models[index].ID
			models[index].State = states[identifier]
			models[index].Diagnostic = diagnostics[identifier]
		}
	}
	return models
}

func dwarfStarModelState(state AllocationState) (ModelState, string) {
	switch state {
	case StateStarting:
		return ModelLoading, ""
	case StateReady, StateDraining:
		return ModelLoaded, ""
	case StateFailed:
		return ModelFailed, ""
	case StateStopping:
		return ModelUnknown, "DwarfStar residency is unavailable while the backend stops"
	default:
		return ModelUnknown, "DwarfStar residency is unavailable"
	}
}

// probeLlamaResidency deliberately constructs the router's read-only /models
// endpoint with no query. The narrow decode discards paths, child arguments,
// presets, and entries outside the frozen gateway registry.
func (server *Server) probeLlamaResidency(ctx context.Context, rawUpstream string) (map[string]ModelState, map[string]string) {
	states, diagnostics := server.unknownLlamaResidency("llama.cpp router model status is unavailable")
	upstream, err := url.Parse(rawUpstream)
	if err != nil || upstream.Scheme != "http" || upstream.Host == "" {
		return states, diagnostics
	}
	endpoint := *upstream
	endpoint.Path = "/models"
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	probeContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(probeContext, http.MethodGet, endpoint.String(), nil)
	response, err := server.residencyClient.Do(request)
	if err != nil {
		return states, diagnostics
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return states, diagnostics
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResidencyResponseBytes+1))
	if err != nil || len(body) > maxResidencyResponseBytes {
		return states, diagnostics
	}
	var listed routerModelList
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&listed); err != nil {
		return states, diagnostics
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return states, diagnostics
	}
	seen := map[string]bool{}
	for _, listedModel := range listed.Data {
		model, known := server.registry.Lookup(listedModel.ID)
		if !known || model.Backend != textmodel.BackendLlamaCPP {
			continue
		}
		if seen[listedModel.ID] {
			states[listedModel.ID] = ModelUnknown
			diagnostics[listedModel.ID] = "llama.cpp router reported the model more than once"
			continue
		}
		seen[listedModel.ID] = true
		state, diagnostic := decodeRouterModelStatus(listedModel.Status)
		states[listedModel.ID] = state
		diagnostics[listedModel.ID] = diagnostic
	}
	for identifier := range states {
		if !seen[identifier] {
			diagnostics[identifier] = "llama.cpp router did not report this frozen model"
		}
	}
	return states, diagnostics
}

func (server *Server) unknownLlamaResidency(diagnostic string) (map[string]ModelState, map[string]string) {
	states := map[string]ModelState{}
	diagnostics := map[string]string{}
	for _, model := range server.registry.Models {
		if model.Backend == textmodel.BackendLlamaCPP {
			states[model.ID] = ModelUnknown
			diagnostics[model.ID] = diagnostic
		}
	}
	return states, diagnostics
}

func decodeRouterModelStatus(raw json.RawMessage) (ModelState, string) {
	var status routerModelStatus
	if len(raw) == 0 || json.Unmarshal(raw, &status) != nil {
		return ModelUnknown, "llama.cpp router returned invalid model status"
	}
	if status.Failed {
		return ModelFailed, ""
	}
	switch strings.ToLower(status.Value) {
	case "unloaded":
		return ModelUnloaded, ""
	case "downloading", "downloaded", "loading":
		return ModelLoading, ""
	case "loaded":
		return ModelLoaded, ""
	case "sleeping":
		return ModelSleeping, ""
	case "failed":
		return ModelFailed, ""
	default:
		return ModelUnknown, "llama.cpp router returned unsupported model status"
	}
}
