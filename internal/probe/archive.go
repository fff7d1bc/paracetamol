// Package probe contains read-only maintainer research helpers. Probes emit
// evidence but never install, extract, or mutate catalog content.
package probe

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

type ArchiveMember struct {
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	SafePath       bool    `json:"safe_path"`
	Size           uint64  `json:"size"`
	CompressedSize uint64  `json:"compressed_size"`
	SHA256         *string `json:"sha256"`
}

type ArchiveSummary struct {
	Path           string          `json:"path"`
	Size           int64           `json:"size"`
	SHA256         string          `json:"sha256"`
	DuplicateNames []string        `json:"duplicate_names"`
	Members        []ArchiveMember `json:"members"`
}

func InspectZIP(file string, selected []string) (ArchiveSummary, error) {
	status, err := os.Stat(file)
	if err != nil || !status.Mode().IsRegular() {
		return ArchiveSummary{}, fmt.Errorf("archive is not a regular file: %s", file)
	}
	handle, err := os.Open(file)
	if err != nil {
		return ArchiveSummary{}, err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, handle); err != nil {
		handle.Close()
		return ArchiveSummary{}, err
	}
	if err := handle.Close(); err != nil {
		return ArchiveSummary{}, err
	}
	archive, err := zip.OpenReader(file)
	if err != nil {
		return ArchiveSummary{}, fmt.Errorf("inspect ZIP %s: %w", file, err)
	}
	defer archive.Close()
	counts := make(map[string]int)
	for _, member := range archive.File {
		counts[member.Name]++
	}
	duplicates := make([]string, 0)
	for name, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, name)
		}
	}
	sort.Strings(duplicates)
	members := archive.File
	if len(selected) > 0 {
		members = make([]*zip.File, 0, len(selected))
		for _, wanted := range selected {
			if counts[wanted] != 1 {
				return ArchiveSummary{}, fmt.Errorf("expected exactly one archive member %q; found %d", wanted, counts[wanted])
			}
			for _, member := range archive.File {
				if member.Name == wanted {
					members = append(members, member)
					break
				}
			}
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	summaries := make([]ArchiveMember, 0, len(members))
	for _, member := range members {
		kind := "file"
		mode := member.Mode()
		if mode.IsDir() {
			kind = "directory"
		} else if mode&os.ModeSymlink != 0 {
			kind = "symlink"
		} else if !mode.IsRegular() {
			kind = "other"
		}
		summary := ArchiveMember{Name: member.Name, Type: kind, SafePath: safeArchivePath(member.Name), Size: member.UncompressedSize64, CompressedSize: member.CompressedSize64}
		if kind == "file" {
			handle, err := member.Open()
			if err != nil {
				return ArchiveSummary{}, err
			}
			digest := sha256.New()
			_, copyErr := io.Copy(digest, handle)
			closeErr := handle.Close()
			if copyErr != nil {
				return ArchiveSummary{}, copyErr
			}
			if closeErr != nil {
				return ArchiveSummary{}, closeErr
			}
			value := hex.EncodeToString(digest.Sum(nil))
			summary.SHA256 = &value
		}
		summaries = append(summaries, summary)
	}
	return ArchiveSummary{Path: file, Size: status.Size(), SHA256: hex.EncodeToString(digest.Sum(nil)), DuplicateNames: duplicates, Members: summaries}, nil
}

func safeArchivePath(value string) bool {
	if value == "" || path.IsAbs(value) || strings.Contains(value, "\\") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
