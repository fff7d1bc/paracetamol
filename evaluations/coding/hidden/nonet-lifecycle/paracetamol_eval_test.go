package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const paracetamolParentDeathHelper = "PARACETAMOL_PARENT_DEATH_HELPER"

func TestParacetamolSIGTERMIsRelayedToTarget(t *testing.T) {
	process, err := os.StartProcess("/bin/sleep", []string{"sleep", "60"}, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	pid := process.Pid
	if err := process.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	defer func() {
		if pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}()

	term := make(chan os.Signal, 1)
	term <- syscall.SIGTERM
	status, err := waitForTargetSignal(&spawnedChild{pid: pid}, term)
	if err != nil {
		t.Fatalf("waitForTargetSignal: %v", err)
	}
	if !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("status = %v, want SIGTERM", status)
	}
	pid = -1
}

func TestParacetamolParentDeathTerminatesTarget(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "target.pid")
	command := exec.Command(
		os.Args[0], "-test.run=^TestParacetamolParentDeathHelper$",
	)
	command.Env = append(
		os.Environ(),
		paracetamolParentDeathHelper+"=1",
		"PARACETAMOL_TARGET_PID_FILE="+pidFile,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("parent-death helper failed: %v\n%s", err, output)
	}

	contents, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("ReadFile target pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || pid <= 0 {
		t.Fatalf("target pid = %q: %v", contents, err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if errors.Is(stateErr, os.ErrNotExist) {
			return
		}
		fields := strings.Fields(string(state))
		if stateErr == nil && len(fields) > 2 && fields[2] == "Z" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("target remained alive after its nonet parent exited")
}

func TestParacetamolParentDeathHelper(t *testing.T) {
	if os.Getenv(paracetamolParentDeathHelper) != "1" {
		return
	}
	pidFile := os.Getenv("PARACETAMOL_TARGET_PID_FILE")
	setupReader, setupWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("setup pipe: %v", err)
	}
	syncReader, syncWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("sync pipe: %v", err)
	}
	syscall.CloseOnExec(int(setupWriter.Fd()))

	child, err := spawnInUserNamespace(
		[]string{
			"/bin/sh",
			"-c",
			"printf '%s\\n' \"$$\" > \"$1\"; exec /bin/sleep 60",
			"sh",
			pidFile,
		},
		os.Environ(),
		int(syncReader.Fd()),
		-1,
		int(setupWriter.Fd()),
		"",
	)
	if err != nil {
		t.Fatalf("spawnInUserNamespace: %v", err)
	}
	syncReader.Close()
	setupWriter.Close()
	if err := installIdentityMappings(child.pid, os.Getuid(), os.Getgid()); err != nil {
		child.kill()
		t.Fatalf("installIdentityMappings: %v", err)
	}
	if _, err := syncWriter.Write([]byte{1}); err != nil {
		child.kill()
		t.Fatalf("release child: %v", err)
	}
	syncWriter.Close()
	status, err := readChildSetupStatus(setupReader)
	if err != nil || !status.targetStarted {
		child.kill()
		t.Fatalf("target startup status = %+v, err = %v", status, err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); err == nil {
			os.Exit(0)
		}
		time.Sleep(5 * time.Millisecond)
	}
	child.kill()
	t.Fatal("target did not report its pid")
}
