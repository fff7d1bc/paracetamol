package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/content"
	"paracetamol/internal/storage"
)

func assertWorkflowMutationReleased(t *testing.T, root string) {
	t.Helper()
	lock, err := storage.AcquireMutationLock(root)
	if err != nil {
		t.Fatalf("workflow operation retained mutation lock: %v", err)
	}
	lock.Close()
}

func TestWorkflowOnlyInstallExcludesHeldMutationLock(t *testing.T) {
	for _, state := range []string{"missing", "modified", "ready"} {
		t.Run(state, func(t *testing.T) {
			app, runner, data := workflowPreflightFixture(t)
			workflow := app.catalog.Workflows["first"]
			destination := workflowPath(data, workflow)
			var before []byte
			args := []string{"install", workflow.ID, "--data-dir", data}
			if state == "ready" {
				if err := app.installWorkflow(workflow, data, false); err != nil {
					t.Fatal(err)
				}
				var err error
				before, err = os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
			} else if state == "modified" {
				writeOwnershipSentinel(t, destination)
				before = []byte("must survive\n")
				args = append(args, "--force")
			} else if err := os.MkdirAll(data, 0o755); err != nil {
				t.Fatal(err)
			}
			lock, err := storage.AcquireMutationLock(data)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			var output bytes.Buffer
			app.Stdout = &output
			if err := app.contentWorkflows(args); !errors.Is(err, storage.ErrMutationBusy) {
				t.Fatalf("workflow entered held mutation reservation: %v", err)
			}
			if output.Len() != 0 || len(runner.commands) != 0 {
				t.Fatalf("blocked workflow reported success or ran subprocesses: %q, %v", output.String(), runner.commands)
			}
			actual, err := os.ReadFile(destination)
			if before == nil {
				if !os.IsNotExist(err) {
					t.Fatalf("blocked workflow created destination: %v", err)
				}
			} else if err != nil || !bytes.Equal(actual, before) {
				t.Fatalf("blocked workflow changed bytes: %q, %v", actual, err)
			}
		})
	}
}

func TestWorkflowInstallRetainsReservationThroughPublicationAndReadyOutput(t *testing.T) {
	for _, phase := range []string{"standalone", "first-workflow", "ready"} {
		t.Run(phase, func(t *testing.T) {
			app, runner, data := workflowPreflightFixture(t)
			match := "Installed workflow:"
			var install func() error
			if phase == "standalone" {
				install = func() error { return app.contentWorkflows([]string{"install", "first", "--data-dir", data}) }
			} else {
				mirror := prepareWorkflowOfflineInstall(t, app, data)
				if phase == "ready" {
					match = "All selected content is verified and ready."
				}
				install = func() error {
					return app.contentInstall([]string{"all", "--data-dir", data, "--non-interactive", "--local-mirror", mirror})
				}
			}
			writer := &cleanupInstallPauseWriter{match: match, entered: make(chan struct{}), release: make(chan struct{})}
			app.Stdout = writer
			done := make(chan error, 1)
			go func() { done <- install() }()
			finished := false
			finish := func() {
				if !finished {
					close(writer.release)
					if err := <-done; err != nil {
						t.Errorf("workflow install: %v", err)
					}
					finished = true
				}
			}
			defer finish()
			waitCleanupInstallPause(t, writer)
			if !workflowReady(data, app.catalog.Workflows["first"]) {
				t.Fatal("pause occurred before first workflow publication")
			}
			if phase != "standalone" {
				if _, err := content.RequireBundle(*app.catalog, app.catalog.Bundles["first"], data); err != nil {
					t.Fatalf("workflow phase did not retain verified artifact: %v", err)
				}
			}
			for _, scope := range []string{"data", "downloads", "caches"} {
				cleanupRunner := &commandRunner{}
				cleanup, output, _ := testApp(t, cleanupRunner)
				if err := cleanup.commandCleanup([]string{scope, "--data-dir", data, "--yes"}); !errors.Is(err, storage.ErrMutationBusy) {
					t.Fatalf("cleanup entered workflow %s phase: %v", phase, err)
				}
				if output.Len() != 0 || len(cleanupRunner.commands) != 0 {
					t.Fatalf("busy cleanup reached planning or subprocesses: %q, %v", output.String(), cleanupRunner.commands)
				}
			}
			finish()
			assertWorkflowMutationReleased(t, data)
			if phase == "standalone" && len(runner.commands) != 0 {
				t.Fatalf("standalone workflow needed Podman: %v", runner.commands)
			}
			for id, workflow := range app.catalog.Workflows {
				if phase != "standalone" || id == "first" {
					if !workflowReady(data, workflow) {
						t.Fatalf("completed workflow %s is missing", id)
					}
				}
			}
		})
	}
}

func TestWorkflowInstallRejectsCleanupApprovalReservation(t *testing.T) {
	app, _, data := workflowPreflightFixture(t)
	workflow := app.catalog.Workflows["first"]
	writeOwnershipSentinel(t, workflowPath(data, workflow))
	cleanup, _, _ := testApp(t, &commandRunner{run: gatewayOwnershipPodman})
	cleanup.Environment = app.Environment
	cleanup.Stdin = preflightTerminalReader{Reader: strings.NewReader("no\n")}
	writer := &cleanupInstallPauseWriter{match: "Proceed with", entered: make(chan struct{}), release: make(chan struct{})}
	cleanup.Stdout = writer
	done := make(chan error, 1)
	go func() { done <- cleanup.commandCleanup([]string{"data", "--data-dir", data}) }()
	defer func() { close(writer.release); <-done }()
	waitCleanupInstallPause(t, writer)
	if err := app.contentWorkflows([]string{"install", "first", "--force", "--data-dir", data}); !errors.Is(err, storage.ErrMutationBusy) {
		t.Fatalf("workflow bypassed cleanup approval reservation: %v", err)
	}
	if actual, err := os.ReadFile(workflowPath(data, workflow)); err != nil || string(actual) != "must survive\n" {
		t.Fatalf("workflow changed during cleanup approval: %q %v", actual, err)
	}
}

func TestWorkflowInstallCancellationAndFailureReleaseReservation(t *testing.T) {
	for _, phase := range []string{"first-workflow", "last-workflow", "late-conflict"} {
		t.Run(phase, func(t *testing.T) {
			app, _, data := workflowPreflightFixture(t)
			mirror := prepareWorkflowOfflineInstall(t, app, data)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			app.Context = ctx
			var output bytes.Buffer
			triggered := false
			app.Stdout = ownershipHookWriter{Writer: &output, hook: func(value string) {
				if triggered {
					return
				}
				if phase == "late-conflict" && strings.Contains(value, "Installed ") && strings.Contains(value, "model.bin") {
					triggered = true
					writeOwnershipSentinel(t, workflowPath(data, app.catalog.Workflows["last"]))
				} else if strings.Contains(value, "Installed workflow:") && (phase == "first-workflow" || phase == "last-workflow" && strings.Contains(value, "last.json")) {
					triggered = true
					cancel()
				}
			}}
			err := app.contentInstall([]string{"all", "--data-dir", data, "--non-interactive", "--local-mirror", mirror})
			if !triggered || err == nil || phase != "late-conflict" && !errors.Is(err, context.Canceled) {
				t.Fatalf("phase=%s triggered=%t error=%v", phase, triggered, err)
			}
			if strings.Contains(output.String(), "All selected content is verified and ready.") {
				t.Fatalf("failed/cancelled install claimed readiness: %s", output.String())
			}
			if phase == "late-conflict" {
				if _, err := os.Lstat(workflowPath(data, app.catalog.Workflows["first"])); !os.IsNotExist(err) {
					t.Fatalf("did not preflight all workflows again before publication: %v", err)
				}
			}
			if phase == "first-workflow" {
				if _, err := os.Lstat(workflowPath(data, app.catalog.Workflows["last"])); !os.IsNotExist(err) {
					t.Fatalf("published next workflow after cancellation: %v", err)
				}
			}
			if _, err := content.RequireBundle(*app.catalog, app.catalog.Bundles["first"], data); err != nil {
				t.Fatalf("lost completed verified model after workflow failure: %v", err)
			}
			assertWorkflowMutationReleased(t, data)
		})
	}
}

func TestWorkflowOnlyCancelledInstallLeavesMissingRootAbsent(t *testing.T) {
	app, runner, data := workflowPreflightFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.Context = ctx
	if err := app.contentWorkflows([]string{"install", "first", "--data-dir", data}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled workflow installation = %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(data); !os.IsNotExist(err) {
		t.Fatalf("cancelled workflow created data: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("cancelled workflow ran subprocesses: %v", runner.commands)
	}
}
