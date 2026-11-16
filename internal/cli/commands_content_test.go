package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/process"
	"paracetamol/internal/project"
	"paracetamol/internal/recipes"
)

func TestExactBundleBrowserCoversCatalogExactlyOnce(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	covered := make(map[string]int)
	for _, category := range exactBundleCategories {
		bundles, err := exactBundlesInCategory(managed, category.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, bundle := range bundles {
			covered[bundle.ID]++
		}
	}
	if len(covered) != len(managed.Bundles) {
		t.Fatalf("browser covers %d of %d bundles", len(covered), len(managed.Bundles))
	}
	for identifier, count := range covered {
		if count != 1 {
			t.Fatalf("bundle %s appears in %d categories", identifier, count)
		}
	}
}

func TestGuidedContentCanBrowseExactApplicationBundles(t *testing.T) {
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"model": {ID: "model", Size: 1024}},
		Bundles: map[string]catalog.Bundle{
			"llama-special": {ID: "llama-special", Application: "llama-cpp", Artifacts: []string{"model"}, Groups: []string{"all"}, Description: "Special quantization"},
		},
	}
	var output bytes.Buffer
	values, err := recipes.ForApplication("llama-cpp")
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Context: context.Background(), Environment: map[string]string{},
		Stdin: terminalPromptReader{Reader: strings.NewReader(strconv.Itoa(len(values)+1) + "\n1\n")}, Stdout: &output,
	}
	target, selection, err := app.guidedContentSelection(managed, "llama-cpp")
	if err != nil {
		t.Fatal(err)
	}
	if target != "llama-special" || selection != "" {
		t.Fatalf("selection = %q %q", target, selection)
	}
	for _, expected := range []string{"llama.cpp content:", "exact bundles", "Special quantization"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("guided browser lacks %q:\n%s", expected, output.String())
		}
	}
}

func TestContentModelInventoryIncludesAliasesAndDetailedPolicy(t *testing.T) {
	dataRoot := t.TempDir()
	llama := catalog.Artifact{ID: "llama", Destination: "qwen/model.gguf", Target: "llama-models", Size: 1024, SHA256: "0"}
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"llama": llama},
		Bundles: map[string]catalog.Bundle{
			"qwen": {ID: "qwen", Application: "llama-cpp", Artifacts: []string{"llama"}},
		},
		LlamaPresets: map[string]catalog.LlamaPreset{
			"qwen-base": {ID: "qwen-base", Bundle: "qwen", Artifact: "llama", DefaultContext: 65536, Jinja: true},
			"qwen-mtp":  {ID: "qwen-mtp", Bundle: "qwen", Artifact: "llama", DefaultContext: 131072, SpeculativeType: "draft-mtp", DraftTokens: 3, SamplingPolicy: "qwen-policy"},
		},
	}
	var stdout, stderr bytes.Buffer
	app := App{Context: context.Background(), Environment: map[string]string{}, Stdin: bytes.NewReader(nil), Stdout: &stdout, Stderr: &stderr, catalog: &managed}
	if err := app.contentList([]string{"models", "--data-dir", dataRoot, "--details"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"llama.cpp models", "qwen-base, qwen-mtp", "Managed preset details",
		"Default context", "131072 tokens", "Sampling", "gateway policy qwen-policy",
	} {
		if !bytes.Contains(stdout.Bytes(), []byte(expected)) {
			t.Fatalf("inventory lacks %q:\n%s", expected, stdout.String())
		}
	}
}

func TestLlamaModelLoadInventoryPolicyIncludesResidentStrixDefaults(t *testing.T) {
	got := llamaModelLoadInventoryPolicy(map[string]string{
		"strix-halo": catalog.LlamaModelLoadMMapLazyTokenEmbedding,
	})
	for _, expected := range []string{"strix-halo=mmap-lazy-token-embedding", "strix-point=resident"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("model-load policy %q lacks %q", got, expected)
		}
	}
}

func TestContentListValidatesApplicationFilters(t *testing.T) {
	managed := catalog.Catalog{Bundles: map[string]catalog.Bundle{}, LlamaPresets: map[string]catalog.LlamaPreset{}}
	for _, arguments := range [][]string{{"models", "--application", "comfyui", "--data-dir", t.TempDir()}, {"models", "--application", "dwarfstar"}, {"bundles", "--application", "dwarfstar"}, {"bundles", "--application", "unknown"}} {
		var stdout, stderr bytes.Buffer
		app := App{Context: context.Background(), Environment: map[string]string{}, Stdout: &stdout, Stderr: &stderr, catalog: &managed}
		if err := app.contentList(arguments); err == nil {
			t.Fatalf("%v succeeded", arguments)
		}
	}
}

func TestContentInstallDryRunStatesThatItDidNotMutateStorage(t *testing.T) {
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"model": {ID: "model", Destination: "model.gguf", Target: "llama-models", Size: 1, SHA256: "0"}},
		Bundles:   map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
	}
	dataRoot := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer
	app := App{Context: context.Background(), Environment: map[string]string{}, Stdin: bytes.NewReader(nil), Stdout: &stdout, Stderr: &stderr, catalog: &managed}
	if err := app.contentInstall([]string{"bundle", "--dry-run", "--data-dir", dataRoot}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Dry run: no data directory was created") {
		t.Fatalf("dry-run output lacks mutation summary:\n%s", stdout.String())
	}
	if _, err := os.Stat(dataRoot); !os.IsNotExist(err) {
		t.Fatalf("dry run created data root: %v", err)
	}
}

func TestContentInstallValidatesMirrorBeforeApprovalAndDryRun(t *testing.T) {
	for _, name := range []string{"missing", "regular-file", "dangling", "move-without-mirror", "overlap"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(name+"/dry="+strconv.FormatBool(dryRun), func(t *testing.T) {
				base := t.TempDir()
				data, mirror := filepath.Join(base, "data"), filepath.Join(base, "mirror")
				want := "cannot access local mirror"
				switch name {
				case "regular-file":
					want = "not a directory"
					if err := os.WriteFile(mirror, []byte("fixture"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "dangling":
					if err := os.Symlink(filepath.Join(base, "absent"), mirror); err != nil {
						t.Fatal(err)
					}
				case "move-without-mirror":
					mirror, want = "", "requires --local-mirror"
				case "overlap":
					want = "must not overlap"
					if err := os.Mkdir(mirror, 0o700); err != nil {
						t.Fatal(err)
					}
					data = filepath.Join(mirror, "new", "data")
				}
				artifact := catalog.Artifact{ID: "model", Destination: "model.gguf", Target: "llama-models", Size: 1, SHA256: "0", License: catalog.License{Status: "unverified"}, Agreements: []string{"terms"}}
				managed := catalog.Catalog{
					Artifacts:  map[string]catalog.Artifact{"model": artifact},
					Bundles:    map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
					Agreements: map[string]catalog.Agreement{"terms": {Name: "Example terms"}},
				}
				runner := &commandRunner{}
				app, stdout, _ := testApp(t, runner)
				app.catalog = &managed
				app.Stdin = terminalPromptReader{Reader: strings.NewReader("yes\nyes\n")}
				app.Environment["XDG_RUNTIME_DIR"] = filepath.Join(base, "runtime")
				args := []string{"bundle", "--data-dir", data}
				if mirror != "" {
					args = append(args, "--local-mirror", mirror)
				} else {
					args = append(args, "--local-mirror-move")
				}
				if dryRun {
					args = append(args, "--dry-run")
				}
				if err := app.contentInstall(args); err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("invalid mirror install = %v; want %q", err, want)
				}
				if stdout.Len() != 0 || len(runner.commands) != 0 {
					t.Fatalf("invalid mirror crossed plan/approval/process boundary: %q, %v", stdout.String(), runner.commands)
				}
				for _, root := range []string{data, filepath.Join(base, "runtime")} {
					if _, err := os.Lstat(root); !os.IsNotExist(err) {
						t.Fatalf("invalid mirror created %s: %v", root, err)
					}
				}
			})
		}
	}
}

func TestContentInstallMirrorDryRunKeepsWorstCaseDownloadPlan(t *testing.T) {
	base := t.TempDir()
	mirror, data := filepath.Join(base, "mirror"), filepath.Join(base, "data")
	if err := os.Mkdir(mirror, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := []byte("fixture")
	if err := os.WriteFile(filepath.Join(mirror, "model.gguf"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := catalog.Artifact{ID: "model", Destination: "model.gguf", Target: "llama-models", Size: int64(len(contents)), SHA256: fmt.Sprintf("%x", sha256.Sum256(contents)), License: catalog.License{Status: "unverified"}}
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{"model": artifact},
		Bundles:   map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
	}
	runner := &commandRunner{}
	app, stdout, _ := testApp(t, runner)
	app.catalog = &managed
	if err := app.contentInstall([]string{"bundle", "--data-dir", data, "--local-mirror", mirror, "--local-mirror-move", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Download: "+humanSize(artifact.Size)) || strings.Contains(stdout.String(), "Checking local mirror") {
		t.Fatalf("mirror preview did not retain the unverified worst-case download size:\n%s", stdout.String())
	}
	if len(runner.commands) != 0 {
		t.Fatalf("mirror preview ran subprocesses: %v", runner.commands)
	}
	if got, err := os.ReadFile(filepath.Join(mirror, "model.gguf")); err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("mirror preview modified source: %q, %v", got, err)
	}
	if _, err := os.Lstat(data); !os.IsNotExist(err) {
		t.Fatalf("mirror preview created data root: %v", err)
	}
}

func TestContentInstallApprovalFailureLeavesDataRootAbsent(t *testing.T) {
	for _, name := range []string{"terms", "risk", "declined", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			artifact := catalog.Artifact{ID: "model", Destination: "model.gguf", Target: "llama-models", Size: 1, SHA256: "0", License: catalog.License{Status: "unverified"}}
			if name == "terms" {
				artifact.Agreements = []string{"terms"}
			}
			managed := catalog.Catalog{
				Artifacts:  map[string]catalog.Artifact{"model": artifact},
				Bundles:    map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
				Agreements: map[string]catalog.Agreement{"terms": {Name: "Example terms"}},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := &commandRunner{}
			app, _, _ := testApp(t, runner)
			app.Context, app.catalog = ctx, &managed
			if name == "declined" || name == "cancelled" {
				app.Stdin = terminalPromptReader{Reader: strings.NewReader("no\n")}
			}
			if name == "cancelled" {
				cancel()
			}
			root := filepath.Join(t.TempDir(), "data")
			mirror := t.TempDir()
			if err := app.contentInstall([]string{"bundle", "--data-dir", root, "--local-mirror", mirror}); err == nil {
				t.Fatal("unapproved install succeeded")
			}
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("unapproved install created data root: %v", err)
			}
			if len(runner.commands) != 0 {
				t.Fatalf("unapproved install ran subprocesses: %v", runner.commands)
			}
		})
	}
}

func TestContentInstallRejectsCollisionBeforeApprovalOrDryRun(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(strconv.FormatBool(dryRun), func(t *testing.T) {
			artifact := catalog.Artifact{ID: "model", Destination: "model.gguf", Target: "llama-models", Size: 1, SHA256: "0"}
			managed := catalog.Catalog{
				Artifacts: map[string]catalog.Artifact{"model": artifact},
				Bundles:   map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
			}
			root := t.TempDir()
			path := content.ArtifactPath(root, artifact)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("user bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			runner := &commandRunner{}
			app, stdout, _ := testApp(t, runner)
			app.catalog = &managed
			app.Stdin = terminalPromptReader{Reader: strings.NewReader("yes\n")}
			args := []string{"bundle", "--data-dir", root}
			if dryRun {
				args = append(args, "--dry-run")
			}
			if err := app.contentInstall(args); err == nil || !strings.Contains(err.Error(), "unexpected managed content") {
				t.Fatalf("collision error = %v", err)
			}
			if strings.Contains(stdout.String(), "Acknowledge") || len(runner.commands) != 0 {
				t.Fatalf("collision crossed approval or process boundary: %q, %v", stdout, runner.commands)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != "user bytes" {
				t.Fatalf("collision changed: %q, %v", got, err)
			}
		})
	}
}

func TestManagedDataRootResolvesExistingAliasWithoutCreatingMissingRoot(t *testing.T) {
	app, _, _ := testApp(t, &commandRunner{})
	base := t.TempDir()
	target := filepath.Join(base, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if root, err := app.resolveManagedDataRoot(alias); err != nil || root != target {
		t.Fatalf("existing alias = %q, %v", root, err)
	}
	underAlias := filepath.Join(alias, "nested", "data")
	want := filepath.Join(target, "nested", "data")
	if root, err := app.resolveManagedDataRoot(underAlias); err != nil || root != want {
		t.Fatalf("missing root under alias = %q, %v; want %q", root, err, want)
	}
	if _, err := os.Lstat(filepath.Join(target, "nested")); !os.IsNotExist(err) {
		t.Fatalf("resolution created missing suffix under alias: %v", err)
	}
	missing := filepath.Join(base, "absent")
	if root, err := app.resolveManagedDataRoot(missing); err != nil || root != missing {
		t.Fatalf("missing root = %q, %v", root, err)
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatalf("resolution created missing root: %v", err)
	}
	dangling := filepath.Join(base, "dangling")
	if err := os.Symlink(missing, dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := app.resolveManagedDataRoot(dangling); err == nil {
		t.Fatal("dangling existing alias was accepted")
	}
	regular := filepath.Join(base, "file")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(base, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{filepath.Join(dangling, "child"), regular, filepath.Join(regular, "child"), loop, filepath.Join(loop, "child")} {
		if root, err := app.resolveManagedDataRoot(invalid); err == nil {
			t.Fatalf("invalid data root %q resolved to %q", invalid, root)
		}
	}
}

func TestContentInstallRefusesMirrorOverlapThroughMissingDataRootAlias(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(strconv.FormatBool(move), func(t *testing.T) {
			base := t.TempDir()
			mirror, alias := filepath.Join(base, "real"), filepath.Join(base, "alias")
			if err := os.Mkdir(mirror, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(mirror, alias); err != nil {
				t.Fatal(err)
			}
			contents := []byte("fixture")
			artifact := catalog.Artifact{
				ID: "model", Destination: "model.gguf", Target: "llama-models", Size: int64(len(contents)),
				SHA256: fmt.Sprintf("%x", sha256.Sum256(contents)), Source: catalog.Source{Path: "model.gguf"}, License: catalog.License{Status: "verified"},
			}
			source := filepath.Join(mirror, "model.gguf")
			if err := os.WriteFile(source, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			managed := catalog.Catalog{
				Artifacts: map[string]catalog.Artifact{"model": artifact},
				Bundles:   map[string]catalog.Bundle{"bundle": {ID: "bundle", Application: "llama-cpp", Artifacts: []string{"model"}}},
			}
			runner := &commandRunner{run: func(command process.Command) process.Result {
				if command.Name != "podman" || len(command.Args) == 0 || command.Args[0] != "ps" {
					t.Errorf("unexpected command after mirror overlap: %v", command)
					return process.Result{Status: 1}
				}
				return process.Result{}
			}}
			app, _, _ := testApp(t, runner)
			app.catalog = &managed
			app.Environment["XDG_RUNTIME_DIR"] = filepath.Join(base, "runtime")
			args := []string{"bundle", "--data-dir", filepath.Join(alias, "new"), "--local-mirror", mirror}
			if move {
				args = append(args, "--local-mirror-move")
			}
			if err := app.contentInstall(args); err == nil || !strings.Contains(err.Error(), "must not overlap") {
				t.Fatalf("overlapping mirror install = %v", err)
			}
			if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, contents) {
				t.Fatalf("overlapping mirror source changed: %q, %v", got, err)
			}
			if _, err := os.Lstat(content.ArtifactPath(filepath.Join(mirror, "new"), artifact)); !os.IsNotExist(err) {
				t.Fatalf("content was installed from overlapping mirror: %v", err)
			}
		})
	}
}

type contentImportTransport func(*http.Request) (*http.Response, error)

func (transport contentImportTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type contentApprovalReader struct {
	io.Reader
	beforeRead func()
}

func (reader *contentApprovalReader) Read(buffer []byte) (int, error) {
	if reader.beforeRead != nil {
		before := reader.beforeRead
		reader.beforeRead = nil
		before()
	}
	return reader.Reader.Read(buffer)
}

func TestContentImportApprovalPreflightAndPackPublication(t *testing.T) {
	contents := []byte("fixture")
	digest := fmt.Sprintf("%x", sha256.Sum256(contents))
	metadata := fmt.Sprintf(`{"sha":%q,"siblings":[{"rfilename":"model.gguf","lfs":{"size":%d,"sha256":%q}}]}`, strings.Repeat("a", 40), len(contents), digest)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	// No socket or provider request is needed. Fail any unexpected HTTP route.
	http.DefaultTransport = contentImportTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://huggingface.co/api/models/example/model/revision/main?blobs=true" {
			return nil, fmt.Errorf("unexpected metadata request: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(metadata)), Request: request}, nil
	})
	for _, name := range []string{"dry-run", "declined", "noninteractive", "forced-noninteractive", "installed-needs-risk", "collision", "dry-run-collision", "late-collision", "pack-collision", "approved-terminal", "approved-flag"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			root, pack := filepath.Join(base, "data"), filepath.Join(base, "imports", "model.json")
			runner := &commandRunner{run: func(command process.Command) process.Result {
				if _, err := os.Stat(pack); err != nil {
					t.Errorf("installer reached Podman before pack publication: %v", err)
				}
				if command.Args[0] == "info" {
					return process.Result{Stdout: []byte("true\n")}
				}
				if command.Args[0] == "image" {
					return process.Result{Status: 1}
				}
				return process.Result{}
			}}
			app, stdout, _ := testApp(t, runner)
			app.Environment["XDG_RUNTIME_DIR"] = filepath.Join(base, "runtime")
			args := []string{"https://huggingface.co/example/model/blob/main/model.gguf", "--save-pack", pack, "--data-dir", root}
			wantError, saved := "--acknowledge-license-risk", false
			switch name {
			case "dry-run":
				args, wantError = append(args, "--dry-run"), ""
			case "declined":
				app.Stdin = terminalPromptReader{Reader: strings.NewReader("no\n")}
				wantError = "acknowledgment declined"
			case "forced-noninteractive":
				app.Stdin = terminalPromptReader{Reader: strings.NewReader("yes\n")}
				args = append(args, "--non-interactive")
			case "approved-terminal":
				app.Stdin = terminalPromptReader{Reader: strings.NewReader("yes\n")}
				wantError, saved = "content tools image not found", true
			case "approved-flag":
				args = append(args, "--non-interactive", "--acknowledge-license-risk")
				wantError, saved = "content tools image not found", true
			case "late-collision":
				// The destination changes after CLI preflight while approval is
				// pending. Install must revalidate before publishing the pack.
				wantError = "unexpected managed content"
				app.Stdin = terminalPromptReader{Reader: &contentApprovalReader{
					Reader: strings.NewReader("yes\n"),
					beforeRead: func() {
						path := content.ArtifactPath(root, catalog.Artifact{Target: "llama-models", Destination: "imported/model.gguf"})
						if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
							t.Error(err)
							return
						}
						if err := os.WriteFile(path, []byte("user bytes of another size"), 0o600); err != nil {
							t.Error(err)
						}
					},
				}}
			case "pack-collision":
				if err := os.MkdirAll(filepath.Dir(pack), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(pack, []byte("user pack"), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--acknowledge-license-risk")
				wantError = "import pack exists with different content"
			}
			if name == "installed-needs-risk" || name == "collision" || name == "dry-run-collision" {
				path := content.ArtifactPath(root, catalog.Artifact{Target: "llama-models", Destination: "imported/model.gguf"})
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				payload := contents
				if name != "installed-needs-risk" {
					payload, wantError = []byte("user bytes of another size"), "unexpected managed content"
				}
				if err := os.WriteFile(path, payload, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if name == "dry-run-collision" {
				args = append(args, "--dry-run")
			}
			err := app.contentImport(args)
			if wantError == "" && err != nil || wantError != "" && (err == nil || !strings.Contains(err.Error(), wantError)) {
				t.Fatalf("import = %v, want %q; output:\n%s", err, wantError, stdout)
			}
			if saved {
				packBytes, err := os.ReadFile(pack)
				if err != nil || !json.Valid(packBytes) || strings.Count(stdout.String(), "Saved local pack:") != 1 {
					t.Fatalf("approved import did not preserve one valid pack: %q, %v", packBytes, err)
				}
			} else {
				if len(runner.commands) != 0 || strings.Contains(stdout.String(), "Saved local pack:") {
					t.Fatalf("rejected/dry import mutated: %v, %q", runner.commands, stdout)
				}
				if name == "pack-collision" {
					if got, err := os.ReadFile(pack); err != nil || string(got) != "user pack" {
						t.Fatalf("existing pack changed: %q, %v", got, err)
					}
				} else if _, err := os.Lstat(filepath.Dir(pack)); !os.IsNotExist(err) {
					t.Fatalf("rejected/dry import created pack directory: %v", err)
				}
				if name != "installed-needs-risk" && name != "collision" && name != "dry-run-collision" && name != "late-collision" {
					if _, err := os.Lstat(root); !os.IsNotExist(err) {
						t.Fatalf("rejected/dry import created data root: %v", err)
					}
				}
			}
			if name == "approved-terminal" || name == "declined" {
				if !strings.Contains(stdout.String(), "Acknowledge the unresolved licensing") {
					t.Fatalf("terminal import skipped ordinary approval: %q", stdout)
				}
			}
		})
	}
}

func TestInstallWorkflowPreservesAndReplacesUserChangesExplicitly(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	identifiers := make([]string, 0, len(managed.Workflows))
	for identifier := range managed.Workflows {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	workflow := managed.Workflows[identifiers[0]]
	dataRoot := t.TempDir()
	var output bytes.Buffer
	app := App{Root: root, Stdout: &output}

	if err := app.installWorkflow(workflow, dataRoot, false); err != nil {
		t.Fatal(err)
	}
	if !workflowReady(dataRoot, workflow) {
		t.Fatal("freshly installed workflow is not ready")
	}
	destination := workflowPath(dataRoot, workflow)
	if err := os.WriteFile(destination, []byte("user modification\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.installWorkflow(workflow, dataRoot, false); err == nil {
		t.Fatal("differing user workflow was replaced without --force")
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "user modification\n" {
		t.Fatalf("user workflow changed after refused install: %q", contents)
	}
	if err := app.installWorkflow(workflow, dataRoot, true); err != nil {
		t.Fatal(err)
	}
	if !workflowReady(dataRoot, workflow) {
		t.Fatal("forced workflow replacement is not ready")
	}
}
