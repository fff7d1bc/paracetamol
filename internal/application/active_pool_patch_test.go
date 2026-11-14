package application

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestActivePoolPatchFrozenScope(t *testing.T) {
	patch := nativeSource(t, "applications/llama-cpp/qwen4exp-active-pools.patch")
	const digest = "0e0819b2cfc8e65159b9a256342a3bb3df8057001aeeb845aeaaa2277ab39b56"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(patch))); got != digest {
		t.Fatalf("frozen PR32247 first-commit digest = %s", got)
	}
	if !strings.HasPrefix(patch, "From 4161abc7209db6012fe2ab4badcaa49fa3e481ab ") ||
		strings.Count(patch, "diff --git ") != 1 ||
		!strings.Contains(patch, "diff --git a/src/llama-memory-hybrid-idx.cpp b/src/llama-memory-hybrid-idx.cpp") {
		t.Fatal("active-pool change must remain the exact single owning-file commit")
	}
	for _, forbidden := range []string{"TOP_K_ROWS", "TOPK_ROWS", "top_k_rows", "row_list", "row-list", "ggml-cuda/", "ggml-vulkan/"} {
		if strings.Contains(patch, forbidden) {
			t.Errorf("active-pool subset must not import attention/kernel extension %q", forbidden)
		}
	}
}

// These are packaging and ABI-order checks, not evidence that the native
// oracle or a GPU workload executed. The actual image build runs those gates.
func TestActivePoolLateInstalledABIContract(t *testing.T) {
	containerfile := nativeSource(t, "Containerfile")
	allowlist := nativeSource(t, ".containerignore")
	for _, name := range []string{"qwen4exp-active-pools.patch", "test-qwen4exp-active-pool.cpp", "build-active-pool-test.sh"} {
		path := "applications/llama-cpp/" + name
		if strings.Count(allowlist, "\n!"+path+"\n") != 1 || strings.Count(containerfile, path) != 1 {
			t.Errorf("active-pool input needs one explicit COPY and allowlist entry: %s", path)
		}
	}
	start := strings.Index(containerfile, "COPY applications/llama-cpp/kpool-prefix-retention.patch ")
	end := strings.Index(containerfile, "\nFROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp")
	if start < 0 || end <= start {
		t.Fatal("missing late host-only build boundary")
	}
	late := containerfile[start:end]
	for _, required := range []string{
		"0e0819b2cfc8e65159b9a256342a3bb3df8057001aeeb845aeaaa2277ab39b56  /opt/paracetamol/llama-qwen4exp-active-pools.patch",
		"git add --update -- .", "git add -- common include src ggml tools tests",
		"--target llama-common llama-server llama-cli llama-bench",
		"ulimit -c 0", "GGML_NO_BACKTRACE=1 LD_LIBRARY_PATH=/opt/llama-install/lib",
		"for guard_case in 1 2 3 4; do",
		`grep -Fx "PASS production assertion guard=$guard_case"`,
		`test "$(grep -c '^PASS production assertion guard=' /tmp/active-pool-candidate/guards.log)" = 4`,
	} {
		if !strings.Contains(late, required) {
			t.Errorf("missing active-pool build qualification %q", required)
		}
	}
	// Prefix retention changes the private context/header ABI. The unchanged
	// baseline binary and its named negative control must finish first; the
	// candidate must use headers and installed libraries from the joint rebuild.
	cursor := 0
	for _, stage := range []string{
		"GIT_INDEX_FILE=/tmp/paracetamol-host-pool.index",
		"git read-tree HEAD",
		`test "$(git write-tree)" = aeeb3f45e00deae964a7949da69f1389fa98176a`,
		"ulimit -c 0",
		"/opt/llama.cpp /opt/llama-install/lib /tmp/active-pool-baseline",
		"/tmp/active-pool-baseline/test-qwen4exp-active-pool --baseline",
		"grep -Fx 'PASS actual production active-kpool API oracle cases=56 mode=baseline'",
		"/tmp/active-pool-baseline/test-qwen4exp-active-pool \\",
		"Unchanged pool scoring incorrectly passed the candidate contract",
		"grep -Fx 'FAIL active pool graph shape idle-second'",
		"git apply /opt/paracetamol/llama-kpool-prefix-retention.patch",
		`test "$(git write-tree)" = 0e4534d7f989c402dafad53e09d816f32a9264af`,
		"git apply --check /opt/paracetamol/llama-qwen4exp-active-pools.patch",
		"git apply /opt/paracetamol/llama-qwen4exp-active-pools.patch",
		`test "$(git write-tree)" = c2acb69ac63536cb67d14d3e14e1ca99bbb69adc`,
		"git apply /opt/paracetamol/llama-recurrent-state-clear.patch",
		`test "$(git write-tree)" = 20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c`,
		"cmake --build build --config Release",
		"sha256sum --check /tmp/host-pool-backends.sha256",
		"cmake --install build --config Release",
		"sha256sum --check /tmp/host-pool-installed-backends.sha256",
		"/opt/llama.cpp /opt/llama-install/lib /tmp/active-pool-candidate",
		"/tmp/active-pool-candidate/test-qwen4exp-active-pool \\",
		"grep -Fx 'PASS actual production active-kpool API oracle cases=56 mode=candidate'",
		"GGML_NO_BACKTRACE=1 LD_LIBRARY_PATH=/opt/llama-install/lib",
		"/tmp/active-pool-candidate/test-qwen4exp-active-pool --guards",
		"for guard_case in 1 2 3 4; do",
		"LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-llama-native-policy",
		"LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-server-speculative-metrics",
		"python -m pip check",
	} {
		next := strings.Index(late[cursor:], stage)
		if next < 0 {
			t.Fatalf("late ABI/qualification order missing stage %q", stage)
		}
		cursor += next + len(stage)
	}
	if !strings.Contains(containerfile, ",kpool-prefix-retention,qwen4exp-active-pools,recurrent-state-clear\"") {
		t.Fatal("patch provenance must preserve prefix, active-pool and shared-clear order")
	}
	if strings.Contains(containerfile[end:], "test-qwen4exp-active-pool") {
		t.Fatal("CPU regression executables must not enter the final runtime")
	}
}

func TestActivePoolFixtureUsesProductionCPUAPI(t *testing.T) {
	fixture := nativeSource(t, "applications/llama-cpp/test-qwen4exp-active-pool.cpp")
	const fixtureDigest = "306f4f9e5f97430389d726ececa75201ac5250857c332ddcbc0e5ebfdc3e7c1c"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(fixture))); got != fixtureDigest {
		t.Fatalf("reviewed actual-API fixture digest = %s", got)
	}
	for _, required := range []string{
		`#include "llama-memory-hybrid-idx.h"`, "ggml_backend_cpu_init()",
		"std::make_unique<llama_memory_hybrid_idx>", "llama_memory_hybrid_idx_context actual(",
		"actual.apply()", "actual.get_n_kpool()", "actual.get_n_kpool_new()", "actual.set_input_kpool(", "actual.next()",
		"Independent enumeration of live cells", "leading tensor guard", "trailing tensor guard",
		"padding overwrites live representative including idle sequence", "duplicate scatter ownership",
		"pool causal/sequence mask", "pool M-RoPE position",
		"five-padded-writes-protect-idle-rep3", "higher-active-lower-idle-shared", "idle-owner-active-sharer",
		"edited-idle-repool", "shifted-idle-repool", "clear-new-image-cells", "image-append",
		"for(bool unified:{true,false}) for(bool order:{true,false})", "GGML_TYPE_F16:GGML_TYPE_F32",
		"for(int g=1;g<=4;++g)", "waitpid(pid,&status,0)", "WIFSIGNALED(status)&&WTERMSIG(status)==SIGABRT",
		"pre-apply guard was bypassed", "if(guard==2) ++pm.t->ne[0]", "if(guard==3) ++pi.t->ne[0]", "guard==4?nullptr:nr.t",
	} {
		if !strings.Contains(fixture, required) {
			t.Errorf("missing actual CPU API/independent oracle boundary %q", required)
		}
	}
	for _, forbidden := range []string{
		"ggml_backend_load", "ggml_backend_init_best", "ggml_backend_dev_init(",
		"ggml_backend_cuda_init", "ggml_backend_vk_init", "dlopen(", "llama_model_load_from_file", "llama_decode(",
		`#include "llama-memory-hybrid-idx.cpp"`, "struct llama_memory_hybrid_idx_context::kpool_state",
		"void llama_memory_hybrid_idx_context::set_input_kpool",
	} {
		if strings.Contains(fixture, forbidden) {
			t.Errorf("CPU fixture must not discover devices, load a model or substitute production implementation: %q", forbidden)
		}
	}
	helper := nativeSource(t, "applications/llama-cpp/build-active-pool-test.sh")
	for _, required := range []string{
		"ded522f8304554317e7ff8f88698dbbbca5425aa", fixtureDigest,
		"aeeb3f45e00deae964a7949da69f1389fa98176a", "20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c",
		"a71de650fdb5611b47870e273d360e582f19472c",
		"08cbd47bcac5e481ef66c586c27510b210f34d76042dbafa6d51d9dbc489831a",
		"49fd8679a2afc53d0864948efcf5ab43f95a7e4c7a063745af3c919310a17e0f",
		"f1e92d94f7e91208cc19202564d2e79c15575573bf75162c0fd28af879b017db",
		"cfd2ef37aed10a95f9eebe032d2e2279eb9e0c6174fab569d137115a04d0e7a7",
		"5f783f7074600954af050b731abff349a92b3e72ed35bbdd82874b96be6e2b93",
		"GIT_INDEX_FILE", "git -C \"$source_dir\" add --update -- .", "git -C \"$source_dir\" diff --cached --check",
		"Unreviewed source tree", "Use installed libraries, not build/bin", "Require a fresh output directory",
		`-L "$library_dir"`, "-Wl,-rpath-link,$library_dir", "-lllama -lggml -lggml-base -lggml-cpu",
		"source.sha256", "installed-libraries.sha256", "binary.sha256",
		`sha256sum --check "$output_dir/source.sha256"`,
		`sha256sum --check "$output_dir/installed-libraries.sha256"`,
	} {
		if !strings.Contains(helper, required) {
			t.Errorf("missing source/installed-library compile coupling %q", required)
		}
	}
	if strings.Contains(helper, "\n\"$output_dir/test-qwen4exp-active-pool\"") || strings.Contains(helper, "podman ") {
		t.Fatal("fixture compiler must not execute diagnostics or launch containers")
	}
}
