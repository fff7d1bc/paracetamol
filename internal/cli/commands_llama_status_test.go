package cli

import (
	"strings"
	"testing"

	"paracetamol/internal/catalog"
)

const validLlamaRuntimeReport = `schema=1
mode=server
profile=strix-halo
backend=rocm
device=AMD Radeon 8060S Graphics
backend_devices=0
architecture=gfx1151
gpu_count=1
router=0
models_max=2
context=262144
listen=0.0.0.0
host_listen=127.0.0.1
port=8080
api_key=0
unified_memory=1
vulkan_f16_kv_contiguize=0
`

func TestParseLlamaRuntimeReport(t *testing.T) {
	parsed, err := parseLlamaRuntimeReport(validLlamaRuntimeReport)
	if err != nil {
		t.Fatal(err)
	}
	if parsed["architecture"] != "gfx1151" || parsed["context"] != "262144" {
		t.Fatalf("unexpected report: %#v", parsed)
	}
}

func TestParseLlamaRuntimeReportRejectsUnknownAndDuplicateKeys(t *testing.T) {
	for _, contents := range []string{
		validLlamaRuntimeReport + "surprise=value\n",
		strings.Replace(validLlamaRuntimeReport, "context=262144", "context=\ncontext=262144", 1),
		strings.Replace(validLlamaRuntimeReport, "host_listen=127.0.0.1", "host_listen=aion.local", 1),
	} {
		if _, err := parseLlamaRuntimeReport(contents); err == nil {
			t.Fatalf("invalid report accepted:\n%s", contents)
		}
	}
}

func TestParseRouterSnapshot(t *testing.T) {
	parsed, err := parseRouterSnapshot("version = 1\n\n[qwen]\nmodel = /content/models/qwen.gguf\nc = 131072\n")
	if err != nil {
		t.Fatal(err)
	}
	if parsed["qwen"]["c"] != "131072" {
		t.Fatalf("unexpected router snapshot: %#v", parsed)
	}
	for _, invalid := range []string{"version = 2\n", "version = 1\nmodel = missing-section\n", "version = 1\n[qwen]\nc = 1\nc = 2\n", "version = 1\n[Qwen 3.8]\nc = 1\n"} {
		if _, err := parseRouterSnapshot(invalid); err == nil {
			t.Fatalf("invalid router snapshot accepted: %q", invalid)
		}
	}
}

func TestParseLlamaProcessCommandRedactsSecretPath(t *testing.T) {
	parsed, err := parseLlamaProcessCommand([]byte("/usr/local/bin/llama-server\x00--api-key-file\x00/run/secrets/key\x00--api-key=secret\x00--port\x008080\x00"))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(parsed, " ")
	if strings.Contains(joined, "/run/secrets/key") || strings.Contains(joined, "secret") || !strings.Contains(joined, "API_KEY_FILE") {
		t.Fatalf("secret path was not redacted: %q", joined)
	}
	if _, err := parseLlamaProcessCommand([]byte{0xff}); err == nil {
		t.Fatal("invalid UTF-8 process command was accepted")
	}
}

func TestLlamaModelLoadStatusUsesResidentStrixDefault(t *testing.T) {
	if got := llamaModelLoadStatus("", "strix-halo"); got != "resident" {
		t.Fatalf("Strix Halo default = %q", got)
	}
	if got := llamaModelLoadStatus("mmap-lazy-token-embedding", "strix-halo"); got != "mmap; lazy per-layer token embedding" {
		t.Fatalf("lazy policy = %q", got)
	}
	if got := llamaModelLoadStatus("", "rdna4"); got != "llama.cpp default" {
		t.Fatalf("RDNA 4 default = %q", got)
	}
}

func TestLlamaStatusReportsExplicitReasoningHistoryPolicy(t *testing.T) {
	for _, test := range []struct {
		name    string
		section map[string]string
		env     map[string]string
		want    string
	}{
		{"router on", map[string]string{"reasoning-preserve": "true"}, nil, "preserved across turns when supported"},
		{"router off", map[string]string{"reasoning-preserve": "false"}, nil, "earlier reasoning preservation disabled"},
		{"direct on", nil, map[string]string{"PARACETAMOL_LLAMA_REASONING_PRESERVE": "1"}, "preserved across turns when supported"},
		{"direct off", nil, map[string]string{"PARACETAMOL_LLAMA_REASONING_PRESERVE": "0"}, "earlier reasoning preservation disabled"},
		{"unknown", nil, nil, "llama.cpp default"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := llamaModelStatusRows(catalog.Catalog{}, nil, test.section, test.env, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row[0] == "Reasoning history" {
					if row[1] != test.want {
						t.Fatalf("history = %q, want %q", row[1], test.want)
					}
					return
				}
			}
			t.Fatal("reasoning-history row missing")
		})
	}
}

func TestDirectLlamaPresetIgnoresBackendIncompatibleMatch(t *testing.T) {
	managed := catalog.Catalog{
		Artifacts: map[string]catalog.Artifact{
			"model": {ID: "model", Destination: "model.gguf"},
		},
		LlamaPresets: map[string]catalog.LlamaPreset{
			"vulkan-only": {ID: "vulkan-only", Artifact: "model", Backends: []string{"vulkan"}},
		},
	}
	environment := map[string]string{
		"PARACETAMOL_LLAMA_MODEL":        "/content/models/model.gguf",
		"PARACETAMOL_LLAMA_DRAFT_TOKENS": "0",
	}
	if preset, err := directLlamaPreset(managed, environment, "rocm", ""); err != nil || preset != nil {
		t.Fatalf("ROCm match preset=%#v err=%v", preset, err)
	}
	preset, err := directLlamaPreset(managed, environment, "vulkan", "")
	if err != nil || preset == nil || preset.ID != "vulkan-only" {
		t.Fatalf("Vulkan match preset=%#v err=%v", preset, err)
	}
}
