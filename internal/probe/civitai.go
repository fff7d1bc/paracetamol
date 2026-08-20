package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const CivitaiAPI = "https://civitai.com/api/v1"

type CivitaiFile struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	SizeKB      float64           `json:"sizeKB"`
	Primary     bool              `json:"primary"`
	Hashes      map[string]string `json:"hashes"`
	Metadata    json.RawMessage   `json:"metadata"`
	DownloadURL string            `json:"downloadUrl"`
}

type CivitaiImage struct {
	ID        int64           `json:"id"`
	URL       string          `json:"url"`
	Type      string          `json:"type"`
	Width     int             `json:"width"`
	Height    int             `json:"height"`
	NSFWLevel int             `json:"nsfwLevel"`
	Meta      json.RawMessage `json:"meta"`
}

type CivitaiVersion struct {
	ID            int64           `json:"id"`
	ModelID       int64           `json:"modelId"`
	Name          string          `json:"name"`
	Description   json.RawMessage `json:"description"`
	BaseModel     string          `json:"baseModel"`
	BaseModelType string          `json:"baseModelType"`
	PublishedAt   string          `json:"publishedAt"`
	Availability  string          `json:"availability"`
	Files         []CivitaiFile   `json:"files"`
	Images        []CivitaiImage  `json:"images"`
}

type CivitaiPermissions struct {
	AllowNoCredit         bool     `json:"allowNoCredit"`
	AllowCommercialUse    []string `json:"allowCommercialUse"`
	AllowDerivatives      bool     `json:"allowDerivatives"`
	AllowDifferentLicense bool     `json:"allowDifferentLicense"`
}

type CivitaiModel struct {
	ID          int64              `json:"id"`
	Name        string             `json:"name"`
	Description json.RawMessage    `json:"description"`
	Type        string             `json:"type"`
	NSFW        bool               `json:"nsfw"`
	Creator     *CivitaiCreator    `json:"creator"`
	Permissions CivitaiPermissions `json:"permissions"`
	Versions    []CivitaiVersion   `json:"versions"`
}

type CivitaiCreator struct {
	Username string `json:"username"`
}

type civitaiRawModel struct {
	ID                    int64            `json:"id"`
	Name                  string           `json:"name"`
	Description           json.RawMessage  `json:"description"`
	Type                  string           `json:"type"`
	NSFW                  bool             `json:"nsfw"`
	Creator               *CivitaiCreator  `json:"creator"`
	AllowNoCredit         bool             `json:"allowNoCredit"`
	AllowCommercialUse    []string         `json:"allowCommercialUse"`
	AllowDerivatives      bool             `json:"allowDerivatives"`
	AllowDifferentLicense bool             `json:"allowDifferentLicense"`
	Versions              []CivitaiVersion `json:"modelVersions"`
}

func InspectCivitai(ctx context.Context, client HTTPClient, apiRoot, resource, identifier, token string) (any, error) {
	if apiRoot == "" {
		apiRoot = CivitaiAPI
	}
	base := strings.TrimRight(apiRoot, "/")
	endpoint := ""
	switch resource {
	case "model":
		if _, err := positiveIdentifier(identifier); err != nil {
			return nil, err
		}
		endpoint = base + "/models/" + identifier
	case "version":
		if _, err := positiveIdentifier(identifier); err != nil {
			return nil, err
		}
		endpoint = base + "/model-versions/" + identifier
	case "hash":
		if identifier == "" {
			return nil, fmt.Errorf("hash cannot be empty")
		}
		endpoint = base + "/model-versions/by-hash/" + url.PathEscape(identifier)
	case "search":
		if identifier == "" {
			return nil, fmt.Errorf("search query cannot be empty")
		}
		endpoint = base + "/models?" + url.Values{"query": {identifier}, "limit": {"20"}}.Encode()
	default:
		return nil, fmt.Errorf("resource must be model, version, hash, or search")
	}
	raw, err := requestObject(ctx, client, endpoint, token)
	if err != nil {
		return nil, fmt.Errorf("Civitai request failed: %w", err)
	}
	if resource == "version" || resource == "hash" {
		var version CivitaiVersion
		if err := json.Unmarshal(raw, &version); err != nil {
			return nil, err
		}
		return version, nil
	}
	if resource == "model" {
		var model civitaiRawModel
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, err
		}
		return summarizeCivitaiModel(model), nil
	}
	var search struct {
		Items []civitaiRawModel `json:"items"`
	}
	if err := json.Unmarshal(raw, &search); err != nil {
		return nil, err
	}
	items := make([]CivitaiModel, 0, len(search.Items))
	for _, item := range search.Items {
		items = append(items, summarizeCivitaiModel(item))
	}
	return struct {
		Items []CivitaiModel `json:"items"`
	}{Items: items}, nil
}

func InspectCivitaiRaw(ctx context.Context, client HTTPClient, apiRoot, resource, identifier, token string) (json.RawMessage, error) {
	if resource != "model" && resource != "version" {
		return nil, fmt.Errorf("raw mode supports model or version")
	}
	if _, err := positiveIdentifier(identifier); err != nil {
		return nil, err
	}
	if apiRoot == "" {
		apiRoot = CivitaiAPI
	}
	segment := "models/"
	if resource == "version" {
		segment = "model-versions/"
	}
	return requestObject(ctx, client, strings.TrimRight(apiRoot, "/")+"/"+segment+identifier, token)
}

func summarizeCivitaiModel(model civitaiRawModel) CivitaiModel {
	return CivitaiModel{ID: model.ID, Name: model.Name, Description: model.Description, Type: model.Type, NSFW: model.NSFW, Creator: model.Creator, Permissions: CivitaiPermissions{AllowNoCredit: model.AllowNoCredit, AllowCommercialUse: model.AllowCommercialUse, AllowDerivatives: model.AllowDerivatives, AllowDifferentLicense: model.AllowDifferentLicense}, Versions: model.Versions}
}

func positiveIdentifier(value string) (int64, error) {
	identifier, err := strconv.ParseInt(value, 10, 64)
	if err != nil || identifier < 1 {
		return 0, fmt.Errorf("identifier must be a positive integer")
	}
	return identifier, nil
}
