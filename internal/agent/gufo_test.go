package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"paracetamol/internal/catalog"
)

func TestGufoClientsExposeNativeReasoningWithoutSamplerOverrides(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog/catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	id := "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl-gufo"
	models, err := AgentModels(managed, []string{id})
	if err != nil || len(models) != 1 {
		t.Fatalf("%v %v", models, err)
	}
	if models[0].Context != 262144 || models[0].MaxOutputTokens != 16384 {
		t.Fatal(models[0])
	}
	configuration, err := PiConfig(managed, "http://127.0.0.1:7455/v1", models, "")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Providers map[string]struct {
			Models []struct {
				Levels   map[string]any  `json:"thinkingLevelMap"`
				Sampling json.RawMessage `json:"samplingParams"`
			}
		}
	}
	if err := json.Unmarshal(configuration, &document); err != nil {
		t.Fatal(err)
	}
	pi := document.Providers[ProviderID].Models[0]
	if pi.Levels["off"] != "none" || pi.Levels["medium"] != "medium" || pi.Levels["xhigh"] != "xhigh" || pi.Levels["high"] != nil || len(pi.Sampling) != 0 {
		t.Fatalf("%+v", pi)
	}
	maki := makiModels(models)[0]
	fields := maki["thinking_fields"].(map[string]any)
	for choice, want := range map[string]string{"off": "none", "medium": "medium", "xhigh": "xhigh", "adaptive": "medium"} {
		if fields[choice].(map[string]string)["reasoning_effort"] != want {
			t.Fatal(fields)
		}
	}
	all, err := AgentModels(managed, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, preferred, reasoning, err := DefaultModel(all, "Pi")
	if err != nil || preferred != RecommendedModel || reasoning != "medium" {
		t.Fatalf("default changed: %s %s %v", preferred, reasoning, err)
	}
	if len(ClientSampling(managed, id)) != 0 {
		t.Fatal("Gufo sampler defaults overridden")
	}
}
