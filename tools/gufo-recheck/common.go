package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/catalog"
)

const (
	regular    = "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"
	policyName = "qwen3.8-flash-next"
)

var errQualityFailed = errors.New("regression checks failed")

// These are experiment controls, not another runtime sampler policy. The real
// gateway inserts defaults. A changed catalog requires deliberate requalification
// of the frozen experiment rather than silently comparing different conditions.
const frozenSamplingJSON = `{"min_p":0.05,"presence_penalty":0,"repeat_penalty":1,"temperature":1,"top_k":20,"top_p":0.95}`

func verifyFrozenPolicy(managed catalog.Catalog) error {
	preset, ok := managed.LlamaPresets[regular]
	if !ok || preset.SamplingPolicy != policyName || preset.DefaultContext != 262144 ||
		preset.ReasoningControl != "effort" || !preset.ReasoningPreserve ||
		!slices.Contains(preset.ReasoningLevels, "xhigh") || !preset.AgentTools ||
		preset.KVCache["strix-halo"] != "f16" {
		return fmt.Errorf("regular Flash-Next no longer matches the frozen context, reasoning or tool contract; review the experiment before continuing")
	}
	policy, ok := managed.SamplingPolicies[policyName]
	if !ok {
		return fmt.Errorf("missing frozen sampling policy %s", policyName)
	}
	encoded, err := json.Marshal(policy.Defaults(true))
	if err != nil {
		return fmt.Errorf("encode current sampling defaults: %w", err)
	}
	var expected, actual map[string]float64
	if err := json.Unmarshal([]byte(frozenSamplingJSON), &expected); err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, &actual); err != nil || len(actual) != len(expected) {
		return fmt.Errorf("sampling policy differs from the frozen experiment; review its controls before continuing")
	}
	for name, value := range expected {
		if actualValue, found := actual[name]; !found || actualValue != value {
			return fmt.Errorf("sampling control %s differs from the frozen experiment; review its controls before continuing", name)
		}
	}
	return nil
}

func verifyCheckModel(managed catalog.Catalog, model string) error {
	if model != regular && model != regular+"-dflash2" {
		return fmt.Errorf("unsupported check model")
	}
	selected, ok := managed.LlamaPresets[model]
	base := managed.LlamaPresets[regular]
	if !ok || selected.Artifact != base.Artifact || selected.MMProjArtifact != base.MMProjArtifact ||
		selected.DefaultContext != base.DefaultContext || selected.SamplingPolicy != base.SamplingPolicy ||
		selected.ReasoningControl != base.ReasoningControl || !selected.ReasoningPreserve ||
		!slices.Contains(selected.ReasoningLevels, "xhigh") || selected.KVCache["strix-halo"] != "f16" {
		return fmt.Errorf("selected control no longer matches the frozen target, projector, context or reasoning policy")
	}
	return nil
}

func origin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("require a numeric loopback HTTP origin with an explicit port")
	}
	ip := net.ParseIP(u.Hostname())
	port, portErr := strconv.Atoi(u.Port())
	if u.Scheme != "http" || u.Opaque != "" || ip == nil || !ip.IsLoopback() ||
		portErr != nil || port < 1 || port > 65535 || u.User != nil ||
		u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("require a numeric loopback HTTP origin with an explicit port and no credentials, path, query or fragment")
	}
	u.Host = net.JoinHostPort(ip.String(), strconv.Itoa(port))
	return u, nil
}

func experimentClient() *http.Client {
	return &http.Client{
		// Explicit loopback destinations must not escape through an environment
		// proxy or redirect. There is no inference or response-header deadline.
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Keep captured prompts, traces and generated build inputs under the checkout's
// ignored build tree. Refuse symlink ancestors and existing output directories;
// an interrupted experiment is evidence, not an output slot to overwrite.
func newOutputDir(root, out string) (string, error) {
	if out == "" {
		return "", fmt.Errorf("--out requires a new directory below the checkout's build directory")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(resolvedRoot, out)
	}
	out = filepath.Clean(out)
	build := filepath.Join(resolvedRoot, "build")
	relative, err := filepath.Rel(build, out)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output must be a new subdirectory of %s", build)
	}
	components := append([]string{"build"}, strings.Split(relative, string(filepath.Separator))...)
	current := resolvedRoot
	for index, component := range components {
		current = filepath.Join(current, component)
		status, inspectErr := os.Lstat(current)
		if inspectErr == nil {
			if status.Mode()&os.ModeSymlink != 0 || !status.IsDir() {
				return "", fmt.Errorf("output ancestor is not a real directory: %s", current)
			}
			if index == len(components)-1 {
				return "", fmt.Errorf("refusing to reuse existing output directory: %s", current)
			}
		} else if !os.IsNotExist(inspectErr) {
			return "", inspectErr
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return "", err
	}
	// Mkdir, rather than MkdirAll for the leaf, rejects a concurrent creator.
	if err := os.Mkdir(out, 0o700); err != nil {
		return "", fmt.Errorf("create private experiment output: %w", err)
	}
	return out, nil
}

func saveJSON(path string, value any) error {
	return atomicfile.JSON(path, value, 0o600, atomicfile.Create)
}
