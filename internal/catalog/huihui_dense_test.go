package catalog

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHuihuiDenseReusesRegularRuntimePolicy(t *testing.T) {
	loaded, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	const id = "huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl"
	regular := loaded.LlamaPresets["unsloth-qwen3.8-27b-ud-q8-k-xl"]
	huihui, ok := loaded.LlamaPresets[id]
	if !ok {
		t.Fatal("missing Huihui dense variant")
	}
	bundle := loaded.Bundles[huihui.Bundle]
	if !reflect.DeepEqual(bundle.Artifacts, []string{huihui.Artifact}) {
		t.Fatalf("variant should require only its own target: %v", bundle.Artifacts)
	}
	artifact := loaded.Artifacts[huihui.Artifact]
	original := loaded.Artifacts[regular.Artifact]
	if artifact.Source.Repository != "huihui-ai/Huihui-Qwen3.8-27B-abliterated-GGUF" ||
		artifact.Source.Path != "Huihui-Qwen3.8-27B-abliterated-UD-Q8_K_XL.gguf" ||
		artifact.Size != original.Size || artifact.SHA256 == original.SHA256 ||
		artifact.License.SPDX != "Apache-2.0" || artifact.License.Status != "verified" ||
		!strings.Contains(artifact.License.URL, artifact.Source.Revision+"/README.md") {
		t.Fatalf("unexpected Huihui dense provenance: %#v", artifact)
	}
	if filepath.Dir(artifact.Destination) == filepath.Dir(original.Destination) {
		t.Fatal("modified weights must not replace the regular model")
	}
	// Weight identity is the only variant boundary. Keep future changes to the
	// shared template, reasoning, sampling, context and cache policy in parity.
	huihui.ID, huihui.Bundle, huihui.Artifact = regular.ID, regular.Bundle, regular.Artifact
	if !reflect.DeepEqual(regular, huihui) {
		t.Fatalf("dense runtime policies diverged: regular=%#v Huihui=%#v", regular, huihui)
	}
}

func TestRetiredTextModelsAreAbsent(t *testing.T) {
	loaded, err := Load(filepath.Join("..", "..", "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"bartowski-kat-coder-v2.5-dev-q8-0",
		"tencent-hy-mt1.5-7b-q8-0",
		"mradermacher-translategemma-27b-it-q8-0",
		"mradermacher-shisa-v2.1-llama3.3-70b-q8-0",
	} {
		if _, ok := loaded.LlamaPresets[id]; ok {
			t.Errorf("retired preset %s remains", id)
		}
		if _, ok := loaded.Bundles["llama-"+id]; ok {
			t.Errorf("retired bundle %s remains", id)
		}
	}
	for _, id := range []string{
		"bartowski-kat-coder-v2.5-dev-q8-gguf",
		"tencent-hy-mt1.5-7b-q8-gguf",
		"mradermacher-translategemma-27b-it-q8-gguf",
		"mradermacher-shisa-v2.1-llama3.3-70b-q8-gguf",
	} {
		if _, ok := loaded.Artifacts[id]; ok {
			t.Errorf("retired artifact %s remains", id)
		}
	}
	for _, id := range []string{"llama-3.3-community-license", "tencent-hy-community-license"} {
		if _, ok := loaded.Agreements[id]; ok {
			t.Errorf("unused retired agreement %s remains", id)
		}
	}
}
