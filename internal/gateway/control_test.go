package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"paracetamol/internal/identity"
)

func controlEnvironment(t *testing.T) map[string]string {
	t.Helper()
	// Keep fake HOME state below the repository's ignored build tree. The
	// abstract control address does not impose a length limit on this path.
	build := filepath.Join("..", "..", "build")
	if err := os.MkdirAll(build, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(build, "gc-")
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(absolute) })
	return map[string]string{"HOME": absolute, "XDG_RUNTIME_DIR": filepath.Join(absolute, "runtime")}
}

func TestLocalControlOwnershipIgnoresRuntimeDirectory(t *testing.T) {
	environment := controlEnvironment(t)
	control, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	for _, runtimeRoot := range []string{"", filepath.Join(environment["HOME"], "another-runtime"), "relative-ignored"} {
		other := map[string]string{"HOME": environment["HOME"], "XDG_RUNTIME_DIR": runtimeRoot}
		if _, err := StartLocalControl(other); err == nil || !strings.Contains(err.Error(), "already running") {
			t.Fatalf("runtime root %q bypassed ownership: %v", runtimeRoot, err)
		}
	}
	control.Finish(nil)
}

func TestLocalControlStopsOnlyItsGatewayAfterDrain(t *testing.T) {
	environment := controlEnvironment(t)
	control, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		otherEnvironment := map[string]string{"HOME": environment["HOME"], "XDG_RUNTIME_DIR": filepath.Join(environment["HOME"], "other-runtime")}
		stopped, stopErr := StopLocalGateway(ctx, otherEnvironment)
		if !stopped && stopErr == nil {
			stopErr = errors.New("live gateway was reported absent")
		}
		result <- stopErr
	}()
	select {
	case <-control.Requested():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-result:
		t.Fatalf("stop returned before gateway cleanup: %v", err)
	default:
	}
	control.Finish(nil)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestLocalControlRequiresAbsoluteHome(t *testing.T) {
	for _, home := range []string{"", "relative-home"} {
		environment := map[string]string{"HOME": home, "XDG_RUNTIME_DIR": "ignored"}
		if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "HOME must be an absolute path") {
			t.Fatalf("home=%q start error=%v", home, err)
		}
		if _, err := StopLocalGateway(context.Background(), environment); err == nil || !strings.Contains(err.Error(), "HOME must be an absolute path") {
			t.Fatalf("home=%q stop error=%v", home, err)
		}
		if _, err := AcquireLocalOperation(environment, "cleanup"); err == nil || !strings.Contains(err.Error(), "HOME must be an absolute path") {
			t.Fatalf("home=%q operation error=%v", home, err)
		}
	}
}

func TestLocalControlRejectsSecondGatewayAndOldCloseCannotReleaseSuccessor(t *testing.T) {
	environment := controlEnvironment(t)
	first, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second gateway error = %v", err)
	}
	first.Finish(nil)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	present, err := StopLocalGateway(context.Background(), environment)
	if err != nil || present {
		t.Fatalf("closed control: present=%t err=%v", present, err)
	}
	second, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("old Close released its successor: %v", err)
	}
	second.Finish(nil)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalControlReportsShutdownFailure(t *testing.T) {
	environment := controlEnvironment(t)
	control, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, stopErr := StopLocalGateway(ctx, environment)
		result <- stopErr
	}()
	select {
	case <-control.Requested():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	control.Finish(errors.New("fixture cleanup failure"))
	if err := <-result; err == nil || !strings.Contains(err.Error(), "fixture cleanup failure") {
		t.Fatalf("shutdown error = %v", err)
	}
}

func TestStopLocalGatewayAbsentIsIdempotent(t *testing.T) {
	present, err := StopLocalGateway(context.Background(), controlEnvironment(t))
	if err != nil || present {
		t.Fatalf("present=%t err=%v", present, err)
	}
}

func TestLocalControlLongHomeAndProductIdentity(t *testing.T) {
	environment := controlEnvironment(t)
	// No filesystem path is created: HOME may be long or not yet exist.
	environment["HOME"] = filepath.Join(environment["HOME"], strings.Repeat("long-home/", 30))
	name, err := localControlPath(environment)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "@") || len(name) > 107 || strings.Contains(name, "long-home") {
		t.Fatalf("control name is not bounded, abstract and opaque: %q", name)
	}
	cleaned := map[string]string{"HOME": environment["HOME"] + "/../" + filepath.Base(environment["HOME"]) + "/."}
	if other, err := localControlPath(cleaned); err != nil || other != name {
		t.Fatalf("lexically equivalent HOME changed ownership: %q %v", other, err)
	}
	control, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	defer control.Finish(nil)
	if _, err := StartLocalControl(cleaned); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("cleaned long HOME bypassed ownership: %v", err)
	}
	if _, err := os.Lstat(environment["HOME"]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control created HOME state: %v", err)
	}
	// Product identity is process-wide and this test deliberately is not
	// parallel. A custom product must select its own kernel lease namespace.
	previous := identity.StateNamespace
	identity.StateNamespace = previous + "-control-test"
	t.Cleanup(func() { identity.StateNamespace = previous })
	otherName, err := localControlPath(environment)
	if err != nil || otherName == name {
		t.Fatalf("product namespace did not change ownership: %q %v", otherName, err)
	}
	other, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.Finish(nil)
}

func TestLocalControlIndependentHomesDoNotInterfere(t *testing.T) {
	firstEnvironment := controlEnvironment(t)
	secondEnvironment := controlEnvironment(t)
	first, err := StartLocalControl(firstEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer first.Finish(nil)
	second, err := AcquireLocalOperation(secondEnvironment, "direct launch")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	defer second.Finish(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if stopped, err := StopLocalGateway(ctx, secondEnvironment); stopped || err != nil {
		t.Fatalf("operation in independent HOME: stopped=%t err=%v", stopped, err)
	}
	select {
	case <-first.Requested():
		t.Fatal("other HOME stop reached first gateway")
	default:
	}
}

func TestLocalControlCannotBeReleasedByDeletingOrReplacingFilesystemState(t *testing.T) {
	environment := controlEnvironment(t)
	legacyRoot := filepath.Join(environment["HOME"], ".local", "share", identity.StateNamespace, "run")
	if err := os.MkdirAll(legacyRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	// A legacy socket path may be an unrelated file. Ownership no longer
	// inspects, replaces or removes it (nor the former lock inode).
	for _, name := range []string{"gateway.sock", "gateway.lock"} {
		if err := os.WriteFile(filepath.Join(legacyRoot, name), []byte("unrelated fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	control, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	defer control.Finish(nil)
	for _, name := range []string{"gateway.sock", "gateway.lock"} {
		if contents, err := os.ReadFile(filepath.Join(legacyRoot, name)); err != nil || string(contents) != "unrelated fixture" {
			t.Fatalf("legacy %s mutated: %q %v", name, contents, err)
		}
	}
	if err := os.RemoveAll(legacyRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("deleting old run directory released ownership: %v", err)
	}
	if err := os.MkdirAll(legacyRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("unrelated-target", filepath.Join(legacyRoot, "gateway.sock")); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLocalOperation(environment, "cleanup"); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("recreating old run directory released ownership: %v", err)
	}
	control.Finish(nil)
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(legacyRoot, "gateway.sock")); err != nil || target != "unrelated-target" {
		t.Fatalf("Close touched unrelated legacy path: %q %v", target, err)
	}
}

func TestLocalOperationExcludesGatewayAndOtherOperationsWithoutStopping(t *testing.T) {
	environment := controlEnvironment(t)
	operation, err := AcquireLocalOperation(environment, "direct launch")
	if err != nil {
		t.Fatal(err)
	}
	defer operation.Close()
	defer operation.Finish(nil)
	if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("gateway bypassed operation reservation: %v", err)
	}
	if _, err := AcquireLocalOperation(environment, "cleanup"); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("cleanup bypassed operation reservation: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Finish is intentionally not called until after the response. An
	// operation reservation must not make a stop wait for its entire lifetime.
	if stopped, err := StopLocalGateway(ctx, environment); stopped || err != nil {
		t.Fatalf("operation stop: stopped=%t err=%v", stopped, err)
	}
	select {
	case <-operation.Requested():
		t.Fatal("stop requested shutdown of non-gateway operation")
	default:
	}
	if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("stop released operation reservation: %v", err)
	}
	operation.Finish(nil)
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	control, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.Finish(nil)
}

func TestLocalOperationNameValidation(t *testing.T) {
	for _, name := range []string{"", "  ", "cleanup\nstop", "cleanup\rstop"} {
		if _, err := AcquireLocalOperation(controlEnvironment(t), name); err == nil {
			t.Fatalf("operation name %q accepted", name)
		}
	}
}

func TestLocalControlPeerCredentialsFailClosed(t *testing.T) {
	for _, credentials := range []*syscall.Ucred{nil, {Uid: 124}} {
		if err := validateLocalPeerUID(credentials, 123); err == nil {
			t.Fatalf("untrusted credentials accepted: %+v", credentials)
		}
	}
	if err := validateLocalPeerUID(&syscall.Ucred{Uid: 123}, 123); err != nil {
		t.Fatalf("matching UID rejected: %v", err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if err := verifyLocalPeer(left); err == nil {
		t.Fatal("non-Unix connection authenticated")
	}
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "closed-peer-fixture")
	connection, err := net.FileConn(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if err := verifyLocalPeer(connection); err == nil {
		t.Fatal("closed connection authenticated")
	}
}

func TestLocalControlProcessHelper(t *testing.T) {
	if os.Getenv("PARACETAMOL_CONTROL_PROCESS_HELPER") != "1" {
		return
	}
	if _, err := StartLocalControl(map[string]string{"HOME": os.Getenv("PARACETAMOL_CONTROL_PROCESS_HOME")}); err != nil {
		t.Fatal(err)
	}
	fmt.Println("lease-ready")
	select {}
}

func TestLocalControlCrashReleasesKernelLease(t *testing.T) {
	environment := controlEnvironment(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestLocalControlProcessHelper$")
	command.Env = append(os.Environ(), "PARACETAMOL_CONTROL_PROCESS_HELPER=1", "PARACETAMOL_CONTROL_PROCESS_HOME="+environment["HOME"])
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err == nil && line != "lease-ready\n" {
			err = fmt.Errorf("unexpected helper output: %q", line)
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not acquire its lease")
	}
	if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("separate process bypassed ownership: %v", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed helper exited successfully")
	}
	control, err := StartLocalControl(environment)
	if err != nil {
		t.Fatalf("crashed process retained kernel lease: %v", err)
	}
	defer control.Close()
	control.Finish(nil)
	entries, err := os.ReadDir(environment["HOME"])
	if err != nil || len(entries) != 0 {
		t.Fatalf("crashed ownership left filesystem state: %v %v", entries, err)
	}
}
