package identity

import "testing"

func TestDerivedIdentity(t *testing.T) {
	if got := Command("build", "llama-cpp"); got != "./rocmplete build llama-cpp" {
		t.Fatalf("command=%q", got)
	}
	if got := Image("llama-cpp:test"); got != "localhost/rocmplete:llama-cpp:test" {
		t.Fatalf("image=%q", got)
	}
	if got := Container("llama-cpp"); got != "rocmplete-llama-cpp" {
		t.Fatalf("container=%q", got)
	}
	if got := EnvironmentPrefix(); got != "ROCMLETE" {
		t.Fatalf("environment prefix=%q", got)
	}
	if got := Current().EnvPrefix; got != "ROCMLETE" {
		t.Fatalf("current environment prefix=%q", got)
	}
}

func TestEnvironmentPrefixFollowsCommandIdentity(t *testing.T) {
	originalCommand, originalPrefix := CommandName, EnvPrefix
	t.Cleanup(func() { CommandName, EnvPrefix = originalCommand, originalPrefix })
	CommandName, EnvPrefix = "new-name", ""
	if got := EnvironmentPrefix(); got != "NEW_NAME" {
		t.Fatalf("derived environment prefix=%q", got)
	}
	EnvPrefix = "EXPLICIT"
	if got := EnvironmentPrefix(); got != "EXPLICIT" {
		t.Fatalf("explicit environment prefix=%q", got)
	}
}
