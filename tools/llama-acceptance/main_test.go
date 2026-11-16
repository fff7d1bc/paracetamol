package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/gateway"
	"paracetamol/internal/podman"
	"paracetamol/tools/llama-acceptance/internal/contract"
	"paracetamol/tools/llama-acceptance/internal/transport"
)

func TestHelpAndSourceOnlyNeedNoGatewayOrPodman(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"check", "--help"}, {"compare", "--help"}, {"prepare-grader", "--help"}, {"prepare-trace", "--help"}, {"self-test", "--help"}, {"self-test", "--source-only"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 {
			t.Fatalf("%v produced no guidance", args)
		}
	}
}

func TestRunRequiresExplicitIdentityAndTimingTolerance(t *testing.T) {
	base := []string{"--gateway-url", "http://127.0.0.1:18455/v1", "--container-id", strings.Repeat("a", 64), "--image-id", "sha256:" + strings.Repeat("b", 64), "--data-dir", "/example/content"}
	for _, tail := range [][]string{{"--suite", "mystery"}, {"--suite", "all"}, {"--suite", "prefill"}} {
		if _, _, err := parseRun(append(append([]string{}, base...), tail...), io.Discard); err == nil {
			t.Fatalf("accepted %v", tail)
		}
	}
	if _, _, err := parseRun(append(base, "--suite", "prefill", "--prefill-regression-percent", "10"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseRun(append(append([]string{}, base...), "--suite", "lifecycle"), io.Discard); err != nil {
		t.Fatalf("standalone lifecycle suite should need neither a grader nor timing tolerance: %v", err)
	}
}

func TestOldRunSchemaIsNotRelabeledForNewCoverage(t *testing.T) {
	for _, version := range []string{"paracetamol.llama-acceptance.v1", "paracetamol.llama-acceptance.v2"} {
		run := comparisonFixture("history", 0, false)
		run.Schema = version
		if err := validateObservation(run); err == nil {
			t.Fatalf("old evidence accepted as instance-bound coverage: %s", version)
		}
	}
}

func TestTraceRunIsExplicitAndDiagnosticOnly(t *testing.T) {
	base := []string{"--gateway-url", "http://127.0.0.1:18455/v1", "--container-id", strings.Repeat("a", 64), "--image-id", "sha256:" + strings.Repeat("b", 64), "--data-dir", "/example/content", "--trace"}
	for _, tail := range [][]string{{"--suite", "coding"}, {"--suite", "all", "--prefill-regression-percent", "10"}, {"--control", "mmq-sum"}} {
		if _, _, err := parseRun(append(append([]string{}, base...), tail...), io.Discard); err == nil {
			t.Fatalf("invalid trace campaign accepted: %v", tail)
		}
	}
	for _, tail := range [][]string{{}, {"--suite", "prefill", "--prefill-regression-percent", "10", "--control", "qsa-host-span"}} {
		if opts, _, err := parseRun(append(append([]string{}, base...), tail...), io.Discard); err != nil || !opts.trace {
			t.Fatalf("explicit trace diagnostic refused: opts=%+v err=%v", opts, err)
		}
	}
}

func TestOutputBoundariesAndCreateOnce(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"", "build", "build/..", "elsewhere", "../outside"} {
		if _, err := validateNewOutput(root, path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "build")); !os.IsNotExist(err) {
		t.Fatal("validation created a build directory")
	}
	path, err := createOutput(root, "build/run")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0700 {
		t.Fatalf("mode %v", info.Mode())
	}
	if _, err := createOutput(root, "build/run"); err == nil {
		t.Fatal("reused evidence")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "build", "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := createOutput(root, "build/alias/run"); err == nil {
		t.Fatal("followed output ancestor symlink")
	}
}

func TestStrictCodeEnvelope(t *testing.T) {
	for _, bad := range []string{"", "```json\n{}\n```", `{}`, `{"code":""}`, `{"code":"a","code":"b"}`, `{"code":"a","extra":0}`, `{"code":"a"} trailing`, `{"code":null}`} {
		if _, err := extractCode(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	code, err := extractCode(` {"code":"package main\n"} `)
	if err != nil || code != "package main\n" {
		t.Fatalf("%q %v", code, err)
	}
}

func TestCodingInfrastructureAndProtocolDoNotTriggerRetries(t *testing.T) {
	for _, kind := range []string{transport.Infrastructure, transport.Policy, transport.Protocol} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			results := codingCases(context.Background(), t.TempDir(), runOptions{model: regularModel}, func(context.Context, string, map[string]any) (contract.Response, error) {
				calls++
				return contract.Response{}, &transport.Error{Kind: kind, Err: errors.New("fixture failure")}
			})
			if calls != 1 || len(results) != 1 || (kind == transport.Infrastructure && results[0].Status != contract.Blocked) || (kind != transport.Infrastructure && results[0].Status != contract.Fail) {
				t.Fatalf("calls=%d results=%+v", calls, results)
			}
		})
	}
}

func TestJoinedErrorSeverityDoesNotHideRequiredFailures(t *testing.T) {
	limit := &transport.Error{Kind: transport.OutputLimit, Err: errors.New("length")}
	for _, kind := range []string{transport.Infrastructure, transport.Policy, transport.Protocol} {
		joined := errors.Join(limit, fmt.Errorf("later report: %w", &transport.Error{Kind: kind, Err: errors.New("invalid report")}))
		if got := transportErrorKind(joined); got != kind {
			t.Fatalf("%s hidden by %s", kind, got)
		}
		calls := 0
		results := codingCases(context.Background(), t.TempDir(), runOptions{model: regularModel}, func(context.Context, string, map[string]any) (contract.Response, error) {
			calls++
			return contract.Response{}, joined
		})
		if calls != 1 || len(results) != 1 || results[0].Status == contract.Review {
			t.Fatalf("calls=%d results=%+v", calls, results)
		}
	}
}

func TestStrictScenarioCeilingDoesNotBecomeCodingReview(t *testing.T) {
	strict := contract.Result{Name: "off-replay-seed-42", Status: contract.Fail, RequestIDs: []string{"r1"}}
	classifyResult(&strict, map[string]string{"r1": transport.OutputLimit})
	if strict.Status != contract.Fail {
		t.Fatal("truncated strict replay became a coding-style REVIEW")
	}
	coding := contract.Result{Name: "go-first", Status: contract.Review, RequestIDs: []string{"r2"}}
	classifyResult(&coding, map[string]string{"r2": transport.OutputLimit})
	if coding.Status != contract.Review {
		t.Fatal("predeclared bounded coding ceiling lost its REVIEW classification")
	}
	for _, ids := range [][]string{{"policy", "infra"}, {"infra", "policy"}, {"protocol", "infra"}, {"infra", "protocol"}} {
		result := contract.Result{Name: "four-worker-isolation", Status: contract.Fail, RequestIDs: ids}
		classifyResult(&result, map[string]string{"policy": transport.Policy, "protocol": transport.Protocol, "infra": transport.Infrastructure})
		if result.Status != contract.Fail {
			t.Fatal("request iteration order hid a hard failure behind infrastructure classification")
		}
	}
	infra := contract.Result{Name: "unavailable", Status: contract.Fail}
	classifyResult(&infra, map[string]string{"unavailable": transport.Infrastructure})
	if infra.Status != contract.Blocked {
		t.Fatal("infrastructure failure did not remain blocked")
	}
}

func TestCodingUnsolicitedToolAnswerNeverReachesGrader(t *testing.T) {
	results := codingCases(context.Background(), t.TempDir(), runOptions{model: regularModel, graderImage: "must-not-be-used"}, func(context.Context, string, map[string]any) (contract.Response, error) {
		return contract.Response{Text: `{"code":"package main"}`, Finish: "tool_calls", Tools: []contract.ToolCall{{Name: "not-allowed"}}}, nil
	})
	if len(results) != 6 {
		t.Fatalf("expected first + two fresh attempts for each task, got %+v", results)
	}
	for _, result := range results {
		if result.Status != contract.Review || !strings.Contains(result.Detail, "without tool calls") {
			t.Fatalf("graded unfinished tool answer: %+v", result)
		}
	}
}

func TestCodingMissesKeepFirstRepairAndFreshSeeds(t *testing.T) {
	var names []string
	var seeds []int
	results := codingCases(context.Background(), t.TempDir(), runOptions{model: regularModel}, func(_ context.Context, name string, payload map[string]any) (contract.Response, error) {
		names = append(names, name)
		seeds = append(seeds, payload["seed"].(int))
		return contract.Response{Message: map[string]any{"role": "assistant", "content": "unfinished", "reasoning_content": "retained"}}, &transport.Error{Kind: transport.OutputLimit, Err: errors.New("length")}
	})
	if len(results) != 8 || len(names) != 8 {
		t.Fatalf("results=%d calls=%v", len(results), names)
	}
	for i, result := range results {
		if result.Status != contract.Review || seeds[i] != []int{42, 42, 1234, 2026}[i%4] {
			t.Fatalf("%+v seeds=%v", result, seeds)
		}
	}
}

func validNativeArgs(model string) []string {
	args := []string{"llama-server", "--ctx-size", "262144", "--parallel", "4", "--batch-size", "2048", "--ubatch-size", "2048", "--cache-type-k", "f16", "--cache-type-v", "f16", "--kv-unified", "--device", "ROCm0", "--model", "/models/target.gguf", "--mmproj", "/models/projector.gguf", "--chat-template-file", "/usr/local/share/paracetamol/llama-chat-templates/qwen3.8.jinja", "--reasoning-preserve", "--load-mode", "none", "--lazy-mode", "on-direct", "--cache-ram", "0", "--fit", "off", "--flash-attn", "on"}
	if strings.HasSuffix(model, "-dflash2") {
		args = append(args, "--spec-type", "draft-dflash", "--spec-draft-n-max", "3", "--spec-draft-sampling", "greedy", "--model-draft", "/models/draft.gguf")
	}
	return args
}

func TestNativePolicyRejectsDriftAndAmbiguity(t *testing.T) {
	for _, model := range []string{regularModel, regularModel + "-dflash2"} {
		args := validNativeArgs(model)
		if _, err := nativePolicy(args, model); err != nil {
			t.Fatal(err)
		}
		for _, extra := range [][]string{{"--cache-type-k", "q8_0"}, {"--no-kv-unified"}, {"--no-reasoning-preserve"}, {"--api-key", "not-to-be-saved"}, {"--ubatch-size=4096"}} {
			if _, err := nativePolicy(append(append([]string{}, args...), extra...), model); err == nil {
				t.Fatalf("accepted %v", extra)
			}
		}
	}
}

func TestContainerInspectionUsesEffectiveCapabilities(t *testing.T) {
	opts := runOptions{containerID: strings.Repeat("a", 64), imageID: "sha256:" + strings.Repeat("b", 64)}
	var c inspectedContainer
	c.ID, c.Image, c.State.Running = opts.containerID, opts.imageID, true
	c.Name = "paracetamol-gateway-llama-cpp"
	c.Config.Labels = map[string]string{podman.ManagedContainerLabel: "true", podman.ManagedApplicationLabel: "llama-cpp", podman.ManagedRoleLabel: gateway.GatewayBackendRole, gateway.GatewayInstanceLabel: strings.Repeat("c", 32)}
	c.HostConfig.ReadonlyRootfs = true
	c.HostConfig.NetworkMode = "pasta"
	c.HostConfig.CapDrop = []string{"CAP_CHOWN", "CAP_SETUID"}
	c.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	c.EffectiveCaps, c.BoundingCaps = []byte("null"), []byte("[]")
	if err := validateContainer(c, opts); err != nil {
		t.Fatal(err)
	}
	c.EffectiveCaps = nil
	if err := validateContainer(c, opts); err == nil {
		t.Fatal("missing capability observation accepted")
	}
	c.EffectiveCaps = []byte(`["CAP_CHOWN"]`)
	if err := validateContainer(c, opts); err == nil {
		t.Fatal("remaining capability accepted")
	}
}

func TestMemoryGuardIsSynchronous(t *testing.T) {
	ctx, stop := memoryGuard(context.Background(), math.MaxInt64)
	defer stop()
	if ctx.Err() == nil || !strings.Contains(context.Cause(ctx).Error(), "memory guard") {
		t.Fatal("inference could start before initial memory guard observation")
	}
}
