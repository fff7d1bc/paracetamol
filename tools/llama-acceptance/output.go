package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"paracetamol/internal/atomicfile"
)

func validateNewOutput(root, out string) (string, error) {
	if out == "" {
		return "", fmt.Errorf("--out requires a fresh directory below checkout build/")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	out = filepath.Clean(out)
	rel, err := filepath.Rel(filepath.Join(root, "build"), out)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output must be a new subdirectory of checkout build/")
	}
	current := root
	parts := append([]string{"build"}, strings.Split(rel, string(filepath.Separator))...)
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, inspectErr := os.Lstat(current)
		if inspectErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("output ancestor must be a real directory")
			}
			if index == len(parts)-1 {
				return "", fmt.Errorf("refusing to reuse existing output directory")
			}
		} else if !os.IsNotExist(inspectErr) {
			return "", inspectErr
		}
	}
	return out, nil
}

func createOutput(root, out string) (string, error) {
	path, err := validateNewOutput(root, out)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func savePrivateJSON(path string, value any) error {
	return atomicfile.JSON(path, value, 0o600, atomicfile.Create)
}
