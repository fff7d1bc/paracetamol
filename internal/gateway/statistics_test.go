package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type statisticsClock struct{ value atomic.Int64 }

func (c *statisticsClock) now() time.Time { return time.Unix(c.value.Load(), 0).UTC() }
func (c *statisticsClock) set(value string) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	c.value.Store(t.Unix())
}

func testStatistics(t *testing.T, path string, clock *statisticsClock, capacity int) *Statistics {
	t.Helper()
	s := newStatistics(StatisticsOptions{Enabled: true, Path: path, RetentionDays: 30, Log: io.Discard}, clock.now, capacity)
	if s.failed.Load() {
		t.Fatal("statistics initialization failed")
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func waitStatistics(t *testing.T, s *Statistics) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.pending.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("statistics writer did not drain")
		}
		time.Sleep(time.Millisecond)
	}
}

func statisticsNumber(value int64) *int64 { return &value }

func TestStatisticsDailyUTCPeriodsRestartAndPrivacy(t *testing.T) {
	clock := &statisticsClock{}
	clock.set("2026-10-17T12:00:00Z")
	path := filepath.Join(t.TempDir(), "private", "usage.sqlite3")
	s := testStatistics(t, path, clock, 1024)
	record := RequestRecord{Model: "retired-model", Application: "llama-cpp", Outcome: OutcomeSucceeded, FinishedAt: "2026-10-17T00:30:00+02:00",
		Tokens: TokenUsage{Input: statisticsNumber(100), Output: statisticsNumber(20), Cached: statisticsNumber(75), Reasoning: statisticsNumber(5)},
		Timing: RequestTiming{TotalMilliseconds: 1000, FirstOutputMilliseconds: statisticsNumber(50)},
		ID:     "PRIVACY_SENTINEL_REQUEST", SessionID: "PRIVACY_SENTINEL_SESSION", Peer: "PRIVACY_SENTINEL_PEER", UserAgent: "PRIVACY_SENTINEL_AGENT",
		Controls: RequestControls{SamplingMode: "PRIVACY_SENTINEL_CONTROLS"}}
	s.Record(record)
	*record.Tokens.Input = 9999 // numeric values were copied before enqueue
	s.Record(RequestRecord{Model: "new-model", Application: "dwarfstar", Outcome: OutcomeCanceled, FinishedAt: "2026-10-17T12:00:00Z", Timing: RequestTiming{TotalMilliseconds: -1}})
	waitStatistics(t, s)
	for _, days := range []int{1, 7, 30} {
		snapshot, err := s.Snapshot(context.Background(), days)
		if err != nil || snapshot.State != "ready" || len(snapshot.Daily) != days || snapshot.To != "2026-10-17" || snapshot.RecordingSince != "2026-10-17" {
			t.Fatalf("snapshot(%d): %+v, %v", days, snapshot, err)
		}
		if days == 1 {
			if snapshot.Overall.Requests != 1 || snapshot.Overall.Canceled != 1 || snapshot.Overall.Input.Observations != 0 {
				t.Fatalf("today must exclude previous UTC day: %+v", snapshot.Overall)
			}
		} else if snapshot.Overall.Requests != 2 || snapshot.Overall.Input.Total != 100 || snapshot.Overall.Output.Total != 20 || snapshot.Overall.CacheReuse.Ratio == nil || *snapshot.Overall.CacheReuse.Ratio != .75 {
			t.Fatalf("bad aggregate: %+v", snapshot.Overall)
		}
		encoded, _ := json.Marshal(snapshot)
		if bytes.Contains(encoded, []byte("PRIVACY_SENTINEL")) {
			t.Fatal("private metadata in JSON")
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("PRIVACY_SENTINEL")) {
			t.Fatalf("private metadata in %s", suffix)
		}
		info, _ := os.Stat(path + suffix)
		if info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe mode %s: %v", suffix, info.Mode())
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = testStatistics(t, path, clock, 1024)
	snapshot, err := s.Snapshot(context.Background(), 7)
	if err != nil || snapshot.Overall.Requests != 2 || snapshot.Incomplete || len(snapshot.Models) != 2 {
		t.Fatalf("restart lost history or reported false loss: %+v %v", snapshot, err)
	}
}

func TestStatisticsUnavailableDisabledAndUnsafePaths(t *testing.T) {
	var disabled *Statistics
	disabled.Record(RequestRecord{})
	if err := disabled.Close(); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := disabled.Snapshot(context.Background(), 7); err != nil || snapshot.State != "disabled" {
		t.Fatalf("disabled: %+v %v", snapshot, err)
	}
	if _, err := disabled.Snapshot(context.Background(), 2); err == nil {
		t.Fatal("accepted unsupported period")
	}
	for _, kind := range []string{"relative", "directory", "symlink", "hardlink", "sidecar-symlink", "broad-permissions", "corruption", "future-schema", "unknown-schema", "parent-symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "usage.sqlite3")
			switch kind {
			case "relative":
				path = "relative.sqlite3"
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink", "sidecar-symlink", "parent-symlink":
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "sidecar-symlink" {
					path += "-wal"
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
				if kind == "sidecar-symlink" {
					path = strings.TrimSuffix(path, "-wal")
				}
				if kind == "parent-symlink" {
					path = filepath.Join(path, "db")
				}
			case "hardlink":
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(target, path); err != nil {
					t.Fatal(err)
				}
			case "broad-permissions":
				if err := os.WriteFile(path, nil, 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "corruption":
				if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
					t.Fatal(err)
				}
			case "future-schema", "unknown-schema":
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				statement := "PRAGMA user_version=2"
				if kind == "unknown-schema" {
					statement = "CREATE TABLE unrelated(value TEXT)"
				}
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
				_ = db.Close()
			}
			var log bytes.Buffer
			s := NewStatistics(StatisticsOptions{Enabled: true, Path: path, Log: &log})
			if s == nil {
				t.Fatal("unavailable instance must not look disabled")
			}
			snapshot, err := s.Snapshot(context.Background(), 7)
			if err != errStatisticsUnavailable || snapshot.State != "unavailable" || strings.Contains(err.Error(), dir) || log.Len() == 0 {
				t.Fatalf("unsafe path accepted or diagnostics leaked: %+v %v log=%s", snapshot, err, log.String())
			}
			s.Record(RequestRecord{})
			_ = s.Close()
		})
	}
}

func TestStatisticsRetentionLossAndUncleanRestart(t *testing.T) {
	clock := &statisticsClock{}
	clock.set("2026-10-01T12:00:00Z")
	path := filepath.Join(t.TempDir(), "usage.sqlite3")
	s := testStatistics(t, path, clock, 1)
	// Hold only the database boundary: enqueue never waits for this lock.
	s.dbMu.Lock()
	for range 100 {
		s.Record(RequestRecord{Outcome: OutcomeSucceeded, FinishedAt: "2026-10-01T12:00:00Z"})
	}
	s.dbMu.Unlock()
	if s.dropped.Load() == 0 {
		t.Fatal("bounded queue did not overflow")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = testStatistics(t, path, clock, 1024)
	snapshot, err := s.Snapshot(context.Background(), 30)
	if err != nil || !snapshot.Incomplete || snapshot.DroppedRequests == 0 || snapshot.Overall.Requests+snapshot.DroppedRequests != 100 {
		t.Fatalf("persisted loss missing: %+v %v", snapshot, err)
	}
	// Simulate an unclean process marker without terminating the test process.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, _ := sql.Open("sqlite", path)
	if _, err := db.Exec("UPDATE statistics_meta SET clean_shutdown=0"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	s = testStatistics(t, path, clock, 1024)
	snapshot, err = s.Snapshot(context.Background(), 1)
	if err != nil || !snapshot.Incomplete {
		t.Fatalf("unclean warning missing: %+v %v", snapshot, err)
	}
	clock.set("2026-10-30T12:00:00Z")
	snapshot, err = s.Snapshot(context.Background(), 30)
	if err != nil || snapshot.Overall.Requests == 0 || !snapshot.Incomplete {
		t.Fatalf("inclusive retention boundary was expired early: %+v %v", snapshot, err)
	}
	clock.set("2026-10-31T12:00:00Z")
	snapshot, err = s.Snapshot(context.Background(), 30)
	if err != nil || snapshot.Overall.Requests != 0 || snapshot.Incomplete || snapshot.DroppedRequests != 0 {
		t.Fatalf("expired history/warning retained: %+v %v", snapshot, err)
	}
}

func TestStatisticsIdleMaintenanceAndQueryFailure(t *testing.T) {
	clock := &statisticsClock{}
	clock.set("2026-10-01T12:00:00Z")
	s := testStatistics(t, filepath.Join(t.TempDir(), "usage.sqlite3"), clock, 1024)
	s.Record(RequestRecord{Outcome: OutcomeSucceeded})
	waitStatistics(t, s)
	clock.set("2026-10-31T12:00:00Z")
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.dbMu.Lock()
		var count int
		err := s.db.QueryRow("SELECT count(*) FROM daily_usage").Scan(&count)
		s.dbMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle retention maintenance did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.dbMu.Lock()
	_, err := s.db.Exec("DROP TABLE daily_usage")
	s.dbMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := s.Snapshot(context.Background(), 1); err != errStatisticsUnavailable || snapshot.State != "unavailable" {
		t.Fatalf("query failure hidden: %+v %v", snapshot, err)
	}
}

func TestStatisticsSnapshotIsReadOnlyAndCancellationDoesNotPoisonStore(t *testing.T) {
	clock := &statisticsClock{}
	clock.set("2026-10-01T12:00:00Z")
	s := testStatistics(t, filepath.Join(t.TempDir(), "usage.sqlite3"), clock, 1024)
	s.Record(RequestRecord{Outcome: OutcomeSucceeded})
	waitStatistics(t, s)
	s.dbMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	finished := make(chan error, 1)
	go func() { _, err := s.Snapshot(ctx, 1); finished <- err }()
	select {
	case err := <-finished:
		if err != errStatisticsUnavailable {
			t.Fatalf("canceled snapshot: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("canceled snapshot waited for database lock")
	}
	if _, err := s.db.Exec("PRAGMA query_only=1"); err != nil {
		t.Fatal(err)
	}
	s.dbMu.Unlock()
	snapshot, err := s.Snapshot(context.Background(), 1)
	s.dbMu.Lock()
	_, resetErr := s.db.Exec("PRAGMA query_only=0")
	s.dbMu.Unlock()
	if resetErr != nil {
		t.Fatal(resetErr)
	}
	if err != nil || snapshot.Overall.Requests != 1 || s.failed.Load() {
		t.Fatalf("snapshot writes or canceled query poisoned store: %+v %v", snapshot, err)
	}
}

func TestStatisticsConcurrentShutdownAndDatabaseErrors(t *testing.T) {
	clock := &statisticsClock{}
	clock.set("2026-10-17T12:00:00Z")
	path := filepath.Join(t.TempDir(), "usage.sqlite3")
	s := testStatistics(t, path, clock, 1024)
	other := NewStatistics(StatisticsOptions{Enabled: true, Path: path, Log: io.Discard})
	if !other.failed.Load() {
		t.Fatal("two owners acquired same database")
	}
	_ = other.Close()
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for range 200 {
				s.Record(RequestRecord{Outcome: OutcomeSucceeded})
			}
		})
	}
	group.Go(func() { _ = s.Close() })
	group.Wait()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = testStatistics(t, path, clock, 1024)
	s.dbMu.Lock()
	if _, err := s.db.Exec("DROP TABLE daily_usage"); err != nil {
		t.Fatal(err)
	}
	s.dbMu.Unlock()
	s.Record(RequestRecord{})
	waitStatistics(t, s)
	if snapshot, err := s.Snapshot(context.Background(), 7); err != errStatisticsUnavailable || snapshot.State != "unavailable" || snapshot.DroppedRequests == 0 {
		t.Fatalf("writer failure hidden: %+v %v", snapshot, err)
	}
}

func TestStatisticsMetricValidityAndSaturation(t *testing.T) {
	clock := &statisticsClock{}
	clock.set("2026-10-17T12:00:00Z")
	s := testStatistics(t, filepath.Join(t.TempDir(), "usage.sqlite3"), clock, 1024)
	s.Record(RequestRecord{Outcome: OutcomeSucceeded, Tokens: TokenUsage{Input: statisticsNumber(0), Output: statisticsNumber(-1), Cached: statisticsNumber(1), Reasoning: statisticsNumber(-2)}, Timing: RequestTiming{TotalMilliseconds: -1, FirstOutputMilliseconds: statisticsNumber(-1)}})
	for range 2 {
		s.Record(RequestRecord{Tokens: TokenUsage{Input: statisticsNumber(math.MaxInt64)}, Timing: RequestTiming{TotalMilliseconds: math.MaxInt64}})
	}
	waitStatistics(t, s)
	snapshot, err := s.Snapshot(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	u := snapshot.Overall
	if u.Requests != 3 || u.Failed != 2 || u.Input.Observations != 3 || u.Input.Total != math.MaxInt64 || u.Cached.Observations != 0 || u.Output.Observations != 0 || u.Reasoning.Observations != 0 || u.CacheReuse.Ratio != nil || u.FirstOutputMilliseconds.Observations != 0 {
		t.Fatalf("invalid values or integer overflow: %+v", u)
	}
}
