package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
)

func TestLocalModelDryRunsUseExactFilesAndValidateCompletePlan(t *testing.T) {
	for _, mode := range []string{"server", "cli", "benchmark"} {
		for _, state := range []string{"single", "split", "incomplete", "second-shard", "colon"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				root := t.TempDir()
				name := "model.gguf"
				if state == "split" || state == "incomplete" {
					name = "model-00001-of-00002.gguf"
				} else if state == "second-shard" {
					name = "model-00002-of-00002.gguf"
				} else if state == "colon" {
					name = "model:options.gguf"
				}
				model := filepath.Join(root, name)
				if err := os.WriteFile(model, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
				second := filepath.Join(root, "model-00002-of-00002.gguf")
				if state == "split" {
					if err := os.WriteFile(second, []byte("fixture"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				runner := &commandRunner{}
				app, stdout, _ := testApp(t, runner)
				app.catalog = &catalog.Catalog{}
				data := filepath.Join(root, "absent-data")
				args := []string{"--model", model, "--profile", "cpu", "--data-dir", data, "--dry-run"}
				var err error
				if mode == "benchmark" {
					err = app.benchmarkLlama(args)
				} else {
					err = app.runLlama(mode, args)
				}
				valid := state == "single" || state == "split"
				if (err == nil) != valid {
					t.Fatalf("err=%v output=%s", err, stdout)
				}
				if valid {
					for _, path := range []string{model, second} {
						if path == second && state != "split" {
							continue
						}
						if want := path + ":/content/models/" + filepath.Base(path) + ":ro"; !strings.Contains(stdout.String(), want) {
							t.Fatalf("missing mount %q: %s", want, stdout)
						}
					}
					if strings.Contains(stdout.String(), root+":/content/models") {
						t.Fatalf("dry run mounted parent: %s", stdout)
					}
				} else if strings.Contains(stdout.String(), "Resolved command:") {
					t.Fatalf("invalid model produced command: %s", stdout)
				}
				if _, err := os.Lstat(data); !os.IsNotExist(err) {
					t.Fatalf("dry run created data root: %v", err)
				}
				for _, command := range runner.commands {
					if command.Name != "git" {
						t.Fatalf("dry run started unexpected process: %v", command)
					}
				}
			})
		}
	}
}
