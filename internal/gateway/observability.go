package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	RecentRequestLimit       = 64
	RequestIDHeader          = "X-Paracetamol-Request-ID"
	SessionIDHeader          = "X-Paracetamol-Session-ID"
	maxObservedTextRunes     = 160
	maxObservedValueRunes    = 64
	maxObservedJSONBytes     = 1024 * 1024
	maxObservedSSEEventBytes = 256 * 1024
)

type RequestOutcome string

const (
	OutcomeSucceeded          RequestOutcome = "succeeded"
	OutcomeRejected           RequestOutcome = "rejected"
	OutcomeCanceled           RequestOutcome = "canceled"
	OutcomeQueueFull          RequestOutcome = "queue-full"
	OutcomeBackendStartFailed RequestOutcome = "backend-start-failed"
	OutcomeUpstreamError      RequestOutcome = "upstream-error"
)

type ControlSource string

const (
	ControlClient  ControlSource = "client"
	ControlDefault ControlSource = "managed-or-backend-default"
	ControlInvalid ControlSource = "invalid"
)

type ObservedControl struct {
	Name     string        `json:"name"`
	Source   ControlSource `json:"source"`
	Provided bool          `json:"provided"`
	Value    string        `json:"value,omitempty"`
}

type RequestControls struct {
	Reasoning   []ObservedControl `json:"reasoning"`
	Sampling    []ObservedControl `json:"sampling"`
	Diagnostics []string          `json:"diagnostics,omitempty"`
}

type RequestTiming struct {
	GatewayWaitMilliseconds int64  `json:"gateway_wait_ms"`
	UpstreamMilliseconds    int64  `json:"upstream_ms"`
	TotalMilliseconds       int64  `json:"total_ms"`
	FirstOutputMilliseconds *int64 `json:"first_output_ms,omitempty"`
}

type TokenUsage struct {
	Input     *int64 `json:"input,omitempty"`
	Output    *int64 `json:"output,omitempty"`
	Cached    *int64 `json:"cached,omitempty"`
	Reasoning *int64 `json:"reasoning,omitempty"`
}

// BackendTimings is the closed subset of llama.cpp completion timing metadata
// retained by the gateway. Counts stay paired with their source durations so
// lifetime throughput can be derived from totals instead of averaging rates.
type BackendTimings struct {
	PromptTokens          *int64   `json:"prompt_tokens,omitempty"`
	PromptMilliseconds    *float64 `json:"prompt_ms,omitempty"`
	GeneratedTokens       *int64   `json:"generated_tokens,omitempty"`
	GeneratedMilliseconds *float64 `json:"generated_ms,omitempty"`
	DraftTokens           *int64   `json:"draft_tokens,omitempty"`
	DraftAcceptedTokens   *int64   `json:"draft_accepted_tokens,omitempty"`
}

type responseObservation struct {
	Tokens        TokenUsage
	Timings       BackendTimings
	FirstOutputAt time.Time
}

type RequestRecord struct {
	ID             string          `json:"id"`
	SessionID      string          `json:"session_id,omitempty"`
	Model          string          `json:"model,omitempty"`
	Application    string          `json:"application,omitempty"`
	Peer           string          `json:"peer,omitempty"`
	UserAgent      string          `json:"user_agent,omitempty"`
	Stream         bool            `json:"stream"`
	Outcome        RequestOutcome  `json:"outcome"`
	HTTPStatus     int             `json:"http_status,omitempty"`
	StartedAt      string          `json:"started_at"`
	FinishedAt     string          `json:"finished_at"`
	Timing         RequestTiming   `json:"timing"`
	Tokens         TokenUsage      `json:"tokens"`
	BackendTimings BackendTimings  `json:"backend_timings"`
	Controls       RequestControls `json:"controls"`
}

type requestLedgerEntry struct {
	sequence uint64
	record   RequestRecord
}

type requestLedger struct {
	mu       sync.Mutex
	sequence uint64
	entries  [RecentRequestLimit]requestLedgerEntry
	overall  RequestAggregate
	models   map[string]ModelAggregate
	sessions map[string]sessionLedgerEntry
}

func (ledger *requestLedger) Add(record RequestRecord) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.sequence++
	index := (ledger.sequence - 1) % RecentRequestLimit
	ledger.entries[index] = requestLedgerEntry{sequence: ledger.sequence, record: record}
	ledger.addUsage(record)
}

func (ledger *requestLedger) Recent(limit int) []RequestRecord {
	if limit <= 0 {
		return nil
	}
	if limit > RecentRequestLimit {
		limit = RecentRequestLimit
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.recentLocked(limit)
}

func (ledger *requestLedger) recentLocked(limit int) []RequestRecord {
	if limit <= 0 {
		return nil
	}
	if limit > RecentRequestLimit {
		limit = RecentRequestLimit
	}
	available := int(ledger.sequence)
	if available > RecentRequestLimit {
		available = RecentRequestLimit
	}
	if limit > available {
		limit = available
	}
	result := make([]RequestRecord, 0, limit)
	for offset := 0; offset < limit; offset++ {
		sequence := ledger.sequence - uint64(offset)
		entry := ledger.entries[(sequence-1)%RecentRequestLimit]
		if entry.sequence == sequence {
			result = append(result, entry.record)
		}
	}
	return result
}

type requestObservation struct {
	Model    string
	Stream   bool
	Controls RequestControls
}

var samplerFields = []string{
	"temperature",
	"top_p",
	"top_k",
	"min_p",
	"presence_penalty",
	"repeat_penalty",
}

// inspectRequest extracts only the closed observability allowlist. Keeping the
// decoded request object local prevents messages, tools, and arbitrary client
// fields from crossing into the durable-for-process request record.
func inspectRequest(body []byte) (requestObservation, error) {
	object, err := decodeJSONObject(body)
	if err != nil {
		return requestObservation{}, err
	}
	rawModel, ok := object["model"]
	if !ok {
		return requestObservation{}, fmt.Errorf("Request body requires a model string")
	}
	var model string
	if err := json.Unmarshal(rawModel, &model); err != nil || strings.TrimSpace(model) == "" {
		return requestObservation{}, fmt.Errorf("Request body requires a non-empty model string")
	}
	observation := requestObservation{Model: model}
	observation.Stream, observation.Controls.Diagnostics = observeStream(object["stream"])

	for _, name := range samplerFields {
		observation.Controls.Sampling = append(observation.Controls.Sampling, observeNumber(name, object[name], object[name] != nil))
	}

	reasoning := []ObservedControl{
		observeReasoningString("reasoning_effort", object["reasoning_effort"], object["reasoning_effort"] != nil),
		observeReasoningString("reasoning_strength", object["reasoning_strength"], object["reasoning_strength"] != nil),
	}
	if raw, present := object["chat_template_kwargs"]; present && !isJSONNull(raw) {
		kwargs, kwargsErr := decodeJSONObject(raw)
		if kwargsErr != nil {
			reasoning = append(reasoning, ObservedControl{Name: "chat_template_kwargs", Source: ControlInvalid, Provided: true})
			observation.Controls.Diagnostics = append(observation.Controls.Diagnostics, "chat_template_kwargs has an unexpected type")
		} else {
			reasoning = append(reasoning,
				observeBool("chat_template_kwargs.enable_thinking", kwargs["enable_thinking"], kwargs["enable_thinking"] != nil),
				observeReasoningString("chat_template_kwargs.reasoning_effort", kwargs["reasoning_effort"], kwargs["reasoning_effort"] != nil),
				observeReasoningString("chat_template_kwargs.reasoning_strength", kwargs["reasoning_strength"], kwargs["reasoning_strength"] != nil),
			)
		}
	} else {
		reasoning = append(reasoning,
			ObservedControl{Name: "chat_template_kwargs.enable_thinking", Source: ControlDefault},
			ObservedControl{Name: "chat_template_kwargs.reasoning_effort", Source: ControlDefault},
			ObservedControl{Name: "chat_template_kwargs.reasoning_strength", Source: ControlDefault},
		)
	}
	observation.Controls.Reasoning = reasoning
	if reasoningControlsConflict(reasoning) {
		observation.Controls.Diagnostics = append(observation.Controls.Diagnostics, "conflicting reasoning controls")
	}
	return observation, nil
}

func decodeJSONObject(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, fmt.Errorf("Request body must be one JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("Request body contains trailing data")
	}
	return object, nil
}

func observeStream(raw json.RawMessage) (bool, []string) {
	if len(raw) == 0 || isJSONNull(raw) {
		return false, nil
	}
	var stream bool
	if err := json.Unmarshal(raw, &stream); err != nil {
		return false, []string{"stream has an unexpected type"}
	}
	return stream, nil
}

func observeNumber(name string, raw json.RawMessage, provided bool) ObservedControl {
	if !provided || isJSONNull(raw) {
		return ObservedControl{Name: name, Source: ControlDefault, Provided: provided}
	}
	value := strings.TrimSpace(string(raw))
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return ObservedControl{Name: name, Source: ControlInvalid, Provided: true}
	}
	return ObservedControl{Name: name, Source: ControlClient, Provided: true, Value: boundedText(value, maxObservedValueRunes)}
}

func observeReasoningString(name string, raw json.RawMessage, provided bool) ObservedControl {
	if !provided || isJSONNull(raw) {
		return ObservedControl{Name: name, Source: ControlDefault, Provided: provided}
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ObservedControl{Name: name, Source: ControlInvalid, Provided: true}
	}
	switch value {
	case "none", "off", "on", "adaptive", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return ObservedControl{Name: name, Source: ControlInvalid, Provided: true}
	}
	return ObservedControl{Name: name, Source: ControlClient, Provided: true, Value: boundedText(value, maxObservedValueRunes)}
}

func observeBool(name string, raw json.RawMessage, provided bool) ObservedControl {
	if !provided || isJSONNull(raw) {
		return ObservedControl{Name: name, Source: ControlDefault, Provided: provided}
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return ObservedControl{Name: name, Source: ControlInvalid, Provided: true}
	}
	return ObservedControl{Name: name, Source: ControlClient, Provided: true, Value: strconv.FormatBool(value)}
}

func reasoningControlsConflict(controls []ObservedControl) bool {
	var decision *bool
	values := map[string]string{}
	for _, control := range controls {
		if control.Source != ControlClient {
			continue
		}
		kind := ""
		current := true
		switch {
		case strings.HasSuffix(control.Name, "enable_thinking"):
			kind = "enable_thinking"
			current = control.Value == "true"
		case strings.HasSuffix(control.Name, "reasoning_effort"):
			kind = "reasoning_effort"
			current = control.Value != "none" && control.Value != "off"
		case strings.HasSuffix(control.Name, "reasoning_strength"):
			kind = "reasoning_strength"
			current = control.Value != "none" && control.Value != "off"
		}
		if previous, ok := values[kind]; ok && previous != control.Value {
			return true
		}
		values[kind] = control.Value
		if decision != nil && *decision != current {
			return true
		}
		resolved := current
		decision = &resolved
	}
	return false
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func boundedText(value string, limit int) string {
	clean := make([]rune, 0, limit)
	for _, character := range value {
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf) {
			character = ' '
		}
		if len(clean) == limit {
			return string(clean) + "…"
		}
		clean = append(clean, character)
	}
	return string(clean)
}

func requestPeer(request *http.Request) string {
	peer := request.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	return boundedText(peer, maxObservedTextRunes)
}

// requestSessionID accepts only opaque UUIDs from explicit correlation
// headers. In particular, it never derives identity from prompts or other
// request content. Pi's OpenAI-compatible session-affinity mode supplies the
// second header; generic callers and managed Maki use the first.
func requestSessionID(request *http.Request) string {
	for _, name := range []string{SessionIDHeader, "X-Session-Affinity", "X-Session-ID"} {
		for _, value := range request.Header.Values(name) {
			if normalized, ok := normalizeSessionID(value); ok {
				return normalized
			}
		}
	}
	return ""
}

func normalizeSessionID(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return "", false
			}
		}
	}
	return value, true
}

func controlsLogSummary(controls RequestControls) string {
	parts := controlValues(controls.Reasoning)
	if len(parts) == 0 {
		parts = append(parts, "reasoning=defaults")
	}
	samplers := controlValues(controls.Sampling)
	if len(samplers) == 0 {
		parts = append(parts, "samplers=defaults")
	} else {
		parts = append(parts, samplers...)
	}
	invalid := 0
	for _, group := range [][]ObservedControl{controls.Reasoning, controls.Sampling} {
		for _, control := range group {
			if control.Source == ControlInvalid {
				invalid++
			}
		}
	}
	if invalid > 0 {
		parts = append(parts, fmt.Sprintf("invalid-controls=%d", invalid))
	}
	return strings.Join(parts, " ")
}

func controlValues(controls []ObservedControl) []string {
	values := []string{}
	for _, control := range controls {
		if control.Source == ControlClient {
			values = append(values, control.Name+"="+control.Value)
		}
	}
	return values
}

// responseObserver receives copies of bytes already read by ReverseProxy. Its
// buffers are bounded and parse failures are intentionally silent, so metrics
// can never delay, reject, or reshape an upstream response.
type responseObserver struct {
	stream         bool
	jsonBuffer     []byte
	jsonOversized  bool
	line           []byte
	event          []byte
	eventOversize  bool
	tokens         TokenUsage
	timings        BackendTimings
	now            func() time.Time
	firstOutputAt  time.Time
	inputFromUsage bool
	cacheFromUsage bool
}

func newResponseObserver(contentType string) *responseObserver {
	return &responseObserver{stream: isEventStream(contentType), now: time.Now}
}

func isEventStream(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

func (observer *responseObserver) Observe(value []byte) {
	if observer.stream {
		observer.observeSSE(value)
		return
	}
	if observer.jsonOversized {
		return
	}
	if len(observer.jsonBuffer)+len(value) > maxObservedJSONBytes {
		observer.jsonBuffer = nil
		observer.jsonOversized = true
		return
	}
	observer.jsonBuffer = append(observer.jsonBuffer, value...)
}

func (observer *responseObserver) Finish() responseObservation {
	if observer.stream {
		if len(observer.line) > 0 {
			observer.consumeSSELine(observer.line)
			observer.line = nil
		}
		observer.consumeSSEEvent()
	} else if !observer.jsonOversized {
		observer.observeJSON(observer.jsonBuffer)
	}
	return responseObservation{Tokens: observer.tokens, Timings: observer.timings, FirstOutputAt: observer.firstOutputAt}
}

func (observer *responseObserver) observeSSE(value []byte) {
	for len(value) > 0 {
		newline := bytes.IndexByte(value, '\n')
		if newline < 0 {
			if len(observer.line)+len(value) <= maxObservedSSEEventBytes {
				observer.line = append(observer.line, value...)
			} else {
				observer.line = nil
				observer.eventOversize = true
			}
			return
		}
		if len(observer.line)+newline <= maxObservedSSEEventBytes {
			observer.line = append(observer.line, value[:newline]...)
			observer.consumeSSELine(observer.line)
		} else {
			observer.eventOversize = true
		}
		observer.line = nil
		value = value[newline+1:]
	}
}

func (observer *responseObserver) consumeSSELine(line []byte) {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 {
		observer.consumeSSEEvent()
		return
	}
	if !bytes.HasPrefix(line, []byte("data:")) || observer.eventOversize {
		return
	}
	data := line[len("data:"):]
	if len(data) > 0 && data[0] == ' ' {
		data = data[1:]
	}
	additional := len(data)
	if len(observer.event) > 0 {
		additional++
	}
	if len(observer.event)+additional > maxObservedSSEEventBytes {
		observer.event = nil
		observer.eventOversize = true
		return
	}
	if len(observer.event) > 0 {
		observer.event = append(observer.event, '\n')
	}
	observer.event = append(observer.event, data...)
}

func (observer *responseObserver) consumeSSEEvent() {
	if !observer.eventOversize && len(observer.event) > 0 && !bytes.Equal(bytes.TrimSpace(observer.event), []byte("[DONE]")) {
		observer.observeJSON(observer.event)
	}
	observer.event = nil
	observer.eventOversize = false
}

func (observer *responseObserver) observeJSON(value []byte) {
	object, ok := responseObject(value)
	if !ok {
		return
	}
	// Role-only chunks, keepalives and usage metadata are not generation.
	// Non-streaming JSON cannot reveal when its first token was produced.
	if observer.stream && observer.firstOutputAt.IsZero() && hasGeneratedDelta(object["choices"]) {
		observer.firstOutputAt = observer.now()
	}
	if usage, ok := rawObject(object["usage"]); ok {
		if token, ok := firstInteger(usage, "prompt_tokens", "input_tokens"); ok {
			observer.tokens.Input = token
			observer.inputFromUsage = true
		}
		if token, ok := firstInteger(usage, "completion_tokens", "output_tokens"); ok {
			observer.tokens.Output = token
		}
		if token, ok := firstInteger(usage, "cache_read_input_tokens", "cached_tokens"); ok {
			observer.tokens.Cached = token
			observer.cacheFromUsage = true
		}
		if token, ok := firstInteger(usage, "reasoning_tokens"); ok {
			observer.tokens.Reasoning = token
		}
		for _, name := range []string{"prompt_tokens_details", "input_tokens_details"} {
			if details, ok := rawObject(usage[name]); ok {
				if token, ok := firstInteger(details, "cached_tokens"); ok {
					observer.tokens.Cached = token
					observer.cacheFromUsage = true
				}
			}
		}
		for _, name := range []string{"completion_tokens_details", "output_tokens_details"} {
			if details, ok := rawObject(usage[name]); ok {
				if token, ok := firstInteger(details, "reasoning_tokens"); ok {
					observer.tokens.Reasoning = token
				}
			}
		}
	}
	if timings, ok := rawObject(object["timings"]); ok {
		if token, ok := firstInteger(timings, "prompt_n"); ok {
			observer.timings.PromptTokens = token
		}
		if token, ok := firstInteger(timings, "cache_n"); ok && !observer.cacheFromUsage {
			observer.tokens.Cached = token
		}
		if !observer.inputFromUsage && observer.timings.PromptTokens != nil {
			// llama.cpp prompt_n counts evaluated tokens, not the cached prefix.
			input := *observer.timings.PromptTokens
			if cached := observer.tokens.Cached; cached != nil {
				if input > math.MaxInt64-*cached {
					observer.tokens.Input = nil
				} else {
					input += *cached
					observer.tokens.Input = &input
				}
			} else {
				observer.tokens.Input = &input
			}
		}
		if milliseconds, ok := firstNumber(timings, "prompt_ms"); ok {
			observer.timings.PromptMilliseconds = milliseconds
		}
		if token, ok := firstInteger(timings, "predicted_n"); ok {
			observer.timings.GeneratedTokens = token
			if observer.tokens.Output == nil {
				observer.tokens.Output = token
			}
		}
		if milliseconds, ok := firstNumber(timings, "predicted_ms"); ok {
			observer.timings.GeneratedMilliseconds = milliseconds
		}
		if token, ok := firstInteger(timings, "draft_n"); ok {
			observer.timings.DraftTokens = token
		}
		if token, ok := firstInteger(timings, "draft_n_accepted"); ok {
			observer.timings.DraftAcceptedTokens = token
		}
	}
}

func hasGeneratedDelta(raw json.RawMessage) bool {
	var choices []struct {
		Delta struct {
			Content          string `json:"content"`
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
			Refusal          string `json:"refusal"`
			ToolCalls        []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	}
	if json.Unmarshal(raw, &choices) != nil {
		return false
	}
	for _, choice := range choices {
		delta := choice.Delta
		if delta.Content != "" || delta.Reasoning != "" || delta.ReasoningContent != "" || delta.Refusal != "" {
			return true
		}
		for _, call := range delta.ToolCalls {
			if call.Function.Name != "" || call.Function.Arguments != "" {
				return true
			}
		}
	}
	return false
}

func responseObject(value []byte) (map[string]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	return object, true
}

func rawObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, false
	}
	return object, true
}

func firstInteger(object map[string]json.RawMessage, names ...string) (*int64, bool) {
	for _, name := range names {
		raw := object[name]
		if len(raw) == 0 || isJSONNull(raw) {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err == nil && value >= 0 {
			resolved := value
			return &resolved, true
		}
	}
	return nil, false
}

func firstNumber(object map[string]json.RawMessage, names ...string) (*float64, bool) {
	for _, name := range names {
		raw := object[name]
		if len(raw) == 0 || isJSONNull(raw) {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err == nil && value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value) {
			resolved := value
			return &resolved, true
		}
	}
	return nil, false
}

type observedBody struct {
	io.ReadCloser
	observer *responseObserver
}

func (body *observedBody) Read(value []byte) (int, error) {
	count, err := body.ReadCloser.Read(value)
	if count > 0 {
		body.observer.Observe(value[:count])
	}
	return count, err
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusResponseWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusResponseWriter) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(value)
}

func (writer *statusResponseWriter) Flush() {
	_ = http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *statusResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}
