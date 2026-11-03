package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLlamaPresetProjectorIsARequiredDistinctBundleArtifact(t *testing.T) {
	preset, err := loadLlamaPreset("fixture", json.RawMessage(`{"bundle":"bundle","artifact":"target","mmproj_artifact":"projector","default_context":4096}`))
	if err != nil || preset.MMProjArtifact != "projector" {
		t.Fatalf("projector not loaded: %+v, %v", preset, err)
	}
	for _, test := range []struct {
		name string
		edit func(*Catalog)
	}{
		{"valid", func(*Catalog) {}},
		{"unknown", func(c *Catalog) { delete(c.Artifacts, "projector") }},
		{"outside bundle", func(c *Catalog) {
			c.Bundles["bundle"] = Bundle{Application: "llama-cpp", Artifacts: []string{"target"}}
		}},
		{"target itself", func(c *Catalog) {
			p := c.LlamaPresets["fixture"]
			p.MMProjArtifact = "target"
			c.LlamaPresets["fixture"] = p
		}},
		{"draft itself", func(c *Catalog) {
			p := c.LlamaPresets["fixture"]
			p.DraftArtifact = "projector"
			c.LlamaPresets["fixture"] = p
		}},
		{"wrong target", func(c *Catalog) { a := c.Artifacts["projector"]; a.Target = "models"; c.Artifacts["projector"] = a }},
		{"not GGUF", func(c *Catalog) {
			a := c.Artifacts["projector"]
			a.Destination = "projector.bin"
			c.Artifacts["projector"] = a
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			managed := Catalog{
				Artifacts: map[string]Artifact{
					"target":    {Target: "llama-models", Destination: "target.gguf"},
					"projector": {Target: "llama-models", Destination: "projector.gguf"},
				},
				Bundles:      map[string]Bundle{"bundle": {Application: "llama-cpp", Artifacts: []string{"target", "projector"}}},
				LlamaPresets: map[string]LlamaPreset{"fixture": preset},
			}
			test.edit(&managed)
			err := managed.validateRelationships()
			if test.name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || (!strings.Contains(err.Error(), "projector") && !strings.Contains(err.Error(), "unknown artifact")) {
				t.Fatalf("unsafe projector relationship accepted: %v", err)
			}
		})
	}
}
