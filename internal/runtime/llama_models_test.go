package runtime

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paracetamol/internal/storage"
)

func TestExternalLlamaMountsOnlySelectedFiles(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, suffix := range []string{":rw", ":rw,Z"} {
			t.Run(strings.Join([]string{map[bool]string{false: "single", true: "split"}[split], suffix}, "/"), func(t *testing.T) {
				root := t.TempDir()
				names := []string{"model.gguf"}
				if split {
					names = []string{"model-00001-of-00002.gguf", "model-00002-of-00002.gguf"}
				}
				for _, name := range append(slices.Clone(names), "private.txt", "other.gguf") {
					if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				local := filepath.Join(root, names[0])
				direct, err := LlamaCommand(LlamaOptions{Image: "image", Profile: "cpu", Mode: "cli", DataDir: "/data", Model: local}, suffix)
				if err != nil {
					t.Fatal(err)
				}
				bench, err := LlamaBenchmarkCommand(LlamaBenchmarkOptions{Image: "image", Profile: "cpu", Model: local}, suffix)
				if err != nil {
					t.Fatal(err)
				}
				for _, command := range [][]string{direct, bench} {
					for _, name := range names {
						mount := filepath.Join(root, name) + ":/content/models/" + name + readOnlySharedSuffix(suffix)
						if !slices.Contains(command, mount) {
							t.Fatalf("missing exact mount %q: %v", mount, command)
						}
					}
					joined := strings.Join(command, " ")
					for _, forbidden := range []string{root + ":", "private.txt", "other.gguf", "label=disable", "/dev/kfd", "--publish"} {
						if strings.Contains(joined, forbidden) {
							t.Fatalf("command contains %q: %s", forbidden, joined)
						}
					}
					for _, required := range []string{"--read-only", "--cap-drop all", "no-new-privileges", "--network none", "PARACETAMOL_LLAMA_MODEL=/content/models/" + names[0]} {
						if !strings.Contains(joined, required) {
							t.Fatalf("command lacks %q: %s", required, joined)
						}
					}
				}
			})
		}
	}
}

func TestExternalLlamaRejectsInvalidMountPlan(t *testing.T) {
	for _, state := range []string{"missing", "directory", "second-shard", "zero-count", "missing-shard", "directory-shard", "symlink-shard", "colon", "non-gguf"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			name := "model.gguf"
			if strings.Contains(state, "shard") {
				name = "model-00001-of-00002.gguf"
			}
			switch state {
			case "second-shard":
				name = "model-00002-of-00002.gguf"
			case "zero-count":
				name = "model-00001-of-00000.gguf"
			case "colon":
				name = "model:options.gguf"
			case "non-gguf":
				name = "model.txt"
			}
			path := filepath.Join(root, name)
			if state == "directory" {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if state != "missing" {
				if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			second := filepath.Join(root, "model-00002-of-00002.gguf")
			if state == "directory-shard" {
				if err := os.Mkdir(second, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if state == "symlink-shard" {
				if err := os.Symlink(path, second); err != nil {
					t.Fatal(err)
				}
			}
			if command, err := LlamaCommand(LlamaOptions{Model: path}, ":rw,Z"); err == nil || command != nil {
				t.Fatalf("direct command=%v err=%v", command, err)
			}
			if command, err := LlamaBenchmarkCommand(LlamaBenchmarkOptions{Model: path}, ":rw,Z"); err == nil || command != nil {
				t.Fatalf("benchmark command=%v err=%v", command, err)
			}
		})
	}
}

func TestExternalLlamaResolvesSelectedSymlinkWithoutMountingParent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "model.gguf")
	if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.gguf")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	mounts, model, err := llamaModelMounts(storage.Layout{}, link, "", ":rw,Z")
	if err != nil || model != "/content/models/model.gguf" || !slices.Equal(mounts, []string{"--volume", target + ":/content/models/model.gguf:ro,z"}) {
		t.Fatalf("mounts=%v model=%s err=%v", mounts, model, err)
	}
}
