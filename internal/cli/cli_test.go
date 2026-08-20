package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/identity"
)

func TestHelpListsCoreCommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	status := Main(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	for _, command := range []string{"build", "content", "run", "agent", "benchmark"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help does not contain %q:\n%s", command, stdout.String())
		}
	}
}

func TestLeafHelpUsesProductCommandIdentity(t *testing.T) {
	original := identity.CommandName
	t.Cleanup(func() { identity.CommandName = original })
	identity.CommandName = "renamed-local"
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), []string{"acceptance", "--help"}, strings.NewReader(""), &stdout, &stderr); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage: ./renamed-local acceptance [OPTIONS]") {
		t.Fatalf("unexpected renamed help: %q", stderr.String())
	}
}

func TestContentInstallHelpIncludesOptionsAndExamples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{"content", "install", "llama-cpp", "all", "--help"}, strings.NewReader(""), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	for _, expected := range []string{"Options:", "--local-mirror VALUE", "--local-mirror-move", "--accept-license", "Examples:", "content install llama-cpp all"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("content install help lacks %q:\n%s", expected, stderr.String())
		}
	}
}

type blockingPromptReader struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type terminalPromptReader struct{ io.Reader }

func (terminalPromptReader) IsTerminal() bool { return true }

func (reader *blockingPromptReader) Read([]byte) (int, error) {
	reader.once.Do(func() { close(reader.entered) })
	<-reader.release
	return 0, io.EOF
}

func TestPromptReturnsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &blockingPromptReader{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(reader.release)
	var output bytes.Buffer
	app := App{Context: ctx, Environment: map[string]string{}, Stdin: reader, Stdout: &output}
	result := make(chan error, 1)
	go func() {
		_, err := app.promptLine("Selection: ", true)
		result <- err
	}()
	<-reader.entered
	cancel()
	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt remained blocked after cancellation")
	}
	if output.String() != "\nSelection: " {
		t.Fatalf("prompt = %q", output.String())
	}
}

func TestContentApprovalsRemainSeparateInteractiveQuestions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := App{
		Context: context.Background(), Environment: map[string]string{},
		Stdin: terminalPromptReader{Reader: strings.NewReader("yes\nyes\n")}, Stdout: &stdout, Stderr: &stderr,
	}
	acknowledged, err := app.confirmContentApprovals(
		[]catalog.Agreement{{Name: "Example terms"}},
		[]catalog.Artifact{{ID: "unknown-license"}},
		false, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !acknowledged {
		t.Fatal("unverified license was not acknowledged")
	}
	for _, prompt := range []string{"Confirm that you accept these terms", "Acknowledge the unresolved licensing"} {
		if !strings.Contains(stdout.String(), "\n"+prompt) {
			t.Fatalf("missing separated prompt %q:\n%s", prompt, stdout.String())
		}
	}
}

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	status := Main(context.Background(), []string{"--version"}, strings.NewReader(""), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Paracetamol") {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
}

func TestFlattenedAcceptanceHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), []string{"acceptance", "--help"}, strings.NewReader(""), &stdout, &stderr); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage: ./paracetamol acceptance [OPTIONS]") {
		t.Fatalf("unexpected acceptance help: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if status := Main(context.Background(), []string{"acceptance", "run", "--help"}, strings.NewReader(""), &stdout, &stderr); status == 0 {
		t.Fatalf("legacy acceptance run unexpectedly succeeded")
	}
}

func TestBenchmarkFamiliesExposeModes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), []string{"benchmark", "comfyui", "--help"}, strings.NewReader(""), &stdout, &stderr); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "comfyui COMMAND") || !strings.Contains(stdout.String(), "suite") {
		t.Fatalf("unexpected benchmark help: %s", stdout.String())
	}
}

func TestCommandGroupHelpIsSuccessful(t *testing.T) {
	for _, command := range []string{"run", "content", "agent", "images", "cleanup", "benchmark"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := Main(context.Background(), []string{command, "--help"}, strings.NewReader(""), &stdout, &stderr)
			if status != 0 {
				t.Fatalf("status = %d, stderr = %q", status, stderr.String())
			}
			if !strings.Contains(stdout.String(), "Usage: ./paracetamol "+command) {
				t.Fatalf("unexpected group help: %q", stdout.String())
			}
		})
	}
}
