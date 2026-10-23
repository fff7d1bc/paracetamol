package runtime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/verification"
)

func routerDraftFixture(t *testing.T) (catalog.Catalog, string, func(string, bool)) {
	t.Helper()
	root := t.TempDir()
	payload := []byte("model")
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{},
		Bundles: map[string]catalog.Bundle{
			"base":   {ID: "base", Application: "llama-cpp", Artifacts: []string{"target-1", "target-2"}},
			"dflash": {ID: "dflash", Application: "llama-cpp", Artifacts: []string{"target-1", "target-2", "draft"}},
		},
		LlamaPresets: map[string]catalog.LlamaPreset{
			"base":   {ID: "base", Bundle: "base", Artifact: "target-1", DefaultContext: 262144},
			"dflash": {ID: "dflash", Bundle: "dflash", Artifact: "target-1", DraftArtifact: "draft", DefaultContext: 262144, SpeculativeType: "draft-dflash", DraftTokens: 3},
		},
	}
	for _, id := range []string{"target-1", "target-2", "draft"} {
		managed.Artifacts[id] = catalog.Artifact{ID: id, Target: "llama-models", Destination: id + ".gguf", Size: int64(len(payload)), SHA256: fmt.Sprintf("%x", sha256.Sum256(payload))}
	}
	install := func(id string, verified bool) {
		t.Helper()
		artifact := managed.Artifacts[id]
		path := content.ArtifactPath(root, artifact)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0600); err != nil {
			t.Fatal(err)
		}
		if !verified {
			return
		}
		store, err := verification.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		status, err := content.VerifyArtifact(context.Background(), store, root, artifact, nil)
		if err != nil || status.State != content.Verified {
			t.Fatalf("verify %s: state=%s err=%v", id, status.State, err)
		}
		if err := store.Save(); err != nil {
			t.Fatal(err)
		}
	}
	return managed, root, install
}

func TestRenderRouterOptionalDraftDoesNotRequireInstall(t *testing.T) {
	managed, root, install := routerDraftFixture(t)
	install("target-1", true)
	install("target-2", true)
	contents, installed, err := RenderRouter(managed, root, "rocm", "strix-halo")
	if err != nil || !slices.Equal(installed, []string{"base"}) || !strings.Contains(contents, "[base]") || strings.Contains(contents, "[dflash]") {
		t.Fatalf("missing optional draft broke base router: installed=%v err=%v contents=%s", installed, err, contents)
	}
	if _, err := content.RequireBundle(managed, managed.Bundles["dflash"], root); err == nil {
		t.Fatal("explicitly requested variant accepted a missing draft")
	}
	install("draft", true)
	contents, installed, err = RenderRouter(managed, root, "rocm", "strix-halo")
	if err != nil || !slices.Equal(installed, []string{"base", "dflash"}) || !strings.Contains(contents, "[dflash]") || !strings.Contains(contents, "spec-draft-sampling = greedy") {
		t.Fatalf("verified draft did not enable variant: installed=%v err=%v contents=%s", installed, err, contents)
	}
}

func TestRenderRouterSharedDraftDoesNotInstallOtherTargetFamily(t *testing.T) {
	for _, first := range []string{"regular", "huihui"} {
		t.Run(first+" first", func(t *testing.T) {
			managed, root, install := routerDraftFixture(t)
			for _, id := range []string{"huihui-1", "huihui-2"} {
				artifact := managed.Artifacts["target-1"]
				artifact.ID, artifact.Destination = id, id+".gguf"
				managed.Artifacts[id] = artifact
			}
			managed.Bundles["huihui-base"] = catalog.Bundle{ID: "huihui-base", Application: "llama-cpp", Artifacts: []string{"huihui-1", "huihui-2"}}
			managed.Bundles["huihui-dflash"] = catalog.Bundle{ID: "huihui-dflash", Application: "llama-cpp", Artifacts: []string{"huihui-1", "huihui-2", "draft"}}
			for _, id := range []string{"base", "dflash"} {
				preset := managed.LlamaPresets[id]
				preset.ID, preset.Bundle, preset.Artifact = "huihui-"+id, "huihui-"+id, "huihui-1"
				managed.LlamaPresets[preset.ID] = preset
			}
			install("draft", true)
			if _, _, err := RenderRouter(managed, root, "rocm", "strix-halo"); err == nil || !strings.Contains(err.Error(), "no installed managed") {
				t.Fatalf("draft alone was treated as a partial target install: %v", err)
			}
			firstTargets, secondTargets := []string{"target-1", "target-2"}, []string{"huihui-1", "huihui-2"}
			want, absentBundle := []string{"base", "dflash"}, "huihui-dflash"
			if first == "huihui" {
				firstTargets, secondTargets = secondTargets, firstTargets
				want, absentBundle = []string{"huihui-base", "huihui-dflash"}, "dflash"
			}
			for _, id := range firstTargets {
				install(id, true)
			}
			_, installed, err := RenderRouter(managed, root, "rocm", "strix-halo")
			if err != nil || !slices.Equal(installed, want) {
				t.Fatalf("shared draft made an absent family incomplete: installed=%v want=%v err=%v", installed, want, err)
			}
			if _, err := content.RequireBundle(managed, managed.Bundles[absentBundle], root); err == nil {
				t.Fatal("explicit bundle selection accepted the wholly absent target")
			}
			// A draft shared with an absent family still must be valid for the
			// installed family; skipping absence must not hide existing bad bytes.
			if err := os.WriteFile(content.ArtifactPath(root, managed.Artifacts["draft"]), []byte("other"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := RenderRouter(managed, root, "rocm", "strix-halo"); err == nil || !strings.Contains(err.Error(), "is incomplete") {
				t.Fatalf("invalid shared draft was ignored for installed target: %v", err)
			}
			install("draft", true)
			install(secondTargets[0], true)
			if _, _, err := RenderRouter(managed, root, "rocm", "strix-halo"); err == nil || !strings.Contains(err.Error(), "is incomplete") {
				t.Fatalf("shared draft hid a partial target family: %v", err)
			}
			for _, id := range secondTargets[1:] {
				install(id, true)
			}
			_, installed, err = RenderRouter(managed, root, "rocm", "strix-halo")
			if err != nil || !slices.Equal(installed, []string{"base", "dflash", "huihui-base", "huihui-dflash"}) {
				t.Fatalf("both target families were not discovered: installed=%v err=%v", installed, err)
			}
		})
	}
}

func TestRenderRouterOptionalDraftRemainsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, artifact, state string
	}{
		{"partial target", "target-2", "missing"},
		{"unverified target", "target-2", "unverified"},
		{"corrupt target size", "target-2", "size-mismatch"},
		{"stale target receipt", "target-2", "stale"},
		{"unverified draft", "draft", "unverified"},
		{"corrupt draft size", "draft", "size-mismatch"},
		{"stale draft receipt", "draft", "stale"},
		{"unexpected draft type", "draft", "directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			managed, root, install := routerDraftFixture(t)
			for _, id := range []string{"target-1", "target-2"} {
				if id != tc.artifact {
					install(id, true)
				}
			}
			path := content.ArtifactPath(root, managed.Artifacts[tc.artifact])
			switch tc.state {
			case "unverified":
				install(tc.artifact, false)
			case "size-mismatch", "stale":
				install(tc.artifact, true)
				payload := "bad"
				if tc.state == "stale" {
					payload = "other"
				}
				if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := RenderRouter(managed, root, "rocm", "strix-halo"); err == nil || !strings.Contains(err.Error(), "is incomplete") {
				t.Fatalf("unsafe optional variant state accepted: %v", err)
			}
			if _, err := content.RequireBundle(managed, managed.Bundles["dflash"], root); err == nil {
				t.Fatal("explicitly requested incomplete variant was accepted")
			}
		})
	}
}

func TestRouterPinsOnlyDFlashProposalSampling(t *testing.T) {
	for _, speculativeType := range []string{"", "draft-mtp", "draft-dflash"} {
		t.Run(speculativeType, func(t *testing.T) {
			preset := catalog.LlamaPreset{ID: "fixture", Artifact: "target", DefaultContext: 262144, SpeculativeType: speculativeType}
			if speculativeType != "" {
				preset.DraftTokens = 3
				preset.DraftArtifact = "draft"
			}
			if speculativeType == "draft-dflash" {
				preset.DraftTokens = 15
				preset.DraftTokensByBackend = map[string]int64{"vulkan": 4}
			}
			managed := catalog.Catalog{
				Artifacts:    map[string]catalog.Artifact{"target": {Destination: "target.gguf"}, "draft": {Destination: "draft.gguf"}},
				LlamaPresets: map[string]catalog.LlamaPreset{"fixture": preset},
			}
			for _, backend := range []string{"rocm", "vulkan"} {
				contents, err := RenderRouterModels(managed, backend, "strix-halo", []string{"fixture"})
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if speculativeType == "draft-dflash" {
					want = 1
				}
				if got := strings.Count(contents, "spec-draft-sampling = greedy\n"); got != want {
					t.Fatalf("unexpected proposal policy for %s: %s", backend, contents)
				}
				for _, forbidden := range []string{"probabilistic", "temperature", "top-p", "top-k", "min-p"} {
					if strings.Contains(contents, forbidden) {
						t.Fatalf("router changed target sampling with %s: %s", forbidden, contents)
					}
				}
				if speculativeType != "" && !strings.Contains(contents, fmt.Sprintf("spec-type = %s\nspec-draft-n-max = %d\n", speculativeType, preset.DraftTokensForBackend(backend))) {
					t.Fatalf("router changed speculation: %s", contents)
				}
			}
		})
	}
}
