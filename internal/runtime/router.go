package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rocmplete/internal/catalog"
	"rocmplete/internal/content"
	"rocmplete/internal/platform"
	"rocmplete/internal/storage"
	"rocmplete/internal/verification"
)

func RenderRouter(managed catalog.Catalog, dataRoot, backend string) (string, []string, error) {
	store, err := verification.Load(dataRoot)
	if err != nil {
		return "", nil, err
	}
	identifiers := make([]string, 0, len(managed.LlamaPresets))
	for identifier := range managed.LlamaPresets {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	sections := []string{"version = 1", ""}
	installed := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		preset := managed.LlamaPresets[identifier]
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
		artifact := managed.Artifacts[preset.Artifact]
		section := []string{"[" + identifier + "]", "model = /content/models/" + artifact.Destination, fmt.Sprintf("c = %d", preset.DefaultContext)}
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
		if preset.ReasoningPreserve {
			section = append(section, "reasoning-preserve = true")
		}
		if preset.ChatTemplate != "" {
			section = append(section, "jinja = true", "chat-template-file = /usr/local/share/rocmplete/llama-chat-templates/"+preset.ChatTemplate+".jinja")
		}
		if preset.SamplingPolicy != "" {
			policy := managed.SamplingPolicies[preset.SamplingPolicy]
			encoded, encodeErr := orderedSamplingJSON(policy)
			if encodeErr != nil {
				return "", nil, encodeErr
			}
			section = append(section, "sampling-defaults-by-reasoning = "+encoded)
		}
		for _, profile := range platform.ProfileIDs() {
			if value := preset.FlashAttention[profile]; value != "" {
				section = append(section, "rocmplete-flash-attn-"+profile+" = "+value)
			}
			if value := preset.KVCache[profile]; value != "" {
				section = append(section, "rocmplete-kv-cache-"+profile+" = "+value)
			}
		}
		if preset.SpeculativeType != "" {
			section = append(section, "spec-type = "+preset.SpeculativeType, fmt.Sprintf("spec-draft-n-max = %d", preset.DraftTokensForBackend(backend)))
			if preset.DraftArtifact != "" {
				section = append(section, "model-draft = /content/models/"+managed.Artifacts[preset.DraftArtifact].Destination)
			}
		}
		sections = append(sections, append(section, "load-on-startup = false", "")...)
		installed = append(installed, identifier)
	}
	if len(installed) == 0 {
		return "", nil, fmt.Errorf("no managed llama.cpp presets are installed")
	}
	return strings.Join(sections, "\n"), installed, nil
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
	temporary, err := os.CreateTemp(filepath.Dir(path), ".models.ini.*.tmp")
	if err != nil {
		return "", err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", err
	}
	if _, err := temporary.WriteString(contents); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(name, path); err != nil {
		return "", err
	}
	return path, nil
}
