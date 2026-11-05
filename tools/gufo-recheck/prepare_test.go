package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/project"
)

const prepareProductionFixture = `ARG UBUNTU_IMAGE=example.invalid/ubuntu@sha256:abc
ARG ROCM_VERSION=10.1.0
ARG ROCM_RUNTIME_IMAGE=localhost/example:runtime
FROM ${UBUNTU_IMAGE} AS rocm-runtime
RUN echo production
FROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk
ARG ROCM_VERSION
RUN echo sdk
`

func TestComposeCurrentCheckoutPrefix(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	production, err := os.ReadFile(filepath.Join(root, "Containerfile"))
	if err != nil {
		t.Fatal(err)
	}
	generated, manifest, err := composeGufoRecipe(production, gufoFragment)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(generated, production) || manifest.ProductionContainerfileSHA256 != hashRecipeBytes(production) {
		t.Fatal("generated recipe must derive from the current production Containerfile, not a saved snapshot")
	}
}

func TestComposeGufoRecipeFreshProductionAndManifest(t *testing.T) {
	for _, suffix := range []string{"# first checkout\n", "# changed checkout\n"} {
		production := []byte(prepareProductionFixture + suffix)
		generated, manifest, err := composeGufoRecipe(production, gufoFragment)
		if err != nil {
			t.Fatal(err)
		}
		want := append(append(append([]byte(nil), production...), '\n'), gufoFragment...)
		if !bytes.Equal(generated, want) {
			t.Fatal("recipe does not preserve the exact current production prefix and embedded fragment")
		}
		if manifest.SchemaVersion != 1 || manifest.GufoCommit != gufoCommit {
			t.Fatalf("incorrect manifest identity: %+v", manifest)
		}
		if manifest.ProductionContainerfileSHA256 != hashRecipeBytes(production) ||
			manifest.FragmentSHA256 != hashRecipeBytes(gufoFragment) ||
			manifest.GeneratedContainerfileSHA256 != hashRecipeBytes(generated) {
			t.Fatalf("incorrect input/output hashes: %+v", manifest)
		}
		if manifest.ProductionDefaults.ROCmVersion != "10.1.0" || manifest.ProductionDefaults.ROCmRuntimeImage != "localhost/example:runtime" {
			t.Fatalf("defaults are not observations of production: %+v", manifest)
		}
	}
}

func TestComposeGufoRecipeRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name       string
		production string
		fragment   string
	}{
		{"missing SDK", strings.ReplaceAll(prepareProductionFixture, "AS native-rocm-sdk", "AS other-sdk"), string(gufoFragment)},
		{"wrong SDK base", strings.ReplaceAll(prepareProductionFixture, "FROM ${ROCM_RUNTIME_IMAGE} AS native", "FROM unrelated AS native"), string(gufoFragment)},
		{"duplicate SDK", prepareProductionFixture + "FROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk\n", string(gufoFragment)},
		{"colliding stage", prepareProductionFixture + "FROM native-rocm-sdk AS gufo-recheck\n", string(gufoFragment)},
		{"missing runtime default", strings.ReplaceAll(prepareProductionFixture, "ARG ROCM_RUNTIME_IMAGE=localhost/example:runtime\n", ""), string(gufoFragment)},
		{"duplicate version default", "ARG ROCM_VERSION=11\n" + prepareProductionFixture, string(gufoFragment)},
		{"invalid full pin", prepareProductionFixture, strings.ReplaceAll(string(gufoFragment), gufoCommit, "2c97b72")},
		{"inconsistent pin", prepareProductionFixture, strings.Replace(string(gufoFragment), gufoCommit, strings.Repeat("a", 40), 1)},
		{"missing pin", prepareProductionFixture, strings.Replace(string(gufoFragment), "ARG GUFO_COMMIT="+gufoCommit, "ARG DEBIAN_FRONTEND=noninteractive", 1)},
		{"version override", prepareProductionFixture, string(gufoFragment) + "\nARG ROCM_VERSION=11\n"},
		{"runtime override", prepareProductionFixture, string(gufoFragment) + "\nARG ROCM_RUNTIME_IMAGE=other\n"},
		{"base override", prepareProductionFixture, string(gufoFragment) + "\nARG UBUNTU_IMAGE=other\n"},
		{"version environment override", prepareProductionFixture, string(gufoFragment) + "\nENV ROCM_VERSION=11\n"},
		{"base environment override", prepareProductionFixture, string(gufoFragment) + "\nENV UBUNTU_IMAGE=other\n"},
		{"missing verification", prepareProductionFixture, strings.ReplaceAll(string(gufoFragment), `test "$(git rev-parse HEAD)" = "${GUFO_COMMIT}"`, "true")},
		{"unreviewed stage", prepareProductionFixture, string(gufoFragment) + "\nFROM other AS other\n"},
		{"production pin collision", prepareProductionFixture + "ARG GUFO_COMMIT=other\n", string(gufoFragment)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := composeGufoRecipe([]byte(test.production), []byte(test.fragment)); err == nil {
				t.Fatal("unsafe recipe accepted")
			}
		})
	}
}

func TestPrepareGeneratesOnlyPrivateRecipeAndManifest(t *testing.T) {
	root := prepareFixtureRoot(t, prepareProductionFixture)
	// No subprocess can be discovered; preparation needs no Podman, Git, or
	// download helper and succeeds entirely from checkout bytes.
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	if err := runPrepare(context.Background(), []string{"--out", "build/prepared"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "build", "prepared")
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected output: %v %v", entries, err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("output not private: %s %v", entry.Name(), err)
		}
	}
	generated, err := os.ReadFile(filepath.Join(out, "Containerfile"))
	if err != nil || !bytes.HasPrefix(generated, []byte(prepareProductionFixture)) {
		t.Fatalf("current production prefix absent: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(out, "prepare.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest prepareManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.GeneratedContainerfileSHA256 != hashRecipeBytes(generated) {
		t.Fatalf("invalid generated manifest: %v %+v", err, manifest)
	}
	for _, instruction := range []string{"Review both files", "./paracetamol build runtime", "podman build --pull=never", "not GPU acceptance"} {
		if !strings.Contains(stdout.String(), instruction) {
			t.Fatalf("missing manual instruction %q in %s", instruction, stdout.String())
		}
	}
	if err := runPrepare(context.Background(), []string{"--out", "build/prepared"}, &stdout, &stderr); err == nil {
		t.Fatal("existing output accepted")
	}
	after, _ := os.ReadFile(filepath.Join(out, "Containerfile"))
	if !bytes.Equal(generated, after) {
		t.Fatal("existing output clobbered")
	}
}

func TestPrepareHelpCancellationAndValidationDoNotWrite(t *testing.T) {
	for _, test := range []struct {
		name       string
		args       []string
		production string
		cancel     bool
		wantError  bool
	}{
		{"help", []string{"--help"}, prepareProductionFixture, false, false},
		{"required out", nil, prepareProductionFixture, false, true},
		{"positional", []string{"--out", "build/out", "extra"}, prepareProductionFixture, false, true},
		{"cancelled", []string{"--out", "build/out"}, prepareProductionFixture, true, true},
		{"invalid source", []string{"--out", "build/out"}, "FROM wrong AS other\n", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := prepareFixtureRoot(t, test.production)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			err := runPrepare(ctx, test.args, &bytes.Buffer{}, &bytes.Buffer{})
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v", err)
			}
			if _, err := os.Lstat(filepath.Join(root, "build")); !os.IsNotExist(err) {
				t.Fatalf("unexpected write before complete validation: %v", err)
			}
		})
	}
}

func TestGufoFragmentPreservesReviewedBuildAndLicense(t *testing.T) {
	for _, required := range []string{
		"-DCMAKE_HIP_ARCHITECTURES=gfx1151", "-DENGINE_ENABLE_HIP=ON", "-DCMAKE_BUILD_TYPE=Release",
		"-DGUFO_BUILD_TOOLS=OFF", "-DBUILD_TESTING=OFF", "rocm-sdk path --root", "rocm-sdk path --cmake",
		"-DCMAKE_INSTALL_RPATH=${site_packages}/_rocm_sdk_core/lib;${site_packages}/_rocm_sdk_libraries/lib",
		"/opt/gufo/LICENSE /usr/local/share/licenses/gufo/LICENSE", "test ! -e /dev/kfd", "test ! -e /dev/dri",
		"ldd /usr/local/bin/gufo", "gufo --help", "gufo help serve llm", "python -m pip check",
	} {
		if !bytes.Contains(gufoFragment, []byte(required)) {
			t.Errorf("missing reviewed build/check instruction %s", required)
		}
	}
}

func prepareFixtureRoot(t *testing.T, production string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "catalog"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"Containerfile": production, "catalog/catalog.json": "{}"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PARACETAMOL_INTERNAL_PROJECT_ROOT", root)
	return root
}
