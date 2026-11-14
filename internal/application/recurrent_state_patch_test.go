package application

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestRecurrentStateClearPackaging(t *testing.T) {
	const relative = "applications/llama-cpp/recurrent-state-clear.patch"
	const digest = "0230801b15720da628329b02693715d658a69be45fc4d03df398bed405283a61"
	patch := nativeSource(t, relative)
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(patch))); got != digest {
		t.Fatalf("shared PR31913 subset digest = %s", got)
	}
	if strings.Count(patch, "diff --git ") != 1 ||
		!strings.Contains(patch, "diff --git a/src/llama-graph.cpp b/src/llama-graph.cpp") ||
		strings.Contains(patch, "deepseek") || strings.Contains(patch, "tests/") {
		t.Fatal("reset correction must remain the exact shared-helper hunk")
	}
	containerfile := nativeSource(t, "Containerfile")
	allowlist := nativeSource(t, ".containerignore")
	for _, name := range []string{
		"recurrent-state-clear.patch", "test-recurrent-state-clear.cpp", "build-recurrent-state-clear-test.sh",
	} {
		path := "applications/llama-cpp/" + name
		if !strings.Contains(allowlist, "\n!"+path+"\n") || !strings.Contains(containerfile, path) {
			t.Errorf("missing reset build input %s", path)
		}
	}
	start := strings.Index(containerfile, "COPY applications/llama-cpp/kpool-prefix-retention.patch ")
	end := strings.Index(containerfile, "\nFROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp")
	if start < 0 || end <= start {
		t.Fatal("missing late host-only update boundary")
	}
	late := containerfile[start:end]
	baseline := strings.Index(late, "baseline /opt/llama.cpp /opt/llama-install/lib /tmp/recurrent-clear-baseline")
	apply := strings.Index(late, "git apply /opt/paracetamol/llama-recurrent-state-clear.patch")
	install := strings.Index(late, "cmake --install build --config Release")
	candidate := strings.Index(late, "candidate /opt/llama.cpp /opt/llama-install/lib /tmp/recurrent-clear-candidate")
	if baseline < 0 || apply <= baseline || install <= apply || candidate <= install {
		t.Fatal("actual old helper must run before patching, and candidate after rebuilt libraries are installed")
	}
	prefixApply := strings.Index(late, "git apply /opt/paracetamol/llama-kpool-prefix-retention.patch")
	rejected := strings.Index(late, "grep -Fx 'FAIL actual production build_rs clear operation'")
	if prefixApply <= baseline || rejected < 0 || prefixApply <= rejected {
		t.Fatal("old-header fixture must finish before the prefix patch changes its private class ABI")
	}
	for _, required := range []string{
		digest + "  /opt/paracetamol/llama-recurrent-state-clear.patch",
		"git apply --check /opt/paracetamol/llama-recurrent-state-clear.patch",
		"20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c",
		"--baseline-characterization", "FAIL actual production build_rs clear operation",
		"Unchanged reset incorrectly passed the candidate contract",
		"graphs=6 poisoned-clears=2 empty-clears=2 extra-copy-rows=2 attention=unchanged mode=baseline-characterization",
		"graphs=6 poisoned-clears=2 empty-clears=2 extra-copy-rows=2 attention=unchanged mode=candidate",
		"sha256sum --check /tmp/host-pool-backends.sha256",
		"sha256sum --check /tmp/host-pool-installed-backends.sha256",
	} {
		if !strings.Contains(late, required) {
			t.Errorf("missing reset qualification/build guard %q", required)
		}
	}
	// Directly name the complete configured backend set. An empty find/xargs
	// pipeline can otherwise hash stdin and look like a successful manifest.
	for _, library := range []string{"libggml.so", "libggml-base.so", "libggml-cpu.so", "libggml-hip.so", "libggml-vulkan.so"} {
		for _, prefix := range []string{"build/bin/", "/opt/llama-install/lib/"} {
			if !strings.Contains(late, prefix+library) {
				t.Errorf("backend preservation must require %s%s", prefix, library)
			}
		}
	}
	if strings.Contains(late, "xargs") {
		t.Fatal("backend closure must not accept an empty discovery pipeline")
	}
	if !strings.Contains(containerfile, ",recurrent-state-clear\"") {
		t.Fatal("shared reset provenance missing from final patch label")
	}
	fixture := nativeSource(t, "applications/llama-cpp/test-recurrent-state-clear.cpp")
	for _, required := range []string{
		"ggml_backend_cpu_init()", "graph.build_inp_mem_hybrid()", "graph.build_rs(",
		"result.set_inputs(&ubatch)", "input->get_recr()->s_copy", "protected_before",
		"attention/indexer payload changed", "baseline persistent poison", "test.graphs == 6",
	} {
		if !strings.Contains(fixture, required) {
			t.Errorf("missing actual-helper or independent state oracle %q", required)
		}
	}
	for _, forbidden := range []string{"ggml_backend_load", "ggml_backend_init_best", "ggml_fill_inplace(", "ggml_scale_inplace("} {
		if strings.Contains(fixture, forbidden) {
			t.Errorf("fixture must not discover a GPU or replace the helper with %q", forbidden)
		}
	}
	helper := nativeSource(t, "applications/llama-cpp/build-recurrent-state-clear-test.sh")
	for _, required := range []string{
		"ded522f8304554317e7ff8f88698dbbbca5425aa",
		"c3d34e39816a7c13f389ad1f0e233b99bd9e60bc70f261c61645cf36f892fcbc",
		"9c684fd9069a8b52a29fd0c3a8a48b52fe35074c6cc09d78954f419191cc3c6c",
		"GIT_INDEX_FILE", "sha256sum --check --strict -",
	} {
		if !strings.Contains(helper, required) {
			t.Errorf("missing reset source-coupling guard %q", required)
		}
	}
	if strings.Contains(containerfile[end:], "test-recurrent-state-clear") {
		t.Fatal("native regression executables must not be copied into the production runtime")
	}
}
