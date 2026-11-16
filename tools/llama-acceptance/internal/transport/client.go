// Package transport drives the ordinary production gateway. It neither starts
// backends nor rewrites their policy, and never treats transport success as
// evidence of a complete model answer.
package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/catalog"
	"paracetamol/internal/config"
	"paracetamol/internal/gateway"
	"paracetamol/internal/project"
	"paracetamol/tools/llama-acceptance/internal/contract"
)

const maxResponse = 16 * 1024 * 1024

const (
	Protocol       = "protocol"
	Infrastructure = "infrastructure"
	Policy         = "policy"
	OutputLimit    = "output-limit"
)

// Error retains a classification without discarding cancellation/error identity.
type Error struct {
	Kind string
	Err  error
}

func (e *Error) Error() string          { return e.Kind + ": " + e.Err.Error() }
func (e *Error) Unwrap() error          { return e.Err }
func failure(kind, detail string) error { return &Error{Kind: kind, Err: errors.New(detail)} }
func wrap(kind string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Err: err}
}

// Options are operator-selected inputs, not attestation. In particular the
// caller must verify native endpoint container ownership before supplying it.
// The key is never written to evidence. Catalog nil selects the current checkout.
type Options struct {
	GatewayURL string
	Key        string
	OutputDir  string
	Model      string
	Catalog    *catalog.Catalog
	NativeURL  string
}

type Client struct {
	endpoint, key, output, model, native string
	instance, inventory                  string
	preset                               catalog.LlamaPreset
	policy                               catalog.SamplingPolicy
	http                                 *http.Client
	nativeSequence                       atomic.Uint64
}

type Info struct {
	InstanceID           string `json:"instance_id"`
	InventoryFingerprint string `json:"inventory_fingerprint"`
	Model                string `json:"model"`
}

func (client *Client) Info() Info {
	return Info{InstanceID: client.instance, InventoryFingerprint: client.inventory, Model: client.model}
}

// New performs read-only metadata preflight. It creates no output directory and
// performs no inference, model installation, container start or native mutation.
func New(ctx context.Context, options Options) (*Client, error) {
	endpoint, err := config.NormalizeGatewayURL(options.GatewayURL)
	if err != nil {
		return nil, wrap(Infrastructure, err)
	}
	if err := validateNative(strings.TrimSuffix(endpoint, "/v1")); err != nil {
		return nil, failure(Infrastructure, "acceptance gateway must be a numeric loopback HTTP origin ending in /v1")
	}
	if options.Key != "" {
		if err := config.ValidateGatewayKey(options.Key); err != nil {
			return nil, wrap(Infrastructure, err)
		}
	}
	if options.OutputDir != "" {
		info, err := os.Lstat(options.OutputDir)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return nil, failure(Infrastructure, "output must be an existing private directory, not a symlink")
		}
	}
	managed := options.Catalog
	if managed == nil {
		root, err := project.Root()
		if err != nil {
			return nil, wrap(Infrastructure, err)
		}
		loaded, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
		if err != nil {
			return nil, wrap(Infrastructure, err)
		}
		managed = &loaded
	}
	preset, ok := managed.LlamaPresets[options.Model]
	if !ok || preset.SamplingPolicy == "" {
		return nil, failure(Policy, "model requires a catalog-owned sampling policy")
	}
	policy, ok := managed.SamplingPolicies[preset.SamplingPolicy]
	if !ok || len(policy.Thinking) != 6 || len(policy.NonThinking) != 6 {
		return nil, failure(Policy, "model requires complete thinking and non-thinking sampler policies")
	}
	policy.Thinking, policy.NonThinking = policy.Defaults(true), policy.Defaults(false)
	client := &Client{endpoint: endpoint, key: options.Key,
		output: options.OutputDir, model: options.Model, preset: preset, policy: policy,
		http: &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	// This dedicated transport has no overall or response-header inference
	// timeout. Bounded metadata probes use their own contexts, not this client.
	if options.NativeURL != "" {
		if err := validateNative(options.NativeURL); err != nil {
			client.Close()
			return nil, wrap(Infrastructure, err)
		}
		client.native = options.NativeURL
	}
	ids, err := gateway.FetchModelIDs(ctx, endpoint, options.Key)
	if err != nil {
		client.Close()
		return nil, wrap(Infrastructure, err)
	}
	if !slices.Contains(ids, options.Model) {
		client.Close()
		return nil, failure(Policy, "expected model is absent from gateway inventory")
	}
	status, err := gateway.FetchStatus(ctx, endpoint, options.Key, 0)
	if err != nil {
		client.Close()
		return nil, wrap(Infrastructure, err)
	}
	if !hexIdentity(status.InstanceID, 32) || !hexIdentity(status.InventoryFingerprint, 64) {
		client.Close()
		return nil, failure(Protocol, "gateway has no canonical instance/inventory identity")
	}
	found := 0
	for _, model := range status.Models {
		if model.ID == options.Model {
			found++
			if model.Application != "llama-cpp" || model.ConfiguredContext != preset.DefaultContext {
				client.Close()
				return nil, failure(Policy, "gateway model application/context differs from catalog")
			}
		}
	}
	if found != 1 {
		client.Close()
		return nil, failure(Policy, "gateway status does not identify exactly one expected model")
	}
	client.instance, client.inventory = status.InstanceID, status.InventoryFingerprint
	return client, nil
}

func hexIdentity(value string, length int) bool {
	if len(value) != length || strings.Trim(value, "0") == "" {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (client *Client) Close() { client.http.CloseIdleConnections() }

func (client *Client) save(name string, raw []byte) error {
	return wrap(Infrastructure, atomicfile.Write(filepath.Join(client.output, name), raw, 0600, atomicfile.Create))
}

func (client *Client) saveJSON(name string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return wrap(Infrastructure, err)
	}
	return client.save(name, append(raw, '\n'))
}

// Call saves exact synthetic request/response bytes and the correlated report.
// A reused evidence name fails before contacting inference. Cancellation keeps
// the partial reply and makes one bounded, independent report recovery attempt.
func (client *Client) Call(ctx context.Context, name string, payload map[string]any) (answer contract.Response, callErr error) {
	if client.output == "" {
		return answer, failure(Infrastructure, "inference requires an existing private evidence directory")
	}
	if name == "" || len(name) > 96 || strings.IndexFunc(name, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') }) >= 0 {
		return answer, failure(Infrastructure, "evidence name must contain only lowercase letters, digits, underscore and hyphen")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return answer, wrap(Infrastructure, err)
	}
	request, err := object(raw)
	if err != nil {
		return answer, wrap(Protocol, err)
	}
	if model, exists := request["model"]; exists && model != client.model {
		return answer, failure(Policy, "request model differs from selected acceptance model")
	}
	request["model"] = client.model
	stream := false
	if value, exists := request["stream"]; exists {
		var ok bool
		stream, ok = value.(bool)
		if !ok {
			return answer, failure(Protocol, "request stream must be boolean")
		}
	}
	if _, err := client.expectedMode(request); err != nil {
		return answer, wrap(Policy, err)
	}
	body, _ := json.Marshal(request)
	for _, suffix := range []string{".response.json", ".response.sse", ".report.json", ".correlation.json"} {
		if _, err := os.Lstat(filepath.Join(client.output, name+suffix)); !os.IsNotExist(err) {
			return answer, failure(Infrastructure, "evidence name already has response/report artifacts or cannot be inspected")
		}
	}
	if err := client.save(name+".request.json", body); err != nil {
		return answer, err
	}
	var opaque [16]byte
	if _, err := rand.Read(opaque[:]); err != nil {
		return answer, wrap(Infrastructure, err)
	}
	opaque[6], opaque[8] = opaque[6]&0x0f|0x40, opaque[8]&0x3f|0x80
	session := fmt.Sprintf("%x-%x-%x-%x-%x", opaque[:4], opaque[4:6], opaque[6:8], opaque[8:10], opaque[10:])
	if err := client.saveJSON(name+".correlation.json", map[string]string{"session_id": session}); err != nil {
		return answer, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return answer, wrap(Infrastructure, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set(gateway.SessionIDHeader, session)
	if client.key != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+client.key)
	}
	response, err := client.http.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			answer, recoverErr := client.recoverCanceled(ctx, name, session, stream, request)
			return answer, errors.Join(wrap(Infrastructure, ctx.Err()), recoverErr)
		}
		return answer, wrap(Infrastructure, err)
	}
	defer response.Body.Close()
	answer.RequestID = response.Header.Get(gateway.RequestIDHeader)
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	oversized := len(raw) > maxResponse
	if oversized {
		raw = raw[:maxResponse]
		readErr = errors.Join(readErr, errors.New("response exceeds 16 MiB; only prefix retained"))
	}
	suffix := ".response.json"
	if stream {
		suffix = ".response.sse"
	}
	// A truncated body is expected after intentional cancellation. Header,
	// policy, identity and persistence failures are not: keep them independent
	// so context.Canceled cannot accidentally authorize a passing cancellation.
	validationErr := client.save(name+suffix, raw)
	callErr = validationErr
	invalid := func(err error) {
		validationErr = errors.Join(validationErr, err)
		callErr = errors.Join(callErr, err)
	}
	if oversized {
		invalid(failure(Protocol, "response exceeded the evidence bound"))
	}
	if readErr != nil {
		callErr = errors.Join(callErr, wrap(Protocol, readErr))
	}
	if response.StatusCode != http.StatusOK {
		invalid(failure(Protocol, fmt.Sprintf("gateway HTTP status %d", response.StatusCode)))
	}
	if markers := response.Header.Values(gateway.IdentityHeader); len(markers) != 1 || markers[0] != gateway.IdentityValue {
		invalid(failure(Protocol, "response is not uniquely gateway-marked"))
	}
	if ids := response.Header.Values(gateway.RequestIDHeader); len(ids) != 1 || len(answer.RequestID) != 50 || !strings.HasPrefix(answer.RequestID, "r"+client.instance+"-") {
		invalid(failure(Protocol, "response request ID differs from preflight gateway epoch"))
	}
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || !stream && media != "application/json" || stream && media != "text/event-stream" {
		invalid(failure(Protocol, "response content type does not match requested transport"))
	}
	parsed, parseErr := parse(raw, stream)
	parsed.RequestID = answer.RequestID
	answer = parsed
	callErr = errors.Join(callErr, parseErr)
	canceledReportValid := false
	if answer.RequestID != "" {
		// Even after cancellation, fetch the canceled request's eventual report.
		// This context is bounded and used only for metadata, never inference.
		reportCtx := ctx
		if ctx.Err() != nil {
			reportCtx = context.WithoutCancel(ctx)
		}
		report, reportErr := client.completed(reportCtx, answer.RequestID)
		answer.Report = report
		if report.Schema != "" {
			invalid(client.saveJSON(name+".report.json", report))
		}
		invalid(reportErr)
		if reportErr == nil {
			invalid(client.validateCorrelation(report, answer, stream))
			if report.Record != nil && report.Record.SessionID != session {
				invalid(failure(Protocol, "request report session correlation mismatch"))
			}
			if ctx.Err() == nil {
				invalid(client.validateReport(report, answer, request, stream))
			} else {
				err := client.validateCanceled(report, answer, request, stream, session)
				invalid(err)
				canceledReportValid = err == nil
			}
		}
	}
	if ctx.Err() != nil {
		if readErr != nil && !errors.Is(readErr, ctx.Err()) {
			invalid(wrap(Protocol, readErr))
		}
		if parseErr != nil && !cancellationTruncation(parseErr) {
			invalid(parseErr)
		}
		answer.CancellationVerified = canceledReportValid && validationErr == nil
		// Truncated body/parser errors are expected interruption evidence, not
		// independent protocol failures. validationErr retains every unexpected
		// body/parser error and all policy, correlation and persistence failures.
		callErr = errors.Join(wrap(Infrastructure, ctx.Err()), validationErr)
	}
	return answer, callErr
}

// A non-streaming reply may not have sent HTTP headers when cancellation
// occurs. Production's opaque session metadata can recover its exact request
// identity without guessing from timing, prompts, model name or native slots.
func (client *Client) recoverCanceled(ctx context.Context, name, session string, stream bool, request map[string]any) (contract.Response, error) {
	answer := contract.Response{}
	suffix := ".response.json"
	if stream {
		suffix = ".response.sse"
	}
	if err := client.save(name+suffix, nil); err != nil {
		return answer, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for {
		status, err := gateway.FetchStatus(ctx, client.endpoint, client.key, gateway.RecentRequestLimit)
		if err != nil {
			return answer, wrap(Infrastructure, err)
		}
		if status.InstanceID != client.instance || status.InventoryFingerprint != client.inventory {
			return answer, failure(Protocol, "gateway identity changed while recovering canceled request")
		}
		matches := 0
		for _, record := range status.RecentRequests {
			if record.SessionID == session {
				matches++
				answer.RequestID = record.ID
			}
		}
		if matches > 1 {
			return answer, failure(Protocol, "ambiguous canceled request session correlation")
		}
		if matches == 1 {
			report, err := client.completed(ctx, answer.RequestID)
			answer.Report = report
			if report.Schema != "" {
				err = errors.Join(err, client.saveJSON(name+".report.json", report))
			}
			if err != nil {
				return answer, err
			}
			if err := client.validateCanceled(report, answer, request, stream, session); err != nil {
				return answer, err
			}
			answer.CancellationVerified = true
			return answer, nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return answer, wrap(Infrastructure, fmt.Errorf("canceled request ID unavailable: %w", ctx.Err()))
		case <-timer.C:
		}
	}
}

func cancellationTruncation(err error) bool {
	// Ignore only the parser's incomplete-body observations, never an error
	// envelope, duplicate field or malformed complete event merely because the
	// caller also canceled. Keep this list aligned with the parser contract.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var classified *Error
	if errors.As(err, &classified) {
		switch classified.Err.Error() {
		case "incomplete/unterminated SSE completion", "incomplete JSON object", "incomplete JSON array":
			return true
		}
	}
	return false
}

func (client *Client) validateCanceled(report gateway.RequestReport, response contract.Response, request map[string]any, stream bool, session string) error {
	if err := client.validateCorrelation(report, response, stream); err != nil {
		return err
	}
	if report.Record.SessionID != session || report.Record.Outcome != gateway.OutcomeCanceled || report.Record.HTTPStatus != 0 && report.Record.HTTPStatus != http.StatusOK {
		return failure(Protocol, "request does not establish the expected session cancellation")
	}
	return client.validatePolicy(report, request)
}

func (client *Client) completed(ctx context.Context, id string) (gateway.RequestReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var last gateway.RequestReport
	for {
		report, err := gateway.FetchRequestReport(ctx, client.endpoint, client.key, id)
		if err != nil && !errors.Is(err, gateway.ErrRequestNotRetained) {
			return last, wrap(Infrastructure, err)
		}
		if err == nil {
			last = report
			if report.State == "completed" {
				return report, nil
			}
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, wrap(Infrastructure, fmt.Errorf("completed request report unavailable: %w", ctx.Err()))
		case <-timer.C:
		}
	}
}

// expectedMode is an acceptance oracle for explicit thinking controls, not a
// request rewriter. It refuses ambiguous controls rather than choosing a winner.
func (client *Client) expectedMode(request map[string]any) (string, error) {
	thinking := client.preset.ReasoningControl != "" && client.preset.ReasoningDefault != "off" && client.preset.ReasoningDefault != "none"
	selected := false
	selectMode := func(next bool) error {
		if selected && thinking != next {
			return errors.New("conflicting thinking selections")
		}
		if !next && !client.preset.ReasoningOff {
			return errors.New("model does not support thinking off")
		}
		thinking, selected = next, true
		return nil
	}
	effort := ""
	selectEffort := func(value any, top bool) error {
		text, ok := value.(string)
		if !ok {
			return errors.New("reasoning effort must be a string")
		}
		if top && text == "none" {
			return selectMode(false)
		}
		levels := client.preset.ReasoningLevels
		if client.preset.ReasoningControl == "toggle" {
			levels = []string{"minimal", "low", "medium", "high", "xhigh", "max"}
		}
		if !slices.Contains(levels, text) || effort != "" && effort != text && client.preset.ReasoningControl != "toggle" {
			return errors.New("unsupported or conflicting reasoning effort")
		}
		effort = text
		return selectMode(true)
	}
	if value, exists := request["reasoning_effort"]; exists {
		if err := selectEffort(value, true); err != nil {
			return "", err
		}
	}
	if value, exists := request["chat_template_kwargs"]; exists {
		kwargs, ok := value.(map[string]any)
		if !ok {
			return "", errors.New("chat template controls must be an object")
		}
		if value, exists := kwargs["enable_thinking"]; exists {
			enabled, ok := value.(bool)
			if !ok {
				return "", errors.New("enable_thinking must be boolean")
			}
			if err := selectMode(enabled); err != nil {
				return "", err
			}
		}
		if value, exists := kwargs["reasoning_effort"]; exists {
			if err := selectEffort(value, false); err != nil {
				return "", err
			}
		}
	}
	if thinking {
		return "thinking", nil
	}
	return "non_thinking", nil
}

func (client *Client) validateReport(report gateway.RequestReport, response contract.Response, request map[string]any, stream bool) error {
	if err := client.validateCorrelation(report, response, stream); err != nil {
		return err
	}
	r := report.Record
	if r.Outcome != gateway.OutcomeSucceeded || r.HTTPStatus != http.StatusOK {
		return failure(Protocol, "gateway request outcome was not successful")
	}
	c := r.Completion
	if c.Observation != gateway.CompletionAvailable || len(c.FinishReasons) != 1 || c.FinishReasons[0].Index != 0 || c.FinishReasons[0].FinishReason != response.Finish || stream && (c.StreamDone == nil || !*c.StreamDone) {
		return failure(Protocol, "gateway report does not establish the same completed answer")
	}
	if err := client.validatePolicy(report, request); err != nil {
		return err
	}
	_, err := Metrics(response)
	return err
}

func (client *Client) validateCorrelation(report gateway.RequestReport, response contract.Response, stream bool) error {
	r := report.Record
	if report.Schema != gateway.RequestReportSchema || report.State != "completed" || r == nil || report.InstanceID != client.instance || report.InventoryFingerprint != client.inventory || r.ID != response.RequestID || r.Model != client.model || r.Application != "llama-cpp" || r.Stream != stream {
		return failure(Protocol, "request report identity, inventory or request correlation mismatch")
	}
	return nil
}

func (client *Client) validatePolicy(report gateway.RequestReport, request map[string]any) error {
	r := report.Record
	mode, err := client.expectedMode(request)
	if err != nil {
		return wrap(Policy, err)
	}
	controls := r.Controls
	if controls.SamplingPolicy != client.preset.SamplingPolicy || controls.SamplingMode != mode || len(controls.Diagnostics) != 0 || len(controls.Sampling) != 6 || len(controls.Reasoning) != 5 {
		return failure(Policy, "gateway sampling policy/mode differs from expected catalog policy")
	}
	defaults, seen := client.policy.Defaults(mode == "thinking"), map[string]bool{}
	for _, control := range controls.Sampling {
		want, exists := defaults[control.Name]
		if !exists || seen[control.Name] {
			return failure(Policy, "unknown or repeated sampler provenance")
		}
		seen[control.Name] = true
		source := gateway.ControlGatewayDefault
		_, provided := request[control.Name]
		if explicit, exists := request[control.Name]; exists && explicit != nil {
			want, source, provided = explicit, gateway.ControlClient, true
		}
		if control.Source != source || control.Provided != provided || !equalNumber(want, control.Value) {
			return failure(Policy, "gateway sampler value/source mismatch: "+control.Name)
		}
	}
	// Explicit reasoning must be observed with client provenance. Defaults are
	// checked through the effective mode above, not guessed from visible text.
	wants := map[string]any{}
	for _, name := range []string{"reasoning_effort", "reasoning_strength"} {
		if value, ok := request[name]; ok {
			wants[name] = value
		}
	}
	if kwargs, ok := request["chat_template_kwargs"].(map[string]any); ok {
		for _, name := range []string{"enable_thinking", "reasoning_effort", "reasoning_strength"} {
			if value, ok := kwargs[name]; ok {
				wants["chat_template_kwargs."+name] = value
			}
		}
	}
	seen = map[string]bool{}
	reasoningNames := []string{"reasoning_effort", "reasoning_strength", "chat_template_kwargs.enable_thinking", "chat_template_kwargs.reasoning_effort", "chat_template_kwargs.reasoning_strength"}
	for _, control := range controls.Reasoning {
		if seen[control.Name] || !slices.Contains(reasoningNames, control.Name) {
			return failure(Policy, "duplicate reasoning provenance")
		}
		seen[control.Name] = true
		if want, exists := wants[control.Name]; exists {
			value := fmt.Sprint(want)
			if control.Source != gateway.ControlClient || !control.Provided || control.Value != value {
				return failure(Policy, "explicit reasoning provenance mismatch")
			}
			delete(wants, control.Name)
		} else if control.Provided || control.Source != gateway.ControlDefault {
			return failure(Policy, "unexpected reasoning override")
		}
	}
	if len(wants) != 0 {
		return failure(Policy, "missing explicit reasoning provenance")
	}
	return nil
}

func equalNumber(want any, value string) bool {
	raw, err := json.Marshal(want)
	if err != nil {
		return false
	}
	x, err := strconv.ParseFloat(string(raw), 64)
	y, other := strconv.ParseFloat(value, 64)
	return err == nil && other == nil && !math.IsNaN(x) && !math.IsInf(x, 0) && x == y
}

// Measurements preserves paired native counts/durations; no throughput is
// fabricated from absent counters or averaged per-request rates.
type Measurements struct {
	Input, Cached, Prompt, Generated int64
	PromptMS, GeneratedMS            float64
}

func Metrics(response contract.Response) (Measurements, error) {
	var result Measurements
	if response.Report.Record == nil {
		return result, failure(Infrastructure, "request has no metrics report")
	}
	r := response.Report.Record
	t := r.BackendTimings
	if r.Tokens.Input == nil || r.Tokens.Output == nil || r.Tokens.Cached == nil || t.PromptTokens == nil || t.GeneratedTokens == nil || t.PromptMilliseconds == nil || t.GeneratedMilliseconds == nil {
		return result, failure(Infrastructure, "required token/cache/native timing observations are missing")
	}
	result = Measurements{Input: *r.Tokens.Input, Cached: *r.Tokens.Cached, Prompt: *t.PromptTokens, Generated: *t.GeneratedTokens, PromptMS: *t.PromptMilliseconds, GeneratedMS: *t.GeneratedMilliseconds}
	if result.Input < 0 || result.Cached < 0 || result.Prompt < 0 || result.Generated < 0 || result.Cached > result.Input || result.Prompt != result.Input-result.Cached || *r.Tokens.Output != result.Generated {
		return Measurements{}, failure(Protocol, "token/cache/native work counters are inconsistent")
	}
	// Visible answers, reasoning and tool calls establish generation even when
	// counters agree with one another at zero. Prompt work may still be zero
	// when a positive input was entirely cached; do not invent new evaluation.
	if (response.Text != "" || response.Reasoning != "" || len(response.Tools) > 0) && (result.Input == 0 || result.Generated == 0) {
		return Measurements{}, failure(Protocol, "nonempty assistant generation lacks positive input/generated tokens")
	}
	for _, duration := range []float64{result.PromptMS, result.GeneratedMS} {
		if duration < 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
			return Measurements{}, failure(Protocol, "invalid native timing")
		}
	}
	if result.Prompt > 0 && result.PromptMS <= 0 || result.Generated > 0 && result.GeneratedMS <= 0 {
		return Measurements{}, failure(Protocol, "positive work lacks positive native duration")
	}
	return result, nil
}

func validateNative(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("native diagnostics require a numeric loopback HTTP origin")
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	if u.Scheme != "http" || u.Opaque != "" || u.User != nil || ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("native diagnostics require a numeric loopback HTTP origin with explicit port and no path, credentials, query or fragment")
	}
	return nil
}

// Slots probes only the read-only native route supplied by the operator. Its
// address is never inferred from a gateway report or made publicly accessible.
func (client *Client) Slots(ctx context.Context) ([]contract.Slot, error) {
	if client.native == "" {
		return nil, failure(Infrastructure, "exact-owned native slot diagnostic endpoint is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, client.native+"/slots?model="+url.QueryEscape(client.model), nil)
	response, err := client.http.Do(request)
	if err != nil {
		return nil, wrap(Infrastructure, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if client.output != "" {
		if len(raw) > 1024*1024 {
			raw = raw[:1024*1024]
			err = errors.Join(err, errors.New("native slots exceeded size bound; prefix only"))
		}
		if saveErr := client.save(fmt.Sprintf("slots-%06d.json", client.nativeSequence.Add(1)), raw); saveErr != nil {
			return nil, saveErr
		}
	}
	if err != nil || len(raw) > 1024*1024 || response.StatusCode != http.StatusOK {
		return nil, failure(Infrastructure, "native slot observation failed or exceeded size bound")
	}
	return parseSlots(raw)
}

// Tokenize calibrates a synthetic corpus against the exact selected native
// tokenizer. It generates no tokens and makes no estimate from byte length.
func (client *Client) Tokenize(ctx context.Context, text string) (int, error) {
	if client.native == "" {
		return 0, failure(Infrastructure, "exact-owned native tokenizer endpoint is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, err := json.Marshal(map[string]any{"content": text, "add_special": false, "model": client.model})
	if err != nil {
		return 0, wrap(Infrastructure, err)
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, client.native+"/tokenize?model="+url.QueryEscape(client.model), bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return 0, wrap(Infrastructure, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse || response.StatusCode != http.StatusOK {
		return 0, failure(Infrastructure, "native tokenization failed or exceeded response bound")
	}
	value, err := object(raw)
	if err != nil {
		return 0, wrap(Protocol, err)
	}
	if value["error"] != nil {
		return 0, failure(Protocol, "native tokenizer returned an error envelope")
	}
	tokens, ok := value["tokens"].([]any)
	if !ok {
		return 0, failure(Protocol, "native tokenizer did not return token IDs")
	}
	for _, value := range tokens {
		if token, ok := integer(value); !ok || token < 0 {
			return 0, failure(Protocol, "native tokenizer returned invalid token ID")
		}
	}
	if text != "" && len(tokens) == 0 {
		return 0, failure(Protocol, "native tokenizer returned no tokens for nonempty text")
	}
	return len(tokens), nil
}
