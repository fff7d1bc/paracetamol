package contentpack

import (
	"os"
	"path/filepath"
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
