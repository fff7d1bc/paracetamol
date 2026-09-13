//go:build linux

package platform

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
)

// ModelProfile resolves only the exact selected devices for profile-scoped
// inventory gates. Unknown topology stays auto, which cannot opt into restricted
// models. Containers still independently validate the architecture with ROCm.
func ModelProfile(profile string, renderNodes []string) string {
	if profile != "auto" {
		return profile
	}
	detected, err := detectModelProfile(os.DirFS("/"), renderNodes)
	if err != nil {
		return "auto"
	}
	return detected
}

func detectModelProfile(files fs.FS, renderNodes []string) (string, error) {
	const topology = "sys/class/kfd/kfd/topology/nodes"
	if len(renderNodes) == 0 {
		return "", fmt.Errorf("no selected render nodes")
	}
	entries, err := fs.ReadDir(files, topology)
	if err != nil {
		return "", err
	}
	byMinor := make(map[int]string)
	for _, entry := range entries {
		contents, err := fs.ReadFile(files, path.Join(topology, entry.Name(), "properties"))
		if err != nil {
			return "", err
		}
		values := map[string]int{}
		for _, line := range strings.Split(string(contents), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 || (fields[0] != "drm_render_minor" && fields[0] != "gfx_target_version") {
				continue
			}
			value, err := strconv.Atoi(fields[1])
			if _, duplicate := values[fields[0]]; err != nil || duplicate || value < 0 {
				return "", fmt.Errorf("invalid KFD topology property")
			}
			values[fields[0]] = value
		}
		minor, target := values["drm_render_minor"], values["gfx_target_version"]
		if minor == 0 && target == 0 { // CPU topology node
			continue
		}
		if minor < 128 || target == 0 {
			return "", fmt.Errorf("incomplete KFD GPU topology")
		}
		// KFD encodes decimal major/minor/stepping, unlike the gfx name's
		// hexadecimal final two digits. For example 110501 means gfx1151.
		architecture := fmt.Sprintf("gfx%d%x%x", target/10000, target/100%100, target%100)
		if _, duplicate := byMinor[minor]; duplicate {
			return "", fmt.Errorf("ambiguous KFD render node")
		}
		byMinor[minor] = architecture
	}
	selectedProfile := ""
	seen := make(map[string]bool)
	for _, node := range renderNodes {
		if !renderNodePattern.MatchString(node) || seen[node] {
			return "", fmt.Errorf("invalid selected render node")
		}
		seen[node] = true
		minor, _ := strconv.Atoi(strings.TrimPrefix(node, "/dev/dri/renderD"))
		profile, ok := ProfileForArchitecture(byMinor[minor])
		if !ok || (selectedProfile != "" && selectedProfile != profile.ID) {
			return "", fmt.Errorf("selected devices have unknown or mixed profiles")
		}
		selectedProfile = profile.ID
	}
	return selectedProfile, nil
}
