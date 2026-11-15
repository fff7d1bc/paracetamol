package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/project"
)

//go:embed native.Containerfile.fragment
var nativeFragment []byte

var nativeInputPaths = []string{
	"applications/llama-cpp/test-qwen4exp-state.cpp",
	"applications/llama-cpp/test-topk-moe-lifetime.cpp",
	"applications/llama-cpp/test-qwen4exp-conv-rollback.cpp",
	"applications/llama-cpp/build-conv-rollback-test.sh",
	"applications/llama-cpp/test-qwen4exp-active-pool.cpp",
	"applications/llama-cpp/build-active-pool-test.sh",
	"applications/llama-cpp/test-recurrent-state-clear.cpp",
	"applications/llama-cpp/build-recurrent-state-clear-test.sh",
}

type nativePreparation struct {
	Schema           string            `json:"schema"`
	ProductionSHA256 string            `json:"production_containerfile_sha256"`
	FragmentSHA256   string            `json:"fragment_sha256"`
	GeneratedSHA256  string            `json:"generated_containerfile_sha256"`
	SourceCommit     string            `json:"source_commit"`
	SourceTree       string            `json:"source_tree"`
	Inputs           map[string]string `json:"fixture_sha256"`
	UpstreamTests    bool              `json:"upstream_tests"`
}

func runPrepareNative(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("llama-acceptance prepare-native", flag.ContinueOnError)
	set.SetOutput(stderr)
	out := set.String("out", "", "fresh private recipe directory below checkout build/")
	upstream := set.Bool("upstream-tests", false, "also compile upstream backend-ops and context-state tests without rebuilding libraries")
	set.Usage = func() {
		fmt.Fprintln(stderr, "Usage: llama-acceptance prepare-native --out build/NEW [--upstream-tests]\nGenerate a matching-source diagnostic recipe only; no downloads, builds or inference.")
		set.PrintDefaults()
	}
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if set.NArg() != 0 || *out == "" {
		return fmt.Errorf("prepare-native requires --out and accepts no positional arguments")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := project.Root()
	if err != nil {
		return err
	}
	if _, err := validateNewOutput(root, *out); err != nil {
		return err
	}
	production, err := nativeReadInput(root, "Containerfile")
	if err != nil {
		return err
	}
	inputs := map[string]string{}
	for _, path := range nativeInputPaths {
		data, e := nativeReadInput(root, path)
		if e != nil {
			return e
		}
		inputs[path] = hashBytes(data)
	}
	recipe, manifest, err := composeNativeRecipe(production, nativeFragment, inputs, *upstream)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := createOutput(root, *out)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(dir, "Containerfile"), recipe, 0o600, atomicfile.Create); err != nil {
		return err
	}
	if err := savePrivateJSON(filepath.Join(dir, "prepare-native.json"), manifest); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Prepared %s and prepare-native.json\nNo download, image build, test or inference was run.\n", filepath.Join(dir, "Containerfile"))
	fmt.Fprintln(stdout, "Build the current runtime explicitly, then build target llama-acceptance-native with --pull=never from the checkout root. Export /opt/probe using an exact temporary container ID; compare installed-libraries.sha256 with the normal image before execution. Native probes do not establish model quality or actual optimized-kernel launch.")
	return nil
}

func nativeReadInput(root, path string) ([]byte, error) {
	full := filepath.Join(root, filepath.FromSlash(path))
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil, fmt.Errorf("native input %s: %w", path, err)
	}
	if resolved != full {
		return nil, fmt.Errorf("native input must have no symlink components: %s", path)
	}
	info, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 4*1024*1024 {
		return nil, fmt.Errorf("native input must be a bounded nonempty regular file: %s", path)
	}
	return os.ReadFile(full)
}

// Parse logical production instructions so comments cannot satisfy stage guards.
// This intentionally supports only the current explicit FROM base AS name form.
func nativeInstructions(raw []byte) ([][2]string, error) {
	var instructions [][2]string
	pending := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		continuation := strings.HasSuffix(line, "\\")
		pending += strings.TrimSuffix(line, "\\") + " "
		if continuation {
			continue
		}
		parts := strings.Fields(pending)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid Containerfile instruction")
		}
		instructions = append(instructions, [2]string{strings.ToUpper(parts[0]), strings.TrimSpace(pending[len(parts[0]):])})
		pending = ""
	}
	if pending != "" {
		return nil, fmt.Errorf("unterminated Containerfile continuation")
	}
	return instructions, nil
}

func nativeSourceContract(production []byte) (string, string, error) {
	instructions, err := nativeInstructions(production)
	if err != nil {
		return "", "", err
	}
	stages := map[string]string{}
	defaults := map[string]string{}
	beforeStage := true
	builderWorkdir := false
	stage := ""
	tree := ""
	var logical strings.Builder
	treePattern := regexp.MustCompile(`io\.github\.fff7d1bc\.paracetamol\.llama-cpp\.source-tree="([0-9a-f]{40})"`)
	for _, instruction := range instructions {
		logical.WriteString(instruction[0] + " " + instruction[1] + "\n")
		switch instruction[0] {
		case "ARG":
			if beforeStage {
				name, value, ok := strings.Cut(instruction[1], "=")
				if ok {
					if _, exists := defaults[name]; exists {
						return "", "", fmt.Errorf("duplicate production ARG %s", name)
					}
					defaults[name] = value
				}
			}
		case "FROM":
			beforeStage = false
			parts := strings.Fields(instruction[1])
			if len(parts) != 3 || !strings.EqualFold(parts[1], "AS") {
				return "", "", fmt.Errorf("unreviewed production stage syntax")
			}
			stage = strings.ToLower(parts[2])
			if _, exists := stages[stage]; exists {
				return "", "", fmt.Errorf("duplicate production stage %s", stage)
			}
			stages[stage] = parts[0]
		case "WORKDIR":
			if stage == "llama-builder" && instruction[1] == "/opt/llama.cpp" {
				builderWorkdir = true
			}
		case "LABEL":
			for _, match := range treePattern.FindAllStringSubmatch(instruction[1], -1) {
				if stage != "llama-cpp" || tree != "" {
					return "", "", fmt.Errorf("ambiguous production source-tree label")
				}
				tree = match[1]
			}
		}
	}
	for name, base := range map[string]string{"native-rocm-sdk": "${ROCM_RUNTIME_IMAGE}", "llama-rocm-sdk": "native-rocm-sdk", "llama-builder": "llama-rocm-sdk", "llama-cpp": "${ROCM_RUNTIME_IMAGE}"} {
		if stages[name] != base {
			return "", "", fmt.Errorf("production stage %s changed its base", name)
		}
	}
	for _, name := range []string{"llama-acceptance-native-builder", "llama-acceptance-native"} {
		if _, exists := stages[name]; exists {
			return "", "", fmt.Errorf("production already contains native diagnostic stage %s", name)
		}
	}
	hex40 := regexp.MustCompile(`^[0-9a-f]{40}$`)
	if !hex40.MatchString(defaults["LLAMA_CPP_COMMIT"]) || !hex40.MatchString(tree) || !builderWorkdir {
		return "", "", fmt.Errorf("missing exact source identity or builder workdir")
	}
	for _, name := range []string{"ROCM_VERSION", "ROCM_RUNTIME_IMAGE"} {
		if defaults[name] == "" || strings.ContainsAny(defaults[name], " \t$\"'") {
			return "", "", fmt.Errorf("missing literal production %s", name)
		}
	}
	for _, guard := range []string{`test "$(git rev-parse HEAD)" = "${LLAMA_CPP_COMMIT}"`, `-DCMAKE_INSTALL_PREFIX=/opt/llama-install`, `COPY --from=llama-builder /opt/llama-install/ /usr/local/`, `test "$(git write-tree)" = ` + tree} {
		if !strings.Contains(logical.String(), guard) {
			return "", "", fmt.Errorf("production source/install contract changed: %s", guard)
		}
	}
	return defaults["LLAMA_CPP_COMMIT"], tree, nil
}

func composeNativeRecipe(production, fragment []byte, inputs map[string]string, upstream bool) ([]byte, nativePreparation, error) {
	var manifest nativePreparation
	if string(fragment) != string(nativeFragment) {
		return nil, manifest, fmt.Errorf("unreviewed native fragment")
	}
	commit, tree, err := nativeSourceContract(production)
	if err != nil {
		return nil, manifest, err
	}
	if len(inputs) != len(nativeInputPaths) {
		return nil, manifest, fmt.Errorf("incomplete native fixture inputs")
	}
	var lines []string
	for _, path := range nativeInputPaths {
		digest := inputs[path]
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(digest) {
			return nil, manifest, fmt.Errorf("missing native input hash %s", path)
		}
		lines = append(lines, "'"+digest+"  /opt/paracetamol/"+filepath.Base(path)+"'")
	}
	extra, binaries := "", ""
	if upstream {
		extra = nativeUpstreamTests
		binaries = "test-backend-ops test-llama-context"
	}
	resolved := strings.NewReplacer("@INPUT_HASHES@", strings.Join(lines, " \\\n        "), "@SOURCE_COMMIT@", commit, "@SOURCE_TREE@", tree, "@UPSTREAM_TESTS@", extra, "@UPSTREAM_BINARIES@", binaries).Replace(string(fragment))
	if strings.Contains(resolved, "@SOURCE_") || strings.Contains(resolved, "@INPUT_") {
		return nil, manifest, fmt.Errorf("unresolved native recipe placeholder")
	}
	recipe := append(append(append([]byte(nil), production...), '\n'), []byte(resolved)...)
	manifest = nativePreparation{Schema: "paracetamol.native-preparation.v1", ProductionSHA256: hashBytes(production), FragmentSHA256: hashBytes(fragment), GeneratedSHA256: hashBytes(recipe), SourceCommit: commit, SourceTree: tree, Inputs: inputs, UpstreamTests: upstream}
	return recipe, manifest, nil
}

// Reviewed from the pinned source's tests/CMakeLists.txt: backend-ops has one
// translation unit linked to ggml; context has the seven explicit files below
// and links llama-common. Compile tests only, against installed production libs.
const nativeUpstreamTests = `RUN test -f tests/test-backend-ops.cpp && test -f tests/test-llama-context.cpp && \
    sha256sum tests/CMakeLists.txt tests/test-backend-ops.cpp tests/test-llama-context.cpp \
        tests/llama-context/cases-kv.cpp tests/llama-context/cases-rs.cpp \
        tests/llama-context/test-case.cpp tests/llama-context/test-log.cpp \
        tests/llama-context/test-runner.cpp tests/llama-context/test-util.cpp \
        > /opt/native-probes/upstream-inputs.sha256 && \
    c++ -O2 -std=c++17 -DGGML_SHARED -DGGML_BACKEND_SHARED \
        -Iggml/include -Iggml/src tests/test-backend-ops.cpp \
        -L/opt/llama-install/lib -Wl,-rpath-link,/opt/llama-install/lib \
        -Wl,-rpath,/usr/local/lib -lggml -lggml-base -pthread -ldl \
        -o /opt/native-probes/test-backend-ops && \
    c++ -O2 -std=c++17 -DGGML_SHARED -DGGML_BACKEND_SHARED \
        -Icommon -Isrc -Iinclude -Iggml/include -Ivendor -Itests \
        tests/test-llama-context.cpp tests/llama-context/cases-kv.cpp \
        tests/llama-context/cases-rs.cpp tests/llama-context/test-case.cpp \
        tests/llama-context/test-log.cpp tests/llama-context/test-runner.cpp \
        tests/llama-context/test-util.cpp \
        -L/opt/llama-install/lib -Wl,-rpath-link,/opt/llama-install/lib \
        -Wl,-rpath,/usr/local/lib -lllama-common -lllama -lggml -lggml-base -pthread -ldl \
        -o /opt/native-probes/test-llama-context && \
    (cd /opt/llama-install/lib && sha256sum --check /opt/native-probes/installed-libraries.sha256)`
