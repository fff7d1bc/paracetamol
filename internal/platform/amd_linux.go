//go:build linux

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"paracetamol/internal/identity"
)

var renderNodePattern = regexp.MustCompile(`^/dev/dri/renderD[0-9]+$`)

const accessReadWrite = 4 | 2

func RequestedRenderNodes(values []string, explicitlySet bool, environment map[string]string) ([]string, error) {
	if explicitlySet {
		return append([]string(nil), values...), nil
	}
	pluralName := identity.EnvironmentPrefix() + "_RENDER_NODES"
	singularName := identity.EnvironmentPrefix() + "_RENDER_NODE"
	if plural, ok := environment[pluralName]; ok {
		parts := strings.Split(plural, ",")
		for index := range parts {
			parts[index] = strings.TrimSpace(parts[index])
			if parts[index] == "" {
				return nil, fmt.Errorf("%s must be a comma-separated list of exact render nodes", pluralName)
			}
		}
		return parts, nil
	}
	if singular := environment[singularName]; singular != "" {
		return []string{singular}, nil
	}
	return nil, nil
}

func SelectRenderNodes(requested []string) ([]string, error) {
	pluralName := identity.EnvironmentPrefix() + "_RENDER_NODES"
	selected := append([]string(nil), requested...)
	if len(selected) == 0 {
		nodes, err := filepath.Glob("/dev/dri/renderD*")
		if err != nil {
			return nil, err
		}
		sort.Strings(nodes)
		if len(nodes) == 0 {
			return nil, fmt.Errorf("no /dev/dri/renderD* nodes found; use --profile cpu for a CPU-only smoke test")
		}
		if len(nodes) > 1 {
			return nil, fmt.Errorf("multiple render nodes found (%s); select an exact set with --render-node or %s", strings.Join(nodes, ", "), pluralName)
		}
		selected = nodes
	}
	seen := make(map[string]bool)
	for _, node := range selected {
		if seen[node] {
			return nil, fmt.Errorf("render nodes must not contain duplicates")
		}
		seen[node] = true
		if !renderNodePattern.MatchString(node) {
			return nil, fmt.Errorf("render node must look like /dev/dri/renderD128")
		}
		if _, err := os.Stat(node); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("render node does not exist: %s", node)
			}
			return nil, fmt.Errorf("inspect render node %s: %w", node, err)
		}
	}
	return selected, nil
}

func CheckDeviceAccess(device string) error {
	if _, err := os.Stat(device); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("device does not exist: %s", device)
		}
		return fmt.Errorf("inspect device %s: %w", device, err)
	}
	if err := syscall.Access(device, accessReadWrite); err != nil {
		return fmt.Errorf("current user needs read and write access to %s; run %q for a persistent host fix", device, identity.Command("doctor"))
	}
	return nil
}

func CheckAMDDeviceAccess(renderNodes []string) error {
	if err := CheckDeviceAccess("/dev/kfd"); err != nil {
		return err
	}
	for _, node := range renderNodes {
		if err := CheckDeviceAccess(node); err != nil {
			return err
		}
	}
	return nil
}
