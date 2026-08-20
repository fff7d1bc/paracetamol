package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"
)

type fakeLifecycle struct {
	mu       sync.Mutex
	events   []string
	startErr map[Allocation]error
}

func (fake *fakeLifecycle) Start(_ context.Context, allocation Allocation) (*url.URL, error) {
	fake.mu.Lock()
	fake.events = append(fake.events, "start "+string(allocation))
	err := fake.startErr[allocation]
	fake.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return url.Parse("http://127.0.0.1/" + string(allocation))
}

func (fake *fakeLifecycle) Stop(_ context.Context, allocation Allocation) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.events = append(fake.events, "stop "+string(allocation))
	return nil
}

func (fake *fakeLifecycle) Events() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string(nil), fake.events...)
}

func acquireAsync(scheduler *Scheduler, allocation Allocation) <-chan acquireResult {
	result := make(chan acquireResult, 1)
	go func() {
		lease, err := scheduler.Acquire(context.Background(), allocation)
		result <- acquireResult{lease: lease, err: err}
	}()
	return result
}

func receiveLease(t *testing.T, result <-chan acquireResult) *Lease {
	t.Helper()
	select {
	case acquired := <-result:
		if acquired.err != nil {
			t.Fatal(acquired.err)
		}
		return acquired.lease
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lease")
		return nil
	}
}

func assertWaiting(t *testing.T, result <-chan acquireResult) {
	t.Helper()
	select {
	case acquired := <-result:
		t.Fatalf("request unexpectedly completed: %#v", acquired)
	case <-time.After(20 * time.Millisecond):
	}
}

func waitForStatus(t *testing.T, scheduler *Scheduler, predicate func(SchedulerStatus) bool) SchedulerStatus {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, err := scheduler.Status(context.Background())
		if err == nil && predicate(status) {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for scheduler state")
	return SchedulerStatus{}
}

func TestSchedulerPermitsCompatibleConcurrency(t *testing.T) {
	fake := &fakeLifecycle{}
	scheduler, err := NewScheduler(fake, 64, map[Allocation]int{AllocationDwarfStar: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	second := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	status, err := scheduler.Status(context.Background())
	if err != nil || status.ActiveRequests != 2 || status.Allocation != AllocationLlamaCPP {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	first.Release()
	second.Release()
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerDrainsInFIFOOrderAcrossAllocations(t *testing.T) {
	fake := &fakeLifecycle{}
	scheduler, _ := NewScheduler(fake, 64, map[Allocation]int{AllocationDwarfStar: 1})
	llama := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	dwarfResult := acquireAsync(scheduler, AllocationDwarfStar)
	waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.QueuedRequests == 1 && status.State == StateDraining })
	lateLlamaResult := acquireAsync(scheduler, AllocationLlamaCPP)
	assertWaiting(t, dwarfResult)
	assertWaiting(t, lateLlamaResult)
	llama.Release()
	dwarf := receiveLease(t, dwarfResult)
	assertWaiting(t, lateLlamaResult)
	dwarf.Release()
	lateLlama := receiveLease(t, lateLlamaResult)
	lateLlama.Release()
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "[start llama-cpp stop llama-cpp start dwarfstar stop dwarfstar start llama-cpp stop llama-cpp]"
	if got := fmt.Sprint(fake.Events()); got != want {
		t.Fatalf("events=%s, want %s", got, want)
	}
}

func TestSchedulerRemovesCancelledQueuedRequest(t *testing.T) {
	fake := &fakeLifecycle{}
	scheduler, _ := NewScheduler(fake, 64, map[Allocation]int{AllocationDwarfStar: 1})
	llama := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := scheduler.Acquire(ctx, AllocationDwarfStar); result <- err }()
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	llama.Release()
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(fake.Events()); got != "[start llama-cpp stop llama-cpp]" {
		t.Fatalf("events=%s", got)
	}
}

func TestSchedulerReturnsStartFailureAndCanServeAnotherAllocation(t *testing.T) {
	fake := &fakeLifecycle{startErr: map[Allocation]error{AllocationDwarfStar: errors.New("load failed")}}
	scheduler, _ := NewScheduler(fake, 64, nil)
	if _, err := scheduler.Acquire(context.Background(), AllocationDwarfStar); err == nil {
		t.Fatal("backend start failure was hidden")
	}
	lease := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	lease.Release()
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerBoundsWaitingQueue(t *testing.T) {
	fake := &fakeLifecycle{}
	scheduler, _ := NewScheduler(fake, 1, map[Allocation]int{AllocationDwarfStar: 1})
	llama := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	waiting := acquireAsync(scheduler, AllocationDwarfStar)
	waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.QueuedRequests == 1 })
	if _, err := scheduler.Acquire(context.Background(), AllocationLlamaCPP); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue overflow err=%v", err)
	}
	llama.Release()
	dwarf := receiveLease(t, waiting)
	dwarf.Release()
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
