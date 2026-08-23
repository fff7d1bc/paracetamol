package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/gateway"
	"paracetamol/internal/identity"
	"paracetamol/internal/storage"
	"paracetamol/internal/textmodel"
)

type PiPlan struct {
	Command           []string
	RuntimeRoot       string
	DefaultProvider   string
	DefaultModel      string
	DefaultThinking   string
	Endpoint          string
	Config            []byte
	ModelPicker       []byte
	CompletionDivider []byte
	Mode              string
	Remote            bool
}

func NormalizeGatewayURL(value string) (string, error) {
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r <= 32 }) >= 0 {
		return "", fmt.Errorf("gateway URL must not be empty or contain whitespace")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("gateway URL must be a credential-free HTTP(S) URL")
	}
	if parsed.Port() != "" {
		port, err := parsePort(parsed.Port())
		if err != nil || port == 0 {
			return "", fmt.Errorf("invalid gateway URL port")
		}
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path != "/v1" {
		return "", fmt.Errorf("gateway URL path must be /v1")
	}
	parsed.Path, parsed.RawPath = "/v1", ""
	return parsed.String(), nil
}

func DiscoverGatewayModels(ctx context.Context, endpoint string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cannot reach gateway model inventory: %w", err)
	}
	defer response.Body.Close()
	markers := response.Header.Values(gateway.IdentityHeader)
	if len(markers) != 1 || markers[0] != gateway.IdentityValue {
		return nil, fmt.Errorf("%s is not a %s gateway (HTTP %d)", endpoint, identity.DisplayName, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway model inventory returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		return nil, fmt.Errorf("gateway model inventory is unreadable or too large")
	}
	var document struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Data == nil {
		return nil, fmt.Errorf("gateway returned an invalid model inventory")
	}
	seen := make(map[string]bool)
	var result []string
	for _, item := range document.Data {
		if item.ID != "" && !seen[item.ID] {
			seen[item.ID] = true
			result = append(result, item.ID)
		}
	}
	return result, nil
}

func PiConfig(managed catalog.Catalog, endpoint string, selected []textmodel.Model) ([]byte, error) {
	models := make([]map[string]any, 0, len(selected))
	for _, model := range selected {
		entry := map[string]any{
			"id": model.ID, "name": model.ID, "reasoning": model.ReasoningControl != "",
			"input": []string{"text"}, "contextWindow": model.Context,
			"maxTokens": model.MaxOutputTokens, "cost": zeroCost(),
		}
		if sampling := ClientSampling(managed, model.ID); len(sampling) > 0 {
			entry["samplingParams"] = sampling
		}
		if model.ReasoningControl != "" {
			levels := map[string]any{"off": nil, "minimal": nil, "low": nil, "medium": nil, "high": nil, "xhigh": nil, "max": nil}
			if model.ReasoningOff {
				levels["off"] = "none"
			}
			if model.ReasoningControl == "toggle" {
				levels["high"] = "high"
			} else {
				for _, level := range model.ReasoningLevels {
					levels[level] = level
				}
			}
			entry["thinkingLevelMap"] = levels
			switch model.ReasoningControl {
			case "toggle":
				entry["compat"] = map[string]any{"thinkingFormat": "qwen-chat-template", "supportsReasoningEffort": false}
			case "effort":
				entry["compat"] = map[string]any{"thinkingFormat": "openai", "supportsReasoningEffort": true}
			default:
				entry["compat"] = map[string]any{"thinkingFormat": "chat-template", "supportsReasoningEffort": false, "chatTemplateKwargs": map[string]any{"reasoning_strength": map[string]string{"$var": "thinking.effort"}, "preserve_thinking": true}}
			}
		}
		models = append(models, entry)
	}
	document := map[string]any{"providers": map[string]any{ProviderID: piProvider(identity.DisplayName+" gateway", endpoint, models)}}
	encoded, err := json.MarshalIndent(document, "", "  ")
	return append(encoded, '\n'), err
}

func piProvider(name, endpoint string, models []map[string]any) map[string]any {
	return map[string]any{"name": name, "baseUrl": endpoint, "api": "openai-completions", "apiKey": "paracetamol-local", "authHeader": false, "compat": map[string]any{"supportsDeveloperRole": false, "supportsReasoningEffort": true, "sendSessionAffinityHeaders": true, "sessionAffinityFormat": "openai-nosession"}, "models": models}
}

func zeroCost() map[string]int {
	return map[string]int{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}
}

func CreatePiPlan(ctx context.Context, managed catalog.Catalog, projectRoot, gatewayURL string, arguments []string, runtime PiRuntime) (PiPlan, error) {
	return createPiPlan(ctx, managed, projectRoot, gatewayURL, arguments, runtime, nil, true)
}

// CreatePiPlanForModels is the deterministic benchmark seam. Normal sessions
// must discover the live gateway inventory instead of supplying it themselves.
func CreatePiPlanForModels(ctx context.Context, managed catalog.Catalog, projectRoot, gatewayURL string, arguments []string, runtime PiRuntime, advertised []string) (PiPlan, error) {
	return createPiPlan(ctx, managed, projectRoot, gatewayURL, arguments, runtime, advertised, false)
}

func createPiPlan(ctx context.Context, managed catalog.Catalog, projectRoot, gatewayURL string, arguments []string, runtime PiRuntime, advertised []string, discover bool) (PiPlan, error) {
	if len(arguments) > 0 && arguments[0] == "--" {
		arguments = arguments[1:]
	}
	if len(arguments) > 0 && arguments[0] == "update" && (len(arguments) == 1 || contains(arguments[1:], "--self") || contains(arguments[1:], "--all") || contains(arguments[1:], "self") || contains(arguments[1:], "pi")) {
		return PiPlan{}, fmt.Errorf("Pi is managed by %s; update the checkout and run %s", identity.DisplayName, identity.Command("agent", "install", "pi"))
	}
	prefix := []string{runtime.Node, runtime.Entrypoint}
	mode := "session"
	if len(arguments) > 0 && contains([]string{"--help", "-h", "--version", "-v"}, arguments[0]) {
		mode = "passthrough"
	} else if len(arguments) > 0 && contains([]string{"install", "remove", "uninstall", "update", "list", "config", "auth"}, arguments[0]) {
		mode = "management"
	}
	plan := PiPlan{RuntimeRoot: runtime.Root, Mode: mode}
	if mode == "passthrough" {
		plan.Command = append(prefix, arguments...)
		return plan, nil
	}
	endpoint, err := NormalizeGatewayURL(gatewayURL)
	if err != nil {
		return PiPlan{}, err
	}
	plan.Endpoint, plan.Remote = endpoint, remoteEndpoint(endpoint)
	if mode == "session" && discover {
		advertised, err = DiscoverGatewayModels(ctx, endpoint)
		if err != nil {
			return PiPlan{}, err
		}
	}
	if mode != "session" {
		advertised = nil
	}
	models, err := AgentModels(managed, advertised)
	if err != nil {
		return PiPlan{}, err
	}
	plan.Config, err = PiConfig(managed, endpoint, models)
	if err != nil {
		return PiPlan{}, err
	}
	plan.ModelPicker, err = regularFile(filepath.Join(projectRoot, "agent-clients", "pi", "extensions", "paracetamol-model-picker.ts"), "Pi model-picker extension")
	if err != nil {
		return PiPlan{}, err
	}
	plan.CompletionDivider, err = regularFile(filepath.Join(projectRoot, "agent-clients", "pi", "extensions", "paracetamol-completion-divider.ts"), "Pi completion-divider extension")
	if err != nil {
		return PiPlan{}, err
	}
	if mode != "session" {
		plan.Command = append(prefix, arguments...)
		return plan, nil
	}
	provider, model, thinking, err := DefaultModel(models, "Pi")
	if err != nil {
		return PiPlan{}, err
	}
	plan.DefaultProvider, plan.DefaultModel, plan.DefaultThinking = provider, model, thinking
	plan.Command = append(prefix, "--offline", "--no-approve", "--provider", provider, "--model", model, "--thinking", thinking)
	plan.Command = append(plan.Command, arguments...)
	return plan, nil
}

func PreparePiState(plan PiPlan, dataRoot string) (string, error) {
	paths, err := PrepareSandboxPaths(dataRoot, "pi")
	if err != nil {
		return "", err
	}
	agentDir := filepath.Join(paths.Data, "pi", "agent")
	extensions := filepath.Join(agentDir, "extensions")
	for _, path := range []string{filepath.Join(paths.Data, "pi"), agentDir, extensions} {
		if err := secureDirectory(path, true); err != nil {
			return "", err
		}
	}
	if err := storage.ValidateManagedParent(filepath.Join(agentDir, "models.json"), paths.Root, dataRoot, "Pi model config"); err != nil {
		return "", err
	}
	for path, contents := range map[string][]byte{filepath.Join(agentDir, "models.json"): plan.Config, filepath.Join(extensions, "paracetamol-model-picker.ts"): plan.ModelPicker, filepath.Join(extensions, "paracetamol-completion-divider.ts"): plan.CompletionDivider} {
		if err := writeAtomic(path, contents, 0o600, false); err != nil {
			return "", err
		}
	}
	return agentDir, nil
}

func PiEnvironment(base map[string]string, agentDir string, offline bool) map[string]string {
	result := cloneEnvironment(base)
	result["PI_CODING_AGENT_DIR"] = agentDir
	result["PI_SKIP_VERSION_CHECK"] = "1"
	result["PI_TELEMETRY"] = "0"
	if offline {
		result["PI_OFFLINE"] = "1"
	} else {
		delete(result, "PI_OFFLINE")
	}
	return result
}

func remoteEndpoint(endpoint string) bool {
	parsed, _ := url.Parse(endpoint)
	host := parsed.Hostname()
	return host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback())
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func cloneEnvironment(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func parsePort(value string) (int, error) {
	var port int
	_, err := fmt.Sscan(value, &port)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port")
	}
	return port, nil
}
