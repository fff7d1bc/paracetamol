package probe

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestInspectCivitaiVersion(t *testing.T) {
	client := httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/model-versions/42" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		body := `{"id":42,"modelId":7,"name":"v1","files":[{"id":1,"name":"model.safetensors","sizeKB":12,"hashes":{"SHA256":"abcd"}}],"images":[]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	result, err := InspectCivitai(context.Background(), client, "https://example.invalid/api", "version", "42", "")
	version, ok := result.(CivitaiVersion)
	if err != nil || !ok || version.ID != 42 || len(version.Files) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
