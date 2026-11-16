package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
)

func TestDirectLlamaPublicationWarnsWithOrWithoutKeyFile(t *testing.T) {
	for _, address := range []struct {
		value string
		warn  bool
	}{
		{"127.0.0.1", false}, {"::1", false},
		{"192.0.2.42", true}, {"2001:db8::42", true},
		{"0.0.0.0", true}, {"::", true},
	} {
		for _, keyFile := range []bool{false, true} {
			name := address.value + "/anonymous"
			if keyFile {
				name = address.value + "/key-file"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				model, key := filepath.Join(root, "model.gguf"), filepath.Join(root, "key.txt")
				const secret = "private-key-sentinel-not-for-output"
				if err := os.WriteFile(model, []byte("fixture only"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(key, []byte(secret), 0o600); err != nil {
					t.Fatal(err)
				}
				data := filepath.Join(root, "absent-data")
				args := []string{"--model", model, "--profile", "cpu", "--listen", address.value, "--data-dir", data, "--dry-run"}
				if keyFile {
					args = append(args, "--api-key-file", key)
				}
				runner := &commandRunner{}
				app, stdout, stderr := testApp(t, runner)
				app.catalog = &catalog.Catalog{}
				if err := app.runLlama("server", args); err != nil {
					t.Fatal(err)
				}
				warning := stderr.String()
				if strings.Contains(warning, "WARNING:") != address.warn {
					t.Fatalf("publication warning=%q, want warning=%t", warning, address.warn)
				}
				if address.warn {
					if !strings.Contains(warning, net.JoinHostPort(address.value, "8080")) {
						t.Fatalf("warning lacks publication endpoint: %q", warning)
					}
					want := "without authentication"
					if keyFile {
						want = "HTTP does not encrypt keys or prompts"
					}
					if !strings.Contains(warning, want) {
						t.Fatalf("warning lacks %q: %q", want, warning)
					}
				}
				if strings.Contains(stdout.String()+warning, secret) {
					t.Fatal("API key contents leaked into output")
				}
				for _, command := range runner.commands {
					if command.Name == "podman" {
						t.Fatalf("dry run invoked Podman: %v", command.Args)
					}
				}
				if _, err := os.Stat(data); !os.IsNotExist(err) {
					t.Fatalf("dry run created persistent data: %v", err)
				}
			})
		}
	}
}
