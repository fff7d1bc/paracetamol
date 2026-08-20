package ui

import (
	"bytes"
	"testing"
)

func TestRedirectedOutputIsPlain(t *testing.T) {
	terminal := New(&bytes.Buffer{}, map[string]string{"TERM": "xterm-256color"})
	if got := terminal.Success("ready"); got != "ready" {
		t.Fatalf("redirected style = %q", got)
	}
}
