package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rocmplete/internal/catalog"
	"rocmplete/internal/identity"
	"rocmplete/internal/storage"
)

type MakiPlan struct {
	Command           []string
	DefaultProvider   string
	DefaultModel      string
	DefaultThinking   string
	Endpoint          string
	DwarfStarEndpoint string
	Init              []byte
	Providers         map[string][]byte
	Tiers             []byte
	Mode              string
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

func CreateMakiPlan(managed catalog.Catalog, dataRoot, projectRoot string, port, dwarfstarPort int, arguments []string, environment map[string]string) (MakiPlan, error) {
	if len(arguments) > 0 && arguments[0] == "--" {
		arguments = arguments[1:]
	}
	executable, err := FindRealExecutable("maki", filepath.Join(projectRoot, "bin", "maki"), environment)
	if err != nil {
		return MakiPlan{}, err
	}
	mode := "session"
	if len(arguments) > 0 && contains([]string{"--help", "-h", "--version", "-V", "update", "rollback", "migrate"}, arguments[0]) {
		mode = "passthrough"
	} else if len(arguments) > 0 && contains([]string{"auth", "models", "index", "mcp", "prompt"}, arguments[0]) {
		mode = "management"
	}
	provider, model, thinking := ProviderID, RecommendedModel, ReasoningDefault(managed.LlamaPresets[RecommendedModel])
	if mode == "session" {
		provider, model, thinking, err = DefaultModel(managed, dataRoot, "Maki")
		if err != nil {
			return MakiPlan{}, err
		}
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	dwarfstarEndpoint := fmt.Sprintf("http://127.0.0.1:%d/v1", dwarfstarPort)
	models := makiModels(managed)
	dwarfstarModels := []map[string]any{{"id": DwarfStarModel, "tier": "medium", "context_window": DwarfStarContext, "max_output_tokens": DwarfStarOutput, "supports_thinking": true, "thinking_fields": map[string]any{"off": map[string]string{"reasoning_effort": "none"}, "adaptive": map[string]string{"reasoning_effort": "high"}, "high": map[string]string{"reasoning_effort": "high"}}}}
	plan := MakiPlan{Command: append([]string{executable}, arguments...), Endpoint: endpoint, DwarfStarEndpoint: dwarfstarEndpoint, Init: []byte(fmt.Sprintf("maki.setup({\n  always_thinking = \"adaptive\",\n  provider = { default_model = %s },\n  plugins = { task = { max_concurrent = 1 } },\n})\n", luaString(provider+"/"+model))), Providers: map[string][]byte{ProviderID: makiProvider(identity.DisplayName+" llama.cpp", endpoint, models), DwarfStarProviderID: makiProvider(identity.DisplayName+" DwarfStar", dwarfstarEndpoint, dwarfstarModels)}, Mode: mode}
	tiers, _ := json.MarshalIndent(map[string]string{"compaction": provider + "/" + model, "weak": provider + "/" + model, "medium": provider + "/" + model, "strong": provider + "/" + model}, "", "  ")
	plan.Tiers = append(tiers, '\n')
	if mode == "session" {
		plan.DefaultProvider, plan.DefaultModel, plan.DefaultThinking = provider, model, thinking
	}
	return plan, nil
}

func makiModels(managed catalog.Catalog) []map[string]any {
	var ids []string
	for id, preset := range managed.LlamaPresets {
		if preset.AgentTools {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	result := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		preset := managed.LlamaPresets[id]
		model := map[string]any{"id": id, "tier": "medium", "context_window": preset.DefaultContext, "max_output_tokens": OutputLimit(preset.DefaultContext), "supports_thinking": preset.ReasoningControl != ""}
		if preset.ReasoningControl != "" {
			fields := make(map[string]any)
			switch preset.ReasoningControl {
			case "toggle":
				fields["off"] = map[string]any{"chat_template_kwargs": map[string]bool{"enable_thinking": false}}
				fields["adaptive"] = map[string]any{"chat_template_kwargs": map[string]bool{"enable_thinking": true}}
			default:
				field := "reasoning_" + preset.ReasoningControl
				fields["adaptive"] = map[string]string{field: preset.ReasoningDefault}
				for _, level := range preset.ReasoningLevels {
					fields[level] = map[string]string{field: level}
				}
				if preset.ReasoningOff {
					fields["off"] = map[string]string{field: "none"}
				}
			}
			model["thinking_fields"] = fields
			if !preset.ReasoningOff {
				model["requires_thinking"] = true
			}
		}
		result = append(result, model)
	}
	return result
}

func makiProvider(display, endpoint string, models []map[string]any) []byte {
	info, _ := json.Marshal(map[string]any{"display_name": display, "base": "llama-cpp", "has_auth": false})
	listed, _ := json.Marshal(models)
	resolved, _ := json.Marshal(map[string]any{"base_url": endpoint, "headers": map[string]string{}})
	return []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ncase \"${1:-}\" in\n  info) printf '%%s\\n' %s ;;\n  models) printf '%%s\\n' %s ;;\n  resolve|refresh|reload) printf '%%s\\n' %s ;;\n  *) printf '%%s\\n' %s >&2; exit 2 ;;\nesac\n", shellQuote(string(info)), shellQuote(string(listed)), shellQuote(string(resolved)), shellQuote("unsupported "+identity.DisplayName+" provider command")))
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
	seed := filepath.Join(stateDir, "rocmplete-tier-seed")
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
