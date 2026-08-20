package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStorageDataDir(t *testing.T) {
	value, found, err := parseStorageDataDir(strings.NewReader("# comment\n[storage]\ndata_dir = \"/mnt/ai/#models\" # trailing\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !found || value != "/mnt/ai/#models" {
		t.Fatalf("value = %q, found = %v", value, found)
	}
}

func TestParseStorageDataDirRejectsUnknownSettings(t *testing.T) {
	_, _, err := parseStorageDataDir(strings.NewReader("[storage]\ncache = '/tmp'\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %v", err)
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

func TestValidateListenAddressDoesNotResolveNames(t *testing.T) {
	if err := ValidateListenAddress("aion.local"); err == nil {
		t.Fatal("hostname unexpectedly accepted")
	}
	if err := ValidateListenAddress("0.0.0.0"); err != nil {
		t.Fatalf("IPv4 rejected: %v", err)
	}
}
