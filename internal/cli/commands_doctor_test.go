package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/process"
)

func TestDoctorMissingProbeImageIsActionableAndDoesNotCreateData(t *testing.T) {
	runner := &commandRunner{run: func(command process.Command) process.Result {
		switch {
		case len(command.Args) > 0 && command.Args[0] == "info":
			return process.Result{Stdout: []byte("true\n")}
		case len(command.Args) > 0 && command.Args[0] == "--version":
			return process.Result{Stdout: []byte("podman version 5.7.0\n")}
		case len(command.Args) > 1 && command.Args[0] == "image" && command.Args[1] == "exists":
			return process.Result{Status: 1}
		default:
			return process.Result{}
		}
	}}
	app, stdout, _ := testApp(t, runner)
	dataRoot := filepath.Join(t.TempDir(), "missing")
	if err := app.commandDoctor([]string{"--data-dir", dataRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataRoot); !os.IsNotExist(err) {
		t.Fatalf("doctor created its data root: %v", err)
	}
	for _, expected := range []string{"GPU probe", "not built", "build pytorch-base"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("doctor output lacks %q:\n%s", expected, stdout.String())
		}
	}
}

func TestWritableStateDistinguishesMissingAndNonDirectoryPaths(t *testing.T) {
	root := t.TempDir()
	if state := writableState(filepath.Join(root, "missing", "data")); state != "not created; parent writable" {
		t.Fatalf("missing state=%q", state)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if state := writableState(file); state != "not a directory" {
		t.Fatalf("file state=%q", state)
	}
}
