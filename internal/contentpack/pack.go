// Package contentpack loads the intentionally narrow ignored local-pack
// schema used by verified one-file remote imports.
package contentpack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"paracetamol/internal/catalog"
)

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
var revision = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)

type document struct {
	Schema    int                    `json:"schema_version"`
	Artifacts map[string]rawArtifact `json:"artifacts"`
	Bundles   map[string]rawBundle   `json:"bundles"`
}
type rawArtifact struct {
	Description string     `json:"description"`
	Source      rawSource  `json:"source"`
	Target      string     `json:"target"`
	Destination string     `json:"destination"`
	Size        int64      `json:"size"`
	SHA256      string     `json:"sha256"`
	License     rawLicense `json:"license"`
}
type rawSource struct {
	Provider       string `json:"provider"`
	Repository     string `json:"repository"`
	Revision       string `json:"revision"`
	Path           string `json:"path"`
	Host           string `json:"host"`
	Filename       string `json:"filename"`
	DownloadURL    string `json:"download_url"`
	ModelID        int64  `json:"model_id"`
	ModelVersionID int64  `json:"model_version_id"`
	RequiresAuth   bool   `json:"requires_auth"`
}
type rawLicense struct {
	SPDX               string `json:"spdx"`
	Status             string `json:"status"`
	URL                string `json:"url"`
	Warning            string `json:"warning"`
	UpstreamRepository string `json:"upstream_repository"`
	UpstreamLicense    string `json:"upstream_license"`
	UpstreamLicenseURL string `json:"upstream_license_url"`
}

type rawBundle struct {
	Description string   `json:"description"`
	Application string   `json:"application"`
	Artifacts   []string `json:"artifacts"`
	Groups      []string `json:"groups"`
}

func Load(base catalog.Catalog, files []string) (catalog.Catalog, []string, error) {
	result := base
	result.Artifacts = cloneMap(base.Artifacts)
	result.Bundles = cloneMap(base.Bundles)
	selected := make([]string, 0, len(files))
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			return catalog.Catalog{}, nil, fmt.Errorf("read content pack %s: %w", file, err)
		}
		decoder := json.NewDecoder(bytes.NewReader(contents))
		decoder.DisallowUnknownFields()
		var raw document
		if err := decoder.Decode(&raw); err != nil {
			return catalog.Catalog{}, nil, fmt.Errorf("invalid content pack %s: %w", file, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return catalog.Catalog{}, nil, fmt.Errorf("invalid content pack %s: trailing JSON", file)
		}
		if raw.Schema != 2 || len(raw.Artifacts) == 0 || len(raw.Bundles) == 0 {
			return catalog.Catalog{}, nil, fmt.Errorf("content pack %s must use schema 2 and contain artifacts and bundles", file)
		}
		artifactIDs := make([]string, 0, len(raw.Artifacts))
		for id := range raw.Artifacts {
			artifactIDs = append(artifactIDs, id)
		}
		sort.Strings(artifactIDs)
		for _, id := range artifactIDs {
			value := raw.Artifacts[id]
			if !identifier.MatchString(id) || value.Description == "" || value.Size <= 0 || !digest.MatchString(value.SHA256) {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack %s has invalid artifact %s", file, id)
			}
			if value.Target != "models" && value.Target != "llama-models" && value.Target != "workflows" {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack artifact %s has unsupported target", id)
			}
			clean := path.Clean(value.Destination)
			if clean != value.Destination || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || clean == "." {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack artifact %s has unsafe destination", id)
			}
			if value.Target == "workflows" && !strings.HasSuffix(value.Destination, ".json") {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack workflow artifact %s must be JSON", id)
			}
			if value.License.SPDX != "NOASSERTION" || value.License.Status != "unverified" || value.License.Warning == "" || !httpsURL(value.License.URL) || value.License.UpstreamLicenseURL != "" && !httpsURL(value.License.UpstreamLicenseURL) {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack artifact %s must retain explicit NOASSERTION metadata", id)
			}
			source := catalog.Source{}
			if value.Source.Provider == "civitai" {
				if value.Source.Host != "civitai.com" && value.Source.Host != "civitai.red" || value.Source.ModelID <= 0 || value.Source.ModelVersionID <= 0 || path.Base(value.Source.Filename) != value.Source.Filename || !exactCivitaiDownload(value.Source.DownloadURL, value.Source.Host, value.Source.ModelVersionID) {
					return catalog.Catalog{}, nil, fmt.Errorf("content pack artifact %s has invalid Civitai source", id)
				}
				source = catalog.Source{Provider: "civitai", ProviderHost: value.Source.Host, ModelID: value.Source.ModelID, ModelVersionID: value.Source.ModelVersionID, Path: value.Source.Filename, DownloadURL: value.Source.DownloadURL, RequiresAuth: value.Source.RequiresAuth}
			} else {
				if value.Source.Provider != "" && value.Source.Provider != "huggingface" || value.Source.Repository == "" || !revision.MatchString(value.Source.Revision) || !safeRelative(value.Source.Path) {
					return catalog.Catalog{}, nil, fmt.Errorf("content pack artifact %s has invalid Hugging Face source", id)
				}
				source = catalog.Source{Provider: "huggingface", Repository: value.Source.Repository, Revision: value.Source.Revision, Path: value.Source.Path}
			}
			if _, ok := result.Artifacts[id]; ok {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack artifact collides with catalog: %s", id)
			}
			result.Artifacts[id] = catalog.Artifact{ID: id, Description: value.Description, Source: source, Destination: value.Destination, Size: value.Size, SHA256: value.SHA256, Target: value.Target, License: catalog.License{SPDX: value.License.SPDX, Status: value.License.Status, URL: value.License.URL, Warning: value.License.Warning, UpstreamRepository: value.License.UpstreamRepository, UpstreamLicense: value.License.UpstreamLicense, UpstreamLicenseURL: value.License.UpstreamLicenseURL}}
		}
		bundleIDs := make([]string, 0, len(raw.Bundles))
		for id := range raw.Bundles {
			bundleIDs = append(bundleIDs, id)
		}
		sort.Strings(bundleIDs)
		for _, id := range bundleIDs {
			value := raw.Bundles[id]
			if !identifier.MatchString(id) || value.Description == "" || value.Application != "comfyui" && value.Application != "llama-cpp" || len(value.Artifacts) == 0 {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack has invalid bundle %s", id)
			}
			if !exactGroups(value.Groups, "all", map[string]string{"comfyui": "comfyui", "llama-cpp": "llama"}[value.Application]) {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack bundle %s has invalid selector groups", id)
			}
			seenArtifacts := make(map[string]bool)
			for _, artifact := range value.Artifacts {
				if seenArtifacts[artifact] {
					return catalog.Catalog{}, nil, fmt.Errorf("content pack bundle %s contains duplicate artifact %s", id, artifact)
				}
				seenArtifacts[artifact] = true
				if _, ok := raw.Artifacts[artifact]; !ok {
					return catalog.Catalog{}, nil, fmt.Errorf("content pack bundle %s references external artifact", id)
				}
				target := raw.Artifacts[artifact].Target
				if value.Application == "llama-cpp" && target != "llama-models" || value.Application == "comfyui" && target != "models" && target != "workflows" {
					return catalog.Catalog{}, nil, fmt.Errorf("content pack bundle %s has an artifact for another application", id)
				}
			}
			if _, ok := result.Bundles[id]; ok {
				return catalog.Catalog{}, nil, fmt.Errorf("content pack bundle collides with catalog: %s", id)
			}
			result.Bundles[id] = catalog.Bundle{ID: id, Description: value.Description, Application: value.Application, Artifacts: value.Artifacts, Groups: value.Groups}
			selected = append(selected, id)
		}
	}
	return result, selected, nil
}

func httpsURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Port() == "" && parsed.Fragment == ""
}

func exactCivitaiDownload(value, host string, version int64) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() == host && parsed.User == nil && parsed.Port() == "" && parsed.Path == "/api/download/models/"+fmt.Sprint(version) && parsed.Fragment == ""
}

func safeRelative(value string) bool {
	return value != "" && path.Clean(value) == value && !strings.HasPrefix(value, "/") && value != "." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "/../")
}

func exactGroups(values []string, expected ...string) bool {
	if len(values) != len(expected) {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range expected {
		if !seen[value] {
			return false
		}
	}
	return len(seen) == len(values)
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
