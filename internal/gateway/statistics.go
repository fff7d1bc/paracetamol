package gateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const statisticsSchema = "paracetamol.statistics.v1"

var errStatisticsUnavailable = errors.New("gateway statistics unavailable")

type StatisticsOptions struct {
	Enabled       bool
	Path          string
	RetentionDays int
	Log           io.Writer
}

type StatisticsSnapshot struct {
	Schema          string            `json:"schema"`
	State           string            `json:"state"`
	RetentionDays   int               `json:"retention_days"`
	PeriodDays      int               `json:"period_days"`
	From            string            `json:"from"`
	To              string            `json:"to"`
	CollectedAt     string            `json:"collected_at"`
	RecordingSince  string            `json:"recording_since,omitempty"`
	Incomplete      bool              `json:"incomplete"`
	DroppedRequests uint64            `json:"dropped_requests"`
	PendingRequests uint64            `json:"pending_requests"`
	Overall         StatisticsUsage   `json:"overall"`
	Daily           []DailyStatistics `json:"daily"`
	Models          []ModelStatistics `json:"models"`
}

type StatisticsUsage struct {
	Requests                uint64              `json:"requests"`
	Succeeded               uint64              `json:"succeeded"`
	Failed                  uint64              `json:"failed"`
	Canceled                uint64              `json:"canceled"`
	Input                   AggregateMetric     `json:"input"`
	Output                  AggregateMetric     `json:"output"`
	Cached                  AggregateMetric     `json:"cached"`
	Reasoning               AggregateMetric     `json:"reasoning"`
	CacheReuse              AggregateCacheReuse `json:"cache_reuse"`
	TotalMilliseconds       AggregateMetric     `json:"total_ms"`
	FirstOutputMilliseconds AggregateMetric     `json:"first_output_ms"`
}

type DailyStatistics struct {
	Date  string          `json:"date"`
	Usage StatisticsUsage `json:"usage"`
}

type ModelStatistics struct {
	Model       string          `json:"model"`
	Application string          `json:"application"`
	Usage       StatisticsUsage `json:"usage"`
}

// This is deliberately not a RequestRecord: no request/session identifiers,
// peer, user agent, controls or exact request timestamps cross the queue.
type statisticsEvent struct {
	day, model, application string
	counters                statisticsCounters
}

type statisticsCounters [19]uint64

type Statistics struct {
	retention         int
	now               func() time.Time
	log               io.Writer
	db                *sql.DB
	lock              *os.File
	queue             chan statisticsEvent
	done              chan struct{}
	mu                sync.Mutex // serializes enqueue with close, never database work
	closed            bool
	dbMu              sync.Mutex
	dropped           atomic.Uint64
	pending           atomic.Uint64
	failed            atomic.Bool
	closeErr          error
	closeOnce         sync.Once
	persistedDropped  uint64    // protected by dbMu, excludes not-yet-persisted losses
	maintenanceDay    string    // protected by dbMu
	nextHealthAttempt time.Time // protected by dbMu
}

func NewStatistics(options StatisticsOptions) *Statistics {
	return newStatistics(options, time.Now, 1024)
}

func newStatistics(options StatisticsOptions, now func() time.Time, capacity int) *Statistics {
	if !options.Enabled {
		return nil
	}
	if options.RetentionDays == 0 {
		options.RetentionDays = 90
	}
	s := &Statistics{retention: options.RetentionDays, now: now, log: options.Log}
	if s.log == nil {
		s.log = os.Stderr
	}
	if s.retention < 30 || s.retention > 365 {
		s.fail(errors.New("statistics retention must be between 30 and 365 days"))
		return s
	}
	if err := s.open(options.Path); err != nil {
		s.fail(err)
		if s.db != nil {
			_ = s.db.Close()
			s.db = nil
		}
		if s.lock != nil {
			_ = s.lock.Close()
			s.lock = nil
		}
		return s
	}
	s.queue = make(chan statisticsEvent, capacity)
	s.done = make(chan struct{})
	go s.writeLoop()
	return s
}

func (s *Statistics) fail(err error) {
	if !s.failed.Swap(true) {
		fmt.Fprintf(s.log, "gateway statistics unavailable: %v\n", err)
	}
}

// Record does no disk I/O and never waits for queue capacity. Numeric values
// are copied here, before the caller can reuse its record or pointed values.
func (s *Statistics) Record(record RequestRecord) {
	if s == nil || s.queue == nil {
		return
	}
	event := statisticsEventFromRecord(record, s.now())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.pending.Add(1)
	select {
	case s.queue <- event:
	default:
		s.pending.Add(^uint64(0))
		s.dropped.Add(1)
	}
}

func statisticsEventFromRecord(record RequestRecord, now time.Time) statisticsEvent {
	finished, err := time.Parse(time.RFC3339Nano, record.FinishedAt)
	if err != nil {
		finished = now
	}
	// Invalid subset observations must not turn into plausible cache/reasoning
	// totals. Input already includes cached tokens; output includes reasoning.
	if record.Tokens.Cached != nil && record.Tokens.Input != nil && *record.Tokens.Cached > *record.Tokens.Input {
		record.Tokens.Cached = nil
	}
	if record.Tokens.Reasoning != nil && record.Tokens.Output != nil && *record.Tokens.Reasoning > *record.Tokens.Output {
		record.Tokens.Reasoning = nil
	}
	var a RequestAggregate
	addAggregate(&a, record)
	u := StatisticsUsage{Requests: 1, Input: a.Tokens.Input, Output: a.Tokens.Output, Cached: a.Tokens.Cached, Reasoning: a.Tokens.Reasoning,
		CacheReuse: a.CacheReuse, TotalMilliseconds: a.Timing.Total, FirstOutputMilliseconds: a.Timing.FirstOutput}
	switch record.Outcome {
	case OutcomeSucceeded:
		u.Succeeded = 1
	case OutcomeCanceled:
		u.Canceled = 1
	default:
		u.Failed = 1
	}
	return statisticsEvent{day: finished.UTC().Format(time.DateOnly), model: statisticsIdentity(record.Model), application: statisticsIdentity(record.Application), counters: usageCounters(u)}
}

func statisticsIdentity(value string) string {
	if len(value) > 512 || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return strings.Clone(value)
}

func usageCounters(u StatisticsUsage) statisticsCounters {
	return statisticsCounters{u.Requests, u.Succeeded, u.Failed, u.Canceled,
		u.Input.Total, u.Input.Observations, u.Output.Total, u.Output.Observations,
		u.Cached.Total, u.Cached.Observations, u.Reasoning.Total, u.Reasoning.Observations,
		u.TotalMilliseconds.Total, u.TotalMilliseconds.Observations, u.FirstOutputMilliseconds.Total, u.FirstOutputMilliseconds.Observations,
		u.CacheReuse.InputTokens, u.CacheReuse.CachedTokens, u.CacheReuse.Observations}
}

func (c statisticsCounters) usage() StatisticsUsage {
	u := StatisticsUsage{Requests: c[0], Succeeded: c[1], Failed: c[2], Canceled: c[3],
		Input: AggregateMetric{c[4], c[5]}, Output: AggregateMetric{c[6], c[7]}, Cached: AggregateMetric{c[8], c[9]}, Reasoning: AggregateMetric{c[10], c[11]},
		TotalMilliseconds: AggregateMetric{c[12], c[13]}, FirstOutputMilliseconds: AggregateMetric{c[14], c[15]},
		CacheReuse: AggregateCacheReuse{InputTokens: c[16], CachedTokens: c[17], Observations: c[18]}}
	if c[18] > 0 && c[16] > 0 {
		ratio := float64(c[17]) / float64(c[16])
		u.CacheReuse.Ratio = &ratio
	}
	return u
}

func (c *statisticsCounters) add(other statisticsCounters) {
	for i := range c {
		c[i] = saturatingAdd(c[i], other[i])
	}
}

func statisticsInteger(value uint64) int64 {
	return int64(min(value, math.MaxInt64))
}

func (s *Statistics) writeLoop() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var batch []statisticsEvent
		select {
		case event, ok := <-s.queue:
			if !ok {
				s.finish()
				return
			}
			batch = append(batch, event)
		case <-ticker.C:
		}
		for len(batch) < 128 {
			select {
			case event, ok := <-s.queue:
				if !ok {
					goto flush
				}
				batch = append(batch, event)
			default:
				goto flush
			}
		}
	flush:
		s.dbMu.Lock()
		err := s.writeBatch(context.Background(), batch, false)
		s.dbMu.Unlock()
		if err != nil {
			s.dropped.Add(uint64(len(batch)))
			s.fail(err)
			s.dbMu.Lock()
			s.nextHealthAttempt = s.now().Add(time.Minute)
			s.dbMu.Unlock()
		}
		s.pending.Add(0 - uint64(len(batch)))
	}
}

func (s *Statistics) finish() {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	if err := s.writeBatch(context.Background(), nil, true); err != nil {
		s.fail(err)
	}
}

// Close joins the writer after draining accepted events. Shutdown callers
// should stop accepting HTTP work first; Record after Close is ignored.
func (s *Statistics) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.queue != nil {
			s.mu.Lock()
			s.closed = true
			close(s.queue)
			s.mu.Unlock()
			<-s.done
		}
		s.dbMu.Lock()
		defer s.dbMu.Unlock()
		if s.db != nil {
			if err := s.db.Close(); err != nil {
				s.fail(err)
			}
		}
		if s.lock != nil {
			_ = s.lock.Close()
		}
		if s.failed.Load() {
			s.closeErr = errStatisticsUnavailable
		}
	})
	return s.closeErr
}

func (s *Statistics) Snapshot(ctx context.Context, days int) (StatisticsSnapshot, error) {
	if days != 1 && days != 7 && days != 30 {
		return StatisticsSnapshot{}, errors.New("statistics period must be 1, 7 or 30 days")
	}
	now := time.Now().UTC()
	if s != nil {
		now = s.now().UTC()
	}
	start := now.AddDate(0, 0, 1-days)
	result := StatisticsSnapshot{Schema: statisticsSchema, State: "disabled", PeriodDays: days, From: start.Format(time.DateOnly), To: now.Format(time.DateOnly), CollectedAt: now.Format(time.RFC3339), Daily: []DailyStatistics{}, Models: []ModelStatistics{}}
	if s == nil {
		return result, nil
	}
	result.RetentionDays, result.State = s.retention, "unavailable"
	result.DroppedRequests, result.PendingRequests = s.dropped.Load(), s.pending.Load()
	result.Incomplete = result.DroppedRequests > 0 || s.failed.Load()
	if s.db == nil || s.failed.Load() {
		return result, errStatisticsUnavailable
	}
	if !s.dbMu.TryLock() {
		// Database work has its own short busy timeout. A canceled HTTP read
		// need not wait even that long behind the writer's transaction.
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return result, errStatisticsUnavailable
			case <-ticker.C:
				if s.dbMu.TryLock() {
					goto locked
				}
			}
		}
	}
locked:
	defer s.dbMu.Unlock()
	if err := s.readSnapshot(ctx, &result); err != nil {
		// Never send paths or SQLite diagnostics to an HTTP client.
		if ctx.Err() == nil {
			s.fail(err)
		}
		return result, errStatisticsUnavailable
	}
	result.State = "ready"
	return result, nil
}

func (s *Statistics) readSnapshot(ctx context.Context, result *StatisticsSnapshot) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cutoff := s.now().UTC().AddDate(0, 0, 1-s.retention).Format(time.DateOnly)
	var incomplete bool
	if err := tx.QueryRowContext(ctx, "SELECT recording_since FROM statistics_meta WHERE id=1").Scan(&result.RecordingSince); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(incomplete),0) FROM statistics_health WHERE day>=? AND day<=?", cutoff, result.To).Scan(&incomplete); err != nil {
		return err
	}
	var retained int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(dropped),0) FROM statistics_health WHERE day>=? AND day<=?", cutoff, result.To).Scan(&retained); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT day,model,application,"+strings.Join(statisticsColumns, ",")+" FROM daily_usage WHERE day >= ? AND day <= ? ORDER BY day,model,application", result.From, result.To)
	if err != nil {
		return err
	}
	defer rows.Close()
	daily := make(map[string]statisticsCounters)
	models := make(map[[2]string]statisticsCounters)
	var overall statisticsCounters
	for rows.Next() {
		var day, model, application string
		var numbers [19]int64
		args := []any{&day, &model, &application}
		for i := range numbers {
			args = append(args, &numbers[i])
		}
		if err := rows.Scan(args...); err != nil {
			return err
		}
		var counters statisticsCounters
		for i, number := range numbers {
			if number < 0 {
				return errors.New("negative statistics counter")
			}
			counters[i] = uint64(number)
		}
		d := daily[day]
		d.add(counters)
		daily[day] = d
		key := [2]string{model, application}
		m := models[key]
		m.add(counters)
		models[key] = m
		overall.add(counters)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	result.DroppedRequests = saturatingAdd(uint64(retained), s.dropped.Load()-s.persistedDropped)
	result.PendingRequests = s.pending.Load()
	result.Incomplete = incomplete || result.DroppedRequests > 0 || s.failed.Load()
	result.Overall = overall.usage()
	first, _ := time.Parse(time.DateOnly, result.From)
	for i := range result.PeriodDays {
		day := first.AddDate(0, 0, i).Format(time.DateOnly)
		result.Daily = append(result.Daily, DailyStatistics{Date: day, Usage: daily[day].usage()})
	}
	for key, counters := range models {
		result.Models = append(result.Models, ModelStatistics{Model: key[0], Application: key[1], Usage: counters.usage()})
	}
	sort.Slice(result.Models, func(i, j int) bool {
		if result.Models[i].Model == result.Models[j].Model {
			return result.Models[i].Application < result.Models[j].Application
		}
		return result.Models[i].Model < result.Models[j].Model
	})
	return nil
}
