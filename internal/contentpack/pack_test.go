package contentpack

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
)

const validPack = `{
  "schema_version": 2,
  "artifacts": {
    "import-model": {
      "description": "fixture",
      "source": {
        "repository": "example/model",
        "revision": "0123456789abcdef0123456789abcdef01234567",
        "path": "weights/model.gguf"
      },
      "target": "llama-models",
      "destination": "imported/model.gguf",
      "size": 123,
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "license": {
        "spdx": "NOASSERTION",
        "status": "unverified",
        "url": "https://huggingface.co/example/model",
        "warning": "not independently verified",
        "upstream_repository": "example/model",
        "upstream_license": "unknown",
        "upstream_license_url": "https://huggingface.co/example/model"
      }
    }
  },
  "bundles": {
    "import-model": {
      "description": "fixture",
      "application": "llama-cpp",
      "artifacts": ["import-model"],
      "groups": ["all", "llama"]
    }
  }
}`

func loadText(t *testing.T, base catalog.Catalog, contents string) (catalog.Catalog, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "pack.json")
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(base, []string{file})
	return loaded, err
}

func emptyCatalog() catalog.Catalog {
	return catalog.Catalog{Artifacts: map[string]catalog.Artifact{}, Bundles: map[string]catalog.Bundle{}}
}

func TestLoadValidNarrowPack(t *testing.T) {
	loaded, err := loadText(t, emptyCatalog(), validPack)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Artifacts["import-model"].Source.Provider != "huggingface" || loaded.Bundles["import-model"].Application != "llama-cpp" {
		t.Fatalf("unexpected loaded pack: %#v", loaded)
	}
}

func fixturePack(t *testing.T) document {
	t.Helper()
	var pack document
	if err := json.Unmarshal([]byte(validPack), &pack); err != nil {
		t.Fatal(err)
	}
	return pack
}

func writePack(t *testing.T, pack document) string {
	t.Helper()
	contents, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "pack.json")
	if err := os.WriteFile(file, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestLoadRejectsDestinationCollisions(t *testing.T) {
	for _, shape := range []struct{ name, first, second, want string }{
		{"exact", "imported/model.gguf", "imported/model.gguf", "share destination"},
		{"parent-first", "imported/model.gguf", "imported/model.gguf/nested/child.gguf", "overlapping destinations"},
		{"child-first", "imported/model.gguf/nested/child.gguf", "imported/model.gguf", "overlapping destinations"},
	} {
		for _, scope := range []string{"base", "same-pack", "cross-pack"} {
			for _, sameDigest := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/same-digest=%t", shape.name, scope, sameDigest), func(t *testing.T) {
					pack := fixturePack(t)
					original := pack.Artifacts["import-model"]
					original.Destination = shape.first
					pack.Artifacts["import-model"] = original
					other := original
					other.Destination = shape.second
					if !sameDigest {
						other.SHA256 = strings.Repeat("1", 64)
					}
					base := emptyCatalog()
					var files []string
					switch scope {
					case "base":
						base.Artifacts["existing-model"] = catalog.Artifact{ID: "existing-model", Target: other.Target, Destination: other.Destination, SHA256: other.SHA256, Size: other.Size}
						files = []string{writePack(t, pack)}
					case "same-pack":
						pack.Artifacts["second-model"] = other
						bundle := pack.Bundles["import-model"]
						bundle.Artifacts = append(bundle.Artifacts, "second-model")
						pack.Bundles["import-model"] = bundle
						files = []string{writePack(t, pack)}
					case "cross-pack":
						second := fixturePack(t)
						second.Artifacts = map[string]rawArtifact{"second-model": other}
						bundle := second.Bundles["import-model"]
						bundle.Artifacts = []string{"second-model"}
						second.Bundles = map[string]rawBundle{"second-model": bundle}
						files = []string{writePack(t, pack), writePack(t, second)}
					}
					before := base
					before.Artifacts = maps.Clone(base.Artifacts)
					before.Bundles = maps.Clone(base.Bundles)
					loaded, selected, err := Load(base, files)
					if err == nil || !strings.Contains(err.Error(), shape.want) {
						t.Fatalf("Load collision = %v", err)
					}
					if len(loaded.Artifacts) != 0 || len(selected) != 0 {
						t.Fatal("rejected merge returned partial catalog or selection")
					}
					if !reflect.DeepEqual(base, before) {
						t.Fatal("rejected merge mutated the input catalog")
					}
				})
			}
		}
	}
}

func TestLoadAllowsSameDestinationInDifferentTargets(t *testing.T) {
	for _, destination := range []string{"imported/fixture.json", "imported", "imported/fixture.json/nested/child.json"} {
		t.Run(destination, func(t *testing.T) {
			pack := fixturePack(t)
			artifact := pack.Artifacts["import-model"]
			artifact.Destination = "imported/fixture.json"
			pack.Artifacts["import-model"] = artifact
			base := emptyCatalog()
			for _, target := range []string{"models", "workflows"} {
				base.Artifacts[target] = catalog.Artifact{ID: target, Target: target, Destination: destination}
			}
			loaded, selected, err := Load(base, []string{writePack(t, pack)})
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Artifacts) != 3 || len(selected) != 1 {
				t.Fatalf("unexpected merged catalog: %#v, %v", loaded, selected)
			}
		})
	}
}

func TestLoadRejectsExternalArtifactReferences(t *testing.T) {
	for _, scope := range []string{"base", "cross-pack"} {
		t.Run(scope, func(t *testing.T) {
			base := emptyCatalog()
			var files []string
			if scope == "base" {
				base.Artifacts["external-model"] = catalog.Artifact{ID: "external-model"}
			} else {
				external := fixturePack(t)
				artifact := external.Artifacts["import-model"]
				artifact.Destination = "external/model.gguf"
				external.Artifacts = map[string]rawArtifact{"external-model": artifact}
				bundle := external.Bundles["import-model"]
				bundle.Artifacts = []string{"external-model"}
				external.Bundles = map[string]rawBundle{"external-model": bundle}
				files = append(files, writePack(t, external))
			}
			pack := fixturePack(t)
			bundle := pack.Bundles["import-model"]
			bundle.Artifacts = []string{"external-model"}
			pack.Bundles["import-model"] = bundle
			files = append(files, writePack(t, pack))
			if _, _, err := Load(base, files); err == nil || !strings.Contains(err.Error(), "references external artifact") {
				t.Fatalf("Load external artifact reference = %v", err)
			}
		})
	}
}

func TestLoadApplicationTargetBoundary(t *testing.T) {
	for application, group := range map[string]string{"llama-cpp": "llama", "comfyui": "comfyui"} {
		for target, owner := range map[string]string{"llama-models": "llama-cpp", "models": "comfyui", "workflows": "comfyui", "unknown": ""} {
			t.Run(application+"/"+target, func(t *testing.T) {
				contents := strings.NewReplacer(
					`"application": "llama-cpp"`, `"application": "`+application+`"`,
					`["all", "llama"]`, `["all", "`+group+`"]`,
					`"target": "llama-models"`, `"target": "`+target+`"`,
				).Replace(validPack)
				if target == "workflows" {
					contents = strings.ReplaceAll(contents, "model.gguf", "workflow.json")
				}
				loaded, err := loadText(t, emptyCatalog(), contents)
				if application != owner {
					if err == nil {
						t.Fatal("accepted an artifact outside the bundle's application partition")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if loaded.Artifacts["import-model"].Target != target || loaded.Bundles["import-model"].Application != application {
					t.Fatal("application or target changed during load")
				}
			})
		}
	}
}

func TestLoadRejectsRetiredDwarfStarDeclarations(t *testing.T) {
	for _, contents := range []string{
		strings.Replace(validPack, `"llama-cpp"`, `"dwarfstar"`, 1),
		strings.Replace(validPack, `"llama-models"`, `"dwarfstar-models"`, 1),
		strings.Replace(validPack, `["all", "llama"]`, `["all", "dwarfstar"]`, 1),
		strings.Replace(validPack, `"schema_version": 2,`, `"schema_version": 2, "dwarfstar_presets": {},`, 1),
	} {
		if _, err := loadText(t, emptyCatalog(), contents); err == nil {
			t.Fatalf("accepted retired DwarfStar declaration:\n%s", contents)
		}
	}
}

func TestLoadRejectsNestedUnknownFieldsAndUnsupportedProvider(t *testing.T) {
	for _, contents := range []string{
		strings.Replace(validPack, `"warning": "not independently verified",`, `"warning": "not independently verified", "surprise": true,`, 1),
		strings.Replace(validPack, `"repository": "example/model",`, `"provider": "other", "repository": "example/model",`, 1),
		strings.Replace(validPack, `["all", "llama"]`, `["all", "qwen"]`, 1),
	} {
		if _, err := loadText(t, emptyCatalog(), contents); err == nil {
			t.Fatalf("accepted invalid pack:\n%s", contents)
		}
	}
}

func TestLoadFailureDoesNotMutateBaseCatalog(t *testing.T) {
	base := emptyCatalog()
	base.Artifacts["import-model"] = catalog.Artifact{ID: "import-model", SHA256: "existing"}
	if _, err := loadText(t, base, validPack); err == nil {
		t.Fatal("accepted artifact collision")
	}
	if len(base.Artifacts) != 1 || base.Artifacts["import-model"].SHA256 != "existing" || len(base.Bundles) != 0 {
		t.Fatalf("base catalog mutated after failed load: %#v", base)
	}
}

func TestLoadRequiresExactCivitaiDownloadURL(t *testing.T) {
	contents := strings.Replace(validPack, `"repository": "example/model",
        "revision": "0123456789abcdef0123456789abcdef01234567",
        "path": "weights/model.gguf"`, `"provider": "civitai",
        "host": "civitai.com",
        "filename": "model.gguf",
        "download_url": "https://example.com/api/download/models/456",
        "model_id": 123,
        "model_version_id": 456,
        "requires_auth": false`, 1)
	if _, err := loadText(t, emptyCatalog(), contents); err == nil {
		t.Fatal("accepted cross-host Civitai download URL")
	}
}

func TestLoadRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pack.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":2,"artifacts":{},"bundles":{}} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := emptyCatalog()
	if _, _, err := Load(base, []string{path}); err == nil {
		t.Fatal("accepted trailing JSON")
	}
}
