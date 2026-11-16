package content

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/process"
	"paracetamol/internal/verification"
)

func TestInstallLockUsesDirectoryIdentity(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "data")
	other := filepath.Join(base, "other")
	for _, path := range []string{root, other} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	parentAlias := filepath.Join(base, "parent-alias")
	if err := os.Symlink(base, parentAlias); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireInstallLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseInstallLock(lock)
	if err := Install(InstallOptions{DataRoot: root, DryRun: true, Runner: &mirrorPreflightRunner{}}); err != nil {
		t.Fatalf("dry run tried to acquire a held install lock: %v", err)
	}
	for _, path := range []string{root, alias, filepath.Join(parentAlias, "data")} {
		contender, err := acquireInstallLock(path)
		if contender != nil {
			releaseInstallLock(contender)
		}
		if err == nil || !strings.Contains(err.Error(), "another content installation is active") {
			t.Fatalf("same-directory lock through %s = %v", path, err)
		}
	}
	independent, err := acquireInstallLock(other)
	if err != nil {
		t.Fatalf("different data directory blocked: %v", err)
	}
	releaseInstallLock(independent)
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, lock.Fd(), syscall.F_GETFD, 0)
	if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
		t.Fatalf("lock descriptor is not close-on-exec: flags=%d, error=%v", flags, errno)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("locking created persistent files: %v, %v", entries, err)
	}
}

type installLockPauseWriter struct {
	entered chan struct{}
	release chan struct{}
	paused  bool
}

type installLockCountingRunner struct{ calls int }

func (runner *installLockCountingRunner) LookPath(name string) (string, error) {
	runner.calls++
	return (noSELinuxRunner{}).LookPath(name)
}

func (runner *installLockCountingRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	runner.calls++
	return (noSELinuxRunner{}).Run(ctx, command)
}

func (writer *installLockPauseWriter) Write(contents []byte) (int, error) {
	if !writer.paused {
		writer.paused = true
		close(writer.entered)
		<-writer.release
	}
	return len(contents), nil
}

func waitForInstallLockFixture(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("installer did not reach fixture pause")
	}
}

func TestInstallLockPreventsLostReceiptsAcrossEnvironments(t *testing.T) {
	for _, scenario := range []string{"different-runtime", "runtime-to-fallback", "different-fallback-temp"} {
		t.Run(scenario, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "data")
			firstEnvironment := map[string]string{"HOME": filepath.Join(base, "home-a"), "XDG_RUNTIME_DIR": filepath.Join(base, "runtime-a")}
			secondEnvironment := map[string]string{"HOME": filepath.Join(base, "home-b"), "XDG_RUNTIME_DIR": filepath.Join(base, "runtime-b")}
			if scenario != "different-runtime" {
				delete(secondEnvironment, "XDG_RUNTIME_DIR")
			}
			if scenario == "different-fallback-temp" {
				delete(firstEnvironment, "XDG_RUNTIME_DIR")
			}
			// os.TempDir reads the process environment, not InstallOptions.Environment.
			// Switch it only after the first installer has acquired its lock.
			t.Setenv("TMPDIR", filepath.Join(base, "temp-a"))
			var artifacts []catalog.Artifact
			for _, name := range []string{"first", "second"} {
				payload := []byte(name)
				artifact := testArtifact(payload)
				artifact.ID, artifact.Destination = name, name+".gguf"
				artifacts = append(artifacts, artifact)
				path := ArtifactPath(root, artifact)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, payload, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writer := &installLockPauseWriter{entered: make(chan struct{}), release: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				done <- Install(InstallOptions{DataRoot: root, Artifacts: artifacts[:1], Environment: firstEnvironment, Runner: noSELinuxRunner{}, Output: writer})
			}()
			finished := false
			// Always release and join the first installer, including a failed assertion.
			finish := func() {
				if finished {
					return
				}
				close(writer.release)
				if err := <-done; err != nil {
					t.Errorf("first installer: %v", err)
				}
				finished = true
			}
			defer finish()
			waitForInstallLockFixture(t, writer.entered) // receipt store has already been loaded
			t.Setenv("TMPDIR", filepath.Join(base, "temp-b"))
			runner := &installLockCountingRunner{}
			options := InstallOptions{DataRoot: root, Artifacts: artifacts[1:], Environment: secondEnvironment, Runner: runner}
			err := Install(options)
			if err == nil || !strings.Contains(err.Error(), "another content installation is active") {
				t.Fatalf("competing installer = %v", err)
			}
			if runner.calls != 0 {
				t.Fatalf("lock loser ran %d subprocess operations", runner.calls)
			}
			finish()
			options.Runner = noSELinuxRunner{}
			if err := Install(options); err != nil {
				t.Fatalf("retry after completion: %v", err)
			}
			store, err := verification.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(store.Records) != 2 {
				t.Fatalf("lost receipt records: got %d, want 2", len(store.Records))
			}
			plan, err := Plan(root, artifacts)
			if err != nil || plan.Ready != 2 {
				t.Fatalf("both installed artifacts are not ready: %+v, %v", plan, err)
			}
			for _, path := range []string{"runtime-a", "runtime-b", "temp-a", "temp-b"} {
				if _, err := os.Lstat(filepath.Join(base, path)); !os.IsNotExist(err) {
					t.Fatalf("installation created environment-dependent lock state %s: %v", path, err)
				}
			}
		})
	}
}

func TestInstallLockReleasedAfterFailureAndCancellation(t *testing.T) {
	for _, scenario := range []string{"failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			artifact := testArtifact([]byte("wrong"))
			path := ArtifactPath(root, artifact)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("other"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &installLockPauseWriter{entered: make(chan struct{}), release: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				done <- Install(InstallOptions{Context: ctx, DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Runner: noSELinuxRunner{}, Output: writer})
			}()
			waitForInstallLockFixture(t, writer.entered)
			if scenario == "cancelled" {
				cancel()
			}
			close(writer.release)
			err := <-done
			if err == nil || scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("Install = %v", err)
			}
			lock, err := acquireInstallLock(root)
			if err != nil {
				t.Fatalf("completed installer retained lock: %v", err)
			}
			releaseInstallLock(lock)
		})
	}
}

func TestInstallLockProcessHelper(t *testing.T) {
	root := os.Getenv("PARACETAMOL_INSTALL_LOCK_TEST_ROOT")
	if root == "" {
		return
	}
	lock, err := acquireInstallLock(root)
	if err != nil {
		t.Fatal(err)
	}
	// Intentionally omit releaseInstallLock: process exit must release it.
	fmt.Println("locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
	runtime.KeepAlive(lock)
	os.Exit(0)
}

func TestInstallLockReleasedOnProcessExit(t *testing.T) {
	for _, scenario := range []string{"normal", "killed"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInstallLockProcessHelper$")
			command.Env = append(os.Environ(), "PARACETAMOL_INSTALL_LOCK_TEST_ROOT="+root)
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			command.Stderr = os.Stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
			if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
				t.Fatalf("child lock startup: %q, %v", line, err)
			}
			lock, err := acquireInstallLock(root)
			if lock != nil {
				releaseInstallLock(lock)
			}
			if err == nil || !strings.Contains(err.Error(), "another content installation is active") {
				t.Fatalf("child-held lock did not exclude parent: %v", err)
			}
			if scenario == "killed" {
				if err := command.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else if err := stdin.Close(); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); scenario == "normal" && err != nil {
				t.Fatal(err)
			}
			lock, err = acquireInstallLock(root)
			if err != nil {
				t.Fatalf("exited process retained lock: %v", err)
			}
			releaseInstallLock(lock)
		})
	}
}
