package catalog

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestSamplingPolicyValidatesNumbersAtCatalogBoundary(t *testing.T) {
	for _, test := range []struct {
		key, value string
		valid      bool
	}{
		{"temperature", "0", true}, {"temperature", "-0.1", false},
		{"top_p", "1", true}, {"top_p", "1.01", false},
		{"min_p", "0", true}, {"min_p", "-1", false},
		{"top_k", "20", true}, {"top_k", "1.5", false}, {"top_k", "2147483648", false},
		{"presence_penalty", "-1.5", true}, {"repeat_penalty", "0", false},
		{"repeat_penalty", "1", true}, {"temperature", "1e999", false},
		{"temperature", "null", false}, {"temperature", `"1"`, false},
		{"arbitrary", "1", false},
	} {
		t.Run(test.key+"/"+test.value, func(t *testing.T) {
			body := fmt.Sprintf(`{"thinking":{%q:%s},"non_thinking":{"temperature":0.7}}`, test.key, test.value)
			_, err := loadSamplingPolicy("fixture", json.RawMessage(body))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, err=%v", test.valid, err)
			}
		})
	}
}

func TestSamplingPoliciesPreserveReviewedModelDifferences(t *testing.T) {
	managed, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for id, policy := range managed.SamplingPolicies {
		for thinking, expected := range map[bool]map[string]float64{
			true:  {"temperature": 1, "top_p": .95, "top_k": 20, "min_p": 0, "presence_penalty": 0, "repeat_penalty": 1},
			false: {"temperature": .7, "top_p": .8, "top_k": 20, "min_p": 0, "presence_penalty": 1.5, "repeat_penalty": 1},
		} {
			if thinking && id == "qwen3.8-flash-next" {
				expected["min_p"] = .05
			}
			actual := policy.Defaults(thinking)
			if len(actual) != len(expected) {
				t.Fatalf("%s: incomplete tuple %v", id, actual)
			}
			for key, want := range expected {
				value, ok := numeric(actual[key])
				if !ok || value != want {
					t.Fatalf("%s thinking=%v %s=%v want %v", id, thinking, key, value, want)
				}
			}
			actual["temperature"] = 99
			if value, _ := numeric(policy.Defaults(thinking)["temperature"]); value == 99 {
				t.Fatal("defaults mutated the catalog")
			}
		}
	}
	for id, preset := range managed.LlamaPresets {
		preset.SamplingPolicy = "missing"
		managed.LlamaPresets[id] = preset
		break
	}
	if err := managed.validateRelationships(); err == nil {
		t.Fatal("unknown sampling policy accepted")
	}
}
