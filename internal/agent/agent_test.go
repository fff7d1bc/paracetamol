package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"rocmplete/internal/catalog"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPinnedPiRuntimeSource(t *testing.T) {
	source, err := LoadPiSource(filepath.Join(projectRoot(t), "agent-clients", "pi"))
	if err != nil {
		t.Fatal(err)
	}
	if source.PackageVersion == "" || len(source.LockSHA256) != 64 {
		t.Fatalf("source = %#v", source)
	}
}

func TestPiConfigExposesOnlyAgentModels(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := PiConfig(managed, "http://127.0.0.1:8080/v1", "http://127.0.0.1:8000/v1")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	providers, ok := document["providers"].(map[string]any)
	if !ok || providers[ProviderID] == nil || providers[DwarfStarProviderID] == nil {
		t.Fatalf("providers = %#v", document["providers"])
	}
}

func TestNormalizeRemoteLlamaURL(t *testing.T) {
	if value, err := NormalizeLlamaURL("http://aion.local:8080/v1/"); err != nil || value != "http://aion.local:8080/v1" {
		t.Fatalf("value = %q, err = %v", value, err)
	}
	for _, value := range []string{"aion.local:8080/v1", "http://user@aion.local/v1", "http://aion.local/other"} {
		if _, err := NormalizeLlamaURL(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
