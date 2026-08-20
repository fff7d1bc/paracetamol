package buildplan

import (
	"reflect"
	"testing"
)

func TestCommandWithManagedPrerequisites(t *testing.T) {
	got, err := Command(Options{ProjectRoot: "/src/tool", Image: "localhost/tool:app", Target: "app", BaseImage: "localhost/tool:base", RuntimeImage: "localhost/tool:runtime", PipCache: "/cache/pip", VolumeSuffix: ":rw,Z", NoLayerCache: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"podman", "build", "--tag", "localhost/tool:app", "--file", "/src/tool/Containerfile", "--target", "app", "--build-arg", "ROCM_BASE_IMAGE=localhost/tool:base", "--pull=never", "--build-arg", "ROCM_RUNTIME_IMAGE=localhost/tool:runtime", "--pull=never", "--build-arg", "PIP_NO_CACHE_DIR=", "--build-arg", "PIP_CACHE_DIR=/var/cache/rocmplete/pip", "--volume", "/cache/pip:/var/cache/rocmplete/pip:rw,Z", "--no-cache", "/src/tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v\nwant = %#v", got, want)
	}
}
