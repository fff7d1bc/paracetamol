package storage

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
	"syscall"
	"testing"
	"time"
)

func TestMutationLockUsesDirectoryIdentity(t *testing.T) {
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
	lock, err := AcquireMutationLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for _, path := range []string{root, alias, filepath.Join(parentAlias, "data")} {
		contender, err := AcquireMutationLock(path)
		if contender != nil {
			contender.Close()
		}
		if !errors.Is(err, ErrMutationBusy) {
			t.Fatalf("same-directory lock through %s = %v", path, err)
		}
	}
	independent, err := AcquireMutationLock(other)
	if err != nil {
		t.Fatalf("different data directory blocked: %v", err)
	}
	independent.Close()
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, lock.file.Fd(), syscall.F_GETFD, 0)
	if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
		t.Fatalf("lock descriptor is not close-on-exec: flags=%d, error=%v", flags, errno)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("locking created persistent files: %v, %v", entries, err)
	}
}

func TestMutationLockDoesNotPrepareMissingRootOrAcceptFile(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "missing", "data")
	if lock, err := AcquireMutationLock(missing); err == nil {
		lock.Close()
		t.Fatal("missing root acquired mutation ownership")
	}
	if _, err := os.Lstat(filepath.Join(base, "missing")); !os.IsNotExist(err) {
		t.Fatalf("lock acquisition created persistent state: %v", err)
	}
	path := filepath.Join(base, "file")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireMutationLock(path); err == nil {
		lock.Close()
		t.Fatal("non-directory acquired mutation ownership")
	}
	if actual, err := os.ReadFile(path); err != nil || string(actual) != "keep" {
		t.Fatalf("failed lock acquisition changed file: %q, %v", actual, err)
	}
}

func TestMutationLockProcessHelper(t *testing.T) {
	root := os.Getenv("PARACETAMOL_MUTATION_LOCK_TEST_ROOT")
	if root == "" {
		return
	}
	lock, err := AcquireMutationLock(root)
	if err != nil {
		t.Fatal(err)
	}
	// Intentionally omit Close: process exit must release it.
	fmt.Println("locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
	runtime.KeepAlive(lock)
	os.Exit(0)
}

func TestMutationLockReleasedOnProcessExit(t *testing.T) {
	for _, scenario := range []string{"normal", "killed"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMutationLockProcessHelper$")
			command.Env = append(os.Environ(), "PARACETAMOL_MUTATION_LOCK_TEST_ROOT="+root)
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
			lock, err := AcquireMutationLock(root)
			if lock != nil {
				lock.Close()
			}
			if !errors.Is(err, ErrMutationBusy) {
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
			lock, err = AcquireMutationLock(root)
			if err != nil {
				t.Fatalf("exited process retained lock: %v", err)
			}
			lock.Close()
		})
	}
}
