package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
)

func TestValidateComfySuiteResumeRequiresCompletedEvidence(t *testing.T) {
	dataRoot := t.TempDir()
	artifact := catalog.Artifact{ID: "artifact", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 42}
	bundle := catalog.Bundle{ID: "bundle", Artifacts: []string{artifact.ID}}
	managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{artifact.ID: artifact}, Bundles: map[string]catalog.Bundle{bundle.ID: bundle}, Benchmarks: map[string]catalog.Benchmark{bundle.ID: {SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Renderer: "identity"}}}
	configuration := benchmark.ComfyConfiguration{Image: "image", ImageID: "sha256:image", Profile: "strix-halo", RenderNode: "/dev/dri/renderD128", Runs: 1, Seed: 10, MemoryPolicy: "balanced", KernelPolicy: "default", CacheMode: "persistent"}
	const suiteID = "20260820T010203Z-abcdef12"
	attemptID, _ := checkpointAttemptID(suiteID, 1)
	resultPath := comfySuiteAttemptResult(dataRoot, attemptID, bundle.ID)
	result := benchmark.ComfyResult{
		Schema: benchmark.ComfyResultSchema, RunID: attemptID, Bundle: bundle.ID, Status: "complete", StartedAt: "start", FinishedAt: "finish",
		Profile: configuration.Profile, RenderNode: configuration.RenderNode, MemoryPolicy: configuration.MemoryPolicy, KernelPolicy: configuration.KernelPolicy, CacheMode: configuration.CacheMode,
		Image:     benchmark.ImageIdentity{Reference: configuration.Image, ID: configuration.ImageID},
		Workflow:  benchmark.ComfyWorkflowEvidence{SourceSHA256: managed.Benchmarks[bundle.ID].SHA256, Renderer: "identity"},
		Artifacts: comfyArtifactEvidence(managed, bundle),
		Runs:      []benchmark.ComfyRun{{Index: 0, Kind: "cold", Seed: 10, PromptID: "prompt", WallSeconds: 1, Outputs: json.RawMessage(`{}`)}},
	}
	if err := benchmark.WriteJSON(resultPath, result); err != nil {
		t.Fatal(err)
	}
	suite := benchmark.ComfySuite{SuiteID: suiteID, Status: "running", CreatedAt: "now", Configuration: configuration, Entries: []benchmark.ComfySuiteEntry{{Bundle: bundle.ID, Result: resultPath, Status: "pass", Attempts: 1}}}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, dataRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resultPath); err != nil {
		t.Fatal(err)
	}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, dataRoot); err == nil {
		t.Fatal("missing completed result was accepted")
	}
}

func TestValidateComfySuiteResumeRejectsDuplicateEntries(t *testing.T) {
	root := t.TempDir()
	attemptID, _ := checkpointAttemptID("20260820T010203Z-abcdef12", 1)
	result := comfySuiteAttemptResult(root, attemptID, "bundle")
	suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12", Status: "failed", CreatedAt: "now", Entries: []benchmark.ComfySuiteEntry{
		{Bundle: "bundle", Status: "fail", Attempts: 1, Result: result, Error: "one"},
		{Bundle: "bundle", Status: "fail", Attempts: 1, Result: result, Error: "two"},
	}}
	managed := catalog.Catalog{Bundles: map[string]catalog.Bundle{"bundle": {ID: "bundle"}}}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{{ID: "bundle"}}, root); err == nil {
		t.Fatal("duplicate suite entries were accepted")
	}
}

func TestValidateComfySuiteResumeRejectsUnexpectedFailedResult(t *testing.T) {
	bundle := catalog.Bundle{ID: "bundle"}
	managed := catalog.Catalog{Bundles: map[string]catalog.Bundle{bundle.ID: bundle}}
	suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12", Status: "failed", CreatedAt: "now", Entries: []benchmark.ComfySuiteEntry{{Bundle: bundle.ID, Attempts: 1, Status: "fail", Result: "/unexpected/result.json", Error: "failed"}}}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, t.TempDir()); err == nil {
		t.Fatal("failed suite entry retained a misleading result path")
	}
}

func TestComfySuiteAttemptResumePreservesOrphanAndCleanupFailure(t *testing.T) {
	for _, failure := range []string{"parent-checkpoint", "cleanup"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "suite.json")
			bundle := catalog.Bundle{ID: "bundle"}
			managed := catalog.Catalog{Bundles: map[string]catalog.Bundle{bundle.ID: bundle}}
			suite := benchmark.ComfySuite{Schema: benchmark.ComfySuiteSchema, SuiteID: "20260820T010203Z-abcdef12", CreatedAt: "now", Status: "running"}
			if err := benchmark.WriteNewCheckpoint(path, suite); err != nil {
				t.Fatal(err)
			}
			var firstPath, firstID string
			runErr, checkpointErr := runComfySuiteAttempt(path, &suite, bundle.ID, root, func(id string) (string, comfySummary, error) {
				var durable benchmark.ComfySuite
				if err := benchmark.ReadJSON(path, &durable, true); err != nil || len(durable.Entries) != 1 || durable.Entries[0].Status != "running" || durable.Entries[0].Attempts != 1 {
					t.Fatalf("attempt not durable before workload: %#v %v", durable, err)
				}
				firstID, firstPath = id, comfySuiteAttemptResult(root, id, bundle.ID)
				if err := benchmark.WriteJSON(firstPath, map[string]string{"evidence": "first"}); err != nil {
					t.Fatal(err)
				}
				if failure == "parent-checkpoint" {
					if err := os.Rename(path, path+".saved"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(path, 0o755); err != nil {
						t.Fatal(err)
					}
					return firstPath, comfySummary{}, nil
				}
				return firstPath, comfySummary{}, errors.New("clean up ComfyUI benchmark container: fixture failure")
			})
			resumePath := path
			if failure == "parent-checkpoint" {
				if checkpointErr == nil || runErr != nil {
					t.Fatalf("expected parent write failure: %v %v", runErr, checkpointErr)
				}
				resumePath += ".saved"
			} else if runErr == nil || checkpointErr != nil {
				t.Fatalf("expected checkpointed cleanup failure: %v %v", runErr, checkpointErr)
			}
			if err := benchmark.ReadJSON(resumePath, &suite, true); err != nil {
				t.Fatal(err)
			}
			if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, root); err != nil {
				t.Fatal(err)
			}
			runErr, checkpointErr = runComfySuiteAttempt(resumePath, &suite, bundle.ID, root, func(id string) (string, comfySummary, error) {
				if id == firstID {
					t.Fatal("reused interrupted attempt identity")
				}
				child := comfySuiteAttemptResult(root, id, bundle.ID)
				return child, comfySummary{}, benchmark.WriteJSON(child, map[string]string{"evidence": "retry"})
			})
			if runErr != nil || checkpointErr != nil {
				t.Fatalf("retry failed: %v %v", runErr, checkpointErr)
			}
			entry := suite.Entries[0]
			if entry.Status != "pass" || entry.Attempts != 2 || len(entry.PreviousAttempts) != 1 || entry.PreviousAttempts[0].Result != firstPath || failure == "cleanup" && entry.PreviousAttempts[0].Error == "" {
				t.Fatalf("attempt evidence was lost: %#v", entry)
			}
			var first map[string]string
			if err := benchmark.ReadJSON(firstPath, &first, true); err != nil || first["evidence"] != "first" {
				t.Fatalf("old child changed: %v %v", first, err)
			}
		})
	}
}

func TestComfySuiteAttemptRejectsInvalidCountersBeforeWorkload(t *testing.T) {
	for _, attempts := range []int{-1, 0, math.MaxInt} {
		t.Run(fmt.Sprint(attempts), func(t *testing.T) {
			bundle := catalog.Bundle{ID: "bundle"}
			suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12", CreatedAt: "now", Status: "running", Entries: []benchmark.ComfySuiteEntry{{Bundle: bundle.ID, Status: "running", Attempts: attempts}}}
			if err := validateComfySuiteResume(suite, catalog.Catalog{}, []catalog.Bundle{bundle}, t.TempDir()); err == nil {
				t.Fatal("invalid counter accepted")
			}
		})
	}
	if _, err := checkpointAttemptID("../../escape", 1); err == nil {
		t.Fatal("unsafe suite ID accepted")
	}
	suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12", Entries: []benchmark.ComfySuiteEntry{{Bundle: "bundle", Attempts: math.MaxInt}}}
	_, err := runComfySuiteAttempt(filepath.Join(t.TempDir(), "absent.json"), &suite, "bundle", t.TempDir(), func(string) (string, comfySummary, error) {
		t.Fatal("overflow launched workload")
		return "", comfySummary{}, nil
	})
	if err == nil {
		t.Fatal("overflow was accepted")
	}
}

func TestComfySuiteRejectsMismatchedAttemptHistory(t *testing.T) {
	root := t.TempDir()
	bundle := catalog.Bundle{ID: "bundle"}
	suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12", CreatedAt: "now", Status: "running"}
	attemptID, _ := checkpointAttemptID(suite.SuiteID, 2)
	suite.Entries = []benchmark.ComfySuiteEntry{{Bundle: bundle.ID, Status: "running", Attempts: 2, Result: comfySuiteAttemptResult(root, attemptID, bundle.ID)}}
	if err := validateComfySuiteResume(suite, catalog.Catalog{}, []catalog.Bundle{bundle}, root); err == nil {
		t.Fatal("missing previous attempt was accepted")
	}
}

func TestComfySuiteInitialAttemptCheckpointFailureStartsNoWorkload(t *testing.T) {
	suite := benchmark.ComfySuite{SuiteID: "20260820T010203Z-abcdef12"}
	_, err := runComfySuiteAttempt(t.TempDir(), &suite, "bundle", t.TempDir(), func(string) (string, comfySummary, error) {
		t.Fatal("initial checkpoint failure started workload")
		return "", comfySummary{}, nil
	})
	if err == nil {
		t.Fatal("initial checkpoint unexpectedly succeeded")
	}
}

func TestComfySuiteRetriesRealPublishedChildAfterCleanupFailure(t *testing.T) {
	root := gatewayOwnershipHome(t)
	managed, bundle, source, data := comfyOwnershipFixture(t, root)
	port := benchmarkOwnershipPort(t)
	registered, cleanupFails := false, true
	launch := ""
	var server *http.Server
	var prefixes, cacheEnvironments []string
	runner := &commandRunner{run: func(command process.Command) process.Result {
		switch command.Args[0] {
		case "info":
			return process.Result{Stdout: []byte("true")}
		case "image":
			return process.Result{Stdout: []byte("sha256:fixture")}
		case "create":
			registered = true
			launch = strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
			for _, arg := range command.Args {
				if strings.HasPrefix(arg, "HOME=/data/benchmarks/.cache/") {
					cacheEnvironments = append(cacheEnvironments, arg)
				}
			}
			return process.Result{Stdout: []byte(fixtureContainerID)}
		case "container":
			if !registered {
				return process.Result{Status: 1}
			}
			if command.Args[1] == "inspect" {
				return process.Result{Stdout: fixtureRunningContainerJSON(t, fixtureContainerID, comfyBenchmarkContainer, fixtureContainerLabels("comfyui", "benchmark", launch))}
			}
			return process.Result{}
		case "start":
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/system_stats", func(output http.ResponseWriter, request *http.Request) { fmt.Fprint(output, `{}`) })
			mux.HandleFunc("/prompt", func(output http.ResponseWriter, request *http.Request) {
				var body struct {
					Prompt map[string]struct {
						Inputs map[string]any `json:"inputs"`
					} `json:"prompt"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				prefixes = append(prefixes, body.Prompt["1"].Inputs["filename_prefix"].(string))
				fmt.Fprint(output, `{"prompt_id":"fixture"}`)
			})
			mux.HandleFunc("/history/fixture", func(output http.ResponseWriter, request *http.Request) {
				fmt.Fprint(output, `{"fixture":{"status":{"status_str":"success","completed":true},"outputs":{}}}`)
			})
			server = &http.Server{Handler: mux}
			t.Cleanup(func() { _ = server.Close() })
			go func(current *http.Server) { _ = current.Serve(listener) }(server)
			return process.Result{}
		case "rm":
			_ = server.Close()
			if cleanupFails {
				return process.Result{Status: 1, Stderr: []byte("fixture cleanup failure")}
			}
			registered = false
			return process.Result{}
		case "inspect":
			labels, err := json.Marshal(fixtureContainerLabels("comfyui", "benchmark", launch))
			if err != nil {
				t.Fatal(err)
			}
			return process.Result{Stdout: labels}
		default:
			t.Fatalf("unexpected subprocess: %v", command.Args)
			return process.Result{}
		}
	}}
	app, _, _ := testApp(t, runner)
	app.Root = source
	options := comfyBenchmarkOptions{profile: "strix-halo", dataRoot: data, image: "fixture-image", imageID: "sha256:fixture", renderNode: "/dev/dri/renderD999999", port: port, runs: 1, seed: 42, memoryPolicy: "balanced", kernelPolicy: "default", cacheMode: "isolated"}
	suite := benchmark.ComfySuite{Schema: benchmark.ComfySuiteSchema, SuiteID: "20260820T010203Z-abcdef12", CreatedAt: "now", Status: "running", Configuration: comfyConfiguration(options)}
	path := filepath.Join(root, "suite.json")
	if err := benchmark.WriteNewCheckpoint(path, suite); err != nil {
		t.Fatal(err)
	}
	run := func(id string) (string, comfySummary, error) {
		return app.executeComfyBenchmark(managed, bundle, options, id)
	}
	runErr, writeErr := runComfySuiteAttempt(path, &suite, bundle.ID, data, run)
	if runErr == nil || writeErr != nil || !strings.Contains(runErr.Error(), "clean up ComfyUI benchmark container") {
		t.Fatalf("expected cleanup failure: %v %v", runErr, writeErr)
	}
	firstPath := suite.Entries[0].Result
	first, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := benchmark.ReadJSON(path, &suite, true); err != nil {
		t.Fatal(err)
	}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, data); err != nil {
		t.Fatal(err)
	}
	// The controller must not reclaim a failed-cleanup container. This models
	// explicit operator recovery before the next attempt, not automatic removal.
	registered, cleanupFails = false, false
	runErr, writeErr = runComfySuiteAttempt(path, &suite, bundle.ID, data, run)
	if runErr != nil || writeErr != nil {
		t.Fatalf("retry failed: %v %v", runErr, writeErr)
	}
	if err := validateComfySuiteResume(suite, managed, []catalog.Bundle{bundle}, data); err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 2 || prefixes[0] == prefixes[1] || len(cacheEnvironments) != 2 || cacheEnvironments[0] == cacheEnvironments[1] || !strings.Contains(suite.Entries[0].PreviousAttempts[0].Error, "clean up ComfyUI benchmark container") {
		t.Fatalf("reused paths or lost cleanup failure: prefixes=%v caches=%v entry=%#v", prefixes, cacheEnvironments, suite.Entries[0])
	}
	current, err := os.ReadFile(firstPath)
	if err != nil || string(current) != string(first) {
		t.Fatalf("first result changed: %v", err)
	}
}

func TestUpsertComfySuiteEntryReplacesFailedAttempt(t *testing.T) {
	entries := []benchmark.ComfySuiteEntry{{Bundle: "bundle", Status: "fail", Error: "old"}}
	entries = upsertComfySuiteEntry(entries, benchmark.ComfySuiteEntry{Bundle: "bundle", Status: "pass", Result: "result.json"})
	if len(entries) != 1 || entries[0].Status != "pass" || entries[0].Error != "" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestComfySuiteSignatureBindsImageAndArtifactBytes(t *testing.T) {
	artifact := catalog.Artifact{ID: "artifact", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 42}
	bundle := catalog.Bundle{ID: "bundle", Artifacts: []string{artifact.ID}}
	managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{artifact.ID: artifact}, Benchmarks: map[string]catalog.Benchmark{bundle.ID: {SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Renderer: "identity"}}}
	options := comfyBenchmarkOptions{image: "image", imageID: "sha256:one", runs: 1}
	initial, err := comfySuiteSignature(managed, []catalog.Bundle{bundle}, options)
	if err != nil {
		t.Fatal(err)
	}
	options.imageID = "sha256:two"
	rebuilt, err := comfySuiteSignature(managed, []catalog.Bundle{bundle}, options)
	if err != nil {
		t.Fatal(err)
	}
	artifact.SHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	managed.Artifacts[artifact.ID] = artifact
	changedArtifact, err := comfySuiteSignature(managed, []catalog.Bundle{bundle}, options)
	if err != nil {
		t.Fatal(err)
	}
	if initial == rebuilt || rebuilt == changedArtifact {
		t.Fatal("suite signature did not bind rebuilt image and changed artifact")
	}
}
