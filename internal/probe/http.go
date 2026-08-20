package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"rocmplete/internal/identity"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func requestObject(ctx context.Context, client HTTPClient, endpoint, token string) (json.RawMessage, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", identity.CommandName+"-catalog-probe/2")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024+1))
	if err != nil || len(contents) > 32*1024*1024 {
		return nil, fmt.Errorf("metadata response is unreadable or too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("metadata service returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(contents)))
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(contents, &object); err != nil || object == nil {
		return nil, fmt.Errorf("metadata service returned a non-object")
	}
	return json.RawMessage(contents), nil
}
