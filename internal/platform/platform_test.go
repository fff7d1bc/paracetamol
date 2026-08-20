package platform

import (
	"reflect"
	"testing"
)

func TestProfileForArchitecture(t *testing.T) {
	profile, ok := ProfileForArchitecture("gfx1151")
	if !ok || profile.ID != "strix-halo" || profile.Vendor != VendorAMD {
		t.Fatalf("unexpected profile: %#v, %v", profile, ok)
	}
}

func TestSupportedArchitecturesAreStable(t *testing.T) {
	want := []string{"gfx1150", "gfx1151", "gfx1200", "gfx1201"}
	if got := SupportedArchitectures(); !reflect.DeepEqual(got, want) {
		t.Fatalf("architectures = %v, want %v", got, want)
	}
}
