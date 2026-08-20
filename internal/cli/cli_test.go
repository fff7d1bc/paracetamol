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
	status := Main(context.Background(), nil, &stdout, &stderr)
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
	status := Main(context.Background(), []string{"--version"}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ROCmplete") {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
}
