// Package catalog loads and validates pinned managed content declarations.
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"rocmplete/internal/config"
	"rocmplete/internal/platform"
)

const SchemaVersion = 24

var (
	identifierPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	revisionPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	architecturePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

var selectorGroups = map[string]bool{
	"all": true, "comfyui": true, "qwen": true, "wan": true,
	"ltx": true, "ltx-camera": true, "hunyuan": true, "krea": true,
	"llama": true, "dwarfstar": true,
}

type License struct {
	SPDX               string
	Status             string
	URL                string
	Warning            string
	UpstreamRepository string
	UpstreamLicense    string
	UpstreamLicenseURL string
}

type Agreement struct {
	ID      string
	Name    string
	URL     string
	Summary string
}

type Source struct {
	Provider       string
	Repository     string
	Revision       string
	Path           string
	ModelID        int64
	ModelVersionID int64
	RequiresAuth   bool
	ProviderHost   string
	DownloadURL    string
	ArchiveMember  string
	ArchiveMaxSize int64
}

type Artifact struct {
	ID          string
	Description string
	Source      Source
	Destination string
	Size        int64
	SHA256      string
	License     License
	Agreements  []string
	Target      string
}

type Bundle struct {
	ID          string
	Description string
	Application string
	Artifacts   []string
	Workflow    string
	Groups      []string
}

type Workflow struct {
	ID             string
	Description    string
	Destination    string
	SourcePackage  string
	SourceVersion  string
	SourceRevision string
	SourceResource string
	SourceSHA256   string
	RenderedSHA256 string
	Renderer       string
	License        string
	LicenseURL     string
}

type Benchmark struct {
	Bundle         string
	Resource       string
	SHA256         string
	Renderer       string
	RenderedSHA256 string
}

type SamplingPolicy struct {
	ID          string
	Thinking    map[string]any
	NonThinking map[string]any
}

type LlamaPreset struct {
	ID                           string
	Bundle                       string
	Artifact                     string
	DefaultContext               int64
	SpeculativeType              string
	DraftTokens                  int64
	DraftTokensByBackend         map[string]int64
	DraftArtifact                string
	ContextOverrideArchitectures []string
	Jinja                        bool
	AgentTools                   bool
	ReasoningControl             string
	ReasoningLevels              []string
	ReasoningDefault             string
	ReasoningOff                 bool
	ReasoningPreserve            bool
	ChatTemplate                 string
	SamplingPolicy               string
	FlashAttention               map[string]string
	KVCache                      map[string]string
}

func (preset LlamaPreset) DraftTokensForBackend(backend string) int64 {
	if value, ok := preset.DraftTokensByBackend[backend]; ok {
		return value
	}
	return preset.DraftTokens
}

type Catalog struct {
	Agreements       map[string]Agreement
	Artifacts        map[string]Artifact
	Bundles          map[string]Bundle
	Workflows        map[string]Workflow
	Benchmarks       map[string]Benchmark
	SamplingPolicies map[string]SamplingPolicy
	LlamaPresets     map[string]LlamaPreset
}

func (catalog Catalog) BundleSize(bundle Bundle) int64 {
	unique := make(map[string]int64)
	for _, identifier := range bundle.Artifacts {
		artifact := catalog.Artifacts[identifier]
		unique[artifact.SHA256] = artifact.Size
	}
	var total int64
	for _, size := range unique {
		total += size
	}
	return total
}

func (catalog Catalog) BundleAgreements(bundle Bundle) []Agreement {
	seen := make(map[string]bool)
	var result []Agreement
	for _, artifactID := range bundle.Artifacts {
		for _, agreementID := range catalog.Artifacts[artifactID].Agreements {
			if !seen[agreementID] {
				seen[agreementID] = true
				result = append(result, catalog.Agreements[agreementID])
			}
		}
	}
	return result
}

type document struct {
	SchemaVersion    int                        `json:"schema_version"`
	Agreements       map[string]json.RawMessage `json:"agreements"`
	Artifacts        map[string]json.RawMessage `json:"artifacts"`
	ArchiveGroups    map[string]json.RawMessage `json:"archive_collections"`
	Bundles          map[string]json.RawMessage `json:"bundles"`
	Workflows        map[string]json.RawMessage `json:"workflows"`
	Benchmarks       map[string]json.RawMessage `json:"benchmarks"`
	SamplingPolicies map[string]json.RawMessage `json:"llama_sampling_policies"`
	LlamaPresets     map[string]json.RawMessage `json:"llama_presets"`
}

type rawAgreement struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
}

type rawArchive struct {
	Member  string `json:"member"`
	MaxSize int64  `json:"max_size"`
}

type rawSource struct {
	Provider       string      `json:"provider"`
	Repository     string      `json:"repository"`
	Revision       string      `json:"revision"`
	Path           string      `json:"path"`
	ModelID        int64       `json:"model_id"`
	ModelVersionID int64       `json:"model_version_id"`
	RequiresAuth   bool        `json:"requires_auth"`
	Host           string      `json:"host"`
	DownloadURL    string      `json:"download_url"`
	Filename       string      `json:"filename"`
	Archive        *rawArchive `json:"archive"`
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

type rawArtifact struct {
	Description string     `json:"description"`
	Source      rawSource  `json:"source"`
	Destination string     `json:"destination"`
	Size        int64      `json:"size"`
	SHA256      string     `json:"sha256"`
	License     rawLicense `json:"license"`
	Agreements  []string   `json:"agreements"`
	Target      string     `json:"target"`
}

type rawArchiveMember struct {
	Description string `json:"description"`
	Member      string `json:"member"`
	Destination string `json:"destination"`
	Target      string `json:"target"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type rawArchiveCollection struct {
	Source     rawSource                   `json:"source"`
	Archive    rawArchive                  `json:"archive"`
	License    rawLicense                  `json:"license"`
	Agreements []string                    `json:"agreements"`
	Target     string                      `json:"target"`
	Members    map[string]rawArchiveMember `json:"-"`
	RawMembers map[string]json.RawMessage  `json:"members"`
}

type rawBundle struct {
	Description string   `json:"description"`
	Application string   `json:"application"`
	Artifacts   []string `json:"artifacts"`
	Workflow    string   `json:"workflow"`
	Groups      []string `json:"groups"`
}

type rawWorkflow struct {
	Description    string `json:"description"`
	Destination    string `json:"destination"`
	SourcePackage  string `json:"source_package"`
	SourceVersion  string `json:"source_version"`
	SourceRevision string `json:"source_revision"`
	SourceResource string `json:"source_resource"`
	SourceSHA256   string `json:"source_sha256"`
	RenderedSHA256 string `json:"rendered_sha256"`
	Renderer       string `json:"renderer"`
	License        string `json:"license"`
	LicenseURL     string `json:"license_url"`
}

type rawBenchmark struct {
	Resource       string `json:"resource"`
	SHA256         string `json:"sha256"`
	Renderer       string `json:"renderer"`
	RenderedSHA256 string `json:"rendered_sha256"`
}

type rawSamplingPolicy struct {
	Thinking    map[string]any `json:"thinking"`
	NonThinking map[string]any `json:"non_thinking"`
}

type rawLlamaPreset struct {
	Bundle                       string            `json:"bundle"`
	Artifact                     string            `json:"artifact"`
	DefaultContext               int64             `json:"default_context"`
	SpeculativeType              string            `json:"speculative_type"`
	DraftTokens                  int64             `json:"draft_tokens"`
	DraftTokensByBackend         map[string]int64  `json:"draft_tokens_by_backend"`
	DraftArtifact                string            `json:"draft_artifact"`
	ContextOverrideArchitectures []string          `json:"context_override_architectures"`
	Jinja                        bool              `json:"jinja"`
	AgentTools                   bool              `json:"agent_tools"`
	ReasoningControl             string            `json:"reasoning_control"`
	ReasoningLevels              []string          `json:"reasoning_levels"`
	ReasoningDefault             *string           `json:"reasoning_default"`
	ReasoningOff                 bool              `json:"reasoning_off"`
	ReasoningPreserve            bool              `json:"reasoning_preserve"`
	ChatTemplate                 string            `json:"chat_template"`
	SamplingPolicy               string            `json:"sampling_policy"`
	FlashAttention               map[string]string `json:"flash_attention"`
	KVCache                      map[string]string `json:"kv_cache"`
}

func Load(file string) (Catalog, error) {
	contents, err := os.ReadFile(file)
	if err != nil {
		return Catalog{}, fmt.Errorf("load catalog %s: %w", file, err)
	}
	var raw document
	if err := decodeStrict(contents, &raw); err != nil {
		return Catalog{}, fmt.Errorf("load catalog %s: %w", file, err)
	}
	if raw.SchemaVersion != SchemaVersion {
		return Catalog{}, fmt.Errorf("unsupported catalog schema %d", raw.SchemaVersion)
	}
	collections := []any{raw.Agreements, raw.Artifacts, raw.ArchiveGroups, raw.Bundles, raw.Workflows, raw.Benchmarks, raw.SamplingPolicies, raw.LlamaPresets}
	for _, collection := range collections {
		if collection == nil {
			return Catalog{}, fmt.Errorf("catalog collections must be objects")
		}
	}
	catalog := Catalog{
		Agreements: make(map[string]Agreement), Artifacts: make(map[string]Artifact),
		Bundles: make(map[string]Bundle), Workflows: make(map[string]Workflow),
		Benchmarks: make(map[string]Benchmark), SamplingPolicies: make(map[string]SamplingPolicy),
		LlamaPresets: make(map[string]LlamaPreset),
	}
	for id, value := range raw.Agreements {
		if err := validIdentifier(id, "agreement"); err != nil {
			return Catalog{}, err
		}
		var entry rawAgreement
		if err := decodeStrict(value, &entry); err != nil {
			return Catalog{}, fmt.Errorf("agreement %s: %w", id, err)
		}
		if entry.Name == "" || entry.Summary == "" || !isHTTPS(entry.URL) {
			return Catalog{}, fmt.Errorf("agreement %s requires a name, HTTPS URL, and summary", id)
		}
		catalog.Agreements[id] = Agreement{ID: id, Name: entry.Name, URL: entry.URL, Summary: entry.Summary}
	}
	for id, value := range raw.Artifacts {
		artifact, err := loadArtifact(id, value)
		if err != nil {
			return Catalog{}, err
		}
		catalog.Artifacts[id] = artifact
	}
	for collectionID, value := range raw.ArchiveGroups {
		expanded, err := expandArchiveCollection(collectionID, value)
		if err != nil {
			return Catalog{}, err
		}
		for id, artifact := range expanded {
			if _, duplicate := catalog.Artifacts[id]; duplicate {
				return Catalog{}, fmt.Errorf("duplicate artifact identifier %q", id)
			}
			catalog.Artifacts[id] = artifact
		}
	}
	for id, value := range raw.Bundles {
		bundle, err := loadBundle(id, value)
		if err != nil {
			return Catalog{}, err
		}
		catalog.Bundles[id] = bundle
	}
	for id, value := range raw.Workflows {
		workflow, err := loadWorkflow(id, value)
		if err != nil {
			return Catalog{}, err
		}
		catalog.Workflows[id] = workflow
	}
	for id, value := range raw.Benchmarks {
		benchmark, err := loadBenchmark(id, value)
		if err != nil {
			return Catalog{}, err
		}
		catalog.Benchmarks[id] = benchmark
	}
	for id, value := range raw.SamplingPolicies {
		policy, err := loadSamplingPolicy(id, value)
		if err != nil {
			return Catalog{}, err
		}
		catalog.SamplingPolicies[id] = policy
	}
	for id, value := range raw.LlamaPresets {
		preset, err := loadLlamaPreset(id, value)
		if err != nil {
			return Catalog{}, err
		}
		catalog.LlamaPresets[id] = preset
	}
	if err := catalog.validateRelationships(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func decodeStrict(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected data after JSON document")
		}
		return err
	}
	return nil
}

func loadArtifact(id string, value json.RawMessage) (Artifact, error) {
	if err := validIdentifier(id, "artifact"); err != nil {
		return Artifact{}, err
	}
	var raw rawArtifact
	if err := decodeStrict(value, &raw); err != nil {
		return Artifact{}, fmt.Errorf("artifact %s: %w", id, err)
	}
	if raw.Description == "" || raw.Size <= 0 || !sha256Pattern.MatchString(raw.SHA256) {
		return Artifact{}, fmt.Errorf("artifact %s requires description, positive size, and lowercase SHA-256", id)
	}
	destination, err := safeRelative(raw.Destination, id+".destination")
	if err != nil {
		return Artifact{}, err
	}
	target := raw.Target
	if target == "" {
		target = "models"
	}
	allowedTarget := target == "models" || target == "llama-models" || target == "dwarfstar-models" || target == "workflows"
	if !allowedTarget {
		return Artifact{}, fmt.Errorf("artifact %s has unsupported target %q", id, target)
	}
	if target == "workflows" && !strings.HasSuffix(destination, ".json") {
		return Artifact{}, fmt.Errorf("workflow artifact %s destination must end in .json", id)
	}
	source, err := loadSource(id, raw.Source)
	if err != nil {
		return Artifact{}, err
	}
	license, err := loadLicense(id, raw.License)
	if err != nil {
		return Artifact{}, err
	}
	seen := make(map[string]bool)
	for _, agreement := range raw.Agreements {
		if err := validIdentifier(agreement, id+" agreement"); err != nil {
			return Artifact{}, err
		}
		if seen[agreement] {
			return Artifact{}, fmt.Errorf("artifact %s contains duplicate agreement %s", id, agreement)
		}
		seen[agreement] = true
	}
	return Artifact{ID: id, Description: raw.Description, Source: source, Destination: destination, Size: raw.Size, SHA256: raw.SHA256, License: license, Agreements: append([]string(nil), raw.Agreements...), Target: target}, nil
}

func loadSource(id string, raw rawSource) (Source, error) {
	provider := raw.Provider
	if provider == "" {
		provider = "huggingface"
	}
	if provider == "huggingface" {
		if raw.Repository == "" || !revisionPattern.MatchString(raw.Revision) {
			return Source{}, fmt.Errorf("artifact %s Hugging Face source requires repository and full commit revision", id)
		}
		sourcePath, err := safeRelative(raw.Path, id+".source.path")
		if err != nil {
			return Source{}, err
		}
		return Source{Provider: provider, Repository: raw.Repository, Revision: raw.Revision, Path: sourcePath}, nil
	}
	if provider != "civitai" {
		return Source{}, fmt.Errorf("artifact %s has unsupported source provider %q", id, provider)
	}
	if raw.ModelID <= 0 || raw.ModelVersionID <= 0 {
		return Source{}, fmt.Errorf("artifact %s Civitai source requires positive model IDs", id)
	}
	host := raw.Host
	if host == "" {
		host = "civitai.com"
	}
	if host != "civitai.com" && host != "civitai.red" {
		return Source{}, fmt.Errorf("artifact %s has unsupported Civitai host %q", id, host)
	}
	filename, err := safeRelative(raw.Filename, id+".source.filename")
	if err != nil || path.Base(filename) != filename {
		return Source{}, fmt.Errorf("artifact %s Civitai filename must not contain directories", id)
	}
	downloadURL := raw.DownloadURL
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("https://%s/api/download/models/%d", host, raw.ModelVersionID)
	}
	parsed, parseErr := url.Parse(downloadURL)
	if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() != host || parsed.User != nil || parsed.Port() != "" || parsed.Path != fmt.Sprintf("/api/download/models/%d", raw.ModelVersionID) || parsed.Fragment != "" {
		return Source{}, fmt.Errorf("artifact %s has invalid Civitai download URL", id)
	}
	source := Source{Provider: provider, Repository: fmt.Sprintf("%s/models/%d", host, raw.ModelID), Revision: fmt.Sprint(raw.ModelVersionID), Path: filename, ModelID: raw.ModelID, ModelVersionID: raw.ModelVersionID, RequiresAuth: raw.RequiresAuth, ProviderHost: host, DownloadURL: downloadURL}
	if raw.Archive != nil {
		member, memberErr := safeRelative(raw.Archive.Member, id+".source.archive.member")
		if memberErr != nil || raw.Archive.MaxSize <= 0 {
			return Source{}, fmt.Errorf("artifact %s archive requires safe member and positive max_size", id)
		}
		source.ArchiveMember = member
		source.ArchiveMaxSize = raw.Archive.MaxSize
	}
	return source, nil
}

func loadLicense(id string, raw rawLicense) (License, error) {
	if raw.Status != "verified" && raw.Status != "unverified" {
		return License{}, fmt.Errorf("artifact %s license status must be verified or unverified", id)
	}
	if raw.SPDX == "" || !isHTTPS(raw.URL) {
		return License{}, fmt.Errorf("artifact %s license requires SPDX and HTTPS URL", id)
	}
	if raw.Status == "verified" && (raw.SPDX == "NOASSERTION" || raw.Warning != "") {
		return License{}, fmt.Errorf("verified artifact %s license cannot use NOASSERTION or warning", id)
	}
	if raw.Status == "unverified" && (raw.SPDX != "NOASSERTION" || raw.Warning == "" || raw.UpstreamRepository == "" || raw.UpstreamLicense == "") {
		return License{}, fmt.Errorf("unverified artifact %s license requires NOASSERTION, warning, and upstream lineage", id)
	}
	if raw.UpstreamLicenseURL != "" && !isHTTPS(raw.UpstreamLicenseURL) {
		return License{}, fmt.Errorf("artifact %s upstream license URL must use HTTPS", id)
	}
	return License{SPDX: raw.SPDX, Status: raw.Status, URL: raw.URL, Warning: raw.Warning, UpstreamRepository: raw.UpstreamRepository, UpstreamLicense: raw.UpstreamLicense, UpstreamLicenseURL: raw.UpstreamLicenseURL}, nil
}

func expandArchiveCollection(id string, value json.RawMessage) (map[string]Artifact, error) {
	if err := validIdentifier(id, "archive collection"); err != nil {
		return nil, err
	}
	var raw rawArchiveCollection
	if err := decodeStrict(value, &raw); err != nil {
		return nil, fmt.Errorf("archive collection %s: %w", id, err)
	}
	if len(raw.RawMembers) == 0 || raw.Archive.MaxSize <= 0 {
		return nil, fmt.Errorf("archive collection %s requires members and positive max_size", id)
	}
	result := make(map[string]Artifact)
	for memberID, memberValue := range raw.RawMembers {
		var member rawArchiveMember
		if err := decodeStrict(memberValue, &member); err != nil {
			return nil, fmt.Errorf("archive collection %s member %s: %w", id, memberID, err)
		}
		source := raw.Source
		source.Archive = &rawArchive{Member: member.Member, MaxSize: raw.Archive.MaxSize}
		target := member.Target
		if target == "" {
			target = raw.Target
		}
		artifactRaw := rawArtifact{Description: member.Description, Source: source, Destination: member.Destination, Size: member.Size, SHA256: member.SHA256, License: raw.License, Agreements: raw.Agreements, Target: target}
		encoded, err := json.Marshal(artifactRaw)
		if err != nil {
			return nil, err
		}
		artifact, err := loadArtifact(memberID, encoded)
		if err != nil {
			return nil, err
		}
		result[memberID] = artifact
	}
	return result, nil
}

func loadBundle(id string, value json.RawMessage) (Bundle, error) {
	if err := validIdentifier(id, "bundle"); err != nil {
		return Bundle{}, err
	}
	var raw rawBundle
	if err := decodeStrict(value, &raw); err != nil {
		return Bundle{}, fmt.Errorf("bundle %s: %w", id, err)
	}
	if raw.Description == "" || len(raw.Artifacts) == 0 {
		return Bundle{}, fmt.Errorf("bundle %s requires description and artifacts", id)
	}
	if _, ok := config.ApplicationByID(raw.Application); !ok {
		return Bundle{}, fmt.Errorf("bundle %s has unknown application %q", id, raw.Application)
	}
	if err := uniqueIdentifiers(raw.Artifacts, id+" artifacts"); err != nil {
		return Bundle{}, err
	}
	if raw.Workflow != "" {
		if err := validIdentifier(raw.Workflow, id+" workflow"); err != nil {
			return Bundle{}, err
		}
	}
	seenGroups := make(map[string]bool)
	for _, group := range raw.Groups {
		if !selectorGroups[group] {
			return Bundle{}, fmt.Errorf("bundle %s has unsupported group %q", id, group)
		}
		if seenGroups[group] {
			return Bundle{}, fmt.Errorf("bundle %s contains duplicate group %q", id, group)
		}
		seenGroups[group] = true
	}
	return Bundle{ID: id, Description: raw.Description, Application: raw.Application, Artifacts: append([]string(nil), raw.Artifacts...), Workflow: raw.Workflow, Groups: append([]string(nil), raw.Groups...)}, nil
}

func loadWorkflow(id string, value json.RawMessage) (Workflow, error) {
	if err := validIdentifier(id, "workflow"); err != nil {
		return Workflow{}, err
	}
	var raw rawWorkflow
	if err := decodeStrict(value, &raw); err != nil {
		return Workflow{}, fmt.Errorf("workflow %s: %w", id, err)
	}
	destination, err := safeRelative(raw.Destination, id+".destination")
	if err != nil || !strings.HasSuffix(destination, ".json") {
		return Workflow{}, fmt.Errorf("workflow %s destination must be a safe .json path", id)
	}
	resource, err := safeRelative(raw.SourceResource, id+".source_resource")
	if err != nil {
		return Workflow{}, err
	}
	if raw.Description == "" || raw.SourcePackage == "" || raw.SourceVersion == "" || !revisionPattern.MatchString(raw.SourceRevision) || !sha256Pattern.MatchString(raw.SourceSHA256) || !sha256Pattern.MatchString(raw.RenderedSHA256) || raw.Renderer == "" || raw.License == "" || !isHTTPS(raw.LicenseURL) {
		return Workflow{}, fmt.Errorf("workflow %s has invalid provenance", id)
	}
	return Workflow{ID: id, Description: raw.Description, Destination: destination, SourcePackage: raw.SourcePackage, SourceVersion: raw.SourceVersion, SourceRevision: raw.SourceRevision, SourceResource: resource, SourceSHA256: raw.SourceSHA256, RenderedSHA256: raw.RenderedSHA256, Renderer: raw.Renderer, License: raw.License, LicenseURL: raw.LicenseURL}, nil
}

func loadBenchmark(id string, value json.RawMessage) (Benchmark, error) {
	if err := validIdentifier(id, "benchmark bundle"); err != nil {
		return Benchmark{}, err
	}
	var raw rawBenchmark
	if err := decodeStrict(value, &raw); err != nil {
		return Benchmark{}, fmt.Errorf("benchmark %s: %w", id, err)
	}
	resource, err := safeRelative(raw.Resource, id+".resource")
	if err != nil || !sha256Pattern.MatchString(raw.SHA256) {
		return Benchmark{}, fmt.Errorf("benchmark %s requires safe resource and SHA-256", id)
	}
	if raw.Renderer == "" {
		raw.Renderer = "identity"
	}
	if raw.RenderedSHA256 != "" && !sha256Pattern.MatchString(raw.RenderedSHA256) {
		return Benchmark{}, fmt.Errorf("benchmark %s has invalid rendered SHA-256", id)
	}
	return Benchmark{Bundle: id, Resource: resource, SHA256: raw.SHA256, Renderer: raw.Renderer, RenderedSHA256: raw.RenderedSHA256}, nil
}

func loadSamplingPolicy(id string, value json.RawMessage) (SamplingPolicy, error) {
	if err := validIdentifier(id, "sampling policy"); err != nil {
		return SamplingPolicy{}, err
	}
	var raw rawSamplingPolicy
	if err := decodeStrict(value, &raw); err != nil {
		return SamplingPolicy{}, fmt.Errorf("sampling policy %s: %w", id, err)
	}
	for mode, settings := range map[string]map[string]any{"thinking": raw.Thinking, "non_thinking": raw.NonThinking} {
		if len(settings) == 0 {
			return SamplingPolicy{}, fmt.Errorf("sampling policy %s requires %s settings", id, mode)
		}
		allowed := map[string]bool{"temperature": true, "top_p": true, "top_k": true, "min_p": true, "presence_penalty": true, "repeat_penalty": true}
		for key, value := range settings {
			if !allowed[key] {
				return SamplingPolicy{}, fmt.Errorf("sampling policy %s has unsupported %s setting %q", id, mode, key)
			}
			number, ok := numeric(value)
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
				return SamplingPolicy{}, fmt.Errorf("sampling policy %s %s.%s must be numeric", id, mode, key)
			}
		}
	}
	return SamplingPolicy{ID: id, Thinking: raw.Thinking, NonThinking: raw.NonThinking}, nil
}

func loadLlamaPreset(id string, value json.RawMessage) (LlamaPreset, error) {
	if err := validIdentifier(id, "llama.cpp preset"); err != nil {
		return LlamaPreset{}, err
	}
	var raw rawLlamaPreset
	if err := decodeStrict(value, &raw); err != nil {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s: %w", id, err)
	}
	if raw.DefaultContext <= 0 || raw.DefaultContext%1024 != 0 {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s default_context must be a positive multiple of 1024", id)
	}
	if raw.SpeculativeType != "" && raw.SpeculativeType != "draft-mtp" && raw.SpeculativeType != "draft-dflash" {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s has unsupported speculative_type", id)
	}
	if raw.DraftTokens < 0 {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s draft_tokens cannot be negative", id)
	}
	for backend, tokens := range raw.DraftTokensByBackend {
		if (backend != string(platform.BackendROCm) && backend != string(platform.BackendVulkan)) || tokens <= 0 {
			return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s has invalid backend draft tokens", id)
		}
	}
	seenArchitectures := make(map[string]bool)
	for _, architecture := range raw.ContextOverrideArchitectures {
		if !architecturePattern.MatchString(architecture) {
			return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s has unsupported context architecture %q", id, architecture)
		}
		if seenArchitectures[architecture] {
			return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s contains duplicate context architecture %q", id, architecture)
		}
		seenArchitectures[architecture] = true
	}
	if raw.ReasoningControl != "" && raw.ReasoningControl != "toggle" && raw.ReasoningControl != "effort" && raw.ReasoningControl != "strength" {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s has unsupported reasoning_control", id)
	}
	if err := uniqueIdentifiers(raw.ReasoningLevels, id+" reasoning levels"); err != nil {
		return LlamaPreset{}, err
	}
	reasoningDefault := "off"
	if raw.ReasoningControl == "toggle" {
		reasoningDefault = "on"
	} else if len(raw.ReasoningLevels) > 0 {
		reasoningDefault = raw.ReasoningLevels[0]
	}
	if raw.ReasoningDefault != nil {
		reasoningDefault = *raw.ReasoningDefault
	}
	if reasoningDefault != "off" && reasoningDefault != "on" && !contains(raw.ReasoningLevels, reasoningDefault) {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s has invalid reasoning_default", id)
	}
	if raw.ReasoningControl == "toggle" && (len(raw.ReasoningLevels) != 0 || reasoningDefault != "on" || !raw.ReasoningOff) {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s has inconsistent toggle reasoning settings", id)
	}
	if (raw.ReasoningControl == "effort" || raw.ReasoningControl == "strength") && len(raw.ReasoningLevels) == 0 {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s reasoning levels are required", id)
	}
	if raw.ReasoningPreserve && !raw.AgentTools {
		return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s reasoning_preserve requires agent_tools", id)
	}
	for profile, setting := range raw.FlashAttention {
		if _, ok := platform.LookupProfile(profile); !ok || (setting != "on" && setting != "off" && setting != "auto") {
			return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s has invalid flash_attention policy", id)
		}
	}
	for profile, setting := range raw.KVCache {
		if _, ok := platform.LookupProfile(profile); !ok || (setting != "f16" && setting != "q8_0" && setting != "q4_0") {
			return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s has invalid kv_cache policy", id)
		}
		if setting != "f16" && raw.FlashAttention[profile] != "on" {
			return LlamaPreset{}, fmt.Errorf("llama.cpp preset %s quantized KV cache requires flash attention", id)
		}
	}
	return LlamaPreset{ID: id, Bundle: raw.Bundle, Artifact: raw.Artifact, DefaultContext: raw.DefaultContext, SpeculativeType: raw.SpeculativeType, DraftTokens: raw.DraftTokens, DraftTokensByBackend: cloneMap(raw.DraftTokensByBackend), DraftArtifact: raw.DraftArtifact, ContextOverrideArchitectures: append([]string(nil), raw.ContextOverrideArchitectures...), Jinja: raw.Jinja, AgentTools: raw.AgentTools, ReasoningControl: raw.ReasoningControl, ReasoningLevels: append([]string(nil), raw.ReasoningLevels...), ReasoningDefault: reasoningDefault, ReasoningOff: raw.ReasoningOff, ReasoningPreserve: raw.ReasoningPreserve, ChatTemplate: raw.ChatTemplate, SamplingPolicy: raw.SamplingPolicy, FlashAttention: cloneMap(raw.FlashAttention), KVCache: cloneMap(raw.KVCache)}, nil
}

func (catalog Catalog) validateRelationships() error {
	destinations := make(map[string]string)
	blobs := make(map[string]int64)
	for id, artifact := range catalog.Artifacts {
		for _, agreement := range artifact.Agreements {
			if _, ok := catalog.Agreements[agreement]; !ok {
				return fmt.Errorf("artifact %s references unknown agreement %s", id, agreement)
			}
		}
		key := artifact.Target + "\x00" + artifact.Destination
		if previous, duplicate := destinations[key]; duplicate {
			return fmt.Errorf("artifacts %s and %s share destination %s", previous, id, artifact.Destination)
		}
		destinations[key] = id
		if previous, duplicate := blobs[artifact.SHA256]; duplicate && previous != artifact.Size {
			return fmt.Errorf("SHA-256 %s has inconsistent sizes", artifact.SHA256)
		}
		blobs[artifact.SHA256] = artifact.Size
	}
	for id, bundle := range catalog.Bundles {
		for _, artifact := range bundle.Artifacts {
			if _, ok := catalog.Artifacts[artifact]; !ok {
				return fmt.Errorf("bundle %s references unknown artifact %s", id, artifact)
			}
		}
		if bundle.Workflow != "" {
			if _, ok := catalog.Workflows[bundle.Workflow]; !ok {
				return fmt.Errorf("bundle %s references unknown workflow %s", id, bundle.Workflow)
			}
		}
		if bundle.Application == "dwarfstar" {
			if bundle.Workflow != "" || len(bundle.Artifacts) > 2 {
				return fmt.Errorf("DwarfStar bundle %s must contain one target and at most one support artifact", id)
			}
			for _, artifactID := range bundle.Artifacts {
				artifact := catalog.Artifacts[artifactID]
				if artifact.Target != "dwarfstar-models" || !strings.HasSuffix(strings.ToLower(artifact.Destination), ".gguf") {
					return fmt.Errorf("DwarfStar bundle %s must use dwarfstar-models GGUF artifacts", id)
				}
			}
		}
	}
	for id := range catalog.Benchmarks {
		if _, ok := catalog.Bundles[id]; !ok {
			return fmt.Errorf("benchmark references unknown bundle %s", id)
		}
	}
	for id, bundle := range catalog.Bundles {
		if bundle.Workflow != "" {
			if _, ok := catalog.Benchmarks[id]; !ok {
				return fmt.Errorf("workflow bundle %s has no managed benchmark", id)
			}
		}
	}
	for id, preset := range catalog.LlamaPresets {
		bundle, ok := catalog.Bundles[preset.Bundle]
		if !ok || bundle.Application != "llama-cpp" {
			return fmt.Errorf("llama.cpp preset %s references an invalid bundle", id)
		}
		artifact, ok := catalog.Artifacts[preset.Artifact]
		if !ok || !contains(bundle.Artifacts, preset.Artifact) || artifact.Target != "llama-models" || !strings.HasSuffix(strings.ToLower(artifact.Destination), ".gguf") {
			return fmt.Errorf("llama.cpp preset %s references an invalid model artifact", id)
		}
		if preset.DraftArtifact != "" {
			draft, exists := catalog.Artifacts[preset.DraftArtifact]
			if !exists || preset.DraftArtifact == preset.Artifact || !contains(bundle.Artifacts, preset.DraftArtifact) || draft.Target != "llama-models" || !strings.HasSuffix(strings.ToLower(draft.Destination), ".gguf") {
				return fmt.Errorf("llama.cpp preset %s references an invalid draft artifact", id)
			}
		}
		if preset.SamplingPolicy != "" {
			if _, ok := catalog.SamplingPolicies[preset.SamplingPolicy]; !ok {
				return fmt.Errorf("llama.cpp preset %s references unknown sampling policy %s", id, preset.SamplingPolicy)
			}
		}
	}
	return nil
}

func validIdentifier(value, field string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s has invalid identifier %q", field, value)
	}
	return nil
}

func uniqueIdentifiers(values []string, field string) error {
	seen := make(map[string]bool)
	for _, value := range values {
		if err := validIdentifier(value, field); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("%s contains duplicate %q", field, value)
		}
		seen[value] = true
	}
	return nil
}

func safeRelative(value, field string) (string, error) {
	if value == "" || strings.HasPrefix(value, "/") || path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") {
		return "", fmt.Errorf("%s must be a safe relative path", field)
	}
	return value, nil
}

func isHTTPS(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	if source == nil {
		return map[K]V{}
	}
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func numeric(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func SortedBundleIDs(catalog Catalog) []string {
	identifiers := make([]string, 0, len(catalog.Bundles))
	for identifier := range catalog.Bundles {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	return identifiers
}
