package process

import (
	"context"
	"testing"
)

func TestOSRunnerPreservesExitStatusAndCapturedOutput(t *testing.T) {
	result, err := (OSRunner{}).Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "printf output; printf error >&2; exit 7"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 7 || string(result.Stdout) != "output" || string(result.Stderr) != "error" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
