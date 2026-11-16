package cli

import (
	"strings"
	"testing"

	"paracetamol/internal/identity"
	"paracetamol/internal/process"
)

func TestLlamaGuideQualifiesPreferredModelAndOffersSelection(t *testing.T) {
	runner := &commandRunner{run: func(command process.Command) process.Result {
		t.Fatalf("guide probed or started a workload: %v", command)
		return process.Result{}
	}}
	app, output, _ := testApp(t, runner)
	if err := app.commandGuide([]string{"llama-cpp"}); err != nil {
		t.Fatal(err)
	}
	for _, detail := range []string{
		"high-memory ROCm Strix Halo only", "fast SSD", "over 100 GiB", "Other hosts",
		identity.Command("content", "install", "llama-cpp"),
		identity.Command("content", "install", "llama-cpp", "qwen3.8-flash-next"),
		"--preset unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl-dflash2",
	} {
		if !strings.Contains(output.String(), detail) {
			t.Errorf("guide omits %q:\n%s", detail, output.String())
		}
	}
}
