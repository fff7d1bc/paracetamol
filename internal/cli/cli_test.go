package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
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

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	status := Main(context.Background(), []string{"--version"}, strings.NewReader(""), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ROCmplete") {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
}

func TestFlattenedAcceptanceHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), []string{"acceptance", "--help"}, strings.NewReader(""), &stdout, &stderr); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage: ./rocmplete acceptance [OPTIONS]") {
		t.Fatalf("unexpected acceptance help: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if status := Main(context.Background(), []string{"acceptance", "run", "--help"}, strings.NewReader(""), &stdout, &stderr); status == 0 {
		t.Fatalf("legacy acceptance run unexpectedly succeeded")
	}
}

func TestComfyBenchmarkRenameIsExplicit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), []string{"benchmark", "comfyui", "--help"}, strings.NewReader(""), &stdout, &stderr); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "benchmark comfyui") {
		t.Fatalf("unexpected benchmark help: %s", stderr.String())
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
			if !strings.Contains(stdout.String(), "Usage: ./rocmplete "+command) {
				t.Fatalf("unexpected group help: %q", stdout.String())
			}
		})
	}
}
