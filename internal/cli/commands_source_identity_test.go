package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/process"
)

func TestProjectSourceIdentityRequiresMatchingGitWorktree(t *testing.T) {
	parent := t.TempDir()
	run := func(root string, arguments ...string) process.Result {
		t.Helper()
		result, err := (process.OSRunner{}).Run(t.Context(), process.Command{
			Name: "git", Args: append([]string{"-C", root}, arguments...),
			Env: sourceIdentityGitEnvironment(parent),
		})
		if err != nil || result.Status != 0 {
			t.Fatalf("git %v failed: %v status=%d stderr=%s", arguments, err, result.Status, result.Stderr)
		}
		return result
	}
	checkout := filepath.Join(parent, "checkout")
	if err := os.Mkdir(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	run(checkout, "init", "--quiet")
	tracked := filepath.Join(checkout, "main.go")
	if err := os.WriteFile(tracked, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(checkout, "add", "main.go")
	run(checkout, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "--no-gpg-sign", "-m", "fixture")
	revision := strings.TrimSpace(string(run(checkout, "rev-parse", "HEAD").Stdout))
	linked := filepath.Join(parent, "linked-worktree")
	run(checkout, "worktree", "add", "--quiet", "--detach", linked)
	symlink := filepath.Join(parent, "checkout-link")
	if err := os.Symlink(checkout, symlink); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{checkout, linked, symlink} {
		t.Run(filepath.Base(root), func(t *testing.T) {
			app, _, _ := testApp(t, sourceIdentityOSRunner{parent})
			app.Root = root
			got, err := app.projectSourceIdentity()
			if err != nil || got != revision {
				t.Fatalf("matching checkout identity=%q want=%q err=%v", got, revision, err)
			}
			if got := app.projectRevision(); got != revision {
				t.Fatalf("matching checkout revision=%q want=%q", got, revision)
			}
		})
	}
	if err := os.WriteFile(tracked, []byte("package different\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, _ := testApp(t, sourceIdentityOSRunner{parent})
	app.Root = checkout
	if got, err := app.projectSourceIdentity(); err != nil || !strings.HasPrefix(got, revision+"+dirty.") {
		t.Fatalf("tracked dirty identity=%q err=%v", got, err)
	}
	for name, root := range map[string]string{
		"standalone copy": filepath.Join(parent, "execution-copy"),
		"ancestor repo":   filepath.Join(checkout, "execution-copy"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			app, _, _ := testApp(t, sourceIdentityOSRunner{parent})
			app.Root = root
			if got, err := app.projectSourceIdentity(); err == nil || got != "" || !strings.Contains(err.Error(), "Git checkout") {
				t.Fatalf("Git-free copy accepted: identity=%q err=%v", got, err)
			}
			if got := app.projectRevision(); got != "unavailable" {
				t.Fatalf("Git-free copy borrowed another revision: %q", got)
			}
		})
	}
}

type sourceIdentityOSRunner struct{ ceiling string }

func (runner sourceIdentityOSRunner) LookPath(name string) (string, error) {
	return (process.OSRunner{}).LookPath(name)
}

func (runner sourceIdentityOSRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	command.Env = sourceIdentityGitEnvironment(runner.ceiling)
	return (process.OSRunner{}).Run(ctx, command)
}

func sourceIdentityGitEnvironment(ceiling string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + ceiling,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CEILING_DIRECTORIES=" + ceiling}
}

func TestIdentityBoundCommandsRejectGitFreeCopyBeforePreparation(t *testing.T) {
	managed := catalog.Catalog{LlamaPresets: map[string]catalog.LlamaPreset{
		"resident-dflash-fixture": {ID: "resident-dflash-fixture", DefaultContext: 262144,
			SpeculativeType: "draft-dflash", DraftTokens: 3, AgentTools: true,
			DraftArtifact: "draft-fixture", ModelLoad: map[string]string{"strix-halo": catalog.LlamaModelLoadResident}},
	}}
	for name, arguments := range map[string][]string{
		"acceptance prepare": {"acceptance", "--prepare", "--non-interactive"},
		"speculative sweep":  {"benchmark", "llama-cpp", "speculative", "--preset", "resident-dflash-fixture"},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry-run=%t", name, dryRun), func(t *testing.T) {
				root := t.TempDir()
				runner := &commandRunner{run: func(command process.Command) process.Result {
					if command.Name != "git" {
						t.Errorf("identity failure reached external host/workload command: %v", command)
						return process.Result{Status: 1}
					}
					// Use actual Git, but no inherited repository overrides or user
					// configuration. A copied execution tree has no Git metadata.
					command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root,
						"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
					result, err := (process.OSRunner{}).Run(t.Context(), command)
					if err != nil {
						t.Fatal(err)
					}
					return result
				}}
				app, stdout, stderr := testApp(t, runner)
				app.Root, app.catalog = root, &managed
				args := append([]string(nil), arguments...)
				args = append(args, "--profile", "strix-halo", "--render-node", "/dev/dri/renderD999999",
					"--data-dir", filepath.Join(root, "data"), "--output", filepath.Join(root, "result.json"))
				if dryRun {
					args = append(args, "--dry-run")
				}
				err := app.Dispatch(args)
				if err == nil || !strings.Contains(err.Error(), "project") {
					t.Fatalf("missing early source-identity failure: %v", err)
				}
				if len(runner.commands) == 0 || stdout.Len() != 0 || stderr.Len() != 0 {
					t.Fatalf("unexpected planning/execution: commands=%v stdout=%q stderr=%q", runner.commands, stdout.String(), stderr.String())
				}
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("identity failure created data/checkpoint: entries=%v err=%v", entries, err)
				}
			})
		}
	}
}
