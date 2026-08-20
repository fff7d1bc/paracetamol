package podman

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"paracetamol/internal/process"
)

type fakeRunner struct {
	commands []process.Command
	result   process.Result
}

func (runner *fakeRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	runner.commands = append(runner.commands, command)
	return runner.result, nil
}
func (*fakeRunner) LookPath(string) (string, error) { return "/usr/bin/podman", nil }

func TestManagedArguments(t *testing.T) {
	want := []string{"--label", ManagedContainerLabel + "=true", "--label", ManagedRoleLabel + "=runtime", "--label", ManagedApplicationLabel + "=llama-cpp"}
	if got := ManagedArguments("llama-cpp", "runtime"); !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments = %v, want %v", got, want)
	}
}

func TestRequireRootless(t *testing.T) {
	runner := &fakeRunner{result: process.Result{Stdout: []byte("true\n")}}
	if err := (Client{Runner: runner}).RequireRootless(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := runner.commands[0].Args; !reflect.DeepEqual(got, []string{"info", "--format", "{{.Host.Security.Rootless}}"}) {
		t.Fatalf("arguments = %v", got)
	}
}

func TestRequireRootlessNeedsPodman(t *testing.T) {
	runner := missingRunner{}
	if err := (Client{Runner: runner}).RequireRootless(context.Background()); err == nil {
		t.Fatal("missing Podman accepted")
	}
}

func TestTypedLifecycleCommands(t *testing.T) {
	runner := &fakeRunner{}
	client := Client{Runner: runner}
	if err := client.RemoveContainer(context.Background(), "managed", 2, Streams{}); err != nil {
		t.Fatal(err)
	}
	if err := client.Logs(context.Background(), LogOptions{Container: "managed", Follow: true, Tail: 50}); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveImage(context.Background(), "localhost/image:test", Streams{}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"rm", "--force", "--time", "2", "--ignore", "managed"},
		{"logs", "--follow", "--tail", "50", "managed"},
		{"image", "rm", "localhost/image:test"},
	}
	for index := range want {
		if !reflect.DeepEqual(runner.commands[index].Args, want[index]) {
			t.Fatalf("command %d = %v, want %v", index, runner.commands[index].Args, want[index])
		}
	}
}

type missingRunner struct{}

func (missingRunner) Run(context.Context, process.Command) (process.Result, error) {
	return process.Result{}, errors.New("unexpected")
}
func (missingRunner) LookPath(string) (string, error) { return "", errors.New("missing") }
