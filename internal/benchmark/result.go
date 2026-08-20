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

	"rocmplete/internal/atomicfile"
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
	if err := atomicfile.JSON(path, value, 0o644, atomicfile.Create); err != nil {
		return fmt.Errorf("write benchmark result: %w", err)
	}
	return nil
}

// WriteCheckpoint atomically creates or replaces a regular checkpoint owned
// by an in-progress suite. It refuses symlinks and other unexpected targets.
func WriteCheckpoint(path string, value any) error {
	return atomicfile.JSON(path, value, 0o644, atomicfile.ReplaceRegular)
}

// WriteNewCheckpoint publishes the first state of a resumable run without
// replacing a path selected by another concurrent run.
func WriteNewCheckpoint(path string, value any) error {
	return atomicfile.JSON(path, value, 0o644, atomicfile.Create)
}

// ReadJSON decodes one complete JSON document. Strict mode rejects unknown
// fields and is appropriate for ROCmplete-owned resumable state.
func ReadJSON(path string, destination any, strict bool) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer handle.Close()
	decoder := json.NewDecoder(handle)
	decoder.UseNumber()
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode JSON %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("JSON contains invalid trailing data: %s", path)
	}
	return nil
}
