package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"paracetamol/internal/identity"
)

// LocalControl gives the foreground gateway a same-user stop path. The lock
// prevents two gateways from claiming the same socket, including after a crash
// leaves the socket inode behind. The HTTP listener is never a stop endpoint.
type LocalControl struct {
	path       string
	listener   *net.UnixListener
	lock       *os.File
	stop       chan struct{}
	done       chan struct{}
	acceptDone chan struct{}
	handlers   sync.WaitGroup
	mu         sync.Mutex
	finishing  bool
	finish     sync.Once
	result     error
}

func localControlPath(environment map[string]string) (string, error) {
	root := environment["XDG_RUNTIME_DIR"]
	if root != "" {
		if !filepath.IsAbs(root) {
			return "", fmt.Errorf("XDG_RUNTIME_DIR must be an absolute path")
		}
		root = filepath.Join(root, identity.StateNamespace)
	} else {
		home := environment["HOME"]
		if home == "" || !filepath.IsAbs(home) {
			return "", fmt.Errorf("HOME must be an absolute path when XDG_RUNTIME_DIR is unset")
		}
		root = filepath.Join(home, ".local", "share", identity.StateNamespace, "run")
	}
	return filepath.Join(root, "gateway.sock"), nil
}

func StartLocalControl(environment map[string]string) (*LocalControl, error) {
	path, err := localControlPath(environment)
	if err != nil {
		return nil, err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create gateway control directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 || !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("gateway control directory must be a private directory owned by the current user: %s", directory)
	}
	lockPath := filepath.Join(directory, "gateway.lock")
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open gateway control lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	releaseLock := true
	defer func() {
		if releaseLock {
			_ = lock.Close()
		}
	}()
	lockInfo, statErr := lock.Stat()
	if statErr != nil {
		return nil, statErr
	}
	if !lockInfo.Mode().IsRegular() || lockInfo.Mode().Perm()&0o077 != 0 || !ownedByCurrentUser(lockInfo) {
		return nil, fmt.Errorf("gateway control lock must be a private regular file owned by the current user: %s", lockPath)
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("gateway already running for this user")
		}
		return nil, fmt.Errorf("lock gateway control: %w", err)
	}
	// Only the lock holder may replace a socket left by a crashed gateway.
	if old, statErr := os.Lstat(path); statErr == nil {
		if old.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(old) {
			return nil, fmt.Errorf("refusing non-owned or non-socket gateway control path: %s", path)
		}
		if err = os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale gateway control socket: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	listener, listenErr := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if listenErr != nil {
		return nil, fmt.Errorf("listen on gateway control socket: %w", listenErr)
	}
	listener.SetUnlinkOnClose(false)
	if err = os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	control := &LocalControl{path: path, listener: listener, lock: lock, stop: make(chan struct{}, 1), done: make(chan struct{}), acceptDone: make(chan struct{})}
	releaseLock = false
	go control.accept()
	return control, nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func (control *LocalControl) Requested() <-chan struct{} { return control.stop }

func (control *LocalControl) Finish(result error) {
	control.finish.Do(func() {
		control.mu.Lock()
		control.finishing = true
		control.result = result
		close(control.done)
		control.mu.Unlock()
		// main exits as soon as runGateway returns. Deliver the reply before
		// that happens, including when shutdown was requested by a stop client.
		control.handlers.Wait()
	})
}

func (control *LocalControl) Close() error {
	listenErr := control.listener.Close()
	<-control.acceptDone
	removeErr := os.Remove(control.path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	lockErr := control.lock.Close()
	return errors.Join(listenErr, removeErr, lockErr)
}

func (control *LocalControl) accept() {
	defer close(control.acceptDone)
	for {
		connection, err := control.listener.Accept()
		if err != nil {
			return
		}
		control.mu.Lock()
		if control.finishing {
			control.mu.Unlock()
			_ = connection.Close()
			continue
		}
		control.handlers.Add(1)
		control.mu.Unlock()
		go control.handle(connection)
	}
}

func (control *LocalControl) handle(connection net.Conn) {
	defer control.handlers.Done()
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	command, err := bufio.NewReader(io.LimitReader(connection, 32)).ReadString('\n')
	if err != nil || command != "stop\n" {
		_, _ = io.WriteString(connection, "error invalid command\n")
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	select {
	case control.stop <- struct{}{}:
	default:
	}
	<-control.done
	if control.result != nil {
		_, _ = io.WriteString(connection, "error "+control.result.Error()+"\n")
		return
	}
	_, _ = io.WriteString(connection, "stopped\n")
}

// StopLocalGateway waits for the gateway's existing graceful shutdown path.
// A missing socket means that no controllable gateway is present; it never
// guesses a PID or contacts the public HTTP endpoint.
func StopLocalGateway(ctx context.Context, environment map[string]string) (bool, error) {
	path, err := localControlPath(environment)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(info) {
		return false, fmt.Errorf("refusing non-owned or non-socket gateway control path: %s", path)
	}
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if errors.Is(err, syscall.ECONNREFUSED) {
			return clearStaleControl(path, info)
		}
		return false, fmt.Errorf("connect to gateway control socket: %w", err)
	}
	defer connection.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopCancel()
	if _, err := io.WriteString(connection, "stop\n"); err != nil {
		return false, fmt.Errorf("request gateway stop: %w", err)
	}
	response, err := bufio.NewReader(io.LimitReader(connection, 4096)).ReadString('\n')
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if err != nil {
		return true, fmt.Errorf("wait for gateway stop: %w", err)
	}
	if response == "stopped\n" {
		return true, nil
	}
	if strings.HasPrefix(response, "error ") {
		return true, fmt.Errorf("gateway shutdown failed: %s", strings.TrimSpace(strings.TrimPrefix(response, "error ")))
	}
	return true, fmt.Errorf("unexpected gateway control reply")
}

func clearStaleControl(path string, observed os.FileInfo) (bool, error) {
	lockPath := filepath.Join(filepath.Dir(path), "gateway.lock")
	fd, err := syscall.Open(lockPath, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false, fmt.Errorf("inspect stale gateway control lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	defer lock.Close()
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return false, fmt.Errorf("gateway control socket refused connection while gateway lock is held: %w", err)
	}
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !os.SameFile(observed, current) || current.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(current) {
		return false, fmt.Errorf("gateway control socket changed during stale-state inspection: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("remove stale gateway control socket: %w", err)
	}
	return false, nil
}
