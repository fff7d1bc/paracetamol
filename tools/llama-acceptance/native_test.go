package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/project"
)

const nativeProductionFixture = `ARG ROCM_VERSION=10.1.0
ARG ROCM_RUNTIME_IMAGE=localhost/example:runtime
ARG LLAMA_CPP_COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
FROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk
FROM native-rocm-sdk AS llama-rocm-sdk
FROM llama-rocm-sdk AS llama-builder
WORKDIR /opt/llama.cpp
RUN test "$(git rev-parse HEAD)" = "${LLAMA_CPP_COMMIT}" && cmake -DCMAKE_INSTALL_PREFIX=/opt/llama-install .
RUN test "$(git write-tree)" = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
FROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp
COPY --from=llama-builder /opt/llama-install/ /usr/local/
LABEL io.github.fff7d1bc.paracetamol.llama-cpp.source-tree="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`

func nativeTestInputs() map[string]string {
	inputs := map[string]string{}
	for _, path := range nativeInputPaths {
		inputs[path] = hashBytes([]byte("fixture " + path))
	}
	return inputs
}

func TestComposeNativeCurrentProductionAndImmutableInputs(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	production, err := os.ReadFile(filepath.Join(root, "Containerfile"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]string{}
	for _, path := range nativeInputPaths {
		data, err := nativeReadInput(root, path)
		if err != nil {
			t.Fatal(err)
		}
		inputs[path] = hashBytes(data)
	}
	for _, upstream := range []bool{false, true} {
		recipe, manifest, err := composeNativeRecipe(production, nativeFragment, inputs, upstream)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(recipe, production) || manifest.ProductionSHA256 != hashBytes(production) || manifest.GeneratedSHA256 != hashBytes(recipe) || manifest.FragmentSHA256 != hashBytes(nativeFragment) {
			t.Fatal("recipe not composed from current checkout inputs")
		}
		if manifest.SourceCommit == "" || manifest.SourceTree == "" || manifest.UpstreamTests != upstream || len(manifest.Inputs) != 8 {
			t.Fatal(manifest)
		}
		for _, path := range nativeInputPaths {
			if !bytes.Contains(recipe, []byte(inputs[path]+"  /opt/paracetamol/"+filepath.Base(path))) {
				t.Fatalf("missing build-time input hash %s", path)
			}
		}
		if bytes.Contains(recipe, []byte("@SOURCE_")) || bytes.Contains(recipe, []byte("@INPUT_")) || bytes.Contains(recipe, []byte("@UPSTREAM_")) {
			t.Fatal("unresolved placeholders")
		}
		if bytes.Contains(recipe, []byte("tests/test-backend-ops.cpp")) != upstream {
			t.Fatal("upstream tests option ignored")
		}
	}
}

func TestComposeNativeRejectsUnknownContracts(t *testing.T) {
	for name, production := range map[string]string{
		"wrong builder":               strings.ReplaceAll(nativeProductionFixture, "FROM llama-rocm-sdk AS llama-builder", "FROM unrelated AS llama-builder"),
		"wrong runtime":               strings.ReplaceAll(nativeProductionFixture, "FROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp", "FROM native-rocm-sdk AS llama-cpp"),
		"duplicate stage":             nativeProductionFixture + "FROM llama-rocm-sdk AS llama-builder\n",
		"collision":                   nativeProductionFixture + "FROM llama-builder AS llama-acceptance-native\n",
		"short pin":                   strings.ReplaceAll(nativeProductionFixture, strings.Repeat("a", 40), "aaaa"),
		"ambiguous pin":               "ARG LLAMA_CPP_COMMIT=bad\n" + nativeProductionFixture,
		"missing workdir":             strings.ReplaceAll(nativeProductionFixture, "WORKDIR /opt/llama.cpp\n", ""),
		"missing source verification": strings.ReplaceAll(nativeProductionFixture, `test "$(git rev-parse HEAD)" = "${LLAMA_CPP_COMMIT}"`, `true`),
		"comment source verification": strings.ReplaceAll(nativeProductionFixture, `RUN test "$(git rev-parse HEAD)"`, `# RUN test "$(git rev-parse HEAD)"`),
		"install mismatch":            strings.ReplaceAll(nativeProductionFixture, "COPY --from=llama-builder /opt/llama-install/ /usr/local/", "COPY --from=llama-builder /elsewhere/ /usr/local/"),
		"tree mismatch":               strings.ReplaceAll(nativeProductionFixture, `LABEL io.github`, `# LABEL io.github`),
		"unterminated":                nativeProductionFixture + "RUN echo \\\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := composeNativeRecipe([]byte(production), nativeFragment, nativeTestInputs(), false); err == nil {
				t.Fatal("unreviewed contract accepted")
			}
		})
	}
	if _, _, err := composeNativeRecipe([]byte(nativeProductionFixture), append(append([]byte(nil), nativeFragment...), []byte("\nRUN true\n")...), nativeTestInputs(), false); err == nil {
		t.Fatal("unreviewed fragment accepted")
	}
	inputs := nativeTestInputs()
	delete(inputs, nativeInputPaths[0])
	if _, _, err := composeNativeRecipe([]byte(nativeProductionFixture), nativeFragment, inputs, false); err == nil {
		t.Fatal("missing input accepted")
	}
	inputs = nativeTestInputs()
	inputs[nativeInputPaths[0]] = "injection '"
	if _, _, err := composeNativeRecipe([]byte(nativeProductionFixture), nativeFragment, inputs, false); err == nil {
		t.Fatal("invalid hash accepted")
	}
}

func nativeFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"Containerfile": nativeProductionFixture, "catalog/catalog.json": "{}"}
	for _, path := range nativeInputPaths {
		files[path] = "fixture " + path
	}
	for path, data := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PARACETAMOL_INTERNAL_PROJECT_ROOT", root)
	return root
}

func TestPrepareNativeWritesOnlyPrivateRecipeAndManifestCreateOnce(t *testing.T) {
	root := nativeFixtureRoot(t)
	t.Setenv("PATH", t.TempDir())
	var out, stderr bytes.Buffer
	if err := runPrepareNative(context.Background(), []string{"--out", "build/new", "--upstream-tests"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "build", "new")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("output=%v err=%v", entries, err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("non-private output %s", entry.Name())
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "prepare-native.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest nativePreparation
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	recipe, err := os.ReadFile(filepath.Join(dir, "Containerfile"))
	if err != nil || manifest.GeneratedSHA256 != hashBytes(recipe) {
		t.Fatal("manifest/generated recipe mismatch")
	}
	if !strings.Contains(out.String(), "No download, image build, test or inference was run") {
		t.Fatal(out.String())
	}
	if err := runPrepareNative(context.Background(), []string{"--out", "build/new"}, &out, &stderr); err == nil {
		t.Fatal("existing output accepted")
	}
}

func TestPrepareNativeRejectsBeforeWriting(t *testing.T) {
	for _, kind := range []string{"help", "missing out", "positional", "cancel", "missing input", "input symlink", "output symlink", "outside build", "invalid production"} {
		t.Run(kind, func(t *testing.T) {
			root := nativeFixtureRoot(t)
			args := []string{"--out", "build/new"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantError := true
			switch kind {
			case "help":
				args = []string{"--help"}
				wantError = false
			case "missing out":
				args = nil
			case "positional":
				args = append(args, "extra")
			case "cancel":
				cancel()
			case "missing input":
				if err := os.Remove(filepath.Join(root, nativeInputPaths[0])); err != nil {
					t.Fatal(err)
				}
			case "input symlink":
				path := filepath.Join(root, nativeInputPaths[0])
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, nativeInputPaths[1]), path); err != nil {
					t.Fatal(err)
				}
			case "output symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "build")); err != nil {
					t.Fatal(err)
				}
			case "outside build":
				args = []string{"--out", "elsewhere/new"}
			case "invalid production":
				if err := os.WriteFile(filepath.Join(root, "Containerfile"), []byte("FROM unrelated AS other\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := runPrepareNative(ctx, args, &bytes.Buffer{}, &bytes.Buffer{})
			if (err != nil) != wantError {
				t.Fatalf("err=%v", err)
			}
			if _, err := os.Lstat(filepath.Join(root, "build", "new")); !os.IsNotExist(err) {
				t.Fatalf("unexpected output mutation %v", err)
			}
		})
	}
}

func TestNativeFragmentDoesNotRebuildOrExecuteInference(t *testing.T) {
	for _, required := range []string{"FROM llama-builder AS llama-acceptance-native-builder", "FROM llama-cpp AS llama-acceptance-native", "/opt/llama-install/lib", "sha256sum --check", "build-conv-rollback-test.sh", "build-active-pool-test.sh", "build-recurrent-state-clear-test.sh candidate", "-lllama-common -lllama -lggml -lggml-base -pthread -ldl", "ENTRYPOINT [\"/bin/false\"]"} {
		if !bytes.Contains(nativeFragment, []byte(required)) {
			t.Fatalf("missing native contract %s", required)
		}
	}
	for _, forbidden := range []string{"cmake --build", "cmake -S", "git apply", "--no-deps", "--gpu-layers", "--state-smoke", "apt-get", "pip install"} {
		if bytes.Contains(nativeFragment, []byte(forbidden)) || strings.Contains(nativeUpstreamTests, forbidden) {
			t.Fatalf("unexpected diagnostic rebuild/inference %s", forbidden)
		}
	}
}
