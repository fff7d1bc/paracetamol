package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFrozenFixturesAndIndependentOracles(t *testing.T) {
	for key, value := range Manifest() {
		if strings.HasPrefix(key, "vectors/") {
			t.Logf("%s %s", key, value)
		}
	}
	if err := ValidateFixtures(); err != nil {
		t.Fatal(err)
	}
	for _, task := range Tasks() {
		input, want, err := vectors(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(want) != 71 {
			t.Fatalf("%s count=%d", task.ID, len(want))
		}
		input2, want2, _ := vectors(task.ID)
		if !bytes.Equal(input, input2) || fmt.Sprint(want) != fmt.Sprint(want2) {
			t.Fatal("nondeterministic dataset")
		}
		for _, forbidden := range []string{"fixture.xml", "heap-pop", "private project", "PROVENANCE", "oracle"} {
			if strings.Contains(task.Prompt, forbidden) {
				t.Fatalf("prompt leaked grader material %q", forbidden)
			}
		}
		var rows []map[string]any
		for _, expected := range want {
			rows = append(rows, map[string]any{"values": json.RawMessage(expected), "owned": true})
		}
		raw, _ := json.Marshal(rows)
		if err := verifyOutput(raw, want); err != nil {
			t.Fatal(err)
		}
		rows[0]["owned"] = false
		bad, _ := json.Marshal(rows)
		if verifyOutput(bad, want) == nil {
			t.Fatal("ownership miss accepted")
		}
		if verifyOutput(append(raw, []byte("\nPASS")...), want) == nil {
			t.Fatal("trailing output accepted")
		}
		if verifyOutput([]byte(`{"passed":true}`), want) == nil {
			t.Fatal("self reported pass accepted")
		}
	}
	got := intervalOracle([][2]int64{{8, 9}, {1, 4}, {4, 8}, {-2, -1}})
	if !slices.Equal(got, [][2]int64{{-2, -1}, {1, 9}}) {
		t.Fatalf("closure oracle: %v", got)
	}
	if _, _, err := vectors("unknown"); err == nil {
		t.Fatal("unknown task accepted")
	}
}

func TestOutputDirectorySafety(t *testing.T) {
	root := filepath.Join(t.TempDir(), "build")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, filepath.Join(root, "..", "escape"), filepath.Join(root, "bad:volume")} {
		if _, err := outputBelow(path, root); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := outputBelow(filepath.Join(root, "link", "child"), root); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	path := filepath.Join(root, "nested", "fresh")
	if _, err := outputBelow(path, root); err != nil {
		t.Fatal(err)
	}
	if _, err := outputBelow(path, root); err == nil {
		t.Fatal("existing output overwritten")
	}
}

func validInspection(cid, name, owner, image, stage string) inspection {
	var i inspection
	i.ID = cid
	i.Name = name
	i.Image = image
	i.EffectiveCaps = json.RawMessage(`[]`)
	i.BoundingCaps = json.RawMessage(`[]`)
	i.State.Status = "exited"
	i.Config.Labels = map[string]string{ownerLabel: owner}
	i.Config.User = "65532:65532"
	i.HostConfig.NetworkMode = "none"
	i.HostConfig.ReadonlyRootfs = true
	i.HostConfig.CapDrop = []string{"ALL"}
	i.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	i.HostConfig.Memory = 2 << 30
	i.HostConfig.MemorySwap = 2 << 30
	i.HostConfig.NanoCpus = 2e9
	i.HostConfig.PidsLimit = 128
	i.HostConfig.Tmpfs = map[string]string{"/tmp": "rw,nosuid,nodev,size=512m,mode=1777"}
	i.Mounts = append(i.Mounts, struct {
		Type, Source, Destination string
		RW                        bool
	}{"bind", stage, "/input", false})
	return i
}

func TestInspectConfinementFailsClosed(t *testing.T) {
	image := "sha256:" + strings.Repeat("a", 64)
	cid := strings.Repeat("b", 64)
	for _, fault := range []string{"", "owner", "image", "name", "cid", "network", "readonly", "privileged", "caps", "security", "memory", "swap", "cpus", "pids", "device", "mount", "tmpfs", "user"} {
		t.Run(fault, func(t *testing.T) {
			i := validInspection(cid, "grade", "owner", image, "/stage")
			switch fault {
			case "owner":
				i.Config.Labels[ownerLabel] = "other"
			case "image":
				i.Image = "other"
			case "name":
				i.Name = "other"
			case "cid":
				i.ID = "other"
			case "network":
				i.HostConfig.NetworkMode = "host"
			case "readonly":
				i.HostConfig.ReadonlyRootfs = false
			case "privileged":
				i.HostConfig.Privileged = true
			case "caps":
				i.HostConfig.CapDrop = nil
			case "security":
				i.HostConfig.SecurityOpt = nil
			case "memory":
				i.HostConfig.Memory = 0
			case "swap":
				i.HostConfig.MemorySwap = -1
			case "cpus":
				i.HostConfig.NanoCpus = 0
			case "pids":
				i.HostConfig.PidsLimit = 0
			case "device":
				i.HostConfig.Devices = []json.RawMessage{json.RawMessage(`{}`)}
			case "mount":
				i.Mounts[0].RW = true
			case "tmpfs":
				i.HostConfig.Tmpfs = nil
			case "user":
				i.Config.User = "0"
			}
			raw, _ := json.Marshal([]inspection{i})
			if err := verifyConfinement(raw, cid, "grade", "owner", image, "/stage"); (err == nil) != (fault == "") {
				t.Fatalf("fault=%s error=%v", fault, err)
			}
		})
	}
}

func TestCreateArgumentsContainOnlyPrivateReadOnlyBind(t *testing.T) {
	args := createArgs("sha256:"+strings.Repeat("a", 64), "/private/input", "/private/cid", "grade", "owner", "trusted command")
	for _, arg := range []string{"--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--memory=2g", "--cpus=2", "--pids-limit=128", "/private/input:/input:ro,Z"} {
		if !slices.Contains(args, arg) {
			t.Fatalf("missing %s", arg)
		}
	}
	for _, arg := range args {
		if strings.Contains(arg, "docker.sock") || strings.Contains(arg, "/dev/kfd") || strings.HasPrefix(arg, "--device") || strings.HasPrefix(arg, "--privileged") {
			t.Fatalf("unsafe argument %s", arg)
		}
	}
}

func TestBoundedBufferCancelsExcessOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &boundedBuffer{cancel: cancel}
	if _, err := b.Write(bytes.Repeat([]byte{'x'}, outputLimit+1)); err == nil {
		t.Fatal("unbounded output")
	}
	if len(b.data) != outputLimit || ctx.Err() == nil {
		t.Fatal("output cap did not cancel")
	}
}

func fakeOptions(t *testing.T, mode string) Options {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "catalog"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"Containerfile", "catalog/catalog.json"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PARACETAMOL_INTERNAL_PROJECT_ROOT", root)
	t.Setenv("CODING_FAKE_PODMAN", "1")
	t.Setenv("CODING_FAKE_MODE", mode)
	t.Setenv("CODING_FAKE_STATE", root)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(root, "podman-fake")
	script := "#!/bin/sh\nexec " + strconv.Quote(executable) + " -test.run '^TestFakePodmanProcess$' -- \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return Options{ImageID: "sha256:" + strings.Repeat("a", 64), OutputDir: filepath.Join(root, "build", "grade"), Podman: shim}
}

// A fake Podman process only emits fabricated metadata/output. It never runs
// candidate or trusted solution code on the host.
func TestFakePodmanProcess(t *testing.T) {
	if os.Getenv("CODING_FAKE_PODMAN") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	root, mode := os.Getenv("CODING_FAKE_STATE"), os.Getenv("CODING_FAKE_MODE")
	appendEvent := func(s string) {
		f, _ := os.OpenFile(filepath.Join(root, "events"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		fmt.Fprintln(f, s)
		_ = f.Close()
	}
	appendEvent(args[0])
	write := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	switch args[0] {
	case "info":
		write(map[string]any{"host": map[string]any{"security": map[string]bool{"rootless": mode != "rootful"}}})
	case "image":
		write([]map[string]any{{"Id": "sha256:" + strings.Repeat("a", 64), "Config": map[string]any{"Labels": map[string]string{graderLabel: Version}}}})
	case "create":
		get := func(key string) string {
			i := slices.Index(args, key)
			if i < 0 || i+1 == len(args) {
				os.Exit(90)
			}
			return args[i+1]
		}
		cid := strings.Repeat("b", 64)
		name := get("--name")
		owner := strings.TrimPrefix(get("--label"), ownerLabel+"=")
		stage := strings.TrimSuffix(get("--volume"), ":/input:ro,Z")
		i := validInspection(cid, name, owner, "sha256:"+strings.Repeat("a", 64), stage)
		if mode == "insecure" {
			i.HostConfig.NetworkMode = "host"
		}
		if mode == "foreign" {
			i.Config.Labels[ownerLabel] = "foreign"
		}
		raw, _ := json.Marshal([]inspection{i})
		_ = os.WriteFile(filepath.Join(root, "inspect.json"), raw, 0600)
		if mode != "create-no-cid" {
			_ = os.WriteFile(get("--cidfile"), []byte(cid), 0600)
		}
		if mode == "create-error" || mode == "create-no-cid" {
			os.Exit(125)
		}
		fmt.Println(cid)
	case "inspect":
		raw, _ := os.ReadFile(filepath.Join(root, "inspect.json"))
		_, _ = os.Stdout.Write(raw)
	case "start":
		if mode == "cancel" {
			if err := os.WriteFile(filepath.Join(root, "ready-to-cancel"), []byte("started"), 0600); err != nil {
				os.Exit(92)
			}
			time.Sleep(time.Minute)
		}
		if mode == "candidate-error" {
			os.Exit(1)
		}
		if mode == "output-limit" {
			_, _ = os.Stdout.Write(bytes.Repeat([]byte{'x'}, outputLimit+1))
			os.Exit(0)
		}
		_, want, _ := vectors("go-merge-unique-v1")
		var rows []map[string]any
		for _, expected := range want {
			rows = append(rows, map[string]any{"values": expected, "owned": true})
		}
		if mode == "wrong-output" {
			rows = rows[:1]
		}
		write(rows)
	case "rm":
		if mode != "rm-leak" && mode != "rm-error-present" {
			_ = os.WriteFile(filepath.Join(root, "removed"), []byte("yes"), 0600)
		}
		if mode == "rm-error-present" || mode == "rm-error-absent" {
			os.Exit(125)
		}
	case "container":
		if _, err := os.Stat(filepath.Join(root, "removed")); err == nil {
			os.Exit(1)
		}
	default:
		os.Exit(91)
	}
	os.Exit(0)
}

func TestGradeLifecycleAndExternalOracle(t *testing.T) {
	for _, mode := range []string{"pass", "wrong-output", "candidate-error", "output-limit", "insecure", "rootful", "foreign", "create-error", "create-no-cid", "rm-leak", "rm-error-present", "rm-error-absent"} {
		t.Run(mode, func(t *testing.T) {
			o := fakeOptions(t, mode)
			r, err := Grade(context.Background(), o, "go-merge-unique-v1", "package main")
			blocked := mode == "insecure" || mode == "rootful" || mode == "foreign" || mode == "create-error" || mode == "create-no-cid" || mode == "rm-leak" || mode == "rm-error-present"
			if (err != nil) != blocked || r.Passed != (mode == "pass" || mode == "rm-error-absent") {
				t.Fatalf("result=%+v error=%v", r, err)
			}
			events, _ := os.ReadFile(filepath.Join(os.Getenv("CODING_FAKE_STATE"), "events"))
			e := string(events)
			if blocked && mode != "rm-leak" && mode != "rm-error-present" && strings.Contains(e, "start\n") {
				t.Fatal("started without confinement")
			}
			if mode == "foreign" && strings.Contains(e, "rm\n") {
				t.Fatal("removed foreign container")
			}
			if mode != "rootful" && mode != "foreign" && !strings.Contains(e, "rm\n") {
				t.Fatal("owned container not removed")
			}
			if _, err := os.Stat(filepath.Join(o.OutputDir, "result.json")); err != nil {
				t.Fatal("result not preserved")
			}
		})
	}
}

func TestCanceledGradeCleansExactOwnedContainer(t *testing.T) {
	o := fakeOptions(t, "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, err := Grade(ctx, o, "go-merge-unique-v1", "package main")
		done <- err
	}()
	defer func() { cancel(); <-finished }()
	// Race-instrumented helper processes may spend seconds in metadata and
	// exit bookkeeping. Cancel only after creation, inspection and fake start,
	// so this tests owned-container cleanup rather than preflight cancellation.
	ready := time.NewTicker(5 * time.Millisecond)
	defer ready.Stop()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
waitForStart:
	for {
		select {
		case err := <-done:
			t.Fatalf("grade ended before cancellation precondition: %v", err)
		case <-deadline.C:
			t.Fatal("fake grading process did not reach start")
		case <-ready.C:
			if _, err := os.Stat(filepath.Join(os.Getenv("CODING_FAKE_STATE"), "ready-to-cancel")); err == nil {
				break waitForStart
			}
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation identity, got %v", err)
	}
	events, _ := os.ReadFile(filepath.Join(os.Getenv("CODING_FAKE_STATE"), "events"))
	if !strings.Contains(string(events), "rm\n") {
		t.Fatal("cancel left owned container")
	}
}

func TestGradeAcceptsOnlyFullImmutableImageIDs(t *testing.T) {
	for _, id := range []string{strings.Repeat("a", 64), "sha256:" + strings.Repeat("a", 64), "localhost/toy:latest", strings.Repeat("a", 12), strings.Repeat("z", 64)} {
		t.Run(id, func(t *testing.T) {
			o := fakeOptions(t, "pass")
			o.ImageID = id
			result, err := Grade(context.Background(), o, "go-merge-unique-v1", "package main")
			valid := id == strings.Repeat("a", 64) || id == "sha256:"+strings.Repeat("a", 64)
			if (err == nil) != valid || result.Passed != valid {
				t.Fatalf("image ID %q: result=%+v error=%v", id, result, err)
			}
		})
	}
}

func TestPrepareIsCreateOnceAndDoesNotInvokePodman(t *testing.T) {
	o := fakeOptions(t, "pass")
	path, err := Prepare(o.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Containerfile" {
		t.Fatal(path)
	}
	if _, err := Prepare(o.OutputDir); err == nil {
		t.Fatal("prepare overwrote inputs")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("CODING_FAKE_STATE"), "events")); !os.IsNotExist(err) {
		t.Fatal("prepare executed Podman")
	}
}

// Explicit developer opt-in. Normal make test never launches a container or
// runs generated code. A full immutable locally built grader image is required.
func TestSandboxSelfTest(t *testing.T) {
	image := os.Getenv("PARACETAMOL_TEST_GRADER_IMAGE")
	if image == "" {
		t.Skip("explicit local CPU grader image not supplied")
	}
	output := os.Getenv("PARACETAMOL_TEST_GRADER_OUTPUT")
	if output == "" {
		t.Fatal("self-test needs an explicit fresh output beneath checkout build/")
	}
	results, err := SelfTest(context.Background(), Options{ImageID: image, OutputDir: output})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Status != "PASS" {
			t.Errorf("%s: %s", result.Name, result.Detail)
		}
	}
	if len(results) != 8 {
		t.Fatalf("self-test controls=%d", len(results))
	}
}
