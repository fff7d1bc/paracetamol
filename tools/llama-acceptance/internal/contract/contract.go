// Package contract defines the small boundary between frozen acceptance cases
// and their production-gateway transport. It contains no backend lifecycle.
package contract

import (
	"context"

	"paracetamol/internal/gateway"
)

const Version = "paracetamol.llama-acceptance.v2"

type Status string

const (
	Pass    Status = "PASS"
	Fail    Status = "FAIL"
	Blocked Status = "BLOCKED"
	Review  Status = "REVIEW"
)

// Result is scoped to a named case. REVIEW retains a bounded coding miss or a
// performance observation without pretending it proves an engine defect.
type Result struct {
	Name         string             `json:"name"`
	Status       Status             `json:"status"`
	Detail       string             `json:"detail,omitempty"`
	RequestIDs   []string           `json:"request_ids,omitempty"`
	Measurements map[string]float64 `json:"measurements,omitempty"`
}

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Response preserves the full assistant object for real history appends. It
// must not be reconstructed from Text because that discards reasoning/tools.
type Response struct {
	RequestID string
	Message   map[string]any
	Text      string
	Reasoning string
	Finish    string
	Tools     []ToolCall
	Report    gateway.RequestReport
	// CancellationVerified is set only by transport after it validates the
	// expected canceled report, policy, correlation and persisted evidence.
	CancellationVerified bool
}

type Call func(context.Context, string, map[string]any) (Response, error)

// Slot is read only. Missing native counters remain nil, never invented zeroes.
type Slot struct {
	ID            int
	Processing    bool
	PromptTokens  *int64
	DecodedTokens *int64
}

type ProbeSlots func(context.Context) ([]Slot, error)

// Tokenize uses the selected native model's tokenizer without generation. It
// calibrates public corpora before populating a near-limit shared cache.
type Tokenize func(context.Context, string) (int, error)

type Options struct {
	Model    string
	Slots    ProbeSlots
	Tokenize Tokenize
}

func Worst(results []Result) Status {
	worst := Pass
	for _, result := range results {
		switch result.Status {
		case Fail:
			return Fail
		case Blocked:
			worst = Blocked
		case Review:
			if worst == Pass {
				worst = Review
			}
		case Pass:
		default:
			return Fail
		}
	}
	if len(results) == 0 {
		return Blocked
	}
	return worst
}
