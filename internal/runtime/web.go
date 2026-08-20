package runtime

import (
	"fmt"

	"rocmplete/internal/config"
	"rocmplete/internal/podman"
	"rocmplete/internal/storage"
)

type WebOptions struct {
	Image                    string
	Profile                  string
	Listen                   string
	Port                     int
	DataDir                  string
	RenderNodes              []string
	Detach                   bool
	Unconfined               bool
	DisableBundledExtensions bool
	Arguments                []string
	ContainerName            string
	Application              string
	MemoryPolicy             string
	KernelPolicy             string
	Environment              []string
	Publish                  bool
	NetworkNone              bool
	ContainerRole            string
}

func WebCommand(options WebOptions, volumeSuffix string) []string {
	application, _ := config.ApplicationByID(options.Application)
	if options.ContainerName == "" {
		options.ContainerName = application.ContainerName
	}
	if options.ContainerRole == "" {
		options.ContainerRole = "application"
	}
	layout := storage.Layout{Root: options.DataDir}
	readOnly := readOnlySharedSuffix(volumeSuffix)
	command := []string{"podman", "run", "--rm", "--userns", "keep-id", "--umask", podman.CurrentUmask(), "--name", options.ContainerName}
	command = append(command, podman.ManagedArguments(options.Application, options.ContainerRole)...)
	if options.Publish {
		command = append(command, publicationNetwork(options.Listen)...)
		command = append(command, "--publish", publishedPort(options.Listen, options.Port))
	}
	command = append(command, "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges", "--pids-limit", "2048", "--ulimit", "core=0:0", "--shm-size", "8g", "--tmpfs", "/tmp:rw,nosuid,nodev,size=8g", "--volume", layout.Application(options.Application)+":/data"+volumeSuffix)
	if options.Application == "comfyui" {
		command = append(command, "--volume", layout.ComfyModels()+":/content/models"+readOnly)
	}
	command = env(command, "ROCMLETE_PROFILE", options.Profile)
	command = env(command, "ROCMLETE_LISTEN", containerListen(options.Listen))
	command = env(command, "ROCMLETE_HOST_LISTEN", options.Listen)
	command = env(command, "ROCMLETE_PORT", options.Port)
	command = env(command, "ROCMLETE_KERNEL_POLICY", options.KernelPolicy)
	command = env(command, "ROCMLETE_DISABLE_BUNDLED_EXTENSIONS", boolInt(options.DisableBundledExtensions))
	if options.Application == "comfyui" {
		command = env(command, "ROCMLETE_MEMORY_POLICY", options.MemoryPolicy)
	}
	if options.NetworkNone {
		command = append(command, "--network", "none")
	}
	for _, value := range options.Environment {
		command = append(command, "--env", value)
	}
	if options.Profile != "cpu" {
		command = append(command, gpuDeviceArguments(options.RenderNodes)...)
	}
	if options.Unconfined {
		command = append(command, "--security-opt", "seccomp=unconfined")
	}
	if options.Detach {
		command = append(command, "--detach")
	}
	if options.KernelPolicy == "experimental" {
		command = append(command, "--env", "TORCH_ROCM_AOTRITON_ENABLE_EXPERIMENTAL=1", "--env", "TORCH_BLAS_PREFER_HIPBLASLT=1")
	}
	return append(command, append([]string{options.Image}, options.Arguments...)...)
}

func boolInt(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func validateWebOptions(options WebOptions) error {
	if options.Port <= 0 || options.Port > 65535 {
		return fmt.Errorf("invalid web port %d", options.Port)
	}
	return nil
}
