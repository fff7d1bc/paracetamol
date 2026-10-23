package clientconfig

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/textmodel"
)

func catalogModels(t *testing.T) []textmodel.Model {
	t.Helper()
	managed, err := catalog.Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	models, err := textmodel.All(managed)
	if err != nil {
		t.Fatal(err)
	}
	return models
}

func TestMakiExportFiltersAndSortsWithoutMutatingInput(t *testing.T) {
	models := []textmodel.Model{
		{ID: "z", AgentTools: true, Context: 4096, MaxOutputTokens: 1000},
		{ID: "not-for-agents"},
		{ID: "a", AgentTools: true, Context: 8192, MaxOutputTokens: 2000},
	}
	files, err := Maki(models, "http://gateway.test:7455/v1", false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files.Providers)
	for _, required := range []string{`base_url = "http://gateway.test:7455/v1"`, `discover_models = false`, `api_key = "paracetamol-local"`, `api_key_env = "PARACETAMOL_API_KEY"`, `default_model = "paracetamol/a"`, "context_window = 8192", "max_output_tokens = 2000"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing %s in %s", required, text)
		}
	}
	if strings.Contains(text, "not-for-agents") || strings.Index(text, `id = "a"`) > strings.Index(text, `id = "z"`) || models[0].ID != "z" {
		t.Fatal("filter/order mutated input or leaked a model")
	}
	again, err := Maki([]textmodel.Model{models[2], models[0]}, "http://gateway.test:7455/v1", false)
	if err != nil || !bytes.Equal(files.Providers, again.Providers) || !bytes.Equal(files.Init, again.Init) {
		t.Fatal("export is not deterministic")
	}
}

func TestMakiExportDefaultsAuthenticationAndIsolation(t *testing.T) {
	models := catalogModels(t)
	files, err := Maki(models, "https://gateway.example/v1", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"api_key =", "Authorization", "X-Paracetamol-Session-ID", "temperature", "top_p", "allowed_models", "model-tiers", "provider.register", "net_hosts"} {
		if bytes.Contains(files.Providers, []byte(forbidden)) || bytes.Contains(files.Init, []byte(forbidden)) {
			t.Fatalf("export contains %s", forbidden)
		}
	}
	if !bytes.Contains(files.Init, []byte("default_model = "+luaString("paracetamol/"+RecommendedModel))) || !bytes.Contains(files.Init, []byte(`always_thinking = "adaptive"`)) {
		t.Fatalf("wrong startup defaults: %s", files.Init)
	}
	if !bytes.Contains(files.Init, []byte("task = { max_concurrent = 2 }")) {
		t.Fatalf("wrong task concurrency: %s", files.Init)
	}
	for _, model := range models {
		fields := MakiThinkingFields(model)
		if model.ReasoningControl == "" {
			if len(fields) != 0 {
				t.Fatalf("invented reasoning for %s", model.ID)
			}
			continue
		}
		if (fields["off"] != nil) != model.ReasoningOff || fields["adaptive"] == nil {
			t.Fatalf("wrong thinking support for %s: %v", model.ID, fields)
		}
		if model.ID == RecommendedModel && fields["adaptive"].(map[string]any)["reasoning_effort"] != "xhigh" {
			t.Fatal("recommended model did not select xhigh")
		}
		if model.ReasoningControl != "toggle" {
			for _, level := range model.ReasoningLevels {
				if fields[level].(map[string]any)["reasoning_"+model.ReasoningControl] != level {
					t.Fatalf("wrong native level %s/%s", model.ID, level)
				}
			}
		}
	}
}

func TestMakiEmptyInventoryAndFallback(t *testing.T) {
	for _, models := range [][]textmodel.Model{nil, {}, {{ID: "chat-only"}}} {
		if _, err := Maki(models, "http://localhost:7455/v1", false); err == nil {
			t.Fatal("empty eligible inventory accepted")
		}
	}
	models := []textmodel.Model{{ID: "a-optional", AgentTools: true}}
	for _, id := range []string{"unsloth-qwen3.8-27b-ud-q8-k-xl", "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl", RecommendedModel} {
		models = append(models, textmodel.Model{ID: id, AgentTools: true})
		selected, err := DefaultModel(models)
		if err != nil || selected.ID != id {
			t.Fatalf("fallback=%v want=%s err=%v", selected, id, err)
		}
	}
	if _, err := Maki(models, "https://user:secret@example/v1", true); err == nil {
		t.Fatal("credential-bearing URL accepted")
	}
}

func TestMakiDFlash2ExportsPreferAcceptedFlashNext(t *testing.T) {
	const baseID = "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"
	if RecommendedModel != baseID+"-dflash2" {
		t.Fatalf("recommended model did not select Flash-Next DFlash2: %s", RecommendedModel)
	}
	models := catalogModels(t)
	byID := make(map[string]textmodel.Model)
	for _, model := range models {
		byID[model.ID] = model
	}
	variants := []string{
		RecommendedModel,
		"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl-dflash2",
	}
	for _, modelID := range variants {
		t.Run(modelID, func(t *testing.T) {
			model, ok := byID[modelID]
			if !ok || !model.AgentTools {
				t.Fatal("missing DFlash2 agent model")
			}
			wantFields := map[string]any{}
			for mode, effort := range map[string]string{"adaptive": "medium", "off": "none", "low": "low", "medium": "medium", "xhigh": "xhigh"} {
				if mode == "adaptive" && modelID == RecommendedModel {
					effort = "xhigh"
				}
				wantFields[mode] = map[string]any{"reasoning_effort": effort}
			}
			if fields := MakiThinkingFields(model); !reflect.DeepEqual(fields, wantFields) {
				t.Fatalf("optional model invented levels or request fields: %#v", fields)
			}
			files, err := Maki(models, "https://gateway.example/v1", true)
			if err != nil {
				t.Fatal(err)
			}
			if count := strings.Count(string(files.Providers), "\nid = "+quote(modelID)+"\n"); count != 1 {
				t.Fatalf("optional model exported %d times", count)
			}
			for _, forbidden := range []string{"temperature", "top_p", "top_k", "min_p", "presence_penalty", "repeat_penalty", "thinking_budget", "draft_sampling"} {
				if bytes.Contains(files.Providers, []byte(forbidden)) || bytes.Contains(files.Init, []byte(forbidden)) {
					t.Fatalf("optional model introduced client-owned sampling: %s", forbidden)
				}
			}
		})
	}
	base := byID[baseID]
	if base.ReasoningDefault != "medium" || ReasoningDefault(base) != "xhigh" || byID[RecommendedModel].ReasoningDefault != "medium" || ReasoningDefault(byID[RecommendedModel]) != "xhigh" {
		t.Fatalf("HTTP fallback or client reasoning preference is wrong: base=%#v preferred=%#v", base, byID[RecommendedModel])
	}
	// The accepted Flash-Next preference is independent of catalog ordering.
	for _, inventory := range [][]textmodel.Model{models, {byID[variants[1]], byID[variants[0]], base}} {
		selected, err := DefaultModel(inventory)
		if err != nil || selected.ID != RecommendedModel {
			t.Fatalf("wrong accepted default: %s, %v", selected.ID, err)
		}
		files, err := Maki(inventory, "https://gateway.example/v1", true)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(files.Providers, []byte("default_model = "+quote("paracetamol/"+RecommendedModel))) || !bytes.Contains(files.Init, []byte("default_model = "+luaString("paracetamol/"+RecommendedModel))) || !bytes.Contains(files.Providers, []byte(`"adaptive" = { "reasoning_effort" = "xhigh" }`)) {
			t.Fatal("export changed the recommended model or xhigh effort")
		}
	}
	for _, model := range models {
		if strings.Contains(model.ID, "muse-glimmer") || strings.Contains(model.ID, "qwen3.8-27b-mtp-") {
			t.Fatalf("retired managed model still exported: %s", model.ID)
		}
	}
}

func TestMakiLiteralEscapingAndToggle(t *testing.T) {
	value := "Zażółć 日本語 \\ \"\n\x01 ]=] ]==]"
	var decoded string
	if err := json.Unmarshal([]byte(quote(value)), &decoded); err != nil || decoded != value || strings.Contains(quote(value), `\x`) {
		t.Fatal("invalid TOML string escaping")
	}
	if luaString(value) != "[===["+value+"]===]" {
		t.Fatal("unsafe Lua string")
	}
	model := textmodel.Model{ReasoningControl: "toggle", ReasoningOff: true}
	fields := MakiThinkingFields(model)
	if inlineValue(fields["off"]) != `{ "chat_template_kwargs" = { "enable_thinking" = false } }` {
		t.Fatal("toggle not rendered as nested TOML")
	}
	strength := textmodel.Model{ReasoningControl: "strength", ReasoningLevels: []string{"low", "high"}, ReasoningDefault: "high"}
	if got := MakiThinkingFields(strength); !reflect.DeepEqual(got, map[string]any{
		"adaptive": map[string]any{"reasoning_strength": "high"},
		"low":      map[string]any{"reasoning_strength": "low"},
		"high":     map[string]any{"reasoning_strength": "high"},
	}) {
		t.Fatalf("generic native strength control changed: %v", got)
	}
}
