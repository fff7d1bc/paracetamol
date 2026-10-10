package gateway

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/verification"
)

func TestGufoSchedulerSerializesAndSwitchesAcrossAllEngines(t *testing.T) {
	fake := &fakeLifecycle{}
	scheduler, err := NewScheduler(fake, 64, map[Allocation]int{AllocationGufo: 1, AllocationDwarfStar: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := receiveLease(t, acquireAsync(scheduler, AllocationGufo))
	second := acquireAsync(scheduler, AllocationGufo)
	waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.QueuedRequests == 1 })
	assertWaiting(t, second)
	first.Release()
	receiveLease(t, second).Release()
	for _, allocation := range []Allocation{AllocationLlamaCPP, AllocationDwarfStar, AllocationGufo} {
		receiveLease(t, acquireAsync(scheduler, allocation)).Release()
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "start gufo,stop gufo,start llama-cpp,stop llama-cpp,start dwarfstar,stop dwarfstar,start gufo,stop gufo"
	if got := strings.Join(fake.Events(), ","); got != want {
		t.Fatalf("%s", got)
	}
}

func gufoRegistryFixture(t *testing.T) (catalog.Catalog, string) {
	t.Helper()
	root := t.TempDir()
	artifact := catalog.Artifact{ID: "weights", Destination: "flash-next/one.gguf", Target: "llama-models", Size: 4, SHA256: "fixture"}
	managed := catalog.Catalog{
		Artifacts:   map[string]catalog.Artifact{"weights": artifact},
		Bundles:     map[string]catalog.Bundle{"gufo-bundle": {ID: "gufo-bundle", Application: "gufo", Artifacts: []string{"weights"}}},
		GufoPresets: map[string]catalog.GufoPreset{"flash-next-gufo": {ID: "flash-next-gufo", Bundle: "gufo-bundle", Artifact: "weights", DefaultContext: 262144, MaxOutputTokens: 16384, CacheRAMBytes: 28 << 30, AgentTools: true, ReasoningControl: "effort", ReasoningLevels: []string{"medium", "xhigh"}, ReasoningDefault: "medium", ReasoningOff: true}},
	}
	path := content.ArtifactPath(root, artifact)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := verification.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(path, artifact.Size, artifact.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	return managed, root
}

func TestGufoRegistryHardwareAndReceiptGates(t *testing.T) {
	managed, root := gufoRegistryFixture(t)
	for _, profile := range []string{"strix-halo", "strix-point", "rdna4", "cpu"} {
		for _, nodes := range [][]string{nil, {"/dev/dri/renderD128"}, {"/dev/dri/renderD128", "/dev/dri/renderD129"}} {
			registry, _, err := BuildRegistry(managed, root, []string{"gufo"}, profile, nodes, "rocm")
			want := profile == "strix-halo" && len(nodes) == 1
			if (err == nil) != want {
				t.Fatalf("%s %v err=%v", profile, nodes, err)
			}
			if want && (len(registry.Models) != 1 || registry.Models[0].Backend != "gufo" || !registry.Models[0].AgentTools) {
				t.Fatalf("%+v", registry.Models)
			}
		}
	}
	path := content.ArtifactPath(root, managed.Artifacts["weights"])
	if err := os.WriteFile(path, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildRegistry(managed, root, []string{"gufo"}, "strix-halo", []string{"/dev/dri/renderD128"}, "rocm"); err == nil {
		t.Fatal("stale receipt accepted")
	}
}

func TestGufoLifecycleCommandAndSingleModelPolicy(t *testing.T) {
	managed, root := gufoRegistryFixture(t)
	registry, _, err := BuildRegistry(managed, root, []string{"gufo"}, "strix-halo", []string{"/dev/dri/renderD128"}, "rocm")
	if err != nil {
		t.Fatal(err)
	}
	options := LifecycleOptions{Catalog: managed, Registry: registry, DataRoot: root, Profile: "strix-halo", RenderNodes: []string{"/dev/dri/renderD128"}, LlamaBackend: "rocm", LlamaModelsMax: 1, VolumeSuffix: ":rw", Runner: &lifecycleRunner{}, Log: io.Discard}
	lifecycle, err := NewContainerLifecycle(options)
	if err != nil {
		t.Fatal(err)
	}
	command, port, ready, err := lifecycle.command(AllocationGufo)
	if err != nil || port != 8080 || ready != "/ready" {
		t.Fatalf("%d %s %v", port, ready, err)
	}
	joined := strings.Join(command, " ")
	for _, want := range []string{"--publish 127.0.0.1::8080", "--name paracetamol-gateway-gufo", "PARACETAMOL_GUFO_CONTEXT=262144", ":/content/models/one.gguf:ro"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %s", want, joined)
		}
	}
	options.Registry.Models = append(options.Registry.Models, options.Registry.Models[0])
	if _, err := NewContainerLifecycle(options); err == nil {
		t.Fatal("multiple eager Gufo models accepted")
	}
	if state, _ := singleModelState(StateReady, "gufo"); state != ModelLoaded {
		t.Fatal(state)
	}
	if state, message := singleModelState(StateStopping, "gufo"); state != ModelUnknown || !strings.Contains(message, "gufo") {
		t.Fatal(state, message)
	}
}
