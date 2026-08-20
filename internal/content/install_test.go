package content

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

type relabelRunner struct{}

func (relabelRunner) LookPath(name string) (string, error) { return "/usr/bin/" + name, nil }
func (relabelRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	switch filepath.Base(command.Name) {
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
func (noSELinuxRunner) Run(context.Context, process.Command) (process.Result, error) {
	return process.Result{}, fmt.Errorf("unexpected process")
}

type corruptingRelabelRunner struct{}

func (corruptingRelabelRunner) LookPath(name string) (string, error) {
	return "/usr/bin/" + name, nil
}
func (corruptingRelabelRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	switch filepath.Base(command.Name) {
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
	if err := Install(InstallOptions{Context: context.Background(), DataRoot: dataRoot, Artifacts: []catalog.Artifact{artifact}, LocalMirror: mirror, Environment: map[string]string{"XDG_RUNTIME_DIR": runtimeRoot}}); err != nil {
		t.Fatal(err)
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
	if err := Install(InstallOptions{Context: context.Background(), DataRoot: dataRoot, Artifacts: []catalog.Artifact{first, second}, Environment: map[string]string{"XDG_RUNTIME_DIR": runtimeRoot}}); err != nil {
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
