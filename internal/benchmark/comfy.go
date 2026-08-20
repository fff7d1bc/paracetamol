package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rocmplete/internal/atomicfile"
	"rocmplete/internal/catalog"
	"rocmplete/internal/identity"
)

const (
	ComfyResultSchema = "rocmplete.comfyui-benchmark.v2"
	ComfySuiteSchema  = "rocmplete.comfyui-benchmark-suite.v2"
)

var SyntheticInput = identity.StateNamespace + "/benchmark-input-768.png"

type ComfyConfiguration struct {
	Image        string `json:"image"`
	ImageID      string `json:"image_id,omitempty"`
	Profile      string `json:"profile"`
	RenderNode   string `json:"render_node"`
	Port         int    `json:"port"`
	Runs         int    `json:"runs"`
	Seed         int    `json:"seed"`
	MemoryPolicy string `json:"memory_policy"`
	KernelPolicy string `json:"kernel_policy"`
	CacheMode    string `json:"cache_mode"`
	Unconfined   bool   `json:"unconfined"`
}

type ComfySuiteEntry struct {
	Bundle          string  `json:"bundle"`
	Result          string  `json:"result,omitempty"`
	Status          string  `json:"status"`
	ColdSeconds     float64 `json:"cold_seconds,omitempty"`
	WarmMeanSeconds float64 `json:"warm_mean_seconds,omitempty"`
	Error           string  `json:"error,omitempty"`
}

type ComfySuite struct {
	Schema        string             `json:"schema"`
	SuiteID       string             `json:"suite_id"`
	Signature     string             `json:"signature"`
	Status        string             `json:"status"`
	CreatedAt     string             `json:"created_at"`
	FinishedAt    string             `json:"finished_at,omitempty"`
	Configuration ComfyConfiguration `json:"configuration"`
	Entries       []ComfySuiteEntry  `json:"entries"`
}

type ComfyArtifactEvidence struct {
	Identifier string `json:"identifier"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
}

type ComfyWorkflowEvidence struct {
	SourceSHA256   string `json:"benchmark_source_sha256"`
	Renderer       string `json:"benchmark_renderer"`
	RenderedSHA256 string `json:"benchmark_rendered_sha256"`
}

type ComfySyntheticInput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type ComfyRun struct {
	Index       int             `json:"index"`
	Kind        string          `json:"kind"`
	Seed        int             `json:"seed"`
	PromptID    string          `json:"prompt_id"`
	WallSeconds float64         `json:"wall_seconds"`
	Outputs     json.RawMessage `json:"outputs"`
}

type ComfyResult struct {
	Schema          string                  `json:"schema"`
	RunID           string                  `json:"run_id"`
	Bundle          string                  `json:"bundle"`
	Status          string                  `json:"status"`
	StartedAt       string                  `json:"started_at"`
	FinishedAt      string                  `json:"finished_at"`
	Profile         string                  `json:"profile"`
	RenderNode      string                  `json:"render_node"`
	MemoryPolicy    string                  `json:"memory_policy"`
	KernelPolicy    string                  `json:"kernel_policy"`
	CacheMode       string                  `json:"cache_mode"`
	Unconfined      bool                    `json:"unconfined"`
	Image           ImageIdentity           `json:"image"`
	Workflow        ComfyWorkflowEvidence   `json:"workflow"`
	Artifacts       []ComfyArtifactEvidence `json:"artifacts"`
	SyntheticInput  *ComfySyntheticInput    `json:"synthetic_input"`
	System          map[string]any          `json:"system"`
	Runs            []ComfyRun              `json:"runs"`
	OutputDirectory string                  `json:"output_directory"`
}

func LoadPrompt(root string, spec catalog.Benchmark) (map[string]any, error) {
	path := filepath.Join(root, "catalog", spec.Resource)
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read benchmark workflow %s: %w", path, err)
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != spec.SHA256 {
		return nil, fmt.Errorf("benchmark workflow does not match catalog: %s", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var prompt map[string]any
	if err := decoder.Decode(&prompt); err != nil || len(prompt) == 0 {
		return nil, fmt.Errorf("benchmark workflow is invalid: %s", path)
	}
	if spec.Renderer != "" && spec.Renderer != "identity" {
		if err := renderHunyuan(prompt, spec.Renderer); err != nil {
			return nil, err
		}
		canonical, err := CanonicalJSON(prompt)
		if err != nil {
			return nil, err
		}
		rendered := sha256.Sum256(canonical)
		if hex.EncodeToString(rendered[:]) != spec.RenderedSHA256 {
			return nil, fmt.Errorf("rendered benchmark workflow does not match catalog for %s", spec.Bundle)
		}
	}
	return prompt, nil
}

func CanonicalJSON(value any) ([]byte, error) {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(contents, '\n'), nil
}

func renderHunyuan(prompt map[string]any, renderer string) error {
	mode := ""
	model, dimension := "", ""
	steps, shift := int64(0), int64(0)
	switch renderer {
	case "hunyuan-i2v-480p-step-distilled":
		mode, model, dimension, steps, shift = "i2v", "hunyuanvideo1.5_480p_i2v_step_distilled_fp16.safetensors", "HunyuanVideo15ImageToVideo", 12, 7
	case "hunyuan-t2v-480p-cfg-distilled":
		mode, model, dimension, steps, shift = "t2v", "hunyuanvideo1.5_480p_t2v_cfg_distilled_fp16.safetensors", "EmptyHunyuanVideo15Latent", 50, 5
	default:
		return fmt.Errorf("unknown benchmark renderer %q", renderer)
	}
	var save string
	for identifier, raw := range prompt {
		node, ok := raw.(map[string]any)
		if !ok || node["class_type"] != "SaveVideo" {
			continue
		}
		inputs, _ := node["inputs"].(map[string]any)
		if inputs["filename_prefix"] == "video/hunyuan_video_1.5" {
			if save != "" {
				return fmt.Errorf("official Hunyuan benchmark has multiple base SaveVideo nodes")
			}
			save = identifier
		}
	}
	if save == "" {
		return fmt.Errorf("official Hunyuan benchmark has no base SaveVideo node")
	}
	reachable := map[string]bool{}
	pending := []string{save}
	for len(pending) > 0 {
		identifier := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if reachable[identifier] {
			continue
		}
		node, ok := prompt[identifier].(map[string]any)
		if !ok {
			return fmt.Errorf("Hunyuan benchmark has a broken node link")
		}
		reachable[identifier] = true
		inputs, ok := node["inputs"].(map[string]any)
		if !ok {
			return fmt.Errorf("Hunyuan benchmark node has invalid inputs")
		}
		for _, value := range inputs {
			link, ok := value.([]any)
			if ok && len(link) == 2 {
				if source, ok := link[0].(string); ok {
					pending = append(pending, source)
				}
			}
		}
	}
	for identifier := range prompt {
		if !reachable[identifier] {
			delete(prompt, identifier)
		}
	}
	counts := map[string]int{"UNETLoader": 0, dimension: 0, "BasicScheduler": 0, "CFGGuider": 0, "ModelSamplingSD3": 0}
	for _, raw := range prompt {
		node, _ := raw.(map[string]any)
		class, _ := node["class_type"].(string)
		inputs, _ := node["inputs"].(map[string]any)
		switch class {
		case "UNETLoader":
			inputs["unet_name"] = model
			counts[class]++
		case dimension:
			inputs["width"], inputs["height"] = int64(832), int64(480)
			counts[class]++
		case "BasicScheduler":
			inputs["steps"] = steps
			counts[class]++
		case "CFGGuider":
			inputs["cfg"] = int64(1)
			counts[class]++
		case "ModelSamplingSD3":
			inputs["shift"] = shift
			counts[class]++
		}
	}
	for name, count := range counts {
		if count != 1 {
			return fmt.Errorf("official Hunyuan %s benchmark changed: %s count is %d", mode, name, count)
		}
	}
	return nil
}

func PreparePrompt(source map[string]any, seed int64, prefix string) (map[string]any, bool, error) {
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, false, err
	}
	var prompt map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&prompt); err != nil {
		return nil, false, err
	}
	hasInput := false
	for _, raw := range prompt {
		node, ok := raw.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("benchmark workflow contains an invalid node")
		}
		inputs, ok := node["inputs"].(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("benchmark workflow node has invalid inputs")
		}
		for _, field := range []string{"seed", "noise_seed"} {
			if value, exists := inputs[field]; exists {
				if _, linked := value.([]any); !linked {
					inputs[field] = seed
				}
			}
		}
		switch node["class_type"] {
		case "LoadImage":
			inputs["image"], hasInput = SyntheticInput, true
		case "LoadVideo":
			return nil, false, fmt.Errorf("managed benchmark does not support video-conditioned input")
		case "SaveImage", "SaveVideo", "VHS_VideoCombine":
			inputs["filename_prefix"] = prefix
		}
	}
	return prompt, hasInput, nil
}

func SmokeImagePrompt(prompt map[string]any) error { return alterSmokePrompt(prompt, false) }

func SmokeVideoPrompt(prompt map[string]any) error { return alterSmokePrompt(prompt, true) }

func alterSmokePrompt(prompt map[string]any, video bool) error {
	counts := map[string]int{"switch": 0, "latent": 0, "positive": 0}
	if video {
		counts["duration"] = 0
	}
	for _, raw := range prompt {
		node, _ := raw.(map[string]any)
		class, _ := node["class_type"].(string)
		inputs, _ := node["inputs"].(map[string]any)
		metadata, _ := node["_meta"].(map[string]any)
		title := fmt.Sprint(metadata["title"])
		if (!video && class == "PrimitiveBoolean" && title == "Enable 4 Steps LoRA?") || (video && class == "PrimitiveBoolean" && title == "Enable Lightning LoRA") {
			inputs["value"] = true
			counts["switch"]++
		} else if !video && class == "EmptySD3LatentImage" {
			inputs["width"], inputs["height"] = int64(768), int64(768)
			counts["latent"]++
		} else if video && class == "EmptyHunyuanLatentVideo" {
			inputs["width"], inputs["height"] = int64(832), int64(480)
			counts["latent"]++
		} else if video && class == "PrimitiveFloat" && title == "Float (Duration)" {
			inputs["value"] = 0.25
			counts["duration"]++
		} else if class == "CLIPTextEncode" && strings.Contains(title, "Positive") {
			if video {
				inputs["text"] = "A red cube slowly rotating on a blue table, studio lighting"
			} else {
				inputs["text"] = "A red cube on a blue table, studio lighting, sharp focus"
			}
			counts["positive"]++
		}
	}
	for field, count := range counts {
		if count != 1 {
			return fmt.Errorf("smoke benchmark no longer has exactly one expected %s node", field)
		}
	}
	return nil
}

func EnsureSyntheticInput(dataRoot string) (string, string, error) {
	imageValue := image.NewRGBA(image.Rect(0, 0, 768, 768))
	for y := 0; y < 768; y++ {
		for x := 0; x < 768; x++ {
			imageValue.SetRGBA(x, y, color.RGBA{uint8(x * 255 / 767), uint8(y * 255 / 767), uint8((x ^ y) * 255 / 1023), 255})
		}
	}
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&encoded, imageValue); err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(encoded.Bytes())
	destination := filepath.Join(dataRoot, "apps", "comfyui", "input", filepath.FromSlash(SyntheticInput))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", "", err
	}
	if current, err := os.ReadFile(destination); err == nil {
		if bytes.Equal(current, encoded.Bytes()) {
			return destination, hex.EncodeToString(digest[:]), nil
		}
		return "", "", fmt.Errorf("refusing to replace differing synthetic benchmark input: %s", destination)
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	if err := atomicfile.Write(destination, encoded.Bytes(), 0o644, atomicfile.Create); err != nil {
		return "", "", err
	}
	return destination, hex.EncodeToString(digest[:]), nil
}

func PortAvailable(port int) error {
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("benchmark port %d is unavailable: %w", port, err)
	}
	return listener.Close()
}

func WaitForServer(ctx context.Context, baseURL string) (map[string]any, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	return WaitForServerWithClient(ctx, client, baseURL, time.Second)
}

func WaitForServerWithClient(ctx context.Context, client HTTPDoer, baseURL string, interval time.Duration) (map[string]any, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("poll interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/system_stats", nil)
		response, err := client.Do(request)
		if err == nil {
			contents, readErr := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
			response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && len(contents) <= 4*1024*1024 {
				var value map[string]any
				if json.Unmarshal(contents, &value) == nil {
					return value, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func QueuePrompt(ctx context.Context, baseURL string, prompt map[string]any) (string, error) {
	return QueuePromptWithClient(ctx, http.DefaultClient, baseURL, prompt)
}

func QueuePromptWithClient(ctx context.Context, client HTTPDoer, baseURL string, prompt map[string]any) (string, error) {
	body, err := json.Marshal(map[string]any{"prompt": prompt, "client_id": identity.StateNamespace + "-" + Identifier()})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/prompt", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || response.StatusCode != http.StatusOK || len(contents) > 4*1024*1024 {
		return "", fmt.Errorf("ComfyUI rejected benchmark prompt with HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(contents)))
	}
	var value struct {
		PromptID string `json:"prompt_id"`
	}
	if json.Unmarshal(contents, &value) != nil || value.PromptID == "" {
		return "", fmt.Errorf("ComfyUI returned no benchmark prompt identifier")
	}
	return value.PromptID, nil
}

func WaitForPrompt(ctx context.Context, baseURL, promptID string) (map[string]any, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	return WaitForPromptWithClient(ctx, client, baseURL, promptID, time.Second)
}

func WaitForPromptWithClient(ctx context.Context, client HTTPDoer, baseURL, promptID string, interval time.Duration) (map[string]any, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("poll interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/history/"+promptID, nil)
		response, err := client.Do(request)
		if err == nil {
			contents, readErr := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024+1))
			response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && len(contents) <= 16*1024*1024 {
				var value map[string]any
				if json.Unmarshal(contents, &value) == nil {
					if raw, ok := value[promptID].(map[string]any); ok {
						if status, ok := raw["status"].(map[string]any); ok && status["completed"] == false {
							// Still running.
						} else {
							return raw, nil
						}
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func WriteSuiteReports(subject string, suite ComfySuite, format, explicit string, replace bool) ([]string, error) {
	markdown := RenderSuiteMarkdown(suite)
	html := RenderSuiteHTML(suite)
	base := strings.TrimSuffix(subject, filepath.Ext(subject))
	var outputs []struct{ path, contents string }
	if format == "markdown" || format == "both" {
		path := base + ".md"
		if explicit != "" {
			path = explicit
		}
		outputs = append(outputs, struct{ path, contents string }{path, markdown})
	}
	if format == "html" || format == "both" {
		path := base + ".html"
		if explicit != "" {
			path = explicit
		}
		outputs = append(outputs, struct{ path, contents string }{path, html})
	}
	paths := make([]string, 0, len(outputs))
	policy := atomicfile.Create
	if replace {
		policy = atomicfile.ReplaceRegular
	}
	for _, output := range outputs {
		path, err := filepath.Abs(output.path)
		if err != nil {
			return nil, err
		}
		if err := atomicfile.Write(path, []byte(output.contents), 0o644, policy); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func RenderSuiteMarkdown(suite ComfySuite) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# %s benchmark suite\n\n- Suite: `%s`\n- Status: `%s`\n\n", identity.DisplayName, suite.SuiteID, suite.Status)
	builder.WriteString("| Bundle | Status | Cold (s) | Warm mean (s) | Result |\n|---|---:|---:|---:|---|\n")
	for _, entry := range suiteEntries(suite.Entries) {
		fmt.Fprintf(&builder, "| `%s` | %s | %s | %s | `%s` |\n", entry.Bundle, entry.Status, seconds(entry.ColdSeconds), seconds(entry.WarmMeanSeconds), entry.Result)
	}
	return builder.String()
}

func RenderSuiteHTML(suite ComfySuite) string {
	var rows strings.Builder
	for _, entry := range suiteEntries(suite.Entries) {
		fmt.Fprintf(&rows, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n", html.EscapeString(entry.Bundle), html.EscapeString(entry.Status), seconds(entry.ColdSeconds), seconds(entry.WarmMeanSeconds), html.EscapeString(entry.Result))
	}
	display := html.EscapeString(identity.DisplayName)
	return fmt.Sprintf("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>%s benchmark suite</title></head><body><h1>%s benchmark suite %s</h1><p>Status: %s</p><table><thead><tr><th>Bundle</th><th>Status</th><th>Cold (s)</th><th>Warm mean (s)</th><th>Result</th></tr></thead><tbody>%s</tbody></table></body></html>\n", display, display, html.EscapeString(suite.SuiteID), html.EscapeString(suite.Status), rows.String())
}

func suiteEntries(entries []ComfySuiteEntry) []ComfySuiteEntry {
	result := append([]ComfySuiteEntry(nil), entries...)
	sort.Slice(result, func(i, j int) bool { return result[i].Bundle < result[j].Bundle })
	return result
}

func seconds(value float64) string {
	if value > 0 {
		return fmt.Sprintf("%.2f", value)
	}
	return "—"
}
