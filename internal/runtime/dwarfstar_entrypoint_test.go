package runtime

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"paracetamol/internal/project"
)

func TestDwarfStarEntrypointSelectsExternalDSparkModel(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cli", "server"} {
		for _, enabled := range []string{"0", "1"} {
			t.Run(mode+"/dspark="+enabled, func(t *testing.T) {
				dir := t.TempDir()
				write := func(name, contents string, perm os.FileMode) string {
					t.Helper()
					path := filepath.Join(dir, name)
					if err := os.WriteFile(path, []byte(contents), perm); err != nil {
						t.Fatal(err)
					}
					return path
				}
				model := write("target with spaces.gguf", "fixture", 0600)
				support := write("support with spaces.gguf", "fixture", 0600)
				write("rocminfo", "#!/bin/sh\nprintf '  Name: gfx1151\\n'\n", 0700)
				// The entrypoint normally runs inside the container. Substitute its
				// sole directory mutation so this test cannot touch host /data.
				write("mkdir", "#!/bin/sh\ntest \"$*\" = '-p /data/home'\n", 0700)
				for _, binary := range []string{"ds4", "ds4-server"} {
					write(binary, "#!/bin/sh\nprintf '\\nARGV\\000'\nprintf '%s\\000' \"$@\"\n", 0700)
				}
				cmd := exec.Command("bash", filepath.Join(root, "applications/dwarfstar/entrypoint.sh"))
				cmd.Env = []string{
					"PATH=" + dir + ":/usr/bin:/bin",
					"PARACETAMOL_PROFILE=strix-halo",
					"PARACETAMOL_DWARFSTAR_MODE=" + mode,
					"PARACETAMOL_DWARFSTAR_MODEL=" + model,
					"PARACETAMOL_DWARFSTAR_DSPARK=" + enabled,
					"PARACETAMOL_DWARFSTAR_DSPARK_MODEL=" + support,
					"PARACETAMOL_DWARFSTAR_NO_THINKING=1",
					"PARACETAMOL_DWARFSTAR_PROMPT=Return true",
				}
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("entrypoint: %v\n%s", err, output)
				}
				_, argv, ok := bytes.Cut(output, []byte("\nARGV\x00"))
				if !ok {
					t.Fatalf("entrypoint did not reach the stub: %s", output)
				}
				var args []string
				for _, arg := range bytes.Split(bytes.TrimSuffix(argv, []byte{0}), []byte{0}) {
					args = append(args, string(arg))
				}
				if slices.Contains(args, "--mtp") {
					t.Fatalf("external DSpark selected embedded MTP: %v", args)
				}
				index := slices.Index(args, "--mtp-model")
				if enabled == "1" {
					if index < 0 || index+1 >= len(args) || args[index+1] != support || !slices.Contains(args, "--dspark") {
						t.Fatalf("DSpark model argument: %v", args)
					}
					if mode == "cli" {
						i := slices.Index(args, "--temp")
						if i < 0 || i+1 >= len(args) || args[i+1] != "0" {
							t.Fatalf("DSpark CLI must retain greedy sampling: %v", args)
						}
					}
				} else if index >= 0 || slices.Contains(args, "--dspark") || slices.Contains(args, support) {
					t.Fatalf("ordinary run selected DSpark: %v", args)
				}
			})
		}
	}
}
