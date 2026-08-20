// Package remoteimport resolves allowlisted provider metadata into one pinned
// local artifact. It never turns arbitrary URLs into downloader inputs.
package remoteimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/identity"
)

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var unsafeSlug = regexp.MustCompile(`[^a-z0-9._-]+`)

var weightSuffixes = []string{".safetensors", ".ckpt", ".pt", ".pth", ".bin"}

type Kind struct {
	ID, Label, Application, Target, Prefix string
	Suffixes                               []string
}

var Kinds = map[string]Kind{
	"comfyui:checkpoint":      {"comfyui:checkpoint", "ComfyUI checkpoint", "comfyui", "models", "checkpoints/imported", []string{".safetensors", ".ckpt"}},
	"comfyui:diffusion-model": {"comfyui:diffusion-model", "ComfyUI diffusion model", "comfyui", "models", "diffusion_models/imported", weightSuffixes},
	"comfyui:lora":            {"comfyui:lora", "ComfyUI LoRA", "comfyui", "models", "loras/imported", weightSuffixes},
	"comfyui:vae":             {"comfyui:vae", "ComfyUI VAE", "comfyui", "models", "vae/imported", weightSuffixes},
	"comfyui:text-encoder":    {"comfyui:text-encoder", "ComfyUI text encoder", "comfyui", "models", "text_encoders/imported", weightSuffixes},
	"comfyui:controlnet":      {"comfyui:controlnet", "ComfyUI ControlNet model", "comfyui", "models", "controlnet/imported", weightSuffixes},
	"comfyui:upscaler":        {"comfyui:upscaler", "ComfyUI upscaler", "comfyui", "models", "upscale_models/imported", weightSuffixes},
	"comfyui:workflow":        {"comfyui:workflow", "exact imported ComfyUI workflow", "comfyui", "workflows", "remote", []string{".json"}},
	"llama-cpp:model":         {"llama-cpp:model", "llama.cpp GGUF model", "llama-cpp", "llama-models", "imported", []string{".gguf"}},
}

type File struct {
	ID, Name    string
	Size        int64
	SHA256      string
	Primary     bool
	DownloadURL string
}
type Discovery struct {
	Provider, SourceURL, Title, Repository, Revision, DeclaredLicense, ModelType, Host string
	Files                                                                              []File
	ModelID, VersionID                                                                 int64
	RequiresAuth                                                                       bool
}
type Plan struct {
	Discovery Discovery
	File      File
	Kind      Kind
	Artifact  catalog.Artifact
	Bundle    catalog.Bundle
	Pack      map[string]any
}

func Provider(raw string) (string, error) {
	parsed, err := normalizedURL(raw)
	if err != nil {
		return "", err
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	switch host {
	case "civitai.com", "civitai.red":
		return "civitai", nil
	case "huggingface.co":
		return "huggingface", nil
	default:
		return "", fmt.Errorf("unsupported import host %q", host)
	}
}

func Discover(ctx context.Context, raw string, version int64, hfToken, civitaiToken string) (Discovery, error) {
	provider, err := Provider(raw)
	if err != nil {
		return Discovery{}, err
	}
	if provider == "civitai" {
		return discoverCivitai(ctx, raw, version, civitaiToken)
	}
	return discoverHF(ctx, raw, hfToken)
}

func CivitaiVersions(ctx context.Context, raw, token string) ([][2]string, error) {
	host, model, version, err := civitaiIdentity(raw)
	if err != nil {
		return nil, err
	}
	if version > 0 {
		return [][2]string{{fmt.Sprint(version), "version from URL"}}, nil
	}
	if model == 0 {
		return nil, nil
	}
	var metadata map[string]any
	if err := requestJSON(ctx, "https://"+host+"/api/v1/models/"+fmt.Sprint(model), token, "Civitai", &metadata); err != nil {
		return nil, err
	}
	values, _ := metadata["modelVersions"].([]any)
	var result [][2]string
	for _, rawValue := range values {
		value, _ := rawValue.(map[string]any)
		id, ok := jsonInt(value["id"])
		if !ok {
			continue
		}
		description, stringOK := value["name"].(string)
		if !stringOK || description == "" {
			description = "unnamed version"
		}
		if base, ok := value["baseModel"].(string); ok && base != "" {
			description += " — " + base
		}
		result = append(result, [2]string{fmt.Sprint(id), description})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Civitai model has no selectable versions")
	}
	return result, nil
}

func CompatibleKinds(file File) []Kind {
	var result []Kind
	ids := make([]string, 0, len(Kinds))
	for id := range Kinds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		kind := Kinds[id]
		if hasSuffix(file.Name, kind.Suffixes) {
			result = append(result, kind)
		}
	}
	return result
}

func CandidateKinds(discovery Discovery, file File) []Kind {
	compatible := CompatibleKinds(file)
	if strings.HasSuffix(strings.ToLower(file.Name), ".gguf") || discovery.Provider != "civitai" {
		return compatible
	}
	typeKey := strings.ReplaceAll(strings.ToLower(discovery.ModelType), " ", "")
	mapping := map[string][]string{"checkpoint": {"comfyui:checkpoint", "comfyui:diffusion-model"}, "lora": {"comfyui:lora"}, "locon": {"comfyui:lora"}, "dora": {"comfyui:lora"}, "controlnet": {"comfyui:controlnet"}, "vae": {"comfyui:vae"}, "upscaler": {"comfyui:upscaler"}, "workflows": {"comfyui:workflow"}, "workflow": {"comfyui:workflow"}}
	if containsString([]string{"aestheticgradient", "embedding", "hypernetwork", "motionmodule", "poses", "textualinversion", "wildcards"}, typeKey) {
		return nil
	}
	ids, ok := mapping[typeKey]
	if !ok {
		return compatible
	}
	allowed := make(map[string]bool)
	for _, kind := range compatible {
		allowed[kind.ID] = true
	}
	var result []Kind
	for _, id := range ids {
		if allowed[id] {
			result = append(result, Kinds[id])
		}
	}
	if typeKey == "checkpoint" && !allowed["comfyui:checkpoint"] {
		return nil
	}
	return result
}

func SelectFile(discovery Discovery, selector string) (File, error) {
	var matches []File
	for _, file := range discovery.Files {
		if file.ID == selector || file.Name == selector {
			matches = append(matches, file)
		}
	}
	if len(matches) != 1 {
		return File{}, fmt.Errorf("remote file %q did not select exactly one file", selector)
	}
	return matches[0], nil
}

func AutomaticFile(discovery Discovery) (File, bool) {
	if len(discovery.Files) == 1 {
		return discovery.Files[0], true
	}
	var values []File
	for _, file := range discovery.Files {
		if file.Primary {
			values = append(values, file)
		}
	}
	if len(values) == 1 {
		return values[0], true
	}
	return File{}, false
}

func SelectKind(identifier string, file File) (Kind, error) {
	kind, ok := Kinds[identifier]
	if !ok {
		return Kind{}, fmt.Errorf("unknown import type %q", identifier)
	}
	if !hasSuffix(file.Name, kind.Suffixes) {
		return Kind{}, fmt.Errorf("%s cannot install %q", kind.Label, file.Name)
	}
	return kind, nil
}

func BuildPlan(discovery Discovery, file File, kind Kind) (Plan, error) {
	if err := validatePlanInputs(discovery, file); err != nil {
		return Plan{}, err
	}
	compatible := false
	for _, item := range CompatibleKinds(file) {
		compatible = compatible || item.ID == kind.ID
	}
	if !compatible {
		return Plan{}, fmt.Errorf("%s cannot install %q", kind.Label, file.Name)
	}
	sourceIdentity := ""
	source := catalog.Source{}
	rawSource := map[string]any{}
	if discovery.Provider == "civitai" {
		sourceIdentity = fmt.Sprintf("civitai-v%d-f%s", discovery.VersionID, file.ID)
		source = catalog.Source{Provider: "civitai", ProviderHost: discovery.Host, ModelID: discovery.ModelID, ModelVersionID: discovery.VersionID, Path: file.Name, DownloadURL: file.DownloadURL, RequiresAuth: discovery.RequiresAuth}
		rawSource = map[string]any{"provider": "civitai", "host": discovery.Host, "model_id": discovery.ModelID, "model_version_id": discovery.VersionID, "filename": file.Name, "download_url": file.DownloadURL, "requires_auth": discovery.RequiresAuth}
	} else {
		sourceIdentity = "hf-" + slug(strings.ReplaceAll(discovery.Repository, "/", "-"), 32) + "-" + file.SHA256[:12]
		source = catalog.Source{Provider: "huggingface", Repository: discovery.Repository, Revision: discovery.Revision, Path: file.ID}
		rawSource = map[string]any{"repository": discovery.Repository, "revision": discovery.Revision, "path": file.ID}
	}
	id := "import-" + slug(sourceIdentity, 56) + "-" + slug(strings.ReplaceAll(kind.ID, ":", "-"), 32)
	destination := kind.Prefix + "/" + file.Name
	warning := identity.DisplayName + " imported this remote file on request but did not independently verify that its declared permissions cover the hosted bytes."
	license := catalog.License{SPDX: "NOASSERTION", Status: "unverified", URL: discovery.SourceURL, Warning: warning, UpstreamRepository: discovery.Repository, UpstreamLicense: discovery.DeclaredLicense, UpstreamLicenseURL: discovery.SourceURL}
	artifact := catalog.Artifact{ID: id, Description: file.Name + " from " + discovery.Title, Source: source, Target: kind.Target, Destination: destination, Size: file.Size, SHA256: file.SHA256, License: license}
	group := "comfyui"
	if kind.Application == "llama-cpp" {
		group = "llama"
	}
	bundle := catalog.Bundle{ID: id, Description: file.Name + " imported from " + discovery.Provider, Application: kind.Application, Artifacts: []string{id}, Groups: []string{"all", group}}
	pack := map[string]any{"schema_version": 2, "artifacts": map[string]any{id: map[string]any{"description": artifact.Description, "source": rawSource, "target": kind.Target, "destination": destination, "size": file.Size, "sha256": file.SHA256, "license": map[string]any{"spdx": "NOASSERTION", "status": "unverified", "url": discovery.SourceURL, "warning": warning, "upstream_repository": discovery.Repository, "upstream_license": discovery.DeclaredLicense, "upstream_license_url": discovery.SourceURL}}}, "bundles": map[string]any{id: map[string]any{"description": bundle.Description, "application": kind.Application, "artifacts": []string{id}, "groups": bundle.Groups}}}
	return Plan{Discovery: discovery, File: file, Kind: kind, Artifact: artifact, Bundle: bundle, Pack: pack}, nil
}

func PackBytes(plan Plan) ([]byte, error) {
	encoded, err := json.MarshalIndent(plan.Pack, "", "  ")
	return append(encoded, '\n'), err
}

func validatePlanInputs(discovery Discovery, file File) error {
	if discovery.Provider != "civitai" && discovery.Provider != "huggingface" {
		return fmt.Errorf("unsupported remote provider %q", discovery.Provider)
	}
	if _, err := normalizedURL(discovery.SourceURL); err != nil {
		return fmt.Errorf("invalid remote source URL: %w", err)
	}
	if file.Name == "" || path.Base(file.Name) != file.Name || !recognized(file.Name) || file.Size <= 0 || !shaPattern.MatchString(file.SHA256) || file.SHA256 != strings.ToLower(file.SHA256) {
		return fmt.Errorf("remote file metadata is invalid")
	}
	if discovery.Provider == "civitai" {
		fileID, err := strconv.ParseInt(file.ID, 10, 64)
		if err != nil || fileID <= 0 || discovery.ModelID <= 0 || discovery.VersionID <= 0 || discovery.Host != "civitai.com" && discovery.Host != "civitai.red" || !exactCivitaiDownload(file.DownloadURL, discovery.Host, discovery.VersionID) {
			return fmt.Errorf("Civitai discovery metadata is invalid")
		}
		return nil
	}
	parts := strings.Split(discovery.Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || !revisionPattern.MatchString(discovery.Revision) || !safeHFPath(file.ID) {
		return fmt.Errorf("Hugging Face discovery metadata is invalid")
	}
	return nil
}

func discoverCivitai(ctx context.Context, raw string, requested int64, token string) (Discovery, error) {
	host, modelID, urlVersion, err := civitaiIdentity(raw)
	if err != nil {
		return Discovery{}, err
	}
	if requested > 0 && urlVersion > 0 && requested != urlVersion {
		return Discovery{}, fmt.Errorf("Civitai URL and --version disagree")
	}
	versionID := requested
	if versionID == 0 {
		versionID = urlVersion
	}
	if versionID == 0 {
		return Discovery{}, fmt.Errorf("Civitai URL does not select a version")
	}
	var version map[string]any
	if err := requestJSON(ctx, "https://"+host+"/api/v1/model-versions/"+fmt.Sprint(versionID), token, "Civitai", &version); err != nil {
		return Discovery{}, err
	}
	returnedVersion, ok := jsonInt(version["id"])
	if !ok || returnedVersion != versionID {
		return Discovery{}, fmt.Errorf("Civitai returned inconsistent version metadata")
	}
	returnedModel, ok := jsonInt(version["modelId"])
	if !ok || modelID > 0 && returnedModel != modelID {
		return Discovery{}, fmt.Errorf("Civitai returned inconsistent model metadata")
	}
	modelID = returnedModel
	var model map[string]any
	if err := requestJSON(ctx, "https://"+host+"/api/v1/models/"+fmt.Sprint(modelID), token, "Civitai", &model); err != nil {
		return Discovery{}, err
	}
	var files []File
	if values, ok := version["files"].([]any); ok {
		for _, rawValue := range values {
			value, ok := rawValue.(map[string]any)
			if !ok {
				continue
			}
			file, ok := civitaiFile(value, host, versionID)
			if ok {
				files = append(files, file)
			}
		}
	}
	if len(files) == 0 {
		return Discovery{}, fmt.Errorf("Civitai version has no supported file with exact size and SHA-256")
	}
	title, _ := model["name"].(string)
	if name, ok := version["name"].(string); ok && name != "" {
		title += " — " + name
	}
	modelType, _ := model["type"].(string)
	availability, _ := version["availability"].(string)
	return Discovery{Provider: "civitai", SourceURL: fmt.Sprintf("https://%s/models/%d?modelVersionId=%d", host, modelID, versionID), Title: title, Repository: fmt.Sprintf("%s/models/%d", host, modelID), Revision: fmt.Sprint(versionID), DeclaredLicense: "Civitai model-page permissions", ModelType: modelType, Host: host, Files: files, ModelID: modelID, VersionID: versionID, RequiresAuth: host == "civitai.red" || availability != "" && strings.ToLower(availability) != "public"}, nil
}

func discoverHF(ctx context.Context, raw, token string) (Discovery, error) {
	repository, requestedRevision, requestedPath, err := hfIdentity(raw)
	if err != nil {
		return Discovery{}, err
	}
	repositoryParts := strings.Split(repository, "/")
	encodedRepository := url.PathEscape(repositoryParts[0]) + "/" + url.PathEscape(repositoryParts[1])
	endpoint := "https://huggingface.co/api/models/" + encodedRepository + "?blobs=true"
	if requestedRevision != "" {
		endpoint = "https://huggingface.co/api/models/" + encodedRepository + "/revision/" + url.PathEscape(requestedRevision) + "?blobs=true"
	}
	var metadata map[string]any
	if err := requestJSON(ctx, endpoint, token, "Hugging Face", &metadata); err != nil {
		return Discovery{}, err
	}
	revision, _ := metadata["sha"].(string)
	if !revisionPattern.MatchString(revision) {
		return Discovery{}, fmt.Errorf("Hugging Face did not resolve to a full commit")
	}
	var files []File
	if values, ok := metadata["siblings"].([]any); ok {
		for _, rawValue := range values {
			value, ok := rawValue.(map[string]any)
			if !ok {
				continue
			}
			filePath, _ := value["rfilename"].(string)
			if !recognized(filePath) || !safeHFPath(filePath) || requestedPath != "" && filePath != requestedPath {
				continue
			}
			lfs, _ := value["lfs"].(map[string]any)
			size, ok := jsonInt(lfs["size"])
			digest, _ := lfs["sha256"].(string)
			if !ok || size <= 0 || !shaPattern.MatchString(digest) {
				continue
			}
			files = append(files, File{ID: filePath, Name: path.Base(filePath), Size: size, SHA256: strings.ToLower(digest), Primary: requestedPath == filePath})
		}
	}
	if len(files) == 0 {
		return Discovery{}, fmt.Errorf("Hugging Face repository has no supported LFS model file")
	}
	license := "not declared by provider metadata"
	if card, ok := metadata["cardData"].(map[string]any); ok {
		if value, ok := card["license"].(string); ok && value != "" {
			license = value
		}
	}
	private, _ := metadata["private"].(bool)
	gated := metadata["gated"] != nil && metadata["gated"] != false
	sourceURL := (&url.URL{Scheme: "https", Host: "huggingface.co", Path: "/" + repository + "/tree/" + revision}).String()
	if requestedPath != "" {
		sourceURL = (&url.URL{Scheme: "https", Host: "huggingface.co", Path: "/" + repository + "/blob/" + revision + "/" + requestedPath}).String()
	}
	return Discovery{Provider: "huggingface", SourceURL: sourceURL, Title: repository, Repository: repository, Revision: revision, DeclaredLicense: license, Files: files, RequiresAuth: private || gated}, nil
}

func requestJSON(ctx context.Context, endpoint, token, provider string, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", identity.CommandName+"-content-import/1")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if next.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-HTTPS metadata redirect")
		}
		if len(via) > 0 && next.URL.Hostname() != via[0].URL.Hostname() {
			next.Header.Del("Authorization")
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("%s metadata request failed: %w", provider, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s metadata request returned HTTP %d", provider, response.StatusCode)
	}
	const maximumMetadataSize = 16 * 1024 * 1024
	contents, err := io.ReadAll(io.LimitReader(response.Body, maximumMetadataSize+1))
	if err != nil {
		return fmt.Errorf("%s metadata response could not be read: %w", provider, err)
	}
	if len(contents) > maximumMetadataSize {
		return fmt.Errorf("%s metadata response exceeds %d bytes", provider, maximumMetadataSize)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%s metadata response is invalid: %w", provider, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%s metadata response contains trailing data", provider)
	}
	return nil
}

func normalizedURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("content import requires a plain HTTPS provider URL")
	}
	return parsed, nil
}
func civitaiIdentity(raw string) (string, int64, int64, error) {
	parsed, err := normalizedURL(raw)
	if err != nil {
		return "", 0, 0, err
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host != "civitai.com" && host != "civitai.red" {
		return "", 0, 0, fmt.Errorf("unsupported Civitai host %q", host)
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	var model, version int64
	if (len(segments) == 2 || len(segments) == 3) && segments[0] == "models" {
		model, _ = strconv.ParseInt(segments[1], 10, 64)
	} else if len(segments) == 4 && segments[0] == "api" && segments[1] == "download" && segments[2] == "models" {
		version, _ = strconv.ParseInt(segments[3], 10, 64)
	} else {
		return "", 0, 0, fmt.Errorf("Civitai import requires a model page or download URL")
	}
	if model <= 0 && version <= 0 {
		return "", 0, 0, fmt.Errorf("Civitai import requires a positive model or version ID")
	}
	values := parsed.Query()["modelVersionId"]
	if len(values) > 1 {
		return "", 0, 0, fmt.Errorf("several modelVersionId values")
	}
	if len(values) == 1 {
		query, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || query <= 0 {
			return "", 0, 0, fmt.Errorf("invalid modelVersionId")
		}
		if version > 0 && version != query {
			return "", 0, 0, fmt.Errorf("conflicting version IDs")
		}
		version = query
	}
	return host, model, version, nil
}
func hfIdentity(raw string) (string, string, string, error) {
	parsed, err := normalizedURL(raw)
	if err != nil {
		return "", "", "", err
	}
	if strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.") != "huggingface.co" {
		return "", "", "", fmt.Errorf("unsupported Hugging Face host %q", parsed.Hostname())
	}
	var parts []string
	for _, item := range strings.Split(parsed.EscapedPath(), "/") {
		if item != "" {
			decoded, decodeErr := url.PathUnescape(item)
			if decodeErr != nil || decoded == "" || strings.Contains(decoded, "/") {
				return "", "", "", fmt.Errorf("Hugging Face URL contains an invalid path segment")
			}
			parts = append(parts, decoded)
		}
	}
	if len(parts) < 2 || parts[0] == "datasets" || parts[0] == "spaces" {
		return "", "", "", fmt.Errorf("Hugging Face URL must name a model repository")
	}
	if parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", "", "", fmt.Errorf("Hugging Face URL contains an unsafe repository path")
	}
	repository := parts[0] + "/" + parts[1]
	if len(parts) == 2 {
		return repository, "", "", nil
	}
	if len(parts) < 5 || parts[2] != "blob" && parts[2] != "resolve" {
		return "", "", "", fmt.Errorf("unsupported Hugging Face URL")
	}
	filePath := strings.Join(parts[4:], "/")
	if !safeHFPath(filePath) {
		return "", "", "", fmt.Errorf("unsafe Hugging Face path")
	}
	return repository, parts[3], filePath, nil
}
func civitaiFile(value map[string]any, host string, versionID int64) (File, bool) {
	name, ok := value["name"].(string)
	if !ok || name == "" || path.Base(name) != name {
		return File{}, false
	}
	hashes, _ := value["hashes"].(map[string]any)
	digest, _ := hashes["SHA256"].(string)
	id, idOK := jsonInt(value["id"])
	sizeKB, sizeOK := jsonNumber(value["sizeKB"])
	download, _ := value["downloadUrl"].(string)
	if !idOK || id <= 0 || !sizeOK || sizeKB <= 0 || !shaPattern.MatchString(digest) || !exactCivitaiDownload(download, host, versionID) || !recognized(name) {
		return File{}, false
	}
	exactSize := sizeKB * 1024
	size := int64(math.Round(exactSize))
	if size <= 0 || math.Abs(exactSize-float64(size)) > 0.001 {
		return File{}, false
	}
	primary, _ := value["primary"].(bool)
	return File{ID: fmt.Sprint(id), Name: name, Size: size, SHA256: strings.ToLower(digest), Primary: primary, DownloadURL: download}, true
}
func jsonInt(value any) (int64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := number.Int64()
		return parsed, err == nil
	}
	number, ok := jsonNumber(value)
	if !ok || number != float64(int64(number)) {
		return 0, false
	}
	return int64(number), true
}
func jsonNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		value, err := typed.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}
func recognized(name string) bool { return hasSuffix(name, append(weightSuffixes, ".gguf", ".json")) }

func exactCivitaiDownload(value, host string, version int64) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() == host && parsed.User == nil && parsed.Port() == "" && parsed.Path == "/api/download/models/"+fmt.Sprint(version) && parsed.Fragment == ""
}

func safeHFPath(value string) bool {
	return value != "" && path.Clean(value) == value && !strings.HasPrefix(value, "/") && value != "." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "/../")
}
func hasSuffix(name string, suffixes []string) bool {
	name = strings.ToLower(name)
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
func slug(value string, limit int) string {
	rendered := strings.Trim(unsafeSlug.ReplaceAllString(strings.ToLower(value), "-"), "._-")
	if len(rendered) > limit {
		rendered = strings.TrimRight(rendered[:limit], "._-")
	}
	if rendered == "" {
		return "content"
	}
	return rendered
}
func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func Digest(contents []byte) string {
	digest := sha256.Sum256(contents)
	return fmt.Sprintf("%x", digest)
}
