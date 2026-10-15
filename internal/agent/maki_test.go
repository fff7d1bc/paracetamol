package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paracetamol/internal/catalog"
	"paracetamol/internal/gateway"
	"paracetamol/internal/textmodel"
)

func TestMakiPermissionsLimitEgressToSelectedAuthority(t *testing.T) {
	for endpoint, authority := range map[string]string{
		"http://gateway.test:7455/v1":  "gateway.test:7455",
		"https://gateway.example/v1": "gateway.example:443",
		"http://127.0.0.1/v1":        "127.0.0.1:80",
		"http://[::1]:7455/v1":       "[::1]:7455",
	} {
		manifest := string(makiPermissions(endpoint))
		if !strings.Contains(manifest, `net_hosts = ["`+authority+`"]`) {
			t.Fatalf("%s: %s", endpoint, manifest)
		}
		for _, permission := range []string{"fs_read", "fs_write", "run", "env", "agent"} {
			if !strings.Contains(manifest, permission+" = false") {
				t.Fatalf("unnecessary permission %s: %s", permission, manifest)
			}
		}
	}
}

func TestMakiLuaLiteralPreservesJSON(t *testing.T) {
	value := `{"text":"Zażółć 日本語 \\ \" ]=] ]==]"}`
	literal := luaString(value)
	if literal != "[===["+value+"]===]" {
		t.Fatalf("unsafe Lua literal %s", literal)
	}
}

func TestMakiRetiresOnlyOwnedProviderAndPreservesSessions(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "hardlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			plan := MakiPlan{Init: []byte("-- new plugin\n"), Tiers: []byte("{}\n")}
			paths, err := PrepareMakiState(plan, root)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(paths.Config, "maki", "providers")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			old := filepath.Join(dir, ProviderID)
			other := filepath.Join(paths.State, "maki", "session-fixture")
			const contents = "private session must survive"
			if err := os.WriteFile(other, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			tiers := filepath.Join(paths.State, "maki", "model-tiers")
			const chosenTiers = `{"strong":"user/chosen-model"}`
			if err := os.WriteFile(tiers, []byte(chosenTiers), 0o600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "regular":
				err = os.WriteFile(old, []byte("#!/bin/sh\n# old credentials\n"), 0o700)
			case "symlink":
				err = os.Symlink(other, old)
			case "hardlink":
				err = os.Link(other, old)
			case "directory":
				err = os.Mkdir(old, 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = PrepareMakiState(plan, root)
			if kind == "regular" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(old); !os.IsNotExist(err) {
					t.Fatalf("legacy provider remains: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsafe provider accepted")
			}
			if got, err := os.ReadFile(other); err != nil || string(got) != contents {
				t.Fatalf("session modified: %s %v", got, err)
			}
			if got, err := os.ReadFile(tiers); err != nil || string(got) != chosenTiers {
				t.Fatalf("user tier assignment modified: %s %v", got, err)
			}
		})
	}
}

func TestMakiGeneratedFilesStayPrivateAndRejectLinks(t *testing.T) {
	for _, name := range []string{"init.lua", "plugin.toml", "providers.toml"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			plan := MakiPlan{Init: []byte("fixture"), Permissions: makiPermissions("http://127.0.0.1:7455/v1"), ProviderConfig: []byte("fixture")}
			paths, err := PrepareMakiState(plan, root)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(paths.Config, "maki", name)
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("private file mode: %v", err)
			}
			outside := filepath.Join(root, "unmanaged")
			if err := os.WriteFile(outside, []byte("must survive"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareMakiState(plan, root); err == nil {
				t.Fatal("followed symlink")
			}
			if got, err := os.ReadFile(outside); err != nil || string(got) != "must survive" {
				t.Fatalf("unmanaged file changed: %v", err)
			}
		})
	}
}

func TestMakiPlanUsesVerifiedInventoryAndOfflineManagement(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "maki"), []byte("not executed"), 0o700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
		fmt.Fprintf(w, `{"data":[{"id":%q}]}`, RecommendedModel)
	}))
	defer server.Close()
	for _, args := range [][]string{nil, {"models"}, {"session", "list"}, {"trust", "list"}} {
		endpoint := "http://127.0.0.1:1/v1"
		if args == nil {
			endpoint = server.URL + "/v1"
		}
		plan, err := CreateMakiPlan(context.Background(), managed, projectRoot(t), endpoint, args, map[string]string{"PATH": bin}, "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(plan.Init), `"codec":"openai"`) || !strings.Contains(string(plan.Init), "provider.list_models = function() return {} end") {
			t.Fatal("not a static Lua provider")
		}
		if !strings.Contains(string(plan.ProviderConfig), endpoint) {
			t.Fatal("missing selected endpoint")
		}
		if args == nil {
			if plan.DefaultModel != RecommendedModel || strings.Contains(string(plan.Init), "antirez-") {
				t.Fatal("session not restricted to verified inventory")
			}
		} else if plan.Mode != "management" || !strings.Contains(string(plan.Init), "antirez-") {
			t.Fatal("offline management lost catalog")
		}
	}
}

func TestMakiEnvironmentCannotRedirectManagedGateway(t *testing.T) {
	base := map[string]string{"HOME": t.TempDir(), "PARACETAMOL_BASE_URL": "http://wrong.example/v1"}
	env, err := MakiEnvironment(base, SandboxPaths{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := env["PARACETAMOL_BASE_URL"]; ok {
		t.Fatal("host origin override escaped")
	}
	if base["PARACETAMOL_BASE_URL"] == "" {
		t.Fatal("host environment mutated")
	}
}

// Optional transport acceptance against the installed client. No model or GPU
// is used. The fake endpoint exercises real Lua loading and request encoding.
func TestInstalledMakiProvider(t *testing.T) {
	executable := os.Getenv("PARACETAMOL_TEST_MAKI")
	if executable == "" {
		t.Skip("set PARACETAMOL_TEST_MAKI to an absolute Maki executable")
	}
	if !filepath.IsAbs(executable) {
		t.Fatal("PARACETAMOL_TEST_MAKI must be absolute")
	}
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		model, mode, field string
		value              any
	}{
		{RecommendedModel, "adaptive", "reasoning_effort", "medium"},
		{RecommendedModel, "off", "reasoning_effort", "none"},
		{RecommendedModel, "low", "reasoning_effort", "low"},
		{RecommendedModel, "medium", "reasoning_effort", "medium"},
		{RecommendedModel, "high", "reasoning_effort", "medium"},
		{RecommendedModel, "xhigh", "reasoning_effort", "xhigh"},
		{"unsloth-qwen3.6-27b-mtp-q8-0", "off", "chat_template_kwargs", false},
		{"unsloth-qwen3.6-27b-mtp-q8-0", "adaptive", "chat_template_kwargs", true},
		{"unsloth-qwen3.6-27b-mtp-q8-0", "high", "chat_template_kwargs", true},
		{"unsloth-qwen3.6-35b-a3b-mtp-ud-q8-k-xl", "off", "chat_template_kwargs", false},
		{"unsloth-qwen3.6-35b-a3b-mtp-ud-q8-k-xl", "adaptive", "chat_template_kwargs", true},
		{"meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k", "off", "reasoning_strength", "low"},
		{"meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k", "adaptive", "reasoning_strength", "high"},
		{"meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k", "xhigh", "reasoning_strength", "xhigh"},
	} {
		t.Run(test.model+"/"+test.mode, func(t *testing.T) {
			testInstalledMakiProvider(t, executable, managed, test.model, test.mode, test.field, test.value)
		})
	}
}

func testInstalledMakiProvider(t *testing.T, executable string, managed catalog.Catalog, model, mode, field string, expected any) {
	const key = "fixture-key-0123456789abcdef0123456789abcdef"
	var mu sync.Mutex
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(gateway.IdentityHeader, gateway.IdentityValue)
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("missing gateway key")
			http.Error(w, "unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprintf(w, `{"data":[{"id":%q}]}`, model)
		case "/v1/chat/completions":
			if r.Header.Get("X-Paracetamol-Session-ID") == "" {
				t.Error("missing correlation header")
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
				fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"fixture reasoning","tool_calls":[{"index":0,"id":"tool-fixture","type":"function","function":{"name":"bash","arguments":"{\"command\":\"printf MAKI_TOOL_OK\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`)
				return
			}
			fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"MAKI_PLUGIN_OK"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}

data: [DONE]

`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	environment := map[string]string{"HOME": t.TempDir(), "PATH": filepath.Dir(executable) + ":/usr/bin:/bin"}
	plan, err := CreateMakiPlan(ctx, managed, projectRoot(t), server.URL+"/v1", []string{"--print", "--yolo", "--max-turns", "2", "Reply MAKI_PLUGIN_OK"}, environment, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plan.Init), "antirez-") {
		t.Fatal("unadvertised model leaked")
	}
	if mode != "adaptive" {
		plan.Init = []byte(strings.Replace(string(plan.Init), `"always_thinking":"adaptive"`, `"always_thinking":"`+mode+`"`, 1))
	}
	paths, err := PrepareMakiState(plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	environment, err = MakiEnvironment(environment, paths)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, plan.Command[0], plan.Command[1:]...)
	command.Dir = t.TempDir()
	for name, value := range environment {
		command.Env = append(command.Env, name+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "MAKI_PLUGIN_OK") {
		t.Fatalf("Maki: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "warning:") || strings.Contains(string(output), "migrate providers") {
		t.Fatalf("Maki warning: %s", output)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests=%d", len(bodies))
	}
	body := bodies[0]
	actual := body[field]
	if field == "chat_template_kwargs" {
		if nested, ok := actual.(map[string]any); ok {
			actual = nested["enable_thinking"]
		}
	}
	if body["model"] != model || actual != expected {
		t.Fatalf("wrong model/reasoning: %v", body)
	}
	for _, field := range []string{"temperature", "top_p", "top_k", "min_p", "thinking_budget"} {
		if _, ok := body[field]; ok {
			t.Fatalf("client overrode gateway sampling: %s", field)
		}
	}
	replay, _ := json.Marshal(bodies[1]["messages"])
	for _, required := range []string{"MAKI_TOOL_OK", `"role":"tool"`, `"reasoning_content":"fixture reasoning"`} {
		if !strings.Contains(string(replay), required) {
			t.Fatalf("tool/reasoning replay missing %s: %s", required, replay)
		}
	}
}

func TestMakiModelRowsMatchEveryReviewedModel(t *testing.T) {
	managed, err := catalog.Load(filepath.Join(projectRoot(t), "catalog", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	models, err := AgentModels(managed, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := makiModels(models)
	for i, model := range models {
		row := rows[i]
		if row["prefixes"].([]string)[0] != model.ID || row["context_window"] != model.Context || row["max_output_tokens"] != model.MaxOutputTokens {
			t.Fatalf("wrong row: %v", row)
		}
		if _, legacy := row["id"]; legacy {
			t.Fatalf("legacy script row: %v", row)
		}
		if row["supports_thinking"] != (model.ReasoningControl != "") {
			t.Fatalf("wrong support: %v", row)
		}
		if model.ReasoningControl == "" {
			continue
		}
		fields := row["thinking_fields"].(map[string]any)
		if fields["adaptive"] == nil {
			t.Fatalf("missing default: %s", model.ID)
		}
		for _, level := range textmodel.ReasoningLevels(model) {
			if model.ReasoningControl != "toggle" && fields[level] == nil {
				t.Fatalf("missing %s for %s", level, model.ID)
			}
		}
	}
}
