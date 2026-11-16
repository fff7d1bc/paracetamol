package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"paracetamol/internal/config"
	"paracetamol/internal/identity"
	"paracetamol/internal/imagearchive"
	"paracetamol/internal/process"
)

// Tiny Docker archives exercise the ordinary CLI save/load path without a
// Podman service, image build, model download or inference workload.
func imageTransferFixture(t *testing.T, references ...string) ([]byte, map[string]string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	write := func(name string, data []byte) {
		t.Helper()
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	type entry struct {
		Config   string
		RepoTags []string
		Layers   []string
	}
	var manifest []entry
	ids := make(map[string]string)
	for _, reference := range references {
		configuration, err := json.Marshal(map[string]string{"architecture": runtime.GOARCH, "os": "linux", "fixture": reference})
		if err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(configuration))
		ids[reference] = digest
		write(digest+".json", configuration)
		manifest = append(manifest, entry{Config: digest + ".json", RepoTags: []string{reference}, Layers: []string{"layer/layer.tar"}})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	write("manifest.json", data)
	write("layer/layer.tar", []byte("fixture layer"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), ids
}

type imageTransferRunner struct {
	commands               [][]string
	before, after          map[string]string
	archive                []byte
	loaded                 bool
	saveStatus, loadStatus int
	onSave                 func(string) error
}

func (*imageTransferRunner) LookPath(string) (string, error) { return "/fake/podman", nil }

func (runner *imageTransferRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	args := command.Args
	runner.commands = append(runner.commands, append([]string(nil), args...))
	ids := runner.before
	if runner.loaded {
		ids = runner.after
	}
	switch {
	case reflect.DeepEqual(args, []string{"info", "--format", "{{.Host.Security.Rootless}}"}):
		return process.Result{Stdout: []byte("true\n")}, nil
	case len(args) == 3 && args[0] == "image" && args[1] == "exists":
		if _, present := ids[args[2]]; present {
			return process.Result{}, nil
		}
		return process.Result{Status: 1}, nil
	case len(args) == 5 && reflect.DeepEqual(args[:4], []string{"image", "inspect", "--format", "{{.Id}}"}):
		if id, present := ids[args[4]]; present {
			return process.Result{Stdout: []byte(id + "\n")}, nil
		}
		return process.Result{Status: 125, Stderr: []byte("missing image")}, nil
	case len(args) >= 6 && reflect.DeepEqual(args[:4], []string{"save", "--format", "docker-archive", "--output"}):
		if err := os.WriteFile(args[4], runner.archive, 0o600); err != nil {
			return process.Result{}, err
		}
		if runner.onSave != nil {
			if err := runner.onSave(args[4]); err != nil {
				return process.Result{}, err
			}
		}
		return process.Result{Status: runner.saveStatus}, nil
	case len(args) == 3 && args[0] == "load" && args[1] == "--input":
		runner.loaded = true
		return process.Result{Status: runner.loadStatus}, nil
	default:
		return process.Result{}, fmt.Errorf("unexpected fake Podman command %q", args)
	}
}

func (runner *imageTransferRunner) count(command string) int {
	count := 0
	for _, args := range runner.commands {
		if len(args) > 0 && args[0] == command {
			count++
		}
	}
	return count
}

func TestImagesExportPublishesVerifiedBareAndPrefixedIDs(t *testing.T) {
	for _, prefix := range []string{"", "sha256:"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			data, ids := imageTransferFixture(t, config.ContentToolsImage)
			runner := &imageTransferRunner{archive: data, before: map[string]string{config.ContentToolsImage: prefix + ids[config.ContentToolsImage]}}
			app, stdout, _ := testApp(t, runner)
			root := t.TempDir()
			output := filepath.Join(root, "export.tar")
			runner.onSave = func(partial string) error {
				if filepath.Dir(partial) != root || partial == output {
					t.Fatalf("save not using same-directory partial: %s", partial)
				}
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatalf("final output visible during save: %v", err)
				}
				return nil
			}
			if err := app.Dispatch([]string{"images", "export", "content-tools", "--output", output}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(output)
			if err != nil || !bytes.Equal(got, data) || runner.count("save") != 1 || !strings.Contains(stdout.String(), "Exported:") {
				t.Fatalf("publication err=%v save=%d output=%s", err, runner.count("save"), stdout)
			}
			info, err := os.Stat(output)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("archive mode=%v err=%v", info, err)
			}
			assertImageTransferFiles(t, root, "export.tar")
			// Create-once publication may not overwrite an existing archive.
			if err := app.Dispatch([]string{"images", "export", "content-tools", "--output", output}); err == nil || runner.count("save") != 1 {
				t.Fatalf("re-export err=%v saves=%d", err, runner.count("save"))
			}
		})
	}
}

func TestImagesExportFailureDoesNotPublishOrLeavePartial(t *testing.T) {
	for _, test := range []struct {
		name       string
		id         string
		badArchive bool
		saveStatus int
		want       string
		wantSaves  int
	}{
		{name: "different config", id: strings.Repeat("0", 64), want: "identity changed", wantSaves: 1},
		{name: "malformed identity", id: "sha256:not-a-digest", want: "invalid image ID"},
		{name: "malformed archive", badArchive: true, want: "image archive", wantSaves: 1},
		{name: "save failure", saveStatus: 125, want: "exit status 125", wantSaves: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, ids := imageTransferFixture(t, config.ContentToolsImage)
			id := test.id
			if id == "" {
				id = ids[config.ContentToolsImage]
			}
			if test.badArchive {
				data = []byte("not an archive")
			}
			runner := &imageTransferRunner{archive: data, before: map[string]string{config.ContentToolsImage: id}, saveStatus: test.saveStatus}
			app, _, _ := testApp(t, runner)
			root := t.TempDir()
			err := app.Dispatch([]string{"images", "export", "content-tools", "--output", filepath.Join(root, "export.tar")})
			if err == nil || !strings.Contains(err.Error(), test.want) || runner.count("save") != test.wantSaves {
				t.Fatalf("err=%v saves=%d want=%d", err, runner.count("save"), test.wantSaves)
			}
			assertImageTransferFiles(t, root)
		})
	}
}

func TestImagesImportIdenticalDoesNotLoad(t *testing.T) {
	for _, prefix := range []string{"", "sha256:"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			data, ids := imageTransferFixture(t, config.ContentToolsImage)
			root := t.TempDir()
			input := writeImageTransferInput(t, root, data)
			runner := &imageTransferRunner{before: map[string]string{config.ContentToolsImage: prefix + ids[config.ContentToolsImage]}}
			app, stdout, _ := testApp(t, runner)
			if err := app.Dispatch([]string{"images", "import", input}); err != nil {
				t.Fatal(err)
			}
			if runner.loaded || !strings.Contains(stdout.String(), "already present") {
				t.Fatalf("loaded=%t output=%s", runner.loaded, stdout)
			}
			assertImageTransferInputUnchanged(t, root, data)
		})
	}
}

func TestImagesImportLoadsThenVerifiesEveryImage(t *testing.T) {
	for _, prefix := range []string{"", "sha256:"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			data, ids := imageTransferFixture(t, config.ContentToolsImage, config.ROCmRuntimeImage)
			root := t.TempDir()
			input := writeImageTransferInput(t, root, data)
			runner := &imageTransferRunner{before: map[string]string{config.ContentToolsImage: ids[config.ContentToolsImage]}, after: make(map[string]string)}
			for ref, id := range ids {
				runner.after[ref] = prefix + id
			}
			app, stdout, _ := testApp(t, runner)
			if err := app.Dispatch([]string{"images", "import", input}); err != nil {
				t.Fatal(err)
			}
			if runner.count("load") != 1 || !strings.Contains(stdout.String(), "Imported 2 managed images.") {
				t.Fatalf("loads=%d output=%s", runner.count("load"), stdout)
			}
			want := [][]string{
				{"info", "--format", "{{.Host.Security.Rootless}}"},
				{"image", "exists", config.ContentToolsImage},
				{"image", "inspect", "--format", "{{.Id}}", config.ContentToolsImage},
				{"image", "exists", config.ROCmRuntimeImage},
				{"load", "--input", input},
				{"image", "inspect", "--format", "{{.Id}}", config.ContentToolsImage},
				{"image", "inspect", "--format", "{{.Id}}", config.ROCmRuntimeImage},
			}
			if !reflect.DeepEqual(runner.commands, want) {
				t.Fatalf("commands=%q want=%q", runner.commands, want)
			}
			assertImageTransferInputUnchanged(t, root, data)
		})
	}
}

func TestImagesImportPreflightRejectsConflictAndMalformedIDBeforeLoad(t *testing.T) {
	for _, id := range []string{strings.Repeat("0", 64), "sha256:" + strings.Repeat("0", 64), "bad", "sha256:"} {
		t.Run("id="+id, func(t *testing.T) {
			// The first sorted image is missing, but a later conflict must still
			// prevent load rather than replacing that tag as a side effect.
			data, _ := imageTransferFixture(t, config.ContentToolsImage, config.ROCmRuntimeImage)
			root := t.TempDir()
			input := writeImageTransferInput(t, root, data)
			runner := &imageTransferRunner{before: map[string]string{config.ROCmRuntimeImage: id}}
			app, _, _ := testApp(t, runner)
			err := app.Dispatch([]string{"images", "import", input})
			if err == nil || runner.loaded {
				t.Fatalf("err=%v loaded=%t", err, runner.loaded)
			}
			want := "invalid image ID"
			if len(strings.TrimPrefix(id, "sha256:")) == 64 {
				want = "different image"
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("err=%v want=%q", err, want)
			}
			assertImageTransferInputUnchanged(t, root, data)
		})
	}
}

func TestImagesImportPostloadFailurePreservesArchiveAndRejectsSuccess(t *testing.T) {
	for _, test := range []struct {
		name       string
		id         string
		missing    bool
		loadStatus int
		want       string
	}{
		{name: "different config", id: strings.Repeat("0", 64), want: "verification failed"},
		{name: "different prefixed config", id: "sha256:" + strings.Repeat("0", 64), want: "verification failed"},
		{name: "malformed ID", id: "sha256:bad", want: "verification failed"},
		{name: "inspection failed", missing: true, want: "verification failed"},
		{name: "load failed", loadStatus: 125, want: "exit status 125"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, _ := imageTransferFixture(t, config.ContentToolsImage)
			root := t.TempDir()
			input := writeImageTransferInput(t, root, data)
			runner := &imageTransferRunner{after: make(map[string]string), loadStatus: test.loadStatus}
			if !test.missing {
				runner.after[config.ContentToolsImage] = test.id
			}
			app, stdout, _ := testApp(t, runner)
			err := app.Dispatch([]string{"images", "import", input})
			if err == nil || !strings.Contains(err.Error(), test.want) || runner.count("load") != 1 || strings.Contains(stdout.String(), "Imported ") {
				t.Fatalf("err=%v loads=%d output=%s", err, runner.count("load"), stdout)
			}
			assertImageTransferInputUnchanged(t, root, data)
		})
	}
}

func TestImagesDryRunValidatesWithoutMutation(t *testing.T) {
	for _, operation := range []string{"export", "import"} {
		t.Run(operation, func(t *testing.T) {
			data, ids := imageTransferFixture(t, config.ContentToolsImage)
			root := t.TempDir()
			runner := &imageTransferRunner{archive: data}
			app, stdout, _ := testApp(t, runner)
			dataRoot := filepath.Join(root, "absent-data")
			app.Environment[identity.EnvironmentPrefix()+"_DATA_DIR"] = dataRoot
			var args []string
			if operation == "export" {
				runner.before = ids
				args = []string{"images", "export", "content-tools", "--output", filepath.Join(root, "export.tar"), "--dry-run"}
			} else {
				args = []string{"images", "import", writeImageTransferInput(t, root, data), "--dry-run"}
			}
			if err := app.Dispatch(args); err != nil {
				t.Fatal(err)
			}
			if runner.count("save") != 0 || runner.loaded || !strings.Contains(stdout.String(), "Command:") {
				t.Fatalf("dry run mutated images: commands=%q output=%s", runner.commands, stdout)
			}
			if _, err := os.Lstat(dataRoot); !os.IsNotExist(err) {
				t.Fatalf("dry run created persistent data: %v", err)
			}
			if operation == "export" {
				assertImageTransferFiles(t, root)
			} else {
				assertImageTransferInputUnchanged(t, root, data)
			}
		})
	}
}

func writeImageTransferInput(t *testing.T, root string, data []byte) string {
	t.Helper()
	input := filepath.Join(root, "input.tar")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Keep the archive's canonical identity distinct from Podman's bare .Id.
	archive, err := imagearchive.Inspect(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, image := range archive.Images {
		if !strings.HasPrefix(image.ID, "sha256:") || len(image.ID) != 71 {
			t.Fatalf("archive identity is not canonical: %q", image.ID)
		}
	}
	return input
}

func assertImageTransferInputUnchanged(t *testing.T, root string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, "input.tar"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("input archive changed: %v", err)
	}
	assertImageTransferFiles(t, root, "input.tar")
}

func assertImageTransferFiles(t *testing.T, root string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remaining files=%v want=%v", got, want)
	}
}
