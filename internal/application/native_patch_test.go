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
	if !ok || !strings.HasSuffix(spec.Image, "-ded522f-r77") {
		t.Fatalf("native patch packaging must use its current image identity: %q", spec.Image)
	}
}

func TestHIPMMQCompatibilityPackaging(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		value, err := os.ReadFile("../../" + path)
		if err != nil {
			t.Fatal(err)
		}
		return string(value)
	}
	containerfile := read("Containerfile")
	allowlist := read(".containerignore")
	for _, name := range []string{
		"hip-mmq-activation-sum-compat.patch",
		"test-mmq-activation-sum.cpp",
		"build-mmq-activation-sum-test.sh",
	} {
		path := "applications/llama-cpp/" + name
		if !strings.Contains(allowlist, "\n!"+path+"\n") || !strings.Contains(containerfile, path) {
			t.Errorf("missing build-context input %s", path)
		}
	}
	for _, required := range []string{
		"git apply --check /opt/paracetamol/llama-hip-mmq-activation-sum-compat.patch",
		"git apply /opt/paracetamol/llama-hip-mmq-activation-sum-compat.patch",
		"bash /opt/paracetamol/build-mmq-activation-sum-test.sh",
		",hip-mmq-activation-sum-compat,",
	} {
		if !strings.Contains(containerfile, required) {
			t.Errorf("missing HIP compatibility packaging %q", required)
		}
	}
	if strings.Contains(containerfile, "quantized-kv-state-copy") || strings.Contains(allowlist, "quantized-kv-state-copy") {
		t.Fatal("upstream-owned state-copy correction must not remain in the local build")
	}
	if _, err := os.Stat("../../applications/llama-cpp/quantized-kv-state-copy.patch"); !os.IsNotExist(err) {
		t.Fatalf("obsolete state-copy patch remains or cannot be checked: %v", err)
	}
}

func TestConvRollbackFixturePackaging(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		value, err := os.ReadFile("../../" + path)
		if err != nil {
			t.Fatal(err)
		}
		return string(value)
	}
	containerfile := read("Containerfile")
	allowlist := read(".containerignore")
	for _, name := range []string{"test-qwen4exp-conv-rollback.cpp", "build-conv-rollback-test.sh"} {
		path := "applications/llama-cpp/" + name
		if !strings.Contains(allowlist, "\n!"+path+"\n") || !strings.Contains(containerfile, path) {
			t.Errorf("missing rollback fixture build input %s", path)
		}
	}
	for _, required := range []string{
		"bash /opt/paracetamol/build-conv-rollback-test.sh",
		"/opt/llama.cpp /opt/llama-install/lib /tmp/conv-rollback-test",
		"/tmp/conv-rollback-test/test-qwen4exp-conv-rollback --cpu",
	} {
		if !strings.Contains(containerfile, required) {
			t.Errorf("missing CPU-only rollback build gate %q", required)
		}
	}
}
