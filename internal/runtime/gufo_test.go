package runtime

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paracetamol/internal/project"
)

func gufoFixture() GufoOptions {
	return GufoOptions{Image: "localhost/test:gufo", DataDir: "/data-root", Model: "/models/test/one.gguf", ModelFiles: []string{"/models/test/one.gguf", "/models/test/two.gguf"}, ModelID: "test-gufo", Profile: "strix-halo", RenderNodes: []string{"/dev/dri/renderD128"}, Context: 262144, OutputTokens: 16384, CacheRAMBytes: 28 << 30, Reasoning: "medium", DynamicHostPort: true, ContainerName: "paracetamol-gateway-gufo", ContainerRole: "gateway-backend"}
}

func TestGufoCommandConfinement(t *testing.T) {
	command, err := GufoCommand(gufoFixture(), ":rw,Z")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	for _, want := range []string{"--read-only", "--cap-drop all", "--security-opt no-new-privileges", "--device /dev/kfd", "--device /dev/dri/renderD128", "--publish 127.0.0.1::8080", "/models/test/one.gguf:/content/models/one.gguf:ro,z", "/models/test/two.gguf:/content/models/two.gguf:ro,z", "PARACETAMOL_GUFO_CONTEXT=262144", "PARACETAMOL_GUFO_REASONING=medium"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	for _, forbidden := range []string{"--privileged", "--network host", "renderD129", "/models/test:/content", "dwarfstar", "pytorch"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("unexpected %q in %s", forbidden, joined)
		}
	}
}

func TestGufoCommandRejectsUnsafeAndUnsupportedOptions(t *testing.T) {
	for name, mutate := range map[string]func(*GufoOptions){
		"cpu":             func(o *GufoOptions) { o.Profile = "cpu" },
		"other GPU":       func(o *GufoOptions) { o.Profile = "rdna4" },
		"multi GPU":       func(o *GufoOptions) { o.RenderNodes = append(o.RenderNodes, "/dev/dri/renderD129") },
		"missing GPU":     func(o *GufoOptions) { o.RenderNodes = nil },
		"context":         func(o *GufoOptions) { o.Context = 262145 },
		"output":          func(o *GufoOptions) { o.OutputTokens = o.Context },
		"cache":           func(o *GufoOptions) { o.CacheRAMBytes = 33 << 30 },
		"reasoning":       func(o *GufoOptions) { o.Reasoning = "high" },
		"unmounted model": func(o *GufoOptions) { o.ModelFiles = nil },
		"foreign shard":   func(o *GufoOptions) { o.ModelFiles = append(o.ModelFiles, "/elsewhere/two.gguf") },
		"duplicate shard": func(o *GufoOptions) { o.ModelFiles = append(o.ModelFiles, o.Model) },
		"relative path":   func(o *GufoOptions) { o.Model = "test.gguf" },
		"IPv6":            func(o *GufoOptions) { o.Listen = "::1" },
		"port":            func(o *GufoOptions) { o.Port = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			options := gufoFixture()
			mutate(&options)
			if _, err := GufoCommand(options, ""); err == nil {
				t.Fatal("invalid options accepted")
			}
		})
	}
}

func TestGufoEntrypointPolicy(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "applications/gufo/entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, reasoning := range []string{"off", "medium", "xhigh", "high", "overflow", "other-gpu", "trailing"} {
		t.Run(reasoning, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, text string) string {
				p := filepath.Join(dir, name)
				if err := os.WriteFile(p, []byte(text), 0700); err != nil {
					t.Fatal(err)
				}
				return p
			}
			// Substitute the container-only mount prefix, never create host /content.
			script := write("entrypoint.sh", strings.ReplaceAll(string(source), "/content/models", dir))
			model := write("one.gguf", "fixture")
			gpu := "gfx1151"
			if reasoning == "other-gpu" {
				gpu = "gfx1201"
			}
			write("rocminfo", "#!/bin/sh\nprintf '  Name: "+gpu+"\\n'\n")
			write("mkdir", "#!/bin/sh\ntest \"$*\" = '-p /data/home'\n")
			write("gufo", "#!/bin/sh\nprintf '\\nARGV\\000'\nprintf '%s\\000' \"$@\"\n")
			mode := reasoning
			if slices.Contains([]string{"overflow", "other-gpu", "trailing"}, mode) {
				mode = "medium"
			}
			args := []string{script}
			if reasoning == "trailing" {
				args = append(args, "--download")
			}
			cmd := exec.Command("bash", args...)
			cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "PARACETAMOL_GUFO_MODEL=" + model, "PARACETAMOL_GUFO_MODEL_ID=fixture", "PARACETAMOL_GUFO_REASONING=" + mode}
			if reasoning == "overflow" {
				cmd.Env = append(cmd.Env, "PARACETAMOL_GUFO_CONTEXT=18446744073709813760")
			}
			output, err := cmd.CombinedOutput()
			valid := slices.Contains([]string{"off", "medium", "xhigh"}, reasoning)
			if !valid {
				if err == nil {
					t.Fatalf("accepted invalid policy: %s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
			if !bytes.Contains(output, []byte("Container bind 0.0.0.0:8080")) || bytes.Contains(output, []byte("Host publish")) {
				t.Fatalf("banner must not mistake the container port for an ephemeral host port: %s", output)
			}
			_, argv, ok := bytes.Cut(output, []byte("\nARGV\x00"))
			if !ok {
				t.Fatalf("no Gufo invocation: %s", output)
			}
			text := strings.ReplaceAll(string(argv), "\x00", " ")
			for _, want := range []string{"serve llm", "--context 262144", "--sessions 1", "--speculative off", "--preserve-thinking on"} {
				if !strings.Contains(text, want) {
					t.Errorf("missing %s in %s", want, text)
				}
			}
			if reasoning == "off" {
				if !strings.Contains(text, "--think off") || strings.Contains(text, "--reasoning-effort") {
					t.Fatal(text)
				}
			} else if !strings.Contains(text, "--reasoning-effort "+reasoning) {
				t.Fatal(text)
			}
			for _, forbidden := range []string{"--temp", "--top-p", "--cache-disk", "--mtp", "--download"} {
				if strings.Contains(text, forbidden) {
					t.Fatal(text)
				}
			}
		})
	}
}
