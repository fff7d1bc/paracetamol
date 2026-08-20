// Package platform models accelerator capabilities independently of a vendor.
package platform

import (
	"fmt"
	"sort"
	"strings"
)

type Vendor string
type RuntimeFamily string
type Backend string

const (
	VendorAMD Vendor = "amd"

	RuntimeROCm   RuntimeFamily = "rocm"
	RuntimeVulkan RuntimeFamily = "vulkan"

	BackendROCm   Backend = "rocm"
	BackendVulkan Backend = "vulkan"
)

// Profile selects hardware policy; it does not select a physical device.
type Profile struct {
	ID            string
	Vendor        Vendor
	Architectures []string
}

var profiles = map[string]Profile{
	"rdna4": {
		ID:            "rdna4",
		Vendor:        VendorAMD,
		Architectures: []string{"gfx1200", "gfx1201"},
	},
	"strix-halo": {
		ID:            "strix-halo",
		Vendor:        VendorAMD,
		Architectures: []string{"gfx1151"},
	},
	"strix-point": {
		ID:            "strix-point",
		Vendor:        VendorAMD,
		Architectures: []string{"gfx1150"},
	},
}

// ProfileIDs returns the stable GPU profile order used in help and validation.
func ProfileIDs() []string {
	return []string{"rdna4", "strix-halo", "strix-point"}
}

func AllowedProfiles() []string {
	return []string{"auto", "rdna4", "strix-halo", "strix-point", "cpu"}
}

func LookupProfile(identifier string) (Profile, bool) {
	profile, ok := profiles[identifier]
	if !ok {
		return Profile{}, false
	}
	profile.Architectures = append([]string(nil), profile.Architectures...)
	return profile, true
}

func ProfileForArchitecture(architecture string) (Profile, bool) {
	for _, identifier := range ProfileIDs() {
		profile := profiles[identifier]
		for _, supported := range profile.Architectures {
			if architecture == supported {
				return LookupProfile(identifier)
			}
		}
	}
	return Profile{}, false
}

func ValidateProfile(identifier string) error {
	for _, allowed := range AllowedProfiles() {
		if identifier == allowed {
			return nil
		}
	}
	return fmt.Errorf("profile must be one of %s", strings.Join(AllowedProfiles(), ", "))
}

func SupportedArchitectures() []string {
	var architectures []string
	for _, profile := range profiles {
		architectures = append(architectures, profile.Architectures...)
	}
	sort.Strings(architectures)
	return architectures
}

// DeviceSet is the exact host device selection passed to a workload.
type DeviceSet struct {
	Vendor      Vendor
	RenderNodes []string
}

func (set DeviceSet) Clone() DeviceSet {
	set.RenderNodes = append([]string(nil), set.RenderNodes...)
	return set
}
