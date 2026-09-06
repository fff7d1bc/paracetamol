package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"paracetamol/internal/config"
	"paracetamol/internal/identity"
)

// Metadata probes do not follow even same-host redirects. Credentials belong
// to the explicitly selected endpoint, not to a service it redirects us to.
var metadataHTTPClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func fetchMetadata(ctx context.Context, endpoint, key, path string, query url.Values) ([]byte, error) {
	normalized, err := config.NormalizeGatewayURL(endpoint)
	if err != nil {
		return nil, err
	}
	if key != "" {
		if err := config.ValidateGatewayKey(key); err != nil {
			return nil, err
		}
	}
	parsed, _ := url.Parse(normalized)
	parsed.Path, parsed.RawQuery = path, query.Encode()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	request.Header.Set("Accept", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := metadataHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cannot reach gateway at %s: %w", endpoint, err)
	}
	defer response.Body.Close()
	markers := response.Header.Values(IdentityHeader)
	if len(markers) != 1 || markers[0] != IdentityValue {
		return nil, fmt.Errorf("%s is not a %s gateway (HTTP %d)", endpoint, identity.DisplayName, response.StatusCode)
	}
	if response.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("gateway authentication failed (HTTP 401). Configure --gateway-api-key-file or [gateway.client].api_key_file with the matching key")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway returned HTTP %d", response.StatusCode)
	}
	const limit = 2 * 1024 * 1024
	contents, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(contents) > limit {
		return nil, fmt.Errorf("gateway metadata is unreadable or too large")
	}
	return contents, nil
}

func FetchModelIDs(ctx context.Context, endpoint, key string) ([]string, error) {
	raw, err := fetchMetadata(ctx, endpoint, key, "/v1/models", nil)
	if err != nil {
		return nil, err
	}
	var document struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Data == nil {
		return nil, fmt.Errorf("gateway returned an invalid model inventory")
	}
	seen := map[string]bool{}
	var result []string
	for _, item := range document.Data {
		if item.ID != "" && !seen[item.ID] {
			seen[item.ID] = true
			result = append(result, item.ID)
		}
	}
	return result, nil
}

func FetchStatus(ctx context.Context, endpoint, key string, recentRequests int) (Status, error) {
	if recentRequests < 0 || recentRequests > RecentRequestLimit {
		return Status{}, fmt.Errorf("invalid recent request count")
	}
	query := url.Values{}
	if recentRequests > 0 {
		query.Set("requests", strconv.Itoa(recentRequests))
	}
	raw, err := fetchMetadata(ctx, endpoint, key, "/paracetamol/v1/status", query)
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		return Status{}, fmt.Errorf("cannot decode gateway status")
	}
	if status.Schema != StatusSchema {
		return Status{}, fmt.Errorf("gateway returned unsupported status schema %q", status.Schema)
	}
	return status, nil
}
