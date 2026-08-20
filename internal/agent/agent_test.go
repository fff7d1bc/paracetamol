package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rocmplete/internal/catalog"
	"rocmplete/internal/process"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

type sandboxRunner struct{}

func (sandboxRunner) Run(context.Context, process.Command) (process.Result, error) {
	return process.Result{Status: 1}, nil
}

func (sandboxRunner) LookPath(name string) (string, error) {
	if name == "bwrap" {
		return "/usr/bin/bwrap", nil
	}
	return "", errors.New("missing")
}

func TestSandboxPlanClearsHostEnvironmentAndMountsOnlyProjectWritable(t *testing.T) {
	dataRoot, workdir, home := t.TempDir(), t.TempDir(), t.TempDir()
	plan, err := CreateSandboxPlan(
		context.Background(), sandboxRunner{}, []string{"/bin/sh", "-c", "true"},
		dataRoot, workdir, "pi", map[string]string{"PI_OFFLINE": "1"},
		map[string]string{"HOME": home, "PATH": "/usr/bin", "SECRET": "not-forwarded", "TERM": "xterm"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Command, " ")
	for _, expected := range []string{"--unshare-all", "--share-net", "--clearenv", "--cap-drop ALL", "--bind " + workdir + " " + workdir, "--setenv PI_OFFLINE 1"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("sandbox lacks %q: %s", expected, joined)
		}
	}
	if strings.Contains(joined, "SECRET") || strings.Contains(joined, home) {
		t.Fatalf("sandbox leaked host state: %s", joined)
	}
}

func TestSandboxRejectsWorkdirContainingHostHome(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := CreateSandboxPlan(context.Background(), sandboxRunner{}, []string{"/bin/sh"}, t.TempDir(), parent, "pi", nil, map[string]string{"HOME": home}, nil)
	if err == nil {
		t.Fatal("sandbox accepted a workdir containing host home")
	}
}

func TestManagedPrivateFileRefusesHardLinks(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	alias := filepath.Join(directory, "alias.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new"), 0o600, false); err == nil {
		t.Fatal("managed private file replaced a hard link")
	}
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
