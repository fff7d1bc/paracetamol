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
	for _, command := range []string{"build", "content", "run", "benchmark"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help does not contain %q:\n%s", command, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "agent") {
		t.Fatalf("retired client command is still advertised: %s", stdout.String())
	}
}

func TestRetiredClientCommandsCannotLaunchOrInstall(t *testing.T) {
	for _, arguments := range [][]string{
		{"agent", "install", "pi"}, {"agent", "run", "pi"},
		{"agent", "run", "maki"}, {"benchmark", "agent", "--list-tasks"},
	} {
		runner := &commandRunner{}
		app, _, _ := testApp(t, runner)
		if err := app.Dispatch(arguments); err == nil {
			t.Fatalf("retired command accepted: %v", arguments)
		}
		if len(runner.commands) != 0 {
			t.Fatalf("retired command executed a subprocess: %v", runner.commands)
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
	if !strings.Contains(stdout.String(), "Usage: ./renamed-local acceptance [OPTIONS]") || stderr.Len() != 0 {
		t.Fatalf("unexpected renamed help: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestContentInstallHelpIncludesOptionsAndExamples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{"content", "install", "llama-cpp", "all", "--help"}, strings.NewReader(""), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	for _, expected := range []string{"Options:", "--local-mirror PATH", "--local-mirror-move", "--accept-license", "Examples:", "content install llama-cpp all"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("content install help lacks %q:\n%s", expected, stdout.String())
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
	if !strings.Contains(stdout.String(), "Usage: ./paracetamol acceptance [OPTIONS]") || stderr.Len() != 0 {
		t.Fatalf("unexpected acceptance help: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if status := Main(context.Background(), []string{"acceptance", "run", "--help"}, strings.NewReader(""), &stdout, &stderr); status == 0 {
		t.Fatalf("legacy acceptance run unexpectedly succeeded")
	}
}

func TestLeafHelpUsesStdoutAndMeaningfulMetavariables(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{"run", "llama-cpp", "server", "--help"}, strings.NewReader(""), &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	for _, expected := range []string{"--model MODEL", "--listen ADDRESS", "--port PORT", "Examples:"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("help lacks %q:\n%s", expected, stdout.String())
		}
	}
}

func TestFlagErrorIsPrintedOnceWithUsageStatus(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{"content", "install", "llama-cpp", "all", "--local-mirror"}, strings.NewReader(""), &stdout, &stderr)
	if status != 2 {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	if strings.Count(stderr.String(), "flag needs an argument") != 1 || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("unexpected parse error:\n%s", stderr.String())
	}
}

func TestNestedCommandHelpIsAvailable(t *testing.T) {
	for _, arguments := range [][]string{{"run", "llama-cpp", "--help"}, {"benchmark", "llama-cpp", "--help"}} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if status := Main(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr); status != 0 {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "Commands:") {
				t.Fatalf("missing nested help: %q", stdout.String())
			}
		})
	}
}

func TestRunRejectsRemovedMixedMathOption(t *testing.T) {
	for _, command := range [][]string{
		{"run", "gateway"},
		{"run", "llama-cpp", "server"},
		{"run", "llama-cpp", "cli"},
	} {
		for _, value := range []string{"default", "mixed"} {
			arguments := append(append([]string{}, command...), "--no-config", "--qwen4exp-math", value)
			var stdout, stderr bytes.Buffer
			status := Main(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr)
			if status != 2 || !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("%v status=%d stdout=%q stderr=%q", arguments, status, stdout.String(), stderr.String())
			}
		}
	}
}

func TestCleanupRejectsFlagsOutsideSelectedScope(t *testing.T) {
	for _, arguments := range [][]string{
		{"cleanup", "containers", "--image-tag", "example"},
		{"cleanup", "containers", "--application", "llama-cpp"},
		{"cleanup", "downloads", "--application", "llama-cpp"},
		{"cleanup", "images", "--data-dir", "/tmp/example"},
		{"cleanup", "build-cache", "--application", "llama-cpp"},
	} {
		var stdout, stderr bytes.Buffer
		if status := Main(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr); status != 2 {
			t.Fatalf("%v status=%d stdout=%q stderr=%q", arguments, status, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("%v unexpectedly accepted: %q", arguments, stderr.String())
		}
	}
}

func TestCleanupApplicationScopeIsPositionalAndOtherScopesRejectPositionals(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{"cleanup", "downloads", "llama-cpp"}, strings.NewReader(""), &stdout, &stderr)
	if status != 2 || !strings.Contains(stderr.String(), "accepts no positional arguments") {
		t.Fatalf("downloads positional status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	status = Main(context.Background(), []string{"cleanup", "containers", "llama-cpp", "--help"}, strings.NewReader(""), &stdout, &stderr)
	if status != 0 || !strings.Contains(stdout.String(), "[APPLICATION]") || strings.Contains(stdout.String(), "--application") {
		t.Fatalf("container scope help status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}

func TestPromptEOFIsReportedAsCancellation(t *testing.T) {
	var output bytes.Buffer
	app := App{Context: context.Background(), Environment: map[string]string{}, Stdin: strings.NewReader(""), Stdout: &output}
	if _, err := app.promptLine("Selection: ", true); err == nil || err.Error() != "input closed; cancelled" {
		t.Fatalf("error=%v", err)
	}
}

func TestTerminalEOFEndsThePromptLine(t *testing.T) {
	var output bytes.Buffer
	app := App{Context: context.Background(), Environment: map[string]string{}, Stdin: terminalPromptReader{Reader: strings.NewReader("")}, Stdout: &output}
	if _, err := app.promptLine("Selection: ", true); err == nil {
		t.Fatal("terminal EOF succeeded")
	}
	if output.String() != "\nSelection: \n" {
		t.Fatalf("prompt=%q", output.String())
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
	for _, command := range []string{"run", "content", "images", "cleanup", "benchmark"} {
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

func TestIncompleteLeafCommandsShowRecoveryHelp(t *testing.T) {
	for _, arguments := range [][]string{
		{"build"}, {"shell"}, {"logs"}, {"stop"},
		{"images", "export"}, {"images", "import"},
		{"content", "install", "--non-interactive"},
		{"content", "import", "--non-interactive"},
		{"content", "workflows", "install"},
		{"benchmark", "comfyui", "run"},
		{"benchmark", "llama-cpp", "throughput"},
		{"benchmark", "llama-cpp", "speculative"},
		{"benchmark", "report"},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := Main(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr)
			if status != 2 || !strings.HasPrefix(stdout.String(), "Usage: ") || !strings.Contains(stderr.String(), "error:") {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		})
	}
}

func TestUnknownCommandsShowNearestGroupHelp(t *testing.T) {
	for _, arguments := range [][]string{
		{"unknown"}, {"run", "unknown"}, {"content", "unknown"},
		{"content", "workflows", "unknown"}, {"images", "unknown"},
		{"agent", "run", "pi"}, {"agent", "run", "maki"}, {"benchmark", "agent"},
		{"benchmark", "unknown"}, {"benchmark", "comfyui", "unknown"},
		{"benchmark", "llama-cpp", "unknown"}, {"cleanup", "unknown"},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := Main(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr)
			if status != 2 || !strings.HasPrefix(stdout.String(), "Usage: ") || !strings.Contains(stderr.String(), "error:") {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExplicitZeroCivitaiVersionIsRejectedBeforeNetworkAccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{"content", "import", "https://civitai.com/models/1", "--version", "0", "--non-interactive"}, strings.NewReader(""), &stdout, &stderr)
	if status != 2 || !strings.Contains(stderr.String(), "--version must be positive") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}

func TestRetiredApplicationCommandsAreRejected(t *testing.T) {
	for _, arguments := range [][]string{
		{"run", "dwarfstar", "server", "--dry-run"},
		{"run", "dwarfstar", "cli", "--help"},
		{"guide", "dwarfstar"},
		{"build", "dwarfstar"},
		{"acceptance", "--application", "dwarfstar", "--dry-run"},
	} {
		var stdout, stderr bytes.Buffer
		if status := Main(context.Background(), append([]string{"--no-config"}, arguments...), strings.NewReader(""), &stdout, &stderr); status != 2 {
			t.Fatalf("%v status=%d stdout=%q stderr=%q", arguments, status, stdout.String(), stderr.String())
		}
	}
}
