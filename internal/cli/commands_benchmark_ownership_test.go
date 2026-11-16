package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/runtime"
	"paracetamol/internal/verification"
)

type benchmarkOwnershipRunner struct {
	t                 *testing.T
	name, application string
	mode              string
	cancel            context.CancelFunc
	ran               bool
	launches          []string
	removals          []string
}

func (runner *benchmarkOwnershipRunner) LookPath(name string) (string, error) {
	if name == "podman" {
		return "podman", nil
	}
	return "", errors.New("fixture executable is absent")
}

func (runner *benchmarkOwnershipRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	runner.t.Helper()
	if command.Name != "podman" || len(command.Args) == 0 {
		return process.Result{}, fmt.Errorf("unexpected benchmark fixture command %v", command)
	}
	switch command.Args[0] {
	case "info":
		return process.Result{Stdout: []byte("true")}, nil
	case "image":
		return process.Result{Stdout: []byte("sha256:fixture-image")}, nil
	case "create":
		runner.ran = true
		if len(command.Args) < 3 || command.Args[1] != "--label" || !strings.HasPrefix(command.Args[2], podman.ManagedLaunchLabel+"=") {
			runner.t.Fatalf("benchmark has no launch prefix: %v", command.Args)
		}
		token := strings.TrimPrefix(command.Args[2], podman.ManagedLaunchLabel+"=")
		if len(token) != 32 {
			runner.t.Fatalf("malformed launch token %q", token)
		}
		runner.launches = append(runner.launches, token)
		if runner.mode == "interrupted" {
			runner.cancel()
			return process.Result{}, context.Canceled
		}
		if runner.mode == "different launch" || runner.mode == "foreign occupant" {
			return process.Result{Status: 125, Stderr: []byte("name already occupied")}, nil
		}
		return process.Result{Status: 7, Stderr: []byte("fixture workload failed")}, nil
	case "ps":
		if runner.ran {
			return process.Result{Stdout: []byte(runner.name)}, nil
		}
		return process.Result{}, nil
	case "container":
		if !runner.ran || runner.mode == "absent" {
			return process.Result{Status: 1}, nil
		}
		if ctx.Err() != nil {
			runner.t.Fatal("benchmark cleanup inherited cancellation")
		}
		if command.Args[1] == "inspect" {
			labels := runner.labels()
			if runner.mode == "replacement during capture" {
				labels[podman.ManagedLaunchLabel] = strings.Repeat("f", 32)
			}
			return process.Result{Stdout: fixtureContainerJSON(runner.t, fixtureContainerID, runner.name, labels)}, nil
		}
		return process.Result{}, nil
	case "inspect":
		if ctx.Err() != nil {
			runner.t.Fatal("launch-label inspection inherited cancellation")
		}
		if runner.mode == "inspection failure" {
			return process.Result{}, errors.New("fixture inspection unavailable")
		}
		data, err := json.Marshal(runner.labels())
		return process.Result{Stdout: data}, err
	case "rm":
		if ctx.Err() != nil {
			runner.t.Fatal("benchmark removal inherited cancellation")
		}
		id := command.Args[len(command.Args)-1]
		if id != fixtureContainerID {
			runner.t.Fatalf("benchmark removed reusable name %q", id)
		}
		runner.removals = append(runner.removals, id)
		return process.Result{}, nil
	}
	return process.Result{}, fmt.Errorf("unexpected benchmark fixture arguments %v", command.Args)
}

func (runner *benchmarkOwnershipRunner) labels() map[string]string {
	token := runner.launches[len(runner.launches)-1]
	if runner.mode == "foreign occupant" {
		return nil
	}
	if runner.mode == "different launch" {
		token = strings.Repeat("e", 32)
	}
	role := "benchmark"
	if runner.mode == "wrong role" {
		role = "application"
	}
	return fixtureContainerLabels(runner.application, role, token)
}

func TestBenchmarkFailureCleanupIsLaunchSpecific(t *testing.T) {
	for _, applicationPath := range []string{"throughput", "speculative", "comfyui"} {
		for _, mode := range []string{"own failure", "different launch", "foreign occupant", "replacement during capture", "absent", "inspection failure", "wrong role", "interrupted"} {
			t.Run(applicationPath+"/"+mode, func(t *testing.T) {
				root := gatewayOwnershipHome(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				runner := &benchmarkOwnershipRunner{t: t, name: runtime.LlamaBenchmarkContainer, application: "llama-cpp", mode: mode, cancel: cancel}
				app, _, _ := testApp(t, runner)
				app.Context = ctx
				managed := catalog.Catalog{}
				app.catalog = &managed
				var err error
				switch applicationPath {
				case "throughput":
					model := filepath.Join(root, "fixture.gguf")
					if err := os.WriteFile(model, []byte("fixture"), 0o600); err != nil {
						t.Fatal(err)
					}
					err = app.benchmarkLlama([]string{"--model", model, "--profile", "cpu", "--data-dir", filepath.Join(root, "data")})
				case "speculative":
					runner.name = speculativeBenchmarkContainer
					// This is the production per-trial path below GPU validation.
					// The fake launch fails before any health/inference HTTP.
					_, err = app.executeSpeculativeTrial([]string{"podman", "run", "--name", runner.name, "fixture-image"}, 1, map[string]any{"fixture": true}, t.TempDir(), "fixture-image")
				case "comfyui":
					runner.name, runner.application = comfyBenchmarkContainer, "comfyui"
					managed, bundle, source, data := comfyOwnershipFixture(t, root)
					app.Root = source
					options := comfyBenchmarkOptions{profile: "strix-halo", dataRoot: data, image: "fixture-image", renderNode: "/dev/dri/renderD999999", port: benchmarkOwnershipPort(t), runs: 1, seed: 42, memoryPolicy: "balanced", kernelPolicy: "default", cacheMode: "isolated"}
					_, _, err = app.executeComfyBenchmark(managed, bundle, options, "20260820T010203Z-abcdef12")
					cache := filepath.Join(data, "apps", "comfyui", "benchmarks", ".cache", "20260820T010203Z-abcdef12-fixture")
					if _, statErr := os.Stat(cache); !os.IsNotExist(statErr) {
						t.Fatalf("failure retained isolated cache: %v", statErr)
					}
				}
				if err == nil || len(runner.launches) != 1 {
					t.Fatalf("expected one failed fake launch: launches=%v err=%v", runner.launches, err)
				}
				wantRemoval := mode == "own failure" || mode == "interrupted"
				if (len(runner.removals) == 1) != wantRemoval {
					t.Fatalf("removals=%v wantRemoval=%t err=%v", runner.removals, wantRemoval, err)
				}
				if mode == "inspection failure" && !strings.Contains(err.Error(), "fixture inspection unavailable") {
					t.Fatalf("lost cleanup failure: %v", err)
				}
				if mode == "interrupted" && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost original cancellation: %v", err)
				}
				if mode != "interrupted" && applicationPath == "throughput" && !strings.Contains(err.Error(), "fixture workload failed") && mode != "different launch" && mode != "foreign occupant" {
					t.Fatalf("lost original benchmark failure: %v", err)
				}
			})
		}
	}
}

func comfyOwnershipFixture(t *testing.T, root string) (catalog.Catalog, catalog.Bundle, string, string) {
	t.Helper()
	source, data := filepath.Join(root, "source"), filepath.Join(root, "data")
	prompt := []byte(`{"1":{"class_type":"SaveImage","inputs":{"filename_prefix":"fixture"}}}`)
	resource := "benchmarks/fixture.json"
	path := filepath.Join(source, "catalog", resource)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, prompt, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(prompt)
	payload := []byte("x")
	artifactDigest := sha256.Sum256(payload)
	artifact := catalog.Artifact{ID: "fixture", Target: "models", Destination: "fixture/model.bin", Size: 1, SHA256: hex.EncodeToString(artifactDigest[:])}
	bundle := catalog.Bundle{ID: "fixture", Application: "comfyui", Artifacts: []string{artifact.ID}}
	managed := catalog.Catalog{Artifacts: map[string]catalog.Artifact{artifact.ID: artifact}, Bundles: map[string]catalog.Bundle{bundle.ID: bundle}, Benchmarks: map[string]catalog.Benchmark{bundle.ID: {Bundle: bundle.ID, Resource: resource, SHA256: hex.EncodeToString(digest[:]), Renderer: "identity"}}}
	artifactPath := content.ArtifactPath(data, artifact)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(artifactPath, artifact.Size, artifact.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	return managed, bundle, source, data
}

func benchmarkOwnershipPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestSpeculativeTrialsAndRetriesReceiveFreshIdentityOutsideDefinition(t *testing.T) {
	runner := &benchmarkOwnershipRunner{t: t, name: speculativeBenchmarkContainer, application: "llama-cpp", mode: "own failure"}
	app, _, _ := testApp(t, runner)
	command := []string{"podman", "run", "--name", speculativeBenchmarkContainer, "fixture-image", "--prompt", "--label=" + podman.ManagedLaunchLabel + "=workload"}
	original := append([]string(nil), command...)
	definition := speculativeDefinition{Preset: "fixture", Depths: []int{1}, ContextDepths: []int{4096}, Repetitions: 2, Seed: 42}
	fingerprint, err := jsonDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	planned := makeSpeculativeTrials(definition.Depths, definition.ContextDepths, definition.Repetitions, definition.Seed)
	result := speculativeResult{Schema: benchmark.SpeculativeSchema, SuiteID: "20260820T010203Z-abcdef12", Definition: definition, Fingerprint: fingerprint, Status: "running", StartedAt: "now", Trials: append([]benchmark.SpeculativeTrial(nil), planned...)}
	var failed error
	for range 3 { // Same depth, another trial, then a resumed retry.
		if _, failed = app.executeSpeculativeTrial(command, 1, map[string]any{"seed": 42}, t.TempDir(), "fixture-image"); failed == nil {
			t.Fatal("fixture launch unexpectedly succeeded")
		}
	}
	if len(runner.launches) != 3 || runner.launches[0] == runner.launches[1] || runner.launches[0] == runner.launches[2] || runner.launches[1] == runner.launches[2] || len(runner.removals) != 3 {
		t.Fatalf("trials reused launch identity or missed cleanup: launches=%v removals=%v", runner.launches, runner.removals)
	}
	if !reflect.DeepEqual(command, original) {
		t.Fatal("launch metadata mutated reusable depth command")
	}
	if after, err := jsonDigest(definition); err != nil || after != fingerprint {
		t.Fatalf("ephemeral launch changed definition fingerprint: %q %v", after, err)
	}
	result.Trials[0].Status, result.Trials[0].StartedAt, result.Trials[0].FinishedAt, result.Trials[0].Error = "failed", "start", "finish", failed.Error()
	checkpoint := filepath.Join(gatewayOwnershipHome(t), "checkpoint.json")
	if err := checkpointFailedSpeculativeSweep(checkpoint, result, failed); !errors.Is(err, failed) {
		t.Fatalf("checkpoint lost original trial failure: %v", err)
	}
	var resumed speculativeResult
	if err := benchmark.ReadJSON(checkpoint, &resumed, true); err != nil {
		t.Fatal(err)
	}
	if resumed.Status != "failed" || resumed.FinishedAt == "" || resumed.Trials[0].Error != failed.Error() {
		t.Fatalf("early failed sweep wrote inconsistent checkpoint: %#v", resumed)
	}
	if err := validateSpeculativeResume(resumed, planned); err != nil {
		t.Fatalf("fresh launch metadata changed resume contract: %v", err)
	}
	encoded, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range runner.launches {
		if strings.Contains(string(encoded), token) {
			t.Fatal("ephemeral launch identity entered the checkpoint")
		}
	}
	// Exercise the actual resume transitions from the terminal failed fixture.
	// Either persisted running state must load if the process stops immediately.
	startedAt := resumed.StartedAt
	if err := checkpointRunningSpeculativeSweep(checkpoint, &resumed); err != nil {
		t.Fatal(err)
	}
	var running speculativeResult
	if err := benchmark.ReadJSON(checkpoint, &running, true); err != nil {
		t.Fatal(err)
	}
	if running.Status != "running" || running.FinishedAt != "" || running.StartedAt != startedAt {
		t.Fatalf("retry retained terminal sweep metadata: %#v", running)
	}
	if err := validateSpeculativeResume(running, planned); err != nil {
		t.Fatalf("interrupted resumed sweep cannot load: %v", err)
	}
	if err := checkpointRunningSpeculativeTrial(checkpoint, &running, 0); err != nil {
		t.Fatal(err)
	}
	if err := benchmark.ReadJSON(checkpoint, &running, true); err != nil {
		t.Fatal(err)
	}
	trial := running.Trials[0]
	if trial.Status != "running" || trial.StartedAt == "" || trial.StartedAt == "start" || trial.FinishedAt != "" || trial.Error != "" || trial.Metrics != nil {
		t.Fatalf("retry retained terminal trial metadata: %#v", trial)
	}
	if err := validateSpeculativeResume(running, planned); err != nil {
		t.Fatalf("interrupted retried trial cannot load: %v", err)
	}
}

func TestBenchmarkDryRunIncludesLaunchIdentityWithoutStartingWork(t *testing.T) {
	for _, applicationPath := range []string{"throughput", "comfyui"} {
		t.Run(applicationPath, func(t *testing.T) {
			root := gatewayOwnershipHome(t)
			runner := &benchmarkOwnershipRunner{t: t}
			app, stdout, _ := testApp(t, runner)
			managed := catalog.Catalog{}
			app.catalog = &managed
			data := filepath.Join(root, "data")
			if applicationPath == "throughput" {
				model := filepath.Join(root, "fixture.gguf")
				if err := os.WriteFile(model, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := app.benchmarkLlama([]string{"--model", model, "--profile", "cpu", "--data-dir", data, "--dry-run"}); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(data); !os.IsNotExist(err) {
					t.Fatalf("throughput dry run created data: %v", err)
				}
			} else {
				managed, bundle, source, fixtureData := comfyOwnershipFixture(t, root)
				app.Root = source
				options := comfyBenchmarkOptions{profile: "strix-halo", dataRoot: fixtureData, image: "fixture-image", port: 8190, runs: 1, seed: 42, memoryPolicy: "balanced", kernelPolicy: "default", cacheMode: "isolated", dryRun: true}
				if _, _, err := app.executeComfyBenchmark(managed, bundle, options, "20260820T010203Z-abcdef12"); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(fixtureData, "apps")); !os.IsNotExist(err) {
					t.Fatalf("ComfyUI dry run prepared runtime state: %v", err)
				}
			}
			if !strings.Contains(stdout.String(), "--label "+podman.ManagedLaunchLabel+"=") || runner.ran || len(runner.removals) != 0 {
				t.Fatalf("dishonest benchmark dry run: ran=%t removals=%v output=%s", runner.ran, runner.removals, stdout)
			}
		})
	}
}
