package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"rocmplete/internal/config"
	"rocmplete/internal/platform"
	"rocmplete/internal/podman"
	"rocmplete/internal/storage"
)

type LlamaOptions struct {
	Image                        string
	Profile                      string
	Mode                         string
	DataDir                      string
	Backend                      string
	SourceRevision               string
	Model                        string
	ManagedModel                 string
	ManagedDraft                 string
	SpeculativeType              string
	DraftTokens                  int64
	ContextOverrideArchitectures []string
	Jinja                        bool
	ReasoningPreserve            bool
	ChatTemplate                 string
	SamplingDefaults             map[string]any
	ProfileFlashAttention        map[string]string
	ProfileKVCache               map[string]string
	RouterPreset                 string
	ModelsMax                    int
	RenderNodes                  []string
	Listen                       string
	Port                         int
	Context                      int64
	Prompt                       *string
	APIKeyFile                   string
	Detach                       bool
	Interactive                  bool
	Unconfined                   bool
	ContainerName                string
	ContainerRole                string
	AutoRemove                   bool
	Arguments                    []string
	Environment                  []string
}

type LlamaBenchmarkOptions struct {
	Image            string
	Profile          string
	DataDir          string
	Backend          string
	Model            string
	ManagedModel     string
	RenderNodes      []string
	Repetitions      int
	PromptTokens     int
	GenerationTokens int
	ContextDepth     int
	BatchSize        int
	UBatchSize       int
	CacheTypeK       string
	CacheTypeV       string
	FlashAttention   string
	Unconfined       bool
}

func LlamaCommand(options LlamaOptions, volumeSuffix string) ([]string, error) {
	layout := storage.Layout{Root: options.DataDir}
	readOnly := readOnlySharedSuffix(volumeSuffix)
	application, _ := config.ApplicationByID("llama-cpp")
	if options.ContainerName == "" {
		options.ContainerName = application.ContainerName
	}
	if options.ContainerRole == "" {
		options.ContainerRole = "application"
	}
	modelRoot := layout.LlamaModels()
	containerModel := ""
	if options.Model != "" {
		modelRoot = filepath.Dir(options.Model)
		containerModel = "/content/models/" + filepath.Base(options.Model)
	} else if options.ManagedModel != "" {
		containerModel = "/content/models/" + options.ManagedModel
	}
	command := []string{"podman", "run"}
	if options.AutoRemove {
		command = append(command, "--rm")
	}
	command = append(command, "--userns", "keep-id", "--umask", podman.CurrentUmask(), "--name", options.ContainerName)
	command = append(command, podman.ManagedArguments("llama-cpp", options.ContainerRole)...)
	command = append(command, "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges", "--pids-limit", "2048", "--ulimit", "core=0:0", "--shm-size", "8g", "--tmpfs", "/tmp:rw,nosuid,nodev,size=1g", "--volume", layout.Application("llama-cpp")+":/data"+volumeSuffix, "--volume", modelRoot+":/content/models"+readOnly)
	command = env(command, "ROCMLETE_PROFILE", options.Profile)
	command = env(command, "ROCMLETE_LLAMA_BACKEND", options.Backend)
	command = env(command, "ROCMLETE_SOURCE_REVISION", options.SourceRevision)
	command = env(command, "ROCMLETE_LLAMA_MODE", options.Mode)
	command = env(command, "ROCMLETE_LLAMA_MODEL", containerModel)
	draft := ""
	if options.ManagedDraft != "" {
		draft = "/content/models/" + options.ManagedDraft
	}
	command = env(command, "ROCMLETE_LLAMA_DRAFT_MODEL", draft)
	command = env(command, "ROCMLETE_LLAMA_SPECULATIVE_TYPE", options.SpeculativeType)
	command = env(command, "ROCMLETE_LLAMA_DRAFT_TOKENS", options.DraftTokens)
	var overrides []string
	for _, architecture := range options.ContextOverrideArchitectures {
		overrides = append(overrides, fmt.Sprintf("%s.context_length=int:%d", architecture, options.Context))
	}
	command = env(command, "ROCMLETE_LLAMA_CONTEXT_OVERRIDE", strings.Join(overrides, ","))
	command = env(command, "ROCMLETE_LLAMA_JINJA", boolInt(options.Jinja))
	command = env(command, "ROCMLETE_LLAMA_REASONING_PRESERVE", boolInt(options.ReasoningPreserve))
	command = env(command, "ROCMLETE_LLAMA_CHAT_TEMPLATE", options.ChatTemplate)
	sampling := ""
	if len(options.SamplingDefaults) > 0 {
		encoded, err := json.Marshal(options.SamplingDefaults)
		if err != nil {
			return nil, fmt.Errorf("encode sampling defaults: %w", err)
		}
		sampling = string(encoded)
	}
	command = env(command, "ROCMLETE_LLAMA_SAMPLING_DEFAULTS", sampling)
	command = env(command, "ROCMLETE_LISTEN", containerListen(options.Listen))
	command = env(command, "ROCMLETE_HOST_LISTEN", options.Listen)
	command = env(command, "ROCMLETE_PORT", options.Port)
	command = env(command, "ROCMLETE_GPU_COUNT", len(options.RenderNodes))
	command = env(command, "ROCMLETE_RENDER_NODES", strings.Join(options.RenderNodes, ","))
	for _, profile := range platform.ProfileIDs() {
		key := strings.ToUpper(strings.ReplaceAll(profile, "-", "_"))
		command = env(command, "ROCMLETE_LLAMA_FLASH_ATTN_"+key, options.ProfileFlashAttention[profile])
		command = env(command, "ROCMLETE_LLAMA_KV_CACHE_"+key, options.ProfileKVCache[profile])
	}
	if options.Mode != "server" {
		command = append(command, "--network", "none")
	}
	if options.RouterPreset != "" {
		command = append(command, "--volume", options.RouterPreset+":/run/rocmplete/models.ini"+readOnly)
		command = env(command, "ROCMLETE_LLAMA_ROUTER", 1)
		command = env(command, "ROCMLETE_LLAMA_MODELS_MAX", options.ModelsMax)
	}
	if options.Mode == "server" {
		command = append(command, publicationNetwork(options.Listen)...)
		command = append(command, "--publish", publishedPort(options.Listen, options.Port))
	}
	if options.Profile != "cpu" {
		command = append(command, gpuDeviceArguments(options.RenderNodes)...)
	}
	if options.APIKeyFile != "" {
		command = append(command, "--volume", options.APIKeyFile+":/run/secrets/llama-api-key"+readOnly)
	}
	if options.Unconfined {
		command = append(command, "--security-opt", "seccomp=unconfined")
	}
	for _, value := range options.Environment {
		command = append(command, "--env", value)
	}
	if options.Detach {
		command = append(command, "--detach")
	}
	if options.Interactive {
		command = append(command, "--interactive", "--tty")
	}
	command = append(command, options.Image)
	if options.Context > 0 {
		command = append(command, "--ctx-size", fmt.Sprint(options.Context))
	}
	if options.APIKeyFile != "" {
		command = append(command, "--api-key-file", "/run/secrets/llama-api-key")
	}
	if options.Prompt != nil {
		command = append(command, "--prompt", *options.Prompt, "--single-turn")
	}
	command = append(command, options.Arguments...)
	return command, nil
}

// LlamaBenchmarkCommand deliberately bypasses the interactive/server policy
// surface and exposes only the bounded llama-bench measurements maintained by
// the control plane.
func LlamaBenchmarkCommand(options LlamaBenchmarkOptions, volumeSuffix string) []string {
	layout := storage.Layout{Root: options.DataDir}
	readOnly := readOnlySharedSuffix(volumeSuffix)
	modelRoot := layout.LlamaModels()
	containerModel := "/content/models/" + options.ManagedModel
	if options.Model != "" {
		modelRoot = filepath.Dir(options.Model)
		containerModel = "/content/models/" + filepath.Base(options.Model)
	}
	command := []string{
		"podman", "run", "--rm", "--userns", "keep-id", "--umask", podman.CurrentUmask(),
		"--name", "rocmplete-llama-cpp-benchmark",
	}
	command = append(command, podman.ManagedArguments("llama-cpp", "benchmark")...)
	command = append(command,
		"--network", "none", "--read-only", "--cap-drop", "all",
		"--security-opt", "no-new-privileges", "--pids-limit", "2048",
		"--ulimit", "core=0:0", "--shm-size", "8g",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=1g",
		"--volume", modelRoot+":/content/models"+readOnly,
		"--env", "HOME=/tmp",
	)
	command = env(command, "ROCMLETE_PROFILE", options.Profile)
	command = env(command, "ROCMLETE_LLAMA_BACKEND", options.Backend)
	command = env(command, "ROCMLETE_LLAMA_MODE", "bench")
	command = env(command, "ROCMLETE_LLAMA_MODEL", containerModel)
	command = env(command, "ROCMLETE_GPU_COUNT", len(options.RenderNodes))
	if options.Profile != "cpu" {
		command = append(command, gpuDeviceArguments(options.RenderNodes)...)
	}
	if options.Unconfined {
		command = append(command, "--security-opt", "seccomp=unconfined")
	}
	return append(command, options.Image,
		"--repetitions", fmt.Sprint(options.Repetitions),
		"--n-prompt", fmt.Sprint(options.PromptTokens),
		"--n-gen", fmt.Sprint(options.GenerationTokens),
		"--n-depth", fmt.Sprint(options.ContextDepth),
		"--batch-size", fmt.Sprint(options.BatchSize),
		"--ubatch-size", fmt.Sprint(options.UBatchSize),
		"--cache-type-k", options.CacheTypeK,
		"--cache-type-v", options.CacheTypeV,
		"--flash-attn", options.FlashAttention,
		"--output", "json", "--progress",
	)
}
