//go:build linux

package platform

import (
	"reflect"
	"testing"

	"paracetamol/internal/identity"
)

func TestRequestedRenderNodesUsesProductEnvironmentPrefix(t *testing.T) {
	originalCommand, originalPrefix := identity.CommandName, identity.EnvPrefix
	t.Cleanup(func() { identity.CommandName, identity.EnvPrefix = originalCommand, originalPrefix })
	identity.CommandName, identity.EnvPrefix = "renamed-local", ""

	nodes, err := RequestedRenderNodes(nil, false, map[string]string{
		"RENAMED_LOCAL_RENDER_NODES": "/dev/dri/renderD128,/dev/dri/renderD129",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/dev/dri/renderD128", "/dev/dri/renderD129"}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("nodes=%q, want %q", nodes, want)
	}
}
