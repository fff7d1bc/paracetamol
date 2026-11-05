package content

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

type relabelRunner struct{}

func (relabelRunner) LookPath(name string) (string, error) { return "/usr/bin/" + name, nil }
func (relabelRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	switch filepath.Base(command.Name) {
	case "podman":
		if len(command.Args) > 0 && command.Args[0] == "ps" {
			return process.Result{}, nil
		}
		return process.Result{}, fmt.Errorf("unexpected podman command %v", command.Args)
	case "getenforce":
		return process.Result{Stdout: []byte("Enforcing\n")}, nil
	case "chcon":
		path := command.Args[len(command.Args)-1]
		if err := os.Chmod(path, 0o600); err != nil { // changes ctime like a real relabel
			return process.Result{}, err
		}
		return process.Result{}, nil
	default:
		return process.Result{}, fmt.Errorf("unexpected command %s", command.Name)
	}
}

type noSELinuxRunner struct{}

func (noSELinuxRunner) LookPath(string) (string, error) { return "", fmt.Errorf("missing") }
func (noSELinuxRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	if command.Name == "podman" && len(command.Args) > 0 && command.Args[0] == "ps" {
		return process.Result{}, nil
	}
	return process.Result{}, fmt.Errorf("unexpected process")
}

type corruptingRelabelRunner struct{}

func (corruptingRelabelRunner) LookPath(name string) (string, error) {
	return "/usr/bin/" + name, nil
}
func (corruptingRelabelRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	switch filepath.Base(command.Name) {
	case "podman":
		if len(command.Args) > 0 && command.Args[0] == "ps" {
			return process.Result{}, nil
		}
		return process.Result{}, fmt.Errorf("unexpected podman command %v", command.Args)
	case "getenforce":
		return process.Result{Stdout: []byte("Enforcing\n")}, nil
	case "chcon":
		path := command.Args[len(command.Args)-1]
		return process.Result{}, os.WriteFile(path, []byte("broken"), 0o600)
	default:
		return process.Result{}, fmt.Errorf("unexpected command %s", command.Name)
	}
}

func TestDownloadCommandIsNamedConfinedAndDoesNotInlineTokens(t *testing.T) {
	artifact := testArtifact([]byte("fixture"))
	artifact.Source.Provider = "huggingface"
	artifact.Source.Repository = "owner/model"
	artifact.Source.Revision = strings.Repeat("a", 40)
	options := InstallOptions{Context: context.Background(), DataRoot: "/data", Image: "content-image", Environment: map[string]string{"HF_TOKEN": "secret-value"}}
	command, err := downloadCommand(options, podman.Client{Runner: noSELinuxRunner{}}, artifact, "paracetamol-download-test")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	for _, expected := range []string{"--name paracetamol-download-test", "--read-only", "--cap-drop all", "no-new-privileges", "--label " + podman.ManagedRoleLabel + "=download", "--env HF_TOKEN"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("download command lacks %q: %s", expected, joined)
		}
	}
	if strings.Contains(joined, "secret-value") {
		t.Fatalf("download command inlined token: %s", joined)
	}
}

func testArtifact(contents []byte) catalog.Artifact {
	digest := sha256.Sum256(contents)
	return catalog.Artifact{ID: "fixture", Destination: "fixture/model.gguf", Target: "llama-models", Size: int64(len(contents)), SHA256: fmt.Sprintf("%x", digest), Source: catalog.Source{Path: "model.gguf"}, License: catalog.License{Status: "verified"}}
}

type interlockRunner struct {
	noSELinuxRunner
	labels map[string]string
	err    error
	calls  []string
}

func (runner *interlockRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	runner.calls = append(runner.calls, strings.Join(command.Args, " "))
	if command.Name != "podman" {
		return process.Result{}, fmt.Errorf("unexpected process %s", command.Name)
	}
	switch command.Args[0] {
	case "ps":
		if !strings.Contains(strings.Join(command.Args, " "), "label="+podman.ManagedContainerLabel+"=true") {
			return process.Result{}, fmt.Errorf("missing ownership filter")
		}
		return process.Result{Stdout: []byte("paracetamol-download-stale\n")}, runner.err
	case "inspect":
		raw, err := json.Marshal(runner.labels)
		return process.Result{Stdout: raw}, err
	default:
		return process.Result{}, fmt.Errorf("unexpected podman mutation %v", command.Args)
	}
}

func TestInstallInterlocksBeforeEveryStagingPath(t *testing.T) {
	for _, reuse := range []string{"network", "mirror", "staged", "archive"} {
		t.Run(reuse, func(t *testing.T) {
			root := t.TempDir()
			contents := []byte("exact fixture")
			artifact := testArtifact(contents)
			if reuse == "archive" {
				artifact.Source.ArchiveMember = "model.gguf"
				artifact.Source.ArchiveMaxSize = 1024
			}
			payload := payloadPath(root, artifact)
			if err := os.MkdirAll(filepath.Dir(payload), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(payload, contents, 0o644); err != nil {
				t.Fatal(err)
			}
			if reuse == "network" || reuse == "mirror" {
				if err := os.Remove(payload); err != nil {
					t.Fatal(err)
				}
			}
			runner := &interlockRunner{labels: map[string]string{podman.ManagedContainerLabel: "true", podman.ManagedRoleLabel: "download"}}
			options := InstallOptions{Runner: runner, DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")}}
			if reuse == "mirror" {
				// The interlock must run before even indexing the mirror.
				options.LocalMirror = filepath.Join(t.TempDir(), "absent-mirror")
			}
			err := Install(options)
			if err == nil || !strings.Contains(err.Error(), "podman rm --force paracetamol-download-stale") {
				t.Fatalf("missing exact-container guidance: %v", err)
			}
			if len(runner.calls) != 2 {
				t.Fatalf("unexpected operations: %v", runner.calls)
			}
			if reuse == "staged" || reuse == "archive" {
				if got, err := os.ReadFile(payload); err != nil || !bytes.Equal(got, contents) {
					t.Fatalf("staging was changed: %q, %v", got, err)
				}
			}
			if _, err := os.Stat(ArtifactPath(root, artifact)); !os.IsNotExist(err) {
				t.Fatalf("content was installed: %v", err)
			}
		})
	}
}

func TestDownloaderInterlockChecksBothLabelsAndFailsClosed(t *testing.T) {
	for _, labels := range []map[string]string{
		{podman.ManagedContainerLabel: "true", podman.ManagedRoleLabel: "application"},
		{podman.ManagedContainerLabel: "false", podman.ManagedRoleLabel: "download"},
	} {
		runner := &interlockRunner{labels: labels}
		if err := requireNoDownloaders(context.Background(), podman.Client{Runner: runner}); err != nil {
			t.Fatalf("unrelated container blocked installation: %v", err)
		}
	}
	runner := &interlockRunner{err: fmt.Errorf("inspection failed")}
	if err := requireNoDownloaders(context.Background(), podman.Client{Runner: runner}); err == nil {
		t.Fatal("inspection failure was ignored")
	}
}

func TestInstallReusesVerifiedLocalMirror(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "data")
	mirror := filepath.Join(t.TempDir(), "mirror")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := []byte("small exact fixture")
	artifact := testArtifact(contents)
	if err := os.WriteFile(filepath.Join(mirror, "model.gguf"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Install(InstallOptions{Context: context.Background(), Runner: noSELinuxRunner{}, DataRoot: dataRoot, Artifacts: []catalog.Artifact{artifact}, LocalMirror: mirror, Environment: map[string]string{"XDG_RUNTIME_DIR": runtimeRoot}, Output: &output}); err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(output.String(), "Checking local mirror candidate"); count != 1 {
		t.Fatalf("mirror candidate was checked %d times:\n%s", count, output.String())
	}
	for _, expected := range []string{"Verifying", "100.0%", "Installed"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("install output lacks %q:\n%s", expected, output.String())
		}
	}
	status, err := os.ReadFile(ArtifactPath(dataRoot, artifact))
	if err != nil {
		t.Fatal(err)
	}
	if string(status) != string(contents) {
		t.Fatalf("installed %q", status)
	}
	plan, err := Plan(dataRoot, []catalog.Artifact{artifact})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ready != 1 {
		t.Fatalf("ready = %d", plan.Ready)
	}
}

func TestInstallRetriesInterruptedMirrorCopy(t *testing.T) {
	root, mirrorRoot := t.TempDir(), t.TempDir()
	contents := []byte("complete mirror model")
	artifact := testArtifact(contents)
	source := filepath.Join(mirrorRoot, artifact.Source.Path)
	if err := os.WriteFile(source, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	payload := payloadPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(payload), 0o755); err != nil {
		t.Fatal(err)
	}
	// Reproduce the old interrupted direct copy, plus a killed new-style copy.
	if err := os.WriteFile(payload, contents[:5], 0o644); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(filepath.Dir(payload), ".mirror-interrupted.partial")
	if err := os.WriteFile(scratch, contents[:3], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(InstallOptions{Runner: noSELinuxRunner{}, DataRoot: root, Artifacts: []catalog.Artifact{artifact}, LocalMirror: mirrorRoot, Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{source, ArtifactPath(root, artifact)} {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, contents) {
			t.Fatalf("wrong bytes at %s: %q, %v", path, got, err)
		}
	}
}

func TestMirrorCopyRefusesLinksAndUnexpectedDestinations(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "complete", "source-symlink", "source-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			mirrorRoot, staging := t.TempDir(), t.TempDir()
			contents := []byte("complete mirror model")
			source := filepath.Join(mirrorRoot, "model.gguf")
			destination := filepath.Join(staging, "model.gguf")
			victim := filepath.Join(t.TempDir(), "unrelated")
			if err := os.WriteFile(source, contents, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(victim, destination)
			case "hardlink":
				err = os.Link(victim, destination)
			case "directory":
				err = os.Mkdir(destination, 0o755)
			case "complete":
				err = os.WriteFile(destination, contents, 0o644)
			case "source-symlink", "source-hardlink":
				if err = os.Remove(source); err == nil {
					if kind == "source-symlink" {
						err = os.Symlink(victim, source)
					} else {
						err = os.Link(victim, source)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, move := range []bool{false, true} {
				if err := (localMirror{root: mirrorRoot, move: move}).materialize(source, destination); err == nil {
					t.Fatalf("accepted %s with move=%v", kind, move)
				}
			}
			if got, err := os.ReadFile(victim); err != nil || string(got) != "keep" {
				t.Fatalf("linked bytes changed: %q, %v", got, err)
			}
		})
	}
}

func TestMirrorCopyHonorsHostUmask(t *testing.T) {
	const childMask = "PARACETAMOL_TEST_MIRROR_COPY_UMASK"
	if text := os.Getenv(childMask); text != "" {
		mask, err := strconv.ParseUint(text, 8, 32)
		if err != nil {
			t.Fatal(err)
		}
		// Change umask only in a dedicated subprocess, never alongside other
		// tests or goroutines in the parent test binary.
		syscall.Umask(int(mask))
		root := t.TempDir()
		source, destination := filepath.Join(root, "source.gguf"), filepath.Join(root, "copy.gguf")
		if err := os.WriteFile(source, []byte("model"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := (localMirror{root: root}).materialize(source, destination); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := info.Mode().Perm(), os.FileMode(0o644)&^os.FileMode(mask); got != want {
			t.Fatalf("copy mode=%#o want=%#o", got, want)
		}
		return
	}
	for _, mask := range []string{"0022", "0027", "0077"} {
		t.Run(mask, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestMirrorCopyHonorsHostUmask$")
			command.Env = append(os.Environ(), childMask+"="+mask)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("umask=%s: %v\n%s", mask, err, output)
			}
		})
	}
}

func TestInstallMovesOldSourceNamesToPublisherNamedDestinations(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "data")
	mirror := filepath.Join(t.TempDir(), "mirror")
	artifacts := make([]catalog.Artifact, 0, 2)
	for _, variant := range []string{"base", "mtp"} {
		contents := []byte("different " + variant + " GGUF bytes")
		artifact := testArtifact(contents)
		artifact.ID = "unsloth-qwen-" + variant + "-gguf"
		artifact.Source.Path = "Qwen3.8-27B-Q8_0.gguf"
		artifact.Destination = "unsloth-qwen-" + variant + "/unsloth-Qwen3.8-27B-Q8_0.gguf"
		artifacts = append(artifacts, artifact)
		oldPath := filepath.Join(mirror, variant, artifact.Source.Path)
		if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(oldPath, contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Install(InstallOptions{
		Context: context.Background(), Runner: noSELinuxRunner{}, DataRoot: dataRoot, Artifacts: artifacts,
		LocalMirror: mirror, MoveFromMirror: true,
		Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")},
	}); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		variant := strings.TrimSuffix(strings.TrimPrefix(artifact.ID, "unsloth-qwen-"), "-gguf")
		if _, err := os.Lstat(filepath.Join(mirror, variant, artifact.Source.Path)); !os.IsNotExist(err) {
			t.Fatalf("mirror source for %s was not moved: %v", artifact.ID, err)
		}
		contents, err := os.ReadFile(ArtifactPath(dataRoot, artifact))
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(contents)) != artifact.SHA256 {
			t.Fatalf("installed wrong bytes for %s", artifact.ID)
		}
	}
	plan, err := Plan(dataRoot, artifacts)
	if err != nil || plan.Ready != len(artifacts) {
		t.Fatalf("migrated artifacts not ready: plan=%#v err=%v", plan, err)
	}
}

func TestDownloadMeasurementIgnoresStaleHFPartialsAndTracksActiveFile(t *testing.T) {
	root := t.TempDir()
	contents := bytes.Repeat([]byte("x"), 100)
	artifact := testArtifact(contents)
	artifact.Source.Provider = "huggingface"
	artifact.Source.Path = "weights/model.gguf"
	target := filepath.Join(root, artifact.Source.Path)
	cache := filepath.Join(root, ".cache", "huggingface", "download", "weights")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(cache, ".model."+artifact.SHA256+".fixture.incomplete")
	if err := os.WriteFile(partial, contents[:20], 0o644); err != nil {
		t.Fatal(err)
	}
	measurement := newDownloadMeasurement(root, target, artifact)
	if got := measurement.bytes(); got != 0 {
		t.Fatalf("stale partial counted as %d bytes", got)
	}
	if err := os.WriteFile(partial, contents[:40], 0o644); err != nil {
		t.Fatal(err)
	}
	if got := measurement.bytes(); got != 40 {
		t.Fatalf("active partial = %d bytes", got)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := measurement.bytes(); got != 100 {
		t.Fatalf("completed payload = %d bytes", got)
	}
}

func TestInstallAcceptsExpectedSELinuxRelabelMetadataChange(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "data")
	mirror := filepath.Join(t.TempDir(), "mirror")
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := []byte("small exact fixture")
	artifact := testArtifact(contents)
	if err := os.WriteFile(filepath.Join(mirror, "model.gguf"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	err := Install(InstallOptions{
		Context:     context.Background(),
		DataRoot:    dataRoot,
		Artifacts:   []catalog.Artifact{artifact},
		LocalMirror: mirror,
		Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")},
		Runner:      relabelRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallExistingContentRecordsPostRelabelIdentity(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "data")
	contents := []byte("small exact fixture")
	artifact := testArtifact(contents)
	destination := ArtifactPath(dataRoot, artifact)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(InstallOptions{
		Context:     context.Background(),
		DataRoot:    dataRoot,
		Artifacts:   []catalog.Artifact{artifact},
		Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")},
		Runner:      relabelRunner{},
	}); err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(dataRoot, []catalog.Artifact{artifact})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ready != 1 {
		t.Fatalf("post-relabel receipt is immediately stale: ready = %d", plan.Ready)
	}
}

func TestInstallHashesStagedContentAfterRelabel(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "data")
	mirror := filepath.Join(t.TempDir(), "mirror")
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := []byte("model!")
	artifact := testArtifact(contents)
	if err := os.WriteFile(filepath.Join(mirror, "model.gguf"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	err := Install(InstallOptions{
		Context:     context.Background(),
		DataRoot:    dataRoot,
		Artifacts:   []catalog.Artifact{artifact},
		LocalMirror: mirror,
		Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")},
		Runner:      corruptingRelabelRunner{},
	})
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("expected post-label hash mismatch, got %v", err)
	}
	if _, statErr := os.Stat(ArtifactPath(dataRoot, artifact)); !os.IsNotExist(statErr) {
		t.Fatalf("corrupted staged content was published: %v", statErr)
	}
}

func TestInstallRefusesUnexpectedDestination(t *testing.T) {
	dataRoot := t.TempDir()
	artifact := testArtifact([]byte("expected"))
	destination := ArtifactPath(dataRoot, artifact)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("different bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Install(InstallOptions{Context: context.Background(), DataRoot: dataRoot, Artifacts: []catalog.Artifact{artifact}, Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")}})
	if err == nil {
		t.Fatal("expected refusal")
	}
}

func TestInstallKeepsSharedArchiveUntilEverySelectedMemberIsInstalled(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "data")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	firstContents, secondContents := []byte("first model"), []byte("second model")
	makeArtifact := func(id, member, destination string, contents []byte) catalog.Artifact {
		digest := sha256.Sum256(contents)
		return catalog.Artifact{ID: id, Destination: destination, Target: "llama-models", Size: int64(len(contents)), SHA256: fmt.Sprintf("%x", digest), Source: catalog.Source{Provider: "civitai", Path: "models.zip", DownloadURL: "https://example.invalid/models.zip", ArchiveMember: member, ArchiveMaxSize: 1024 * 1024}, License: catalog.License{Status: "verified"}}
	}
	first := makeArtifact("first", "models/first.gguf", "fixture/first.gguf", firstContents)
	second := makeArtifact("second", "models/second.gguf", "fixture/second.gguf", secondContents)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, contents := range map[string][]byte{first.Source.ArchiveMember: firstContents, second.Source.ArchiveMember: secondContents} {
		member, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	download := downloadPath(dataRoot, first)
	if err := os.MkdirAll(filepath.Dir(download), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(download, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(InstallOptions{Context: context.Background(), Runner: noSELinuxRunner{}, DataRoot: dataRoot, Artifacts: []catalog.Artifact{first, second}, Environment: map[string]string{"XDG_RUNTIME_DIR": runtimeRoot}}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		artifact catalog.Artifact
		expected []byte
	}{{first, firstContents}, {second, secondContents}} {
		contents, err := os.ReadFile(ArtifactPath(dataRoot, item.artifact))
		if err != nil || !bytes.Equal(contents, item.expected) {
			t.Fatalf("artifact %s was not installed: %v", item.artifact.ID, err)
		}
	}
	if _, err := os.Stat(stagingRoot(dataRoot, first)); !os.IsNotExist(err) {
		t.Fatalf("completed archive staging remains: %v", err)
	}
}

type archiveRunner struct {
	noSELinuxRunner
	path      string
	contents  []byte
	status    int
	downloads int
}

func (runner *archiveRunner) LookPath(name string) (string, error) {
	if name == "podman" {
		return "/usr/bin/podman", nil
	}
	return runner.noSELinuxRunner.LookPath(name)
}

func (runner *archiveRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	if command.Name != "podman" {
		return process.Result{}, fmt.Errorf("unexpected process %s", command.Name)
	}
	switch command.Args[0] {
	case "ps", "image", "rm":
		return process.Result{}, nil
	case "info":
		return process.Result{Stdout: []byte("true\n")}, nil
	case "run":
		runner.downloads++
		return process.Result{Status: runner.status}, os.WriteFile(runner.path, runner.contents, 0o644)
	default:
		return process.Result{}, fmt.Errorf("unexpected podman operation %v", command.Args)
	}
}

func archiveFixture(t *testing.T, member string, contents []byte) (catalog.Artifact, []byte) {
	t.Helper()
	artifact := testArtifact(contents)
	artifact.Source = catalog.Source{Provider: "civitai", Path: "model.zip", ArchiveMember: "model.gguf", ArchiveMaxSize: 1024 * 1024}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	file, err := writer.Create(member)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return artifact, archive.Bytes()
}

func TestArchiveRetryRedownloadsInterruptedTransfer(t *testing.T) {
	root := t.TempDir()
	artifact, archive := archiveFixture(t, "model.gguf", []byte("fixture model"))
	path := downloadPath(root, artifact)
	runner := &archiveRunner{path: path, contents: archive[:len(archive)/2], status: 1}
	options := InstallOptions{Runner: runner, DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")}}
	if err := Install(options); err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("expected interrupted transfer: %v", err)
	}
	if stagedDownloadReady(root, artifact) {
		t.Fatal("nonempty interrupted ZIP counted as complete")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, runner.contents) {
		t.Fatalf("interrupted bytes were removed: %v", err)
	}
	runner.contents, runner.status = archive, 0
	if err := Install(options); err != nil {
		t.Fatal(err)
	}
	if runner.downloads != 2 {
		t.Fatalf("downloads = %d, expected retry", runner.downloads)
	}
}

func TestCompletedArchiveMismatchRemainsFailClosedOnRetry(t *testing.T) {
	for _, mismatch := range []string{"malformed", "layout", "size", "hash"} {
		t.Run(mismatch, func(t *testing.T) {
			root := t.TempDir()
			member := "model.gguf"
			if mismatch == "layout" {
				member = "other.gguf"
			}
			artifact, archive := archiveFixture(t, member, []byte("fixture model"))
			if mismatch == "malformed" {
				archive = []byte("a completely transferred non-ZIP response")
			} else if mismatch == "size" {
				artifact.Size++
			} else if mismatch == "hash" {
				artifact.SHA256 = strings.Repeat("0", 64)
			}
			path := downloadPath(root, artifact)
			runner := &archiveRunner{path: path, contents: archive}
			options := InstallOptions{Runner: runner, DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")}}
			for attempt := 0; attempt < 2; attempt++ {
				if err := Install(options); err == nil {
					t.Fatal("accepted changed archive")
				}
				if !stagedDownloadReady(root, artifact) {
					t.Fatal("completed rejected archive was marked for redownload")
				}
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, archive) {
					t.Fatalf("rejected archive changed: %v", err)
				}
			}
			if runner.downloads != 1 {
				t.Fatalf("redownloaded rejected archive: %d", runner.downloads)
			}
		})
	}
}

func TestArchiveCompletionIsBoundToExactFileIdentity(t *testing.T) {
	root := t.TempDir()
	artifact, archive := archiveFixture(t, "model.gguf", []byte("fixture model"))
	path := downloadPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recordArchiveCompletion(path, artifact.Source.ArchiveMaxSize); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("interrupted replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	if stagedDownloadReady(root, artifact) {
		t.Fatal("stale completion marker accepted replacement partial")
	}
}

func TestArchiveDownloadRefusesUnexpectedTransportPaths(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "parent-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			artifact, archive := archiveFixture(t, "model.gguf", []byte("fixture model"))
			if kind == "parent-symlink" {
				artifact.Source.Path = "nested/model.zip"
			}
			path := downloadPath(root, artifact)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			victimRoot := t.TempDir()
			victim := filepath.Join(victimRoot, "model.zip")
			if err := os.WriteFile(victim, []byte("keep unrelated bytes"), 0o644); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(victim, path)
			case "hardlink":
				err = os.Link(victim, path)
			case "directory":
				err = os.Mkdir(path, 0o755)
			case "parent-symlink":
				if err = os.Remove(filepath.Dir(path)); err == nil {
					err = os.Symlink(victimRoot, filepath.Dir(path))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			runner := &archiveRunner{path: path, contents: archive}
			err = Install(InstallOptions{Runner: runner, DataRoot: root, Artifacts: []catalog.Artifact{artifact}, Environment: map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "runtime")}})
			if err == nil {
				t.Fatal("accepted unexpected archive path")
			}
			if runner.downloads != 0 {
				t.Fatalf("launched %d downloaders against unsafe path", runner.downloads)
			}
			if got, err := os.ReadFile(victim); err != nil || string(got) != "keep unrelated bytes" {
				t.Fatalf("aliased bytes changed: %q, %v", got, err)
			}
		})
	}
}
