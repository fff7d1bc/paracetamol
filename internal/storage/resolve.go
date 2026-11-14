package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveDataRoot resolves existing aliases, including above a missing suffix,
// without creating persistent data before validation and approval complete.
func ResolveDataRoot(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	ancestor := root
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				return "", fmt.Errorf("data directory ancestor is not a directory: %s", ancestor)
			}
			suffix, err := filepath.Rel(ancestor, root)
			if err != nil {
				return "", err
			}
			return filepath.Join(resolved, suffix), nil
		} else if !os.IsNotExist(err) || filepath.Dir(ancestor) == ancestor {
			return "", err
		}
		ancestor = filepath.Dir(ancestor)
	}
}
