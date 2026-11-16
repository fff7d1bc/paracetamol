package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"paracetamol/internal/project"
)

func TestTraceCurrentProductionContract(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	production, err := nativeReadInput(root, "Containerfile")
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"", "qsa-host-span"} {
		recipe, manifest, err := composeTraceRecipe(production, control)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(recipe, production) || manifest.ProductionSHA256 != hashBytes(production) || manifest.SourceCommit != controlCommit || manifest.BaseTree != controlBaseTree {
			t.Fatal("production recipe/source identity lost")
		}
	}
}

func TestTraceRecipeSourceIdentityAndShell(t *testing.T) {
	for _, control := range []string{"", "qsa-host-span"} {
		recipe, manifest, err := composeTraceRecipe([]byte(controlProductionFixture()), control)
		if err != nil {
			t.Fatal(err)
		}
		if manifest.Control != control || manifest.GeneratedSHA256 != hashBytes(recipe) || manifest.FragmentSHA256 != hashBytes([]byte(traceFragment)) || manifest.EntrypointSHA256 != hashBytes([]byte(traceEntrypoint)) {
			t.Fatal("lost trace input identity")
		}
		for _, required := range []string{"--target llama --verbose", "ACCEPTANCE_QSA_SPAN", "cache_k->ne[2]", "unaffected-installed.sha256", "cmp /opt/trace/evidence/unaffected-installed.sha256", "ARG ACCEPTANCE_TRACE_TREE", "test -x /opt/venv/bin/rocprofv3", "diagnostic-overhead", "trace-%pid%"} {
			if !bytes.Contains(recipe, []byte(required)) {
				t.Fatalf("missing %s", required)
			}
		}
		if strings.Contains(string(recipe), "cache_k->ne[2] <= 262140") != (control != "") {
			t.Fatal("wrong fixed mutation")
		}
		instructions, err := nativeInstructions(recipe)
		if err != nil {
			t.Fatal(err)
		}
		for _, instruction := range instructions {
			if instruction[0] != "RUN" {
				continue
			}
			command := exec.Command("bash", "-n")
			command.Stdin = strings.NewReader(instruction[1])
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("trace shell: %s: %v", output, err)
			}
		}
	}
}

func TestTraceRejectsUnknownSourceAndControl(t *testing.T) {
	for _, production := range []string{
		strings.ReplaceAll(controlProductionFixture(), controlCommit, strings.Repeat("f", 40)),
		strings.ReplaceAll(controlProductionFixture(), controlBaseTree, strings.Repeat("e", 40)),
		controlProductionFixture() + "FROM llama-cpp AS llama-acceptance-trace\n",
	} {
		if _, _, err := composeTraceRecipe([]byte(production), ""); err == nil {
			t.Fatal("unknown source accepted")
		}
	}
	if _, _, err := composeTraceRecipe([]byte(controlProductionFixture()), "mmq-sum"); err == nil {
		t.Fatal("unknown control accepted")
	}
}

func TestPrepareTracePrivateCreateOnceAndNoSubprocess(t *testing.T) {
	root := nativeFixtureRoot(t)
	if err := os.WriteFile(filepath.Join(root, "Containerfile"), []byte(controlProductionFixture()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	var out, stderr bytes.Buffer
	args := []string{"--out", "build/trace", "--control", "qsa-host-span"}
	if err := runPrepareTrace(context.Background(), args, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "build/trace"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("%v %v", entries, err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("not private")
		}
	}
	if err := runPrepareTrace(context.Background(), args, &out, &stderr); err == nil {
		t.Fatal("output reused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runPrepareTrace(ctx, []string{"--out", "build/cancelled"}, &out, &stderr); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := os.Stat(filepath.Join(root, "build/cancelled")); !os.IsNotExist(err) {
		t.Fatal("cancelled output created")
	}
}

func TestTraceSpanPatchIsOnlyObservation(t *testing.T) {
	// Apply the exact tracked hunk to its mechanically reconstructed context,
	// without a source checkout, compiler, Podman, model or GPU.
	var before strings.Builder
	for _, line := range strings.Split(traceSpanPatch, "\n") {
		if strings.HasPrefix(line, " ") {
			before.WriteString(strings.TrimPrefix(line, " ") + "\n")
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, "src/models/qwen4exp.cpp")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(before.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "apply", "--check", "-")
	command.Dir = root
	command.Stdin = strings.NewReader(traceSpanPatch)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("span observation patch: %s: %v", output, err)
	}
}

func TestTraceMockServerProcess(t *testing.T) {
	if os.Getenv("PARACETAMOL_TRACE_MOCK") != "1" {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	if err := os.WriteFile(os.Getenv("PARACETAMOL_TRACE_READY"), []byte("ready"), 0o600); err != nil {
		os.Exit(9)
	}
	var got os.Signal
	select {
	case got = <-ch:
	case <-time.After(10 * time.Second):
		os.Exit(9)
	}
	if err := os.WriteFile(os.Getenv("PARACETAMOL_TRACE_FLUSHED"), []byte(got.String()), 0o600); err != nil {
		os.Exit(9)
	}
	os.Exit(7)
}

func TestTraceWrapperForwardsNativeStopAndRetainsFlushStatus(t *testing.T) {
	for _, stop := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(stop.String(), func(t *testing.T) {
			root := t.TempDir()
			profiler := filepath.Join(root, "profiler")
			server := filepath.Join(root, "server")
			wrapper := filepath.Join(root, "wrapper")
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			// Mirrors pinned Linux rocprofv3 single-pass exec semantics. No
			// profiler/GPU is executed, and no host path outside t.TempDir is written.
			fake := "#!/bin/bash\nif [[ $1 == --version ]]; then echo mock; exit; fi\nwhile [[ $1 != -- ]]; do shift; done\nshift\nexec \"$@\"\n"
			if err := os.WriteFile(profiler, []byte(fake), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(server, []byte("#!/bin/bash\nexec '"+strings.ReplaceAll(executable, "'", "'\"'\"'")+"' -test.run=^TestTraceMockServerProcess$\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			script := strings.NewReplacer("/opt/venv/bin/rocprofv3", profiler, "/opt/trace/llama-server", server, "/data/acceptance-trace-v1.XXXXXXXX", filepath.Join(root, "trace.XXXXXXXX")).Replace(traceEntrypoint)
			if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			ready, flushed := filepath.Join(root, "ready"), filepath.Join(root, "flushed")
			command := exec.Command("bash", wrapper)
			command.Env = append(os.Environ(), "PARACETAMOL_TRACE_MOCK=1", "PARACETAMOL_TRACE_READY="+ready, "PARACETAMOL_TRACE_FLUSHED="+flushed)
			var output bytes.Buffer
			command.Stdout = &output
			command.Stderr = &output
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = command.Process.Kill() })
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mock server not ready")
				}
				time.Sleep(time.Millisecond)
			}
			if err := command.Process.Signal(stop); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			select {
			case err := <-done:
				if err == nil || command.ProcessState.ExitCode() != 7 {
					t.Fatalf("lost child status: %v %s", err, output.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("wrapper did not forward stop/wait for flush")
			}
			data, err := os.ReadFile(flushed)
			if err != nil || string(data) != stop.String() {
				t.Fatalf("signal/flush=%q %v", data, err)
			}
			files, err := filepath.Glob(filepath.Join(root, "trace.*", "profiler-exit-status"))
			if err != nil || len(files) != 1 {
				t.Fatalf("missing exit status: %v %v", files, err)
			}
			data, err = os.ReadFile(files[0])
			if err != nil || string(data) != "7\n" {
				t.Fatal("lost flushed exit status")
			}
		})
	}
}
