package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

var assetSuffixes = []string{".bin", ".ckpt", ".cube", ".engine", ".gguf", ".onnx", ".pt", ".pth", ".safetensors", ".vae"}

type WorkflowPackage struct {
	Identifier string   `json:"identifier"`
	Source     string   `json:"source"`
	Versions   []string `json:"versions"`
	NodeTypes  []string `json:"node_types"`
}

type WorkflowModes struct {
	Active   int `json:"active"`
	Bypassed int `json:"bypassed"`
	Other    int `json:"other"`
}

type WorkflowSummary struct {
	Path              string            `json:"path"`
	Format            string            `json:"format"`
	NodeCount         int               `json:"node_count"`
	NodeModes         *WorkflowModes    `json:"node_modes,omitempty"`
	DeclaredPackages  []WorkflowPackage `json:"declared_packages,omitempty"`
	CoreNodeTypes     []string          `json:"core_node_types,omitempty"`
	UnattributedTypes []string          `json:"unattributed_node_types,omitempty"`
	NodeTypes         []string          `json:"node_types,omitempty"`
	AssetReferences   []string          `json:"asset_references"`
}

func InspectWorkflow(path string) (WorkflowSummary, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return WorkflowSummary{}, fmt.Errorf("read workflow %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil || root == nil {
		return WorkflowSummary{}, fmt.Errorf("workflow root is not an object: %s", path)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return WorkflowSummary{}, fmt.Errorf("workflow contains trailing data: %s", path)
	}
	if _, ui := root["nodes"].([]any); ui {
		return inspectUIWorkflow(path, root)
	}
	return inspectAPIWorkflow(path, root)
}

func inspectUIWorkflow(path string, root map[string]any) (WorkflowSummary, error) {
	collections := []map[string]any{root}
	if definitions, ok := root["definitions"].(map[string]any); ok {
		if subgraphs, ok := definitions["subgraphs"].([]any); ok {
			for _, raw := range subgraphs {
				if group, ok := raw.(map[string]any); ok {
					collections = append(collections, group)
				}
			}
		}
	}
	packages := make(map[string]map[string]map[string]bool)
	core, unattributed, assets := map[string]bool{}, map[string]bool{}, map[string]bool{}
	modes := WorkflowModes{}
	count := 0
	for _, collection := range collections {
		nodes, ok := collection["nodes"].([]any)
		if !ok {
			return WorkflowSummary{}, fmt.Errorf("UI workflow has an invalid node collection")
		}
		for _, raw := range nodes {
			node, ok := raw.(map[string]any)
			if !ok {
				return WorkflowSummary{}, fmt.Errorf("UI workflow contains an invalid node")
			}
			nodeType, ok := node["type"].(string)
			if !ok || nodeType == "" {
				return WorkflowSummary{}, fmt.Errorf("UI workflow node has no valid type")
			}
			count++
			mode := int64(0)
			if number, ok := node["mode"].(json.Number); ok {
				mode, _ = number.Int64()
			}
			switch mode {
			case 0:
				modes.Active++
			case 4:
				modes.Bypassed++
			default:
				modes.Other++
			}
			properties, _ := node["properties"].(map[string]any)
			registry, _ := properties["cnr_id"].(string)
			repository, _ := properties["aux_id"].(string)
			version, _ := properties["ver"].(string)
			source, identifier := "", ""
			switch {
			case registry == "comfy-core":
				core[nodeType] = true
			case registry != "":
				source, identifier = "registry", registry
			case repository != "":
				source, identifier = "repository", repository
			default:
				unattributed[nodeType] = true
			}
			if identifier != "" {
				key := source + "\x00" + identifier
				if packages[key] == nil {
					packages[key] = map[string]map[string]bool{"versions": {}, "types": {}}
				}
				packages[key]["types"][nodeType] = true
				if version != "" {
					packages[key]["versions"][version] = true
				}
			}
			collectAssets(node["widgets_values"], assets)
		}
	}
	declared := make([]WorkflowPackage, 0, len(packages))
	for key, values := range packages {
		source, identifier, _ := strings.Cut(key, "\x00")
		declared = append(declared, WorkflowPackage{Identifier: identifier, Source: source, Versions: sortedSet(values["versions"]), NodeTypes: sortedSet(values["types"])})
	}
	sort.Slice(declared, func(i, j int) bool {
		if declared[i].Source == declared[j].Source {
			return declared[i].Identifier < declared[j].Identifier
		}
		return declared[i].Source < declared[j].Source
	})
	return WorkflowSummary{Path: path, Format: "ui", NodeCount: count, NodeModes: &modes, DeclaredPackages: declared, CoreNodeTypes: sortedSet(core), UnattributedTypes: sortedSet(unattributed), AssetReferences: sortedSet(assets)}, nil
}

func inspectAPIWorkflow(path string, root map[string]any) (WorkflowSummary, error) {
	types, assets := map[string]bool{}, map[string]bool{}
	for _, raw := range root {
		node, ok := raw.(map[string]any)
		if !ok {
			return WorkflowSummary{}, fmt.Errorf("API workflow contains an invalid node")
		}
		nodeType, typeOK := node["class_type"].(string)
		inputs, inputsOK := node["inputs"].(map[string]any)
		if !typeOK || nodeType == "" || !inputsOK {
			return WorkflowSummary{}, fmt.Errorf("API workflow node is missing type or inputs")
		}
		types[nodeType] = true
		collectAssets(inputs, assets)
	}
	return WorkflowSummary{Path: path, Format: "api", NodeCount: len(root), NodeTypes: sortedSet(types), AssetReferences: sortedSet(assets)}, nil
}

func collectAssets(value any, result map[string]bool) {
	switch typed := value.(type) {
	case string:
		lower := strings.ToLower(typed)
		for _, suffix := range assetSuffixes {
			if strings.HasSuffix(lower, suffix) {
				result[typed] = true
				break
			}
		}
	case []any:
		for _, item := range typed {
			collectAssets(item, result)
		}
	case map[string]any:
		for _, item := range typed {
			collectAssets(item, result)
		}
	}
}

func sortedSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
