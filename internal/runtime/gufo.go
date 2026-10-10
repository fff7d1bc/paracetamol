package runtime

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"paracetamol/internal/config"
	"paracetamol/internal/podman"
	"paracetamol/internal/storage"
)

type GufoOptions struct {
	Image, DataDir, Model, ModelID, Profile, Listen, Reasoning string
	RenderNodes                                                []string
	ModelFiles                                                 []string
	Context, OutputTokens, CacheRAMBytes                       int64
	Port                                                       int
	Detach, Unconfined, DynamicHostPort                        bool
	ContainerName, ContainerRole                               string
}

func GufoCommand(options GufoOptions, volumeSuffix string) ([]string, error) {
	application, _ := config.ApplicationByID("gufo")
	if options.Profile != "auto" && options.Profile != "strix-halo" || len(options.RenderNodes) != 1 {
		return nil, fmt.Errorf("Gufo requires Strix Halo and exactly one render node")
	}
	if options.Context < 4096 || options.Context > 262144 || options.Context%1024 != 0 || options.OutputTokens < 1 || options.OutputTokens >= options.Context {
		return nil, fmt.Errorf("invalid Gufo context or output limit")
	}
	if options.CacheRAMBytes < 0 || options.CacheRAMBytes > 32<<30 {
		return nil, fmt.Errorf("Gufo RAM cache must be between 0 and 34359738368 bytes")
	}
	if options.Reasoning != "off" && options.Reasoning != "medium" && options.Reasoning != "xhigh" {
		return nil, fmt.Errorf("Gufo reasoning must be off, medium, or xhigh")
	}
	if !filepath.IsAbs(options.Model) || !strings.HasSuffix(options.Model, ".gguf") || options.ModelID == "" {
		return nil, fmt.Errorf("Gufo requires an absolute GGUF path and model identifier")
	}
	seen := map[string]bool{}
	for _, path := range options.ModelFiles {
		if !filepath.IsAbs(path) || filepath.Dir(path) != filepath.Dir(options.Model) || !strings.HasSuffix(path, ".gguf") || seen[path] {
			return nil, fmt.Errorf("Gufo model files must be distinct GGUF shards in the selected model directory")
		}
		seen[path] = true
	}
	if !seen[options.Model] {
		return nil, fmt.Errorf("Gufo model files must include the selected model")
	}
	if options.ContainerName == "" {
		options.ContainerName = application.ContainerName
	}
	if options.ContainerRole == "" {
		options.ContainerRole = "application"
	}
	if options.Listen == "" {
		options.Listen = config.DefaultListen
	}
	if ip := net.ParseIP(options.Listen); ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("Gufo direct publication requires an IPv4 address; use the gateway for IPv6")
	}
	if options.Port == 0 {
		options.Port = application.Port
	}
	if options.Port < 1 || options.Port > 65535 {
		return nil, fmt.Errorf("invalid Gufo port")
	}
	layout := storage.Layout{Root: options.DataDir}
	command := []string{"podman", "run", "--rm", "--userns", "keep-id", "--umask", podman.CurrentUmask(), "--name", options.ContainerName}
	command = append(command, podman.ManagedArguments("gufo", options.ContainerRole)...)
	command = append(command, "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges", "--pids-limit", "2048", "--ulimit", "core=0:0", "--shm-size", "8g", "--tmpfs", "/tmp:rw,nosuid,nodev,size=1g",
		"--volume", layout.Application("gufo")+":/data"+volumeSuffix)
	// Gufo discovers sibling projectors and draft models automatically. Mount
	// only receipt-verified bundle files, not unreviewed neighbours on the host.
	for _, path := range options.ModelFiles {
		command = append(command, "--volume", path+":/content/models/"+filepath.Base(path)+readOnlySharedSuffix(volumeSuffix))
	}
	for _, setting := range [][2]string{
		{"PARACETAMOL_PROFILE", options.Profile}, {"PARACETAMOL_GUFO_MODEL", "/content/models/" + filepath.Base(options.Model)},
		{"PARACETAMOL_GUFO_MODEL_ID", options.ModelID}, {"PARACETAMOL_GUFO_CONTEXT", fmt.Sprint(options.Context)},
		{"PARACETAMOL_GUFO_OUTPUT_TOKENS", fmt.Sprint(options.OutputTokens)}, {"PARACETAMOL_GUFO_CACHE_RAM_BYTES", fmt.Sprint(options.CacheRAMBytes)},
		{"PARACETAMOL_GUFO_REASONING", options.Reasoning}, {"PARACETAMOL_LISTEN", "0.0.0.0"},
		{"PARACETAMOL_PORT", fmt.Sprint(options.Port)},
	} {
		command = env(command, setting[0], setting[1])
	}
	command = append(command, publicationNetwork(options.Listen)...)
	publication := publishedPort(options.Listen, options.Port)
	if options.DynamicHostPort {
		publication = dynamicallyPublishedPort(options.Listen, options.Port)
	}
	command = append(command, "--publish", publication)
	command = append(command, gpuDeviceArguments(options.RenderNodes)...)
	if options.Unconfined {
		command = append(command, "--security-opt", "seccomp=unconfined")
	}
	if options.Detach {
		command = append(command, "--detach")
	}
	return append(command, options.Image), nil
}
