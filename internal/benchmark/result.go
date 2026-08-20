// Package benchmark executes managed workloads and writes durable result files.
package benchmark

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func Timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func Identifier() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("%08x", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(bytes[:])
}

func DefaultPath(directory, suffix string) string {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	return filepath.Join(directory, stamp+"-"+Identifier()+suffix)
}

// WriteJSON publishes a new result atomically and never replaces an existing
// result. Benchmark evidence is append-only even when a caller chooses a path.
func WriteJSON(path string, value any) error {
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		path = absolute
	}
	if status, err := os.Lstat(path); err == nil {
		return fmt.Errorf("refusing to replace existing benchmark result: %s (%s)", path, status.Mode())
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect benchmark result %s: %w", path, err)
	}
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode benchmark result: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare benchmark result directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("refusing to replace existing benchmark result: %s", path)
		}
		return fmt.Errorf("publish benchmark result %s: %w", path, err)
	}
	return nil
}

// WriteCheckpoint atomically creates or replaces a regular checkpoint owned
// by an in-progress suite. It refuses symlinks and other unexpected targets.
func WriteCheckpoint(path string, value any) error {
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		path = absolute
	}
	if status, err := os.Lstat(path); err == nil && !status.Mode().IsRegular() {
		return fmt.Errorf("checkpoint is not a regular file: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func ReadObject(path string) (map[string]any, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	decoder := json.NewDecoder(handle)
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode benchmark result %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("benchmark result contains invalid trailing data: %s", path)
	}
	return value, nil
}
