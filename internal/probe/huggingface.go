package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const HuggingFaceAPI = "https://huggingface.co/api"

type HFLicense struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

type HFFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	LFS    bool   `json:"lfs"`
	BlobID string `json:"blob_id,omitempty"`
}

type HFSummary struct {
	Repository string          `json:"repository"`
	Revision   string          `json:"revision"`
	Private    bool            `json:"private"`
	Gated      json.RawMessage `json:"gated"`
	Disabled   bool            `json:"disabled"`
	License    HFLicense       `json:"license"`
	Files      []HFFile        `json:"files,omitempty"`
	File       *HFFile         `json:"file,omitempty"`
}

type hfResponse struct {
	ID       string          `json:"id"`
	ModelID  string          `json:"modelId"`
	SHA      string          `json:"sha"`
	Private  bool            `json:"private"`
	Gated    json.RawMessage `json:"gated"`
	Disabled bool            `json:"disabled"`
	CardData struct {
		License     string `json:"license"`
		LicenseName string `json:"license_name"`
		LicenseLink string `json:"license_link"`
	} `json:"cardData"`
	Siblings []struct {
		Filename string `json:"rfilename"`
		Size     int64  `json:"size"`
		BlobID   string `json:"blobId"`
		LFS      *struct {
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"lfs"`
	} `json:"siblings"`
}

func InspectHuggingFace(ctx context.Context, client HTTPClient, apiRoot, repository, revision, file, token string) (HFSummary, error) {
	if apiRoot == "" {
		apiRoot = HuggingFaceAPI
	}
	if repository == "" || strings.Contains(repository, "..") {
		return HFSummary{}, fmt.Errorf("invalid Hugging Face repository")
	}
	parts := strings.Split(repository, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	endpoint := strings.TrimRight(apiRoot, "/") + "/models/" + strings.Join(parts, "/")
	if revision != "" {
		endpoint += "/revision/" + url.PathEscape(revision) + "?blobs=true"
	}
	raw, err := requestObject(ctx, client, endpoint, token)
	if err != nil {
		return HFSummary{}, fmt.Errorf("Hugging Face request failed: %w", err)
	}
	var response hfResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return HFSummary{}, err
	}
	summary := HFSummary{Repository: response.ID, Revision: response.SHA, Private: response.Private, Gated: response.Gated, Disabled: response.Disabled, License: HFLicense{ID: response.CardData.License, Name: response.CardData.LicenseName, URL: response.CardData.LicenseLink}}
	if summary.Repository == "" {
		summary.Repository = response.ModelID
	}
	for _, sibling := range response.Siblings {
		item := HFFile{Path: sibling.Filename, Size: sibling.Size, BlobID: sibling.BlobID}
		if sibling.LFS != nil {
			item.LFS, item.Size, item.SHA256 = true, sibling.LFS.Size, sibling.LFS.SHA256
		}
		summary.Files = append(summary.Files, item)
	}
	sort.Slice(summary.Files, func(i, j int) bool { return summary.Files[i].Path < summary.Files[j].Path })
	if file != "" {
		matches := 0
		for index := range summary.Files {
			if summary.Files[index].Path == file {
				matches++
				selected := summary.Files[index]
				summary.File = &selected
			}
		}
		if matches != 1 {
			return HFSummary{}, fmt.Errorf("expected exactly one file %q at %s@%s; found %d", file, repository, revision, matches)
		}
		summary.Files = nil
	}
	return summary, nil
}
