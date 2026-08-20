package main

import (
	"context"
	"testing"
	"time"
)

func TestParacetamolClosesSuccessfulProbeReturningAfterSelection(t *testing.T) {
	original := probeFunc
	t.Cleanup(func() {
		probeFunc = original
	})

	winnerConn := &fakeConn{}
	lateConn := &fakeConn{}
	probeFunc = func(ctx context.Context, target target, _ time.Duration) probeResult {
		if target.raw == "winner:22" {
			time.Sleep(5 * time.Millisecond)
			return probeResult{target: target, conn: winnerConn, up: true, at: time.Now()}
		}
		<-ctx.Done()
		return probeResult{target: target, conn: lateConn, up: true, at: time.Now()}
	}

	selected, _, err := waitForTarget(config{
		targets: []target{
			{raw: "winner:22"},
			{raw: "late:22"},
		},
		selectionInterval: time.Millisecond,
		connectTimeout:    250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("waitForTarget returned error: %v", err)
	}
	if selected.conn != winnerConn {
		t.Fatal("waitForTarget did not return the winning connection")
	}
	waitForParacetamolClosedConn(t, lateConn)
	if winnerConn.closed.Load() != 0 {
		t.Fatal("waitForTarget closed the winning connection")
	}
	closeConn(selected.conn)
}

func TestParacetamolClosesSuccessfulProbeReturningAfterTimeout(t *testing.T) {
	original := probeFunc
	t.Cleanup(func() {
		probeFunc = original
	})

	lateConn := &fakeConn{}
	release := make(chan struct{})
	probeFunc = func(_ context.Context, target target, _ time.Duration) probeResult {
		<-release
		return probeResult{target: target, conn: lateConn, up: true, at: time.Now()}
	}

	started := time.Now()
	_, _, err := waitForTarget(config{
		targets:           []target{{raw: "late:22"}},
		selectionInterval: time.Millisecond,
		connectTimeout:    20 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("waitForTarget succeeded, want timeout")
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("waitForTarget delayed timeout for %v", elapsed)
	}

	close(release)
	waitForParacetamolClosedConn(t, lateConn)
}

func waitForParacetamolClosedConn(t *testing.T, conn *fakeConn) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatal("late successful connection was not closed")
		case <-ticker.C:
			if conn.closed.Load() > 0 {
				return
			}
		}
	}
}
