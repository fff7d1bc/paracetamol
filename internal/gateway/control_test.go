package gateway

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func controlEnvironment(t *testing.T) map[string]string {
	t.Helper()
	// Unix socket paths are short on Linux; Go's test-name temp directories
	// under GOTMPDIR are long enough to exceed sockaddr_un.sun_path. Keep the
	// prefix short too: the HOME suffix must fit in hosted CI checkout paths.
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
		environment := map[string]string{"HOME": home, "XDG_RUNTIME_DIR": t.TempDir()}
		if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "HOME must be an absolute path") {
			t.Fatalf("home=%q start error=%v", home, err)
		}
		if _, err := StopLocalGateway(context.Background(), environment); err == nil || !strings.Contains(err.Error(), "HOME must be an absolute path") {
			t.Fatalf("home=%q stop error=%v", home, err)
		}
	}
}

func TestLocalControlRejectsSecondGatewayAndRecoversStaleSocket(t *testing.T) {
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
	path, err := localControlPath(environment)
	if err != nil {
		t.Fatal(err)
	}
	// A crashed process can leave a socket inode, but not its flock.
	// UnixListener with unlink disabled simulates an interrupted owner.
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	present, err := StopLocalGateway(context.Background(), environment)
	if err != nil || present {
		t.Fatalf("stale control socket: present=%t err=%v", present, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket was not removed: %v", err)
	}
	listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := StartLocalControl(environment)
	if err != nil {
		t.Fatal(err)
	}
	second.Finish(nil)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalControlRefusesForeignPathAndReportsShutdownFailure(t *testing.T) {
	environment := controlEnvironment(t)
	path, err := localControlPath(environment)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := StartLocalControl(environment); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("foreign control path error = %v", err)
	}
	if _, err := StopLocalGateway(context.Background(), environment); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("foreign stop path error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
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
