package remoteimport

import "testing"

func TestBuildHuggingFaceGGUFPlan(t *testing.T) {
	discovery := Discovery{Provider: "huggingface", SourceURL: "https://huggingface.co/example/model", Title: "example/model", Repository: "example/model", Revision: "0123456789abcdef0123456789abcdef01234567", DeclaredLicense: "apache-2.0"}
	file := File{ID: "weights/model.gguf", Name: "model.gguf", Size: 123, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	kind, err := SelectKind("llama-cpp:model", file)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(discovery, file, kind)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Artifact.Target != "llama-models" || plan.Artifact.Destination != "imported/model.gguf" || plan.Artifact.License.SPDX != "NOASSERTION" {
		t.Fatalf("artifact = %#v", plan.Artifact)
	}
	if _, err := PackBytes(plan); err != nil {
		t.Fatal(err)
	}
}

func TestProviderAllowlist(t *testing.T) {
	for _, value := range []string{"http://huggingface.co/a/b", "https://example.com/a/b", "https://user@huggingface.co/a/b"} {
		if _, err := Provider(value); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestProviderSpecificIdentitiesRemainAllowlisted(t *testing.T) {
	if _, _, _, err := hfIdentity("https://example.com/owner/model"); err == nil {
		t.Fatal("Hugging Face parser accepted another host")
	}
	if _, _, _, err := civitaiIdentity("https://example.com/models/123"); err == nil {
		t.Fatal("Civitai parser accepted another host")
	}
	if _, _, _, err := civitaiIdentity("https://civitai.com/models/0"); err == nil {
		t.Fatal("Civitai parser accepted a zero model ID")
	}
}

func TestBuildPlanRejectsMalformedDiscoveryWithoutPanicking(t *testing.T) {
	discovery := Discovery{Provider: "huggingface", SourceURL: "https://huggingface.co/example/model", Repository: "example/model", Revision: "0123456789abcdef0123456789abcdef01234567"}
	kind := Kinds["llama-cpp:model"]
	for _, file := range []File{
		{Name: "model.gguf", Size: 1, SHA256: "short"},
		{ID: "../model.gguf", Name: "model.gguf", Size: 1, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{ID: "model.gguf", Name: "../model.gguf", Size: 1, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	} {
		if _, err := BuildPlan(discovery, file, kind); err == nil {
			t.Fatalf("accepted malformed discovery file: %#v", file)
		}
	}
}

func TestCivitaiDownloadMustMatchSelectedHostAndVersion(t *testing.T) {
	if exactCivitaiDownload("https://example.com/api/download/models/456", "civitai.com", 456) {
		t.Fatal("accepted cross-host download")
	}
	if exactCivitaiDownload("https://civitai.com/api/download/models/123", "civitai.com", 456) {
		t.Fatal("accepted another version")
	}
	if !exactCivitaiDownload("https://civitai.com/api/download/models/456", "civitai.com", 456) {
		t.Fatal("rejected exact Civitai download")
	}
}
