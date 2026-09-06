package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"paracetamol/internal/identity"
)

func TestGatewayKeyFilesAndIndependentClientPrecedence(t *testing.T) {
	root := t.TempDir()
	files := []string{filepath.Join(root, "server.key"), filepath.Join(root, "client.key"), filepath.Join(root, "flag.key")}
	for index, file := range files {
		if err := os.WriteFile(file, []byte(strings.Repeat(string(rune('a'+index)), 64)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configuration := Configuration{Gateway: GatewayConfiguration{APIKeyFile: &files[0], Client: GatewayClientConfiguration{APIKeyFile: &files[1]}}}
	key, err := SelectGatewayServerKey("", nil, configuration)
	if err != nil || key != strings.Repeat("a", 64) {
		t.Fatalf("server selection: %v", err)
	}
	key, err = SelectGatewayClientKey("", nil, configuration)
	if err != nil || key != strings.Repeat("b", 64) {
		t.Fatalf("client selection: %v", err)
	}
	prefix := identity.EnvironmentPrefix()
	environment := map[string]string{prefix + "_GATEWAY_API_KEY_FILE": files[0], prefix + "_GATEWAY_CLIENT_API_KEY_FILE": files[0]}
	key, err = SelectGatewayClientKey(files[2], environment, configuration)
	if err != nil || key != strings.Repeat("c", 64) {
		t.Fatalf("flag selection: %v", err)
	}
	key, err = SelectGatewayClientKey("", environment, configuration)
	if err != nil || key != strings.Repeat("a", 64) {
		t.Fatalf("env selection: %v", err)
	}
	delete(environment, prefix+"_GATEWAY_CLIENT_API_KEY_FILE")
	configuration.Gateway.Client.APIKeyFile = nil
	key, err = SelectGatewayClientKey("", environment, configuration)
	if err != nil || key != "" {
		t.Fatal("client inherited local server key")
	}
	environment[prefix+"_GATEWAY_API_KEY_FILE"] = ""
	if _, err := SelectGatewayServerKey("", environment, configuration); err == nil {
		t.Fatal("empty environment disabled authentication")
	}
	empty := ""
	configuration.Gateway.APIKeyFile = &empty
	if _, err := SelectGatewayServerKey("", nil, configuration); err == nil {
		t.Fatal("empty configured path disabled authentication")
	}
}

func TestGatewayKeyFileFailsClosedWithoutExposingItsContent(t *testing.T) {
	const secret = "sensitive-token-that-must-not-be-in-errors"
	for _, test := range []struct {
		name, content string
		mode          os.FileMode
	}{
		{"empty", "", 0o600}, {"short", "short", 0o600}, {"multiline", secret + "\n" + secret, 0o600},
		{"interpolation", "!" + secret, 0o600}, {"environment", "$" + secret, 0o600}, {"public", secret, 0o644},
		{"large", strings.Repeat(secret, 30), 0o600}, {"whitespace", " " + secret, 0o600},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(file, []byte(test.content), test.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(file, test.mode); err != nil {
				t.Fatal(err)
			}
			_, err := SelectGatewayServerKey(file, nil, Configuration{})
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte(secret+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := SelectGatewayServerKey(target, nil, Configuration{}); err != nil || got != secret {
		t.Fatalf("CRLF: %v", err)
	}
	link, fifo := filepath.Join(root, "link"), filepath.Join(root, "fifo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{link, fifo, root, filepath.Join(root, "missing"), "relative.key"} {
		if _, err := SelectGatewayServerKey(file, nil, Configuration{}); err == nil {
			t.Fatalf("accepted %s", file)
		}
	}
}
