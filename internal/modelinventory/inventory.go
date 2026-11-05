// Package modelinventory performs read-only discovery of managed and local
// text-model files. It never creates the selected data directory.
package modelinventory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/storage"
	"paracetamol/internal/verification"
)

var ggufShard = regexp.MustCompile(`(?i)^(.+)-([0-9]{5})-of-([0-9]{5})\.gguf$`)

type LlamaModel struct {
	Path           string
	Size           int64
	State          string
	Source         string
	Presets        []string
	Bundles        []string
	Artifacts      []string
	ShardCount     int
	ExpectedShards int
}

func Llama(managed catalog.Catalog, dataRoot string, scanPaths []string) ([]LlamaModel, error) {
	store, err := verification.Load(dataRoot)
	if err != nil {
		return nil, err
	}
	catalogModels, claimed, err := catalogLlamaModels(managed, dataRoot, store)
	if err != nil {
		return nil, err
	}

	root := (storage.Layout{Root: dataRoot}).LlamaModels()
	paths := make(map[string]string)
	remember := func(path string) {
		identity, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			identity, _ = filepath.Abs(path)
		}
		if _, exists := paths[identity]; !exists {
			paths[identity] = path
		}
	}
	if status, statErr := os.Lstat(root); statErr == nil {
		if status.Mode()&os.ModeSymlink != 0 || !status.IsDir() {
			return nil, fmt.Errorf("llama.cpp model root is not a real directory: %s", root)
		}
		found, walkErr := directoryGGUFs(root)
		if walkErr != nil {
			return nil, walkErr
		}
		for _, path := range found {
			remember(path)
		}
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("inspect llama.cpp model root %s: %w", root, statErr)
	}
	for _, value := range scanPaths {
		found, scanErr := scanPath(value)
		if scanErr != nil {
			return nil, scanErr
		}
		for _, path := range found {
			remember(path)
		}
	}

	var loosePaths []string
	for identity, path := range paths {
		if !claimed[identity] {
			loosePaths = append(loosePaths, path)
		}
	}
	loose, err := looseModels(loosePaths)
	if err != nil {
		return nil, err
	}
	models := append(catalogModels, loose...)
	sort.Slice(models, func(left, right int) bool {
		if models[left].Source != models[right].Source {
			return models[left].Source == "catalog"
		}
		leftPath, rightPath := strings.ToLower(models[left].Path), strings.ToLower(models[right].Path)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return strings.Join(models[left].Presets, "\x00") < strings.Join(models[right].Presets, "\x00")
	})
	return models, nil
}

func catalogLlamaModels(managed catalog.Catalog, dataRoot string, store *verification.Store) ([]LlamaModel, map[string]bool, error) {
	// Sharing target bytes does not imply sharing launch requirements. Keep
	// plain and speculative bundles separate, while grouping true aliases.
	type group struct{ artifact, bundle string }
	presetsByGroup := make(map[group][]string)
	for id, preset := range managed.LlamaPresets {
		key := group{preset.Artifact, preset.Bundle}
		presetsByGroup[key] = append(presetsByGroup[key], id)
	}
	groups := make([]group, 0, len(presetsByGroup))
	for key := range presetsByGroup {
		groups = append(groups, key)
	}
	sort.Slice(groups, func(left, right int) bool {
		if groups[left].artifact != groups[right].artifact {
			return groups[left].artifact < groups[right].artifact
		}
		return groups[left].bundle < groups[right].bundle
	})
	claimed := make(map[string]bool)
	models := make([]LlamaModel, 0, len(groups))
	for _, key := range groups {
		presets := presetsByGroup[key]
		sort.Strings(presets)
		required := make(map[string]catalog.Artifact)
		auxiliary := make(map[string]bool)
		for _, presetID := range presets {
			preset := managed.LlamaPresets[presetID]
			if preset.MMProjArtifact != "" {
				auxiliary[preset.MMProjArtifact] = true
			}
			if preset.DraftArtifact != "" {
				auxiliary[preset.DraftArtifact] = true
			}
		}
		for _, artifactID := range managed.Bundles[key.bundle].Artifacts {
			artifact := managed.Artifacts[artifactID]
			if artifact.Target == "llama-models" {
				required[artifactID] = artifact
			}
		}
		requiredIDs := sortedKeys(required)
		states := make([]content.State, 0, len(requiredIDs))
		var expected, observed int64
		shards := 0
		for _, artifactID := range requiredIDs {
			artifact := required[artifactID]
			// Drafts and projectors affect readiness and size, not target sharding.
			if !auxiliary[artifactID] {
				shards++
			}
			expected += artifact.Size
			status, inspectErr := content.InspectArtifact(store, dataRoot, artifact, false)
			if inspectErr != nil {
				return nil, nil, inspectErr
			}
			states = append(states, status.State)
			if info, statErr := os.Lstat(status.Path); statErr == nil {
				observed += info.Size()
			}
			identity, resolveErr := filepath.EvalSymlinks(status.Path)
			if resolveErr != nil {
				identity, _ = filepath.Abs(status.Path)
			}
			claimed[identity] = true
		}
		state := aggregate(states)
		size := observed
		if state == "missing" {
			size = expected
		}
		primary := managed.Artifacts[key.artifact]
		models = append(models, LlamaModel{
			Path: content.ArtifactPath(dataRoot, primary), Size: size, State: state,
			Source: "catalog", Presets: presets, Bundles: []string{key.bundle}, Artifacts: requiredIDs,
			ShardCount: shards, ExpectedShards: shards,
		})
	}
	return models, claimed, nil
}

func aggregate(states []content.State) string {
	counts := make(map[content.State]int)
	for _, state := range states {
		counts[state]++
	}
	if counts[content.Missing] == len(states) {
		return "missing"
	}
	for _, candidate := range []content.State{content.Unexpected, content.HashMismatch, content.SizeMismatch} {
		if counts[candidate] > 0 {
			if candidate == content.Unexpected {
				return "user-file"
			}
			return string(candidate)
		}
	}
	if counts[content.Missing] > 0 {
		return "partial"
	}
	if counts[content.Unverified] > 0 {
		return "unverified"
	}
	return "ready"
}

func scanPath(value string) ([]string, error) {
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve model scan path %s: %w", value, err)
		}
		value = filepath.Join(home, value[2:])
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return nil, fmt.Errorf("resolve model scan path %s: %w", value, err)
	}
	status, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("model scan path does not exist: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect model scan path %s: %w", path, err)
	}
	if status.Mode().IsRegular() {
		if !strings.EqualFold(filepath.Ext(path), ".gguf") {
			return nil, fmt.Errorf("model scan file is not a .gguf file: %s", path)
		}
		return []string{path}, nil
	}
	if !status.IsDir() {
		return nil, fmt.Errorf("model scan path is not a file or directory: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve model scan path %s: %w", path, err)
	}
	return directoryGGUFs(resolved)
}

func directoryGGUFs(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("scan model directory %s: %w", root, walkErr)
		}
		if path != root && entry.Type()&os.ModeSymlink != 0 && entry.IsDir() {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".gguf") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}

type shardKey struct {
	directory string
	prefix    string
	total     int
}

func looseModels(paths []string) ([]LlamaModel, error) {
	standalone := make([]LlamaModel, 0)
	groups := make(map[shardKey]map[int]string)
	sort.Strings(paths)
	for _, path := range paths {
		match := ggufShard.FindStringSubmatch(filepath.Base(path))
		if match == nil {
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				standalone = append(standalone, LlamaModel{Path: path, State: "broken", Source: "local", ShardCount: 1, ExpectedShards: 1})
				continue
			}
			standalone = append(standalone, LlamaModel{Path: path, Size: info.Size(), State: "ready", Source: "local", ShardCount: 1, ExpectedShards: 1})
			continue
		}
		part, _ := strconv.Atoi(match[2])
		total, _ := strconv.Atoi(match[3])
		if total < 1 || part < 1 || part > total {
			info, _ := os.Stat(path)
			var size int64
			if info != nil {
				size = info.Size()
			}
			standalone = append(standalone, LlamaModel{Path: path, Size: size, State: "broken", Source: "local", ShardCount: 1, ExpectedShards: 1})
			continue
		}
		key := shardKey{directory: filepath.Dir(path), prefix: strings.ToLower(match[1]), total: total}
		if groups[key] == nil {
			groups[key] = make(map[int]string)
		}
		groups[key][part] = path
	}
	models := standalone
	keys := make([]shardKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].directory+keys[i].prefix < keys[j].directory+keys[j].prefix
	})
	for _, key := range keys {
		parts := groups[key]
		first := parts[1]
		if first == "" {
			indexes := make([]int, 0, len(parts))
			for part := range parts {
				indexes = append(indexes, part)
			}
			sort.Ints(indexes)
			first = parts[indexes[0]]
		}
		state := "ready"
		if len(parts) != key.total {
			state = "partial"
		}
		var size int64
		for part := 1; part <= key.total; part++ {
			path := parts[part]
			if path == "" {
				state = "partial"
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				state = "broken"
				continue
			}
			size += info.Size()
		}
		models = append(models, LlamaModel{Path: first, Size: size, State: state, Source: "local", ShardCount: len(parts), ExpectedShards: key.total})
	}
	return models, nil
}

func sortedKeys(values map[string]catalog.Artifact) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
