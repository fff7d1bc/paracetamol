package probe

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (function httpDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestInspectHuggingFaceImmutableFile(t *testing.T) {
	client := httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.Contains(request.URL.String(), "/revision/abc%2Fdef?blobs=true") || request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request: %s %#v", request.URL, request.Header)
		}
		body := `{"id":"owner/model","sha":"0123","cardData":{"license":"apache-2.0"},"siblings":[{"rfilename":"model.gguf","blobId":"blob","lfs":{"size":12,"sha256":"abcd"}}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	result, err := InspectHuggingFace(context.Background(), client, "https://example.invalid/api", "owner/model", "abc/def", "model.gguf", "secret")
	if err != nil || result.File == nil || result.File.SHA256 != "abcd" || len(result.Files) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
