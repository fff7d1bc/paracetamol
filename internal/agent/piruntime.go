package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"rocmplete/internal/identity"
	"rocmplete/internal/process"
	"rocmplete/internal/storage"
)

const PiPackage = "@earendil-works/pi-coding-agent"

var exactVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

type PiSource struct {
	Root           string
	PackageJSON    []byte
	PackageLock    []byte
	PackageVersion string
	MinimumNode    [3]int
	LockSHA256     string
}

type PiRuntime struct {
	Root           string
	Node           string
	Entrypoint     string
	PackageVersion string
	NodeVersion    string
	LockSHA256     string
}

type piReceipt struct {
	Schema         int    `json:"schema_version"`
	Package        string `json:"package"`
	PackageVersion string `json:"package_version"`
	LockSHA256     string `json:"lock_sha256"`
	NodeVersion    string `json:"node_version"`
	Entrypoint     string `json:"entrypoint"`
}

func LoadPiSource(root string) (PiSource, error) {
	packageJSON, err := regularFile(filepath.Join(root, "package.json"), "Pi package manifest")
	if err != nil {
		return PiSource{}, err
	}
	packageLock, err := regularFile(filepath.Join(root, "package-lock.json"), "Pi package lock")
	if err != nil {
		return PiSource{}, err
	}
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
		Engines      map[string]string `json:"engines"`
	}
	if err := json.Unmarshal(packageJSON, &manifest); err != nil || len(manifest.Dependencies) != 1 || !exactVersion.MatchString(manifest.Dependencies[PiPackage]) {
		return PiSource{}, fmt.Errorf("Pi package manifest must have one exact %s dependency", PiPackage)
	}
	requirement := manifest.Engines["node"]
	if !strings.HasPrefix(requirement, ">=") {
		return PiSource{}, fmt.Errorf("Pi Node requirement must be >=MAJOR.MINOR.PATCH")
	}
	minimum, err := parseVersion(strings.TrimPrefix(requirement, ">="))
	if err != nil {
		return PiSource{}, fmt.Errorf("invalid Pi Node requirement: %w", err)
	}
	var lock struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Version      string            `json:"version"`
			License      string            `json:"license"`
			Resolved     string            `json:"resolved"`
			Integrity    string            `json:"integrity"`
			Dependencies map[string]string `json:"dependencies"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(packageLock, &lock); err != nil || lock.LockfileVersion != 3 {
		return PiSource{}, fmt.Errorf("Pi package lock is invalid")
	}
	version := manifest.Dependencies[PiPackage]
	if lock.Packages[""].Dependencies[PiPackage] != version || lock.Packages["node_modules/"+PiPackage].Version != version {
		return PiSource{}, fmt.Errorf("Pi package lock does not pin the manifest")
	}
	for path, metadata := range lock.Packages {
		if path == "" {
			continue
		}
		if metadata.Version == "" || metadata.License == "" || !strings.HasPrefix(metadata.Resolved, "https://registry.npmjs.org/") || !strings.HasPrefix(metadata.Integrity, "sha512-") {
			return PiSource{}, fmt.Errorf("Pi package lock has incomplete immutable metadata for %s", path)
		}
	}
	digest := sha256.New()
	_, _ = digest.Write(packageJSON)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(packageLock)
	return PiSource{Root: root, PackageJSON: packageJSON, PackageLock: packageLock, PackageVersion: version, MinimumNode: minimum, LockSHA256: fmt.Sprintf("%x", digest.Sum(nil))}, nil
}

func parseVersion(value string) ([3]int, error) {
	value = strings.TrimPrefix(value, "v")
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf("expected semantic version")
	}
	var result [3]int
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, err
		}
		result[index] = number
	}
	return result, nil
}

func systemExecutable(runner process.Runner, name string) (string, error) {
	for _, candidate := range []string{"/usr/bin/" + name, "/bin/" + name} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return filepath.EvalSymlinks(candidate)
		}
	}
	return "", fmt.Errorf("system %s is required in /bin or /usr/bin", name)
}

func nodeRuntime(ctx context.Context, runner process.Runner, minimum [3]int) (string, string, error) {
	node, err := systemExecutable(runner, "node")
	if err != nil {
		return "", "", err
	}
	result, err := runner.Run(ctx, process.Command{Name: node, Args: []string{"--version"}})
	if err != nil || result.Status != 0 {
		return "", "", fmt.Errorf("cannot query system Node.js")
	}
	version := strings.TrimSpace(string(result.Stdout))
	actual, err := parseVersion(version)
	if err != nil || versionLess(actual, minimum) {
		return "", "", fmt.Errorf("managed Pi requires Node.js >=%d.%d.%d; found %s", minimum[0], minimum[1], minimum[2], version)
	}
	return node, strings.TrimPrefix(version, "v"), nil
}

func piRuntimeRoot(dataRoot string) string { return (storage.Layout{Root: dataRoot}).PiRuntime() }

func piInstallation(root string, source PiSource) string {
	return filepath.Join(root, "installations", source.LockSHA256)
}

func ResolvePiRuntime(ctx context.Context, runner process.Runner, dataRoot, projectRoot string) (PiRuntime, error) {
	source, err := LoadPiSource(filepath.Join(projectRoot, "agent-clients", "pi"))
	if err != nil {
		return PiRuntime{}, err
	}
	node, nodeVersion, err := nodeRuntime(ctx, runner, source.MinimumNode)
	if err != nil {
		return PiRuntime{}, err
	}
	installation := piInstallation(piRuntimeRoot(dataRoot), source)
	if info, err := os.Lstat(installation); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return PiRuntime{}, fmt.Errorf("managed Pi %s is not installed; run %s", source.PackageVersion, identity.Command("agent", "install", "pi"))
	}
	return runtimeFromInstallation(installation, source, node, nodeVersion)
}

func runtimeFromInstallation(installation string, source PiSource, node, nodeVersion string) (PiRuntime, error) {
	contents, err := regularFile(filepath.Join(installation, "receipt.json"), "Pi runtime receipt")
	if err != nil {
		return PiRuntime{}, err
	}
	var receipt piReceipt
	if err := json.Unmarshal(contents, &receipt); err != nil || receipt.Schema != 1 || receipt.Package != PiPackage || receipt.PackageVersion != source.PackageVersion || receipt.LockSHA256 != source.LockSHA256 {
		return PiRuntime{}, fmt.Errorf("Pi runtime receipt does not match this checkout")
	}
	entrypoint := filepath.Join(installation, filepath.FromSlash(receipt.Entrypoint))
	resolvedRoot, err := filepath.EvalSymlinks(installation)
	if err != nil {
		return PiRuntime{}, err
	}
	resolvedEntry, err := filepath.EvalSymlinks(entrypoint)
	if err != nil || !pathWithin(resolvedEntry, resolvedRoot) {
		return PiRuntime{}, fmt.Errorf("Pi entrypoint escapes its installation")
	}
	if info, err := os.Stat(resolvedEntry); err != nil || !info.Mode().IsRegular() {
		return PiRuntime{}, fmt.Errorf("Pi entrypoint is not a regular file")
	}
	return PiRuntime{Root: resolvedRoot, Node: node, Entrypoint: resolvedEntry, PackageVersion: source.PackageVersion, NodeVersion: nodeVersion, LockSHA256: source.LockSHA256}, nil
}

func InstallPiRuntime(ctx context.Context, runner process.Runner, dataRoot, projectRoot string, environment []string, output, errorsOutput io.Writer) (PiRuntime, bool, error) {
	source, err := LoadPiSource(filepath.Join(projectRoot, "agent-clients", "pi"))
	if err != nil {
		return PiRuntime{}, false, err
	}
	node, nodeVersion, err := nodeRuntime(ctx, runner, source.MinimumNode)
	if err != nil {
		return PiRuntime{}, false, err
	}
	npm, err := systemExecutable(runner, "npm")
	if err != nil {
		return PiRuntime{}, false, err
	}
	root := piRuntimeRoot(dataRoot)
	if err := storage.ValidateManagedParent(filepath.Join(root, ".boundary"), root, dataRoot, "Pi runtime"); err != nil {
		return PiRuntime{}, false, err
	}
	for _, item := range []struct {
		path    string
		private bool
	}{{filepath.Dir(filepath.Dir(root)), false}, {filepath.Dir(root), true}, {root, true}, {filepath.Join(root, "installations"), true}} {
		if err := secureDirectory(item.path, item.private); err != nil {
			return PiRuntime{}, false, err
		}
	}
	lock, err := os.OpenFile(filepath.Join(root, ".install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return PiRuntime{}, false, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return PiRuntime{}, false, fmt.Errorf("another Pi runtime installation is active")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	installation := piInstallation(root, source)
	if info, err := os.Lstat(installation); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return PiRuntime{}, false, fmt.Errorf("Pi installation is not a real directory: %s", installation)
		}
		runtime, err := runtimeFromInstallation(installation, source, node, nodeVersion)
		return runtime, false, err
	} else if !os.IsNotExist(err) {
		return PiRuntime{}, false, err
	}
	staging, err := os.MkdirTemp(filepath.Join(root, "installations"), ".install-*")
	if err != nil {
		return PiRuntime{}, false, err
	}
	activated := false
	defer func() {
		if !activated {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := writeAtomic(filepath.Join(staging, "package.json"), source.PackageJSON, 0o600, false); err != nil {
		return PiRuntime{}, false, err
	}
	if err := writeAtomic(filepath.Join(staging, "package-lock.json"), source.PackageLock, 0o600, false); err != nil {
		return PiRuntime{}, false, err
	}
	childEnv := append([]string(nil), environment...)
	childEnv = append(childEnv, "PATH=/usr/bin:/bin", "npm_config_update_notifier=false")
	result, err := runner.Run(ctx, process.Command{Name: npm, Args: []string{"ci", "--prefix", staging, "--omit=dev", "--ignore-scripts", "--no-audit", "--no-fund"}, Env: childEnv, Stdin: os.Stdin, Stdout: output, Stderr: errorsOutput})
	if err != nil || result.Status != 0 {
		return PiRuntime{}, false, fmt.Errorf("npm failed to install managed Pi %s", source.PackageVersion)
	}
	entrypoint, relative, err := installedPiEntrypoint(staging)
	if err != nil {
		return PiRuntime{}, false, err
	}
	probe, err := runner.Run(ctx, process.Command{Name: node, Args: []string{entrypoint, "--version"}, Env: childEnv})
	if err != nil || probe.Status != 0 || strings.TrimSpace(string(probe.Stdout)) != source.PackageVersion {
		return PiRuntime{}, false, fmt.Errorf("staged Pi version check failed")
	}
	receipt, _ := json.MarshalIndent(piReceipt{Schema: 1, Package: PiPackage, PackageVersion: source.PackageVersion, LockSHA256: source.LockSHA256, NodeVersion: nodeVersion, Entrypoint: relative}, "", "  ")
	if err := writeAtomic(filepath.Join(staging, "receipt.json"), append(receipt, '\n'), 0o600, false); err != nil {
		return PiRuntime{}, false, err
	}
	if err := os.Rename(staging, installation); err != nil {
		return PiRuntime{}, false, err
	}
	activated = true
	runtime, err := runtimeFromInstallation(installation, source, node, nodeVersion)
	return runtime, true, err
}

func installedPiEntrypoint(staging string) (string, string, error) {
	root := filepath.Join(staging, "node_modules", filepath.FromSlash(PiPackage))
	contents, err := regularFile(filepath.Join(root, "package.json"), "installed Pi package manifest")
	if err != nil {
		return "", "", err
	}
	var manifest struct {
		Bin map[string]string `json:"bin"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil || manifest.Bin["pi"] == "" {
		return "", "", fmt.Errorf("installed Pi package has no pi binary")
	}
	entrypoint, err := filepath.EvalSymlinks(filepath.Join(root, manifest.Bin["pi"]))
	if err != nil {
		return "", "", err
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if !pathWithin(entrypoint, resolvedRoot) {
		return "", "", fmt.Errorf("installed Pi binary escapes its package")
	}
	relative, _ := filepath.Rel(staging, entrypoint)
	return entrypoint, filepath.ToSlash(relative), nil
}

func pathWithin(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func versionLess(actual, minimum [3]int) bool {
	for index := range actual {
		if actual[index] != minimum[index] {
			return actual[index] < minimum[index]
		}
	}
	return false
}
