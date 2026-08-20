package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/process"
)

type commandRunner struct {
	commands []process.Command
	run      func(process.Command) process.Result
}

type recordingExecutor struct {
	path, arguments, environment string
}

func (executor *recordingExecutor) Exec(path string, arguments, environment []string) error {
	executor.path = path
	executor.arguments = strings.Join(arguments, "|")
	executor.environment = strings.Join(environment, "|")
	return nil
}

func (runner *commandRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	runner.commands = append(runner.commands, command)
	if runner.run != nil {
		return runner.run(command), nil
	}
	return process.Result{}, nil
}

func (*commandRunner) LookPath(name string) (string, error) {
	if name == "podman" {
		return "/usr/bin/podman", nil
	}
	return "", errors.New("missing")
}

func testApp(t *testing.T, runner process.Runner) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	return &App{Context: context.Background(), Root: filepath.Join("/src", "paracetamol"), Environment: map[string]string{"HOME": t.TempDir()}, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Runner: runner}, &stdout, &stderr
}

func TestBuildLlamaUsesOnlyItsDependencyClosure(t *testing.T) {
	runner := &commandRunner{run: func(command process.Command) process.Result {
		if len(command.Args) > 0 && command.Args[0] == "info" {
			return process.Result{Stdout: []byte("true\n")}
		}
		return process.Result{}
	}}
	app, stdout, _ := testApp(t, runner)
	if err := app.commandBuild([]string{"llama-cpp", "--no-cache"}); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, command := range runner.commands {
		joined += strings.Join(command.Args, " ") + "\n"
	}
	if !strings.Contains(joined, "--target rocm-runtime") || !strings.Contains(joined, "--target llama-cpp") {
		t.Fatalf("missing closure:\n%s", joined)
	}
	for _, unwanted := range []string{"--target content-tools", "--target rocm-base", "--target comfyui", "--target dwarfstar"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("unexpected target %q:\n%s", unwanted, joined)
		}
	}
	if !strings.Contains(stdout.String(), "Build summary") {
		t.Fatalf("missing summary: %s", stdout.String())
	}
}

func TestBuildAllContinuesAfterIndependentFailure(t *testing.T) {
	runner := &commandRunner{run: func(command process.Command) process.Result {
		if len(command.Args) > 0 && command.Args[0] == "info" {
			return process.Result{Stdout: []byte("true\n")}
		}
		if strings.Contains(strings.Join(command.Args, " "), "--target content-tools") {
			return process.Result{Status: 7}
		}
		return process.Result{}
	}}
	app, stdout, _ := testApp(t, runner)
	if err := app.commandBuild([]string{"all", "--no-cache"}); err == nil {
		t.Fatal("failed independent build returned success")
	}
	output := stdout.String()
	if !strings.Contains(output, "failed   content-tools") || !strings.Contains(output, "built    llama-cpp") || !strings.Contains(output, "built    comfyui") {
		t.Fatalf("incomplete partial-success summary:\n%s", output)
	}
}

func TestBuildRejectsUnknownTargetBeforeHostMutation(t *testing.T) {
	runner := &commandRunner{}
	app, _, _ := testApp(t, runner)
	if err := app.commandBuild([]string{"unknown"}); err == nil {
		t.Fatal("unknown target succeeded")
	}
	if len(runner.commands) != 0 {
		t.Fatalf("commands ran before validation: %#v", runner.commands)
	}
}

func TestCLIModesDoNotDefineServerFlags(t *testing.T) {
	app, _, _ := testApp(t, &commandRunner{})
	if err := app.runLlama("cli", []string{"--router"}); err == nil {
		t.Fatal("llama.cpp CLI accepted --router")
	}
	if err := app.runDwarfStar("cli", []string{"--listen", "0.0.0.0"}); err == nil {
		t.Fatal("DwarfStar CLI accepted --listen")
	}
}

func TestAgentExecBoundaryReceivesSortedEnvironment(t *testing.T) {
	executor := &recordingExecutor{}
	app, _, _ := testApp(t, &commandRunner{})
	app.Executor = executor
	if err := app.execProcess([]string{"/bin/tool", "argument"}, map[string]string{"Z": "last", "A": "first"}); err != nil {
		t.Fatal(err)
	}
	if executor.path != "/bin/tool" || executor.arguments != "/bin/tool|argument" || executor.environment != "A=first|Z=last" {
		t.Fatalf("unexpected exec request: %#v", executor)
	}
}

func TestProjectSourceIdentityHashesUntrackedContents(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.txt")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &commandRunner{run: func(command process.Command) process.Result {
		joined := strings.Join(command.Args, " ")
		switch {
		case strings.Contains(joined, "rev-parse"):
			return process.Result{Stdout: []byte(strings.Repeat("a", 40) + "\n")}
		case strings.Contains(joined, "ls-files"):
			return process.Result{Stdout: []byte("new.txt\x00")}
		default:
			return process.Result{}
		}
	}}
	app, _, _ := testApp(t, runner)
	app.Root = root
	first, err := app.projectSourceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := app.projectSourceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(first, "+dirty.") {
		t.Fatalf("identities did not bind content: %q %q", first, second)
	}
}
