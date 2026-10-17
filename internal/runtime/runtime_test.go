package runtime

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
)

func TestWebCommandConfinesCPUApplication(t *testing.T) {
	command := WebCommand(WebOptions{Image: "image", Profile: "cpu", Listen: "127.0.0.1", Port: 8188, DataDir: "/data", Application: "comfyui", MemoryPolicy: "balanced", KernelPolicy: "default", Publish: true}, ":rw")
	joined := strings.Join(command, " ")
	for _, required := range []string{"--read-only", "--cap-drop all", "no-new-privileges", "127.0.0.1:8188:8188/tcp", "/data/content/comfyui/models:/content/models:ro"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("command lacks %q: %s", required, joined)
		}
	}
	if strings.Contains(joined, "/dev/kfd") {
		t.Fatalf("CPU command exposes GPU: %s", joined)
	}
}

func TestWildcardPublicationConstrainsPastaToIPv4(t *testing.T) {
	got := publicationNetwork("0.0.0.0")
	if strings.Join(got, " ") != "--network pasta:-4" {
		t.Fatalf("arguments = %v", got)
	}
}

func TestLlamaBenchmarkCommandIsOfflineAndConstrained(t *testing.T) {
	command := LlamaBenchmarkCommand(LlamaBenchmarkOptions{Image: "image", Profile: "strix-halo", Backend: "rocm", DataDir: "/data", ManagedModel: "model.gguf", RenderNodes: []string{"/dev/dri/renderD128"}, Repetitions: 2, PromptTokens: 32, GenerationTokens: 16, BatchSize: 128, UBatchSize: 64, CacheTypeK: "f16", CacheTypeV: "f16", FlashAttention: "auto"}, ":rw")
	joined := strings.Join(command, " ")
	for _, required := range []string{"--network none", "--read-only", "--cap-drop all", "/dev/kfd", "/dev/dri/renderD128", "--output json"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("benchmark command lacks %q: %s", required, joined)
		}
	}
}

func TestLlamaServerExposesOnlySelectedGPUAndOnePort(t *testing.T) {
	command, err := LlamaCommand(LlamaOptions{
		Image: "image", Profile: "strix-halo", Mode: "server", Backend: "rocm",
		DataDir: "/data", ManagedModel: "model.gguf", RenderNodes: []string{"/dev/dri/renderD129"},
		Listen: "127.0.0.1", Port: 8080, AutoRemove: true,
		ProfileModelLoad: map[string]string{"strix-halo": catalog.LlamaModelLoadMMapLazyTokenEmbedding},
	}, ":rw")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	for _, required := range []string{"--read-only", "--cap-drop all", "no-new-privileges", "--device /dev/kfd", "--device /dev/dri/renderD129", "--publish 127.0.0.1:8080:8080/tcp", "/data/content/llama-cpp/models:/content/models:ro", "PARACETAMOL_LLAMA_MODEL_LOAD_STRIX_HALO=mmap-lazy-token-embedding"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("server command lacks %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{"/dev/dri/renderD128", "--privileged", "--network host"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("server command contains %q: %s", forbidden, joined)
		}
	}
}

func TestGatewayBackendUsesDynamicLoopbackPortAndOwnedRole(t *testing.T) {
	command, err := LlamaCommand(LlamaOptions{
		Image: "llama-image", Profile: "strix-halo", Mode: "server", Backend: "rocm",
		DataDir: "/data", RouterPreset: "/data/models.ini", ModelsMax: 2,
		RenderNodes: []string{"/dev/dri/renderD129"}, Listen: "127.0.0.1", Port: 8080,
		ContainerName: "gateway-llama", ContainerRole: "gateway-backend", AutoRemove: true,
		Detach: true, DynamicHostPort: true,
	}, ":rw")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	for _, required := range []string{"--publish 127.0.0.1::8080/tcp", "gateway-backend", "--detach"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("command lacks %q: %s", required, joined)
		}
	}
}

func TestRenderRouterModelsUsesOnlyFrozenSelection(t *testing.T) {
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{
			"one": {ID: "one", Destination: "one.gguf"},
			"two": {ID: "two", Destination: "two.gguf"},
		},
		LlamaPresets: map[string]catalog.LlamaPreset{
			"one": {ID: "one", Artifact: "one", DefaultContext: 4096},
			"two": {ID: "two", Artifact: "two", DefaultContext: 8192, ModelLoad: map[string]string{"strix-halo": catalog.LlamaModelLoadMMapLazyTokenEmbedding}},
		},
	}
	contents, err := RenderRouterModels(managed, "rocm", "strix-halo", []string{"two"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(contents, "[one]") || !strings.Contains(contents, "[two]") {
		t.Fatalf("unexpected frozen router contents:\n%s", contents)
	}
	for _, required := range []string{"paracetamol-model-load-strix-halo = mmap-lazy-token-embedding", "paracetamol-model-load-strix-point = resident"} {
		if !strings.Contains(contents, required) {
			t.Fatalf("router contents lack %q:\n%s", required, contents)
		}
	}
	if _, err := RenderRouterModels(managed, "rocm", "strix-halo", []string{"missing"}); err == nil {
		t.Fatal("unknown selected preset was accepted")
	}
	managed.LlamaPresets["two"] = catalog.LlamaPreset{ID: "two", Artifact: "two", DefaultContext: 8192, Backends: []string{"vulkan"}}
	if _, err := RenderRouterModels(managed, "rocm", "strix-halo", []string{"two"}); err == nil || !strings.Contains(err.Error(), "does not support backend rocm") {
		t.Fatalf("backend-incompatible router preset err=%v", err)
	}
	preset := managed.LlamaPresets["two"]
	preset.Backends = []string{"rocm", "vulkan"}
	preset.BackendProfiles = map[string][]string{"rocm": {"strix-halo"}}
	preset.ModelLoad = map[string]string{"strix-halo": catalog.LlamaModelLoadStreamTokenEmbedding}
	managed.LlamaPresets["two"] = preset
	if _, err := RenderRouterModels(managed, "rocm", "auto", []string{"two"}); err == nil {
		t.Fatal("unknown hardware acquired restricted model")
	}
	contents, err = RenderRouterModels(managed, "rocm", "strix-halo", []string{"one", "two"})
	if err != nil || !strings.Contains(contents, "paracetamol-allowed-profiles = strix-halo") || !strings.Contains(contents, "paracetamol-model-load-strix-halo = stream-token-embedding") {
		t.Fatalf("missing per-model policy: %s, %v", contents, err)
	}
	ordinary := strings.Split(strings.Split(contents, "[one]")[1], "[two]")[0]
	if strings.Contains(ordinary, "stream-token-embedding") || strings.Contains(ordinary, "allowed-profiles") || !strings.Contains(ordinary, "paracetamol-model-load-strix-halo = resident") {
		t.Fatalf("exceptional policy leaked into ordinary model: %s", ordinary)
	}
}

func TestLlamaRequestSlots(t *testing.T) {
	for _, slots := range []int{0, 1, 4, 64, -1, 65} {
		options := LlamaOptions{Image: "image", Profile: "cpu", Mode: "server", DataDir: "/data", Backend: "rocm", ManagedModel: "model.gguf", Listen: "127.0.0.1", Port: 8080, Slots: slots}
		command, err := LlamaCommand(options, "")
		if slots < 0 || slots > 64 {
			if err == nil {
				t.Fatalf("accepted slots=%d", slots)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := slots
		if want == 0 {
			want = 4
		}
		if !strings.Contains(strings.Join(command, " "), fmt.Sprintf("PARACETAMOL_LLAMA_SLOTS=%d", want)) {
			t.Fatalf("slots=%d command=%v", slots, command)
		}
	}
}

func TestLlamaRuntimeDoesNotOwnGatewaySamplingDefaults(t *testing.T) {
	managed := catalog.Catalog{
		Artifacts:        map[string]catalog.Artifact{"model": {Destination: "model.gguf"}},
		LlamaPresets:     map[string]catalog.LlamaPreset{"qwen": {ID: "qwen", Artifact: "model", DefaultContext: 4096, SamplingPolicy: "policy"}},
		SamplingPolicies: map[string]catalog.SamplingPolicy{"policy": {Thinking: map[string]any{"temperature": 1}, NonThinking: map[string]any{"temperature": .7}}},
	}
	contents, err := RenderRouterModels(managed, "rocm", "cpu", []string{"qwen"})
	if err != nil {
		t.Fatal(err)
	}
	command, err := LlamaCommand(LlamaOptions{Image: "image", Profile: "cpu", Mode: "server", DataDir: "/data", Backend: "rocm", ManagedModel: "model.gguf", Listen: "127.0.0.1", Port: 8080}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{contents, strings.Join(command, " ")} {
		if strings.Contains(strings.ToLower(value), "sampling") || strings.Contains(value, "temperature") {
			t.Fatalf("engine retained a gateway sampling option: %s", value)
		}
	}
}

func TestLlamaCPUCLIIsOfflineAndHasNoGPUDevices(t *testing.T) {
	prompt := "hello"
	command, err := LlamaCommand(LlamaOptions{
		Image: "image", Profile: "cpu", Mode: "cli", Backend: "rocm",
		DataDir: "/data", ManagedModel: "model.gguf", Prompt: &prompt, AutoRemove: true,
	}, ":rw")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	if !strings.Contains(joined, "--network none") || !strings.Contains(joined, "--single-turn") {
		t.Fatalf("CLI command is not an offline single turn: %s", joined)
	}
	for _, forbidden := range []string{"--publish", "/dev/kfd", "/dev/dri/render"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("CPU CLI command contains %q: %s", forbidden, joined)
		}
	}
}

func TestReasoningHistoryPolicyIsExplicitForDirectAndRouter(t *testing.T) {
	for _, preserve := range []bool{false, true} {
		t.Run(fmt.Sprint(preserve), func(t *testing.T) {
			managed := catalog.Catalog{
				Artifacts: map[string]catalog.Artifact{
					"model": {ID: "model", Destination: "model.gguf"},
				},
				LlamaPresets: map[string]catalog.LlamaPreset{
					"model": {ID: "model", Artifact: "model", DefaultContext: 4096, ReasoningPreserve: preserve},
				},
			}
			contents, err := RenderRouterModels(managed, "rocm", "strix-halo", []string{"model"})
			if err != nil {
				t.Fatal(err)
			}
			if want := fmt.Sprintf("reasoning-preserve = %t\n", preserve); !strings.Contains(contents, want) {
				t.Fatalf("router policy lacks %q:\n%s", want, contents)
			}
			command, err := LlamaCommand(LlamaOptions{
				Image: "image", Profile: "cpu", Mode: "cli", Backend: "rocm",
				DataDir: "/data", ManagedModel: "model.gguf", ReasoningPreserve: preserve,
			}, ":rw")
			if err != nil {
				t.Fatal(err)
			}
			if want := "PARACETAMOL_LLAMA_REASONING_PRESERVE=" + boolInt(preserve); !slices.Contains(command, want) {
				t.Fatalf("direct policy lacks %q: %v", want, command)
			}
		})
	}
}

func TestReadOnlySharedMountUsesSharedSELinuxCategory(t *testing.T) {
	if got := readOnlySharedSuffix(":rw,Z"); got != ":ro,z" {
		t.Fatalf("suffix=%q", got)
	}
}
