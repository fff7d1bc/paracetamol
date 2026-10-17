package imagearchive

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"paracetamol/internal/config"
	"paracetamol/internal/identity"
)

func writeArchive(t *testing.T, unsafe bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "images.tar")
	handle, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(handle)
	configBytes, _ := json.Marshal(map[string]any{"architecture": runtime.GOARCH, "os": "linux"})
	digest := sha256.Sum256(configBytes)
	configName := fmt.Sprintf("%x.json", digest)
	manifest, _ := json.Marshal([]manifestEntry{{Config: configName, RepoTags: []string{config.ContentToolsImage}, Layers: []string{"layer/layer.tar"}}})
	members := []struct {
		name string
		data []byte
	}{{"manifest.json", manifest}, {configName, configBytes}, {"layer/layer.tar", []byte("layer")}}
	if unsafe {
		members = append(members, struct {
			name string
			data []byte
		}{"../escape", []byte("bad")})
	}
	for _, member := range members {
		if err := writer.WriteHeader(&tar.Header{Name: member.name, Mode: 0o644, Size: int64(len(member.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(member.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectAndValidateManagedArchive(t *testing.T) {
	archive, err := Inspect(writeArchive(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManaged(archive, []string{config.ContentToolsImage}); err != nil {
		t.Fatal(err)
	}
}

func TestInspectRejectsTraversal(t *testing.T) {
	if _, err := Inspect(writeArchive(t, true)); err == nil {
		t.Fatal("accepted unsafe archive member")
	}
}

func TestSelectedReferencesUseBuildClosure(t *testing.T) {
	references, err := SelectedReferences("llama-cpp")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{config.ROCmRuntimeImage, mustApplicationImage(t, "llama-cpp")}
	if !reflect.DeepEqual(references, want) {
		t.Fatalf("references=%q want=%q", references, want)
	}
	if _, err := SelectedReferences("base"); err == nil {
		t.Fatal("retired base target still accepted")
	}
	if _, err := SelectedReferences("dwarfstar"); err == nil {
		t.Fatal("retired DwarfStar export target still accepted")
	}
}

func TestAllReferencesContainOnlyCurrentBuildUnits(t *testing.T) {
	want := []string{
		config.ContentToolsImage, config.ROCmRuntimeImage, config.ROCmBaseImage,
		mustApplicationImage(t, "comfyui"), mustApplicationImage(t, "llama-cpp"),
	}
	references, err := SelectedReferences("all")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(references, want) {
		t.Fatalf("references=%q want=%q", references, want)
	}
	if !reflect.DeepEqual(ManagedReferences(), want) {
		t.Fatalf("managed references=%q want=%q", ManagedReferences(), want)
	}
}

func TestValidateManagedRejectsRetiredApplicationImage(t *testing.T) {
	reference := identity.Image("dwarfstar-ubuntu26.04-rocm10.1-0aaea5a-r12")
	archive := Archive{Images: []Image{{Reference: reference, OperatingSystem: "linux", Architecture: runtime.GOARCH}}}
	if err := ValidateManaged(archive, []string{reference}); err == nil {
		t.Fatal("retired DwarfStar archive image was accepted")
	}
}

func TestValidateManagedAcceptsIndependentBuildUnits(t *testing.T) {
	for _, reference := range []string{config.ContentToolsImage, config.ROCmRuntimeImage} {
		archive := Archive{Images: []Image{{Reference: reference, OperatingSystem: "linux", Architecture: runtime.GOARCH}}}
		if err := ValidateManaged(archive, []string{reference}); err != nil {
			t.Fatalf("independent image %s was rejected: %v", reference, err)
		}
	}
}

func mustApplicationImage(t *testing.T, identifier string) string {
	t.Helper()
	application, ok := config.ApplicationByID(identifier)
	if !ok {
		t.Fatalf("missing application %s", identifier)
	}
	return application.Image
}
