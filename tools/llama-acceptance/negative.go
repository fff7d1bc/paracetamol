package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/identity"
	"paracetamol/internal/project"
)

//go:embed negative.Containerfile.fragment
var negativeFragment []byte

const controlCommit = "ded522f8304554317e7ff8f88698dbbbca5425aa"
const controlBaseTree = "20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c"

type controlPreparation struct {
	Schema           string            `json:"schema"`
	Control          string            `json:"control"`
	ProductionSHA256 string            `json:"production_containerfile_sha256"`
	FragmentSHA256   string            `json:"fragment_sha256"`
	GeneratedSHA256  string            `json:"generated_containerfile_sha256"`
	SourceCommit     string            `json:"source_commit"`
	BaseTree         string            `json:"base_tree"`
	OwningFile       string            `json:"owning_file"`
	OwningSHA256     string            `json:"owning_sha256"`
	Inputs           map[string]string `json:"input_sha256"`
}

func runPrepareControl(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("llama-acceptance prepare-control", flag.ContinueOnError)
	set.SetOutput(stderr)
	out := set.String("out", "", "fresh private control recipe directory below checkout build/")
	control := set.String("control", "", "required fixed diagnostic: mmq-sum or qsa-host-span")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if set.NArg() != 0 || *out == "" || (*control != "mmq-sum" && *control != "qsa-host-span") {
		return fmt.Errorf("prepare-control requires --out and --control mmq-sum|qsa-host-span, without positional arguments")
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
	if *control == "mmq-sum" {
		for _, path := range controlMMQPaths {
			data, e := nativeReadInput(root, path)
			if e != nil {
				return e
			}
			inputs[path] = hashBytes(data)
		}
	}
	recipe, manifest, err := composeControlRecipe(production, negativeFragment, *control, inputs)
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
	if err := savePrivateJSON(filepath.Join(dir, "prepare-control.json"), manifest); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Prepared %s and prepare-control.json\nNo download, image build or inference was run.\n", filepath.Join(dir, "Containerfile"))
	fmt.Fprintln(stdout, "Diagnostic only: never use normal image tags or treat this as upgrade acceptance. Build llama-acceptance-control-builder first; extract /opt/control/source-tree.txt using an exact temporary container ID. Then build llama-acceptance-control with mandatory --build-arg ACCEPTANCE_CONTROL_TREE=THE_EXTRACTED_TREE. Inspect and retain both image IDs and /opt/control/evidence.")
	return nil
}

var controlMMQPaths = []string{"applications/llama-cpp/hip-mmq-activation-sum-compat.patch", "applications/llama-cpp/build-mmq-activation-sum-test.sh", "applications/llama-cpp/test-mmq-activation-sum.cpp"}

func composeControlRecipe(production, fragment []byte, control string, inputs map[string]string) ([]byte, controlPreparation, error) {
	var manifest controlPreparation
	if string(fragment) != string(negativeFragment) {
		return nil, manifest, fmt.Errorf("unreviewed control fragment")
	}
	commit, tree, err := nativeSourceContract(production)
	if err != nil {
		return nil, manifest, err
	}
	if commit != controlCommit || tree != controlBaseTree {
		return nil, manifest, fmt.Errorf("fixed controls require deliberate review for changed production source")
	}
	instructions, err := nativeInstructions(production)
	if err != nil {
		return nil, manifest, err
	}
	patches := ""
	pattern := regexp.MustCompile(`io\.github\.fff7d1bc\.paracetamol\.llama-cpp\.patches="([a-z0-9,-]+)"`)
	for _, instruction := range instructions {
		if instruction[0] == "FROM" {
			parts := strings.Fields(instruction[1])
			name := strings.ToLower(parts[2])
			if name == "llama-acceptance-control-builder" || name == "llama-acceptance-control" {
				return nil, manifest, fmt.Errorf("control stage collision")
			}
		}
		if instruction[0] == "LABEL" {
			for _, match := range pattern.FindAllStringSubmatch(instruction[1], -1) {
				if patches != "" {
					return nil, manifest, fmt.Errorf("ambiguous production patch label")
				}
				patches = match[1]
			}
		}
	}
	if patches == "" || !regexp.MustCompile(`^[a-zA-Z0-9._-]+$`).MatchString(identity.LabelNamespace) {
		return nil, manifest, fmt.Errorf("missing production patch label or unsafe label namespace")
	}
	file, digest, target, library, numstat, hunks, mutation := "", "", "", "", "", "", ""
	var inputLines []string
	switch control {
	case "mmq-sum":
		file = "ggml/src/ggml-cuda/quantize.cu"
		digest = "a1f93b5f882c62dd617c22ee06a4af6db5a27017f27010de1ebab9411bdb9f28"
		target = "ggml-hip"
		library = "libggml-hip.so"
		numstat = "0\\t18\\t" + file
		hunks = "3"
		mutation = mmqControlMutation
		if len(inputs) != 3 || inputs[controlMMQPaths[0]] != "827caed7300ee0555fa4bf6c1f1725aa01866ada4308344a6e9269a00a3b4baa" {
			return nil, manifest, fmt.Errorf("MMQ control requires exact compatibility patch and source-policy inputs")
		}
		for i, path := range controlMMQPaths {
			hash := inputs[path]
			if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) {
				return nil, manifest, fmt.Errorf("invalid control input hash")
			}
			name := filepath.Base(path)
			if i == 0 {
				name = "llama-hip-mmq-activation-sum-compat.patch"
			}
			inputLines = append(inputLines, "'"+hash+"  /opt/paracetamol/"+name+"'")
		}
	case "qsa-host-span":
		if len(inputs) != 0 {
			return nil, manifest, fmt.Errorf("unexpected QSA control inputs")
		}
		file = "src/models/qwen4exp.cpp"
		digest = "18b3e67959d253d63e518274fc5bfbd851a326207988652c909e8e761fcc3919"
		target = "llama"
		library = "libllama.so"
		numstat = "1\\t1\\t" + file
		hunks = "1"
		mutation = qsaControlMutation
	default:
		return nil, manifest, fmt.Errorf("unsupported fixed negative control")
	}
	resolved := strings.NewReplacer("@SOURCE_COMMIT@", commit, "@BASE_TREE@", tree, "@OWNING_FILE@", file, "@OWNING_HASH@", digest, "@LIBRARY@", library, "@INPUT_HASHES@", strings.Join(inputLines, " \\\n        "), "@MUTATION@", mutation, "@NUMSTAT@", numstat, "@HUNKS@", hunks, "@TARGET@", target, "@CONTROL@", control, "@PATCHES@", patches, "@LABEL_NAMESPACE@", identity.LabelNamespace).Replace(string(fragment))
	if regexp.MustCompile(`@[A-Z_]+@`).MatchString(resolved) {
		return nil, manifest, fmt.Errorf("unresolved negative-control placeholder")
	}
	recipe := append(append(append([]byte(nil), production...), '\n'), []byte(resolved)...)
	manifest = controlPreparation{Schema: "paracetamol.negative-control-preparation.v1", Control: control, ProductionSHA256: hashBytes(production), FragmentSHA256: hashBytes(fragment), GeneratedSHA256: hashBytes(recipe), SourceCommit: commit, BaseTree: tree, OwningFile: file, OwningSHA256: digest, Inputs: inputs}
	return recipe, manifest, nil
}

// Before/after source-policy execution distinguishes an intentional policy
// rejection from an initially broken compiler or fixture. It is not model math.
const mmqControlMutation = `bash /opt/paracetamol/build-mmq-activation-sum-test.sh /opt/llama.cpp /tmp/control-mmq-before > /opt/control/evidence/mmq-policy-before.log 2>&1; \
    grep -Fx 'MMQ activation-sum HIP compatibility CPU source check PASS (3 layouts)' /opt/control/evidence/mmq-policy-before.log; \
    git apply --reverse --check /opt/paracetamol/llama-hip-mmq-activation-sum-compat.patch; \
    git apply --reverse /opt/paracetamol/llama-hip-mmq-activation-sum-compat.patch; \
    if bash /opt/paracetamol/build-mmq-activation-sum-test.sh /opt/llama.cpp /tmp/control-mmq-after > /opt/control/evidence/mmq-policy-after.log 2>&1; then \
        echo 'Modified MMQ source unexpectedly passed compatibility policy' >&2; exit 1; \
    fi; printf '%s\n' 'expected-source-policy-rejection' > /opt/control/evidence/mmq-policy-after.status;`

// This deliberately narrows the owning host matcher, retaining the correct HIP
// backend. It is not a historical r62 reconstruction or a production proposal.
const qsaControlMutation = `printf '%s\n' \
        'diff --git a/src/models/qwen4exp.cpp b/src/models/qwen4exp.cpp' \
        '--- a/src/models/qwen4exp.cpp' '+++ b/src/models/qwen4exp.cpp' \
        '@@ -1093,7 +1093,7 @@' \
        '         cparams.flash_attn && n_embd_head == 256 && n_head_kv > 0 && n_head == 12*n_head_kv &&' \
        '         cache_k->type == GGML_TYPE_F16 && cache_v->type == GGML_TYPE_F16 &&' \
        '         cache_k->ne[3] == 1 && cache_v->ne[3] == 1 && cache_v->nb[1] < cache_v->nb[2] &&' \
        '-        cache_k->ne[2] > 0 && cache_k->ne[2] <= 262144 &&' \
        '+        cache_k->ne[2] > 0 && cache_k->ne[2] <= 262140 &&' \
        '         (n_tokens <= 512 || cache_k->ne[2] % 4 == 0) &&' \
        '         inp_kpool->n_sel > 0 && inp_kpool->n_sel <= 2560 &&' \
        '         causal && causal->ne[2] == 1 && causal->ne[3] == 1;' \
        > /opt/control/evidence/requested-mutation.patch; \
    git apply --check /opt/control/evidence/requested-mutation.patch; \
    git apply /opt/control/evidence/requested-mutation.patch;`
