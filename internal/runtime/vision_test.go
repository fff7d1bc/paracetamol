package runtime

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
)

func TestProjectorDirectAndRouterMapping(t *testing.T) {
	command, err := LlamaCommand(LlamaOptions{Image: "image", Mode: "server", Profile: "cpu", Backend: "rocm", DataDir: "/data", ManagedModel: "target.gguf", ManagedMMProj: "vision/projector.gguf", Listen: "127.0.0.1", Port: 8080}, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	if !strings.Contains(joined, "PARACETAMOL_LLAMA_MMPROJ=/content/models/vision/projector.gguf") || strings.Contains(joined, "/dev/kfd") {
		t.Fatalf("wrong projector mapping/security: %s", joined)
	}
	managed := catalog.Catalog{
		Artifacts:    map[string]catalog.Artifact{"target": {Destination: "target.gguf"}, "projector": {Destination: "vision/projector.gguf"}},
		LlamaPresets: map[string]catalog.LlamaPreset{"vision": {Artifact: "target", MMProjArtifact: "projector", DefaultContext: 4096}, "text": {Artifact: "target", DefaultContext: 4096}},
	}
	for _, id := range []string{"text", "vision"} {
		contents, err := RenderRouterModels(managed, "rocm", "cpu", []string{id})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(contents, "mmproj = /content/models/vision/projector.gguf\n") != (id == "vision") {
			t.Fatalf("projector policy leaked: %s", contents)
		}
	}
}

func TestRouterRequiresProjectorButDoesNotInstallItsAbsentTarget(t *testing.T) {
	for _, state := range []string{"missing", "unverified", "stale", "verified"} {
		t.Run(state, func(t *testing.T) {
			managed, root, install := routerDraftFixture(t)
			delete(managed.LlamaPresets, "dflash")
			projector := managed.Artifacts["draft"]
			projector.ID, projector.Destination = "projector", "projector.gguf"
			managed.Artifacts["projector"] = projector
			managed.Bundles["base"] = catalog.Bundle{Application: "llama-cpp", Artifacts: []string{"target-1", "target-2", "projector"}}
			preset := managed.LlamaPresets["base"]
			preset.MMProjArtifact = "projector"
			managed.LlamaPresets["base"] = preset
			if state != "missing" {
				install("projector", state != "unverified")
			}
			if state == "stale" {
				path := content.ArtifactPath(root, projector)
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("model"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := RenderRouter(managed, root, "rocm", "cpu"); err == nil || !strings.Contains(err.Error(), "no installed managed") {
				t.Fatalf("projector alone implied target installation: %v", err)
			}
			install("target-1", true)
			install("target-2", true)
			contents, installed, err := RenderRouter(managed, root, "rocm", "cpu")
			if state != "verified" {
				if err == nil || !strings.Contains(err.Error(), "is incomplete") {
					t.Fatalf("accepted required projector state %s: %v", state, err)
				}
			} else if err != nil || !slices.Equal(installed, []string{"base"}) || !strings.Contains(contents, "mmproj = /content/models/projector.gguf\n") {
				t.Fatalf("installed=%v err=%v contents=%s", installed, err, contents)
			}
		})
	}
}

func TestLlamaEntrypointProjectorValidation(t *testing.T) {
	for _, router := range []bool{false, true} {
		for _, state := range []string{"text", "regular", "missing", "directory"} {
			t.Run(state+"/router="+map[bool]string{false: "false", true: "true"}[router], func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "projector.gguf")
				if state == "regular" {
					if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
						t.Fatal(err)
					}
				} else if state == "directory" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				env := []string{}
				var ini func(string) string
				if router {
					ini = func(model string) string {
						text := "version = 1\n[fixture]\nmodel = " + model + "\nc = 4096\n"
						if state != "text" {
							text += "mmproj = " + path + "\n"
						}
						return text
					}
				} else if state != "text" {
					env = append(env, "PARACETAMOL_LLAMA_MMPROJ="+path)
				}
				output, err := runLlamaPolicyEntrypoint(t, "rocm", "cpu", env, ini)
				if state == "missing" || state == "directory" {
					if err == nil || !strings.Contains(output, "projector is not a regular file") || strings.Contains(output, "ARG=<") {
						t.Fatalf("unsafe projector accepted: err=%v output=%s", err, output)
					}
					return
				}
				if err != nil {
					t.Fatalf("err=%v output=%s", err, output)
				}
				if router {
					if strings.Contains(output, "mmproj = "+path+"\n") != (state == "regular") {
						t.Fatalf("wrong router projector: %s", output)
					}
				} else if strings.Contains(output, "ARG=<--mmproj>\nARG=<"+path+">\n") != (state == "regular") {
					t.Fatalf("wrong direct projector: %s", output)
				}
			})
		}
	}
}
