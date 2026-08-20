package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func secureDirectory(path string, private bool) error {
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private state path is not a real directory: %s", path)
	}
	if private {
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func writeAtomic(path string, contents []byte, mode os.FileMode, preserve bool) error {
	if info, err := os.Lstat(path); err == nil {
		status, ok := info.Sys().(*syscall.Stat_t)
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !ok || status.Nlink != 1 {
			return fmt.Errorf("managed private file is not a singly linked regular file: %s", path)
		}
		if preserve {
			return os.Chmod(path, mode)
		}
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(current) == string(contents) {
			return os.Chmod(path, mode)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func regularFile(path, description string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is not a regular file: %s", description, path)
	}
	return os.ReadFile(path)
}
