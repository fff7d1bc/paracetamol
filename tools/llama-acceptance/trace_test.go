package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
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

func TestDocumentedTraceExportsArePrivateAndCreateOnce(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "tools/llama-acceptance/trace.md"))
	if err != nil {
		t.Fatal(err)
	}
	block := func(name string) string {
		t.Helper()
		marker := "<!-- trace-export-" + name + " -->\n```bash\n"
		if strings.Count(string(doc), marker) != 1 {
			t.Fatalf("expected one documented %s block", name)
		}
		_, rest, _ := strings.Cut(string(doc), marker)
		command, _, ok := strings.Cut(rest, "\n```")
		if !ok {
			t.Fatalf("unterminated documented %s block", name)
		}
		return command
	}
	create, snapshot := block("create"), block("snapshot")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	mkdir, err := exec.LookPath("mkdir")
	if err != nil {
		t.Fatal(err)
	}
	fixture := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		for _, path := range []string{dir, filepath.Join(dir, "build"), filepath.Join(dir, "bin")} {
			if path != dir {
				if err := os.Mkdir(path, 0o777); err != nil {
					t.Fatal(err)
				}
			}
			// The test must not inherit privacy from t.TempDir or its parent.
			if err := os.Chmod(path, 0o777); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(mkdir, filepath.Join(dir, "bin/mkdir")); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	run := func(dir, command, exportDir string) ([]byte, error) {
		// Only mkdir is executable through PATH. Podman is a shell fixture;
		// no container, inference client or real evidence is used.
		mock := `umask 000
podman() {
  case "$*" in
    'top fixture-container pid hpid args') printf '%s\n' synthetic-process ;;
    'logs fixture-container') printf '%s\n' synthetic-log; printf '%s\n' synthetic-stderr >&2 ;;
    *) return 127 ;;
  esac
}
`
		cmd := exec.Command(bash, "--noprofile", "--norc", "-c", mock+command)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + filepath.Join(dir, "bin"), "CONTAINER_ID=fixture-container", "TRACE_EXPORT_DIR=" + exportDir}
		return cmd.CombinedOutput()
	}
	dir := fixture(t)
	if output, err := run(dir, create, ""); err != nil {
		t.Fatalf("create: %v: %s", err, output)
	}
	for _, arm := range []string{"normal", "control"} {
		exportDir := "build/trace-" + arm + "-export"
		if output, err := run(dir, snapshot, exportDir); err != nil {
			t.Fatalf("%s snapshot: %v: %s", arm, err, output)
		}
		for name, want := range map[string]string{"processes.txt": "synthetic-process\n", "backend.log": "synthetic-log\nsynthetic-stderr\n"} {
			path := filepath.Join(dir, exportDir, name)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != want {
				t.Fatalf("%s: %q: %v", path, data, err)
			}
		}
		if err := filepath.WalkDir(filepath.Join(dir, exportDir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err == nil && info.Mode().Perm()&0o077 != 0 {
				t.Errorf("group/other access to %s: %s", path, info.Mode())
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := run(dir, snapshot, exportDir); err == nil {
			t.Fatalf("%s snapshot overwritten", arm)
		}
		t.Run("existing-"+arm, func(t *testing.T) {
			dir := fixture(t)
			path := filepath.Join(dir, exportDir)
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := run(dir, create, ""); err == nil {
				t.Fatal("existing export directory accepted")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("existing directory modified: %v: %v", info, err)
			}
		})
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
	// A private inherited pipe bounds fixture cleanup even if a failed test
	// leaves the child blocked in a FIFO. No PID lookup or later PID reuse can
	// redirect cleanup to a different process.
	go func() {
		lifetime := os.NewFile(3, "mock-lifetime")
		var permit [1]byte
		_, _ = lifetime.Read(permit[:])
		os.Exit(9)
	}()
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	if err := os.WriteFile(os.Getenv("PARACETAMOL_TRACE_READY"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		os.Exit(9)
	}
	if code := os.Getenv("PARACETAMOL_TRACE_EXIT"); code != "" {
		status, err := strconv.Atoi(code)
		if err != nil {
			os.Exit(9)
		}
		os.Exit(status)
	}
	if os.Getenv("PARACETAMOL_TRACE_SELF_SIGNAL") == "1" {
		signal.Reset(syscall.SIGTERM)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {}
	}
	count := 1
	if os.Getenv("PARACETAMOL_TRACE_REPEAT") == "1" {
		count = 3
	}
	var flushed string
	for range count {
		select {
		case got := <-ch:
			flushed += got.String() + "\n"
		case <-time.After(10 * time.Second):
			os.Exit(9)
		}
		if err := os.WriteFile(os.Getenv("PARACETAMOL_TRACE_FLUSHED"), []byte(flushed), 0o600); err != nil {
			os.Exit(9)
		}
	}
	if gate := os.Getenv("PARACETAMOL_TRACE_CHILD_GATE"); gate != "" {
		f, err := os.Open(gate)
		if err != nil {
			os.Exit(9)
		}
		var permit [1]byte
		if _, err := f.Read(permit[:]); err != nil {
			os.Exit(9)
		}
		_ = f.Close()
	}
	os.Exit(7)
}

type traceWrapperFixture struct {
	root    string
	command *exec.Cmd
	output  bytes.Buffer
	done    chan error
}

func newTraceWrapperFixture(t *testing.T, environment ...string) *traceWrapperFixture {
	t.Helper()
	// Exercise shell substitutions independently of the checkout/temp parent.
	root := filepath.Join(t.TempDir(), "trace fixture with spaces")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	f := &traceWrapperFixture{root: root, done: make(chan error, 1)}
	profiler, server, wrapper := filepath.Join(f.root, "profiler"), filepath.Join(f.root, "server"), filepath.Join(f.root, "wrapper")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Mirrors pinned Linux rocprofv3 single-pass exec semantics. No profiler,
	// container, GPU or path outside the test's temporary directory is used.
	fake := "#!/bin/bash\nif [[ $1 == --version ]]; then echo mock; exit; fi\nwhile [[ $1 != -- ]]; do shift; done\nshift\nexec \"$@\"\n"
	quote := func(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'" }
	script := strings.NewReplacer("/opt/venv/bin/rocprofv3", quote(profiler), "/opt/trace/llama-server", quote(server), "/data/acceptance-trace-v1.XXXXXXXX", quote(filepath.Join(f.root, "trace.XXXXXXXX"))).Replace(traceEntrypoint)
	for path, data := range map[string]string{
		profiler: fake,
		server:   "#!/bin/bash\nexec " + quote(executable) + " -test.run=^TestTraceMockServerProcess$\n",
		wrapper:  script,
	} {
		if err := os.WriteFile(path, []byte(data), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.command = exec.Command("bash", wrapper)
	f.command.Env = append(os.Environ(), "BASH_ENV=", "PARACETAMOL_TRACE_MOCK=1", "PARACETAMOL_TRACE_READY="+filepath.Join(f.root, "ready"), "PARACETAMOL_TRACE_FLUSHED="+filepath.Join(f.root, "flushed"))
	f.command.Env = append(f.command.Env, environment...)
	f.command.Stdout, f.command.Stderr = &f.output, &f.output
	return f
}

func (f *traceWrapperFixture) start(t *testing.T) {
	t.Helper()
	lifetime, keepalive, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = keepalive.Close() })
	f.command.ExtraFiles = []*os.File{lifetime}
	if err := f.command.Start(); err != nil {
		_ = lifetime.Close()
		t.Fatal(err)
	}
	_ = lifetime.Close()
	go func() { f.done <- f.command.Wait() }()
	t.Cleanup(func() {
		_ = f.command.Process.Kill()
	})
	data := f.await(t, "ready", func(data []byte, err error) bool { return err == nil && len(data) != 0 })
	if _, err := strconv.Atoi(string(data)); err != nil {
		t.Fatal(err)
	}
}

func (f *traceWrapperFixture) await(t *testing.T, path string, ready func([]byte, error) bool) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	if !filepath.IsAbs(path) {
		path = filepath.Join(f.root, path)
	}
	for {
		data, err := os.ReadFile(path)
		if ready(data, err) {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out awaiting %s", path)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *traceWrapperFixture) finish(t *testing.T, status int) {
	t.Helper()
	select {
	case err := <-f.done:
		if f.command.ProcessState.ExitCode() != status || (err == nil) != (status == 0) {
			t.Fatalf("exit=%d want=%d: %v: %s", f.command.ProcessState.ExitCode(), status, err, f.output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wrapper did not finish/wait for flush")
	}
	files, err := filepath.Glob(filepath.Join(f.root, "trace.*", "profiler-exit-status"))
	if err != nil || len(files) != 1 {
		t.Fatalf("missing exit status: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil || string(data) != strconv.Itoa(status)+"\n" {
		t.Fatalf("lost child exit status: %q %v", data, err)
	}
}

func TestTraceWrapperPreservesTerminalChildStatuses(t *testing.T) {
	for _, status := range []int{0, 7, 130, 143} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			f := newTraceWrapperFixture(t, "PARACETAMOL_TRACE_EXIT="+strconv.Itoa(status))
			f.start(t)
			f.finish(t, status)
		})
	}
	t.Run("signaled-child", func(t *testing.T) {
		f := newTraceWrapperFixture(t, "PARACETAMOL_TRACE_SELF_SIGNAL=1")
		f.start(t)
		f.finish(t, 143)
	})
}

func TestTraceWrapperForwardsNativeStopAndRetainsFlushStatus(t *testing.T) {
	for _, stop := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(stop.String(), func(t *testing.T) {
			f := newTraceWrapperFixture(t)
			f.start(t)
			if err := f.command.Process.Signal(stop); err != nil {
				t.Fatal(err)
			}
			f.finish(t, 7)
			data, err := os.ReadFile(filepath.Join(f.root, "flushed"))
			if err != nil || string(data) != stop.String()+"\n" {
				t.Fatalf("signal/flush=%q %v", data, err)
			}
		})
	}
}

func TestTraceWrapperRetriesAfterChildExitsDuringInterruptedWait(t *testing.T) {
	for _, stop := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(stop.String(), func(t *testing.T) {
			f := newTraceWrapperFixture(t)
			childGate, hookGate := filepath.Join(f.root, "child-gate"), filepath.Join(f.root, "hook-gate")
			for _, gate := range []string{childGate, hookGate} {
				if err := syscall.Mkfifo(gate, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Pause at the first command after wait records 128+signal. The child
			// cannot exit until this barrier is reached. Then release the child,
			// prove Bash has reaped it, and only then resume the wrapper. No
			// liveness/status expression or trap handler is replaced by the hook.
			hook := `trap 'if [[ ${status-} == "$PARACETAMOL_TRACE_INTERRUPTED_STATUS" && ${hooked-} != 1 ]]; then hooked=1; printf "ready\n" > "$PARACETAMOL_TRACE_HOOK_READY"; read -r permit < "$PARACETAMOL_TRACE_HOOK_GATE"; fi' DEBUG
`
			hookPath := filepath.Join(f.root, "bashenv")
			if err := os.WriteFile(hookPath, []byte(hook), 0o600); err != nil {
				t.Fatal(err)
			}
			f.command.Env = append(f.command.Env, "BASH_ENV="+hookPath, "PARACETAMOL_TRACE_CHILD_GATE="+childGate, "PARACETAMOL_TRACE_HOOK_GATE="+hookGate, "PARACETAMOL_TRACE_HOOK_READY="+filepath.Join(f.root, "hook-ready"), "PARACETAMOL_TRACE_INTERRUPTED_STATUS="+strconv.Itoa(128+int(stop)))
			f.start(t)
			if err := f.command.Process.Signal(stop); err != nil {
				t.Fatal(err)
			}
			f.await(t, "hook-ready", func(data []byte, err error) bool { return err == nil && string(data) == "ready\n" })
			// O_RDWR keeps opening either fixture FIFO bounded even on failure.
			for _, gate := range []string{childGate, hookGate} {
				file, err := os.OpenFile(gate, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = file.Close() })
				if gate == hookGate {
					pidData, err := os.ReadFile(filepath.Join(f.root, "ready"))
					if err != nil {
						t.Fatal(err)
					}
					f.await(t, filepath.Join("/proc", string(pidData), "stat"), func(_ []byte, err error) bool { return os.IsNotExist(err) })
				}
				if _, err := file.WriteString("continue\n"); err != nil {
					t.Fatal(err)
				}
			}
			f.finish(t, 7)
			data, err := os.ReadFile(filepath.Join(f.root, "flushed"))
			if err != nil || string(data) != stop.String()+"\n" {
				t.Fatalf("signal/flush=%q %v", data, err)
			}
		})
	}
}

func TestTraceWrapperForwardsRepeatedStopsWhileFlushing(t *testing.T) {
	f := newTraceWrapperFixture(t, "PARACETAMOL_TRACE_REPEAT=1")
	f.start(t)
	var want string
	for _, stop := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if err := f.command.Process.Signal(stop); err != nil {
			t.Fatal(err)
		}
		want += stop.String() + "\n"
		f.await(t, "flushed", func(data []byte, err error) bool { return err == nil && string(data) == want })
	}
	f.finish(t, 7)
}
