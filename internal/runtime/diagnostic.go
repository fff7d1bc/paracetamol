package runtime

import (
	"fmt"
	"strings"

	"paracetamol/internal/podman"
)

var diagnosticFields = []string{"PyTorch", "ROCm/HIP", "Device", "Architecture", "GPU operation", "GPU devices"}

func ParseGPUDiagnostic(output string) (map[string]string, error) {
	fields := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		label, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		for _, expected := range diagnosticFields {
			if label == expected {
				fields[label] = value
			}
		}
	}
	for _, expected := range diagnosticFields {
		if fields[expected] == "" {
			return nil, fmt.Errorf("GPU diagnostic output is missing %s", expected)
		}
	}
	if fields["GPU operation"] != "passed" || fields["GPU devices"] != "passed" {
		return nil, fmt.Errorf("GPU diagnostic did not pass")
	}
	return fields, nil
}

func GPUDiagnosticCommand(image string, renderNodes []string) []string {
	expected := pythonTuple(renderNodes)
	supported := pythonTuple([]string{"gfx1150", "gfx1151", "gfx1200", "gfx1201"})
	probe := "import glob,pathlib,sys,torch; expected=" + expected + "; supported=" + supported + "; nodes=glob.glob('/dev/dri/renderD*'); assert pathlib.Path('/dev/kfd').exists(),'/dev/kfd missing'; assert sorted(nodes)==sorted(expected),nodes; count=torch.cuda.device_count(); assert count==len(expected),(count,expected); props=[torch.cuda.get_device_properties(i) for i in range(count)]; architectures=[getattr(p,'gcnArchName','unknown').split(':',1)[0] for p in props]; assert len(set(architectures))==1,architectures; all(a in supported for a in architectures) or sys.exit('unsupported GPU architecture(s) '+','.join(architectures)); names=[torch.cuda.get_device_name(i) for i in range(count)]; actual=[torch.arange(1024,device='cuda:%d'%i).sum().item() for i in range(count)]; assert actual==[523776]*count,actual; print('PyTorch:',torch.__version__); print('ROCm/HIP:',torch.version.hip); print('Device:','; '.join(names)); print('Architecture:',architectures[0]); print('GPU operation: passed'); print('GPU devices: passed')"
	command := []string{"podman", "run", "--rm", "--userns", "keep-id"}
	command = append(command, podman.ManagedArguments("", "diagnostic")...)
	command = append(command, "--network", "none", "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges", "--pids-limit", "128", "--ulimit", "core=0:0", "--tmpfs", "/tmp:rw,nosuid,nodev,size=256m")
	command = append(command, gpuDeviceArguments(renderNodes)...)
	return append(command, "--entrypoint", "/opt/venv/bin/python", image, "-c", probe)
}

func CPUIsolationDiagnosticCommand(image string) []string {
	probe := "import glob,pathlib; assert not pathlib.Path('/dev/kfd').exists(),'/dev/kfd exposed'; nodes=glob.glob('/dev/dri/renderD*'); assert not nodes,nodes; print('CPU device isolation: passed')"
	command := []string{"podman", "run", "--rm", "--userns", "keep-id"}
	command = append(command, podman.ManagedArguments("", "diagnostic")...)
	return append(command, "--network", "none", "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges", "--pids-limit", "128", "--ulimit", "core=0:0", "--tmpfs", "/tmp:rw,nosuid,nodev,size=64m", "--entrypoint", "/opt/venv/bin/python", image, "-c", probe)
}

func pythonTuple(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = fmt.Sprintf("%q", value)
	}
	if len(quoted) == 1 {
		return "(" + quoted[0] + ",)"
	}
	return "(" + strings.Join(quoted, ",") + ")"
}
