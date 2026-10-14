package buildplan

import (
	"reflect"
	"testing"

	"paracetamol/internal/application"
)

func TestCommandWithManagedPrerequisites(t *testing.T) {
	got, err := Command(Options{ProjectRoot: "/src/tool", Image: "localhost/tool:app", Target: "app", BaseImage: "localhost/tool:base", RuntimeImage: "localhost/tool:runtime", PipCache: "/cache/pip", VolumeSuffix: ":rw,Z", NoLayerCache: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"podman", "build", "--tag", "localhost/tool:app", "--file", "/src/tool/Containerfile", "--target", "app", "--build-arg", "ROCM_BASE_IMAGE=localhost/tool:base", "--pull=never", "--build-arg", "ROCM_RUNTIME_IMAGE=localhost/tool:runtime", "--pull=never", "--build-arg", "PIP_NO_CACHE_DIR=", "--build-arg", "PIP_CACHE_DIR=/var/cache/paracetamol/pip", "--volume", "/cache/pip:/var/cache/paracetamol/pip:rw,Z", "--no-cache", "/src/tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v\nwant = %#v", got, want)
	}
}

func TestPlanBuildsOnlyDependencyClosure(t *testing.T) {
	steps, err := Plan(Request{ProjectRoot: "/src", Targets: []application.BuildID{application.BuildLlamaCPP}, PipCache: "/cache", VolumeSuffix: ":rw"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Unit.ID != application.BuildRuntime || steps[1].Unit.ID != application.BuildLlamaCPP {
		t.Fatalf("unexpected llama.cpp closure: %#v", steps)
	}
}

func TestPlanNoLayerCacheTouchesOnlySelectedUnit(t *testing.T) {
	steps, err := Plan(Request{ProjectRoot: "/src", Targets: []application.BuildID{application.BuildComfyUI}, PipCache: "/cache", VolumeSuffix: ":rw", NoLayerCache: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Cold != (step.Unit.ID == application.BuildComfyUI) {
			t.Fatalf("cold policy for %s = %v", step.Unit.ID, step.Cold)
		}
	}
}
