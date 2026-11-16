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

//go:embed trace.Containerfile.fragment
var traceFragment string

//go:embed trace-entrypoint.sh
var traceEntrypoint string

type tracePreparation struct {
	Schema           string `json:"schema"`
	Control          string `json:"control,omitempty"`
	SourceCommit     string `json:"source_commit"`
	BaseTree         string `json:"base_tree"`
	ProductionSHA256 string `json:"production_containerfile_sha256"`
	FragmentSHA256   string `json:"fragment_sha256"`
	EntrypointSHA256 string `json:"entrypoint_sha256"`
	GeneratedSHA256  string `json:"generated_containerfile_sha256"`
}

func runPrepareTrace(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("llama-acceptance prepare-trace", flag.ContinueOnError)
	set.SetOutput(stderr)
	out := set.String("out", "", "fresh private trace recipe directory below checkout build/")
	control := set.String("control", "", "optional fixed rejected-path diagnostic: qsa-host-span")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if set.NArg() != 0 || *out == "" || (*control != "" && *control != "qsa-host-span") {
		return fmt.Errorf("prepare-trace requires --out, optional --control qsa-host-span, and no positional arguments")
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
	recipe, manifest, err := composeTraceRecipe(production, *control)
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
	if err := savePrivateJSON(filepath.Join(dir, "prepare-trace.json"), manifest); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Prepared %s and prepare-trace.json\nNo download, build, container, GPU probe or inference was run.\n", filepath.Join(dir, "Containerfile"))
	fmt.Fprintln(stdout, "Diagnostic only. Build llama-acceptance-trace-builder; export /opt/trace/source-tree.txt, then build llama-acceptance-trace with mandatory ACCEPTANCE_TRACE_TREE. Follow trace.md; host matcher logs are not GPU dispatch evidence and traced timing is not normal performance.")
	return nil
}

// The diagnostic only changes host graph logging (and the optional predeclared
// host-span control). HIP libraries must remain byte-identical; graph-captured
// execution is independently observed by rocprofv3, never inferred from logs.
func composeTraceRecipe(production []byte, control string) ([]byte, tracePreparation, error) {
	var manifest tracePreparation
	commit, tree, err := nativeSourceContract(production)
	if err != nil {
		return nil, manifest, err
	}
	if commit != controlCommit || tree != controlBaseTree {
		return nil, manifest, fmt.Errorf("trace protocol requires deliberate review for changed production source")
	}
	if control != "" && control != "qsa-host-span" {
		return nil, manifest, fmt.Errorf("unsupported trace control")
	}
	instructions, err := nativeInstructions(production)
	if err != nil {
		return nil, manifest, err
	}
	for _, instruction := range instructions {
		if instruction[0] == "FROM" && strings.HasPrefix(strings.ToLower(strings.Fields(instruction[1])[2]), "llama-acceptance-trace") {
			return nil, manifest, fmt.Errorf("trace stage collision")
		}
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9._-]+$`).MatchString(identity.LabelNamespace) {
		return nil, manifest, fmt.Errorf("unsafe trace label namespace")
	}
	mutation := ""
	if control != "" {
		mutation = qsaControlMutation
	}
	// printf's format is fixed; every line is shell-quoted, not interpolated.
	quoteLines := func(data string) string {
		var lines []string
		for _, line := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
			lines = append(lines, "'"+strings.ReplaceAll(line, "'", "'\"'\"'")+"'")
		}
		return "printf '%s\\n' " + strings.Join(lines, " \\\n        ")
	}
	resolved := strings.NewReplacer("@SOURCE_COMMIT@", commit, "@BASE_TREE@", tree,
		"@CONTROL@", control, "@LABEL_NAMESPACE@", identity.LabelNamespace,
		"@CONTROL_MUTATION@", strings.ReplaceAll(mutation, "/opt/control/evidence", "/opt/trace/evidence"),
		"@TRACE_PATCH@", quoteLines(traceSpanPatch), "@ENTRYPOINT@", quoteLines(traceEntrypoint)).Replace(traceFragment)
	if regexp.MustCompile(`@[A-Z_]+@`).MatchString(resolved) {
		return nil, manifest, fmt.Errorf("unresolved trace placeholder")
	}
	recipe := append(append(append([]byte(nil), production...), '\n'), []byte(resolved)...)
	manifest = tracePreparation{Schema: "paracetamol.trace-preparation.v1", Control: control, SourceCommit: commit, BaseTree: tree, ProductionSHA256: hashBytes(production), FragmentSHA256: hashBytes([]byte(traceFragment)), EntrypointSHA256: hashBytes([]byte(traceEntrypoint)), GeneratedSHA256: hashBytes(recipe)}
	return recipe, manifest, nil
}

// Owning-file SHA-256 is checked before either mutation. Context is deliberately
// independent of the optional ceiling change so both arms receive the same log.
const traceSpanPatch = `diff --git a/src/models/qwen4exp.cpp b/src/models/qwen4exp.cpp
--- a/src/models/qwen4exp.cpp
+++ b/src/models/qwen4exp.cpp
@@ -1098,6 +1098,15 @@ ggml_tensor * llama_model_qwen4exp::graph::build_layer_attn(
         inp_kpool->n_sel > 0 && inp_kpool->n_sel <= 2560 &&
         causal && causal->ne[2] == 1 && causal->ne[3] == 1;
     const bool compact_selection = selected_keys && graph && std::string(graph) == "1";
+    if (qsa) {
+        LLAMA_LOG_INFO("ACCEPTANCE_QSA_SPAN layer=%d tokens=%lld batch_indices=%zu k_span=%lld v_ne0=%lld v_ne1=%lld v_ne2=%lld n_sel=%lld selected=%d compact=%d\n",
+                il, (long long) n_tokens,
+                ubatch.data ? ubatch.data->batch_idxs.size() : size_t(0), (long long) cache_k->ne[2],
+                (long long) cache_v->ne[0], (long long) cache_v->ne[1],
+                (long long) cache_v->ne[2], (long long) inp_kpool->n_sel,
+                (int) selected_keys, (int) compact_selection);
+    }
+
     ggml_tensor * selected_ids = nullptr;
     ggml_tensor * sel = qsa ? build_qsa_sel(mctx_hyb, inp_kpool, cur, inp_pos, causal,
             il, &selected_ids, selected_keys, compact_selection) : nullptr;
`
