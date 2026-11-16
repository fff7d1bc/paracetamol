package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/identity"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
	"paracetamol/internal/verification"
)

type preflightTerminalReader struct{ io.Reader }

func (preflightTerminalReader) IsTerminal() bool { return true }

func TestApplicationGuidesBuildDownloaderBeforeContent(t *testing.T) {
	for _, application := range []string{"comfyui", "llama-cpp"} {
		t.Run(application, func(t *testing.T) {
			app, stdout, _ := testApp(t, &commandRunner{})
			if err := app.commandGuide([]string{application}); err != nil {
				t.Fatal(err)
			}
			output := stdout.String()
			build := strings.Index(output, identity.Command("build", application))
			downloader := strings.Index(output, identity.Command("build", "content-tools"))
			content := strings.Index(output, identity.Command("content", "install"))
			if build < 0 || downloader <= build || content <= downloader {
				t.Fatalf("incomplete or out-of-order walkthrough:\n%s", output)
			}
		})
	}
}

func TestRuntimePreflightFailuresDoNotPrepareData(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"managed ComfyUI argument", []string{"run", "comfyui", "--profile", "cpu", "--", "--output-directory=/tmp/elsewhere"}, "managed by the launcher"},
		{"invalid ComfyUI policy", []string{"run", "comfyui", "--profile", "cpu", "--memory-policy", "invalid"}, "memory policy"},
		{"unknown llama preset", []string{"run", "llama-cpp", "server", "--profile", "cpu", "--preset", "unknown"}, "unknown llama.cpp preset"},
		{"uninstalled llama preset", []string{"run", "llama-cpp", "server", "--profile", "cpu", "--preset", "missing"}, "not installed"},
		{"missing llama model", []string{"run", "llama-cpp", "server", "--profile", "cpu", "--model", "/nonexistent/model.gguf"}, "GGUF model"},
		{"invalid llama listen", []string{"run", "llama-cpp", "server", "--profile", "cpu", "--preset", "unknown", "--listen", "not-an-ip"}, "listen"},
		{"unknown benchmark preset", []string{"benchmark", "llama-cpp", "throughput", "--profile", "cpu", "--preset", "unknown"}, "unknown llama.cpp preset"},
		{"uninstalled benchmark preset", []string{"benchmark", "llama-cpp", "throughput", "--profile", "cpu", "--preset", "missing"}, "not installed"},
		{"missing benchmark model", []string{"benchmark", "llama-cpp", "throughput", "--profile", "cpu", "--model", "/nonexistent/model.gguf"}, "GGUF model"},
		{"invalid benchmark flags", []string{"benchmark", "llama-cpp", "throughput", "--profile", "cpu", "--preset", "unknown", "--repetitions", "0"}, "--repetitions"},
		{"unknown ComfyUI bundle", []string{"benchmark", "comfyui", "run", "unknown", "--profile", "strix-halo"}, "unknown ComfyUI bundle"},
		{"unknown ComfyUI suite bundle", []string{"benchmark", "comfyui", "suite", "--include", "unknown", "--profile", "strix-halo"}, "unknown benchmark bundle"},
	}
	for _, test := range tests {
		for _, existing := range []bool{false, true} {
			name := test.name + "/absent"
			if existing {
				name = test.name + "/existing"
			}
			t.Run(name, func(t *testing.T) {
				runner := &commandRunner{}
				app, _, _ := testApp(t, runner)
				app.catalog = &catalog.Catalog{
					Artifacts:    map[string]catalog.Artifact{"model": {ID: "model", Target: "llama-models", Destination: "model.gguf", Size: 1, SHA256: "0"}},
					Bundles:      map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
					LlamaPresets: map[string]catalog.LlamaPreset{"missing": {ID: "missing", Bundle: "bundle", Artifact: "model", Backends: []string{"rocm"}}},
				}
				root := filepath.Join(t.TempDir(), "data")
				if existing {
					if err := os.Mkdir(root, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				// Put launcher flags before the reviewed passthrough delimiter.
				args := append([]string(nil), test.args...)
				index := len(args)
				for candidate, arg := range args {
					if arg == "--" {
						index = candidate
						break
					}
				}
				args = append(append(append([]string(nil), args[:index]...), "--data-dir", root), args[index:]...)
				err := app.Dispatch(args)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("error = %v; want %q", err, test.want)
				}
				assertPreflightDataUnchanged(t, root, existing)
				for _, command := range runner.commands {
					if command.Name != "git" {
						t.Fatalf("invalid plan reached Podman: %v", command)
					}
				}
			})
		}
	}
}

func TestComfyBenchmarkParsedPlanAndApprovalDoNotPrepareData(t *testing.T) {
	artifact := catalog.Artifact{ID: "model", Agreements: []string{"terms"}}
	bundle := catalog.Bundle{ID: "bundle", Application: "comfyui", Artifacts: []string{artifact.ID}}
	managed := catalog.Catalog{
		Artifacts:  map[string]catalog.Artifact{artifact.ID: artifact},
		Bundles:    map[string]catalog.Bundle{bundle.ID: bundle},
		Benchmarks: map[string]catalog.Benchmark{bundle.ID: {Bundle: bundle.ID}},
		Agreements: map[string]catalog.Agreement{"terms": {ID: "terms", Name: "Example terms", URL: "https://example.com/terms"}},
	}
	for _, approval := range []string{"non-interactive", "declined", "closed", "cancelled", "accepted", "dry-run"} {
		t.Run(approval, func(t *testing.T) {
			runner := &commandRunner{}
			app, _, _ := testApp(t, runner)
			app.catalog = &managed
			root := filepath.Join(t.TempDir(), "data")
			args := []string{bundle.ID, "--profile", "strix-halo", "--data-dir", root}
			want := "--accept-license"
			switch approval {
			case "non-interactive":
				args = append(args, "--non-interactive")
			case "declined":
				app.Stdin = preflightTerminalReader{strings.NewReader("n\n")}
				want = "declined"
			case "closed":
				app.Stdin = preflightTerminalReader{strings.NewReader("")}
				want = "cancelled"
			case "cancelled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				app.Context = ctx
				input, writer := io.Pipe()
				defer input.Close()
				defer writer.Close()
				app.Stdin = preflightTerminalReader{input}
				want = context.Canceled.Error()
			case "accepted":
				args = append(args, "--accept-license")
				want = ""
			case "dry-run":
				args = append(args, "--dry-run")
				want = ""
			}
			// Exercise parsing and the shared approval boundary without real GPU
			// access. Command-level device validation still precedes approval.
			options, _, err := app.comfyBenchmarkFlags("benchmark comfyui run", "test", args)
			if err != nil {
				t.Fatal(err)
			}
			err = app.requireBenchmarkAgreements(managed, []catalog.Bundle{bundle}, options.acceptLicense, options.dryRun, options.nonInteractive)
			if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
				t.Fatalf("approval error = %v; want %q", err, want)
			}
			if approval == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			assertPreflightDataUnchanged(t, root, false)
			if len(runner.commands) != 0 {
				t.Fatalf("approval failure reached host commands: %v", runner.commands)
			}
		})
	}
}

func TestRuntimePreflightStillPreparesCanonicalApplicationData(t *testing.T) {
	for _, application := range []string{"comfyui", "llama-cpp"} {
		t.Run(application, func(t *testing.T) {
			parent := t.TempDir()
			physical := filepath.Join(parent, "physical")
			if err := os.Mkdir(physical, 0o755); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(parent, "alias")
			if err := os.Symlink(physical, alias); err != nil {
				t.Fatal(err)
			}
			model := filepath.Join(parent, "model.gguf")
			if err := os.WriteFile(model, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			created, launch := false, ""
			runner := &commandRunner{run: func(command process.Command) process.Result {
				if command.Name == "podman" && len(command.Args) > 0 {
					if command.Args[0] == "info" {
						return process.Result{Stdout: []byte("true\n")}
					}
					if command.Args[0] == "create" {
						created = true
						launch = strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
						return process.Result{Stdout: []byte(fixtureContainerID)}
					}
					if command.Args[0] == "container" {
						if created && command.Args[len(command.Args)-1] == fixtureContainerID {
							if command.Args[1] == "inspect" {
								return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, identity.Container(application), fixtureContainerLabels(application, "application", launch))}
							}
							return process.Result{}
						}
						return process.Result{Status: 1}
					}
				}
				return process.Result{}
			}}
			app, _, _ := testApp(t, runner)
			app.catalog = &catalog.Catalog{}
			args := []string{"run", application}
			if application == "llama-cpp" {
				args = append(args, "server", "--model", model)
			}
			args = append(args, "--profile", "cpu", "--data-dir", filepath.Join(alias, "new-data"))
			if err := app.Dispatch(args); err != nil {
				t.Fatal(err)
			}
			layout := storage.Layout{Root: filepath.Join(physical, "new-data")}
			if info, err := os.Stat(layout.Application(application)); err != nil || !info.IsDir() {
				t.Fatalf("application data not prepared: %v", err)
			}
			found := false
			for _, command := range runner.commands {
				if len(command.Args) > 0 && command.Args[0] == "create" {
					found = strings.Contains(strings.Join(command.Args, " "), layout.Application(application)+":/data")
				}
			}
			if !found {
				t.Fatalf("runtime did not mount canonical application partition: %v", runner.commands)
			}
		})
	}
}

func TestLlamaRouterPublishesOnlyAfterPlanValidation(t *testing.T) {
	root := t.TempDir()
	artifact := catalog.Artifact{ID: "model", Target: "llama-models", Destination: "model.gguf", Size: 1, SHA256: "0"}
	bundle := catalog.Bundle{ID: "bundle", Application: "llama-cpp", Artifacts: []string{artifact.ID}}
	managed := catalog.Catalog{
		Artifacts:    map[string]catalog.Artifact{artifact.ID: artifact},
		Bundles:      map[string]catalog.Bundle{bundle.ID: bundle},
		LlamaPresets: map[string]catalog.LlamaPreset{"preset": {ID: "preset", Bundle: bundle.ID, Artifact: artifact.ID, Backends: []string{"rocm"}, DefaultContext: 4096}},
	}
	path := filepath.Join((storage.Layout{Root: root}).LlamaModels(), artifact.Destination)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(path, artifact.Size, artifact.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	created, launch := false, ""
	runner := &commandRunner{run: func(command process.Command) process.Result {
		if command.Name == "podman" && len(command.Args) > 0 {
			if command.Args[0] == "info" {
				return process.Result{Stdout: []byte("true\n")}
			}
			if command.Args[0] == "create" {
				created = true
				launch = strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
				return process.Result{Stdout: []byte(fixtureContainerID)}
			}
			if command.Args[0] == "container" {
				if created && command.Args[len(command.Args)-1] == fixtureContainerID {
					if command.Args[1] == "inspect" {
						return process.Result{Stdout: fixtureContainerJSON(t, fixtureContainerID, identity.Container("llama-cpp"), fixtureContainerLabels("llama-cpp", "application", launch))}
					}
					return process.Result{}
				}
				return process.Result{Status: 1}
			}
		}
		return process.Result{}
	}}
	app, _, _ := testApp(t, runner)
	app.catalog = &managed
	err = app.runLlama("server", []string{"--router", "--profile", "cpu", "--data-dir", root, "--api-key-file", filepath.Join(root, "absent.key")})
	if err == nil || !strings.Contains(err.Error(), "API-key file") {
		t.Fatalf("router validation error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "apps")); !os.IsNotExist(err) {
		t.Fatalf("invalid router plan published application data: %v", err)
	}
	if err := app.runLlama("server", []string{"--router", "--profile", "cpu", "--data-dir", root}); err != nil {
		t.Fatal(err)
	}
	routerPath := filepath.Join((storage.Layout{Root: root}).Application("llama-cpp"), "models.ini")
	contents, err := os.ReadFile(routerPath)
	if err != nil || !strings.Contains(string(contents), "[preset]") {
		t.Fatalf("valid router configuration was not published: contents=%q err=%v", contents, err)
	}
	for _, command := range runner.commands {
		if len(command.Args) > 0 && command.Args[0] == "create" && strings.Contains(strings.Join(command.Args, " "), routerPath+":/run/paracetamol/models.ini") {
			return
		}
	}
	t.Fatalf("valid router command omitted planned configuration mount: %v", runner.commands)
}

func assertPreflightDataUnchanged(t *testing.T, root string, existing bool) {
	t.Helper()
	if !existing {
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Fatalf("rejected plan created data root: %v", err)
		}
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected plan created data partitions: entries=%v err=%v", entries, err)
	}
}
