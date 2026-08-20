package content

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
	"paracetamol/internal/ui"
	"paracetamol/internal/verification"
)

type InstallOptions struct {
	Context        context.Context
	DataRoot       string
	Artifacts      []catalog.Artifact
	Image          string
	AcceptRisk     bool
	LocalMirror    string
	MoveFromMirror bool
	DryRun         bool
	Environment    map[string]string
	Runner         process.Runner
	Output         io.Writer
}

type InstallPlan struct {
	Statuses      []ArtifactStatus
	DownloadBytes int64
	VerifyBytes   int64
	Ready         int
	Risky         []catalog.Artifact
}

var downloaderSequence atomic.Uint64

func UniqueArtifacts(managed catalog.Catalog, bundles []catalog.Bundle) []catalog.Artifact {
	seen := make(map[string]bool)
	result := make([]catalog.Artifact, 0)
	for _, bundle := range bundles {
		for _, identifier := range bundle.Artifacts {
			if !seen[identifier] {
				seen[identifier] = true
				result = append(result, managed.Artifacts[identifier])
			}
		}
	}
	return result
}

func Plan(dataRoot string, artifacts []catalog.Artifact) (InstallPlan, error) {
	store, err := verification.Load(dataRoot)
	if err != nil {
		return InstallPlan{}, err
	}
	plan := InstallPlan{Statuses: make([]ArtifactStatus, 0, len(artifacts))}
	seenDownloads := make(map[string]bool)
	for _, artifact := range artifacts {
		status, inspectErr := InspectArtifact(store, dataRoot, artifact, false)
		if inspectErr != nil {
			return InstallPlan{}, inspectErr
		}
		plan.Statuses = append(plan.Statuses, status)
		switch status.State {
		case Verified:
			plan.Ready++
		case Unverified:
			plan.VerifyBytes += artifact.Size
		case Missing:
			key := downloadKey(artifact)
			if !seenDownloads[key] {
				seenDownloads[key] = true
				plan.DownloadBytes += downloadSize(artifact)
			}
			if artifact.License.Status != "verified" {
				plan.Risky = append(plan.Risky, artifact)
			}
		}
	}
	return plan, nil
}

func Install(options InstallOptions) error {
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Runner == nil {
		options.Runner = process.OSRunner{}
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Image == "" {
		options.Image = config.ContentToolsImage
	}
	plan, err := Plan(options.DataRoot, options.Artifacts)
	if err != nil {
		return err
	}
	for _, status := range plan.Statuses {
		if status.State == Unexpected || status.State == SizeMismatch || status.State == HashMismatch {
			return controlerr.New("refusing to replace unexpected managed content: %s (%s)", status.Path, status.State)
		}
	}
	if len(plan.Risky) > 0 && !options.AcceptRisk {
		return controlerr.New("selected missing content has unverified licensing; pass --acknowledge-license-risk after reviewing its catalog warnings")
	}
	if options.DryRun {
		return nil
	}
	if err := os.MkdirAll(options.DataRoot, 0o755); err != nil {
		return fmt.Errorf("prepare data directory: %w", err)
	}
	lock, err := acquireInstallLock(options.DataRoot, options.Environment)
	if err != nil {
		return err
	}
	defer releaseInstallLock(lock)

	store, err := verification.Load(options.DataRoot)
	if err != nil {
		return err
	}
	mirror, err := openMirror(options.LocalMirror, options.DataRoot, options.MoveFromMirror)
	if err != nil {
		return err
	}
	client := podman.Client{Runner: options.Runner}
	needsNetwork := false
	mirrorMatches := make(map[string]string)
	for _, artifact := range options.Artifacts {
		status, inspectErr := InspectArtifact(store, options.DataRoot, artifact, false)
		if inspectErr != nil {
			return inspectErr
		}
		if status.State == Missing && !stagedPayloadReady(options.DataRoot, artifact) {
			candidate, findErr := mirror.find(options.Context, artifact, options.Output, options.Environment)
			if findErr != nil {
				return findErr
			}
			mirrorMatches[artifact.ID] = candidate
			if candidate == "" && !stagedDownloadReady(options.DataRoot, artifact) {
				needsNetwork = true
			}
		}
	}
	if needsNetwork {
		if err := client.RequireRootless(options.Context); err != nil {
			return err
		}
		present, err := client.Exists(options.Context, "image", options.Image)
		if err != nil {
			return err
		}
		if !present {
			return controlerr.New("content tools image not found: %s\n  Build it with: %s", options.Image, identity.Command("build", "content-tools"))
		}
	}
	completedStaging := make(map[string]bool)
	for index, artifact := range options.Artifacts {
		status, inspectErr := InspectArtifact(store, options.DataRoot, artifact, false)
		if inspectErr != nil {
			return inspectErr
		}
		if status.State == Verified {
			continue
		}
		if status.State == Unverified {
			terminal := ui.New(options.Output, options.Environment)
			fmt.Fprintf(options.Output, "\n%s SHA-256 for existing %s (%s)\n", terminal.Heading("Verifying"), artifact.Destination, humanBytes(artifact.Size))
			// Relabel before hashing. On enforcing SELinux hosts chcon changes
			// filesystem identity, and a newly written receipt must describe the
			// post-label file that application containers will actually mount.
			if err := client.PrepareSharedContentLabel(options.Context, status.Path); err != nil {
				return err
			}
			progress := ui.NewProgress(options.Output, options.Environment, artifact.Size)
			progress.Update(artifact.Destination, index+1, len(options.Artifacts), 0, false)
			verified, verifyErr := VerifyArtifact(options.Context, store, options.DataRoot, artifact, func(hashed int64) {
				progress.Update(artifact.Destination, index+1, len(options.Artifacts), hashed, false)
			})
			if verifyErr == nil {
				progress.Update(artifact.Destination, index+1, len(options.Artifacts), artifact.Size, true)
			} else {
				progress.Finish()
			}
			if verifyErr != nil {
				return verifyErr
			}
			if verified.State != Verified {
				return controlerr.New("SHA-256 mismatch for existing managed content: %s", verified.Path)
			}
			continue
		}
		if status.State != Missing {
			return controlerr.New("managed content is not installable: %s (%s)", status.Path, status.State)
		}
		if err := installMissing(options, client, store, mirror, mirrorMatches[artifact.ID], artifact, index+1); err != nil {
			return err
		}
		completedStaging[stagingRoot(options.DataRoot, artifact)] = true
	}
	if err := store.Save(); err != nil {
		return err
	}
	for staging := range completedStaging {
		if err := storage.ValidateManagedParent(filepath.Join(staging, ".cleanup-boundary"), staging, options.DataRoot, "completed staging"); err != nil {
			return err
		}
		if err := os.RemoveAll(staging); err != nil {
			return fmt.Errorf("remove completed staging %s: %w", staging, err)
		}
	}
	return nil
}

func installMissing(options InstallOptions, client podman.Client, store *verification.Store, mirror localMirror, mirrorCandidate string, artifact catalog.Artifact, index int) error {
	payload := payloadPath(options.DataRoot, artifact)
	download := downloadPath(options.DataRoot, artifact)
	if err := storage.ValidateManagedParent(payload, stagingPartition(options.DataRoot, artifact), options.DataRoot, "staging"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(payload), 0o755); err != nil {
		return err
	}
	if !stagedPayloadReady(options.DataRoot, artifact) {
		if mirrorCandidate != "" {
			terminal := ui.New(options.Output, options.Environment)
			action := "Copying"
			if mirror.move {
				action = "Moving"
			}
			fmt.Fprintf(options.Output, "%s local mirror file: %s\n", terminal.Heading(action), mirrorCandidate)
			if err := mirror.materialize(mirrorCandidate, payload); err != nil {
				return err
			}
		} else {
			if !stagedDownloadReady(options.DataRoot, artifact) {
				if artifact.Source.Provider == "civitai" && artifact.Source.RequiresAuth && options.Environment["CIVITAI_TOKEN"] == "" {
					return controlerr.New("CIVITAI_TOKEN is required to download %s", artifact.ID)
				}
				if err := (storage.Layout{Root: options.DataRoot}).PrepareDownloads(); err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(download), 0o755); err != nil {
					return err
				}
				terminal := ui.New(options.Output, options.Environment)
				fmt.Fprintf(options.Output, "\n%s [%d/%d] %s (%s)\n", terminal.Heading("Downloading"), index, len(options.Artifacts), artifact.Destination, humanBytes(downloadSize(artifact)))
				containerName := identity.Container(fmt.Sprintf("download-%d-%d", os.Getpid(), downloaderSequence.Add(1)))
				command, err := downloadCommand(options, client, artifact, containerName)
				if err != nil {
					return err
				}
				measurement := newDownloadMeasurement(stagingRoot(options.DataRoot, artifact), download, artifact)
				progress := ui.NewDownloadProgress(options.Output, options.Environment, downloadSize(artifact), artifact.Source.ArchiveMember != "")
				progress.Update(measurement.bytes(), false)
				type downloadResult struct {
					result process.Result
					err    error
				}
				completed := make(chan downloadResult, 1)
				go func() {
					result, runErr := options.Runner.Run(options.Context, process.Command{Name: command[0], Args: command[1:], Stdin: nil, Stdout: io.Discard})
					completed <- downloadResult{result: result, err: runErr}
				}()
				ticker := time.NewTicker(time.Second)
				var outcome downloadResult
			waiting:
				for {
					select {
					case outcome = <-completed:
						break waiting
					case <-ticker.C:
						progress.Update(measurement.bytes(), false)
					}
				}
				ticker.Stop()
				progress.Update(measurement.bytes(), true)
				result, err := outcome.result, outcome.err
				if len(result.Stderr) > 0 {
					fmt.Fprint(options.Output, string(result.Stderr))
					if result.Stderr[len(result.Stderr)-1] != '\n' {
						fmt.Fprintln(options.Output)
					}
				}
				cleanupErr := client.RemoveContainer(context.WithoutCancel(options.Context), containerName, 0, podman.Streams{})
				if err != nil {
					if cleanupErr != nil {
						return fmt.Errorf("download process failed: %w; additionally could not remove %s: %v", err, containerName, cleanupErr)
					}
					return err
				}
				if result.Status != 0 {
					if cleanupErr != nil {
						return controlerr.New("download failed for %s (exit status %d); additionally could not remove %s: %v", artifact.Source.Path, result.Status, containerName, cleanupErr)
					}
					return controlerr.New("download failed for %s (exit status %d)", artifact.Source.Path, result.Status)
				}
				if cleanupErr != nil {
					return cleanupErr
				}
			}
			if artifact.Source.ArchiveMember != "" {
				if err := extractArchive(artifact, download, payload); err != nil {
					return err
				}
			}
		}
	}
	info, err := os.Lstat(payload)
	if err != nil || !info.Mode().IsRegular() || info.Size() != artifact.Size {
		return controlerr.New("downloader did not create the expected regular file: %s", payload)
	}
	// Prepare the final shared label before hashing. Besides changing ctime on
	// SELinux, this is an external mutation boundary: only bytes verified after
	// it returns may be published and receipted.
	if err := client.PrepareSharedContentLabel(options.Context, payload); err != nil {
		return err
	}
	labeled, err := os.Lstat(payload)
	if err != nil || !labeled.Mode().IsRegular() || labeled.Size() != artifact.Size {
		return controlerr.New("managed content changed while preparing its shared label: %s", payload)
	}
	fingerprint := fileIdentity(labeled)
	terminal := ui.New(options.Output, options.Environment)
	fmt.Fprintf(options.Output, "\n%s SHA-256 for %s (%s)\n", terminal.Heading("Verifying"), artifact.Destination, humanBytes(artifact.Size))
	progress := ui.NewProgress(options.Output, options.Environment, artifact.Size)
	progress.Update(artifact.Destination, index, len(options.Artifacts), 0, false)
	digest, err := fileSHA256Context(options.Context, payload, func(hashed int64) {
		progress.Update(artifact.Destination, index, len(options.Artifacts), hashed, false)
	})
	if err == nil {
		progress.Update(artifact.Destination, index, len(options.Artifacts), artifact.Size, true)
	} else {
		progress.Finish()
	}
	if err != nil {
		return err
	}
	if digest != artifact.SHA256 {
		quarantine := fmt.Sprintf("%s.invalid-%s", payload, digest[:12])
		if renameErr := os.Rename(payload, quarantine); renameErr != nil {
			return controlerr.New("SHA-256 mismatch for %s and cannot quarantine it: %v", payload, renameErr)
		}
		return controlerr.New("SHA-256 mismatch for %s; rejected bytes kept at %s", payload, quarantine)
	}
	hashed, err := os.Lstat(payload)
	if err != nil || !hashed.Mode().IsRegular() || fileIdentity(hashed) != fingerprint {
		return controlerr.New("managed content changed during verification: %s", payload)
	}
	destination := ArtifactPath(options.DataRoot, artifact)
	root := artifactRoot(options.DataRoot, artifact)
	if err := storage.ValidateManagedParent(destination, root, options.DataRoot, "content"); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return controlerr.New("destination appeared during download; refusing to overwrite: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := storage.ValidateManagedParent(destination, root, options.DataRoot, "content"); err != nil {
		return err
	}
	recheck, err := os.Lstat(payload)
	if err != nil || !recheck.Mode().IsRegular() || fileIdentity(recheck) != fingerprint {
		return controlerr.New("managed content changed after verification: %s", payload)
	}
	if err := os.Rename(payload, destination); err != nil {
		return fmt.Errorf("install %s: %w", destination, err)
	}
	installed, err := os.Lstat(destination)
	if err != nil || !installed.Mode().IsRegular() || fileObject(installed) != fileObject(recheck) {
		return controlerr.New("managed content changed during installation: %s", destination)
	}
	if err := store.Record(destination, artifact.Size, artifact.SHA256); err != nil {
		return err
	}
	fmt.Fprintf(options.Output, "%s %s\n", terminal.Success("Installed"), destination)
	return nil
}

type downloadFileSignature struct {
	size       int64
	modifiedNS int64
}

// downloadMeasurement follows only the requested payload and unambiguous HF
// partials that appeared or changed during this attempt. It intentionally does
// not sum the whole staging tree, where abandoned retry files may remain.
type downloadMeasurement struct {
	root     string
	target   string
	expected int64
	marker   string
	initial  map[string]downloadFileSignature
	active   map[string]bool
}

func newDownloadMeasurement(root, target string, artifact catalog.Artifact) *downloadMeasurement {
	measurement := &downloadMeasurement{root: root, target: target, expected: downloadSize(artifact), active: make(map[string]bool)}
	if artifact.Source.Provider == "huggingface" {
		measurement.marker = "." + artifact.SHA256 + "."
	}
	measurement.initial = measurement.partialSignatures()
	return measurement
}

func (measurement *downloadMeasurement) bytes() int64 {
	if size, ok := regularSize(measurement.target); ok {
		return min(size, measurement.expected)
	}
	current := measurement.partialSignatures()
	for path, signature := range current {
		if previous, ok := measurement.initial[path]; !ok || previous != signature {
			measurement.active[path] = true
		}
	}
	var largest int64
	for path := range measurement.active {
		if signature, ok := current[path]; ok && signature.size > largest {
			largest = signature.size
		}
	}
	return min(largest, measurement.expected)
}

func (measurement *downloadMeasurement) partialSignatures() map[string]downloadFileSignature {
	result := make(map[string]downloadFileSignature)
	if measurement.marker == "" {
		return result
	}
	relative, err := filepath.Rel(measurement.root, measurement.target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return result
	}
	cache := filepath.Join(measurement.root, ".cache", "huggingface", "download", filepath.Dir(relative))
	entries, err := os.ReadDir(cache)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if !strings.Contains(entry.Name(), measurement.marker) || !strings.HasSuffix(entry.Name(), ".incomplete") {
			continue
		}
		path := filepath.Join(cache, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		result[path] = downloadFileSignature{size: info.Size(), modifiedNS: info.ModTime().UnixNano()}
	}
	return result
}

func regularSize(path string) (int64, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	return info.Size(), true
}

func downloadCommand(options InstallOptions, client podman.Client, artifact catalog.Artifact, containerName string) ([]string, error) {
	relative, err := filepath.Rel(options.DataRoot, stagingRoot(options.DataRoot, artifact))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("artifact staging escapes data directory")
	}
	local := "/storage/" + filepath.ToSlash(relative)
	command := []string{"podman", "run", "--rm", "--name", containerName, "--userns", "keep-id", "--umask", podman.CurrentUmask(), "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges", "--pids-limit", "512", "--tmpfs", "/tmp:rw,nosuid,nodev,size=2g", "--volume", options.DataRoot + ":/storage" + client.SharedSELinuxVolumeSuffix(options.Context), "--env", "HOME=/storage/staging/.home", "--env", "XDG_CACHE_HOME=/storage/staging/.cache", "--env", "HF_HOME=/storage/staging/.cache/huggingface", "--env", "HF_HUB_DISABLE_TELEMETRY=1", "--env", "HF_HUB_DISABLE_PROGRESS_BARS=1", "--env", "HF_HUB_DISABLE_UPDATE_CHECK=1"}
	if options.Environment["HF_TOKEN"] != "" {
		command = append(command, "--env", "HF_TOKEN")
	}
	if options.Environment["CIVITAI_TOKEN"] != "" {
		command = append(command, "--env", "CIVITAI_TOKEN")
	}
	command = append(command, podman.ManagedArguments("", "download")...)
	if artifact.Source.Provider == "civitai" {
		url := artifact.Source.DownloadURL
		if url == "" {
			url = fmt.Sprintf("https://civitai.com/api/download/models/%d", artifact.Source.ModelVersionID)
		}
		command = append(command, "--entrypoint", "/opt/venv/bin/python", options.Image, "/opt/paracetamol/container_download.py", "--url", url, "--output", local+"/"+artifact.Source.Path)
		if artifact.Source.ArchiveMember != "" {
			command = append(command, "--maximum-size", fmt.Sprint(artifact.Source.ArchiveMaxSize))
		} else {
			command = append(command, "--expected-size", fmt.Sprint(artifact.Size))
		}
		command = append(command, "--token-env", "CIVITAI_TOKEN")
		return command, nil
	}
	return append(command, "--entrypoint", "/opt/venv/bin/hf", options.Image, "download", artifact.Source.Repository, artifact.Source.Path, "--revision", artifact.Source.Revision, "--local-dir", local, "--max-workers", "4"), nil
}

func artifactRoot(dataRoot string, artifact catalog.Artifact) string {
	layout := storage.Layout{Root: dataRoot}
	switch artifact.Target {
	case "models":
		return layout.ComfyModels()
	case "llama-models":
		return layout.LlamaModels()
	case "dwarfstar-models":
		return layout.DwarfStarModels()
	case "workflows":
		return filepath.Join(layout.Application("comfyui"), "user", "default", "workflows", "imported")
	default:
		return ""
	}
}

func stagingPartition(dataRoot string, artifact catalog.Artifact) string {
	application := "comfyui"
	if artifact.Target == "llama-models" {
		application = "llama-cpp"
	} else if artifact.Target == "dwarfstar-models" {
		application = "dwarfstar"
	}
	return filepath.Join((storage.Layout{Root: dataRoot}).Staging(), application)
}

func stagingRoot(dataRoot string, artifact catalog.Artifact) string {
	root := stagingPartition(dataRoot, artifact)
	if artifact.Source.ArchiveMember == "" {
		return filepath.Join(root, artifact.ID)
	}
	identity := strings.Join([]string{artifact.Source.Provider, artifact.Source.Repository, artifact.Source.Revision, artifact.Source.Path, artifact.Source.DownloadURL}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return filepath.Join(root, ".archives", fmt.Sprintf("%x", digest))
}

func payloadPath(dataRoot string, artifact catalog.Artifact) string {
	root := stagingRoot(dataRoot, artifact)
	if artifact.Source.ArchiveMember != "" {
		return filepath.Join(root, ".extracted", artifact.ID, filepath.Base(artifact.Destination))
	}
	return filepath.Join(root, filepath.FromSlash(artifact.Source.Path))
}

func downloadPath(dataRoot string, artifact catalog.Artifact) string {
	return filepath.Join(stagingRoot(dataRoot, artifact), filepath.FromSlash(artifact.Source.Path))
}

func downloadSize(artifact catalog.Artifact) int64 {
	if artifact.Source.ArchiveMaxSize > 0 {
		return artifact.Source.ArchiveMaxSize
	}
	return artifact.Size
}

func downloadKey(artifact catalog.Artifact) string {
	if artifact.Source.ArchiveMember != "" {
		return strings.Join([]string{artifact.Source.Provider, artifact.Source.Repository, artifact.Source.Revision, artifact.Source.Path}, "\x00")
	}
	return artifact.ID
}

func stagedPayloadReady(dataRoot string, artifact catalog.Artifact) bool {
	info, err := os.Lstat(payloadPath(dataRoot, artifact))
	return err == nil && info.Mode().IsRegular() && info.Size() == artifact.Size
}

func stagedDownloadReady(dataRoot string, artifact catalog.Artifact) bool {
	info, err := os.Lstat(downloadPath(dataRoot, artifact))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if artifact.Source.ArchiveMember != "" {
		return info.Size() > 0 && info.Size() <= artifact.Source.ArchiveMaxSize
	}
	return info.Size() == artifact.Size
}

func extractArchive(artifact catalog.Artifact, archive, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("inspect archive %s: %w", archive, err)
	}
	defer reader.Close()
	var matches []*zip.File
	for _, file := range reader.File {
		if file.Name == artifact.Source.ArchiveMember {
			matches = append(matches, file)
		}
	}
	if len(matches) != 1 || matches[0].FileInfo().IsDir() || !matches[0].Mode().IsRegular() {
		return controlerr.New("source archive layout changed: expected one regular member %q", artifact.Source.ArchiveMember)
	}
	if int64(matches[0].UncompressedSize64) != artifact.Size {
		return controlerr.New("source archive member changed size: %q", artifact.Source.ArchiveMember)
	}
	input, err := matches[0].Open()
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary := destination + ".partial"
	output, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		if removeErr := os.Remove(temporary); removeErr != nil {
			return removeErr
		}
		output, err = os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, artifact.Size+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || written != artifact.Size {
		_ = os.Remove(temporary)
		return controlerr.New("source archive member changed while extracting %q", artifact.Source.ArchiveMember)
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

type localMirror struct {
	root     string
	move     bool
	byName   map[string][]string
	digests  map[string]string
	reserved map[string]bool
}

func openMirror(root, dataRoot string, move bool) (localMirror, error) {
	if root == "" {
		return localMirror{}, nil
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return localMirror{}, controlerr.New("cannot access local mirror %s: %v", root, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return localMirror{}, err
	}
	data, _ := filepath.Abs(dataRoot)
	if withinPath(data, resolved) || withinPath(resolved, data) {
		return localMirror{}, controlerr.New("local mirror and active data directory must not overlap")
	}
	index := make(map[string][]string)
	err = filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			index[entry.Name()] = append(index[entry.Name()], path)
		}
		return nil
	})
	if err != nil {
		return localMirror{}, err
	}
	for name := range index {
		sort.Strings(index[name])
	}
	return localMirror{root: resolved, move: move, byName: index, digests: make(map[string]string), reserved: make(map[string]bool)}, nil
}

func (mirror *localMirror) find(ctx context.Context, artifact catalog.Artifact, output io.Writer, environment map[string]string) (string, error) {
	if mirror.root == "" {
		return "", nil
	}
	names := []string{artifact.SHA256, filepath.Base(artifact.Destination), filepath.Base(artifact.Source.Path)}
	checked := make(map[string]bool)
	for _, name := range names {
		for _, candidate := range mirror.byName[name] {
			if checked[candidate] || mirror.reserved[candidate] {
				continue
			}
			checked[candidate] = true
			info, err := os.Lstat(candidate)
			if err != nil || !info.Mode().IsRegular() || info.Size() != artifact.Size {
				continue
			}
			fingerprint := fileIdentity(info)
			if fingerprint == "" {
				fingerprint = candidate
			}
			digest, ok := mirror.digests[fingerprint]
			if !ok {
				terminal := ui.New(output, environment)
				fmt.Fprintf(output, "\n%s local mirror candidate %s (%s)\n", terminal.Heading("Checking"), candidate, humanBytes(artifact.Size))
				progress := ui.NewProgress(output, environment, artifact.Size)
				progress.Update(filepath.Base(candidate), 1, 1, 0, false)
				digest, err = fileSHA256Context(ctx, candidate, func(hashed int64) {
					progress.Update(filepath.Base(candidate), 1, 1, hashed, false)
				})
				if err == nil {
					progress.Update(filepath.Base(candidate), 1, 1, artifact.Size, true)
					mirror.digests[fingerprint] = digest
				} else {
					progress.Finish()
					if errors.Is(err, context.Canceled) {
						return "", err
					}
					continue
				}
			}
			if digest == artifact.SHA256 {
				if mirror.move {
					mirror.reserved[candidate] = true
				}
				return candidate, nil
			}
		}
	}
	return "", nil
}

func (mirror localMirror) materialize(source, destination string) error {
	if mirror.move {
		if err := os.Rename(source, destination); err == nil {
			return nil
		}
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if mirror.move {
		return os.Remove(source)
	}
	return nil
}

func withinPath(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func acquireInstallLock(dataRoot string, environment map[string]string) (*os.File, error) {
	runtimeRoot := environment["XDG_RUNTIME_DIR"]
	if runtimeRoot == "" {
		runtimeRoot = filepath.Join(os.TempDir(), fmt.Sprintf("%s-runtime-%d", identity.StateNamespace, os.Geteuid()))
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		return nil, err
	}
	locks := filepath.Join(runtimeRoot, identity.StateNamespace+"-locks")
	if err := os.MkdirAll(locks, 0o700); err != nil {
		return nil, err
	}
	for _, directory := range []string{runtimeRoot, locks} {
		info, err := os.Lstat(directory)
		if err != nil {
			return nil, controlerr.New("cannot inspect content lock directory %s: %v", directory, err)
		}
		status, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ok || int(status.Uid) != os.Geteuid() || info.Mode().Perm()&0o077 != 0 {
			return nil, controlerr.New("content lock directory is not private and owned: %s", directory)
		}
	}
	resolved, err := filepath.EvalSymlinks(dataRoot)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(resolved))
	file, err := os.OpenFile(filepath.Join(locks, fmt.Sprintf("content-%x.lock", digest)), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || int(status.Uid) != os.Geteuid() || info.Mode().Perm()&0o077 != 0 {
		file.Close()
		return nil, controlerr.New("content lock is not a private owned file")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, controlerr.New("another content installation is active for %s", dataRoot)
	}
	return file, nil
}

func releaseInstallLock(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func fileIdentity(info os.FileInfo) string {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d", status.Dev, status.Ino, info.Size(), status.Mtim.Sec*1e9+status.Mtim.Nsec, status.Ctim.Sec*1e9+status.Ctim.Nsec)
}

func fileObject(info os.FileInfo) string {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d:%d", status.Dev, status.Ino, info.Size(), status.Mtim.Sec*1e9+status.Mtim.Nsec)
}

func humanBytes(value int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	number := float64(value)
	unit := 0
	for number >= 1024 && unit < len(units)-1 {
		number /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", number, units[unit])
}
