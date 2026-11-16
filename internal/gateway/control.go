package gateway

import (
	"bufio"
	"context"
	"crypto/sha256"
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

// LocalControl holds the same-HOME, product and UID lifecycle lease. Linux's
// abstract Unix namespace makes ownership independent of deletable files,
// XDG directories, application data and public ports. The kernel releases the
// lease when its listener closes, including after a process crash.
type LocalControl struct {
	listener   *net.UnixListener
	operation  string
	stop       chan struct{}
	done       chan struct{}
	acceptDone chan struct{}
	handlers   sync.WaitGroup
	mu         sync.Mutex
	finishing  bool
	finish     sync.Once
	close      sync.Once
	closeErr   error
	result     error
}

func localControlPath(environment map[string]string) (string, error) {
	home := environment["HOME"]
	if home == "" || !filepath.IsAbs(home) {
		return "", fmt.Errorf("HOME must be an absolute path for gateway ownership and control")
	}
	// Hash the namespace and lexical HOME identity rather than putting either
	// in sun_path. Long HOME paths and custom product names must remain safe;
	// resolving symlinks would instead make ownership depend on mutable files.
	digest := sha256.Sum256([]byte(identity.StateNamespace + "\x00" + filepath.Clean(home)))
	return fmt.Sprintf("@paracetamol-control-%d-%x", os.Getuid(), digest), nil
}

func StartLocalControl(environment map[string]string) (*LocalControl, error) {
	return startLocalControl(environment, "")
}

// AcquireLocalOperation reserves the gateway's lifecycle lease while a direct
// launch or cleanup crosses its ownership-sensitive critical section. Stop
// clients learn that this is not a gateway; they never request its shutdown.
// The caller must retain the returned control until the operation is finished.
func AcquireLocalOperation(environment map[string]string, operation string) (*LocalControl, error) {
	if strings.TrimSpace(operation) == "" || strings.ContainsAny(operation, "\r\n") {
		return nil, fmt.Errorf("local operation must have a non-empty single-line name")
	}
	return startLocalControl(environment, operation)
}

func startLocalControl(environment map[string]string, operation string) (*LocalControl, error) {
	path, err := localControlPath(environment)
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return nil, fmt.Errorf("gateway already running or resources in use by another local operation for this HOME; stop the gateway or wait for the operation to finish")
		}
		return nil, fmt.Errorf("acquire local lifecycle ownership: %w", err)
	}
	control := &LocalControl{listener: listener, operation: operation, stop: make(chan struct{}, 1), done: make(chan struct{}), acceptDone: make(chan struct{})}
	go control.accept()
	return control, nil
}

// Abstract sockets have no filesystem permissions. Authenticate both sides
// before any protocol I/O, and fail closed if peer credentials are unavailable.
func verifyLocalPeer(connection net.Conn) error {
	unix, ok := connection.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("local control connection is not a Unix socket")
	}
	raw, err := unix.SyscallConn()
	if err != nil {
		return fmt.Errorf("inspect local control peer: %w", err)
	}
	var credentials *syscall.Ucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return fmt.Errorf("inspect local control peer: %w", err)
	}
	if credentialErr != nil {
		return fmt.Errorf("inspect local control peer credentials: %w", credentialErr)
	}
	return validateLocalPeerUID(credentials, uint32(os.Getuid()))
}

func validateLocalPeerUID(credentials *syscall.Ucred, uid uint32) error {
	if credentials == nil || credentials.Uid != uid {
		return fmt.Errorf("local control peer is not owned by the current user")
	}
	return nil
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
	control.close.Do(func() {
		control.closeErr = control.listener.Close()
		<-control.acceptDone
	})
	return control.closeErr
}

func (control *LocalControl) accept() {
	defer close(control.acceptDone)
	for {
		connection, err := control.listener.AcceptUnix()
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
	if err := verifyLocalPeer(connection); err != nil {
		return
	}
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	command, err := bufio.NewReader(io.LimitReader(connection, 32)).ReadString('\n')
	if err != nil || command != "stop\n" {
		_, _ = io.WriteString(connection, "error invalid command\n")
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	if control.operation != "" {
		_, _ = io.WriteString(connection, "reserved\n")
		return
	}
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
// It reports false when no gateway owns the lease (including a non-gateway
// operation reservation), so callers may still stop directly run applications.
// It never guesses a PID, removes state or contacts the public HTTP endpoint.
func StopLocalGateway(ctx context.Context, environment map[string]string) (bool, error) {
	path, err := localControlPath(environment)
	if err != nil {
		return false, err
	}
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("connect to gateway control socket: %w", err)
	}
	defer connection.Close()
	if err := verifyLocalPeer(connection); err != nil {
		return false, err
	}
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
	if response == "reserved\n" {
		return false, nil
	}
	if response == "stopped\n" {
		return true, nil
	}
	if strings.HasPrefix(response, "error ") {
		return true, fmt.Errorf("gateway shutdown failed: %s", strings.TrimSpace(strings.TrimPrefix(response, "error ")))
	}
	return true, fmt.Errorf("unexpected gateway control reply")
}
