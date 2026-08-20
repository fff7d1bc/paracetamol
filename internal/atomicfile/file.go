// Package atomicfile publishes durable regular files without exposing partial
// contents. Callers choose explicitly between append-only creation and
// replacement of an owned checkpoint.
package atomicfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type Policy uint8

const (
	Create Policy = iota
	ReplaceRegular
)

func JSON(path string, value any, mode os.FileMode, policy Policy) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return Write(path, append(contents, '\n'), mode, policy)
}

// Ensure creates immutable derived output, or succeeds when the exact same
// singly linked regular file is already present. Differing existing content
// is never replaced.
func Ensure(path string, contents []byte, mode os.FileMode) error {
	status, err := os.Lstat(path)
	if err == nil {
		if !singlyLinkedRegular(status) {
			return fmt.Errorf("existing output is not a singly linked regular file: %s", path)
		}
		current, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(current, contents) {
			return fmt.Errorf("refusing to replace differing existing file: %s", path)
		}
		if status.Mode().Perm() != mode.Perm() {
			return fmt.Errorf("existing output has unexpected permissions: %s (%s)", path, status.Mode().Perm())
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	publishErr := Write(path, contents, mode, Create)
	if publishErr == nil {
		return nil
	}
	// A concurrent creator may have won after the initial check. Verify its
	// exact bytes before treating that race as success.
	status, inspectErr := os.Lstat(path)
	if os.IsNotExist(inspectErr) {
		return publishErr
	}
	if inspectErr != nil || !singlyLinkedRegular(status) {
		return fmt.Errorf("cannot publish immutable output: %s", path)
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(current, contents) {
		return fmt.Errorf("refusing differing concurrently created file: %s", path)
	}
	if status.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("concurrently created output has unexpected permissions: %s (%s)", path, status.Mode().Perm())
	}
	return nil
}

func Write(path string, contents []byte, mode os.FileMode, policy Policy) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve output path %s: %w", path, err)
	}
	path = absolute
	status, inspectErr := os.Lstat(path)
	switch policy {
	case Create:
		if inspectErr == nil {
			return fmt.Errorf("refusing to replace existing file: %s (%s)", path, status.Mode())
		}
	case ReplaceRegular:
		if inspectErr == nil && !singlyLinkedRegular(status) {
			return fmt.Errorf("refusing to replace file that is not a singly linked regular file: %s (%s)", path, status.Mode())
		}
	default:
		return fmt.Errorf("invalid atomic-file policy %d", policy)
	}
	if inspectErr != nil && !os.IsNotExist(inspectErr) {
		return fmt.Errorf("inspect output %s: %w", path, inspectErr)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("prepare output directory %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary output in %s: %w", directory, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	fail := func(cause error) error {
		_ = temporary.Close()
		return cause
	}
	if err := temporary.Chmod(mode); err != nil {
		return fail(fmt.Errorf("set output mode: %w", err))
	}
	if _, err := temporary.Write(contents); err != nil {
		return fail(fmt.Errorf("write temporary output: %w", err))
	}
	if err := temporary.Sync(); err != nil {
		return fail(fmt.Errorf("sync temporary output: %w", err))
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	return publishSynced(path, temporaryPath, policy)
}

// Publish atomically gives an already-complete temporary file its durable
// destination name. The temporary file must be a singly linked regular file
// in the destination directory. Create never replaces a concurrent writer.
func Publish(path, temporaryPath string, mode os.FileMode, policy Policy) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve output path %s: %w", path, err)
	}
	temporary, err := filepath.Abs(temporaryPath)
	if err != nil {
		return fmt.Errorf("resolve temporary path %s: %w", temporaryPath, err)
	}
	if filepath.Dir(absolute) != filepath.Dir(temporary) {
		return fmt.Errorf("temporary output must share destination directory: %s", temporary)
	}
	status, err := os.Lstat(temporary)
	if err != nil || !singlyLinkedRegular(status) {
		return fmt.Errorf("temporary output is not a singly linked regular file: %s", temporary)
	}
	if err := os.Chmod(temporary, mode); err != nil {
		return fmt.Errorf("set temporary output mode: %w", err)
	}
	handle, err := os.Open(temporary)
	if err != nil {
		return fmt.Errorf("open temporary output: %w", err)
	}
	if err := handle.Sync(); err != nil {
		handle.Close()
		return fmt.Errorf("sync temporary output: %w", err)
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	return publishSynced(absolute, temporary, policy)
}

func publishSynced(path, temporaryPath string, policy Policy) error {
	directory := filepath.Dir(path)
	switch policy {
	case Create:
		if err := os.Link(temporaryPath, path); err != nil {
			if os.IsExist(err) {
				return fmt.Errorf("refusing to replace existing file: %s", path)
			}
			return fmt.Errorf("publish output %s: %w", path, err)
		}
	case ReplaceRegular:
		if status, err := os.Lstat(path); err == nil && !singlyLinkedRegular(status) {
			return fmt.Errorf("refusing to replace file that is not a singly linked regular file: %s (%s)", path, status.Mode())
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect output %s: %w", path, err)
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			return fmt.Errorf("replace output %s: %w", path, err)
		}
	default:
		return fmt.Errorf("invalid atomic-file policy %d", policy)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync output directory %s: %w", directory, err)
	}
	return nil
}

func singlyLinkedRegular(status os.FileInfo) bool {
	metadata, ok := status.Sys().(*syscall.Stat_t)
	return ok && status.Mode().IsRegular() && metadata.Nlink == 1
}

func syncDirectory(path string) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}
