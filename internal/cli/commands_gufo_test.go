package cli

import (
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/process"
)

func TestGufoCommandHelpAndRejectedSurfaces(t *testing.T) {
	runner := &commandRunner{}
	app, out, _ := testApp(t, runner)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	app.Root = root
	if err := app.runGufo([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	for _, want := range []string{"--preset", "--reasoning", "--cache-ram-bytes", "262144"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	for _, args := range [][]string{{"--profile", "cpu"}, {"--profile", "rdna4"}, {"--model", "/tmp/unverified.gguf"}, {"--unknown"}, {"unreviewed"}} {
		if err := app.runGufo(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if len(runner.commands) != 0 {
		t.Fatalf("rejected/help invocations ran processes: %v", runner.commands)
	}
}

func TestGufoAcceptanceSelection(t *testing.T) {
	cases := selectedAcceptanceCases([]string{"gufo"})
	if len(cases) != 2 || cases[1].id != "gufo" {
		t.Fatal(cases)
	}
	images := acceptanceImages(cases)
	if len(images) != 2 || images[1].target != "gufo" {
		t.Fatal(images)
	}
}

func TestGufoAcceptanceCleansExactContainerOnAnswerFailure(t *testing.T) {
	for _, answer := range []string{"GUFO_ACCEPTANCE_OK", "wrong"} {
		t.Run(answer, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ready" {
					io.WriteString(w, `{"status":"ready"}`)
					return
				}
				io.WriteString(w, `{"choices":[{"message":{"content":"`+answer+`"}}]}`)
			}))
			defer server.Close()
			id := strings.Repeat("a", 64)
			removed := ""
			runner := &commandRunner{run: func(cmd process.Command) process.Result {
				args := strings.Join(cmd.Args, " ")
				switch {
				case strings.HasPrefix(args, "run "):
					return process.Result{Stdout: []byte(id)}
				case strings.HasPrefix(args, "port "):
					return process.Result{Stdout: []byte(strings.TrimPrefix(server.URL, "http://"))}
				case strings.HasPrefix(args, "rm --force "):
					removed = args
					return process.Result{}
				default:
					t.Errorf("unexpected process %s", args)
					return process.Result{Status: 1}
				}
			}}
			app, _, _ := testApp(t, runner)
			managed := catalog.Catalog{GufoPresets: map[string]catalog.GufoPreset{defaultGufoPreset: {ID: defaultGufoPreset, Bundle: "bundle", Artifact: "model", DefaultContext: 262144, MaxOutputTokens: 16384, CacheRAMBytes: 28 << 30, ReasoningDefault: "medium"}}, Bundles: map[string]catalog.Bundle{"bundle": {Artifacts: []string{"model"}}}, Artifacts: map[string]catalog.Artifact{"model": {Target: "llama-models", Destination: "fixture.gguf"}}}
			paths, err := app.runGufoAcceptance(managed, t.TempDir(), "strix-halo", "/dev/dri/renderD128", "fixture")
			if (err == nil) != (answer == "GUFO_ACCEPTANCE_OK") {
				t.Fatalf("paths=%v err=%v", paths, err)
			}
			if !strings.HasSuffix(removed, " "+id) {
				t.Fatalf("exact created container not cleaned: %s", removed)
			}
		})
	}
}
