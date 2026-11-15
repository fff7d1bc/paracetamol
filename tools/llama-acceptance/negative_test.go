package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/identity"
	"paracetamol/internal/project"
)

func controlProductionFixture() string {
	s := strings.ReplaceAll(nativeProductionFixture, strings.Repeat("a", 40), controlCommit)
	s = strings.ReplaceAll(s, strings.Repeat("b", 40), controlBaseTree)
	return s + "LABEL io.github.fff7d1bc.paracetamol.llama-cpp.patches=\"baseline\"\n"
}

func controlFixtureInputs() map[string]string {
	return map[string]string{controlMMQPaths[0]: "827caed7300ee0555fa4bf6c1f1725aa01866ada4308344a6e9269a00a3b4baa", controlMMQPaths[1]: strings.Repeat("a", 64), controlMMQPaths[2]: strings.Repeat("b", 64)}
}

func TestControlRecipeCurrentInputsAndTwoFixedMutations(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	production, err := os.ReadFile(filepath.Join(root, "Containerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"mmq-sum", "qsa-host-span"} {
		inputs := map[string]string{}
		if control == "mmq-sum" {
			for _, path := range controlMMQPaths {
				data, e := nativeReadInput(root, path)
				if e != nil {
					t.Fatal(e)
				}
				inputs[path] = hashBytes(data)
			}
		}
		recipe, manifest, err := composeControlRecipe(production, negativeFragment, control, inputs)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(recipe, production) || manifest.ProductionSHA256 != hashBytes(production) || manifest.GeneratedSHA256 != hashBytes(recipe) || manifest.FragmentSHA256 != hashBytes(negativeFragment) {
			t.Fatal("lost current recipe identity")
		}
		if manifest.Control != control || manifest.SourceCommit != controlCommit || manifest.BaseTree != controlBaseTree {
			t.Fatal(manifest)
		}
		for _, required := range []string{"FROM llama-builder AS llama-acceptance-control-builder", "FROM llama-cpp AS llama-acceptance-control", "ARG ACCEPTANCE_CONTROL_TREE", `test "$ACCEPTANCE_CONTROL_TREE" = "$(cat /opt/control/source-tree.txt)"`, identity.LabelNamespace + `.llama-acceptance.control="` + control + `"`, `diagnostic/` + control, "unaffected-installed.sha256", "installed-layout.txt", "cmp /opt/control/evidence/unaffected-installed.sha256", "git write-tree > /opt/control/source-tree.txt"} {
			if !bytes.Contains(recipe, []byte(required)) {
				t.Fatalf("missing %s", required)
			}
		}
		if strings.Contains(string(recipe), "@CONTROL@") || strings.Contains(string(recipe), "@MUTATION@") {
			t.Fatal("unresolved placeholders")
		}
		if control == "mmq-sum" {
			for _, required := range []string{"git apply --reverse --check", "--target ggml-hip --verbose", "MMQ activation-sum HIP compatibility CPU source check PASS", "Modified MMQ source unexpectedly passed compatibility policy", "expected-source-policy-rejection", "0\\t18\\tggml/src/ggml-cuda/quantize.cu"} {
				if !bytes.Contains(recipe, []byte(required)) {
					t.Fatal(required)
				}
			}
			if !strings.Contains(string(recipe), `test "$(grep -c '^@@ ' /opt/control/evidence/mutation.diff)" = '3'`) {
				t.Fatal("inverse compatibility patch must retain its three separate diff hunks")
			}
		} else {
			for _, required := range []string{"--target llama --verbose", "cache_k->ne[2] <= 262140", "1\\t1\\tsrc/models/qwen4exp.cpp"} {
				if !bytes.Contains(recipe, []byte(required)) {
					t.Fatal(required)
				}
			}
			if bytes.Contains(recipe[len(production):], []byte("qsa-prefill.cu")) {
				t.Fatal("host control must not narrow HIP backend")
			}
		}
	}
}

func TestControlPreparationRejectsUnknownSourceAndModifiers(t *testing.T) {
	valid := controlProductionFixture()
	for _, control := range []string{"", "hip-only-span", "arbitrary", "mmq-sum;bad"} {
		if _, _, err := composeControlRecipe([]byte(valid), negativeFragment, control, nil); err == nil {
			t.Fatal("unknown control accepted")
		}
	}
	for name, production := range map[string]string{
		"wrong pin":         strings.ReplaceAll(valid, controlCommit, strings.Repeat("f", 40)),
		"wrong tree":        strings.ReplaceAll(valid, controlBaseTree, strings.Repeat("e", 40)),
		"missing patches":   strings.ReplaceAll(valid, `.patches="baseline"`, `.other="baseline"`),
		"ambiguous patches": valid + "LABEL io.github.fff7d1bc.paracetamol.llama-cpp.patches=\"other\"\n",
		"stage collision":   valid + "FROM llama-builder as llama-acceptance-control-builder\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := composeControlRecipe([]byte(production), negativeFragment, "qsa-host-span", nil); err == nil {
				t.Fatal("changed contract accepted")
			}
		})
	}
	inputs := controlFixtureInputs()
	inputs[controlMMQPaths[0]] = strings.Repeat("0", 64)
	if _, _, err := composeControlRecipe([]byte(valid), negativeFragment, "mmq-sum", inputs); err == nil {
		t.Fatal("wrong reverse patch accepted")
	}
	if _, _, err := composeControlRecipe([]byte(valid), negativeFragment, "mmq-sum", nil); err == nil {
		t.Fatal("missing input accepted")
	}
	if _, _, err := composeControlRecipe([]byte(valid), negativeFragment, "qsa-host-span", controlFixtureInputs()); err == nil {
		t.Fatal("unneeded inputs accepted")
	}
	changed := append(append([]byte(nil), negativeFragment...), []byte("\nRUN true\n")...)
	if _, _, err := composeControlRecipe([]byte(valid), changed, "qsa-host-span", nil); err == nil {
		t.Fatal("modified fragment accepted")
	}
}

func TestControlGeneratedShellSyntax(t *testing.T) {
	for _, control := range []string{"mmq-sum", "qsa-host-span"} {
		inputs := map[string]string{}
		if control == "mmq-sum" {
			inputs = controlFixtureInputs()
		}
		recipe, _, err := composeControlRecipe([]byte(controlProductionFixture()), negativeFragment, control, inputs)
		if err != nil {
			t.Fatal(err)
		}
		instructions, err := nativeInstructions(recipe)
		if err != nil {
			t.Fatal(err)
		}
		for _, instruction := range instructions {
			if instruction[0] == "RUN" {
				command := exec.Command("bash", "-n")
				command.Stdin = strings.NewReader(instruction[1])
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("%s shell syntax: %s: %v", control, output, err)
				}
			}
		}
	}
}

func TestPrepareControlPrivateCreateOnceNoSubprocess(t *testing.T) {
	root := nativeFixtureRoot(t)
	if err := os.WriteFile(filepath.Join(root, "Containerfile"), []byte(controlProductionFixture()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	var out, stderr bytes.Buffer
	if err := runPrepareControl(context.Background(), []string{"--control", "qsa-host-span", "--out", "build/control"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "build", "control")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("%v %v", entries, err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("output not private")
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "prepare-control.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest controlPreparation
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	recipe, err := os.ReadFile(filepath.Join(dir, "Containerfile"))
	if err != nil || manifest.GeneratedSHA256 != hashBytes(recipe) {
		t.Fatal("recipe hash mismatch")
	}
	if !strings.Contains(out.String(), "No download, image build or inference was run") {
		t.Fatal(out.String())
	}
	if err := runPrepareControl(context.Background(), []string{"--control", "qsa-host-span", "--out", "build/control"}, &out, &stderr); err == nil {
		t.Fatal("existing evidence overwritten")
	}
}

func TestPrepareControlRejectsBeforeWrites(t *testing.T) {
	for _, kind := range []string{"help", "missing args", "outside build", "cancel", "unknown control"} {
		t.Run(kind, func(t *testing.T) {
			root := nativeFixtureRoot(t)
			if err := os.WriteFile(filepath.Join(root, "Containerfile"), []byte(controlProductionFixture()), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--control", "qsa-host-span", "--out", "build/control"}
			wantError := true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "help":
				args = []string{"--help"}
				wantError = false
			case "missing args":
				args = nil
			case "outside build":
				args = []string{"--control", "qsa-host-span", "--out", "outside/control"}
			case "cancel":
				cancel()
			case "unknown control":
				args = []string{"--control", "unknown", "--out", "build/control"}
			}
			if err := runPrepareControl(ctx, args, &bytes.Buffer{}, &bytes.Buffer{}); (err != nil) != wantError {
				t.Fatalf("err=%v", err)
			}
			if _, err := os.Lstat(filepath.Join(root, "build")); !os.IsNotExist(err) {
				t.Fatalf("unexpected preparation write: %v", err)
			}
		})
	}
}
