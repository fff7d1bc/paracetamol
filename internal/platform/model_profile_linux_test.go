package platform

import (
	"testing"
	"testing/fstest"
)

func TestModelProfileExactSelectedKFDNodes(t *testing.T) {
	files := fstest.MapFS{
		"sys/class/kfd/kfd/topology/nodes/0/properties": {Data: []byte("gfx_target_version 0\ndrm_render_minor 0\n")},
		"sys/class/kfd/kfd/topology/nodes/1/properties": {Data: []byte("gfx_target_version 110501\ndrm_render_minor 128\n")},
		"sys/class/kfd/kfd/topology/nodes/2/properties": {Data: []byte("gfx_target_version 120001\ndrm_render_minor 129\n")},
	}
	for node, want := range map[string]string{"/dev/dri/renderD128": "strix-halo", "/dev/dri/renderD129": "rdna4"} {
		got, err := detectModelProfile(files, []string{node})
		if err != nil || got != want {
			t.Fatalf("%s: profile %s, error %v", node, got, err)
		}
	}
	for _, nodes := range [][]string{nil, {"/dev/dri/renderD130"}, {"/dev/dri/renderD128", "/dev/dri/renderD129"}, {"/dev/dri/renderD128", "/dev/dri/renderD128"}, {"/tmp/renderD128"}} {
		if _, err := detectModelProfile(files, nodes); err == nil {
			t.Fatalf("accepted invalid device selection %v", nodes)
		}
	}
}

func TestModelProfileMalformedAndMissingTopology(t *testing.T) {
	for _, properties := range []string{"", "drm_render_minor 128", "gfx_target_version 110501", "gfx_target_version potato\ndrm_render_minor 128", "gfx_target_version 110501\ngfx_target_version 110501\ndrm_render_minor 128"} {
		files := fstest.MapFS{"sys/class/kfd/kfd/topology/nodes/1/properties": {Data: []byte(properties)}}
		if _, err := detectModelProfile(files, []string{"/dev/dri/renderD128"}); err == nil {
			t.Fatalf("accepted invalid properties %q", properties)
		}
	}
	if got := ModelProfile("auto", nil); got != "auto" {
		t.Fatalf("unknown hardware opted into %s", got)
	}
	if got := ModelProfile("cpu", nil); got != "cpu" {
		t.Fatalf("changed explicit CPU profile to %s", got)
	}
}
