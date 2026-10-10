package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/benchmark"
	"paracetamol/internal/catalog"
	"paracetamol/internal/content"
	"paracetamol/internal/identity"
	"paracetamol/internal/podman"
	"paracetamol/internal/runtime"
	"paracetamol/internal/storage"
)

// Use a distinct container and its returned ID for cleanup. Acceptance must
// not reclaim a user's direct server or a gateway-owned allocation.
func (app *App) runGufoAcceptance(managed catalog.Catalog, dataRoot, profile, renderNode, suiteID string) (paths []string, returned error) {
	preset := managed.GufoPresets[defaultGufoPreset]
	options := runtime.GufoOptions{
		Image: configApplicationImage("gufo"), DataDir: dataRoot, Profile: profile,
		RenderNodes: []string{renderNode}, ModelID: preset.ID,
		Model:   content.ArtifactPath(dataRoot, managed.Artifacts[preset.Artifact]),
		Context: preset.DefaultContext, OutputTokens: preset.MaxOutputTokens,
		CacheRAMBytes: preset.CacheRAMBytes, Reasoning: preset.ReasoningDefault,
		ContainerName: identity.Container("acceptance-gufo-" + suiteID), ContainerRole: "acceptance",
		Detach: true, DynamicHostPort: true, Listen: "127.0.0.1", Port: 8080,
	}
	for _, id := range managed.Bundles[preset.Bundle].Artifacts {
		options.ModelFiles = append(options.ModelFiles, content.ArtifactPath(dataRoot, managed.Artifacts[id]))
	}
	command, err := runtime.GufoCommand(options, app.podman().SELinuxVolumeSuffix(app.Context))
	if err != nil {
		return nil, err
	}
	if err := (storage.Layout{Root: dataRoot}).PrepareRuntime("gufo"); err != nil {
		return nil, err
	}
	result, err := app.run(command, true)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(string(result.Stdout))
	if len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
		return nil, fmt.Errorf("Gufo acceptance returned an invalid container ID")
	}
	defer func() {
		returned = withCleanupFailure(returned, "remove Gufo acceptance container", app.podman().RemoveContainer(contextWithoutCancel(), id, 30, podman.Streams{}))
	}()
	port, err := app.podman().PublishedLoopbackPort(app.Context, id, options.Port)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	readyContext, cancel := context.WithTimeout(app.Context, 30*time.Minute)
	defer cancel()
	if err := benchmark.WaitForURL(readyContext, endpoint+"/ready"); err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"model": preset.ID, "messages": []map[string]string{{"role": "user", "content": "Reply with exactly GUFO_ACCEPTANCE_OK and nothing else."}}, "reasoning_effort": "none", "temperature": 0, "max_tokens": 64})
	request, err := http.NewRequestWithContext(app.Context, http.MethodPost, endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	// Inference follows the command's cancellation context, not a short probe deadline.
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var answer struct {
		Choices []struct {
			Message struct {
				Content   string
				Reasoning string `json:"reasoning_content"`
			}
		}
	}
	if err := json.Unmarshal(raw, &answer); err != nil || response.StatusCode != http.StatusOK || len(answer.Choices) != 1 || strings.TrimSpace(answer.Choices[0].Message.Content) != "GUFO_ACCEPTANCE_OK" || answer.Choices[0].Message.Reasoning != "" {
		return nil, fmt.Errorf("Gufo acceptance returned an unexpected answer (HTTP %d): %.2048s", response.StatusCode, raw)
	}
	path := filepath.Join((storage.Layout{Root: dataRoot}).AcceptanceResults(), "cases", suiteID+"-gufo.json")
	if err := atomicfile.Write(path, raw, 0600, atomicfile.Create); err != nil {
		return nil, err
	}
	return []string{path}, nil
}
