package catalog

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/project"
)

func TestLoadRepositoryCatalog(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Agreements) != 4 || len(loaded.Artifacts) != 64 || len(loaded.Bundles) != 45 || len(loaded.Workflows) != 28 || len(loaded.Benchmarks) != 28 || len(loaded.SamplingPolicies) != 2 || len(loaded.LlamaPresets) != 9 {
		t.Fatalf("unexpected catalog counts: agreements=%d artifacts=%d bundles=%d workflows=%d benchmarks=%d policies=%d llama_presets=%d", len(loaded.Agreements), len(loaded.Artifacts), len(loaded.Bundles), len(loaded.Workflows), len(loaded.Benchmarks), len(loaded.SamplingPolicies), len(loaded.LlamaPresets))
	}
	preset := loaded.LlamaPresets["unsloth-qwen3.8-27b-ud-q8-k-xl"]
	if preset.ReasoningControl != "effort" || preset.ReasoningDefault != "medium" || preset.SamplingPolicy != "qwen3.8-27b" {
		t.Fatalf("unexpected qwen preset: %#v", preset)
	}
	flashNextQ4 := loaded.LlamaPresets["unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"]
	if flashNextQ4.DefaultContext != 262144 || strings.Join(flashNextQ4.Backends, ",") != "rocm,vulkan" || flashNextQ4.ModelLoad["strix-halo"] != LlamaModelLoadStreamTokenEmbedding || flashNextQ4.SpeculativeType != "" || flashNextQ4.KVCache["strix-halo"] != "f16" {
		t.Fatalf("unexpected Qwen3.8 Flash-Next Q4_K_XL preset: %#v", flashNextQ4)
	}
	for id, preset := range loaded.LlamaPresets {
		if preset.SamplingPolicy != "qwen3.8-flash-next" && (len(preset.BackendProfiles) != 0 || len(preset.ModelLoad) != 0) {
			t.Fatalf("unrelated preset %s acquired an exceptional runtime policy", id)
		}
		for profile, cacheType := range preset.KVCache {
			if cacheType != "f16" {
				t.Fatalf("llama.cpp preset %s quantizes the %s target K/V cache as %s", id, profile, cacheType)
			}
		}
	}
}

func TestCatalogRejectsRetiredDwarfStarDeclarations(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		old, replacement, message string
	}{
		"preset collection": {`"llama_presets": {`, `"dwarfstar_presets": {}, "llama_presets": {`, `unknown field "dwarfstar_presets"`},
		"artifact target":   {`"target": "llama-models"`, `"target": "dwarfstar-models"`, `unsupported target "dwarfstar-models"`},
		"application":       {`"application": "llama-cpp"`, `"application": "dwarfstar"`, `unknown application "dwarfstar"`},
	} {
		t.Run(name, func(t *testing.T) {
			modified := strings.Replace(string(contents), test.old, test.replacement, 1)
			if modified == string(contents) {
				t.Fatal("catalog fixture no longer matches")
			}
			file := filepath.Join(t.TempDir(), "catalog.json")
			if err := os.WriteFile(file, []byte(modified), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(file); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("retired declaration was not rejected at its boundary: %v", err)
			}
		})
	}
}

func TestHuihuiFlashNextReusesRegularRuntimePolicy(t *testing.T) {
	loaded, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	regular := loaded.LlamaPresets["unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"]
	huihui, ok := loaded.LlamaPresets["huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl"]
	if !ok {
		t.Fatal("missing Huihui variant")
	}
	bundle := loaded.Bundles[huihui.Bundle]
	regularBundle := loaded.Bundles[regular.Bundle]
	if len(bundle.Artifacts) != 4 {
		t.Fatalf("Huihui requires all four shards, got %v", bundle.Artifacts)
	}
	for i, id := range bundle.Artifacts {
		artifact := loaded.Artifacts[id]
		original := loaded.Artifacts[regularBundle.Artifacts[i]]
		if artifact.Source.Repository != "huihui-ai/Huihui-Qwen3.8-Flash-Next-abliterated-v2-GGUF" ||
			!strings.HasPrefix(artifact.Source.Path, "unsloth-UD-Q4_K_XL/") || artifact.Size != original.Size ||
			artifact.License.SPDX != "NOASSERTION" || artifact.License.Status != "unverified" {
			t.Fatalf("unexpected Huihui shard provenance: %#v", artifact)
		}
		if filepath.Dir(artifact.Destination) == filepath.Dir(original.Destination) {
			t.Fatal("variant shards must not mix with the original model")
		}
	}
	// This is another weight choice, not a second implementation of the
	// template, sampling, allocation, context or reasoning contract.
	huihui.ID, huihui.Bundle, huihui.Artifact = regular.ID, regular.Bundle, regular.Artifact
	if !reflect.DeepEqual(regular, huihui) {
		t.Fatalf("Flash-Next policies diverged: regular=%#v Huihui=%#v", regular, huihui)
	}
	for id := range loaded.LlamaPresets {
		if strings.Contains(id, "qwen3.6") {
			t.Fatalf("retired model remains advertised: %s", id)
		}
	}
	for id := range loaded.Artifacts {
		if strings.Contains(id, "qwen3.6") {
			t.Fatalf("retired artifact remains managed: %s", id)
		}
	}
	for id := range loaded.Bundles {
		if strings.Contains(id, "qwen3.6") {
			t.Fatalf("retired bundle remains selectable: %s", id)
		}
	}
}

func TestPublicTextPresetIDsNameTheGGUFPublisher(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for id, preset := range loaded.LlamaPresets {
		artifact := loaded.Artifacts[preset.Artifact]
		publisher := strings.ToLower(strings.SplitN(artifact.Source.Repository, "/", 2)[0])
		if !strings.HasPrefix(id, publisher+"-") || !strings.HasPrefix(preset.Bundle, "llama-"+publisher+"-") {
			t.Errorf("preset %q and bundle %q should name GGUF publisher %q", id, preset.Bundle, publisher)
		}
	}
}

func TestFlashNextDFlash2VariantsReuseTargetsAndSharedDraft(t *testing.T) {
	loaded, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	const draftID = "incoai-qwen3.8-flash-next-dflash2-q8-0-gguf"
	draft := loaded.Artifacts[draftID]
	if draft.Source.Repository != "incoai/Qwen3.8-Flash-Next-DFlash2-GGUF" ||
		draft.Source.Revision != "4f5a39be8f0a9e9a573ac9824251830a8f4096d9" ||
		draft.Source.Path != "Qwen3.8-Flash-Next-DFlash2-Q8_0.gguf" ||
		draft.Destination != "incoai-qwen3.8-flash-next-dflash2/incoai-Qwen3.8-Flash-Next-DFlash2-Q8_0.gguf" ||
		draft.Size != 652101024 || draft.SHA256 != "541f4d6ee230747a34d4a2c6890e6e08cc8b19c461226dc1829701a544e46e8a" ||
		draft.Target != "llama-models" || draft.License.SPDX != "Apache-2.0" || draft.License.Status != "verified" ||
		draft.License.URL != "https://huggingface.co/incoai/Qwen3.8-Flash-Next-DFlash2-GGUF/blob/4f5a39be8f0a9e9a573ac9824251830a8f4096d9/README.md" {
		t.Fatalf("unexpected shared Flash-Next draft provenance: %#v", draft)
	}
	for _, id := range []string{
		"unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl",
		"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl",
	} {
		t.Run(id, func(t *testing.T) {
			base := loaded.LlamaPresets[id]
			variant, ok := loaded.LlamaPresets[id+"-dflash2"]
			if !ok {
				t.Fatal("missing optional DFlash2 preset")
			}
			if base.SpeculativeType != "" || base.DraftTokens != 0 || base.DraftArtifact != "" || len(base.DraftTokensByBackend) != 0 || strings.Join(base.Backends, ",") != "rocm,vulkan" {
				t.Fatalf("non-speculative control changed: %#v", base)
			}
			baseBundle := loaded.Bundles[base.Bundle]
			variantBundle := loaded.Bundles[variant.Bundle]
			wantArtifacts := append(append([]string(nil), baseBundle.Artifacts...), draftID)
			if variant.Bundle != base.Bundle+"-dflash2" || len(baseBundle.Artifacts) != 4 || !reflect.DeepEqual(variantBundle.Artifacts, wantArtifacts) || strings.Join(variantBundle.Groups, ",") != "all,llama" {
				t.Fatalf("DFlash2 must add only the shared draft to a separate bundle: %#v", variantBundle)
			}
			if variant.SpeculativeType != "draft-dflash" || variant.DraftTokens != 3 || len(variant.DraftTokensByBackend) != 0 || variant.DraftArtifact != draftID || strings.Join(variant.Backends, ",") != "rocm" {
				t.Fatalf("unexpected DFlash2 policy: %#v", variant)
			}
			for _, backend := range []string{"rocm", "vulkan"} {
				for _, profile := range []string{"auto", "cpu", "rdna4", "strix-point", "strix-halo"} {
					if variant.SupportsRuntime(backend, profile) != (backend == "rocm" && profile == "strix-halo") {
						t.Fatalf("DFlash2 exposed on unaccepted %s/%s", backend, profile)
					}
				}
			}
			// The optional pair changes speculation and compatibility, not the
			// target, chat, sampler, reasoning, context, or allocator contract.
			variant.ID, variant.Bundle = base.ID, base.Bundle
			variant.SpeculativeType, variant.DraftTokens, variant.DraftArtifact = "", 0, ""
			variant.Backends = base.Backends
			if !reflect.DeepEqual(base, variant) {
				t.Fatalf("optional DFlash2 changed the target runtime policy: base=%#v variant=%#v", base, variant)
			}
		})
	}
}

func TestPlainDenseQwenPresetsKeepF16AndSharedFamilyPolicy(t *testing.T) {
	loaded, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	control := loaded.LlamaPresets["unsloth-qwen3.8-27b-ud-q8-k-xl"]
	for id, context := range map[string]int64{
		"unsloth-qwen3.8-27b-ud-q8-k-xl":               262144,
		"unsloth-qwen3.8-27b-ud-q4-k-xl":               131072,
		"huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl": 262144,
	} {
		t.Run(id, func(t *testing.T) {
			base, exists := loaded.LlamaPresets[id]
			if !exists {
				t.Fatal("missing plain dense target")
			}
			if base.DefaultContext != context || base.SpeculativeType != "" || base.DraftTokens != 0 || base.DraftArtifact != "" || len(base.DraftTokensByBackend) != 0 || len(base.Backends) != 0 || len(base.BackendProfiles) != 0 || len(base.ModelLoad) != 0 || base.KVCache["strix-halo"] != "f16" || len(base.KVCache) != 1 {
				t.Fatalf("plain dense control policy changed: %#v", base)
			}
			baseBundle := loaded.Bundles[base.Bundle]
			if base.Bundle != "llama-"+id || base.Artifact != id+"-gguf" || !reflect.DeepEqual(baseBundle.Artifacts, []string{base.Artifact}) || strings.Join(baseBundle.Groups, ",") != "all,llama" {
				t.Fatalf("plain dense target must remain a one-artifact bundle: %#v", baseBundle)
			}
			if base.ChatTemplate != "qwen3.8" || base.SamplingPolicy != "qwen3.8-27b" || !base.AgentTools || base.ReasoningControl != "effort" || strings.Join(base.ReasoningLevels, ",") != "low,medium,xhigh" || base.ReasoningDefault != "medium" || !base.ReasoningOff || !base.ReasoningPreserve {
				t.Fatalf("plain dense template, reasoning or sampler contract changed: %#v", base)
			}
			for _, backend := range []string{"rocm", "vulkan"} {
				for _, profile := range []string{"auto", "cpu", "rdna4", "strix-point", "strix-halo"} {
					if !base.SupportsRuntime(backend, profile) {
						t.Fatalf("dense runtime compatibility changed on %s/%s", backend, profile)
					}
				}
			}
			// Weight provenance and the reviewed context differ; the family's
			// template, samplers, reasoning and ordinary loading policy do not.
			base.ID, base.Bundle, base.Artifact, base.DefaultContext = control.ID, control.Bundle, control.Artifact, control.DefaultContext
			if !reflect.DeepEqual(base, control) {
				t.Fatalf("plain dense family policies diverged: control=%#v preset=%#v", control, base)
			}
		})
	}
}

func TestDenseDFlash2CandidatesRemainOutsideManagedCatalog(t *testing.T) {
	loaded, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for id, preset := range loaded.LlamaPresets {
		if strings.Contains(id, "qwen3.8-27b") && (strings.Contains(id, "dflash") || preset.SpeculativeType != "" || preset.DraftArtifact != "") {
			t.Fatalf("unaccepted dense speculation is advertised: %s", id)
		}
	}
	for id, artifact := range loaded.Artifacts {
		if strings.Contains(id, "qwen3.8-27b-dflash") || artifact.Source.Repository == "incoai/Qwen3.8-27B-DFlash2-GGUF" {
			t.Fatalf("rejected dense draft remains managed: %s", id)
		}
	}
	for id := range loaded.Bundles {
		if strings.Contains(id, "qwen3.8-27b") && strings.Contains(id, "dflash") {
			t.Fatalf("rejected dense speculative bundle remains selectable: %s", id)
		}
	}
}

func TestMuseAndDenseMTPAliasesAreRetired(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(contents)), "muse") {
		t.Fatal("retired Muse content or policy remains in the catalog")
	}
	loaded, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"unsloth-qwen3.8-27b-mtp-ud-q8-k-xl", "unsloth-qwen3.8-27b-mtp-ud-q4-k-xl"} {
		if _, exists := loaded.LlamaPresets[id]; exists {
			t.Fatalf("retired dense MTP alias remains selectable: %s", id)
		}
	}
	if gemma := loaded.LlamaPresets["ggml-org-gemma4-31b-it-q8-0-mtp"]; gemma.SpeculativeType != "draft-mtp" || gemma.DraftTokens != 4 {
		t.Fatalf("dense Qwen retirement changed unrelated Gemma MTP: %#v", gemma)
	}
}

func TestManagedTextArtifactsNameGGUFPublisher(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for id, artifact := range loaded.Artifacts {
		if artifact.Target != "llama-models" {
			continue
		}
		publisher := strings.ToLower(strings.SplitN(artifact.Source.Repository, "/", 2)[0])
		if !strings.HasPrefix(id, publisher+"-") ||
			!strings.HasPrefix(artifact.Destination, publisher+"-") ||
			!strings.HasPrefix(filepath.Base(artifact.Destination), publisher+"-") {
			t.Errorf("managed text artifact %q destination %q should name GGUF publisher %q", id, artifact.Destination, publisher)
		}
	}
}

func TestLlamaPresetBackendProfileRestrictions(t *testing.T) {
	preset := LlamaPreset{Backends: []string{"rocm", "vulkan"}, BackendProfiles: map[string][]string{"rocm": {"strix-halo"}}}
	for _, profile := range []string{"auto", "cpu", "rdna4", "strix-point", "strix-halo"} {
		if preset.SupportsRuntime("rocm", profile) != (profile == "strix-halo") || !preset.SupportsRuntime("vulkan", profile) {
			t.Fatalf("wrong backend gate for %s", profile)
		}
	}
	for _, policy := range []string{
		`{"cuda":["strix-halo"]}`, `{"rocm":[]}`, `{"rocm":["auto"]}`,
		`{"rocm":["cpu"]}`, `{"rocm":["strix-halo","strix-halo"]}`,
	} {
		raw := `{"bundle":"fixture","artifact":"fixture","default_context":4096,"backend_profiles":` + policy + `}`
		if _, err := loadLlamaPreset("fixture", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid backend profile restriction %s", policy)
		}
	}
}

func TestLlamaPresetRejectsUnknownOrDuplicateBackends(t *testing.T) {
	for _, raw := range []string{
		`{"bundle":"fixture","artifact":"fixture","backends":["cuda"],"default_context":4096}`,
		`{"bundle":"fixture","artifact":"fixture","backends":["vulkan","vulkan"],"default_context":4096}`,
	} {
		if _, err := loadLlamaPreset("fixture", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid backend policy: %s", raw)
		}
	}
}

func TestLlamaPresetRejectsUnknownModelLoadPolicy(t *testing.T) {
	for _, raw := range []string{
		`{"bundle":"fixture","artifact":"fixture","default_context":4096,"model_load":{"rdna4":"mmap-lazy-token-embedding"}}`,
		`{"bundle":"fixture","artifact":"fixture","default_context":4096,"model_load":{"strix-halo":"lazy"}}`,
	} {
		if _, err := loadLlamaPreset("fixture", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid model-load policy: %s", raw)
		}
	}
}

func TestStreamingEmbeddingRequiresTheAcceptedRuntimeTuple(t *testing.T) {
	valid := `{"bundle":"fixture","artifact":"fixture","default_context":262144,"backends":["rocm","vulkan"],"backend_profiles":{"rocm":["strix-halo"]},"model_load":{"strix-halo":"stream-token-embedding"},"flash_attention":{"strix-halo":"on"},"kv_cache":{"strix-halo":"f16"}}`
	if _, err := loadLlamaPreset("fixture", json.RawMessage(valid)); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"other profile":        strings.ReplaceAll(valid, "strix-halo", "strix-point"),
		"quantized cache":      strings.Replace(valid, `"f16"`, `"q8_0"`, 1),
		"no flash attention":   strings.Replace(valid, `"on"`, `"off"`, 1),
		"unrestricted backend": strings.Replace(valid, `"backend_profiles":{"rocm":["strix-halo"]},`, "", 1),
		"speculative decoding": strings.Replace(valid, `"bundle":`, `"speculative_type":"draft-mtp","draft_tokens":3,"bundle":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadLlamaPreset("fixture", json.RawMessage(invalid)); err == nil {
				t.Fatal("accepted unsafe streaming policy")
			}
		})
	}
}

func TestStreamingEmbeddingDFlashRequiresTheAcceptedRuntimeTuple(t *testing.T) {
	valid := `{"bundle":"fixture","artifact":"fixture","default_context":262144,"backends":["rocm"],"backend_profiles":{"rocm":["strix-halo"]},"model_load":{"strix-halo":"stream-token-embedding"},"flash_attention":{"strix-halo":"on"},"kv_cache":{"strix-halo":"f16"},"speculative_type":"draft-dflash","draft_tokens":3,"draft_artifact":"draft"}`
	if _, err := loadLlamaPreset("fixture", json.RawMessage(valid)); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"MTP":                  strings.Replace(valid, "draft-dflash", "draft-mtp", 1),
		"depth zero":           strings.Replace(valid, `"draft_tokens":3`, `"draft_tokens":0`, 1),
		"depth two":            strings.Replace(valid, `"draft_tokens":3`, `"draft_tokens":2`, 1),
		"depth four":           strings.Replace(valid, `"draft_tokens":3`, `"draft_tokens":4`, 1),
		"backend override":     strings.Replace(valid, `"draft_tokens":3`, `"draft_tokens":3,"draft_tokens_by_backend":{"rocm":4}`, 1),
		"missing draft":        strings.Replace(valid, `,"draft_artifact":"draft"`, "", 1),
		"Vulkan":               strings.ReplaceAll(valid, "rocm", "vulkan"),
		"both backends":        strings.Replace(valid, `"backends":["rocm"]`, `"backends":["rocm","vulkan"]`, 1),
		"implicit backends":    strings.Replace(valid, `"backends":["rocm"],`, "", 1),
		"unrestricted ROCm":    strings.Replace(valid, `"backend_profiles":{"rocm":["strix-halo"]},`, "", 1),
		"extra ROCm profile":   strings.Replace(valid, `"rocm":["strix-halo"]`, `"rocm":["strix-halo","strix-point"]`, 1),
		"other profile":        strings.ReplaceAll(valid, "strix-halo", "strix-point"),
		"quantized cache":      strings.Replace(valid, `"f16"`, `"q8_0"`, 1),
		"no flash attention":   strings.Replace(valid, `"on"`, `"off"`, 1),
		"probabilistic schema": strings.Replace(valid, `"draft_tokens":3`, `"draft_tokens":3,"draft_sampling":true`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadLlamaPreset("fixture", json.RawMessage(invalid)); err == nil {
				t.Fatal("accepted unreviewed streaming speculative policy")
			}
		})
	}
}

func TestEveryCuratedWorkflowResourceMatchesCatalog(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for identifier, workflow := range loaded.Workflows {
		t.Run(identifier, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join(root, "catalog", "workflows", identifier+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if digest := fmt.Sprintf("%x", sha256.Sum256(contents)); digest != workflow.RenderedSHA256 {
				t.Fatalf("rendered hash = %s, want %s", digest, workflow.RenderedSHA256)
			}
			var document any
			if err := json.Unmarshal(contents, &document); err != nil {
				t.Fatalf("invalid workflow JSON: %v", err)
			}
		})
	}
	entries, err := os.ReadDir(filepath.Join(root, "catalog", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(loaded.Workflows) {
		t.Fatalf("workflow resource count = %d, want %d", len(entries), len(loaded.Workflows))
	}
	for _, entry := range entries {
		identifier := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || loaded.Workflows[identifier].ID == "" {
			t.Fatalf("unexpected workflow resource: %s", entry.Name())
		}
	}
}

func TestSafeRelativeRejectsTraversal(t *testing.T) {
	for _, value := range []string{"", "/absolute", "../outside", "models/../outside", "."} {
		if _, err := safeRelative(value, "fixture"); err == nil {
			t.Fatalf("unsafe path %q accepted", value)
		}
	}
}

func TestBundleRequiresSelectorGroupsAndAllMembership(t *testing.T) {
	for _, groups := range []string{`[]`, `["llama"]`, `["all", "all"]`, `["all", "unknown"]`, `["all", "dwarfstar"]`} {
		raw := json.RawMessage(`{"description":"fixture","application":"llama-cpp","artifacts":["artifact"],"groups":` + groups + `}`)
		if _, err := loadBundle("fixture", raw); err == nil {
			t.Fatalf("accepted invalid bundle groups %s", groups)
		}
	}
	raw := json.RawMessage(`{"description":"fixture","application":"llama-cpp","artifacts":["artifact"],"groups":["all","llama"]}`)
	if _, err := loadBundle("fixture", raw); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
}
