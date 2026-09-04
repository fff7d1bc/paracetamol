// Package config owns host defaults, applications, and configuration precedence.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"paracetamol/internal/application"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
)

type Application = application.Spec

var (
	ROCmRuntimeImage  = mustBuildUnit(application.BuildRuntime).Image
	ROCmBaseImage     = mustBuildUnit(application.BuildPyTorchBase).Image
	ContentToolsImage = mustBuildUnit(application.BuildContentTools).Image
)

const (
	DefaultListen      = "127.0.0.1"
	DefaultGatewayPort = 7455
	DefaultGatewayURL  = "http://127.0.0.1:7455/v1"
	maxFileSize        = 64 * 1024
)

// Selection chooses the one host configuration source. An empty selection
// uses the optional XDG path; an explicit path must exist.
type Selection struct {
	Path     string
	Disabled bool
}

type Configuration struct {
	Storage StorageConfiguration
	Gateway GatewayConfiguration

	Path   string
	Loaded bool
}

type StorageConfiguration struct {
	DataDir *string
}

type GatewayConfiguration struct {
	Applications   []string
	Profile        *string
	RenderNodes    []string
	Listen         *string
	Port           *int
	StartupTimeout *string
	Client         GatewayClientConfiguration
	LlamaCPP       GatewayLlamaConfiguration
}

type GatewayClientConfiguration struct {
	URL *string
}

type GatewayLlamaConfiguration struct {
	Backend   *string
	ModelsMax *int
}

func ApplicationByID(identifier string) (Application, bool) { return application.ByID(identifier) }
func Applications() []Application                           { return application.All() }

func mustBuildUnit(identifier application.BuildID) application.BuildUnit {
	unit, ok := application.BuildUnitByID(string(identifier))
	if !ok {
		panic("application registry lacks build unit " + string(identifier))
	}
	return unit
}

func EnvironmentValue(environment map[string]string, name, fallback string) string {
	if value, ok := environment[identity.EnvironmentPrefix()+"_"+name]; ok {
		return value
	}
	return fallback
}

func ConfigFile(environment map[string]string) (string, bool, error) {
	root := environment["XDG_CONFIG_HOME"]
	if root == "" {
		home := environment["HOME"]
		if home == "" {
			return "", false, nil
		}
		root = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(root) {
		return "", false, controlerr.New("XDG_CONFIG_HOME must be an absolute path")
	}
	return filepath.Join(root, identity.StateNamespace, "config.toml"), true, nil
}

func ResolveFile(environment map[string]string, selection Selection) (string, bool, bool, error) {
	if selection.Disabled && selection.Path != "" {
		return "", false, false, controlerr.Usage("configuration path and disabled configuration are mutually exclusive")
	}
	if selection.Disabled {
		return "", false, false, nil
	}
	if selection.Path != "" {
		path, err := filepath.Abs(selection.Path)
		if err != nil {
			return "", false, false, fmt.Errorf("resolve configuration path %s: %w", selection.Path, err)
		}
		return filepath.Clean(path), true, true, nil
	}
	path, available, err := ConfigFile(environment)
	return path, available, false, err
}

func Load(environment map[string]string, selection Selection) (Configuration, error) {
	path, available, explicit, err := ResolveFile(environment, selection)
	if err != nil || !available {
		return Configuration{}, err
	}
	status, err := os.Lstat(path)
	if os.IsNotExist(err) && !explicit {
		return Configuration{}, nil
	}
	if os.IsNotExist(err) {
		return Configuration{}, controlerr.New("configuration does not exist: %s", path)
	}
	if err != nil {
		return Configuration{}, controlerr.New("cannot inspect configuration %s: %v", path, err)
	}
	if !status.Mode().IsRegular() {
		return Configuration{}, controlerr.New("configuration is not a regular file: %s", path)
	}
	if status.Size() > maxFileSize {
		return Configuration{}, controlerr.New("configuration is unexpectedly large: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return Configuration{}, controlerr.New("cannot read configuration %s: %v", path, err)
	}
	configuration, err := parseConfiguration(contents)
	if err != nil {
		return Configuration{}, controlerr.New("cannot read configuration %s: %v", path, err)
	}
	if err := validateConfiguration(configuration, path); err != nil {
		return Configuration{}, err
	}
	configuration.Path = path
	configuration.Loaded = true
	return configuration, nil
}

func validateConfiguration(configuration Configuration, path string) error {
	if value := configuration.Storage.DataDir; value != nil {
		if *value == "" || !filepath.IsAbs(*value) {
			return controlerr.New("[storage].data_dir must be a non-empty absolute path: %s", path)
		}
	}
	stringsToValidate := []struct {
		key   string
		value *string
	}{
		{"[gateway].profile", configuration.Gateway.Profile},
		{"[gateway].listen", configuration.Gateway.Listen},
		{"[gateway].startup_timeout", configuration.Gateway.StartupTimeout},
		{"[gateway.client].url", configuration.Gateway.Client.URL},
		{"[gateway.llama-cpp].backend", configuration.Gateway.LlamaCPP.Backend},
	}
	for _, setting := range stringsToValidate {
		if setting.value != nil && strings.TrimSpace(*setting.value) == "" {
			return controlerr.New("%s must be a non-empty string: %s", setting.key, path)
		}
	}
	if value := configuration.Gateway.Port; value != nil && (*value < 1 || *value > 65535) {
		return controlerr.New("[gateway].port must be between 1 and 65535: %s", path)
	}
	if value := configuration.Gateway.LlamaCPP.ModelsMax; value != nil && *value < 1 {
		return controlerr.New("[gateway.llama-cpp].models_max must be at least 1: %s", path)
	}
	if value := configuration.Gateway.Client.URL; value != nil {
		if _, err := NormalizeGatewayURL(*value); err != nil {
			return controlerr.New("invalid [gateway.client].url in %s: %v", path, err)
		}
	}
	return nil
}

func DefaultDataDir(environment map[string]string) (string, error) {
	configuration, err := Load(environment, Selection{})
	if err != nil {
		return "", err
	}
	return DefaultDataDirWithConfiguration(environment, configuration)
}

func DefaultDataDirWithConfiguration(environment map[string]string, configuration Configuration) (string, error) {
	if configuration.Storage.DataDir != nil {
		return *configuration.Storage.DataDir, nil
	}
	root := environment["XDG_DATA_HOME"]
	if root == "" {
		home := environment["HOME"]
		if home == "" {
			return "", controlerr.New("HOME is not set")
		}
		root = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(root) {
		return "", controlerr.New("XDG_DATA_HOME must be an absolute path")
	}
	return filepath.Join(root, identity.StateNamespace), nil
}

func SelectDataDir(flagValue string, environment map[string]string, configuration Configuration) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if value := EnvironmentValue(environment, "DATA_DIR", ""); value != "" {
		return value, nil
	}
	return DefaultDataDirWithConfiguration(environment, configuration)
}

// DefaultContents returns a complete runnable configuration. Empty application
// and render-node lists preserve automatic discovery rather than making those
// selections mandatory.
func DefaultContents(dataDir string) []byte {
	return []byte("# Paracetamol host configuration. Command-line flags override corresponding\n" +
		"# environment variables where defined; both override these values.\n\n" +
		"[storage]\n" +
		"data_dir = " + strconv.Quote(dataDir) + "\n\n" +
		"[gateway]\n" +
		"# Empty lists keep automatic backend and render-node discovery.\n" +
		"# Loopback is always available; a non-loopback listen address adds publication.\n" +
		"applications = []\n" +
		"profile = \"auto\"\n" +
		"render_nodes = []\n" +
		"listen = \"127.0.0.1\"\n" +
		"port = " + strconv.Itoa(DefaultGatewayPort) + "\n" +
		"startup_timeout = \"30m\"\n\n" +
		"[gateway.client]\n" +
		"url = " + strconv.Quote(DefaultGatewayURL) + "\n\n" +
		"[gateway.llama-cpp]\n" +
		"backend = \"rocm\"\n" +
		"models_max = 1\n")
}

// SelectGatewayClientURL applies the public client precedence and returns a
// canonical OpenAI-compatible gateway base URL.
func SelectGatewayClientURL(flagValue string, environment map[string]string, configuration Configuration) (string, error) {
	configured := ""
	if configuration.Gateway.Client.URL != nil {
		configured = *configuration.Gateway.Client.URL
	}
	for _, candidate := range []string{flagValue, EnvironmentValue(environment, "GATEWAY_URL", ""), configured, DefaultGatewayURL} {
		if candidate != "" {
			return NormalizeGatewayURL(candidate)
		}
	}
	panic("gateway client URL precedence lacks a default")
}

// NormalizeGatewayURL validates the intentionally narrow client endpoint
// contract shared by configuration, status, and managed coding agents.
func NormalizeGatewayURL(value string) (string, error) {
	if value == "" || strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	}) >= 0 {
		return "", fmt.Errorf("gateway URL must not be empty or contain whitespace")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", fmt.Errorf("gateway URL must be a credential-free HTTP(S) URL without a query or fragment")
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return "", fmt.Errorf("invalid gateway URL port")
	}
	if port := parsed.Port(); port != "" {
		parsedPort, err := strconv.Atoi(port)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return "", fmt.Errorf("invalid gateway URL port")
		}
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != "/v1" {
		return "", fmt.Errorf("gateway URL path must be /v1")
	}
	parsed.Path, parsed.RawPath = "/v1", ""
	return parsed.String(), nil
}

func ValidatePort(value string) (int, error) {
	if value == "" || strings.IndexFunc(value, func(character rune) bool {
		return character < '0' || character > '9'
	}) >= 0 {
		return 0, controlerr.Usage("port must be an integer")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, controlerr.Usage("port must be an integer")
	}
	if port < 1 || port > 65535 {
		return 0, controlerr.Usage("port must be between 1 and 65535")
	}
	return port, nil
}

func ValidateListenAddress(value string) error {
	if net.ParseIP(value) == nil {
		return controlerr.Usage("listen address must be an IPv4 or IPv6 address")
	}
	return nil
}
