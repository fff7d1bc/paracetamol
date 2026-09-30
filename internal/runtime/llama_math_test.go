package runtime

import (
	"strings"
	"testing"
)

func TestQwen4expMathPolicy(t *testing.T) {
	for _, tc := range []struct {
		math, backend, profile string
		nodes                  int
		valid                  bool
	}{
		{"default", "vulkan", "cpu", 0, true},
		{"default", "rocm", "rdna4", 2, true},
		{"mixed", "rocm", "auto", 1, true},
		{"mixed", "rocm", "strix-halo", 1, true},
		{"mixed", "vulkan", "strix-halo", 1, false},
		{"mixed", "rocm", "strix-halo", 2, false},
		{"mixed", "rocm", "rdna4", 1, false},
		{"mixed", "rocm", "cpu", 0, false},
		{"unknown", "rocm", "auto", 1, false},
	} {
		err := ValidateQwen4expMath(tc.math, tc.backend, tc.profile, make([]string, tc.nodes))
		if (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
	for _, mode := range []string{"", "default", "mixed"} {
		for _, router := range []string{"", "/tmp/models.ini"} {
			cmd, err := LlamaCommand(LlamaOptions{Profile: "auto", Backend: "rocm", Mode: "server", DataDir: "/data",
				RenderNodes: []string{"/dev/dri/renderD128"}, Qwen4expMath: mode, RouterPreset: router}, "")
			if err != nil {
				t.Fatal(err)
			}
			want := mode
			if want == "" {
				want = "default"
			}
			if !strings.Contains(strings.Join(cmd, " "), "PARACETAMOL_LLAMA_QWEN4EXP_MATH="+want) {
				t.Fatal(cmd)
			}
		}
	}
}
