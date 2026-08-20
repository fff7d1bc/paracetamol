package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectUIWorkflowFindsPackagesModesAndAssets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.json")
	contents := `{"nodes":[{"type":"CoreLoader","properties":{"cnr_id":"comfy-core"},"widgets_values":["models/core.safetensors"]},{"type":"RegistryNode","mode":4,"properties":{"cnr_id":"custom-pack","ver":"2.0"},"widgets_values":["loras/style.safetensors"]},{"type":"UnknownNode"}]}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := InspectWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "ui" || result.NodeModes.Active != 2 || result.NodeModes.Bypassed != 1 || len(result.DeclaredPackages) != 1 || len(result.AssetReferences) != 2 {
		t.Fatalf("unexpected summary: %#v", result)
	}
}

func TestInspectAPIWorkflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.json")
	if err := os.WriteFile(path, []byte(`{"1":{"class_type":"UNETLoader","inputs":{"unet_name":"model.safetensors"}},"2":{"class_type":"KSampler","inputs":{"seed":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := InspectWorkflow(path)
	if err != nil || result.Format != "api" || len(result.NodeTypes) != 2 || len(result.AssetReferences) != 1 {
		t.Fatalf("summary=%#v err=%v", result, err)
	}
}
