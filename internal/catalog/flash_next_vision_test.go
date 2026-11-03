package catalog

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFlashNextRequiresExactPinnedBF16Projectors(t *testing.T) {
	managed, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		base, projector, repository, revision, source, destination, sha256 string
		size                                                               int64
	}{
		{"unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl", "unsloth-qwen3.8-flash-next-125b-a6b-mmproj-bf16", "unsloth/Qwen3.8-Flash-Next-GGUF", "c8b5954a88c2775c546b92593eda40ea041d3176", "mmproj-BF16.gguf", "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl/unsloth-Qwen3.8-Flash-Next-mmproj-BF16.gguf", "2e788f8c511d8093c7b43cb87b2fd7e14228340318057f8fb20c86df2efe2355", 907542944},
		{"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl", "huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-mmproj-bf16", "huihui-ai/Huihui-Qwen3.8-Flash-Next-abliterated-v2-GGUF", "43b4c1a8871afcc27e37bf996a431528b3d957e5", "mmproj-Qwen3.8-Flash-Next-BF16.gguf", "huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl/huihui-ai-abliterated-v2-Qwen3.8-Flash-Next-mmproj-BF16.gguf", "b1a82259702816a5330d7bd7607cd9676b11780e79ff7348c21103ff3ce49bd0", 907543008},
	} {
		t.Run(test.base, func(t *testing.T) {
			projector, ok := managed.Artifacts[test.projector]
			if !ok || projector.Source.Repository != test.repository || projector.Source.Revision != test.revision || projector.Source.Path != test.source || projector.Destination != test.destination || projector.Size != test.size || projector.SHA256 != test.sha256 || projector.Target != "llama-models" {
				t.Fatalf("projector identity changed: %+v", projector)
			}
			if projector.License.SPDX != "NOASSERTION" || projector.License.Status != "unverified" || projector.License.Warning == "" || !strings.Contains(projector.License.URL, test.revision) || projector.License.UpstreamRepository != "Qwen/Qwen3.8-Flash-Next" || !strings.Contains(projector.License.UpstreamLicenseURL, "de4b8e4d43b917e7706784d8bb445c9af86a3540") {
				t.Fatalf("projector license risk/provenance changed: %+v", projector.License)
			}
			for _, suffix := range []string{"", "-dflash2"} {
				preset := managed.LlamaPresets[test.base+suffix]
				if preset.MMProjArtifact != test.projector || !slices.Contains(managed.Bundles[preset.Bundle].Artifacts, test.projector) {
					t.Fatalf("preset %s lacks required matching projector", test.base+suffix)
				}
				target := managed.Artifacts[preset.Artifact]
				if target.Source.Repository != projector.Source.Repository || target.Source.Revision != projector.Source.Revision {
					t.Fatal("projector is not from the pinned target repack")
				}
			}
		})
	}
	withProjector := 0
	for id, preset := range managed.LlamaPresets {
		if preset.MMProjArtifact != "" {
			withProjector++
			if preset.SamplingPolicy != "qwen3.8-flash-next" {
				t.Fatalf("unrelated model %s gained vision", id)
			}
		}
	}
	if withProjector != 4 {
		t.Fatalf("vision preset count=%d", withProjector)
	}
}
