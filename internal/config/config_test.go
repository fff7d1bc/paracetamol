package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/project"
)

func TestLoadDecodesCompleteConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "host.toml")
	contents := `[storage]
data_dir = "/srv/ai"

[gateway]
applications = ["llama-cpp", "dwarfstar"]
profile = "strix-halo"
render_nodes = ["/dev/dri/renderD128"]
listen = "192.168.1.50"
port = 18080
startup_timeout = "45m"

[gateway.llama-cpp]
backend = "vulkan"
models_max = 2
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(map[string]string{}, Selection{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Loaded || configuration.Path != path || configuration.Storage.DataDir == nil || *configuration.Storage.DataDir != "/srv/ai" {
		t.Fatalf("configuration=%#v", configuration)
	}
	if !reflect.DeepEqual(configuration.Gateway.Applications, []string{"llama-cpp", "dwarfstar"}) ||
		!reflect.DeepEqual(configuration.Gateway.RenderNodes, []string{"/dev/dri/renderD128"}) ||
		configuration.Gateway.LlamaCPP.ModelsMax == nil || *configuration.Gateway.LlamaCPP.ModelsMax != 2 {
		t.Fatalf("gateway=%#v", configuration.Gateway)
	}
}

func TestLoadDecodesSupportedTOMLForms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := `# Leading comments and comments after values are supported.
[storage] # storage comment
data_dir = '/srv/ai#literal'

[gateway]
applications = ["llama,cpp", 'dwarf#star', "quoted\"name",] # array comment
profile = "strix\u002dhalo"
render_nodes = ["/dev/dri/renderD128=primary"]
listen = "127.0.0.1"
port = 7_455
startup_timeout = "30m"

[gateway.llama-cpp]
backend = 'rocm'
models_max = +2
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(map[string]string{}, Selection{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Storage.DataDir == nil || *configuration.Storage.DataDir != "/srv/ai#literal" ||
		!reflect.DeepEqual(configuration.Gateway.Applications, []string{"llama,cpp", "dwarf#star", `quoted"name`}) ||
		configuration.Gateway.Profile == nil || *configuration.Gateway.Profile != "strix-halo" ||
		!reflect.DeepEqual(configuration.Gateway.RenderNodes, []string{"/dev/dri/renderD128=primary"}) ||
		configuration.Gateway.Port == nil || *configuration.Gateway.Port != 7455 ||
		configuration.Gateway.LlamaCPP.Backend == nil || *configuration.Gateway.LlamaCPP.Backend != "rocm" ||
		configuration.Gateway.LlamaCPP.ModelsMax == nil || *configuration.Gateway.LlamaCPP.ModelsMax != 2 {
		t.Fatalf("configuration=%#v", configuration)
	}
}

func TestLoadRejectsUnknownAndInvalidSettings(t *testing.T) {
	for name, test := range map[string]struct {
		contents string
		contains string
	}{
		"unknown section":     {"[other]\n", "unknown configuration section"},
		"unknown setting":     {"[gateway]\nmodelz_max = 2\n", "unknown configuration setting"},
		"outside section":     {"port = 7455\n", "outside a configuration section"},
		"data path":           {"[storage]\ndata_dir = 'relative'\n", "non-empty absolute path"},
		"port range":          {"[gateway]\nport = 70000\n", "between 1 and 65535"},
		"models range":        {"[gateway.llama-cpp]\nmodels_max = 0\n", "at least 1"},
		"invalid type":        {"[gateway]\nport = '8080'\n", "decimal integer"},
		"duplicate setting":   {"[gateway]\nport = 8080\nport = 8081\n", "duplicate configuration setting"},
		"duplicate section":   {"[gateway]\n[gateway]\n", "duplicate configuration section"},
		"malformed section":   {"[gateway\n", "malformed configuration section"},
		"array section":       {"[[gateway]]\n", "malformed configuration section"},
		"missing value":       {"[gateway]\nport =\n", "expected key = value"},
		"unquoted string":     {"[gateway]\nprofile = auto\n", "quoted string"},
		"unsupported escape":  {"[gateway]\nprofile = \"a\\x62\"\n", "unsupported escape sequence"},
		"unterminated string": {"[gateway]\nprofile = \"auto\n", "unterminated quoted value"},
		"numeric array item":  {"[gateway]\napplications = [1]\n", "quoted string"},
		"missing array comma": {"[gateway]\napplications = [\"llama-cpp\" \"dwarfstar\"]\n", "comma"},
		"multiline array":     {"[gateway]\napplications = [\n  \"llama-cpp\",\n]\n", "one-line string array"},
		"leading zero":        {"[gateway]\nport = 07455\n", "leading zero"},
		"bad separator":       {"[gateway]\nport = 7__455\n", "decimal integer"},
		"dotted assignment":   {"[gateway]\nllama-cpp.models_max = 2\n", "unknown configuration setting"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(map[string]string{}, Selection{Path: path}); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error=%v, want substring %q", err, test.contains)
			}
		})
	}
}

func TestLoadRejectsInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte{'[', 'g', 'a', 't', 'e', 'w', 'a', 'y', ']', '\n', 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(map[string]string{}, Selection{Path: path}); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("error=%v", err)
	}
}

func TestLoadRejectsNonRegularAndOversizedFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := Load(map[string]string{}, Selection{Path: root}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory error=%v", err)
	}
	path := filepath.Join(root, "large.toml")
	if err := os.WriteFile(path, []byte(strings.Repeat("#", maxFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(map[string]string{}, Selection{Path: path}); err == nil || !strings.Contains(err.Error(), "unexpectedly large") {
		t.Fatalf("large error=%v", err)
	}
}

func TestLoadTreatsEverySettingAsOptional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[storage]\n[gateway]\n[gateway.llama-cpp]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(map[string]string{}, Selection{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Loaded || configuration.Storage.DataDir != nil || configuration.Gateway.Profile != nil || configuration.Gateway.LlamaCPP.Backend != nil {
		t.Fatalf("configuration=%#v", configuration)
	}
}

func TestDefaultDataDirUsesConfiguration(t *testing.T) {
	root := t.TempDir()
	configuration := filepath.Join(root, "config", "paracetamol")
	if err := os.MkdirAll(configuration, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configuration, "config.toml"), []byte("[storage]\ndata_dir = '/srv/ai'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DefaultDataDir(map[string]string{"HOME": root, "XDG_CONFIG_HOME": filepath.Join(root, "config")})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/srv/ai" {
		t.Fatalf("data dir = %q", got)
	}
}

func TestDefaultContentsIsCompleteAndRunnable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, DefaultContents("/srv/paracetamol"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(map[string]string{}, Selection{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Storage.DataDir == nil || *configuration.Storage.DataDir != "/srv/paracetamol" ||
		configuration.Gateway.Profile == nil || *configuration.Gateway.Profile != "auto" ||
		configuration.Gateway.Applications == nil || len(configuration.Gateway.Applications) != 0 ||
		configuration.Gateway.RenderNodes == nil || len(configuration.Gateway.RenderNodes) != 0 ||
		configuration.Gateway.Port == nil || *configuration.Gateway.Port != DefaultGatewayPort ||
		configuration.Gateway.LlamaCPP.Backend == nil || *configuration.Gateway.LlamaCPP.Backend != "rocm" {
		t.Fatalf("configuration=%#v", configuration)
	}
}

func TestTrackedExampleLoads(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(map[string]string{}, Selection{Path: filepath.Join(root, "config.example.toml")})
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Loaded || configuration.Storage.DataDir != nil || configuration.Gateway.Applications == nil || configuration.Gateway.RenderNodes == nil || configuration.Gateway.Port == nil || *configuration.Gateway.Port != DefaultGatewayPort {
		t.Fatalf("configuration=%#v", configuration)
	}
}

func TestGatewayDefaultsAgree(t *testing.T) {
	if DefaultGatewayPort != 7455 || DefaultGatewayURL != "http://127.0.0.1:7455/v1" {
		t.Fatalf("port=%d URL=%q", DefaultGatewayPort, DefaultGatewayURL)
	}
}

func TestExplicitMissingConfigurationFailsWhileDefaultIsOptional(t *testing.T) {
	root := t.TempDir()
	environment := map[string]string{"HOME": root}
	if configuration, err := Load(environment, Selection{}); err != nil || configuration.Loaded {
		t.Fatalf("optional configuration=%#v err=%v", configuration, err)
	}
	_, err := Load(environment, Selection{Path: filepath.Join(root, "missing.toml")})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("explicit error=%v", err)
	}
}

func TestDisabledConfigurationBypassesXDGFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "paracetamol", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[broken]\nvalue = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(map[string]string{"HOME": root, "XDG_CONFIG_HOME": root}, Selection{Disabled: true})
	if err != nil || configuration.Loaded {
		t.Fatalf("configuration=%#v err=%v", configuration, err)
	}
}

func TestValidateListenAddressDoesNotResolveNames(t *testing.T) {
	if err := ValidateListenAddress("aion.local"); err == nil {
		t.Fatal("hostname unexpectedly accepted")
	}
	if err := ValidateListenAddress("0.0.0.0"); err != nil {
		t.Fatalf("IPv4 rejected: %v", err)
	}
}
