package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the actual recipes with a fake compiler in an isolated checkout.
// A successful final shell command must not hide an earlier build failure or
// publish the partial file that a failed compiler can leave behind.
func TestMakePublishesOnlySuccessfulBuilds(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Fatal("make is required for checkout build tests")
	}
	for _, target := range []string{"build", "static"} {
		for _, failure := range []string{"none", "compile", "empty", "mktemp", "chmod", "mv", "INT", "TERM", "HUP"} {
			t.Run(target+"/"+failure, func(t *testing.T) {
				testMakePublication(t, makePath, makefile, target, failure, true)
			})
		}
		t.Run(target+"/failed-first-build", func(t *testing.T) {
			testMakePublication(t, makePath, makefile, target, "compile", false)
		})
	}
}

func testMakePublication(t *testing.T, makePath string, makefile []byte, target, failure string, existing bool) {
	t.Helper()
	root := t.TempDir()
	fakeBin := filepath.Join(root, "fake-bin")
	outputDir := filepath.Join(root, "build", "linux-amd64", "bin")
	for _, dir := range []string{fakeBin, outputDir, filepath.Join(root, "internal/gateway/assets")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), data, mode); err != nil {
			t.Fatal(err)
		}
		// File creation honours the caller's umask. Set the fixture's exact
		// mode so the preservation check also works with a restrictive umask.
		if err := os.Chmod(filepath.Join(root, path), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("Makefile", makefile, 0o644)
	write("go.mod", []byte("module fixture\n\ngo 1.26.0\n"), 0o644)
	write("main.go", []byte("package main\nfunc main() {}\n"), 0o644)
	write("internal/gateway/assets/gateway.css", []byte("body {}\n"), 0o644)
	write("fake-bin/go", []byte(`#!/bin/sh
set -eu
case "$1/$2" in
    env/GOOS) printf 'linux\n'; exit 0 ;;
    env/GOARCH) printf 'amd64\n'; exit 0 ;;
    env/CGO_ENABLED) printf '1\n'; exit 0 ;;
    build/*) ;;
    *) exit 91 ;;
esac
while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then
        shift
        output=$1
        break
    fi
    shift
done
if [ "$BUILD_TEST_FAILURE" = empty ]; then exit 0; fi
printf 'new executable fixture\n' > "$output"
case "$BUILD_TEST_FAILURE" in
    compile) exit 42 ;;
    INT|TERM|HUP) kill -s "$BUILD_TEST_FAILURE" "$PPID" ;;
esac
`), 0o755)
	if failure == "mktemp" || failure == "chmod" || failure == "mv" {
		write("fake-bin/"+failure, []byte("#!/bin/sh\nexit 43\n"), 0o755)
	}
	name := "paracetamol"
	if target == "static" {
		name += "-static"
	}
	destination := filepath.Join(outputDir, name)
	const previous = "previous executable fixture\n"
	if existing {
		write(filepath.Join("build", "linux-amd64", "bin", name), []byte(previous), 0o755)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, makePath, "--no-print-directory", "-B", target, "GO_SOURCE_ROOTS=main.go")
	cmd.Dir = root
	// Do not inherit caller-specific make overrides or a developer's build
	// identity. All output and subprocess boundaries belong to this fixture.
	cmd.Env = []string{"PATH=" + fakeBin + ":" + os.Getenv("PATH"), "BUILD_TEST_FAILURE=" + failure}
	output, buildErr := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("build did not finish: %v\n%s", ctx.Err(), output)
	}
	if (failure == "none") != (buildErr == nil) {
		t.Fatalf("failure=%s build error=%v\n%s", failure, buildErr, output)
	}
	data, err := os.ReadFile(destination)
	if failure != "none" && !existing {
		if !os.IsNotExist(err) {
			t.Fatalf("failed first build published %q, err=%v", data, err)
		}
	} else {
		want := previous
		if failure == "none" {
			want = "new executable fixture\n"
		}
		if err != nil || string(data) != want {
			t.Fatalf("executable=%q want=%q err=%v\n%s", data, want, err, output)
		}
		info, err := os.Stat(destination)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("executable permissions changed: info=%v err=%v", info, err)
		}
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Errorf("temporary build output left behind: %s", entry.Name())
		}
	}
	if failure == "none" {
		// An asset-only edit must invalidate both normal and static binaries.
		check := func(wantStale bool) {
			t.Helper()
			probe := exec.CommandContext(ctx, makePath, "--no-print-directory", "-q", target, "GO_SOURCE_ROOTS=main.go")
			probe.Dir, probe.Env = root, cmd.Env
			output, err := probe.CombinedOutput()
			if wantStale {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
					t.Fatalf("asset did not trigger rebuild: %v %s", err, output)
				}
			} else if err != nil {
				t.Fatalf("completed build still stale: %v %s", err, output)
			}
		}
		check(false)
		later := time.Now().Add(time.Second)
		if err := os.Chtimes(filepath.Join(root, "internal/gateway/assets/gateway.css"), later, later); err != nil {
			t.Fatal(err)
		}
		check(true)
	}
}
