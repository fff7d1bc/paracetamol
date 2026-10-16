package gateway

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"paracetamol/internal/clientconfig"
	"paracetamol/internal/config"
	"paracetamol/internal/textmodel"
)

const makiConfigPath = "/paracetamol/v1/clients/maki/"

func (server *Server) makiConfig(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	endpoint, err := clientConfigURL(request)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request_error", "invalid_base_url", err.Error())
		return
	}
	models := make([]textmodel.Model, 0, len(server.registry.Models))
	for _, model := range server.registry.Models {
		models = append(models, textmodel.Model{
			ID: model.ID, Context: model.Context, MaxOutputTokens: model.MaxOutputTokens,
			AgentTools: model.AgentTools, ReasoningControl: model.ReasoningControl,
			ReasoningLevels: model.ReasoningLevels, ReasoningDefault: model.ReasoningDefault,
			ReasoningOff: model.ReasoningOff,
		})
	}
	// Only the immutable startup registry is consulted. Exporting configuration
	// must neither acquire an allocation nor revalidate model files on disk.
	files, err := clientconfig.Maki(models, endpoint, server.auth.enabled)
	if err != nil {
		writeAPIError(writer, http.StatusConflict, "gateway_error", "no_client_models", err.Error())
		return
	}
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.URL.Path == makiConfigPath+"providers.toml" {
		writer.Header().Set("Content-Type", "application/toml; charset=utf-8")
		writer.Header().Set("Content-Disposition", `attachment; filename="paracetamol-providers.toml"`)
		_, _ = writer.Write(files.Providers)
	} else {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Content-Disposition", `attachment; filename="paracetamol-init.lua"`)
		_, _ = writer.Write(files.Init)
	}
}

func clientConfigURL(request *http.Request) (string, error) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(query) > 1 || len(query["base_url"]) > 1 {
		return "", fmt.Errorf("configuration export accepts only one optional base_url")
	}
	endpoint := ""
	if len(query) != 0 {
		values, ok := query["base_url"]
		if !ok || len(values) != 1 || values[0] == "" {
			return "", fmt.Errorf("configuration export accepts only a non-empty base_url")
		}
		endpoint = values[0]
	} else {
		scheme := "http"
		if request.TLS != nil {
			scheme = "https"
		}
		// Do not trust Forwarded/X-Forwarded-* headers. A TLS-terminating proxy
		// can use explicit base_url, without adding a proxy trust configuration.
		if strings.ContainsAny(request.Host, "/?#@\\") {
			return "", fmt.Errorf("invalid gateway request authority")
		}
		endpoint = scheme + "://" + request.Host + "/v1"
	}
	endpoint, err = config.NormalizeGatewayURL(endpoint)
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(endpoint)
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsUnspecified() {
		return "", fmt.Errorf("use a client-reachable address, not an unspecified bind address")
	}
	return endpoint, nil
}
