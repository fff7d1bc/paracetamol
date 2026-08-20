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
