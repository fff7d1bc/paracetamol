package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"paracetamol/internal/project"
)

func TestLlamaEntrypointScopesQwen4expOptimizations(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "applications/llama-cpp/entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, arch, backend, kernel, graph, cache, want string }{
		{"halo defaults", "gfx1151", "rocm", "auto", "auto", "auto", "1/1/1"},
		{"halo cache opt-out", "gfx1151", "rocm", "auto", "auto", "0", "1/1/0"},
		{"halo graph opt-out also disables cache", "gfx1151", "rocm", "auto", "0", "1", "1/0/0"},
		{"kernel opt-out also disables graph and cache", "gfx1151", "rocm", "0", "1", "1", "0/0/0"},
		{"Vulkan cannot inherit opt-in", "gfx1151", "vulkan", "1", "1", "1", "0/0/0"},
		{"Strix Point cannot inherit opt-in", "gfx1150", "rocm", "1", "1", "1", "0/0/0"},
		{"gfx1200 cannot inherit opt-in", "gfx1200", "rocm", "1", "1", "1", "0/0/0"},
		{"gfx1201 cannot inherit opt-in", "gfx1201", "rocm", "1", "1", "1", "0/0/0"},
		{"CPU cannot inherit opt-in", "cpu", "rocm", "1", "1", "1", "0/0/0"},
		{"invalid graph setting", "gfx1151", "rocm", "auto", "yes", "auto", "error"},
		{"invalid cache setting", "gfx1151", "rocm", "auto", "auto", "yes", "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, contents string) string {
				t.Helper()
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(contents), 0700); err != nil {
					t.Fatal(err)
				}
				return path
			}
			model := write("fixture.gguf", "not a model")
			// Substitute only container filesystem boundaries. Device discovery and
			// final environment policy execute through the real entrypoint.
			script := string(source)
			replacements := map[string]string{
				"source /usr/local/share/paracetamol/llama-model-load-policy.sh": "source " + strconv.Quote(filepath.Join(root, "applications/llama-cpp/model-load-policy.sh")),
				"runtime_report=/tmp/paracetamol-llama-runtime":                  "runtime_report=" + strconv.Quote(filepath.Join(dir, "report")),
			}
			for from, to := range replacements {
				if strings.Count(script, from) != 1 {
					t.Fatalf("entrypoint boundary changed: %s", from)
				}
				script = strings.Replace(script, from, to, 1)
			}
			entrypoint := write("entrypoint", script)
			write("mkdir", "#!/bin/sh\ntest \"$*\" = '-p /data/cache /data/home'\n")
			write("rocminfo", "#!/bin/sh\nprintf '  Name: %s\\n' \"$TEST_ARCH\"\n")
			write("llama-cli", "#!/bin/sh\ntest \"$*\" = '--list-devices' || exit 1\nprintf 'ROCm0: fixture\\nVulkan0: fixture\\n'\n")
			write("llama-server", "#!/bin/sh\nprintf '\\nQSA=%s/%s/%s\\n' \"$PARACETAMOL_QWEN4EXP_QSA_KERNEL\" \"$PARACETAMOL_QWEN4EXP_QSA_GRAPH\" \"$PARACETAMOL_QWEN4EXP_QSA_CACHE\"\n")
			profile, gpus := "auto", "1"
			if tc.arch == "cpu" {
				profile, gpus = "cpu", "0"
			}
			cmd := exec.Command("bash", entrypoint)
			cmd.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin", "TEST_ARCH=" + tc.arch,
				"PARACETAMOL_PROFILE=" + profile, "PARACETAMOL_GPU_COUNT=" + gpus,
				"PARACETAMOL_LLAMA_BACKEND=" + tc.backend, "PARACETAMOL_LLAMA_MODEL=" + model,
				"PARACETAMOL_QWEN4EXP_QSA_KERNEL=" + tc.kernel, "PARACETAMOL_QWEN4EXP_QSA_GRAPH=" + tc.graph,
				"PARACETAMOL_QWEN4EXP_QSA_CACHE=" + tc.cache,
			}
			output, err := cmd.CombinedOutput()
			if tc.want == "error" {
				if err == nil || !strings.Contains(string(output), "invalid Qwen4exp QSA ") {
					t.Fatalf("err=%v output=%s", err, output)
				}
				return
			}
			if err != nil || !strings.Contains(string(output), "\nQSA="+tc.want+"\n") {
				t.Fatalf("err=%v output=%s", err, output)
			}
		})
	}
}
