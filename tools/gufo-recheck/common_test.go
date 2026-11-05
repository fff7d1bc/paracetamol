package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
)

func TestOriginIsStrictAndCanonical(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:18556", "http://[::1]:18556", "http://[::ffff:127.0.0.1]:18556"} {
		if _, err := origin(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://localhost:18556", "http://192.0.2.1:18556", "http://0.0.0.0:18556", "https://127.0.0.1:18556", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://u:p@127.0.0.1:18556", "http://127.0.0.1:18556/", "http://127.0.0.1:18556?", "http://127.0.0.1:18556#fragment"} {
		if _, err := origin(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	a, _ := origin("http://[::ffff:127.0.0.1]:018556")
	b, _ := origin("http://127.0.0.1:18556")
	if a.String() != b.String() {
		t.Fatalf("loopback aliases not canonical: %s %s", a, b)
	}
}

func TestOutputConfinedPrivateAndCreateOnce(t *testing.T) {
	root := t.TempDir()
	path, err := newOutputDir(root, "build/recheck/run")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("permissions=%s", info.Mode())
	}
	if err := saveJSON(filepath.Join(path, "result.json"), map[string]bool{"pass": false}); err != nil {
		t.Fatal(err)
	}
	if err := saveJSON(filepath.Join(path, "result.json"), map[string]bool{"pass": true}); err == nil {
		t.Fatal("overwrote evidence")
	}
	info, _ = os.Stat(filepath.Join(path, "result.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file permissions=%s", info.Mode())
	}
	for _, out := range []string{"", ".", "build", "build/../outside", path, filepath.Join(t.TempDir(), "external")} {
		if _, err := newOutputDir(root, out); err == nil {
			t.Fatalf("accepted output %q", out)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "build", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := newOutputDir(root, "build/link/escape"); err == nil {
		t.Fatal("followed symlink ancestor")
	}
	other := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(other, "build")); err != nil {
		t.Fatal(err)
	}
	if _, err := newOutputDir(other, "build/run"); err == nil {
		t.Fatal("followed symlink build root")
	}
}

func TestFrozenSamplingRejectsDrift(t *testing.T) {
	managed, err := catalog.Load("../../catalog/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFrozenPolicy(managed); err != nil {
		t.Fatal(err)
	}
	policy := managed.SamplingPolicies[policyName]
	policy.Thinking["temperature"] = 0.6
	managed.SamplingPolicies[policyName] = policy
	if err := verifyFrozenPolicy(managed); err == nil {
		t.Fatal("silently accepted changed sampling")
	}
}

func TestControlUsesTheSameTargetAndPolicy(t *testing.T) {
	managed, err := catalog.Load("../../catalog/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{regular, regular + "-dflash2"} {
		if err := verifyCheckModel(managed, model); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyCheckModel(managed, "unrelated-model"); err == nil {
		t.Fatal("accepted unrelated model")
	}
	control := managed.LlamaPresets[regular+"-dflash2"]
	control.Artifact = "different-target"
	managed.LlamaPresets[regular+"-dflash2"] = control
	if err := verifyCheckModel(managed, regular+"-dflash2"); err == nil {
		t.Fatal("accepted changed control weights")
	}
}

func TestExperimentClientDoesNotFollowRedirects(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client := experimentClient()
	defer client.CloseIdleConnections()
	response, err := client.Get(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if called || response.StatusCode != http.StatusTemporaryRedirect || client.Timeout != 0 {
		t.Fatal("redirect followed or an inference deadline was imposed")
	}
}

func TestHelpDoesNotNeedBackendOrOptIn(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"prepare", "--help"}, {"proxy", "--help"}, {"check", "--help"}} {
		var out strings.Builder
		if err := run(context.Background(), args, &out, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 {
			t.Fatalf("missing help for %v", args)
		}
	}
	for _, args := range [][]string{{"proxy"}, {"check"}, {"not-a-command"}} {
		if err := run(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatalf("incomplete or unacknowledged command accepted: %v", args)
		}
	}
}
