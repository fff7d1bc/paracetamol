package project

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Use the installed Go command, not the publication tests' fake compiler: even
// `go env` initializes telemetry before Make has selected a platform directory.
func TestMakeConfinesRealGoStateAndPreservesApplicationConfig(t *testing.T) {
	checkout, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	scratchParent := filepath.Join(checkout, "build", "go-state-tests")
	if err := os.MkdirAll(scratchParent, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(scratchParent, "checkout-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove isolated Go fixture: %v", err)
		}
	})
	for _, dir := range []string{"tools", "tools/llama-acceptance", "internal/gateway/assets", "tests", "home", "user-config"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Makefile", "paracetamol", "bin/paracetamol", "tools/go-test-exec.sh"} {
		data, err := os.ReadFile(filepath.Join(checkout, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		write(name, string(data))
		if strings.HasSuffix(name, "paracetamol") {
			if err := os.Chmod(filepath.Join(root, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("go.mod", "module fixture\n\ngo 1.26.0\n")
	write("main.go", `package main
import ("encoding/json"; "os")
func main() {
 value, set := os.LookupEnv("XDG_CONFIG_HOME")
 config, err := os.UserConfigDir()
 if err != nil { panic(err) }
 json.NewEncoder(os.Stdout).Encode(struct { Value string; Set bool; Config string }{value, set, config})
}
`)
	write("main_test.go", `package main
import ("os"; "testing")
func TestUserConfig(t *testing.T) {
 value, set := os.LookupEnv("XDG_CONFIG_HOME")
 if value != os.Getenv("FIXTURE_XDG_VALUE") || set != (os.Getenv("FIXTURE_XDG_SET") == "true") {
  t.Fatalf("test inherited isolated Go config: value=%q set=%t", value, set)
 }
 for _, key := range []string{"PARACETAMOL_TEST_XDG_CONFIG_HOME_SET", "PARACETAMOL_TEST_XDG_CONFIG_HOME"} {
  if _, present := os.LookupEnv(key); present { t.Errorf("test helper environment leaked: %s", key) }
 }
}
`)
	write("tests/test_fixture.py", "import unittest\n\nclass Fixture(unittest.TestCase):\n    def test_fixture(self):\n        pass\n")
	baseEnv := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(root, "home"),
		"GOPROXY=off", "GOSUMDB=off", "GO_SOURCE_ROOTS=main.go",
	}
	run := func(t *testing.T, env []string, program string, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(program, args...)
		cmd.Dir, cmd.Env = root, env
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", program, args, err, output)
		}
		return output
	}
	assertNoExternalGoState := func(t *testing.T) {
		t.Helper()
		for _, dir := range []string{"home", "user-config"} {
			entries, err := os.ReadDir(filepath.Join(root, dir))
			if err != nil || len(entries) != 0 {
				t.Errorf("Go wrote outside checkout build: %s entries=%v err=%v", dir, entries, err)
			}
		}
	}
	// A nonlocal override must not affect early installed-toolchain discovery.
	probeEnv := append(append([]string(nil), baseEnv...), "XDG_CONFIG_HOME="+filepath.Join(root, "user-config"))
	output := run(t, probeEnv, "make", "-s", "print-bin", "GOTOOLCHAIN=go1.invalid")
	bin := strings.TrimSpace(string(output))
	if !strings.HasPrefix(bin, filepath.Join(root, "build")+string(os.PathSeparator)) {
		t.Fatalf("print-bin escaped build: %q", output)
	}
	if _, err := os.Stat(filepath.Join(root, "build", "go-config", "go", "telemetry", "local")); err != nil {
		t.Fatalf("real Go probe did not use local telemetry config: %v", err)
	}
	assertNoExternalGoState(t)
	formatEnv := append(append([]string(nil), baseEnv...), "XDG_CONFIG_HOME="+filepath.Join(root, "build", "go-config"))
	run(t, formatEnv, "gofmt", "-w", "main.go", "main_test.go")
	run(t, probeEnv, "make", "-s", "check", "GO_PACKAGES=.")
	assertNoExternalGoState(t)

	for _, mode := range []string{"unset", "empty", "spaced"} {
		t.Run(mode, func(t *testing.T) {
			env := append([]string(nil), baseEnv...)
			value, set := "", mode != "unset"
			if mode == "spaced" {
				value = filepath.Join(root, "user-config", `space 'quote" $dollar`)
			}
			if set {
				env = append(env, "XDG_CONFIG_HOME="+value)
			}
			env = append(env, "FIXTURE_XDG_VALUE="+value, "FIXTURE_XDG_SET="+map[bool]string{true: "true", false: "false"}[set])
			run(t, env, "make", "-s", "test", "GO_PACKAGES=.", "GOTOOLCHAIN=auto")
			for _, launcher := range []string{"./paracetamol", "./bin/paracetamol"} {
				var got struct {
					Value  string
					Set    bool
					Config string
				}
				output := run(t, env, launcher)
				if err := json.Unmarshal(output, &got); err != nil {
					t.Fatalf("launcher output=%q err=%v", output, err)
				}
				wantConfig := value
				if wantConfig == "" {
					wantConfig = filepath.Join(root, "home", ".config")
				}
				if got.Value != value || got.Set != set || got.Config != wantConfig {
					t.Errorf("launcher config=%+v want value=%q set=%t config=%q", got, value, set, wantConfig)
				}
			}
			assertNoExternalGoState(t)
		})
	}
	// Clean intentionally has no platform probes, even without HOST_GO* overrides.
	run(t, probeEnv, "make", "-s", "clean")
	run(t, probeEnv, "make", "-s", "clean")
	if _, err := os.Lstat(filepath.Join(root, "build")); !os.IsNotExist(err) {
		t.Errorf("clean-only invocation recreated Go state: %v", err)
	}
	assertNoExternalGoState(t)
	if err := os.Symlink(filepath.Join(root, "user-config"), filepath.Join(root, "build")); err != nil {
		t.Fatal(err)
	}
	clean := exec.Command("make", "-s", "clean")
	clean.Dir, clean.Env = root, probeEnv
	if output, err := clean.CombinedOutput(); err == nil || !strings.Contains(string(output), "refuses a symlink") {
		t.Errorf("clean accepted symlink or failed unexpectedly: %v\n%s", err, output)
	}
	assertNoExternalGoState(t)
}
