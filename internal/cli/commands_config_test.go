package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/config"
)

func TestConfigInitCreatesCompleteDefaultOnce(t *testing.T) {
	root := t.TempDir()
	app, stdout, _ := testApp(t, &commandRunner{})
	app.Environment = map[string]string{
		"HOME":            root,
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
	}
	if err := app.commandConfig([]string{"init"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config", "paracetamol", "config.toml")
	configuration, err := config.Load(app.Environment, config.Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Loaded || configuration.Path != path || configuration.Storage.DataDir == nil || *configuration.Storage.DataDir != filepath.Join(root, "data", "paracetamol") {
		t.Fatalf("configuration=%#v", configuration)
	}
	status, err := os.Stat(path)
	if err != nil || status.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", status.Mode(), err)
	}
	if !strings.Contains(stdout.String(), "Configuration created:") || !strings.Contains(stdout.String(), "automatic discovery") {
		t.Fatalf("output=%q", stdout.String())
	}
	if err := app.commandConfig([]string{"init"}); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("second init error=%v", err)
	}
}

func TestConfigInitHonorsExplicitPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profiles", "aion.toml")
	app, _, _ := testApp(t, &commandRunner{})
	app.Environment = map[string]string{"HOME": root}
	app.ConfigSelection = config.Selection{Path: path}
	if err := app.commandConfig([]string{"init"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
