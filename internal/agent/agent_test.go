package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/gateway"
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

func TestClientKeyStaysInPrivateProviderFiles(t *testing.T) {
	const key = "test-key-0123456789abcdef0123456789abcdef"
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := PiConfig(managed, "http://127.0.0.1:7455/v1", nil, key)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Providers map[string]struct {
			APIKey     string `json:"apiKey"`
			AuthHeader bool   `json:"authHeader"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(configuration, &document); err != nil {
		t.Fatal(err)
	}
	if provider := document.Providers[ProviderID]; provider.APIKey != key || !provider.AuthHeader {
		t.Fatal("Pi auth not configured")
	}
	dataRoot := t.TempDir()
	dir, err := PreparePiState(PiPlan{Config: configuration}, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "models.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Pi private mode: %v", err)
	}
	provider := makiProvider("Paracetamol gateway", "http://127.0.0.1:7455/v1", nil, "", key)
	paths, err := PrepareMakiState(MakiPlan{Providers: map[string][]byte{ProviderID: provider}}, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(paths.Config, "maki", "providers", ProviderID)
	info, err = os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("Maki private mode: %v", err)
	}
	output, err := exec.Command("/bin/sh", path, "resolve").Output()
	if err != nil {
		t.Fatal(err)
	}
	var resolved struct {
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(output, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Headers["Authorization"] != "Bearer "+key {
		t.Fatal("Maki resolver did not supply key")
	}
	configuration, err = PiConfig(managed, "http://127.0.0.1:7455/v1", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = PreparePiState(PiPlan{Config: configuration}, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PrepareMakiState(MakiPlan{Providers: map[string][]byte{ProviderID: makiProvider("Paracetamol gateway", "http://127.0.0.1:7455/v1", nil, "", "")}}, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "models.json"), filepath.Join(paths.Config, "maki/providers", ProviderID)} {
		contents, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(contents), key) {
			t.Fatal("relaunch retained old key")
		}
	}
}

func TestClientModeKeepsManagementIndependentOfGatewayCredentials(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--", "--version"}, {"install", "plugin"}, {"update", "--extensions"}, {"auth"}} {
		if PiMode(args) == "session" {
			t.Fatalf("Pi mode for %v", args)
		}
	}
	for _, args := range [][]string{{"--help"}, {"--", "--version"}, {"models"}, {"update"}, {"index", "src"}} {
		if MakiMode(args) == "session" {
			t.Fatalf("Maki mode for %v", args)
		}
	}
	for _, args := range [][]string{nil, {"--print", "hello"}, {"--", "hello"}} {
		if PiMode(args) != "session" || MakiMode(args) != "session" {
			t.Fatalf("session mode for %v", args)
		}
	}
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
	models, err := AgentModels(managed, []string{"unsloth-qwen3.8-27b-mtp-ud-q8-k-xl", "antirez-deepseek-v4-flash-0731-q2-imatrix"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := PiConfig(managed, "http://127.0.0.1:8080/v1", models, "")
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
	provider := providers[ProviderID].(map[string]any)
	compat := provider["compat"].(map[string]any)
	if compat["sendSessionAffinityHeaders"] != true || compat["sessionAffinityFormat"] != "openai-nosession" {
		t.Fatalf("Pi session correlation is not enabled: %#v", compat)
	}
}

func TestSwiftAppearsInBothClientsWithoutChangingDefault(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	models, err := AgentModels(managed, []string{RecommendedModel, "ukisai-swift1.5-qwen3.8-27b-mtp-q8-0"})
	if err != nil {
		t.Fatal(err)
	}
	provider, selected, thinking, err := DefaultModel(models, "Pi")
	if err != nil {
		t.Fatal(err)
	}
	if provider != ProviderID || selected != RecommendedModel || thinking != "medium" {
		t.Fatalf("default = %q/%q at %q", provider, selected, thinking)
	}
	pi, err := PiConfig(managed, "http://127.0.0.1:7455/v1", models, "")
	if err != nil {
		t.Fatal(err)
	}
	maki := makiProvider("Paracetamol gateway", "http://127.0.0.1:7455/v1", makiModels(models), "", "")
	for name, encoded := range map[string][]byte{"Pi": pi, "Maki": maki} {
		for _, id := range []string{RecommendedModel, "ukisai-swift1.5-qwen3.8-27b-mtp-q8-0"} {
			if !bytes.Contains(encoded, []byte(id)) {
				t.Errorf("%s config lacks %q", name, id)
			}
		}
	}
}

func TestMakiProviderAddsOnlyExplicitPerLaunchSessionHeader(t *testing.T) {
	const session = "019fe5cc-5cad-7a92-aead-f0838931fb95"
	withSession := string(makiProvider("Paracetamol gateway", "http://127.0.0.1:8080/v1", nil, session, ""))
	if !strings.Contains(withSession, "X-Paracetamol-Session-ID") || !strings.Contains(withSession, session) {
		t.Fatalf("provider lacks session header: %s", withSession)
	}
	withoutSession := string(makiProvider("Paracetamol gateway", "http://127.0.0.1:8080/v1", nil, "", ""))
	if strings.Contains(withoutSession, "X-Paracetamol-Session-ID") {
		t.Fatalf("provider emitted an empty session header: %s", withoutSession)
	}
	if generated := newSessionID(); len(generated) != 36 || generated[14] != '4' {
		t.Fatalf("generated session id=%q", generated)
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
		writer.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
		_, _ = writer.Write([]byte(`{"object":"list","data":[{"id":"antirez-deepseek-v4-flash-0731-q2-imatrix"}]}`))
	}))
	defer server.Close()
	plan, err := CreatePiPlan(context.Background(), managed, projectRoot(t), server.URL+"/v1", nil, PiRuntime{Root: "/runtime", Node: "/node", Entrypoint: "/pi"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.DefaultProvider != ProviderID || plan.DefaultModel != "antirez-deepseek-v4-flash-0731-q2-imatrix" || plan.DefaultThinking != "high" {
		t.Fatalf("plan=%#v", plan)
	}
	if strings.Contains(string(plan.Config), RecommendedModel) {
		t.Fatalf("unadvertised model leaked into Pi config: %s", plan.Config)
	}
}

func TestGatewayDiscoveryRequiresExactIdentity(t *testing.T) {
	tests := []struct {
		name      string
		marker    string
		status    int
		wantError string
	}{
		{name: "missing marker", status: http.StatusNotFound, wantError: "is not a Paracetamol gateway (HTTP 404)"},
		{name: "wrong marker", marker: "foreign.gateway", status: http.StatusOK, wantError: "is not a Paracetamol gateway (HTTP 200)"},
		{name: "gateway error", marker: gateway.IdentityValue, status: http.StatusServiceUnavailable, wantError: "gateway returned HTTP 503"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if test.marker != "" {
					writer.Header().Set(gateway.IdentityHeader, test.marker)
				}
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			_, err := gateway.FetchModelIDs(context.Background(), server.URL+"/v1", "")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("err=%v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestPiManagementDoesNotContactGateway(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CreatePiPlan(context.Background(), managed, projectRoot(t), "http://127.0.0.1:1/v1", []string{"list"}, PiRuntime{Root: "/runtime", Node: "/node", Entrypoint: "/pi"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "management" || len(plan.Config) == 0 {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestMakiStateRejectsUnmanagedProviderWithoutDeletingIt(t *testing.T) {
	root := t.TempDir()
	plan := MakiPlan{Providers: map[string][]byte{ProviderID: []byte("#!/bin/sh\n")}, Init: []byte("maki.setup({})\n"), Tiers: []byte("{}\n")}
	paths, err := PrepareMakiState(plan, root)
	if err != nil {
		t.Fatal(err)
	}
	unmanaged := filepath.Join(paths.Config, "maki", "providers", "dwarfstar")
	if err := os.WriteFile(unmanaged, []byte("#!/bin/sh\nset -eu\n# Paracetamol DwarfStar\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareMakiState(plan, root); err == nil || !strings.Contains(err.Error(), "unmanaged entry dwarfstar") {
		t.Fatalf("expected unmanaged provider error, got %v", err)
	}
	if _, err := os.Lstat(unmanaged); err != nil {
		t.Fatalf("unmanaged provider was removed: %v", err)
	}
}
