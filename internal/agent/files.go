package agent

import (
	"fmt"
	"os"
	"syscall"

	"rocmplete/internal/atomicfile"
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
	return atomicfile.Write(path, contents, mode, atomicfile.ReplaceRegular)
}

func regularFile(path, description string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is not a regular file: %s", description, path)
	}
	return os.ReadFile(path)
}
