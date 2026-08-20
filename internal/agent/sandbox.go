package agent

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"rocmplete/internal/identity"
	"rocmplete/internal/process"
	"rocmplete/internal/storage"
)

var (
	SandboxHome    = "/run/" + identity.StateNamespace + "/home"
	SandboxRuntime = "/run/" + identity.StateNamespace + "/runtime"
)

type SandboxPaths struct {
	Root   string
	Config string
	Data   string
	State  string
	Cache  string
}

type Mount struct{ Source, Destination string }

type SandboxPlan struct {
	Command     []string
	Environment []string
	Workdir     string
	StateRoot   string
}

func PrepareSandboxPaths(dataRoot, application string) (SandboxPaths, error) {
	root := filepath.Join((storage.Layout{Root: dataRoot}).Application(application), "sandbox")
	paths := SandboxPaths{Root: root, Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), State: filepath.Join(root, "state"), Cache: filepath.Join(root, "cache")}
	if err := storage.ValidateManagedParent(filepath.Join(paths.Cache, ".boundary"), root, dataRoot, application+" sandbox state"); err != nil {
		return SandboxPaths{}, err
	}
	for _, item := range []struct {
		path    string
		private bool
	}{{filepath.Dir(filepath.Dir(root)), false}, {filepath.Dir(root), true}, {root, true}, {paths.Config, true}, {paths.Data, true}, {paths.State, true}, {paths.Cache, true}} {
		if err := secureDirectory(item.path, item.private); err != nil {
			return SandboxPaths{}, err
		}
	}
	return paths, nil
}

func CreateSandboxPlan(ctx context.Context, runner process.Runner, command []string, dataRoot, workdir, application string, childEnvironment, environment map[string]string, readOnly []Mount) (SandboxPlan, error) {
	if len(command) == 0 {
		return SandboxPlan{}, fmt.Errorf("empty agent command")
	}
	paths, err := PrepareSandboxPaths(dataRoot, application)
	if err != nil {
		return SandboxPlan{}, err
	}
	working, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return SandboxPlan{}, fmt.Errorf("cannot resolve agent working directory: %w", err)
	}
	info, err := os.Stat(working)
	if err != nil || !info.IsDir() {
		return SandboxPlan{}, fmt.Errorf("agent working directory is not a directory: %s", working)
	}
	home := environment["HOME"]
	if resolved, resolveErr := filepath.EvalSymlinks(home); resolveErr == nil && pathWithin(resolved, working) {
		return SandboxPlan{}, fmt.Errorf("refusing sandbox workdir that contains the host home: %s", working)
	}
	if pathWithin(working, paths.Root) || pathWithin(paths.Root, working) {
		return SandboxPlan{}, fmt.Errorf("sandbox workdir overlaps private state: %s", working)
	}
	bwrap, err := runner.LookPath("bwrap")
	if err != nil {
		return SandboxPlan{}, fmt.Errorf("bubblewrap executable not found; install bubblewrap or use --no-sandbox")
	}
	bwrap, err = filepath.EvalSymlinks(bwrap)
	if err != nil {
		return SandboxPlan{}, err
	}
	executable, err := filepath.EvalSymlinks(command[0])
	if err != nil {
		return SandboxPlan{}, fmt.Errorf("cannot resolve agent executable: %w", err)
	}
	prefix := linuxbrewPrefix(executable)
	resolver := runtimeResolver()
	mdns := runtimeMDNSSocket()
	destinations := []string{SandboxHome, filepath.Join(SandboxHome, ".config"), filepath.Join(SandboxHome, ".local"), filepath.Join(SandboxHome, ".local", "share"), filepath.Join(SandboxHome, ".local", "state"), filepath.Join(SandboxHome, ".cache"), SandboxRuntime, filepath.Dir(working)}
	if resolver != "" {
		destinations = append(destinations, filepath.Dir(resolver))
	}
	if mdns != "" {
		destinations = append(destinations, filepath.Dir(mdns))
	}
	for _, mount := range readOnly {
		destinations = append(destinations, filepath.Dir(mount.Destination))
	}
	if prefix != "" {
		destinations = append(destinations, filepath.Dir(prefix))
	} else {
		destinations = append(destinations, filepath.Dir(executable))
	}
	arguments := []string{bwrap, "--unshare-all", "--share-net", "--die-with-parent", "--new-session", "--hostname", identity.StateNamespace, "--cap-drop", "ALL", "--clearenv", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run", "--ro-bind", "/usr", "/usr", "--ro-bind", "/etc", "/etc", "--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin", "--symlink", "usr/lib", "/lib"}
	if _, err := os.Stat("/usr/lib64"); err == nil {
		arguments = append(arguments, "--symlink", "usr/lib64", "/lib64")
	}
	if target, err := os.Readlink("/home"); err == nil && (target == "/var/home" || target == "../var/home") {
		arguments = append(arguments, "--symlink", target, "/home")
	}
	arguments = append(arguments, directoryArguments(destinations)...)
	if resolver != "" {
		arguments = append(arguments, "--ro-bind", resolver, resolver)
	}
	if mdns != "" {
		arguments = append(arguments, "--ro-bind", mdns, mdns)
	}
	if prefix != "" {
		arguments = append(arguments, "--ro-bind", prefix, prefix)
	} else if !pathWithin(executable, "/usr") {
		arguments = append(arguments, "--ro-bind", executable, executable)
	}
	for _, mount := range readOnly {
		source, err := filepath.EvalSymlinks(mount.Source)
		if err != nil {
			return SandboxPlan{}, err
		}
		arguments = append(arguments, "--ro-bind", source, mount.Destination)
	}
	arguments = append(arguments, "--bind", working, working)
	for _, mount := range []Mount{{paths.Config, filepath.Join(SandboxHome, ".config")}, {paths.Data, filepath.Join(SandboxHome, ".local", "share")}, {paths.State, filepath.Join(SandboxHome, ".local", "state")}, {paths.Cache, filepath.Join(SandboxHome, ".cache")}} {
		arguments = append(arguments, "--bind", mount.Source, mount.Destination)
	}
	username := environment["USER"]
	if username == "" {
		if current, err := user.Current(); err == nil {
			username = current.Username
		}
	}
	child := map[string]string{"HOME": SandboxHome, "PATH": sandboxPath(executable), "SHELL": "/bin/sh", "USER": username, "LOGNAME": username, "TMPDIR": "/tmp", "XDG_CONFIG_HOME": filepath.Join(SandboxHome, ".config"), "XDG_DATA_HOME": filepath.Join(SandboxHome, ".local", "share"), "XDG_STATE_HOME": filepath.Join(SandboxHome, ".local", "state"), "XDG_CACHE_HOME": filepath.Join(SandboxHome, ".cache"), "XDG_RUNTIME_DIR": SandboxRuntime}
	for key, value := range childEnvironment {
		child[key] = value
	}
	for key, value := range environment {
		if key == "COLORTERM" || key == "LANG" || key == "LANGUAGE" || key == "LC_ALL" || key == "NO_COLOR" || key == "TERM" || key == "TZ" || strings.HasPrefix(key, "LC_") {
			child[key] = value
		}
	}
	for variable, key := range map[string]string{"GIT_AUTHOR_NAME": "user.name", "GIT_AUTHOR_EMAIL": "user.email"} {
		value := environment[variable]
		if value == "" {
			if git, err := runner.LookPath("git"); err == nil {
				result, _ := runner.Run(ctx, process.Command{Name: git, Args: []string{"config", "--global", "--get", key}})
				if result.Status == 0 {
					value = strings.TrimRight(string(result.Stdout), "\r\n")
				}
			}
		}
		if value != "" && !strings.ContainsAny(value, "\x00\n") {
			child[variable] = value
		}
	}
	if child["GIT_AUTHOR_NAME"] != "" {
		child["GIT_COMMITTER_NAME"] = child["GIT_AUTHOR_NAME"]
	}
	if child["GIT_AUTHOR_EMAIL"] != "" {
		child["GIT_COMMITTER_EMAIL"] = child["GIT_AUTHOR_EMAIL"]
	}
	keys := make([]string, 0, len(child))
	for key := range child {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		arguments = append(arguments, "--setenv", key, child[key])
	}
	arguments = append(arguments, "--chdir", working, "--", executable)
	arguments = append(arguments, command[1:]...)
	return SandboxPlan{Command: arguments, Environment: []string{"PATH=" + environment["PATH"]}, Workdir: working, StateRoot: paths.Root}, nil
}

func directoryArguments(paths []string) []string {
	directories := make(map[string]bool)
	for _, value := range paths {
		current := filepath.Clean(value)
		for current != "/" && current != "." {
			directories[current] = true
			current = filepath.Dir(current)
		}
	}
	values := make([]string, 0, len(directories))
	for value := range directories {
		if value == "/usr" || pathWithin(value, "/usr") || value == "/etc" || pathWithin(value, "/etc") {
			continue
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		di, dj := strings.Count(values[i], "/"), strings.Count(values[j], "/")
		if di != dj {
			return di < dj
		}
		return values[i] < values[j]
	})
	var result []string
	for _, value := range values {
		result = append(result, "--dir", value)
	}
	return result
}

func linuxbrewPrefix(executable string) string {
	current := filepath.Dir(executable)
	for current != "/" {
		if filepath.Base(current) == ".linuxbrew" {
			return current
		}
		current = filepath.Dir(current)
	}
	return ""
}

func sandboxPath(executable string) string {
	var entries []string
	if prefix := linuxbrewPrefix(executable); prefix != "" {
		entries = append(entries, filepath.Join(prefix, "bin"), filepath.Join(prefix, "sbin"))
	}
	return strings.Join(append(entries, "/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"), ":")
}

func runtimeResolver() string {
	info, err := os.Lstat("/etc/resolv.conf")
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return ""
	}
	target, err := filepath.EvalSymlinks("/etc/resolv.conf")
	if err != nil || !pathWithin(target, "/run") || target == "/run" {
		return ""
	}
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
		return target
	}
	return ""
}

func runtimeMDNSSocket() string {
	path := "/run/avahi-daemon/socket"
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	if status, ok := info.Sys().(*syscall.Stat_t); ok && status.Mode&syscall.S_IFMT == syscall.S_IFSOCK {
		return path
	}
	return ""
}
