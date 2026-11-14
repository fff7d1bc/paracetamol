package application

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func nativeSource(t *testing.T, relative string) string {
	t.Helper()
	data, err := os.ReadFile("../../" + relative)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestKPoolPrefixPackaging(t *testing.T) {
	const patch = "applications/llama-cpp/kpool-prefix-retention.patch"
	const digest = "dcd11fd53a7d608a628a6958aa7e89f0245826eec15fe557373953842fae0492"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(nativeSource(t, patch)))); got != digest {
		t.Fatalf("frozen PR32137 patch digest = %s", got)
	}
	containerfile := nativeSource(t, "Containerfile")
	allowlist := nativeSource(t, ".containerignore")
	for _, name := range []string{
		"kpool-prefix-retention.patch", "test-kpool-prefix-layout.cpp",
		"extract-kpool-prefix-test.sh", "build-kpool-prefix-test.sh",
	} {
		path := "applications/llama-cpp/" + name
		if !strings.Contains(allowlist, "\n!"+path+"\n") || !strings.Contains(containerfile, path) {
			t.Errorf("missing prefix layout build input %s", path)
		}
	}
	start := strings.Index(containerfile, "COPY "+patch+" ")
	draft := strings.Index(containerfile, "COPY applications/llama-cpp/dflash-mtmd-position-gap.patch ")
	end := strings.Index(containerfile, "\nFROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp")
	if draft < 0 || start <= draft || end <= start {
		t.Fatal("host pool update must follow the reviewed native and draft builds")
	}
	late := containerfile[start:end]
	for _, required := range []string{
		"GIT_INDEX_FILE=/tmp/paracetamol-host-pool.index",
		"git read-tree HEAD", "git add --update -- .",
		"git add -- common include src ggml tools tests",
		"aeeb3f45e00deae964a7949da69f1389fa98176a",
		digest + "  /opt/paracetamol/llama-kpool-prefix-retention.patch",
		"git apply --check /opt/paracetamol/llama-kpool-prefix-retention.patch",
		"git apply /opt/paracetamol/llama-kpool-prefix-retention.patch",
		"0e4534d7f989c402dafad53e09d816f32a9264af",
		"bash /opt/paracetamol/build-kpool-prefix-test.sh",
		"/opt/llama.cpp /tmp/kpool-prefix-test",
		"--target llama-common llama-server llama-cli llama-bench",
		"/tmp/host-pool-backends.sha256",
		"sha256sum --check /tmp/host-pool-backends.sha256",
		"cmake --install build --config Release",
		"/tmp/test-llama-native-policy", "/tmp/test-server-speculative-metrics",
	} {
		if !strings.Contains(late, required) {
			t.Errorf("missing host-only prefix packaging guard %q", required)
		}
	}
	if strings.Index(late, "GIT_INDEX_FILE=") > strings.Index(late, "git ") {
		t.Fatal("source checks must use a private index before the first Git operation")
	}
	if !strings.Contains(containerfile, ",dflash-mtmd-position-gap,kpool-prefix-retention,") {
		t.Fatal("prefix patch provenance must follow the retained draft correction")
	}
	if !strings.Contains(containerfile[end:], `io.github.fff7d1bc.paracetamol.llama-cpp.source-tree="20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c"`) {
		t.Fatal("normal runtime must identify the guarded final source tree")
	}
	extract := nativeSource(t, "applications/llama-cpp/extract-kpool-prefix-test.sh")
	for _, required := range []string{
		"ded522f8304554317e7ff8f88698dbbbca5425aa",
		"Unsupported prefix owning-file hash", "Nonunique or incomplete production extraction seam",
		"production-decl.inc", "production-scan.inc", "production-update.inc",
	} {
		if !strings.Contains(extract, required) {
			t.Errorf("missing exact-source extraction guard %q", required)
		}
	}
	build := nativeSource(t, "applications/llama-cpp/build-kpool-prefix-test.sh")
	for _, required := range []string{
		"normal crossing resume-cut keep-drafts count-only", "updates=28032 verifier=0",
		"updates=28032 verifier=1", "FAIL integer oracle mismatch:",
		"FAIL production verifier mismatch", "-fsanitize=address,undefined",
		"ASAN_OPTIONS=halt_on_error=1", "UBSAN_OPTIONS=halt_on_error=1",
	} {
		if !strings.Contains(build, required) {
			t.Errorf("missing layout oracle or negative/sanitizer control %q", required)
		}
	}
}
