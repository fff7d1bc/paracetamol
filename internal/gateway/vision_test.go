package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/verification"
)

func TestRegistryRequiresCurrentProjectorReceiptAndFreezesVision(t *testing.T) {
	for _, state := range []string{"missing", "unverified", "stale", "verified"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			managed := catalog.Catalog{
				Artifacts: map[string]catalog.Artifact{},
				Bundles: map[string]catalog.Bundle{
					"text":   {Application: "llama-cpp", Artifacts: []string{"target"}},
					"vision": {Application: "llama-cpp", Artifacts: []string{"target", "projector"}},
				},
				LlamaPresets: map[string]catalog.LlamaPreset{
					"text":   {Bundle: "text", Artifact: "target", DefaultContext: 4096},
					"vision": {Bundle: "vision", Artifact: "target", MMProjArtifact: "projector", DefaultContext: 4096, AgentTools: true},
				},
			}
			store, err := verification.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"target", "projector"} {
				artifact := catalog.Artifact{ID: id, Target: "llama-models", Destination: id + ".gguf", Size: 4, SHA256: "fixture-" + id}
				managed.Artifacts[id] = artifact
				if id == "projector" && state == "missing" {
					continue
				}
				path := content.ArtifactPath(root, artifact)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
					t.Fatal(err)
				}
				if id != "projector" || state != "unverified" {
					if err := store.Record(path, artifact.Size, artifact.SHA256); err != nil {
						t.Fatal(err)
					}
				}
				if id == "projector" && state == "stale" {
					if err := os.Rename(path, path+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := store.Save(); err != nil {
				t.Fatal(err)
			}
			registry, diagnostics, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm")
			if err != nil {
				t.Fatal(err)
			}
			text, ok := registry.Lookup("text")
			if !ok || text.Vision {
				t.Fatal("projector state changed ordinary text model")
			}
			vision, ok := registry.Lookup("vision")
			if ok != (state == "verified") {
				t.Fatalf("state=%s inventory=%v", state, registry.IDs(""))
			}
			if !ok {
				if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Reason, "projector=") {
					t.Fatalf("diagnostics=%v", diagnostics)
				}
				return
			}
			if !vision.Vision {
				t.Fatal("lost vision capability")
			}
			encoded, err := json.Marshal(vision)
			if err != nil || !strings.Contains(string(encoded), `"vision":true`) {
				t.Fatalf("missing JSON capability: %s, %v", encoded, err)
			}
			p := managed.LlamaPresets["vision"]
			p.MMProjArtifact = ""
			managed.LlamaPresets["vision"] = p
			changed, _, err := BuildRegistry(managed, root, []string{"llama-cpp"}, "cpu", nil, "rocm")
			if err != nil || changed.Fingerprint == registry.Fingerprint {
				t.Fatalf("vision change did not alter fingerprint: %v", err)
			}
			frozen, _ := registry.Lookup("vision")
			if !frozen.Vision {
				t.Fatal("catalog mutation changed frozen capability")
			}
		})
	}
}
