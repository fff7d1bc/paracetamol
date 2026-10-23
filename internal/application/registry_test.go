package application

import (
	"slices"
	"testing"
)

func TestLlamaGuideActionsUseRecommendedSelection(t *testing.T) {
	spec, ok := ByID(string(LlamaCPP))
	if !ok || len(spec.AfterBuild) != 1 || len(spec.AfterContent) != 1 ||
		!slices.Equal(spec.AfterBuild[0].Arguments, []string{"content", "install", "llama-cpp", "qwen3.8-flash-next"}) ||
		!slices.Equal(spec.AfterContent[0].Arguments, []string{"run", "llama-cpp", "server", "--preset", "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl-dflash2"}) {
		t.Fatalf("incoherent recommended install/start actions: %#v", spec)
	}
}

func TestManagedApplicationSet(t *testing.T) {
	want := []string{"comfyui", "llama-cpp"}
	got := All()
	if len(got) != len(want) {
		t.Fatalf("applications=%v", got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("application[%d]=%s, want %s", i, got[i].ID, id)
		}
	}
	for _, retired := range []string{"gufo", "dwarfstar"} {
		if _, ok := ByID(retired); ok {
			t.Fatalf("retired application %s is still registered", retired)
		}
		if _, ok := BuildUnitByID(retired); ok {
			t.Fatalf("retired application %s is still buildable", retired)
		}
		if _, err := BuildClosure([]BuildID{BuildID(retired)}); err == nil {
			t.Fatalf("retired application %s has a build closure", retired)
		}
	}
}

func TestRegistryAndBuildClosure(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
	closure, err := BuildClosure([]BuildID{BuildComfyUI, BuildLlamaCPP, BuildContentTools})
	if err != nil {
		t.Fatal(err)
	}
	want := []BuildID{BuildRuntime, BuildPyTorchBase, BuildComfyUI, BuildLlamaCPP, BuildContentTools}
	if len(closure) != len(want) {
		t.Fatalf("closure length = %d, want %d", len(closure), len(want))
	}
	for index, identifier := range want {
		if closure[index].ID != identifier {
			t.Fatalf("closure[%d] = %s, want %s", index, closure[index].ID, identifier)
		}
	}
}

func TestRegistryReturnsDefensiveCopies(t *testing.T) {
	applications := All()
	applications[0].Modes[0] = "mutated"
	applications[0].AfterBuild[0].Arguments[0] = "mutated"
	units := BuildUnits()
	units[1].Prerequisites[0] = BuildContentTools

	spec, _ := ByID(string(ComfyUI))
	unit, _ := BuildUnitByID(string(BuildPyTorchBase))
	if spec.Modes[0] == "mutated" || spec.AfterBuild[0].Arguments[0] == "mutated" || unit.Prerequisites[0] != BuildRuntime {
		t.Fatal("caller mutation escaped the registry boundary")
	}
}
