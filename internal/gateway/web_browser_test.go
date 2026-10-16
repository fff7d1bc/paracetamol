package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Optional real-browser acceptance. Neither Node nor browser tooling is needed
// to build or serve the embedded UI, or to run the regular Go suite.
func TestGatewayBrowser(t *testing.T) {
	if os.Getenv("PARACETAMOL_TEST_BROWSER") == "" || os.Getenv("PARACETAMOL_TEST_PUPPETEER") == "" {
		t.Skip("set PARACETAMOL_TEST_BROWSER and PARACETAMOL_TEST_PUPPETEER for browser acceptance")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("browser started inference") }))
	defer upstream.Close()
	plain, _, server := testGatewayServer(t, upstream)
	server.registry.Models = webFixtureModels()
	locked, _, protected := testGatewayServer(t, upstream)
	protected.registry.Models = webFixtureModels()
	protected.auth, _ = newBearerAuth(testAPIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "testdata/web-browser.mjs", plain.URL, locked.URL)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
}
