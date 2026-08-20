package runtime

import (
	"strings"
	"testing"
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
	for _, expected := range []string{"--network none", "ROCMLETE_HOST_LISTEN=127.0.0.1", "ROCMLETE_PORT=8000"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("command lacks %q: %s", expected, joined)
		}
	}
	if strings.Contains(joined, "--publish") {
		t.Fatalf("CLI command publishes a port: %s", joined)
	}
}
