package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/platform"
	"paracetamol/internal/storage"
	"paracetamol/internal/verification"
)

func RenderRouter(managed catalog.Catalog, dataRoot, backend, profile string) (string, []string, error) {
	store, err := verification.Load(dataRoot)
	if err != nil {
		return "", nil, err
	}
	identifiers := make([]string, 0, len(managed.LlamaPresets))
	for identifier := range managed.LlamaPresets {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	installed := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		preset := managed.LlamaPresets[identifier]
		if !preset.SupportsRuntime(backend, profile) {
			continue
		}
		bundle := managed.Bundles[preset.Bundle]
		statuses, inspectErr := content.InspectBundle(store, managed, bundle, dataRoot, false)
		if inspectErr != nil {
			return "", nil, inspectErr
		}
		allMissing := true
		ready := true
		for _, status := range statuses {
			allMissing = allMissing && status.State == content.Missing
			ready = ready && content.Ready(status.State)
		}
		if allMissing {
			continue
		}
		if !ready {
			return "", nil, fmt.Errorf("managed llama.cpp preset %s is incomplete", identifier)
		}
		installed = append(installed, identifier)
	}
	if len(installed) == 0 {
		return "", nil, fmt.Errorf("no installed managed llama.cpp preset supports backend %s", backend)
	}
	contents, err := RenderRouterModels(managed, backend, profile, installed)
	return contents, installed, err
}

// RenderRouterModels renders exactly the already-validated preset snapshot
// supplied by its caller. The gateway uses this after receipt validation so
// unrelated incomplete content cannot alter its frozen startup inventory.
func RenderRouterModels(managed catalog.Catalog, backend, profile string, identifiers []string) (string, error) {
	if len(identifiers) == 0 {
		return "", fmt.Errorf("router requires at least one llama.cpp preset")
	}
	selected := append([]string(nil), identifiers...)
	sort.Strings(selected)
	for index, identifier := range selected {
		if index > 0 && selected[index-1] == identifier {
			return "", fmt.Errorf("duplicate llama.cpp router preset %q", identifier)
		}
		preset, ok := managed.LlamaPresets[identifier]
		if !ok {
			return "", fmt.Errorf("unknown llama.cpp router preset %q", identifier)
		}
		if !preset.SupportsRuntime(backend, profile) {
			return "", fmt.Errorf("llama.cpp router preset %q does not support backend %s on profile %s", identifier, backend, profile)
		}
	}
	sections := []string{"version = 1", ""}
	for _, identifier := range selected {
		preset := managed.LlamaPresets[identifier]
		artifact, ok := managed.Artifacts[preset.Artifact]
		if !ok {
			return "", fmt.Errorf("llama.cpp preset %s references unknown artifact %s", identifier, preset.Artifact)
		}
		section := []string{"[" + identifier + "]", "model = /content/models/" + artifact.Destination, fmt.Sprintf("c = %d", preset.DefaultContext)}
		if profiles := preset.BackendProfiles[backend]; len(profiles) > 0 {
			section = append(section, "paracetamol-allowed-profiles = "+strings.Join(profiles, ","))
		}
		if len(preset.ContextOverrideArchitectures) > 0 {
			var overrides []string
			for _, architecture := range preset.ContextOverrideArchitectures {
				overrides = append(overrides, fmt.Sprintf("%s.context_length=int:%d", architecture, preset.DefaultContext))
			}
			section = append(section, "fit = off", "override-kv = "+strings.Join(overrides, ","))
		}
		if preset.Jinja {
			section = append(section, "jinja = true")
		}
		// Do not inherit upstream's changing reasoning-history default.
		section = append(section, fmt.Sprintf("reasoning-preserve = %t", preset.ReasoningPreserve))
		if preset.ChatTemplate != "" {
			section = append(section, "jinja = true", "chat-template-file = /usr/local/share/paracetamol/llama-chat-templates/"+preset.ChatTemplate+".jinja")
		}
		if preset.SamplingPolicy != "" {
			policy := managed.SamplingPolicies[preset.SamplingPolicy]
			encoded, encodeErr := orderedSamplingJSON(policy)
			if encodeErr != nil {
				return "", encodeErr
			}
			section = append(section, "sampling-defaults-by-reasoning = "+encoded)
		}
		for _, profile := range platform.ProfileIDs() {
			if value := preset.FlashAttention[profile]; value != "" {
				section = append(section, "paracetamol-flash-attn-"+profile+" = "+value)
			}
			if value := preset.KVCache[profile]; value != "" {
				section = append(section, "paracetamol-kv-cache-"+profile+" = "+value)
			}
			modelLoad := preset.ModelLoad[profile]
			// One router can manage resident and lazy model children, so its
			// generated sections must make the Strix default explicit per model.
			if modelLoad == "" && (profile == "strix-halo" || profile == "strix-point") {
				modelLoad = catalog.LlamaModelLoadResident
			}
			if modelLoad != "" {
				section = append(section, "paracetamol-model-load-"+profile+" = "+modelLoad)
			}
		}
		if preset.SpeculativeType != "" {
			section = append(section, "spec-type = "+preset.SpeculativeType, fmt.Sprintf("spec-draft-n-max = %d", preset.DraftTokensForBackend(backend)))
			if preset.DraftArtifact != "" {
				section = append(section, "model-draft = /content/models/"+managed.Artifacts[preset.DraftArtifact].Destination)
			}
		}
		sections = append(sections, append(section, "load-on-startup = false", "")...)
	}
	return strings.Join(sections, "\n"), nil
}

func orderedSamplingJSON(policy catalog.SamplingPolicy) (string, error) {
	// encoding/json orders string-keyed maps, keeping the generated router file
	// deterministic without coupling request policy to a template renderer.
	encoded, err := jsonMarshal(map[string]any{"non_thinking": policy.NonThinking, "thinking": policy.Thinking})
	return string(encoded), err
}

var jsonMarshal = func(value any) ([]byte, error) {
	return json.Marshal(value)
}

func WriteRouter(dataRoot, contents string) (string, error) {
	path := filepath.Join((storage.Layout{Root: dataRoot}).Application("llama-cpp"), "models.ini")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if status, err := os.Lstat(path); err == nil && (status.Mode()&os.ModeSymlink != 0 || !status.Mode().IsRegular()) {
		return "", fmt.Errorf("refusing unexpected llama.cpp router preset: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := atomicfile.Write(path, []byte(contents), 0o600, atomicfile.ReplaceRegular); err != nil {
		return "", err
	}
	return path, nil
}
