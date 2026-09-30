package config

import "testing"

func TestQwen4expMathConfiguration(t *testing.T) {
	for _, value := range []string{"default", "mixed", "bf16", ""} {
		c, err := parseConfiguration([]byte("[gateway.llama-cpp]\nqwen4exp_math = \"" + value + "\"\n"))
		if err != nil {
			t.Fatal(err)
		}
		err = validateConfiguration(c, "fixture")
		if (err == nil) != (value == "default" || value == "mixed") {
			t.Fatalf("%q: %v", value, err)
		}
	}
	c, err := parseConfiguration(DefaultContents("/data"))
	if err != nil || c.Gateway.LlamaCPP.Qwen4expMath == nil || *c.Gateway.LlamaCPP.Qwen4expMath != "default" {
		t.Fatalf("config=%+v err=%v", c, err)
	}
	if _, err := parseConfiguration([]byte("[gateway.llama-cpp]\nqwen4exp_math = true\n")); err == nil {
		t.Fatal("unquoted mode accepted")
	}
}
