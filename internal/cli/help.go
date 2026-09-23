package cli

import (
	"strings"

	"paracetamol/internal/identity"
)

var metavarByFlag = map[string]string{
	"application":            "APPLICATION",
	"api-key-file":           "PATH",
	"as":                     "TYPE",
	"backend":                "BACKEND",
	"batch-size":             "TOKENS",
	"cache-mode":             "MODE",
	"cache-type-k":           "TYPE",
	"cache-type-v":           "TYPE",
	"context":                "TOKENS",
	"context-depth":          "TOKENS",
	"data-dir":               "PATH",
	"draft-backend-sampling": "POLICY",
	"draft-depth":            "TOKENS",
	"draft-probability-min":  "PROBABILITY",
	"family":                 "FAMILY",
	"file":                   "FILE",
	"flash-attn":             "POLICY",
	"from-file":              "PATH",
	"gateway-url":            "URL",
	"gateway-api-key-file":   "PATH",
	"generation-tokens":      "TOKENS",
	"image":                  "TAG",
	"image-tag":              "TAG",
	"include":                "BUNDLE",
	"kernel-policy":          "POLICY",
	"listen":                 "ADDRESS",
	"local-mirror":           "PATH",
	"memory-policy":          "POLICY",
	"model":                  "MODEL",
	"models-max":             "COUNT",
	"output":                 "PATH",
	"output-tokens":          "TOKENS",
	"poll":                   "PERCENT",
	"port":                   "PORT",
	"preset":                 "PRESET",
	"profile":                "PROFILE",
	"prompt":                 "TEXT",
	"prompt-tokens":          "TOKENS",
	"render-node":            "PATH",
	"repetitions":            "COUNT",
	"report-format":          "FORMAT",
	"requests":               "COUNT",
	"resume":                 "PATH",
	"runs":                   "COUNT",
	"save-pack":              "PATH",
	"scan":                   "PATH",
	"seed":                   "INTEGER",
	"startup-timeout":        "DURATION",
	"tail":                   "LINES",
	"task":                   "TASK",
	"thinking":               "LEVEL",
	"ubatch-size":            "TOKENS",
	"version":                "ID",
}

func flagMetavar(name string) string {
	if value := metavarByFlag[name]; value != "" {
		return value
	}
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// Command examples are tokenized so product identity changes continue to
// produce copyable commands rather than baking one executable name into help.
var commandExampleArguments = map[string][][]string{
	"config init": {{"config", "init"}, {"config", "init", "-c", "configs/aion.toml"}},
	"build": {
		{"build", "all"},
		{"build", "llama-cpp"},
		{"build", "comfyui", "--no-layer-cache"},
	},
	"guide":       {{"guide", "llama-cpp"}, {"guide", "dwarfstar"}},
	"doctor":      {{"doctor"}, {"doctor", "--render-node", "/dev/dri/renderD128"}},
	"acceptance":  {{"acceptance", "--dry-run"}, {"acceptance", "--application", "llama-cpp"}},
	"status":      {{"status"}, {"status", "llama-cpp"}, {"status", "gateway"}, {"status", "gateway", "--requests", "10"}},
	"run comfyui": {{"run", "comfyui"}, {"run", "comfyui", "--", "--enable-manager"}, {"run", "comfyui", "--listen", "127.0.0.1", "--detach"}},
	"run llama-cpp server": {
		{"run", "llama-cpp", "server", "--preset", "qwen3.8-27b-mtp-ud-q8-k-xl"},
		{"run", "llama-cpp", "server", "--router", "--models-max", "2"},
		{"run", "llama-cpp", "server", "--model", "/path/to/model.gguf"},
	},
	"run llama-cpp cli": {
		{"run", "llama-cpp", "cli", "--model", "/path/to/model.gguf"},
		{"run", "llama-cpp", "cli", "--preset", "qwen3.8-27b-mtp-ud-q8-k-xl", "--prompt", "Hello"},
	},
	"run dwarfstar server":            {{"run", "dwarfstar", "server"}, {"run", "dwarfstar", "server", "--dspark"}},
	"run dwarfstar cli":               {{"run", "dwarfstar", "cli"}, {"run", "dwarfstar", "cli", "--no-thinking", "--prompt", "Say hello"}},
	"run gateway":                     {{"run", "gateway"}, {"run", "gateway", "-a", "llama-cpp", "-a", "dwarfstar"}, {"run", "gateway", "--port", "18080"}},
	"shell":                           {{"shell", "comfyui"}, {"shell", "llama-cpp"}},
	"logs":                            {{"logs", "llama-cpp", "--follow"}, {"logs", "comfyui", "--all"}},
	"stop":                            {{"stop", "gateway"}, {"stop", "llama-cpp"}, {"stop", "all"}},
	"content list":                    {{"content", "list"}, {"content", "list", "models", "--details"}, {"content", "list", "models", "--scan", "/path/to/ggufs"}},
	"content status":                  {{"content", "status", "llama-cpp", "qwen3.8"}, {"content", "status", "family", "qwen", "--details"}},
	"content import":                  {{"content", "import"}, {"content", "import", "https://huggingface.co/OWNER/REPOSITORY"}},
	"content workflows list":          {{"content", "workflows", "list"}},
	"content workflows status":        {{"content", "workflows", "status"}, {"content", "workflows", "status", "WORKFLOW"}},
	"content workflows install":       {{"content", "workflows", "install", "WORKFLOW"}},
	"agent install":                   {{"agent", "install", "pi"}},
	"agent run pi":                    {{"agent", "run", "pi"}, {"agent", "run", "pi", "--no-sandbox", "--", "--help"}},
	"agent run maki":                  {{"agent", "run", "maki"}, {"agent", "run", "maki", "--no-sandbox", "--", "--help"}},
	"images export":                   {{"images", "export", "all", "--output", "/backup/paracetamol-images.tar"}},
	"images import":                   {{"images", "import", "/backup/paracetamol-images.tar", "--dry-run"}},
	"cleanup containers":              {{"cleanup", "containers"}, {"cleanup", "containers", "llama-cpp"}},
	"cleanup caches":                  {{"cleanup", "caches"}},
	"cleanup build-cache":             {{"cleanup", "build-cache"}},
	"cleanup downloads":               {{"cleanup", "downloads", "--yes", "--non-interactive"}},
	"cleanup images":                  {{"cleanup", "images"}, {"cleanup", "images", "llama-cpp"}, {"cleanup", "images", "--image-tag", "IMAGE_TAG"}},
	"cleanup data":                    {{"cleanup", "data"}},
	"benchmark comfyui run":           {{"benchmark", "comfyui", "run", "BUNDLE", "--dry-run"}},
	"benchmark comfyui suite":         {{"benchmark", "comfyui", "suite", "--family", "qwen", "--dry-run"}},
	"benchmark agent":                 {{"benchmark", "agent", "--preset", "qwen3.8-27b-mtp-ud-q8-k-xl", "--thinking", "medium", "--dry-run"}},
	"benchmark llama-cpp throughput":  {{"benchmark", "llama-cpp", "throughput", "--preset", "qwen3-0.6b-q8-0", "--dry-run"}},
	"benchmark llama-cpp speculative": {{"benchmark", "llama-cpp", "speculative", "--preset", "qwen3.8-27b-mtp-ud-q8-k-xl", "--dry-run"}},
	"benchmark report":                {{"benchmark", "report", "SUITE.json"}},
}

func examplesFor(name string) []string {
	arguments := commandExampleArguments[name]
	result := make([]string, 0, len(arguments))
	for _, example := range arguments {
		result = append(result, identity.Command(example...))
	}
	return result
}

var groupExampleArguments = []struct {
	name     string
	examples [][]string
}{
	{"run llama-cpp", [][]string{{"run", "llama-cpp", "server", "--router"}, {"run", "llama-cpp", "cli", "--model", "/path/to/model.gguf"}}},
	{"run dwarfstar", [][]string{{"run", "dwarfstar", "server"}, {"run", "dwarfstar", "cli"}}},
	{"agent run", [][]string{{"agent", "run", "pi"}, {"agent", "run", "maki"}}},
	{"benchmark comfyui", [][]string{{"benchmark", "comfyui", "run", "BUNDLE", "--dry-run"}, {"benchmark", "comfyui", "suite", "--dry-run"}}},
	{"benchmark llama-cpp", [][]string{{"benchmark", "llama-cpp", "throughput", "--preset", "PRESET", "--dry-run"}, {"benchmark", "llama-cpp", "speculative", "--preset", "PRESET", "--dry-run"}}},
	{"content workflows", [][]string{{"content", "workflows", "list"}, {"content", "workflows", "status"}}},
	{"content", [][]string{{"content", "list"}, {"content", "install"}, {"content", "status"}}},
	{"images", [][]string{{"images", "export", "all", "--output", "/backup/paracetamol-images.tar"}, {"images", "import", "/backup/paracetamol-images.tar", "--dry-run"}}},
	{"cleanup", [][]string{{"cleanup", "containers"}, {"cleanup", "downloads"}, {"cleanup", "images"}}},
	{"agent", [][]string{{"agent", "install", "pi"}, {"agent", "run", "pi"}}},
	{"benchmark", [][]string{{"benchmark", "agent", "--list-tasks"}, {"benchmark", "llama-cpp", "throughput", "--preset", "PRESET", "--dry-run"}}},
	{"run", [][]string{{"run", "comfyui"}, {"run", "llama-cpp", "server", "--router"}, {"run", "gateway"}}},
}

func examplesForSynopsis(synopsis string) []string {
	for _, group := range groupExampleArguments {
		prefix := "Usage: " + identity.Command(strings.Fields(group.name)...)
		if synopsis == prefix || strings.HasPrefix(synopsis, prefix+" ") {
			result := make([]string, 0, len(group.examples))
			for _, example := range group.examples {
				result = append(result, identity.Command(example...))
			}
			return result
		}
	}
	return nil
}
