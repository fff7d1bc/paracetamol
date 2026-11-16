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

func TestMakeCleanConfinesMutationToBuildDirectory(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Fatal("make is required for checkout cleanup tests")
	}
	for _, kind := range []string{"missing", "directory", "directory-symlink", "file-symlink", "dangling-symlink", "file", "override", "override-ignore-errors"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			checkout := filepath.Join(root, "checkout")
			outside := filepath.Join(root, "outside")
			for _, path := range []string{checkout, outside, filepath.Join(checkout, "internal/gateway/assets"), filepath.Join(checkout, "tools/llama-acceptance")} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(checkout, "Makefile"), makefile, 0o644); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(outside, "sentinel")
			const contents = "unrelated fixture content\n"
			if err := os.WriteFile(sentinel, []byte(contents), 0o400); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(sentinel, 0o400); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(outside, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(outside, 0o755) })

			buildRoot := filepath.Join(checkout, "build")
			args := []string{"--no-print-directory", "clean", "HOST_GOOS=linux", "HOST_GOARCH=amd64", "GO_SOURCE_ROOTS=Makefile"}
			switch kind {
			case "directory":
				cache := filepath.Join(buildRoot, "gomodcache", "readonly")
				if err := os.MkdirAll(cache, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cache, "module.go"), []byte("cache fixture\n"), 0o444); err != nil {
					t.Fatal(err)
				}
				// Recursive chmod must still make Go's read-only cache removable,
				// without following symlinks found inside the owned build tree.
				if err := os.Symlink(outside, filepath.Join(cache, "outside")); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(cache, 0o555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(cache, 0o755) })
			case "directory-symlink", "file-symlink", "dangling-symlink":
				target := outside
				if kind == "file-symlink" {
					target = sentinel
				} else if kind == "dangling-symlink" {
					target = filepath.Join(root, "absent")
				}
				if err := os.Symlink(target, buildRoot); err != nil {
					t.Fatal(err)
				}
			case "file":
				if err := os.WriteFile(buildRoot, []byte(contents), 0o400); err != nil {
					t.Fatal(err)
				}
			case "override", "override-ignore-errors":
				args = append(args, "BUILD_ROOT="+outside)
				if kind == "override-ignore-errors" {
					args = append(args, "--ignore-errors")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, makePath, args...)
			cmd.Dir = checkout
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
			output, cleanErr := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("clean did not finish: %v\n%s", ctx.Err(), output)
			}
			wantSuccess := kind == "missing" || kind == "directory" || kind == "override-ignore-errors"
			if (cleanErr == nil) != wantSuccess {
				t.Errorf("clean error=%v wantSuccess=%t\n%s", cleanErr, wantSuccess, output)
			}
			if kind == "missing" || kind == "directory" {
				retry := exec.CommandContext(ctx, makePath, args...)
				retry.Dir, retry.Env = cmd.Dir, cmd.Env
				if output, err := retry.CombinedOutput(); err != nil {
					t.Errorf("clean is not idempotent: %v\n%s", err, output)
				}
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != contents {
				t.Errorf("outside sentinel=%q err=%v", data, err)
			}
			for path, want := range map[string]os.FileMode{outside: 0o500, sentinel: 0o400} {
				if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
					t.Errorf("outside permissions changed: path=%s info=%v err=%v", path, info, err)
				}
			}
			info, err := os.Lstat(buildRoot)
			if strings.HasSuffix(kind, "symlink") {
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Errorf("refused build symlink changed: info=%v err=%v", info, err)
				}
			} else if kind == "file" {
				if data, err := os.ReadFile(buildRoot); err != nil || string(data) != contents {
					t.Errorf("refused build file changed: data=%q err=%v", data, err)
				}
				if err != nil || info.Mode().Perm() != 0o400 {
					t.Errorf("refused build file permissions changed: info=%v err=%v", info, err)
				}
			} else if !os.IsNotExist(err) {
				t.Errorf("build path remains: info=%v err=%v", info, err)
			}
		})
	}
}

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
	for _, target := range []string{"build", "static", "gufo-recheck", "llama-acceptance"} {
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
	if err := os.MkdirAll(filepath.Join(root, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile("../../tools/go-test-exec.sh")
	if err != nil {
		t.Fatal(err)
	}
	write("tools/go-test-exec.sh", helper, 0o644)
	write("go.mod", []byte("module fixture\n\ngo 1.26.0\n"), 0o644)
	write("main.go", []byte("package main\nfunc main() {}\n"), 0o644)
	if err := os.MkdirAll(filepath.Join(root, "tools", "gufo-recheck", "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("tools/gufo-recheck/Containerfile.fragment", []byte("# experiment fixture\n"), 0o644)
	write("tools/gufo-recheck/main.go", []byte("package main\nfunc main() {}\n"), 0o644)
	write("tools/gufo-recheck/testdata/literal-xml.json", []byte("{}\n"), 0o644)
	if err := os.MkdirAll(filepath.Join(root, "tools", "llama-acceptance"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("tools/llama-acceptance/main.go", []byte("package main\nfunc main() {}\n"), 0o644)
	write("tools/llama-acceptance/fixture.txt", []byte("acceptance fixture\n"), 0o644)
	write("internal/gateway/assets/gateway.css", []byte("body {}\n"), 0o644)
	write("internal/gateway/assets/gateway.js", []byte("// retained embedded fixture\n"), 0o644)
	for _, dir := range []string{"internal/removed-source", "internal/removed-subtree"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(dir, "fixture.go"), []byte("package fixture\n"), 0o644)
	}
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
printf '%s\n' "${BUILD_TEST_EXECUTABLE_CONTENT-new executable fixture}" > "$output"
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
	} else if target == "gufo-recheck" || target == "llama-acceptance" {
		name = target
	}
	destination := filepath.Join(outputDir, name)
	const previous = "previous executable fixture\n"
	if existing {
		write(filepath.Join("build", "linux-amd64", "bin", name), []byte(previous), 0o755)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const sourceRoots = "GO_SOURCE_ROOTS=main.go internal tools"
	cmd := exec.CommandContext(ctx, makePath, "--no-print-directory", "-B", target, sourceRoots)
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
			probe := exec.CommandContext(ctx, makePath, "--no-print-directory", "-q", target, sourceRoots)
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
		if target == "build" {
			// rsync -a replaces source while retaining its old timestamp and
			// preserving excluded build/. Incremental Make cannot detect that
			// transfer; the documented destination make -B build must publish
			// the transferred controller instead of keeping the newer binary.
			path := filepath.Join(root, "main.go")
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("package main\nfunc main() { println(\"transferred\") }\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			check(false)
			for _, force := range []bool{false, true} {
				arguments := []string{"--no-print-directory", "build", sourceRoots}
				if force {
					arguments = append(arguments, "-B")
				}
				build := exec.CommandContext(ctx, makePath, arguments...)
				build.Dir = root
				build.Env = append(append([]string(nil), cmd.Env...), "BUILD_TEST_EXECUTABLE_CONTENT=transferred executable fixture")
				if output, err := build.CombinedOutput(); err != nil {
					t.Fatalf("transferred source force=%t: %v\n%s", force, err, output)
				}
				want := "new executable fixture\n"
				if force {
					want = "transferred executable fixture\n"
				}
				data, err := os.ReadFile(destination)
				if err != nil || string(data) != want {
					t.Fatalf("transferred source force=%t: executable=%q want=%q err=%v", force, data, want, err)
				}
			}
			check(false)
		}
		if target == "gufo-recheck" {
			for _, asset := range []string{"Containerfile.fragment", "testdata/literal-xml.json"} {
				path := filepath.Join(root, "tools", "gufo-recheck", asset)
				before, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				later := time.Now().Add(time.Second)
				if err := os.Chtimes(path, later, later); err != nil {
					t.Fatal(err)
				}
				check(true)
				if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
					t.Fatal(err)
				}
				check(false)
			}
		}
		later := time.Now().Add(time.Second)
		if err := os.Chtimes(filepath.Join(root, "internal/gateway/assets/gateway.css"), later, later); err != nil {
			t.Fatal(err)
		}
		check(true)
		// Return the asset to the past before testing real incremental builds;
		// a future-dated prerequisite would intentionally remain out of date.
		past := time.Now().Add(-time.Hour)
		if err := os.Chtimes(filepath.Join(root, "internal/gateway/assets/gateway.css"), past, past); err != nil {
			t.Fatal(err)
		}
		check(false)

		// Removing a file erases it from Make's dynamically enumerated inputs.
		// The retained parent directories must still invalidate every target,
		// including when the last file and its entire directory are removed.
		removals := []string{
			"internal/removed-source/fixture.go",
			"internal/removed-subtree",
			"internal/gateway/assets/gateway.css",
		}
		if target == "gufo-recheck" {
			removals = append(removals, "tools/gufo-recheck/testdata/literal-xml.json")
		} else if target == "llama-acceptance" {
			removals = append(removals, "tools/llama-acceptance/fixture.txt")
		}
		for _, removed := range removals {
			if err := os.RemoveAll(filepath.Join(root, removed)); err != nil {
				t.Fatal(err)
			}
			check(true)
			want := "rebuilt after removing " + removed + "\n"
			build := exec.CommandContext(ctx, makePath, "--no-print-directory", target, sourceRoots)
			build.Dir = root
			build.Env = append(append([]string(nil), cmd.Env...), "BUILD_TEST_EXECUTABLE_CONTENT="+strings.TrimSuffix(want, "\n"))
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("rebuild after removing %s: %v\n%s", removed, err, output)
			}
			if data, err := os.ReadFile(destination); err != nil || string(data) != want {
				t.Fatalf("removing %s left stale executable=%q want=%q err=%v", removed, data, want, err)
			}
			check(false)
			before, err := os.Stat(destination)
			if err != nil {
				t.Fatal(err)
			}
			// An unchanged launcher/build must not compile or republish.
			unchanged := exec.CommandContext(ctx, makePath, "--no-print-directory", target, sourceRoots)
			unchanged.Dir = root
			unchanged.Env = append(append([]string(nil), cmd.Env...), "BUILD_TEST_EXECUTABLE_CONTENT=unexpected rebuild")
			if output, err := unchanged.CombinedOutput(); err != nil {
				t.Fatalf("unchanged build: %v\n%s", err, output)
			}
			after, err := os.Stat(destination)
			if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("unchanged build republished executable: before=%v after=%v err=%v", before, after, err)
			}
		}
	}
}
