package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/project"
)

// Exercise the actual entrypoint with only filesystem and executable boundaries
// substituted. No fixture executable loads a model or accesses a GPU.
func runLlamaPolicyEntrypoint(t *testing.T, backend, arch string, env []string, router func(string) string) (string, error) {
	t.Helper()
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
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
	source, err := os.ReadFile(filepath.Join(root, "applications/llama-cpp/entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(source)
	for from, to := range map[string]string{
		"source /usr/local/share/paracetamol/llama-model-load-policy.sh": "source " + strconv.Quote(filepath.Join(root, "applications/llama-cpp/model-load-policy.sh")),
		"runtime_report=/tmp/paracetamol-llama-runtime":                  "runtime_report=" + strconv.Quote(filepath.Join(dir, "report")),
		"router_preset=/tmp/paracetamol-models.ini":                      "router_preset=" + strconv.Quote(filepath.Join(dir, "rendered.ini")),
	} {
		if strings.Count(script, from) != 1 {
			t.Fatalf("entrypoint boundary changed: %s", from)
		}
		script = strings.Replace(script, from, to, 1)
	}
	if router != nil {
		ini := write("models.ini", router(model))
		script = strings.ReplaceAll(script, "/run/paracetamol/models.ini", ini)
		env = append(env, "PARACETAMOL_LLAMA_ROUTER=1")
	}
	entrypoint := write("entrypoint", script)
	write("mkdir", "#!/bin/sh\ntest \"$*\" = '-p /data/cache /data/home'\n")
	write("rocminfo", "#!/bin/sh\nprintf '  Name: %s\\n' \"$TEST_ARCH\"\n")
	write("llama-cli", "#!/bin/sh\nif test \"$*\" = '--list-devices'; then\n  printf 'ROCm0: fixture\\nVulkan0: fixture\\n'\nelse\n  printf 'ARG=<%s>\\n' \"$@\"\nfi\n")
	write("llama-server", "#!/bin/sh\nprintf 'ARG=<%s>\\n' \"$@\"\nif test -f \"$TEST_RENDERED_INI\"; then /bin/cat \"$TEST_RENDERED_INI\"; fi\n")
	profile, gpus := "auto", "1"
	if arch == "cpu" {
		profile, gpus = "cpu", "0"
	}
	cmd := exec.Command("bash", entrypoint, "--ctx-size", "262144")
	cmd.Env = append([]string{
		"PATH=" + dir + ":/usr/bin:/bin", "TEST_ARCH=" + arch,
		"TEST_RENDERED_INI=" + filepath.Join(dir, "rendered.ini"),
		"PARACETAMOL_PROFILE=" + profile, "PARACETAMOL_GPU_COUNT=" + gpus,
		"PARACETAMOL_LLAMA_BACKEND=" + backend, "PARACETAMOL_LLAMA_MODEL=" + model,
	}, env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestLlamaEntrypointDFlashProposalPolicyAndStreamGuard(t *testing.T) {
	for _, tc := range []struct {
		name, backend, arch, spec, depth, load, mode, failure string
		missingDraft, nonexistentDraft                        bool
	}{
		{name: "resident DFlash maximum depth remains greedy", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: "15"},
		{name: "resident DFlash depth twelve remains greedy", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: "12"},
		{name: "Vulkan resident DFlash remains greedy", backend: "vulkan", arch: "gfx1151", spec: "draft-dflash", depth: "4"},
		{name: "CPU resident DFlash remains greedy", backend: "rocm", arch: "cpu", spec: "draft-dflash", depth: "3"},
		{name: "MTP unchanged", backend: "rocm", arch: "gfx1151", spec: "draft-mtp", depth: "3"},
		{name: "no speculation unchanged", backend: "rocm", arch: "gfx1151", depth: "0"},
		{name: "stream DFlash server", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: "3", load: "stream-token-embedding"},
		{name: "stream DFlash CLI", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: "3", load: "stream-token-embedding", mode: "cli"},
		{name: "Vulkan stream no spec unchanged", backend: "vulkan", arch: "gfx1151", depth: "0", load: "stream-token-embedding"},
		{name: "stream MTP rejected", backend: "rocm", arch: "gfx1151", spec: "draft-mtp", depth: "3", load: "stream-token-embedding", failure: "speculation requires ROCm Strix Halo DFlash at depth 3"},
		{name: "stream Vulkan DFlash rejected", backend: "vulkan", arch: "gfx1151", spec: "draft-dflash", depth: "3", load: "stream-token-embedding", failure: "speculation requires ROCm Strix Halo DFlash at depth 3"},
		{name: "stream other depth rejected", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: "4", load: "stream-token-embedding", failure: "speculation requires ROCm Strix Halo DFlash at depth 3"},
		{name: "stream missing draft rejected", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: "3", load: "stream-token-embedding", missingDraft: true, failure: "DFlash requires a draft model"},
		{name: "stream nonexistent draft rejected", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: "3", load: "stream-token-embedding", nonexistentDraft: true, failure: "draft model is not a regular file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := []string{"PARACETAMOL_LLAMA_SPECULATIVE_TYPE=" + tc.spec, "PARACETAMOL_LLAMA_DRAFT_TOKENS=" + tc.depth, "PARACETAMOL_LLAMA_MODEL_LOAD_STRIX_HALO=" + tc.load}
			if tc.load == "stream-token-embedding" {
				env = append(env, "PARACETAMOL_LLAMA_KV_CACHE_STRIX_HALO=f16", "PARACETAMOL_LLAMA_FLASH_ATTN_STRIX_HALO=on")
			}
			if tc.spec == "draft-dflash" && !tc.missingDraft {
				draft := filepath.Join(t.TempDir(), "draft.gguf")
				if err := os.WriteFile(draft, []byte("not a draft"), 0600); err != nil {
					t.Fatal(err)
				}
				if tc.nonexistentDraft {
					draft += ".missing"
				}
				env = append(env, "PARACETAMOL_LLAMA_DRAFT_MODEL="+draft)
			}
			if tc.mode != "" {
				env = append(env, "PARACETAMOL_LLAMA_MODE="+tc.mode)
			}
			output, err := runLlamaPolicyEntrypoint(t, tc.backend, tc.arch, env, nil)
			if tc.failure != "" {
				if err == nil || !strings.Contains(output, tc.failure) || strings.Contains(output, "ARG=<") {
					t.Fatalf("err=%v output=%s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("err=%v output=%s", err, output)
			}
			if got := strings.Contains(output, "ARG=<--spec-draft-sampling>\nARG=<greedy>\n"); got != (tc.spec == "draft-dflash") {
				t.Fatalf("unexpected proposal sampler: %s", output)
			}
			if tc.spec != "" {
				if !strings.Contains(output, "ARG=<--spec-draft-n-max>\nARG=<"+tc.depth+">\n") {
					t.Fatalf("draft depth changed: %s", output)
				}
				device := "ROCm0"
				if tc.backend == "vulkan" {
					device = "Vulkan0"
				} else if tc.arch == "cpu" {
					device = "none"
				}
				if !strings.Contains(output, "ARG=<--device-draft>\nARG=<"+device+">\n") {
					t.Fatalf("wrong draft device: %s", output)
				}
			}
			for _, forbidden := range []string{"probabilistic", "ARG=<--temp>", "ARG=<--top-p>", "ARG=<--top-k>", "ARG=<--min-p>"} {
				if strings.Contains(output, forbidden) {
					t.Fatalf("unexpected target sampler: %s", output)
				}
			}
			if tc.load == "stream-token-embedding" && tc.backend == "rocm" {
				for _, tuple := range []string{"--ctx-size>\nARG=<262144", "--batch-size>\nARG=<2048", "--ubatch-size>\nARG=<2048", "--cache-ram>\nARG=<0", "--cache-type-k>\nARG=<f16", "--cache-type-v>\nARG=<f16"} {
					if !strings.Contains(output, "ARG=<"+tuple+">\n") {
						t.Fatalf("stream policy changed: %s", output)
					}
				}
			}
			if tc.mode != "cli" && !strings.Contains(output, "ARG=<--parallel>\nARG=<4>\nARG=<--kv-unified>\n") {
				t.Fatalf("shared request slots changed: %s", output)
			}
		})
	}
}

func TestLlamaEntrypointRouterStreamSpeculation(t *testing.T) {
	for _, tc := range []struct {
		name, backend, arch, spec, failure string
		depth                              int64
		missing, nonexistent, reversed     bool
		proposalSampling                   string
		lastSection                        bool
	}{
		{name: "DFlash accepted", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: 3},
		{name: "section order independent", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: 3, reversed: true},
		{name: "ROCm no spec unchanged", backend: "rocm", arch: "gfx1151"},
		{name: "Vulkan no spec unchanged", backend: "vulkan", arch: "gfx1151"},
		{name: "MTP rejected", backend: "rocm", arch: "gfx1151", spec: "draft-mtp", depth: 3, failure: "speculation requires"},
		{name: "Vulkan DFlash rejected", backend: "vulkan", arch: "gfx1151", spec: "draft-dflash", depth: 3, failure: "speculation requires"},
		{name: "wrong depth rejected", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: 4, failure: "speculation requires"},
		{name: "missing draft rejected", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: 3, missing: true, failure: "speculation requires"},
		{name: "nonexistent draft rejected", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: 3, nonexistent: true, failure: "draft model is not a regular file"},
		{name: "wrong profile rejected", backend: "rocm", arch: "gfx1150", spec: "draft-dflash", depth: 3, failure: "does not support profile"},
		{name: "probabilistic stream rejected", backend: "rocm", arch: "gfx1151", spec: "draft-dflash", depth: 3, proposalSampling: "probabilistic", failure: "speculation requires"},
		{name: "last section validated", backend: "rocm", arch: "gfx1151", spec: "draft-mtp", depth: 3, lastSection: true, failure: "speculation requires"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := runLlamaPolicyEntrypoint(t, tc.backend, tc.arch, nil, func(draft string) string {
				preset := catalog.LlamaPreset{ID: "flash", Artifact: "target", DefaultContext: 262144, SpeculativeType: tc.spec, DraftTokens: tc.depth,
					BackendProfiles: map[string][]string{"rocm": {"strix-halo"}},
					ModelLoad:       map[string]string{"strix-halo": catalog.LlamaModelLoadStreamTokenEmbedding},
					FlashAttention:  map[string]string{"strix-halo": "on"}, KVCache: map[string]string{"strix-halo": "f16"}}
				if tc.spec != "" && !tc.missing {
					preset.DraftArtifact = "draft"
				}
				managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{"target": {Destination: "target.gguf"}, "draft": {Destination: "draft.gguf"}}, LlamaPresets: map[string]catalog.LlamaPreset{"flash": preset}}
				// Render for the allowed hardware, then let the real container boundary
				// independently reject the wrong detected profile or unsafe tuple.
				ini, err := RenderRouterModels(managed, tc.backend, "strix-halo", []string{"flash"})
				if err != nil {
					t.Fatal(err)
				}
				if tc.nonexistent {
					draft += ".missing"
				}
				ini = strings.ReplaceAll(ini, "/content/models/draft.gguf", draft)
				if tc.proposalSampling != "" {
					ini = strings.Replace(ini, "spec-draft-sampling = greedy", "spec-draft-sampling = "+tc.proposalSampling, 1)
				}
				if tc.reversed {
					line := "paracetamol-model-load-strix-halo = stream-token-embedding\n"
					ini = strings.Replace(ini, line, "", 1) + line
				}
				// A later non-stream section must not inherit the first one's guard state.
				if tc.lastSection {
					return ini
				}
				return ini + "[resident-mtp]\nc = 4096\nspec-type = draft-mtp\nspec-draft-n-max = 3\n"
			})
			if tc.failure != "" {
				if err == nil || !strings.Contains(output, tc.failure) || strings.Contains(output, "ARG=<") {
					t.Fatalf("err=%v output=%s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("err=%v output=%s", err, output)
			}
			for _, required := range []string{"parallel = 4\nkv-unified = true\nc = 262144", "cache-type-k = f16", "cache-type-v = f16"} {
				if !strings.Contains(output, required) {
					t.Fatalf("missing router policy %q: %s", required, output)
				}
			}
			if tc.backend == "rocm" {
				for _, required := range []string{"batch-size = 2048", "ubatch-size = 2048", "cache-ram = 0", "lazy-mode = on-direct"} {
					if !strings.Contains(output, required) {
						t.Fatalf("stream router policy changed: %s", output)
					}
				}
			}
			if tc.spec != "" && (!strings.Contains(output, "spec-draft-sampling = greedy\n") || !strings.Contains(output, "device-draft = ROCm0\n")) {
				t.Fatalf("missing explicit greedy ROCm draft policy: %s", output)
			}
		})
	}
}

func TestDenseQwenPresetsKeepNativeF16ForDirectAndRouter(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog/catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"unsloth-qwen3.8-27b-ud-q8-k-xl", "unsloth-qwen3.8-27b-ud-q4-k-xl", "huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl"} {
		t.Run(id, func(t *testing.T) {
			preset, ok := managed.LlamaPresets[id]
			if !ok || preset.SpeculativeType != "" || preset.DraftArtifact != "" || preset.DraftTokens != 0 {
				t.Fatal("missing plain dense preset or unexpected speculation")
			}
			env := []string{"PARACETAMOL_LLAMA_KV_CACHE_STRIX_HALO=" + preset.KVCache["strix-halo"]}
			output, err := runLlamaPolicyEntrypoint(t, "rocm", "gfx1151", env, nil)
			if err != nil {
				t.Fatalf("direct entrypoint: %v\n%s", err, output)
			}
			for _, axis := range []string{"k", "v"} {
				if !strings.Contains(output, "ARG=<--cache-type-"+axis+">\nARG=<f16>\n") {
					t.Fatalf("dense direct native cache precision changed: %s", output)
				}
			}
			if strings.Contains(output, "ARG=<--spec-") || strings.Contains(output, "ARG=<--model-draft>") {
				t.Fatalf("plain dense direct enabled speculation: %s", output)
			}
			output, err = runLlamaPolicyEntrypoint(t, "rocm", "gfx1151", nil, func(string) string {
				ini, err := RenderRouterModels(managed, "rocm", "strix-halo", []string{id})
				if err != nil {
					t.Fatal(err)
				}
				return ini
			})
			if err != nil || !strings.Contains(output, "cache-type-k = f16\ncache-type-v = f16\n") {
				t.Fatalf("dense router native cache precision changed: %v\n%s", err, output)
			}
			if strings.Contains(output, "\nspec-") || strings.Contains(output, "\nmodel-draft = ") {
				t.Fatalf("plain dense router enabled speculation: %s", output)
			}
		})
	}
}

func TestLlamaEntrypointScopesQwen4expOptimizations(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "applications/llama-cpp/entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, arch, backend, kernel, graph, want string }{
		{"halo defaults", "gfx1151", "rocm", "auto", "auto", "1/1"},
		{"halo graph opt-out", "gfx1151", "rocm", "auto", "0", "1/0"},
		{"kernel opt-out also disables graph", "gfx1151", "rocm", "0", "1", "0/0"},
		{"Vulkan cannot inherit opt-in", "gfx1151", "vulkan", "1", "1", "0/0"},
		{"Strix Point cannot inherit opt-in", "gfx1150", "rocm", "1", "1", "0/0"},
		{"gfx1200 cannot inherit opt-in", "gfx1200", "rocm", "1", "1", "0/0"},
		{"gfx1201 cannot inherit opt-in", "gfx1201", "rocm", "1", "1", "0/0"},
		{"CPU cannot inherit opt-in", "cpu", "rocm", "1", "1", "0/0"},
		{"invalid kernel setting", "gfx1151", "rocm", "yes", "auto", "error"},
		{"invalid graph setting", "gfx1151", "rocm", "auto", "yes", "error"},
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
			write("llama-server", "#!/bin/sh\nprintf '\\nQSA=%s/%s\\nVULKAN_KV_CONTIG=%s\\n' \"$PARACETAMOL_QWEN4EXP_QSA_KERNEL\" \"$PARACETAMOL_QWEN4EXP_QSA_GRAPH\" \"${GGML_VK_FA_KV_CONTIG-unset}\"\nprintf 'ARGS=%s\\n' \"$*\"\n")
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
			if !strings.Contains(string(output), "--parallel 4 --kv-unified\n") {
				t.Fatalf("missing default request slots: %s", output)
			}
			// Vulkan layout selection belongs to upstream now. Neither the
			// environment nor the status snapshot should claim a local override.
			if !strings.Contains(string(output), "\nVULKAN_KV_CONTIG=unset\n") {
				t.Fatalf("obsolete Vulkan override: %s", output)
			}
			report, err := os.ReadFile(filepath.Join(dir, "report"))
			if err != nil || !strings.HasPrefix(string(report), "schema=3\n") ||
				strings.Contains(string(report), "vulkan_f16_kv_contiguize") {
				t.Fatalf("err=%v report=%s", err, report)
			}
		})
	}
}
