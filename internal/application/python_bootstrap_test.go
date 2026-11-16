package application

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestPythonBaseBootstrapPinsAndDependentConstraints(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		contents, err := os.ReadFile("../../" + path)
		if err != nil {
			t.Fatal(err)
		}
		return string(contents)
	}
	const source = "containers/python_base/requirements.txt"
	const installed = "/opt/python-bootstrap-requirements.txt"
	pins := make(map[string]string)
	exact := regexp.MustCompile(`^([a-z][a-z0-9-]*)==([0-9][a-zA-Z0-9.+!-]*)$`)
	for _, line := range strings.Split(read(source), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := exact.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("bootstrap requirement is not one exact package version: %q", line)
		}
		if _, duplicate := pins[match[1]]; duplicate {
			t.Fatalf("duplicate bootstrap package: %s", match[1])
		}
		pins[match[1]] = match[2]
	}
	names := make([]string, 0, len(pins))
	for name := range pins {
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"packaging", "pip", "setuptools", "wheel"}) {
		t.Fatalf("bootstrap dependency closure changed: %v", names)
	}
	constraints := read("applications/comfyui/constraints.txt")
	for _, name := range []string{"packaging", "setuptools"} {
		if !strings.Contains("\n"+constraints, "\n"+name+"=="+pins[name]+"\n") {
			t.Fatalf("ComfyUI %s constraint disagrees with shared-base pin", name)
		}
	}
	allowlist := read(".containerignore")
	for _, path := range []string{"containers/python_base/", source} {
		if !strings.Contains(allowlist, "\n!"+path+"\n") {
			t.Fatalf("bootstrap build input is not allowlisted: %s", path)
		}
	}
	containerfile := read("Containerfile")
	baseStart := strings.Index(containerfile, "\nFROM ${ROCM_RUNTIME_IMAGE} AS rocm-base\n")
	comfyStart := strings.Index(containerfile, "\nFROM ${ROCM_BASE_IMAGE} AS comfyui\n")
	nativeStart := strings.Index(containerfile, "\nFROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk\n")
	if baseStart < 0 || comfyStart <= baseStart || nativeStart <= comfyStart {
		t.Fatal("cannot identify independently owned Python and native build stages")
	}
	base, comfy := containerfile[baseStart:comfyStart], containerfile[comfyStart:nativeStart]
	if !strings.Contains(base, "COPY "+source+" "+installed+"\n") {
		t.Fatal("shared base does not retain its bootstrap pin file")
	}
	for stage, text := range map[string]string{"base": base, "comfyui": comfy} {
		installs := 0
		for _, instruction := range strings.Split(text, "\nRUN ") {
			if !strings.Contains(instruction, "python -m pip install") {
				continue
			}
			installs++
			if !(stage == "base" && strings.Contains(instruction, "--requirement "+installed)) && !strings.Contains(instruction, "--constraint "+installed) {
				t.Fatalf("%s pip install is not bound to the shared bootstrap versions:\n%s", stage, instruction)
			}
			if !strings.Contains(instruction, "python -m pip check") {
				t.Fatalf("%s installation omits dependency validation:\n%s", stage, instruction)
			}
		}
		if installs != 2 {
			t.Fatalf("%s pip install boundaries changed: %d", stage, installs)
		}
	}
	if strings.Contains(containerfile[:baseStart], installed) || strings.Contains(containerfile[nativeStart:], installed) {
		t.Fatal("Python-base tool policy leaked into independent content/native images")
	}
}
