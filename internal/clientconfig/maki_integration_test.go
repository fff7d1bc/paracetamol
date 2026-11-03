package clientconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paracetamol/internal/textmodel"
)

// Opt-in native-client acceptance. It exercises Maki's real TOML loading,
// model registry and transport without a GPU, real credentials or user state.
func TestInstalledMaki(t *testing.T) {
	executable := os.Getenv("PARACETAMOL_TEST_MAKI")
	if executable == "" {
		t.Skip("set PARACETAMOL_TEST_MAKI to an absolute native Maki executable")
	}
	if !filepath.IsAbs(executable) {
		t.Fatal("PARACETAMOL_TEST_MAKI must be absolute")
	}
	models := catalogModels(t)
	t.Run("image-tool", func(t *testing.T) {
		for _, model := range models {
			if model.ID == RecommendedModel {
				model.Vision = true
				testNativeMaki(t, executable, []textmodel.Model{model}, "", "reasoning_effort", "xhigh", true)
				return
			}
		}
		t.Fatal("missing native image fixture model")
	})
	t.Run("complete-catalog", func(t *testing.T) {
		// Keep the expected default first for transport assertions, but export
		// every model together to exercise native TOML array-table association.
		var ordered []textmodel.Model
		for _, model := range models {
			if model.ID == RecommendedModel {
				ordered = append(ordered, model)
			}
		}
		for _, model := range models {
			if model.ID != RecommendedModel {
				ordered = append(ordered, model)
			}
		}
		testNativeMaki(t, executable, ordered, "", "reasoning_effort", "xhigh", true)
	})
	for _, test := range []struct {
		model, mode, field, value string
		authenticated             bool
	}{
		{RecommendedModel, "adaptive", "reasoning_effort", "xhigh", false},
		{RecommendedModel, "adaptive", "reasoning_effort", "xhigh", true},
		{RecommendedModel, "off", "reasoning_effort", "none", true},
		{RecommendedModel, "low", "reasoning_effort", "low", true},
		{RecommendedModel, "medium", "reasoning_effort", "medium", true},
		{RecommendedModel, "high", "reasoning_effort", "medium", true},
		{RecommendedModel, "xhigh", "reasoning_effort", "xhigh", true},
		{"unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl", "adaptive", "reasoning_effort", "xhigh", true},
		{"unsloth-qwen3.8-27b-ud-q8-k-xl", "adaptive", "reasoning_effort", "medium", true},
		{"huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl", "adaptive", "reasoning_effort", "medium", true},
		{"huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl", "off", "reasoning_effort", "none", true},
		{"huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl", "xhigh", "reasoning_effort", "xhigh", true},
		{"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl", "adaptive", "reasoning_effort", "medium", true},
		{"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl-dflash2", "adaptive", "reasoning_effort", "medium", true},
		{"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl-dflash2", "", "reasoning_effort", "xhigh", true},
		{"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl-dflash2", "off", "reasoning_effort", "none", true},
		{"huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl-dflash2", "xhigh", "reasoning_effort", "xhigh", true},
	} {
		t.Run(fmt.Sprintf("%s/%s/auth=%t", test.model, test.mode, test.authenticated), func(t *testing.T) {
			var selected []textmodel.Model
			for _, model := range models {
				if model.ID == test.model {
					selected = append(selected, model)
				}
			}
			if len(selected) != 1 {
				t.Fatal("missing catalog fixture")
			}
			testNativeMaki(t, executable, selected, test.mode, test.field, test.value, test.authenticated)
		})
	}
}

func testNativeMaki(t *testing.T, executable string, models []textmodel.Model, mode, field, expected string, authenticated bool) {
	const secret = "native-fixture-0123456789abcdef0123456789abcdef"
	vision := models[0].Vision
	var fixture bytes.Buffer
	if vision {
		picture := image.NewRGBA(image.Rect(0, 0, 32, 24))
		for y := 0; y < 24; y++ {
			for x := 0; x < 32; x++ {
				picture.SetRGBA(x, y, color.RGBA{R: 210, G: uint8(x * 7), B: uint8(y * 9), A: 255})
			}
		}
		if err := png.Encode(&fixture, picture); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var bodies []map[string]any
	var files MakiFiles
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authenticated && r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("missing client credential")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/providers.toml":
			_, _ = w.Write(files.Providers)
		case "/init.lua":
			_, _ = w.Write(files.Init)
		case "/v1/chat/completions":
			if r.Header.Get("X-Paracetamol-Session-ID") != "" {
				t.Error("export invented a shared session identifier")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			bodies = append(bodies, body)
			count := len(bodies)
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			if count == 1 {
				tool, arguments := "bash", `{"command":"printf MAKI_TOOL_OK"}`
				if vision {
					tool, arguments = "view_image", `{"path":"fixture.png"}`
				}
				delta, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "reasoning_content": "fixture reasoning", "tool_calls": []any{map[string]any{"index": 0, "id": "tool-fixture", "type": "function", "function": map[string]any{"name": tool, "arguments": arguments}}}}, "finish_reason": nil}}})
				fmt.Fprintf(w, "data: %s\n\n", delta)
				fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`)
			} else {
				fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"MAKI_NATIVE_OK"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}

data: [DONE]

`)
			}
		default:
			t.Errorf("unexpected discovery/request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var err error
	files, err = Maki(models, server.URL+"/v1", authenticated)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if vision {
		if err := os.WriteFile(filepath.Join(root, "fixture.png"), fixture.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configuration := filepath.Join(root, "config", "maki")
	if err := os.MkdirAll(configuration, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"providers.toml", "init.lua"} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/"+filename, nil)
		if authenticated {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("download: %v %d", err, response.StatusCode)
		}
		if strings.Contains(string(contents), secret) {
			t.Fatal("exported a credential")
		}
		// An empty mode exercises the generated startup default unchanged.
		if filename == "init.lua" && mode != "" {
			contents = []byte(strings.Replace(string(contents), `always_thinking = "xhigh"`, `always_thinking = "`+mode+`"`, 1))
		}
		if err := os.WriteFile(filepath.Join(configuration, filename), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	environment := []string{
		"HOME=" + root, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_DATA_HOME=" + filepath.Join(root, "data"), "XDG_STATE_HOME=" + filepath.Join(root, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "PATH=" + filepath.Dir(executable) + ":/usr/bin:/bin", "TERM=dumb",
	}
	if authenticated {
		environment = append(environment, "PARACETAMOL_API_KEY="+secret)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	run := func(arguments ...string) []byte {
		command := exec.CommandContext(ctx, executable, arguments...)
		command.Env, command.Dir = environment, root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Maki %v: %v\n%s", arguments, err, output)
		}
		if strings.Contains(string(output), "warning:") || strings.Contains(string(output), "migrate providers") {
			t.Fatalf("Maki warning: %s", output)
		}
		return output
	}
	listing := run("models")
	for _, model := range models {
		if strings.Contains(string(listing), model.ID) != model.AgentTools {
			t.Fatalf("wrong picker eligibility for %s: %s", model.ID, listing)
		}
	}
	output := run("--print", "--yolo", "--max-turns", "2", "Reply MAKI_NATIVE_OK")
	if !strings.Contains(string(output), "MAKI_NATIVE_OK") {
		t.Fatalf("incomplete tool round trip: %s", output)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests=%d", len(bodies))
	}
	for _, body := range bodies {
		if body["model"] != models[0].ID || body[field] != expected {
			t.Fatalf("wrong model/reasoning: %v", body)
		}
		for _, sampler := range []string{"temperature", "top_p", "top_k", "min_p", "presence_penalty", "repeat_penalty", "thinking_budget", "draft_sampling"} {
			if _, ok := body[sampler]; ok {
				t.Fatalf("native Maki overrode sampling: %s", sampler)
			}
		}
	}
	replay, _ := json.Marshal(bodies[1]["messages"])
	required := []string{`"role":"tool"`, `"reasoning_content":"fixture reasoning"`}
	if !vision {
		required = append(required, "MAKI_TOOL_OK")
	}
	for _, required := range required {
		if !strings.Contains(string(replay), required) {
			t.Fatalf("missing replay %s: %s", required, replay)
		}
	}
	if vision {
		// Exercise native view_image and OpenAI conversion, not an invented
		// image result. Small fixtures avoid the client's resizing boundary.
		var messages []struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(replay, &messages); err != nil {
			t.Fatal(err)
		}
		images := 0
		for _, message := range messages {
			var parts []struct {
				Type     string `json:"type"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			}
			if json.Unmarshal(message.Content, &parts) != nil {
				continue
			}
			for _, part := range parts {
				if part.Type != "image_url" {
					continue
				}
				images++
				if !strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,") {
					t.Fatal("native image is not an inline PNG")
				}
				decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(part.ImageURL.URL, "data:image/png;base64,"))
				picture, errImage := png.Decode(bytes.NewReader(decoded))
				if err != nil || errImage != nil || picture.Bounds() != image.Rect(0, 0, 32, 24) {
					t.Fatal("native image lost its decoded dimensions")
				}
				if r, g, b, a := picture.At(17, 13).RGBA(); r != 210*257 || g != 119*257 || b != 117*257 || a != 65535 {
					t.Fatal("native image pixels changed")
				}
			}
		}
		if images != 1 {
			t.Fatalf("native replay has %d images, want one", images)
		}
	}
}
