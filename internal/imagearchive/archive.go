// Package imagearchive validates Docker archives before Podman loads them.
package imagearchive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"

	"rocmplete/internal/config"
)

const (
	maxManifestSize = 4 * 1024 * 1024
	maxConfigSize   = 16 * 1024 * 1024
	maxMembers      = 10000
	maxEntries      = 64
)

type Image struct {
	Reference       string
	ID              string
	Architecture    string
	OperatingSystem string
}

type Archive struct {
	Path   string
	Size   int64
	Images []Image
}

type member struct {
	typeflag byte
	link     string
	size     int64
	data     []byte
}

type manifestEntry struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

func ManagedReferences() []string {
	result := []string{config.ContentToolsImage, config.ROCmRuntimeImage, config.ROCmBaseImage}
	for _, application := range config.Applications() {
		result = append(result, application.Image)
	}
	return result
}

func SelectedReferences(target string) ([]string, error) {
	if target == "all" {
		return ManagedReferences(), nil
	}
	if target == "base" {
		return []string{config.ContentToolsImage, config.ROCmRuntimeImage, config.ROCmBaseImage}, nil
	}
	application, ok := config.ApplicationByID(target)
	if !ok {
		return nil, fmt.Errorf("unknown image export target %q", target)
	}
	result := []string{config.ContentToolsImage, config.ROCmRuntimeImage}
	if application.SharedPyTorchBase {
		result = append(result, config.ROCmBaseImage)
	}
	return append(result, application.Image), nil
}

func Inspect(file string) (Archive, error) {
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return Archive{}, fmt.Errorf("image archive is not a non-empty regular file: %s", file)
	}
	handle, err := os.Open(file)
	if err != nil {
		return Archive{}, err
	}
	defer handle.Close()
	var input io.Reader = handle
	header := make([]byte, 2)
	if _, err := io.ReadFull(handle, header); err != nil {
		return Archive{}, fmt.Errorf("read image archive: %w", err)
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		return Archive{}, err
	}
	if header[0] == 0x1f && header[1] == 0x8b {
		compressed, err := gzip.NewReader(handle)
		if err != nil {
			return Archive{}, fmt.Errorf("read compressed image archive: %w", err)
		}
		defer compressed.Close()
		input = compressed
	}
	reader := tar.NewReader(input)
	members := make(map[string]member)
	for count := 1; ; count++ {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Archive{}, fmt.Errorf("read image archive: %w", err)
		}
		if count > maxMembers {
			return Archive{}, fmt.Errorf("image archive contains too many members")
		}
		if !safeName(header.Name) {
			return Archive{}, fmt.Errorf("image archive contains unsafe member %q", header.Name)
		}
		if _, exists := members[header.Name]; exists {
			return Archive{}, fmt.Errorf("image archive repeats member %q", header.Name)
		}
		item := member{typeflag: header.Typeflag, link: header.Linkname, size: header.Size}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeSymlink && header.Typeflag != tar.TypeLink {
			return Archive{}, fmt.Errorf("image archive contains unsupported member type %q", header.Name)
		}
		if (header.Name == "manifest.json" || strings.HasSuffix(header.Name, ".json")) && (header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA) {
			limit := int64(maxConfigSize)
			if header.Name == "manifest.json" {
				limit = maxManifestSize
			}
			if header.Size <= 0 || header.Size > limit {
				return Archive{}, fmt.Errorf("image archive JSON member %q has unsafe size", header.Name)
			}
			item.data, err = io.ReadAll(io.LimitReader(reader, limit+1))
			if err != nil || int64(len(item.data)) != header.Size {
				return Archive{}, fmt.Errorf("image archive member %q is truncated", header.Name)
			}
		}
		members[header.Name] = item
	}
	manifestMember, ok := members["manifest.json"]
	if !ok || len(manifestMember.data) == 0 {
		return Archive{}, fmt.Errorf("image archive must contain one manifest.json")
	}
	for name, item := range members {
		if item.typeflag == tar.TypeSymlink || item.typeflag == tar.TypeLink {
			if !validLayerLink(name, item, members) {
				return Archive{}, fmt.Errorf("image archive contains unsafe link %q", name)
			}
		}
	}
	var manifest []manifestEntry
	if err := json.Unmarshal(manifestMember.data, &manifest); err != nil || len(manifest) == 0 || len(manifest) > maxEntries {
		return Archive{}, fmt.Errorf("image archive manifest is invalid or has an unsafe entry count")
	}
	seen := make(map[string]bool)
	var images []Image
	for _, entry := range manifest {
		if !safeName(entry.Config) || len(entry.RepoTags) == 0 {
			return Archive{}, fmt.Errorf("image archive manifest has invalid config or tags")
		}
		configMember, ok := members[entry.Config]
		if !ok || len(configMember.data) == 0 {
			return Archive{}, fmt.Errorf("image archive config %q is missing", entry.Config)
		}
		for _, layer := range entry.Layers {
			item, ok := members[layer]
			if !safeName(layer) || !ok || !regular(item) && !validLayerLink(layer, item, members) {
				return Archive{}, fmt.Errorf("image archive layer %q is missing or invalid", layer)
			}
		}
		var metadata struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		}
		if err := json.Unmarshal(configMember.data, &metadata); err != nil || metadata.Architecture == "" || metadata.OS == "" {
			return Archive{}, fmt.Errorf("image config %q is invalid", entry.Config)
		}
		digest := sha256.Sum256(configMember.data)
		id := fmt.Sprintf("sha256:%x", digest)
		if entry.Config != strings.TrimPrefix(id, "sha256:")+".json" {
			return Archive{}, fmt.Errorf("image config filename does not match its digest: %s", entry.Config)
		}
		for _, reference := range entry.RepoTags {
			if reference == "" || seen[reference] {
				return Archive{}, fmt.Errorf("image archive repeats or contains an empty tag")
			}
			seen[reference] = true
			images = append(images, Image{Reference: reference, ID: id, Architecture: metadata.Architecture, OperatingSystem: metadata.OS})
		}
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Reference < images[j].Reference })
	return Archive{Path: file, Size: info.Size(), Images: images}, nil
}

func ValidateManaged(archive Archive, expected []string) error {
	managed := make(map[string]bool)
	for _, value := range ManagedReferences() {
		managed[value] = true
	}
	actual := make(map[string]bool)
	for _, image := range archive.Images {
		if !managed[image.Reference] {
			return fmt.Errorf("image archive contains unmanaged or obsolete tag: %s", image.Reference)
		}
		actual[image.Reference] = true
		if image.OperatingSystem != "linux" || image.Architecture != runtime.GOARCH {
			return fmt.Errorf("image %s targets %s/%s, not linux/%s", image.Reference, image.OperatingSystem, image.Architecture, runtime.GOARCH)
		}
	}
	if len(actual) == 0 || !actual[config.ContentToolsImage] {
		return fmt.Errorf("image archive is empty or missing the content-tools image")
	}
	if (actual[config.ROCmBaseImage] || containsApplicationImage(actual)) && !actual[config.ROCmRuntimeImage] {
		return fmt.Errorf("image archive is missing the managed ROCm runtime tag")
	}
	for _, application := range config.Applications() {
		if application.SharedPyTorchBase && actual[application.Image] && !actual[config.ROCmBaseImage] {
			return fmt.Errorf("image archive is missing the managed ROCm/PyTorch base tag")
		}
	}
	if len(expected) > 0 {
		if len(expected) != len(actual) {
			return fmt.Errorf("image archive has the wrong managed tag set")
		}
		for _, reference := range expected {
			if !actual[reference] {
				return fmt.Errorf("image archive is missing expected tag %s", reference)
			}
		}
	}
	return nil
}

func safeName(name string) bool {
	return name != "" && !strings.Contains(name, "\\") && !strings.HasPrefix(name, "/") && path.Clean(name) == name && !strings.Contains(name, "../") && name != ".."
}

func regular(item member) bool { return item.typeflag == tar.TypeReg || item.typeflag == tar.TypeRegA }

func validLayerLink(name string, item member, members map[string]member) bool {
	if (item.typeflag != tar.TypeSymlink && item.typeflag != tar.TypeLink) || !strings.HasSuffix(name, "/layer.tar") || item.link == "" || strings.Contains(item.link, "\\") {
		return false
	}
	target := item.link
	if item.typeflag == tar.TypeSymlink {
		target = path.Join(path.Dir(name), item.link)
	}
	target = path.Clean(target)
	value, ok := members[target]
	return safeName(target) && strings.HasSuffix(target, ".tar") && ok && regular(value)
}

func containsApplicationImage(values map[string]bool) bool {
	for _, application := range config.Applications() {
		if values[application.Image] {
			return true
		}
	}
	return false
}
