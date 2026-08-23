package gateway

import (
	"math"
	"sort"
)

const RecentSessionLimit = 64

type AggregateMetric struct {
	Total        uint64 `json:"total"`
	Observations uint64 `json:"observations"`
}

type AggregateTokens struct {
	Input     AggregateMetric `json:"input"`
	Output    AggregateMetric `json:"output"`
	Cached    AggregateMetric `json:"cached"`
	Reasoning AggregateMetric `json:"reasoning"`
}

type AggregateTiming struct {
	GatewayWait AggregateMetric `json:"gateway_wait_ms"`
	Upstream    AggregateMetric `json:"upstream_ms"`
	Total       AggregateMetric `json:"total_ms"`
}

type AggregateOutcomes struct {
	Succeeded          uint64 `json:"succeeded"`
	Rejected           uint64 `json:"rejected"`
	Canceled           uint64 `json:"canceled"`
	QueueFull          uint64 `json:"queue_full"`
	BackendStartFailed uint64 `json:"backend_start_failed"`
	UpstreamError      uint64 `json:"upstream_error"`
	Other              uint64 `json:"other"`
}

type RequestAggregate struct {
	Requests uint64            `json:"requests"`
	Outcomes AggregateOutcomes `json:"outcomes"`
	Tokens   AggregateTokens   `json:"tokens"`
	Timing   AggregateTiming   `json:"timing"`
}

type ModelAggregate struct {
	Model       string           `json:"model"`
	Application string           `json:"application"`
	Usage       RequestAggregate `json:"usage"`
}

type SessionAggregate struct {
	ID             string           `json:"id"`
	FirstRequestAt string           `json:"first_request_at"`
	LastRequestAt  string           `json:"last_request_at"`
	Usage          RequestAggregate `json:"usage"`
}

type UsageSummary struct {
	Overall  RequestAggregate   `json:"overall"`
	Models   []ModelAggregate   `json:"models"`
	Sessions []SessionAggregate `json:"sessions,omitempty"`
}

type sessionLedgerEntry struct {
	summary      SessionAggregate
	lastSequence uint64
}

func (ledger *requestLedger) addUsage(record RequestRecord) {
	addAggregate(&ledger.overall, record)
	if record.Model != "" {
		if ledger.models == nil {
			ledger.models = make(map[string]ModelAggregate)
		}
		model := ledger.models[record.Model]
		model.Model, model.Application = record.Model, record.Application
		addAggregate(&model.Usage, record)
		ledger.models[record.Model] = model
	}
	if record.SessionID == "" {
		return
	}
	if ledger.sessions == nil {
		ledger.sessions = make(map[string]sessionLedgerEntry)
	}
	session, exists := ledger.sessions[record.SessionID]
	if !exists && len(ledger.sessions) == RecentSessionLimit {
		oldestID := ""
		oldestSequence := uint64(math.MaxUint64)
		for id, candidate := range ledger.sessions {
			if candidate.lastSequence < oldestSequence {
				oldestID, oldestSequence = id, candidate.lastSequence
			}
		}
		delete(ledger.sessions, oldestID)
	}
	if !exists {
		session.summary.ID = record.SessionID
		session.summary.FirstRequestAt = record.StartedAt
	}
	session.summary.LastRequestAt = record.FinishedAt
	addAggregate(&session.summary.Usage, record)
	session.lastSequence = ledger.sequence
	ledger.sessions[record.SessionID] = session
}

func addAggregate(aggregate *RequestAggregate, record RequestRecord) {
	aggregate.Requests = saturatingAdd(aggregate.Requests, 1)
	switch record.Outcome {
	case OutcomeSucceeded:
		aggregate.Outcomes.Succeeded = saturatingAdd(aggregate.Outcomes.Succeeded, 1)
	case OutcomeRejected:
		aggregate.Outcomes.Rejected = saturatingAdd(aggregate.Outcomes.Rejected, 1)
	case OutcomeCanceled:
		aggregate.Outcomes.Canceled = saturatingAdd(aggregate.Outcomes.Canceled, 1)
	case OutcomeQueueFull:
		aggregate.Outcomes.QueueFull = saturatingAdd(aggregate.Outcomes.QueueFull, 1)
	case OutcomeBackendStartFailed:
		aggregate.Outcomes.BackendStartFailed = saturatingAdd(aggregate.Outcomes.BackendStartFailed, 1)
	case OutcomeUpstreamError:
		aggregate.Outcomes.UpstreamError = saturatingAdd(aggregate.Outcomes.UpstreamError, 1)
	default:
		aggregate.Outcomes.Other = saturatingAdd(aggregate.Outcomes.Other, 1)
	}
	addTokenMetric(&aggregate.Tokens.Input, record.Tokens.Input)
	addTokenMetric(&aggregate.Tokens.Output, record.Tokens.Output)
	addTokenMetric(&aggregate.Tokens.Cached, record.Tokens.Cached)
	addTokenMetric(&aggregate.Tokens.Reasoning, record.Tokens.Reasoning)
	addDurationMetric(&aggregate.Timing.GatewayWait, record.Timing.GatewayWaitMilliseconds)
	addDurationMetric(&aggregate.Timing.Upstream, record.Timing.UpstreamMilliseconds)
	addDurationMetric(&aggregate.Timing.Total, record.Timing.TotalMilliseconds)
}

func addTokenMetric(metric *AggregateMetric, value *int64) {
	if value == nil || *value < 0 {
		return
	}
	metric.Total = saturatingAdd(metric.Total, uint64(*value))
	metric.Observations = saturatingAdd(metric.Observations, 1)
}

func addDurationMetric(metric *AggregateMetric, value int64) {
	if value < 0 {
		return
	}
	metric.Total = saturatingAdd(metric.Total, uint64(value))
	metric.Observations = saturatingAdd(metric.Observations, 1)
}

func saturatingAdd(current, value uint64) uint64 {
	if math.MaxUint64-current < value {
		return math.MaxUint64
	}
	return current + value
}

func (ledger *requestLedger) Snapshot(recentLimit int) ([]RequestRecord, UsageSummary) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	recent := ledger.recentLocked(recentLimit)
	summary := UsageSummary{Overall: ledger.overall}
	for _, model := range ledger.models {
		summary.Models = append(summary.Models, model)
	}
	sort.Slice(summary.Models, func(left, right int) bool { return summary.Models[left].Model < summary.Models[right].Model })
	seenSessions := make(map[string]bool)
	for _, record := range recent {
		if record.SessionID == "" || seenSessions[record.SessionID] {
			continue
		}
		seenSessions[record.SessionID] = true
		if session, ok := ledger.sessions[record.SessionID]; ok {
			summary.Sessions = append(summary.Sessions, session.summary)
		}
	}
	return recent, summary
}
