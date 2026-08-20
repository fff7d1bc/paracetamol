package runtime

import (
	"rocmplete/internal/podman"
	"rocmplete/internal/storage"
)

func ShellCommand(image, dataDir, volumeSuffix, application string) []string {
	layout := storage.Layout{Root: dataDir}
	readOnly := readOnlySharedSuffix(volumeSuffix)
	volumes := []string{"--volume", layout.Application(application) + ":/data" + volumeSuffix}
	switch application {
	case "comfyui":
		volumes = append(volumes, "--volume", layout.ComfyModels()+":/content/models"+readOnly)
	case "llama-cpp":
		volumes = append(volumes, "--volume", layout.LlamaModels()+":/content/models"+readOnly)
	case "dwarfstar":
		volumes = append(volumes, "--volume", layout.DwarfStarModels()+":/content/models"+readOnly)
	}
	command := []string{"podman", "run", "--rm", "--userns", "keep-id", "--umask", podman.CurrentUmask(), "-it"}
	command = append(command, podman.ManagedArguments(application, "shell")...)
	command = append(command, "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges", "--ulimit", "core=0:0", "--tmpfs", "/tmp:rw,nosuid,nodev,size=8g")
	command = append(command, volumes...)
	return append(command, "--entrypoint", "/bin/bash", image)
}
