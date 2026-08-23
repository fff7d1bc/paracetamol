package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/process"
	"paracetamol/internal/textmodel"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

type sandboxRunner struct{ bwrap string }

func (sandboxRunner) Run(context.Context, process.Command) (process.Result, error) {
	return process.Result{Status: 1}, nil
}

func (runner sandboxRunner) LookPath(name string) (string, error) {
	if name == "bwrap" && runner.bwrap != "" {
		return runner.bwrap, nil
	}
	return "", errors.New("missing")
}

func newSandboxRunner(t *testing.T) sandboxRunner {
	t.Helper()
	bwrap := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(bwrap, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	return sandboxRunner{bwrap: bwrap}
}

func TestSandboxPlanClearsHostEnvironmentAndMountsOnlyProjectWritable(t *testing.T) {
	dataRoot, workdir, home := t.TempDir(), t.TempDir(), t.TempDir()
	plan, err := CreateSandboxPlan(
		context.Background(), newSandboxRunner(t), []string{"/bin/sh", "-c", "true"},
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
	if info, statErr := os.Stat(standardLinuxbrewPrefix); statErr == nil && info.IsDir() {
		expected := "--ro-bind " + standardLinuxbrewPrefix + " " + standardLinuxbrewPrefix
		if !strings.Contains(joined, expected) {
			t.Fatalf("sandbox did not expose standard Linuxbrew read-only: %s", joined)
		}
	}
}

func TestSandboxAlwaysExposesAvailableLinuxbrewAfterSystemTools(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "home", "linuxbrew", ".linuxbrew")
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	prefixes := linuxbrewPrefixes("/usr/bin/node", prefix)
	if len(prefixes) != 1 || prefixes[0] != prefix {
		t.Fatalf("prefixes=%v", prefixes)
	}
	path := sandboxPath("/usr/bin/node", prefixes)
	if !strings.Contains(path, prefix+"/bin") || strings.Index(path, "/usr/bin") > strings.Index(path, prefix+"/bin") {
		t.Fatalf("managed client PATH does not retain system precedence before Linuxbrew: %s", path)
	}
}

func TestSandboxKeepsClientLinuxbrewPrefixFirstWithoutDuplicates(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), ".linuxbrew")
	executable := filepath.Join(prefix, "Cellar", "maki", "1.0", "bin", "maki")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	prefixes := linuxbrewPrefixes(executable, prefix)
	if len(prefixes) != 1 || prefixes[0] != prefix {
		t.Fatalf("prefixes=%v", prefixes)
	}
	if path := sandboxPath(executable, prefixes); !strings.HasPrefix(path, prefix+"/bin:"+prefix+"/sbin:") {
		t.Fatalf("Linuxbrew client PATH does not start with its runtime: %s", path)
	}
}

func TestSandboxRejectsWritableWorkdirInsideLinuxbrew(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), ".linuxbrew")
	executable := filepath.Join(prefix, "Cellar", "maki", "1.0", "bin", "maki")
	workdir := filepath.Join(prefix, "project")
	for _, directory := range []string{filepath.Dir(executable), workdir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(executable, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := CreateSandboxPlan(
		context.Background(), newSandboxRunner(t), []string{executable}, t.TempDir(), workdir,
		"maki", nil, map[string]string{"HOME": t.TempDir(), "PATH": "/usr/bin"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "overlaps read-only Linuxbrew prefix") {
		t.Fatalf("err=%v", err)
	}
}

func TestSandboxRejectsWorkdirContainingHostHome(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := CreateSandboxPlan(context.Background(), newSandboxRunner(t), []string{"/bin/sh"}, t.TempDir(), parent, "pi", nil, map[string]string{"HOME": home}, nil)
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
	models, err := AgentModels(managed, []string{"qwen3.8-27b-mtp-ud-q8-k-xl", "deepseek-v4-flash-0731-q2-imatrix"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := PiConfig(managed, "http://127.0.0.1:8080/v1", models)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	providers, ok := document["providers"].(map[string]any)
	if !ok || len(providers) != 1 || providers[ProviderID] == nil {
		t.Fatalf("providers = %#v", document["providers"])
	}
	if len(models) != 2 || models[0].Backend != textmodel.BackendDwarfStar || models[1].Backend != textmodel.BackendLlamaCPP {
		t.Fatalf("selected models = %#v", models)
	}
}

func TestNormalizeGatewayURL(t *testing.T) {
	if value, err := NormalizeGatewayURL("http://aion.local:8080/v1/"); err != nil || value != "http://aion.local:8080/v1" {
		t.Fatalf("value = %q, err = %v", value, err)
	}
	for _, value := range []string{"aion.local:8080/v1", "http://user@aion.local/v1", "http://aion.local/other"} {
		if _, err := NormalizeGatewayURL(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestPiSessionUsesOnlyLiveGatewayInventory(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("path=%s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"object":"list","data":[{"id":"deepseek-v4-flash-0731-q2-imatrix"}]}`))
	}))
	defer server.Close()
	plan, err := CreatePiPlan(context.Background(), managed, projectRoot(t), server.URL+"/v1", nil, PiRuntime{Root: "/runtime", Node: "/node", Entrypoint: "/pi"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.DefaultProvider != ProviderID || plan.DefaultModel != "deepseek-v4-flash-0731-q2-imatrix" || plan.DefaultThinking != "high" {
		t.Fatalf("plan=%#v", plan)
	}
	if strings.Contains(string(plan.Config), RecommendedModel) {
		t.Fatalf("unadvertised model leaked into Pi config: %s", plan.Config)
	}
}

func TestPiManagementDoesNotContactGateway(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CreatePiPlan(context.Background(), managed, projectRoot(t), "http://127.0.0.1:1/v1", []string{"list"}, PiRuntime{Root: "/runtime", Node: "/node", Entrypoint: "/pi"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "management" || len(plan.Config) == 0 {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestMakiStateRemovesOnlyRecognizedLegacyDwarfStarProvider(t *testing.T) {
	root := t.TempDir()
	plan := MakiPlan{Providers: map[string][]byte{ProviderID: []byte("#!/bin/sh\n")}, Init: []byte("maki.setup({})\n"), Tiers: []byte("{}\n")}
	paths, err := PrepareMakiState(plan, root)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(paths.Config, "maki", "providers", "dwarfstar")
	if err := os.WriteFile(legacy, []byte("#!/bin/sh\nset -eu\n# Paracetamol DwarfStar\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareMakiState(plan, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy provider still exists: %v", err)
	}
}
