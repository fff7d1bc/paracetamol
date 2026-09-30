package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLlamaMathDryRunAndEarlyValidation(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "fixture.gguf")
	if err := os.WriteFile(model, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"default", "mixed", "bf16"} {
		app, output, _ := testApp(t, &commandRunner{})
		app.Root = filepath.Join("..", "..")
		data := filepath.Join(root, mode, "absent-data")
		err := app.runLlama("server", []string{"--model", model, "--profile", "cpu", "--data-dir", data, "--dry-run", "--qwen4exp-math", mode})
		if mode == "default" {
			if err != nil || !strings.Contains(output.String(), "PARACETAMOL_LLAMA_QWEN4EXP_MATH=default") {
				t.Fatalf("output=%s err=%v", output, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "Qwen4exp math") {
			t.Fatalf("mode=%s err=%v", mode, err)
		}
		if _, err := os.Stat(data); !os.IsNotExist(err) {
			t.Fatalf("dry-run created data, err=%v", err)
		}
	}
}
