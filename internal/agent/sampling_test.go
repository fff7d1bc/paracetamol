package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/textmodel"
)

func TestDirectEvaluationPinsSamplingButGatewaySessionsDoNot(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	models, err := AgentModels(managed, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		if model.SamplingPolicy == "" {
			continue
		}
		for _, thinking := range textmodel.ReasoningLevels(model) {
			plan, err := CreateEvaluationPiPlan(context.Background(), managed, projectRoot(t), "http://127.0.0.1:8187/v1", nil,
				PiRuntime{Root: "/reviewed/runtime", Node: "/node", Entrypoint: "/pi"}, model.ID, thinking)
			if err != nil {
				t.Fatal(err)
			}
			selectedThinking := ""
			for index, arg := range plan.Command {
				if arg == "--thinking" && index+1 < len(plan.Command) {
					selectedThinking = plan.Command[index+1]
				}
			}
			if selectedThinking != thinking || plan.DefaultThinking != thinking {
				t.Fatalf("sampling mode %s disagrees with command %v", thinking, plan.Command)
			}
			var config struct {
				Providers map[string]struct {
					Models []struct {
						Sampling map[string]json.RawMessage `json:"samplingParams"`
					} `json:"models"`
				} `json:"providers"`
			}
			if err := json.Unmarshal(plan.Config, &config); err != nil {
				t.Fatal(err)
			}
			actual := config.Providers[ProviderID].Models[0].Sampling
			for key, value := range managed.SamplingPolicies[model.SamplingPolicy].Defaults(thinking != "off") {
				want, _ := json.Marshal(value)
				if string(actual[key]) != string(want) {
					t.Fatalf("%s %s: %s=%s want %s", model.ID, thinking, key, actual[key], want)
				}
			}
			gatewayConfig, err := PiConfig(managed, plan.Endpoint, []textmodel.Model{model}, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(gatewayConfig, &config); err != nil {
				t.Fatal(err)
			}
			if len(config.Providers[ProviderID].Models[0].Sampling) != 0 {
				t.Fatalf("%s normal session pinned benchmark sampling", model.ID)
			}
		}
	}
}
