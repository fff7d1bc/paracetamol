package application

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestDFlashMultimodalPositionPatchPackaging(t *testing.T) {
	const relative = "applications/llama-cpp/dflash-mtmd-position-gap.patch"
	const digest = "c299f24df7c580ccbe013224dbba50c6adc64eaf513afff918c9f5d8cebce91d"
	patch, err := os.ReadFile("../../" + relative)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(patch)); got != digest {
		t.Fatalf("comment-qualified PR30220 patch digest = %s", got)
	}
	read := func(path string) string {
		t.Helper()
		value, err := os.ReadFile("../../" + path)
		if err != nil {
			t.Fatal(err)
		}
		return string(value)
	}
	if !strings.Contains(read(".containerignore"), "\n!"+relative+"\n") {
		t.Fatal("native patch is not in the build-context allowlist")
	}
	containerfile := read("Containerfile")
	start := strings.Index(containerfile, "COPY "+relative+" ")
	metrics := strings.Index(containerfile, "COPY applications/llama-cpp/server-speculative-metrics.patch ")
	metricsComplete := strings.Index(containerfile, "LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-server-speculative-metrics\n")
	end := strings.Index(containerfile, "\nFROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp")
	if metrics < 0 || metricsComplete <= metrics || start <= metricsComplete || end <= start {
		t.Fatal("position correction must follow the existing native/metrics build")
	}
	late := containerfile[start:end]
	for _, required := range []string{
		digest + "  /opt/paracetamol/llama-dflash-mtmd-position-gap.patch",
		"| sha256sum -c -", "GIT_INDEX_FILE=/tmp/paracetamol-dflash-position-gap.index",
		"git apply --check /opt/paracetamol/llama-dflash-mtmd-position-gap.patch",
		"git apply /opt/paracetamol/llama-dflash-mtmd-position-gap.patch",
		"--target llama-common llama-server llama-cli llama-bench",
		"cmake --install build --config Release", "/tmp/test-llama-native-policy", "/tmp/test-server-speculative-metrics",
	} {
		if !strings.Contains(late, required) {
			t.Errorf("missing late native packaging requirement %q", required)
		}
	}
	if strings.Index(late, "GIT_INDEX_FILE=") > strings.Index(late, "git ") {
		t.Fatal("private index must be active before the first Git command")
	}
	if !strings.Contains(containerfile, ",server-speculative-metrics,dflash-mtmd-position-gap\"") {
		t.Fatal("native patch provenance label omits or reorders the late correction")
	}
	spec, ok := ByID(string(LlamaCPP))
	if !ok || !strings.HasSuffix(spec.Image, "-740251a-r71") {
		t.Fatalf("new native patch must use its r71 image identity: %q", spec.Image)
	}
}
