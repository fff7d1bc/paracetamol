package application

import "testing"

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
