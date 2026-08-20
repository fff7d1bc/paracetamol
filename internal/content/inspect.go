// Package content plans and verifies managed catalog content.
package content

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"paracetamol/internal/catalog"
	"paracetamol/internal/storage"
	"paracetamol/internal/verification"
)

type State string

const (
	Missing      State = "missing"
	Verified     State = "verified"
	Unverified   State = "unverified"
	SizeMismatch State = "size-mismatch"
	HashMismatch State = "hash-mismatch"
	Unexpected   State = "unexpected"
)

type ArtifactStatus struct {
	Artifact catalog.Artifact
	Path     string
	State    State
}

func ArtifactPath(dataRoot string, artifact catalog.Artifact) string {
	layout := storage.Layout{Root: dataRoot}
	var root string
	switch artifact.Target {
	case "models":
		root = layout.ComfyModels()
	case "llama-models":
		root = layout.LlamaModels()
	case "dwarfstar-models":
		root = layout.DwarfStarModels()
	case "workflows":
		root = filepath.Join(layout.Application("comfyui"), "user", "default", "workflows", "imported")
	}
	return filepath.Join(root, filepath.FromSlash(artifact.Destination))
}

func InspectArtifact(store *verification.Store, dataRoot string, artifact catalog.Artifact, verifyHash bool) (ArtifactStatus, error) {
	return inspectArtifact(context.Background(), store, dataRoot, artifact, verifyHash, nil)
}

func VerifyArtifact(ctx context.Context, store *verification.Store, dataRoot string, artifact catalog.Artifact, progress func(int64)) (ArtifactStatus, error) {
	return inspectArtifact(ctx, store, dataRoot, artifact, true, progress)
}

func inspectArtifact(ctx context.Context, store *verification.Store, dataRoot string, artifact catalog.Artifact, verifyHash bool, progress func(int64)) (ArtifactStatus, error) {
	file := ArtifactPath(dataRoot, artifact)
	status := ArtifactStatus{Artifact: artifact, Path: file, State: Missing}
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("inspect managed content %s: %w", file, err)
	}
	if !info.Mode().IsRegular() {
		status.State = Unexpected
		return status, nil
	}
	if info.Size() != artifact.Size {
		status.State = SizeMismatch
		return status, nil
	}
	if !verifyHash && store != nil && store.Matches(file, artifact.Size, artifact.SHA256) {
		status.State = Verified
		return status, nil
	}
	if !verifyHash {
		status.State = Unverified
		return status, nil
	}
	fingerprint := fileIdentity(info)
	digest, err := fileSHA256Context(ctx, file, progress)
	if err != nil {
		return status, err
	}
	hashed, err := os.Lstat(file)
	if err != nil || !hashed.Mode().IsRegular() || fileIdentity(hashed) != fingerprint {
		return status, fmt.Errorf("managed content changed during verification: %s", file)
	}
	if digest != artifact.SHA256 {
		status.State = HashMismatch
		return status, nil
	}
	status.State = Verified
	if store != nil {
		if err := store.Record(file, artifact.Size, artifact.SHA256); err != nil {
			return status, err
		}
	}
	return status, nil
}

func InspectBundle(store *verification.Store, managed catalog.Catalog, bundle catalog.Bundle, dataRoot string, verifyHash bool) ([]ArtifactStatus, error) {
	statuses := make([]ArtifactStatus, 0, len(bundle.Artifacts))
	for _, identifier := range bundle.Artifacts {
		status, err := InspectArtifact(store, dataRoot, managed.Artifacts[identifier], verifyHash)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func Ready(state State) bool { return state == Verified }

func RequireBundle(managed catalog.Catalog, bundle catalog.Bundle, dataRoot string) ([]ArtifactStatus, error) {
	store, err := verification.Load(dataRoot)
	if err != nil {
		return nil, err
	}
	statuses, err := InspectBundle(store, managed, bundle, dataRoot, false)
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if !Ready(status.State) {
			return statuses, fmt.Errorf("managed content is not ready: %s (%s)", status.Path, status.State)
		}
	}
	return statuses, nil
}

func fileSHA256(path string) (string, error) {
	return fileSHA256Context(context.Background(), path, nil)
}

func fileSHA256Context(ctx context.Context, path string, progress func(int64)) (string, error) {
	handle, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer handle.Close()
	digest := sha256.New()
	buffer := make([]byte, 8*1024*1024)
	var hashed int64
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		read, readErr := handle.Read(buffer)
		if read > 0 {
			if _, err := digest.Write(buffer[:read]); err != nil {
				return "", fmt.Errorf("hash %s: %w", path, err)
			}
			hashed += int64(read)
			if progress != nil {
				progress(hashed)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("hash %s: %w", path, readErr)
		}
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}
