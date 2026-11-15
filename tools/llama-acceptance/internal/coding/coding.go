// Package coding owns original toy tasks and a CPU-only generated-code sandbox.
// It never owns inference, retries, downloads, image builds or backend lifecycle.
package coding

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"paracetamol/internal/project"
	"paracetamol/tools/llama-acceptance/internal/contract"
)

const Version = "paracetamol.toy-coding.v1"
const graderLabel = "io.github.fff7d1bc.paracetamol.llama-acceptance.grader"
const ownerLabel = "io.github.fff7d1bc.paracetamol.llama-acceptance.owner"
const outputLimit = 1 << 20

//go:embed fixtures/* PROVENANCE.md
var files embed.FS

type Task struct {
	ID, Language, Prompt string
	MaxTokens            int
}

type Options struct {
	ImageID   string // Full immutable local sha256 image ID, never a tag.
	OutputDir string // Fresh directory beneath this checkout's anchored build/.
	Podman    string // Optional executable; defaults to podman.
}

type GradeResult struct {
	TaskID    string `json:"task_id"`
	Passed    bool   `json:"passed"`
	Cases     int    `json:"cases"`
	Detail    string `json:"detail"`
	OutputDir string `json:"output_dir"`
}

func asset(name string) []byte {
	b, err := files.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return b
}

func Tasks() []Task {
	return []Task{
		{ID: "go-merge-unique-v1", Language: "go", Prompt: string(asset("fixtures/go-prompt.txt")), MaxTokens: 8192},
		{ID: "python-coalesce-v1", Language: "python", Prompt: string(asset("fixtures/python-prompt.txt")), MaxTokens: 8192},
	}
}

func Manifest() map[string]string {
	manifest := map[string]string{"version": Version}
	entries, _ := files.ReadDir("fixtures")
	for _, entry := range entries {
		name := "fixtures/" + entry.Name()
		h := sha256.Sum256(asset(name))
		manifest[name] = hex.EncodeToString(h[:])
	}
	h := sha256.Sum256(asset("PROVENANCE.md"))
	manifest["PROVENANCE.md"] = hex.EncodeToString(h[:])
	for _, task := range Tasks() {
		input, expected, _ := vectors(task.ID)
		encoded, _ := json.Marshal(expected)
		for name, data := range map[string][]byte{"input": input, "expected": encoded} {
			h := sha256.Sum256(data)
			manifest["vectors/"+task.ID+"/"+name] = hex.EncodeToString(h[:])
		}
	}
	return manifest
}

func ValidateFixtures() error {
	for _, task := range Tasks() {
		if task.Prompt == "" || task.MaxTokens <= 0 {
			return fmt.Errorf("invalid task %s", task.ID)
		}
	}
	for name, expected := range frozenHashes {
		if Manifest()[name] != expected {
			return fmt.Errorf("fixture %s changed; review version and provenance", name)
		}
	}
	if len(frozenHashes) != len(Manifest())-1 {
		return fmt.Errorf("fixture manifest closure changed")
	}
	return nil
}

// Prepare only materializes reviewed build inputs. Building/pulling needs a
// separate explicit operator action, and Grade always uses --pull=never.
func Prepare(outputDir string) (string, error) {
	if err := ValidateFixtures(); err != nil {
		return "", err
	}
	dir, err := newOutputDir(outputDir)
	if err != nil {
		return "", err
	}
	for _, name := range []string{"fixtures/Containerfile", "PROVENANCE.md"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), asset(name), 0600); err != nil {
			return "", err
		}
	}
	if err := save(filepath.Join(dir, "manifest.json"), Manifest()); err != nil {
		return "", err
	}
	return filepath.Join(dir, "Containerfile"), nil
}

// Small deterministic datasets are independent of model replies. Inputs are
// public review fixtures, but the expected arrays remain outside the sandbox.
func vectors(taskID string) ([]byte, []json.RawMessage, error) {
	rng := rand.New(rand.NewPCG(0x50415241, 0x43455441))
	var cases []json.RawMessage
	var expected []json.RawMessage
	add := func(input, output any) {
		i, _ := json.Marshal(input)
		o, _ := json.Marshal(output)
		cases = append(cases, i)
		expected = append(expected, o)
	}
	switch taskID {
	case "go-merge-unique-v1":
		inputs := [][][]int64{nil, {}, {nil, {}}, {{1}}, {{1, 1, 2}, {1, 3}}, {{math.MinInt64, 0, math.MaxInt64}, {math.MinInt64, -1, math.MaxInt64}}, {{1, 4, 7}, {2, 5, 8}, {3, 6, 9}}}
		for range 64 {
			rows := make([][]int64, rng.IntN(12))
			for i := range rows {
				for range rng.IntN(20) {
					rows[i] = append(rows[i], int64(rng.IntN(81)-40))
				}
				slices.Sort(rows[i])
			}
			inputs = append(inputs, rows)
		}
		for _, input := range inputs {
			flat := []int64{}
			for _, row := range input {
				flat = append(flat, row...)
			}
			slices.Sort(flat)
			flat = slices.Compact(flat)
			add(input, flat)
		}
	case "python-coalesce-v1":
		inputs := [][][2]int64{{}, {{0, 0}}, {{2, 4}, {0, 2}}, {{0, 10}, {2, 3}, {10, 10}}, {{-8, -5}, {-4, -1}}, {{math.MinInt64, 0}, {0, math.MaxInt64}}}
		for range 64 {
			rows := [][2]int64{}
			for range rng.IntN(16) {
				a, b := int64(rng.IntN(41)-20), int64(rng.IntN(41)-20)
				rows = append(rows, [2]int64{min(a, b), max(a, b)})
			}
			inputs = append(inputs, rows)
		}
		for _, input := range inputs {
			add(input, intervalOracle(input))
		}
		cases = append(cases, json.RawMessage(`[[1267650600228229401496703205376,1267650600228229401496703205378],[1267650600228229401496703205378,1267650600228229401496703205379]]`))
		expected = append(expected, json.RawMessage(`[[1267650600228229401496703205376,1267650600228229401496703205379]]`))
	default:
		return nil, nil, fmt.Errorf("unknown coding task %q", taskID)
	}
	input, err := json.Marshal(cases)
	return input, expected, err
}

// A connectivity closure, not the sorted folding algorithm in the solution.
func intervalOracle(input [][2]int64) [][2]int64 {
	remaining := append([][2]int64{}, input...)
	out := [][2]int64{}
	for len(remaining) > 0 {
		component := remaining[0]
		remaining = remaining[1:]
		for changed := true; changed; {
			changed = false
			for i := 0; i < len(remaining); {
				r := remaining[i]
				if r[0] <= component[1] && component[0] <= r[1] {
					component = [2]int64{min(component[0], r[0]), max(component[1], r[1])}
					remaining = append(remaining[:i], remaining[i+1:]...)
					changed = true
				} else {
					i++
				}
			}
		}
		out = append(out, component)
	}
	slices.SortFunc(out, func(a, b [2]int64) int {
		if a[0] < b[0] {
			return -1
		}
		if a[0] > b[0] {
			return 1
		}
		return 0
	})
	return out
}

func verifyOutput(raw []byte, expected []json.RawMessage) error {
	var rows []struct {
		Values json.RawMessage `json:"values"`
		Owned  bool            `json:"owned"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rows); err != nil {
		return fmt.Errorf("invalid result JSON: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing result output")
	}
	if len(rows) != len(expected) {
		return fmt.Errorf("result count %d, expected %d", len(rows), len(expected))
	}
	decode := func(b []byte) (any, error) {
		var v any
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		err := d.Decode(&v)
		return v, err
	}
	for i, row := range rows {
		actual, err := decode(row.Values)
		if err != nil {
			return fmt.Errorf("case %d malformed", i)
		}
		want, _ := decode(expected[i])
		if !row.Owned || !reflect.DeepEqual(actual, want) {
			return fmt.Errorf("case %d failed value/ownership oracle", i)
		}
	}
	return nil
}

var imagePattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func Grade(ctx context.Context, options Options, taskID, code string) (result GradeResult, returnedErr error) {
	result.TaskID = taskID
	// Podman commonly prints an unprefixed full ID. Accept that exact immutable
	// spelling too, never a tag, short ID or floating reference.
	if len(options.ImageID) == 64 {
		options.ImageID = "sha256:" + options.ImageID
	}
	if err := ValidateFixtures(); err != nil {
		return result, err
	}
	input, expected, err := vectors(taskID)
	if err != nil {
		return result, err
	}
	if len(code) == 0 || len(code) > 256<<10 {
		return result, errors.New("candidate code must contain 1..262144 bytes")
	}
	if !imagePattern.MatchString(options.ImageID) {
		return result, errors.New("grading requires a full immutable local sha256 image ID")
	}
	dir, err := newOutputDir(options.OutputDir)
	if err != nil {
		return result, err
	}
	result.OutputDir = dir
	result.Cases = len(expected)
	defer func() {
		if returnedErr != nil {
			result.Passed = false
			result.Detail = "infrastructure: " + returnedErr.Error()
		}
		if err := save(filepath.Join(dir, "result.json"), result); err != nil {
			returnedErr = errors.Join(returnedErr, err)
		}
	}()
	if err := save(filepath.Join(dir, "manifest.json"), Manifest()); err != nil {
		return result, err
	}
	podman := options.Podman
	if podman == "" {
		podman = "podman"
	}
	metadata := func(args ...string) ([]byte, error) {
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		out, stderr, err := command(c, podman, nil, args...)
		if err != nil {
			return nil, fmt.Errorf("podman metadata: %w: %s", err, stderr)
		}
		return out, nil
	}
	info, err := metadata("info", "--format=json")
	if err != nil {
		return result, err
	}
	var host struct {
		Host struct {
			Security struct {
				Rootless bool `json:"rootless"`
			} `json:"security"`
		} `json:"host"`
	}
	if json.Unmarshal(info, &host) != nil || !host.Host.Security.Rootless {
		return result, errors.New("grading requires rootless Podman")
	}
	image, err := metadata("image", "inspect", options.ImageID)
	if err != nil {
		return result, err
	}
	var images []struct {
		ID     string `json:"Id"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if json.Unmarshal(image, &images) != nil || len(images) != 1 || !sameImage(images[0].ID, options.ImageID) || images[0].Config.Labels[graderLabel] != Version {
		return result, errors.New("image identity or grader recipe label mismatch")
	}
	if err := os.WriteFile(filepath.Join(dir, "image.json"), image, 0600); err != nil {
		return result, err
	}
	stage := filepath.Join(dir, "input")
	if err := os.Mkdir(stage, 0700); err != nil {
		return result, err
	}
	language := "go"
	extension := "go"
	launch := "mkdir -p /tmp/work; cp /input/candidate.go /input/harness.go /tmp/work/; cd /tmp/work; exec /opt/go/bin/go run candidate.go harness.go"
	if taskID == "python-coalesce-v1" {
		language = "python"
		extension = "py"
		launch = "exec /usr/bin/python3.14 -I /input/harness.py"
	}
	for name, data := range map[string][]byte{"candidate." + extension: []byte(code), "harness." + extension: asset("fixtures/" + language + "-harness.txt")} {
		if err := os.WriteFile(filepath.Join(stage, name), data, 0600); err != nil {
			return result, err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), input, 0600); err != nil {
		return result, err
	}
	var token [16]byte
	if _, err := crand.Read(token[:]); err != nil {
		return result, err
	}
	owner := hex.EncodeToString(token[:])
	name := "paracetamol-toy-grade-" + owner
	cidfile := filepath.Join(dir, "container.cid")
	args := createArgs(options.ImageID, stage, cidfile, name, owner, launch)
	// Never accept or run an existing container. Cleanup is installed before
	// create because a canceled Podman client can leave a created container.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := cleanup(cleanupCtx, podman, cidfile, name, owner, options.ImageID); err != nil {
			returnedErr = errors.Join(returnedErr, err)
		}
	}()
	if _, err := metadata(args...); err != nil {
		return result, err
	}
	cid, err := readCID(cidfile)
	if err != nil {
		return result, err
	}
	inspect, err := metadata("inspect", "--type=container", cid)
	if err != nil {
		return result, err
	}
	if err := os.WriteFile(filepath.Join(dir, "container.json"), inspect, 0600); err != nil {
		return result, err
	}
	if err := verifyConfinement(inspect, cid, name, owner, options.ImageID, stage); err != nil {
		return result, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	stdout, stderr, runErr := command(runCtx, podman, input, "start", "--attach", "--interactive", cid)
	runCause := runCtx.Err()
	cancel()
	if err := os.WriteFile(filepath.Join(dir, "stdout.json"), stdout, 0600); err != nil {
		return result, err
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr.txt"), stderr, 0600); err != nil {
		return result, err
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if runCause != nil {
		result.Detail = "candidate exceeded the 120-second grading lifetime"
		return result, nil
	}
	// Attach may close just before Podman records the exited state. This small
	// metadata-only wait is not an additional generation/compilation allowance.
	for attempt := 0; ; attempt++ {
		finished, err := metadata("inspect", "--type=container", cid)
		if err != nil {
			return result, err
		}
		if err := os.WriteFile(filepath.Join(dir, "container-final.json"), finished, 0600); err != nil {
			return result, err
		}
		finalState, err := parseInspection(finished)
		if err != nil || !sameOwner(finalState, cid, name, owner, options.ImageID) || finalState.State.Error != "" {
			return result, errors.New("grading final runtime state or ownership invalid")
		}
		if finalState.State.Status == "exited" {
			break
		}
		if attempt == 19 {
			return result, fmt.Errorf("grading container did not reach exited runtime state: %s", finalState.State.Status)
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	if runErr != nil {
		result.Detail = "candidate compile/execution failed: " + runErr.Error()
		return result, nil
	}
	if err := verifyOutput(stdout, expected); err != nil {
		result.Detail = err.Error()
		return result, nil
	}
	result.Passed = true
	result.Detail = "all deterministic value and ownership cases pass"
	return result, nil
}

func createArgs(image, stage, cidfile, name, owner, launch string) []string {
	return []string{"create", "--pull=never", "--name", name, "--cidfile", cidfile, "--label", ownerLabel + "=" + owner,
		"--network=none", "--read-only", "--read-only-tmpfs=false", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--userns=keep-id:uid=65532,gid=65532", "--user=65532:65532", "--memory=2g", "--memory-swap=2g", "--cpus=2", "--pids-limit=128",
		"--log-driver=none", "--tmpfs", "/tmp:rw,nosuid,nodev,size=512m,mode=1777", "--volume", stage + ":/input:ro,Z",
		"--env=GOMAXPROCS=2", "--env=GOTOOLCHAIN=local", "--env=GOPROXY=off", "--env=GOSUMDB=off", "--env=GOTELEMETRY=off", "--env=CGO_ENABLED=0",
		"--entrypoint=/bin/sh", "--interactive", image, "-eu", "-c", launch}
}

type inspection struct {
	ID            string          `json:"Id"`
	Name          string          `json:"Name"`
	Image         string          `json:"Image"`
	EffectiveCaps json.RawMessage `json:"EffectiveCaps"`
	BoundingCaps  json.RawMessage `json:"BoundingCaps"`
	State         struct {
		Status string `json:"Status"`
		Error  string `json:"Error"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
		User   string            `json:"User"`
	} `json:"Config"`
	HostConfig struct {
		NetworkMode    string            `json:"NetworkMode"`
		ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
		CapDrop        []string          `json:"CapDrop"`
		CapAdd         []string          `json:"CapAdd"`
		SecurityOpt    []string          `json:"SecurityOpt"`
		Memory         int64             `json:"Memory"`
		MemorySwap     int64             `json:"MemorySwap"`
		NanoCpus       int64             `json:"NanoCpus"`
		PidsLimit      int64             `json:"PidsLimit"`
		Devices        []json.RawMessage `json:"Devices"`
		Privileged     bool              `json:"Privileged"`
		Tmpfs          map[string]string `json:"Tmpfs"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type, Source, Destination string
		RW                        bool
	} `json:"Mounts"`
}

func parseInspection(raw []byte) (inspection, error) {
	var rows []inspection
	if err := json.Unmarshal(raw, &rows); err != nil {
		return inspection{}, err
	}
	if len(rows) != 1 {
		return inspection{}, errors.New("ambiguous container inspection")
	}
	return rows[0], nil
}
func sameOwner(i inspection, cid, name, owner, image string) bool {
	return i.ID == cid && strings.TrimPrefix(i.Name, "/") == name && sameImage(i.Image, image) && i.Config.Labels[ownerLabel] == owner
}

func sameImage(actual, expected string) bool {
	return imagePattern.MatchString(expected) && strings.TrimPrefix(actual, "sha256:") == strings.TrimPrefix(expected, "sha256:")
}
func verifyConfinement(raw []byte, cid, name, owner, image, stage string) error {
	i, err := parseInspection(raw)
	if err != nil {
		return err
	}
	h := i.HostConfig
	if !sameOwner(i, cid, name, owner, image) || h.Privileged || h.NetworkMode != "none" || !h.ReadonlyRootfs || i.Config.User != "65532:65532" || len(h.Devices) != 0 || h.Memory != 2<<30 || h.MemorySwap != 2<<30 || h.NanoCpus != 2_000_000_000 || h.PidsLimit != 128 {
		return errors.New("created container does not match required isolation/resource contract")
	}
	// Podman expands --cap-drop=ALL into its default capability list. Check
	// the resulting empty sets, not a literal ALL token in inspection output.
	emptyCaps := func(raw json.RawMessage) bool {
		return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || bytes.Equal(bytes.TrimSpace(raw), []byte("[]"))
	}
	if len(h.CapDrop) == 0 || len(h.CapAdd) != 0 || !emptyCaps(i.EffectiveCaps) || !emptyCaps(i.BoundingCaps) || !slices.Contains(h.SecurityOpt, "no-new-privileges") {
		return errors.New("created container lacks required security controls")
	}
	if len(i.Mounts) != 1 || i.Mounts[0].Type != "bind" || i.Mounts[0].Source != stage || i.Mounts[0].Destination != "/input" || i.Mounts[0].RW {
		return errors.New("unexpected grading container mounts")
	}
	tmpOptions := strings.Split(h.Tmpfs["/tmp"], ",")
	if len(h.Tmpfs) != 1 || !slices.Contains(tmpOptions, "size=512m") || !slices.Contains(tmpOptions, "rw") || !slices.Contains(tmpOptions, "nosuid") || !slices.Contains(tmpOptions, "nodev") || !slices.Contains(tmpOptions, "mode=1777") {
		return errors.New("unexpected grading tmpfs")
	}
	return nil
}

func readCID(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if len(s) != 64 || !regexp.MustCompile(`^[0-9a-f]+$`).MatchString(s) {
		return "", errors.New("invalid grading CID")
	}
	return s, nil
}
func cleanup(ctx context.Context, podman, cidfile, name, owner, image string) error {
	cid, err := readCID(cidfile)
	if os.IsNotExist(err) {
		// A canceled create can commit the container before publishing cidfile.
		// Resolve only this fresh random name, then recheck every identity before
		// removing by the resolved full CID. Never search or prune containers.
		_, _, existsErr := command(ctx, podman, nil, "container", "exists", name)
		var exited *exec.ExitError
		if errors.As(existsErr, &exited) && exited.ExitCode() == 1 {
			return nil
		}
		if existsErr != nil {
			return fmt.Errorf("resolve interrupted grading create: %w", existsErr)
		}
		raw, _, inspectErr := command(ctx, podman, nil, "inspect", "--type=container", name)
		if inspectErr != nil {
			return fmt.Errorf("inspect interrupted grading create: %w", inspectErr)
		}
		i, inspectErr := parseInspection(raw)
		if inspectErr != nil || len(i.ID) != 64 || !imagePattern.MatchString("sha256:"+i.ID) {
			return errors.New("cannot resolve interrupted grading CID safely")
		}
		cid, err = i.ID, nil
	}
	if err != nil {
		return err
	}
	raw, _, err := command(ctx, podman, nil, "inspect", "--type=container", cid)
	if err != nil {
		return fmt.Errorf("inspect exact grading cleanup target: %w", err)
	}
	i, err := parseInspection(raw)
	if err != nil {
		return err
	}
	if !sameOwner(i, cid, name, owner, image) {
		return errors.New("refusing grading cleanup: CID/image/name/owner mismatch")
	}
	_, stderr, removeErr := command(ctx, podman, nil, "rm", "--force", "--time=2", cid)
	// Even a successful client exit is not proof of absence; a failed client
	// may also have completed removal. Inspect this exact CID only in both
	// cases, retaining the original error rather than silently forgetting it.
	_, _, existsErr := command(ctx, podman, nil, "container", "exists", cid)
	var exited *exec.ExitError
	absent := errors.As(existsErr, &exited) && exited.ExitCode() == 1
	evidence := map[string]any{"container_id": cid, "absent": absent}
	if removeErr != nil {
		evidence["remove_error"] = removeErr.Error()
		evidence["remove_stderr"] = string(stderr)
	}
	if existsErr != nil && !absent {
		evidence["absence_error"] = existsErr.Error()
	}
	if err := save(filepath.Join(filepath.Dir(cidfile), "cleanup.json"), evidence); err != nil {
		return fmt.Errorf("record grading cleanup: %w", err)
	}
	if !absent {
		return fmt.Errorf("grading cleanup did not establish exact CID absence: %w", errors.Join(removeErr, existsErr, errors.New("container remains or cannot be inspected")))
	}
	return nil
}

type boundedBuffer struct {
	mu     sync.Mutex
	data   []byte
	cancel context.CancelFunc
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := outputLimit - len(b.data)
	if n > room {
		b.data = append(b.data, p[:room]...)
		b.cancel()
		return n, errors.New("grading output limit exceeded")
	}
	b.data = append(b.data, p...)
	return n, nil
}
func command(ctx context.Context, executable string, input []byte, args ...string) ([]byte, []byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	out, stderr := &boundedBuffer{cancel: cancel}, &boundedBuffer{cancel: cancel}
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return out.data, stderr.data, err
}

func save(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}
func newOutputDir(path string) (string, error) {
	root, err := project.Root()
	if err != nil {
		return "", err
	}
	return outputBelow(path, filepath.Join(root, "build"))
}
func outputBelow(path, buildRoot string) (string, error) {
	if strings.ContainsAny(path, ":\x00\n\r") {
		return "", errors.New("unsafe output directory")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(buildRoot)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("output must be strictly beneath checkout build/")
	}
	current := root
	for _, part := range append([]string{""}, strings.Split(rel, string(filepath.Separator))...) {
		if part != "" {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", errors.New("output ancestor is not a real directory")
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return "", err
	}
	if err := os.Mkdir(abs, 0700); err != nil {
		return "", err
	}
	return abs, nil
}

func SelfTest(ctx context.Context, options Options) ([]contract.Result, error) {
	if err := ValidateFixtures(); err != nil {
		return nil, err
	}
	root, err := newOutputDir(options.OutputDir)
	if err != nil {
		return nil, err
	}
	var results []contract.Result
	for _, task := range Tasks() {
		language := task.Language
		trusted := string(asset("fixtures/" + language + "-solution.txt"))
		mutants := []string{trusted}
		if language == "go" {
			mutants = append(mutants, strings.Replace(trusted, "v:=a[len(a)-1]", "v:=a[0]", 1), strings.Replace(trusted, "h[i].value < h[j].value", "h[i].value > h[j].value", 1), `package main
func MergeSortedUnique(inputs [][]int64) []int64 { if len(inputs)>0 { return inputs[0] };return nil }
`)
		} else {
			mutants = append(mutants, "def coalesce(intervals):\n    return []\n", strings.Replace(trusted, "start <= result[-1][1]", "start < result[-1][1]", 1), strings.Replace(trusted, "for start, end in sorted(intervals):", "intervals.sort()\n    for start, end in intervals:", 1))
		}
		for n, code := range mutants {
			o := options
			o.OutputDir = filepath.Join(root, fmt.Sprintf("%s-control-%d", task.ID, n))
			grade, err := Grade(ctx, o, task.ID, code)
			if err != nil {
				return results, err
			}
			status := contract.Pass
			detail := "trusted solution passes"
			if n > 0 {
				detail = "broken control rejected"
			}
			if grade.Passed != (n == 0) {
				status = contract.Fail
				detail = "sandbox self-test failed: " + grade.Detail
			}
			results = append(results, contract.Result{Name: fmt.Sprintf("coding-selftest/%s/%d", task.ID, n), Status: status, Detail: detail})
		}
	}
	return results, nil
}
