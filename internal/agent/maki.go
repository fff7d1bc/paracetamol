package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/gateway"
	"paracetamol/internal/identity"
	"paracetamol/internal/storage"
	"paracetamol/internal/textmodel"
)

type MakiPlan struct {
	Command         []string
	DefaultProvider string
	DefaultModel    string
	DefaultThinking string
	Endpoint        string
	Init            []byte
	Providers       map[string][]byte
	Tiers           []byte
	Mode            string
	Remote          bool
	Authenticated   bool
}

func FindRealExecutable(name, wrapper string, environment map[string]string) (string, error) {
	resolvedWrapper, _ := filepath.Abs(wrapper)
	if resolved, err := filepath.EvalSymlinks(resolvedWrapper); err == nil {
		resolvedWrapper = resolved
	}
	for _, directory := range filepath.SplitList(environment["PATH"]) {
		if directory == "" {
			directory = "."
		}
		candidate := filepath.Join(directory, name)
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || resolved == resolvedWrapper {
			continue
		}
		if info, err := os.Stat(resolved); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%s executable not found outside %s's bin directory", name, identity.DisplayName)
}

func MakiMode(arguments []string) string {
	if len(arguments) > 0 && arguments[0] == "--" {
		arguments = arguments[1:]
	}
	if len(arguments) > 0 && contains([]string{"--help", "-h", "--version", "-V", "update", "rollback", "migrate"}, arguments[0]) {
		return "passthrough"
	}
	if len(arguments) > 0 && contains([]string{"auth", "models", "index", "mcp", "prompt"}, arguments[0]) {
		return "management"
	}
	return "session"
}

func CreateMakiPlan(ctx context.Context, managed catalog.Catalog, projectRoot, gatewayURL string, arguments []string, environment map[string]string, apiKey string) (MakiPlan, error) {
	if len(arguments) > 0 && arguments[0] == "--" {
		arguments = arguments[1:]
	}
	executable, err := FindRealExecutable("maki", filepath.Join(projectRoot, "bin", "maki"), environment)
	if err != nil {
		return MakiPlan{}, err
	}
	mode := MakiMode(arguments)
	if mode == "passthrough" {
		return MakiPlan{Command: append([]string{executable}, arguments...), Mode: mode}, nil
	}
	endpoint, err := config.NormalizeGatewayURL(gatewayURL)
	if err != nil {
		return MakiPlan{}, err
	}
	if apiKey != "" {
		if err := config.ValidateGatewayKey(apiKey); err != nil {
			return MakiPlan{}, err
		}
	}
	var advertised []string
	if mode == "session" {
		advertised, err = gateway.FetchModelIDs(ctx, endpoint, apiKey)
		if err != nil {
			return MakiPlan{}, err
		}
	}
	models, err := AgentModels(managed, advertised)
	if err != nil {
		return MakiPlan{}, err
	}
	provider, model, thinking, err := DefaultModel(models, "Maki")
	if err != nil {
		return MakiPlan{}, err
	}
	sessionID := ""
	if mode == "session" {
		sessionID = newSessionID()
	}
	plan := MakiPlan{Command: append([]string{executable}, arguments...), Endpoint: endpoint, Remote: remoteEndpoint(endpoint), Authenticated: apiKey != "", Init: []byte(fmt.Sprintf("maki.setup({\n  always_thinking = \"adaptive\",\n  provider = { default_model = %s },\n  plugins = { task = { max_concurrent = 1 } },\n})\n", luaString(provider+"/"+model))), Providers: map[string][]byte{ProviderID: makiProvider(identity.DisplayName+" gateway", endpoint, makiModels(models), sessionID, apiKey)}, Mode: mode}
	tiers, _ := json.MarshalIndent(map[string]string{"compaction": provider + "/" + model, "weak": provider + "/" + model, "medium": provider + "/" + model, "strong": provider + "/" + model}, "", "  ")
	plan.Tiers = append(tiers, '\n')
	if mode == "session" {
		plan.DefaultProvider, plan.DefaultModel, plan.DefaultThinking = provider, model, thinking
	}
	return plan, nil
}

func makiModels(models []textmodel.Model) []map[string]any {
	result := make([]map[string]any, 0, len(models))
	for _, selected := range models {
		model := map[string]any{"id": selected.ID, "tier": "medium", "context_window": selected.Context, "max_output_tokens": selected.MaxOutputTokens, "supports_thinking": selected.ReasoningControl != ""}
		if selected.ReasoningControl != "" {
			fields := make(map[string]any)
			switch selected.ReasoningControl {
			case "toggle":
				fields["off"] = map[string]any{"chat_template_kwargs": map[string]bool{"enable_thinking": false}}
				fields["adaptive"] = map[string]any{"chat_template_kwargs": map[string]bool{"enable_thinking": true}}
			default:
				field := "reasoning_" + selected.ReasoningControl
				fields["adaptive"] = map[string]string{field: selected.ReasoningDefault}
				for _, level := range selected.ReasoningLevels {
					fields[level] = map[string]string{field: level}
				}
				if selected.ReasoningOff {
					fields["off"] = map[string]string{field: "none"}
				}
			}
			model["thinking_fields"] = fields
			if !selected.ReasoningOff {
				model["requires_thinking"] = true
			}
		}
		result = append(result, model)
	}
	return result
}

func makiProvider(display, endpoint string, models []map[string]any, sessionID, apiKey string) []byte {
	info, _ := json.Marshal(map[string]any{"display_name": display, "base": "llama-cpp", "has_auth": false})
	listed, _ := json.Marshal(models)
	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	if sessionID != "" {
		headers["X-Paracetamol-Session-ID"] = sessionID
	}
	resolved, _ := json.Marshal(map[string]any{"base_url": endpoint, "headers": headers})
	return []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ncase \"${1:-}\" in\n  info) printf '%%s\\n' %s ;;\n  models) printf '%%s\\n' %s ;;\n  resolve|refresh|reload) printf '%%s\\n' %s ;;\n  *) printf '%%s\\n' %s >&2; exit 2 ;;\nesac\n", shellQuote(string(info)), shellQuote(string(listed)), shellQuote(string(resolved)), shellQuote("unsupported "+identity.DisplayName+" provider command")))
}

func newSessionID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func luaString(value string) string { encoded, _ := json.Marshal(value); return string(encoded) }

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func PrepareMakiState(plan MakiPlan, dataRoot string) (SandboxPaths, error) {
	paths, err := PrepareSandboxPaths(dataRoot, "maki")
	if err != nil {
		return SandboxPaths{}, err
	}
	configDir := filepath.Join(paths.Config, "maki")
	providersDir := filepath.Join(configDir, "providers")
	stateDir := filepath.Join(paths.State, "maki")
	for _, path := range []string{configDir, providersDir, stateDir} {
		if err := secureDirectory(path, true); err != nil {
			return SandboxPaths{}, err
		}
	}
	entries, err := os.ReadDir(providersDir)
	if err != nil {
		return SandboxPaths{}, err
	}
	for _, entry := range entries {
		if entry.Name() == "dwarfstar" {
			path := filepath.Join(providersDir, entry.Name())
			info, infoErr := entry.Info()
			if infoErr != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
				return SandboxPaths{}, fmt.Errorf("Maki legacy provider %s is not a small regular file", path)
			}
			contents, readErr := os.ReadFile(path)
			if readErr != nil || !strings.HasPrefix(string(contents), "#!/bin/sh\nset -eu\n") || !strings.Contains(string(contents), `Paracetamol DwarfStar`) {
				return SandboxPaths{}, fmt.Errorf("Maki legacy provider %s is not the expected managed file", path)
			}
			if err := os.Remove(path); err != nil {
				return SandboxPaths{}, fmt.Errorf("remove retired Maki DwarfStar provider: %w", err)
			}
			continue
		}
		if _, ok := plan.Providers[entry.Name()]; !ok {
			return SandboxPaths{}, fmt.Errorf("Maki provider directory contains unmanaged entry %s", entry.Name())
		}
	}
	if err := storage.ValidateManagedParent(filepath.Join(configDir, "init.lua"), paths.Root, dataRoot, "Maki state"); err != nil {
		return SandboxPaths{}, err
	}
	if err := writeAtomic(filepath.Join(configDir, "init.lua"), plan.Init, 0o600, false); err != nil {
		return SandboxPaths{}, err
	}
	for name, content := range plan.Providers {
		if err := writeAtomic(filepath.Join(providersDir, name), content, 0o700, false); err != nil {
			return SandboxPaths{}, err
		}
	}
	tiers := filepath.Join(stateDir, "model-tiers")
	seed := filepath.Join(stateDir, "paracetamol-tier-seed")
	preserve := false
	if current, err := os.ReadFile(tiers); err == nil {
		previous, _ := os.ReadFile(seed)
		preserve = len(previous) == 0 || string(current) != string(previous)
	}
	if err := writeAtomic(tiers, plan.Tiers, 0o600, preserve); err != nil {
		return SandboxPaths{}, err
	}
	if err := writeAtomic(seed, plan.Tiers, 0o600, false); err != nil {
		return SandboxPaths{}, err
	}
	return paths, nil
}

func MakiEnvironment(base map[string]string, paths SandboxPaths) (map[string]string, error) {
	legacy := filepath.Join(base["HOME"], ".maki")
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		return nil, fmt.Errorf("Maki would ignore private XDG state while %s exists; run the real 'maki migrate xdg' first", legacy)
	}
	result := cloneEnvironment(base)
	result["XDG_CONFIG_HOME"] = paths.Config
	result["XDG_DATA_HOME"] = paths.Data
	result["XDG_STATE_HOME"] = paths.State
	result["XDG_CACHE_HOME"] = paths.Cache
	return result, nil
}
