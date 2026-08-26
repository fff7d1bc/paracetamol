// Package application owns the closed managed-application and build-unit
// registry. Other packages derive menus, plans, image sets, and capabilities
// from this registry instead of maintaining parallel application lists.
package application

import (
	"fmt"

	"paracetamol/internal/identity"
	"paracetamol/internal/platform"
)

type ID string
type BuildID string

const (
	ComfyUI   ID = "comfyui"
	LlamaCPP  ID = "llama-cpp"
	DwarfStar ID = "dwarfstar"

	BuildRuntime      BuildID = "runtime"
	BuildPyTorchBase  BuildID = "pytorch-base"
	BuildContentTools BuildID = "content-tools"
	BuildComfyUI      BuildID = "comfyui"
	BuildLlamaCPP     BuildID = "llama-cpp"
	BuildDwarfStar    BuildID = "dwarfstar"
)

type Action struct {
	Arguments   []string
	Description string
}

func (action Action) Command() string { return identity.Command(action.Arguments...) }

type Spec struct {
	ID            string
	DisplayName   string
	Summary       string
	Image         string
	ContainerName string
	Build         BuildID
	Port          int
	RuntimeFamily platform.RuntimeFamily
	Modes         []string
	Shell         bool
	Logs          bool
	MultiGPU      bool
	PyTorchBase   bool
	AfterBuild    []Action
	AfterContent  []Action
}

type BuildUnit struct {
	ID            BuildID
	DisplayName   string
	Target        string
	Image         string
	Prerequisites []BuildID
	Application   string
}

var specs = []Spec{
	{
		ID: string(ComfyUI), DisplayName: "ComfyUI",
		Summary:       "image and video generation with curated workflows",
		Image:         identity.Image("comfyui-ubuntu26.04-rocm7.14-0.28.0-r11"),
		ContainerName: identity.Container("comfyui"), Build: BuildComfyUI, Port: 8188,
		RuntimeFamily: platform.RuntimeROCm, Modes: []string{"server"}, Shell: true, Logs: true, MultiGPU: true, PyTorchBase: true,
		AfterBuild:   []Action{{Arguments: []string{"content", "install", "comfyui"}, Description: "choose a reviewed ComfyUI recipe"}},
		AfterContent: []Action{{Arguments: []string{"run", "comfyui"}, Description: "start the private web application"}},
	},
	{
		ID: string(LlamaCPP), DisplayName: "llama.cpp",
		Summary:       "local GGUF inference, routing, and native throughput tests",
		Image:         identity.Image("llama-cpp-ubuntu26.04-rocm7.14-5d5cb4c-r30"),
		ContainerName: identity.Container("llama-cpp"), Build: BuildLlamaCPP, Port: 8080,
		RuntimeFamily: platform.RuntimeROCm, Modes: []string{"server", "cli"}, Shell: true, Logs: true, MultiGPU: true,
		AfterBuild: []Action{{Arguments: []string{"content", "install", "llama-cpp", "qwen3.8"}, Description: "install the reviewed Qwen3.8 family"}},
		AfterContent: []Action{{
			Arguments:   []string{"run", "llama-cpp", "server", "--preset", "qwen3.8-27b-mtp-ud-q8-k-xl"},
			Description: "start the default direct-model server",
		}},
	},
	{
		ID: string(DwarfStar), DisplayName: "DwarfStar",
		Summary:       "experimental high-memory DeepSeek V4 Flash inference",
		Image:         identity.Image("dwarfstar-ubuntu26.04-rocm7.14-84cc882-r7"),
		ContainerName: identity.Container("dwarfstar"), Build: BuildDwarfStar, Port: 8000,
		RuntimeFamily: platform.RuntimeROCm, Modes: []string{"server", "cli"}, Shell: true, Logs: true,
		AfterBuild:   []Action{{Arguments: []string{"content", "install", "dwarfstar", "flash-0731-q2-imatrix"}, Description: "install the reviewed high-memory model"}},
		AfterContent: []Action{{Arguments: []string{"run", "dwarfstar", "server"}, Description: "start the model server"}},
	},
}

var units = []BuildUnit{
	{ID: BuildRuntime, DisplayName: "minimal ROCm runtime", Target: "rocm-runtime", Image: identity.Image("runtime-ubuntu26.04-rocm7.14-r2")},
	{ID: BuildPyTorchBase, DisplayName: "ROCm/PyTorch base", Target: "rocm-base", Image: identity.Image("base-ubuntu26.04-rocm7.14-torch2.11-r5"), Prerequisites: []BuildID{BuildRuntime}},
	{ID: BuildContentTools, DisplayName: "content tools", Target: "content-tools", Image: identity.Image("content-ubuntu26.04-huggingface1.27-r1")},
	{ID: BuildComfyUI, DisplayName: "ComfyUI", Target: "comfyui", Image: specs[0].Image, Prerequisites: []BuildID{BuildPyTorchBase}, Application: string(ComfyUI)},
	{ID: BuildLlamaCPP, DisplayName: "llama.cpp", Target: "llama-cpp", Image: specs[1].Image, Prerequisites: []BuildID{BuildRuntime}, Application: string(LlamaCPP)},
	{ID: BuildDwarfStar, DisplayName: "DwarfStar", Target: "dwarfstar", Image: specs[2].Image, Prerequisites: []BuildID{BuildRuntime}, Application: string(DwarfStar)},
}

func All() []Spec {
	result := make([]Spec, len(specs))
	for index, spec := range specs {
		result[index] = cloneSpec(spec)
	}
	return result
}

func ByID(value string) (Spec, bool) {
	for _, spec := range specs {
		if spec.ID == value {
			return cloneSpec(spec), true
		}
	}
	return Spec{}, false
}

func BuildUnits() []BuildUnit {
	result := make([]BuildUnit, len(units))
	for index, unit := range units {
		result[index] = cloneBuildUnit(unit)
	}
	return result
}

func BuildUnitByID(value string) (BuildUnit, bool) {
	for _, unit := range units {
		if string(unit.ID) == value {
			return cloneBuildUnit(unit), true
		}
	}
	return BuildUnit{}, false
}

func cloneSpec(spec Spec) Spec {
	spec.Modes = append([]string(nil), spec.Modes...)
	spec.AfterBuild = cloneActions(spec.AfterBuild)
	spec.AfterContent = cloneActions(spec.AfterContent)
	return spec
}

func cloneActions(actions []Action) []Action {
	result := make([]Action, len(actions))
	for index, action := range actions {
		result[index] = Action{Arguments: append([]string(nil), action.Arguments...), Description: action.Description}
	}
	return result
}

func cloneBuildUnit(unit BuildUnit) BuildUnit {
	unit.Prerequisites = append([]BuildID(nil), unit.Prerequisites...)
	return unit
}

func BuildClosure(targets []BuildID) ([]BuildUnit, error) {
	index := make(map[BuildID]BuildUnit, len(units))
	for _, unit := range units {
		index[unit.ID] = unit
	}
	state := make(map[BuildID]uint8)
	var ordered []BuildUnit
	var visit func(BuildID) error
	visit = func(identifier BuildID) error {
		unit, ok := index[identifier]
		if !ok {
			return fmt.Errorf("unknown build unit %q", identifier)
		}
		if state[identifier] == 2 {
			return nil
		}
		if state[identifier] == 1 {
			return fmt.Errorf("build dependency cycle at %q", identifier)
		}
		state[identifier] = 1
		for _, prerequisite := range unit.Prerequisites {
			if err := visit(prerequisite); err != nil {
				return err
			}
		}
		state[identifier] = 2
		ordered = append(ordered, unit)
		return nil
	}
	for _, target := range targets {
		if err := visit(target); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func Validate() error {
	applications := make(map[string]bool)
	containers := make(map[string]bool)
	images := make(map[string]bool)
	mappedApplications := make(map[string]bool)
	buildIDs := make(map[BuildID]bool)
	buildTargets := make(map[string]bool)
	for _, spec := range specs {
		if spec.ID == "" || spec.DisplayName == "" || spec.Summary == "" || spec.Image == "" || spec.ContainerName == "" || spec.Build == "" || spec.Port < 1 || spec.RuntimeFamily == "" || len(spec.Modes) == 0 {
			return fmt.Errorf("incomplete application registry entry %q", spec.ID)
		}
		if applications[spec.ID] || containers[spec.ContainerName] || images[spec.Image] {
			return fmt.Errorf("duplicate application identity for %q", spec.ID)
		}
		applications[spec.ID], containers[spec.ContainerName], images[spec.Image] = true, true, true
		seenModes := make(map[string]bool, len(spec.Modes))
		for _, mode := range spec.Modes {
			if mode != "server" && mode != "cli" || seenModes[mode] {
				return fmt.Errorf("application %q has invalid or duplicate mode %q", spec.ID, mode)
			}
			seenModes[mode] = true
		}
		for _, action := range append(append([]Action(nil), spec.AfterBuild...), spec.AfterContent...) {
			if len(action.Arguments) == 0 || action.Description == "" {
				return fmt.Errorf("application %q has an incomplete guide action", spec.ID)
			}
		}
	}
	for _, unit := range units {
		if unit.ID == "" || unit.Target == "" || unit.Image == "" {
			return fmt.Errorf("incomplete build unit %q", unit.ID)
		}
		if buildIDs[unit.ID] || buildTargets[unit.Target] {
			return fmt.Errorf("duplicate build-unit identity for %q", unit.ID)
		}
		buildIDs[unit.ID], buildTargets[unit.Target] = true, true
		if unit.Application != "" && !applications[unit.Application] {
			return fmt.Errorf("build unit %q references unknown application", unit.ID)
		}
		if unit.Application != "" {
			if mappedApplications[unit.Application] {
				return fmt.Errorf("application %q maps to multiple build units", unit.Application)
			}
			mappedApplications[unit.Application] = true
		}
	}
	for _, spec := range specs {
		unit, ok := BuildUnitByID(string(spec.Build))
		if !ok {
			return fmt.Errorf("application %q references unknown build unit", spec.ID)
		}
		if unit.Application != spec.ID || unit.Image != spec.Image {
			return fmt.Errorf("application %q and build unit %q disagree", spec.ID, spec.Build)
		}
		if spec.PyTorchBase && !containsBuildID(unit.Prerequisites, BuildPyTorchBase) {
			return fmt.Errorf("application %q declares PyTorch without the PyTorch base prerequisite", spec.ID)
		}
	}
	_, err := BuildClosure([]BuildID{BuildRuntime, BuildPyTorchBase, BuildContentTools, BuildComfyUI, BuildLlamaCPP, BuildDwarfStar})
	return err
}

func containsBuildID(values []BuildID, expected BuildID) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
