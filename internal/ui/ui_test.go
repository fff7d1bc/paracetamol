package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

type terminalBuffer struct{ bytes.Buffer }

func (*terminalBuffer) IsTerminal() bool { return true }

func TestSemanticStylesAndPlainFallback(t *testing.T) {
	interactive := &terminalBuffer{}
	terminal := New(interactive, map[string]string{"TERM": "xterm-256color"})
	for _, value := range []string{terminal.Heading("heading"), terminal.Command("command"), terminal.Success("ready"), terminal.Warning("warning"), terminal.Error("error"), terminal.Info("info"), terminal.Muted("muted"), terminal.Label("label"), terminal.Prompt("prompt"), terminal.State("missing")} {
		if !strings.Contains(value, "\x1b[") {
			t.Fatalf("terminal value is not styled: %q", value)
		}
	}
	plain := New(&bytes.Buffer{}, map[string]string{"TERM": "xterm-256color"})
	if got := plain.Success("ready"); got != "ready" {
		t.Fatalf("redirected style = %q", got)
	}
}

func TestNoColorPresenceAndDumbTerminalDisableStyles(t *testing.T) {
	for _, environment := range []map[string]string{{"TERM": "xterm", "NO_COLOR": ""}, {"TERM": "dumb"}} {
		if got := New(&terminalBuffer{}, environment).Warning("warning"); got != "warning" {
			t.Fatalf("style enabled for environment %#v: %q", environment, got)
		}
	}
}

func TestPromptAndNextKeepVisualSeparation(t *testing.T) {
	output := &bytes.Buffer{}
	terminal := New(output, nil)
	if got := terminal.PromptText("Continue? [y/N] ", true); got != "\nContinue? [y/N] " {
		t.Fatalf("prompt=%q", got)
	}
	terminal.Next("./paracetamol status")
	if got := output.String(); got != "\nNext:\n    ./paracetamol status\n" {
		t.Fatalf("next=%q", got)
	}
}

func TestRewriteLineUsesOneTTYLineAndPlainLogLines(t *testing.T) {
	interactive := &terminalBuffer{}
	terminal := New(interactive, map[string]string{"TERM": "xterm"})
	width := terminal.RewriteLine("Progress: 50%", 0, false)
	terminal.RewriteLine("Done", width, true)
	if got := interactive.String(); strings.Count(got, "\r") != 2 || strings.Count(got, "\n") != 1 {
		t.Fatalf("interactive rewrite=%q", got)
	}
	redirected := &bytes.Buffer{}
	plain := New(redirected, nil)
	plain.RewriteLine("Progress: 50%", 0, false)
	plain.RewriteLine("Done", 0, true)
	if got := redirected.String(); got != "Progress: 50%\nDone\n" {
		t.Fatalf("redirected rewrite=%q", got)
	}
}

func TestProgressThrottlesAndCompletes(t *testing.T) {
	output := &bytes.Buffer{}
	progress := NewProgress(output, nil, 100)
	now := time.Unix(100, 0)
	progress.now = func() time.Time { return now }
	progress.Update("model.gguf", 1, 1, 0, false)
	progress.Update("model.gguf", 1, 1, 1, false)
	now = now.Add(time.Minute)
	progress.Update("model.gguf", 1, 1, 2, false)
	progress.Update("model.gguf", 1, 1, 100, true)
	if got := output.String(); strings.Count(got, "\n") != 3 || !strings.Contains(got, "0.0%") || !strings.Contains(got, "2.0%") || !strings.Contains(got, "100.0%") {
		t.Fatalf("progress=%q", got)
	}
}

func TestDownloadProgressDistinguishesExactAndBoundedSizes(t *testing.T) {
	var exact strings.Builder
	progress := NewDownloadProgress(&exact, map[string]string{}, 100, false)
	progress.Update(0, false)
	progress.Update(100, true)
	if !strings.Contains(exact.String(), "0.0%") || !strings.Contains(exact.String(), "100.0%") || !strings.Contains(exact.String(), "expected") {
		t.Fatalf("exact progress = %q", exact.String())
	}
	var bounded strings.Builder
	limit := NewDownloadProgress(&bounded, map[string]string{}, 100, true)
	limit.Update(40, false)
	limit.Update(40, true)
	if !strings.Contains(bounded.String(), "limit") || strings.Contains(bounded.String(), "%") {
		t.Fatalf("bounded progress = %q", bounded.String())
	}
}
