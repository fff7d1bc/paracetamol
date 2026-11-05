package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"paracetamol/internal/storage"
)

var localGGUFShard = regexp.MustCompile(`(?i)^(.+)-([0-9]{5})-of-([0-9]{5})(\.gguf)$`)

// Keep external model mounts file-scoped: :z on a parent directory recursively
// relabels unrelated host data, even when that directory is mounted read-only.
// Planning only inspects paths, so dry runs neither stage bytes nor relabel them.
func llamaModelMounts(layout storage.Layout, local, managed, suffix string) ([]string, string, error) {
	readOnly := readOnlySharedSuffix(suffix)
	if local == "" {
		model := ""
		if managed != "" {
			model = "/content/models/" + managed
		}
		return []string{"--volume", layout.LlamaModels() + ":/content/models" + readOnly}, model, nil
	}
	resolved, err := filepath.EvalSymlinks(local)
	if err != nil {
		return nil, "", fmt.Errorf("resolve local GGUF: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, "", err
	}
	if strings.Contains(resolved, ":") {
		return nil, "", fmt.Errorf("local GGUF path cannot contain ':' in a Podman volume: %s", resolved)
	}
	if !strings.EqualFold(filepath.Ext(resolved), ".gguf") {
		return nil, "", fmt.Errorf("local model must name a .gguf file: %s", resolved)
	}
	paths := []string{resolved}
	if match := localGGUFShard.FindStringSubmatch(filepath.Base(resolved)); match != nil {
		part, _ := strconv.Atoi(match[2])
		total, _ := strconv.Atoi(match[3])
		if part != 1 || total < 1 {
			return nil, "", fmt.Errorf("local split GGUF must name the first shard (00001) with a positive shard count: %s", resolved)
		}
		paths = make([]string, total)
		for index := range paths {
			paths[index] = filepath.Join(filepath.Dir(resolved), fmt.Sprintf("%s-%05d-of-%05d%s", match[1], index+1, total, match[4]))
		}
	}
	mounts := make([]string, 0, 2*len(paths))
	for _, path := range paths {
		// Do not follow a sibling shard symlink outside the selected model set.
		status, err := os.Lstat(path)
		if err != nil {
			return nil, "", fmt.Errorf("inspect local GGUF file %s: %w", path, err)
		}
		if !status.Mode().IsRegular() {
			return nil, "", fmt.Errorf("local GGUF file is not a regular file: %s", path)
		}
		mounts = append(mounts, "--volume", path+":/content/models/"+filepath.Base(path)+readOnly)
	}
	return mounts, "/content/models/" + filepath.Base(resolved), nil
}
