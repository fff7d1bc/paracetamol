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

func TestGatewayBackendsUseDynamicLoopbackPortsAndOwnedRole(t *testing.T) {
	llama, err := LlamaCommand(LlamaOptions{
		Image: "llama-image", Profile: "strix-halo", Mode: "server", Backend: "rocm",
		DataDir: "/data", RouterPreset: "/data/models.ini", ModelsMax: 2,
		RenderNodes: []string{"/dev/dri/renderD129"}, Listen: "127.0.0.1", Port: 8080,
		ContainerName: "gateway-llama", ContainerRole: "gateway-backend", AutoRemove: true,
		Detach: true, DynamicHostPort: true,
	}, ":rw")
	if err != nil {
		t.Fatal(err)
	}
	dwarf, err := DwarfStarCommand(DwarfStarOptions{
		Image: "dwarf-image", Mode: "server", DataDir: "/data", Model: "/models/model.gguf",
		RenderNodes: []string{"/dev/dri/renderD129"}, Profile: "strix-halo",
		Listen: "127.0.0.1", Port: 8000, Context: 131072, OutputTokens: 16000,
		ContainerName: "gateway-dwarf", ContainerRole: "gateway-backend", Detach: true,
		DynamicHostPort: true,
	}, ":rw")
	if err != nil {
		t.Fatal(err)
	}
	for name, command := range map[string][]string{"llama.cpp": llama, "DwarfStar": dwarf} {
		joined := strings.Join(command, " ")
		for _, required := range []string{"--publish 127.0.0.1::", "gateway-backend", "--detach"} {
			if !strings.Contains(joined, required) {
				t.Fatalf("%s command lacks %q: %s", name, required, joined)
			}
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
	contents, err := RenderRouterModels(managed, "rocm", []string{"two"})
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
	if _, err := RenderRouterModels(managed, "rocm", []string{"missing"}); err == nil {
		t.Fatal("unknown selected preset was accepted")
	}
	managed.LlamaPresets["two"] = catalog.LlamaPreset{ID: "two", Artifact: "two", DefaultContext: 8192, Backends: []string{"vulkan"}}
	if _, err := RenderRouterModels(managed, "rocm", []string{"two"}); err == nil || !strings.Contains(err.Error(), "does not support backend rocm") {
		t.Fatalf("backend-incompatible router preset err=%v", err)
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
			contents, err := RenderRouterModels(managed, "rocm", []string{"model"})
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

func TestDwarfStarCLIAppliesPrivateServerDefaultsWithoutPublishing(t *testing.T) {
	prompt := "test"
	command, err := DwarfStarCommand(DwarfStarOptions{
		Image:        "image",
		Mode:         "cli",
		DataDir:      "/data",
		Model:        "/models/model.gguf",
		RenderNodes:  []string{"/dev/dri/renderD128"},
		Profile:      "strix-halo",
		Context:      4096,
		OutputTokens: 64,
		Prompt:       &prompt,
	}, ":rw")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	for _, expected := range []string{"--network none", "PARACETAMOL_HOST_LISTEN=127.0.0.1", "PARACETAMOL_PORT=8000"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("command lacks %q: %s", expected, joined)
		}
	}
	if strings.Contains(joined, "--publish") {
		t.Fatalf("CLI command publishes a port: %s", joined)
	}
}
