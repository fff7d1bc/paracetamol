package runtime

import (
	"fmt"
	"path/filepath"

	"rocmplete/internal/config"
	"rocmplete/internal/podman"
	"rocmplete/internal/storage"
)

type DwarfStarOptions struct {
	Image        string
	Mode         string
	DataDir      string
	Model        string
	SupportModel string
	DSpark       bool
	RenderNodes  []string
	Profile      string
	Listen       string
	Port         int
	Context      int64
	OutputTokens int64
	Prompt       *string
	NoThinking   bool
	Detach       bool
	Interactive  bool
	Unconfined   bool
}

func DwarfStarCommand(options DwarfStarOptions, volumeSuffix string) ([]string, error) {
	if options.DSpark && options.SupportModel == "" {
		return nil, fmt.Errorf("DSpark requires one support GGUF")
	}
	if !options.DSpark && options.SupportModel != "" {
		return nil, fmt.Errorf("DSpark support GGUF requires DSpark mode")
	}
	modelRoot := filepath.Dir(options.Model)
	if options.SupportModel != "" && filepath.Dir(options.SupportModel) != modelRoot {
		return nil, fmt.Errorf("DwarfStar target and DSpark support GGUFs must share one directory")
	}
	layout := storage.Layout{Root: options.DataDir}
	readOnly := readOnlySharedSuffix(volumeSuffix)
	application, _ := config.ApplicationByID("dwarfstar")
	command := []string{"podman", "run", "--rm", "--userns", "keep-id", "--umask", podman.CurrentUmask(), "--name", application.ContainerName}
	command = append(command, podman.ManagedArguments("dwarfstar", "application")...)
	command = append(command, "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges", "--pids-limit", "2048", "--ulimit", "core=0:0", "--shm-size", "8g", "--tmpfs", "/tmp:rw,nosuid,nodev,size=1g", "--volume", layout.Application("dwarfstar")+":/data"+volumeSuffix, "--volume", modelRoot+":/content/models"+readOnly)
	command = env(command, "ROCMLETE_PROFILE", options.Profile)
	command = env(command, "ROCMLETE_DWARFSTAR_MODE", options.Mode)
	command = env(command, "ROCMLETE_DWARFSTAR_MODEL", "/content/models/"+filepath.Base(options.Model))
	command = env(command, "ROCMLETE_DWARFSTAR_DSPARK", boolInt(options.DSpark))
	command = env(command, "ROCMLETE_DWARFSTAR_CONTEXT", options.Context)
	command = env(command, "ROCMLETE_DWARFSTAR_OUTPUT_TOKENS", options.OutputTokens)
	command = env(command, "ROCMLETE_DWARFSTAR_NO_THINKING", boolInt(options.NoThinking))
	command = env(command, "ROCMLETE_LISTEN", containerListen(options.Listen))
	command = env(command, "ROCMLETE_HOST_LISTEN", options.Listen)
	command = env(command, "ROCMLETE_PORT", options.Port)
	if options.DSpark {
		command = env(command, "ROCMLETE_DWARFSTAR_DSPARK_MODEL", "/content/models/"+filepath.Base(options.SupportModel))
	}
	if options.Prompt != nil {
		command = env(command, "ROCMLETE_DWARFSTAR_PROMPT", *options.Prompt)
	}
	if options.Mode == "server" {
		command = append(command, publicationNetwork(options.Listen)...)
		command = append(command, "--publish", publishedPort(options.Listen, options.Port))
	} else {
		command = append(command, "--network", "none")
	}
	command = append(command, gpuDeviceArguments(options.RenderNodes)...)
	if options.Unconfined {
		command = append(command, "--security-opt", "seccomp=unconfined")
	}
	if options.Detach {
		command = append(command, "--detach")
	}
	if options.Interactive {
		command = append(command, "--interactive", "--tty")
	}
	return append(command, options.Image), nil
}
