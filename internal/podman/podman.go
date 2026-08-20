// Package podman owns rootless Podman inspection and ownership metadata.
package podman

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"rocmplete/internal/controlerr"
	"rocmplete/internal/identity"
	"rocmplete/internal/process"
)

const (
	ManagedContainerLabel       = identity.LabelNamespace + ".managed"
	ManagedApplicationLabel     = identity.LabelNamespace + ".application"
	ManagedRoleLabel            = identity.LabelNamespace + ".role"
	SharedContentSELinuxContext = "system_u:object_r:container_file_t:s0"
)

type Client struct{ Runner process.Runner }

// CurrentUmask reads procfs without briefly changing the process-wide umask,
// which would race concurrent launcher work.
func CurrentUmask() string {
	handle, err := os.Open("/proc/self/status")
	if err == nil {
		defer handle.Close()
		scanner := bufio.NewScanner(handle)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) == 2 && fields[0] == "Umask:" {
				if value, parseErr := strconv.ParseUint(fields[1], 8, 32); parseErr == nil {
					return fmt.Sprintf("%04o", value)
				}
			}
		}
	}
	return "0022"
}

func (client Client) runner() process.Runner {
	if client.Runner == nil {
		return process.OSRunner{}
	}
	return client.Runner
}

func (client Client) RequireRootless(ctx context.Context) error {
	if _, err := client.runner().LookPath("podman"); err != nil {
		return controlerr.New("podman is not installed")
	}
	result, err := client.runner().Run(ctx, process.Command{Name: "podman", Args: []string{"info", "--format", "{{.Host.Security.Rootless}}"}})
	if err != nil || result.Status != 0 {
		return controlerr.New("cannot query Podman; verify the rootless Podman service and runtime directory")
	}
	if strings.TrimSpace(string(result.Stdout)) != "true" {
		return controlerr.New("rootful Podman is not supported")
	}
	return nil
}

func (client Client) Exists(ctx context.Context, kind, name string) (bool, error) {
	if kind != "image" && kind != "container" {
		return false, fmt.Errorf("unsupported Podman object kind %q", kind)
	}
	result, err := client.runner().Run(ctx, process.Command{Name: "podman", Args: []string{kind, "exists", name}})
	if err != nil {
		return false, err
	}
	if result.Status == 0 {
		return true, nil
	}
	if result.Status == 1 {
		return false, nil
	}
	return false, controlerr.New("cannot inspect Podman %s %s", kind, name)
}

func ManagedArguments(application, role string) []string {
	arguments := []string{"--label", ManagedContainerLabel + "=true", "--label", ManagedRoleLabel + "=" + role}
	if application != "" {
		arguments = append(arguments, "--label", ManagedApplicationLabel+"="+application)
	}
	return arguments
}

func (client Client) ManagedContainerNames(ctx context.Context, application string) ([]string, error) {
	arguments := []string{"ps", "--all", "--filter", "label=" + ManagedContainerLabel + "=true"}
	if application != "" {
		arguments = append(arguments, "--filter", "label="+ManagedApplicationLabel+"="+application)
	}
	arguments = append(arguments, "--format", "{{.Names}}")
	result, err := client.runner().Run(ctx, process.Command{Name: "podman", Args: arguments})
	if err != nil {
		return nil, controlerr.New("cannot inspect managed containers: %v", err)
	}
	if result.Status != 0 {
		detail := strings.TrimSpace(string(result.Stderr))
		if detail == "" {
			detail = fmt.Sprintf("podman ps exited with status %d", result.Status)
		}
		return nil, controlerr.New("cannot inspect managed containers: %s", detail)
	}
	unique := make(map[string]struct{})
	for _, name := range strings.Fields(string(result.Stdout)) {
		unique[name] = struct{}{}
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (client Client) Capture(ctx context.Context, arguments []string, failure string) (string, error) {
	result, err := client.runner().Run(ctx, process.Command{Name: "podman", Args: arguments})
	if err != nil {
		return "", controlerr.New("%s: %v", failure, err)
	}
	if result.Status != 0 {
		detail := strings.TrimSpace(string(result.Stderr))
		if detail != "" {
			return "", controlerr.New("%s: %s", failure, detail)
		}
		return "", controlerr.New("%s", failure)
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func (client Client) SELinuxVolumeSuffix(ctx context.Context) string {
	getenforce, err := client.runner().LookPath("getenforce")
	if err != nil {
		return ":rw"
	}
	result, err := client.runner().Run(ctx, process.Command{Name: getenforce})
	if err == nil && strings.TrimSpace(string(result.Stdout)) == "Enforcing" {
		return ":rw,Z"
	}
	return ":rw"
}

func (client Client) SharedSELinuxVolumeSuffix(ctx context.Context) string {
	return strings.Replace(client.SELinuxVolumeSuffix(ctx), ",Z", ",z", 1)
}

// PrepareSharedContentLabel makes newly materialized model bytes readable by
// later containers with independent MCS categories. It is a no-op away from
// enforcing SELinux hosts.
func (client Client) PrepareSharedContentLabel(ctx context.Context, path string) error {
	if client.SELinuxVolumeSuffix(ctx) != ":rw,Z" {
		return nil
	}
	chcon, err := client.runner().LookPath("chcon")
	if err != nil {
		return controlerr.New("cannot prepare shared SELinux label for %s: chcon is not installed", path)
	}
	result, err := client.runner().Run(ctx, process.Command{Name: chcon, Args: []string{"--no-dereference", SharedContentSELinuxContext, "--", path}})
	if err != nil {
		return controlerr.New("cannot prepare shared SELinux label for %s: %v", path, err)
	}
	if result.Status != 0 {
		detail := strings.TrimSpace(string(result.Stderr))
		if detail == "" {
			detail = fmt.Sprintf("exit status %d", result.Status)
		}
		return controlerr.New("cannot prepare shared SELinux label for %s: %s", path, detail)
	}
	return nil
}

// SELinuxContainerDeviceAccess reports the enforcing host's boolean policy.
// A nil result means the host is not enforcing or the policy is unavailable.
func (client Client) SELinuxContainerDeviceAccess(ctx context.Context) (*bool, error) {
	getenforce, err := client.runner().LookPath("getenforce")
	if err != nil {
		return nil, nil
	}
	getsebool, err := client.runner().LookPath("getsebool")
	if err != nil {
		return nil, nil
	}
	enforcing, err := client.runner().Run(ctx, process.Command{Name: getenforce})
	if err != nil || enforcing.Status != 0 || strings.TrimSpace(string(enforcing.Stdout)) != "Enforcing" {
		return nil, nil
	}
	policy, err := client.runner().Run(ctx, process.Command{Name: getsebool, Args: []string{"container_use_devices"}})
	if err != nil || policy.Status != 0 {
		return nil, nil
	}
	fields := strings.Fields(string(policy.Stdout))
	if len(fields) == 0 {
		return nil, nil
	}
	value := fields[len(fields)-1]
	if value == "on" {
		allowed := true
		return &allowed, nil
	}
	if value == "off" {
		allowed := false
		return &allowed, nil
	}
	return nil, nil
}

func (client Client) RequireContainerDeviceAccess(ctx context.Context) error {
	allowed, err := client.SELinuxContainerDeviceAccess(ctx)
	if err != nil {
		return err
	}
	if allowed != nil && !*allowed {
		return controlerr.New("SELinux blocks GPU memory mapping because container_use_devices is off; run 'sudo setsebool -P container_use_devices 1' and retry")
	}
	return nil
}
