package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/identity"
	"paracetamol/internal/storage"
)

type PiPlan struct {
	Command           []string
	RuntimeRoot       string
	DefaultProvider   string
	DefaultModel      string
	DefaultThinking   string
	Endpoint          string
	DwarfStarEndpoint string
	Config            []byte
	ModelPicker       []byte
	CompletionDivider []byte
	Mode              string
	Remote            bool
}

func NormalizeLlamaURL(value string) (string, error) {
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r <= 32 }) >= 0 {
		return "", fmt.Errorf("Pi llama.cpp URL must not be empty or contain whitespace")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("Pi llama.cpp URL must be a credential-free HTTP(S) URL")
	}
	if parsed.Port() != "" {
		port, err := parsePort(parsed.Port())
		if err != nil || port == 0 {
			return "", fmt.Errorf("invalid Pi llama.cpp URL port")
		}
	}
	endpoint := strings.TrimRight(value, "/")
	if !strings.HasSuffix(endpoint, "/v1") {
		return "", fmt.Errorf("Pi llama.cpp URL path must end with /v1")
	}
	return endpoint, nil
}

func DiscoverRemoteModels(ctx context.Context, endpoint string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cannot reach remote llama.cpp model inventory: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote llama.cpp model inventory returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		return nil, fmt.Errorf("remote llama.cpp model inventory is unreadable or too large")
	}
	var document struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Data == nil {
		return nil, fmt.Errorf("remote llama.cpp returned an invalid model inventory")
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

func PiConfig(managed catalog.Catalog, endpoint, dwarfstarEndpoint string) ([]byte, error) {
	ids := make([]string, 0, len(managed.LlamaPresets))
	for id, preset := range managed.LlamaPresets {
		if preset.AgentTools {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	models := make([]map[string]any, 0, len(ids))
	for _, identifier := range ids {
		preset := managed.LlamaPresets[identifier]
		model := map[string]any{"id": identifier, "name": identifier, "reasoning": preset.ReasoningControl != "", "input": []string{"text"}, "contextWindow": preset.DefaultContext, "maxTokens": OutputLimit(preset.DefaultContext), "cost": zeroCost()}
		if sampling := ClientSampling(managed, identifier); len(sampling) > 0 {
			model["samplingParams"] = sampling
		}
		if preset.ReasoningControl != "" {
			exposed := make(map[string]bool)
			if preset.ReasoningControl == "toggle" {
				exposed["high"] = true
			} else {
				for _, level := range preset.ReasoningLevels {
					exposed[level] = true
				}
			}
			levels := map[string]any{"off": nil, "minimal": nil, "low": nil, "medium": nil, "high": nil, "xhigh": nil, "max": nil}
			if preset.ReasoningOff {
				levels["off"] = "none"
			}
			for level := range exposed {
				levels[level] = level
			}
			model["thinkingLevelMap"] = levels
			switch preset.ReasoningControl {
			case "toggle":
				model["compat"] = map[string]any{"thinkingFormat": "qwen-chat-template", "supportsReasoningEffort": false}
			case "effort":
				model["compat"] = map[string]any{"thinkingFormat": "openai", "supportsReasoningEffort": true}
			default:
				model["compat"] = map[string]any{"thinkingFormat": "chat-template", "supportsReasoningEffort": false, "chatTemplateKwargs": map[string]any{"reasoning_strength": map[string]string{"$var": "thinking.effort"}, "preserve_thinking": true}}
			}
		}
		models = append(models, model)
	}
	dwarfstar := map[string]any{"id": DwarfStarModel, "name": "DeepSeek V4 Flash 0731", "reasoning": true, "thinkingLevelMap": map[string]any{"off": "none", "minimal": nil, "low": nil, "medium": nil, "high": "high", "xhigh": nil, "max": nil}, "input": []string{"text"}, "contextWindow": DwarfStarContext, "maxTokens": DwarfStarOutput, "cost": zeroCost()}
	document := map[string]any{"providers": map[string]any{ProviderID: piProvider(identity.DisplayName+" llama.cpp", endpoint, models), DwarfStarProviderID: piProvider(identity.DisplayName+" DwarfStar", dwarfstarEndpoint, []map[string]any{dwarfstar})}}
	encoded, err := json.MarshalIndent(document, "", "  ")
	return append(encoded, '\n'), err
}

func piProvider(name, endpoint string, models []map[string]any) map[string]any {
	return map[string]any{"name": name, "baseUrl": endpoint, "api": "openai-completions", "apiKey": "paracetamol-local", "authHeader": false, "compat": map[string]any{"supportsDeveloperRole": false, "supportsReasoningEffort": true}, "models": models}
}

func zeroCost() map[string]int {
	return map[string]int{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}
}

func CreatePiPlan(ctx context.Context, managed catalog.Catalog, dataRoot, projectRoot string, port, dwarfstarPort int, llamaURL string, arguments []string, runtime PiRuntime) (PiPlan, error) {
	remote := llamaURL != ""
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	var err error
	if remote {
		endpoint, err = NormalizeLlamaURL(llamaURL)
		if err != nil {
			return PiPlan{}, err
		}
	}
	dwarfstarEndpoint := fmt.Sprintf("http://127.0.0.1:%d/v1", dwarfstarPort)
	config, err := PiConfig(managed, endpoint, dwarfstarEndpoint)
	if err != nil {
		return PiPlan{}, err
	}
	picker, err := regularFile(filepath.Join(projectRoot, "agent-clients", "pi", "extensions", "paracetamol-model-picker.ts"), "Pi model-picker extension")
	if err != nil {
		return PiPlan{}, err
	}
	divider, err := regularFile(filepath.Join(projectRoot, "agent-clients", "pi", "extensions", "paracetamol-completion-divider.ts"), "Pi completion-divider extension")
	if err != nil {
		return PiPlan{}, err
	}
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
	plan := PiPlan{RuntimeRoot: runtime.Root, Endpoint: endpoint, DwarfStarEndpoint: dwarfstarEndpoint, Config: config, ModelPicker: picker, CompletionDivider: divider, Mode: mode, Remote: remote}
	if mode != "session" {
		plan.Command = append(prefix, arguments...)
		return plan, nil
	}
	provider, model, thinking := "", "", ""
	if remote {
		advertised, err := DiscoverRemoteModels(ctx, endpoint)
		if err != nil {
			return PiPlan{}, err
		}
		maintained := make(map[string]bool)
		for _, id := range advertised {
			if preset, ok := managed.LlamaPresets[id]; ok && preset.AgentTools {
				maintained[id] = true
			}
		}
		if maintained[RecommendedModel] {
			model = RecommendedModel
		} else {
			var ids []string
			for id := range maintained {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			if len(ids) == 0 {
				return PiPlan{}, fmt.Errorf("remote llama.cpp router advertises no model maintained for Pi")
			}
			model = ids[0]
		}
		provider, thinking = ProviderID, ReasoningDefault(managed.LlamaPresets[model])
	} else {
		provider, model, thinking, err = DefaultModel(managed, dataRoot, "Pi")
		if err != nil {
			return PiPlan{}, err
		}
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
