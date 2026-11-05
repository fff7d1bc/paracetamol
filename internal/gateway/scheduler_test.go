package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeLifecycle struct {
	mu       sync.Mutex
	events   []string
	startErr map[Allocation]error
	stopHook func() error
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
	fake.events = append(fake.events, "stop "+string(allocation))
	fake.mu.Unlock()
	if fake.stopHook != nil {
		return fake.stopHook()
	}
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

const testAllocation Allocation = "test-backend"

func TestSchedulerPermitsCompatibleConcurrency(t *testing.T) {
	fake := &fakeLifecycle{}
	scheduler, err := NewScheduler(fake, 64, map[Allocation]int{testAllocation: 1})
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
	scheduler, _ := NewScheduler(fake, 64, map[Allocation]int{testAllocation: 1})
	llama := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	otherResult := acquireAsync(scheduler, testAllocation)
	waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.QueuedRequests == 1 && status.State == StateDraining })
	lateLlamaResult := acquireAsync(scheduler, AllocationLlamaCPP)
	assertWaiting(t, otherResult)
	assertWaiting(t, lateLlamaResult)
	llama.Release()
	other := receiveLease(t, otherResult)
	assertWaiting(t, lateLlamaResult)
	other.Release()
	lateLlama := receiveLease(t, lateLlamaResult)
	lateLlama.Release()
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "[start llama-cpp stop llama-cpp start test-backend stop test-backend start llama-cpp stop llama-cpp]"
	if got := fmt.Sprint(fake.Events()); got != want {
		t.Fatalf("events=%s, want %s", got, want)
	}
}

func TestSchedulerRemovesCancelledQueuedRequest(t *testing.T) {
	fake := &fakeLifecycle{}
	scheduler, _ := NewScheduler(fake, 64, map[Allocation]int{testAllocation: 1})
	llama := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := scheduler.Acquire(ctx, testAllocation); result <- err }()
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
	fake := &fakeLifecycle{startErr: map[Allocation]error{testAllocation: errors.New("load failed")}}
	scheduler, _ := NewScheduler(fake, 64, map[Allocation]int{testAllocation: 1})
	if _, err := scheduler.Acquire(context.Background(), testAllocation); err == nil {
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
	scheduler, _ := NewScheduler(fake, 1, map[Allocation]int{testAllocation: 1})
	llama := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
	waiting := acquireAsync(scheduler, testAllocation)
	waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.QueuedRequests == 1 })
	if _, err := scheduler.Acquire(context.Background(), AllocationLlamaCPP); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue overflow err=%v", err)
	}
	llama.Release()
	other := receiveLease(t, waiting)
	other.Release()
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerStopFailureIsStableUntilExplicitShutdown(t *testing.T) {
	for _, retryFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("shutdown-fails=%t", retryFails), func(t *testing.T) {
			stopErr := errors.New("cannot remove owned backend")
			retryStarted := make(chan struct{})
			retryAllowed := make(chan struct{})
			var stopCalls atomic.Int32
			fake := &fakeLifecycle{stopHook: func() error {
				if stopCalls.Add(1) == 1 {
					return stopErr
				}
				close(retryStarted)
				<-retryAllowed
				if retryFails {
					return stopErr
				}
				return nil
			}}
			scheduler, err := NewScheduler(fake, 64, map[Allocation]int{testAllocation: 1})
			if err != nil {
				t.Fatal(err)
			}
			first := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
			second := receiveLease(t, acquireAsync(scheduler, AllocationLlamaCPP))
			first.Fail(errors.New("transport failed"))
			waitForStatus(t, scheduler, func(status SchedulerStatus) bool {
				return status.State == StateFailed && status.ActiveRequests == 2
			})
			queued := []<-chan acquireResult{acquireAsync(scheduler, testAllocation), acquireAsync(scheduler, AllocationLlamaCPP)}
			waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.QueuedRequests == 2 })
			first.Release()
			waitForStatus(t, scheduler, func(status SchedulerStatus) bool { return status.ActiveRequests == 1 })
			if stopCalls.Load() != 0 {
				t.Fatal("stopped backend before every active lease drained")
			}
			second.Release()
			for _, result := range queued {
				select {
				case acquired := <-result:
					if acquired.lease != nil || !errors.Is(acquired.err, stopErr) {
						t.Fatalf("queued result=%#v", acquired)
					}
				case <-time.After(time.Second):
					t.Fatal("stop failure left a queued request waiting")
				}
			}
			// A late failure from the old lease must not overwrite the cleanup
			// failure or make status/admission events retry the failed removal.
			second.Fail(errors.New("late transport failure"))
			var callers sync.WaitGroup
			for range 16 {
				callers.Go(func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					if lease, err := scheduler.Acquire(ctx, AllocationLlamaCPP); lease != nil || !errors.Is(err, stopErr) {
						t.Errorf("future acquisition: lease=%v err=%v", lease, err)
					}
				})
			}
			callers.Wait()
			for range 20 {
				status, err := scheduler.Status(context.Background())
				if err != nil || status.State != StateFailed || status.Allocation != AllocationLlamaCPP || status.ActiveRequests != 0 || status.QueuedRequests != 0 || status.LastError != stopErr.Error() {
					t.Fatalf("failed residency status=%#v err=%v", status, err)
				}
			}
			if stopCalls.Load() != 1 || fmt.Sprint(fake.Events()) != "[start llama-cpp stop llama-cpp]" {
				t.Fatalf("uncontrolled cleanup retries: %v", fake.Events())
			}

			shutdown := make(chan error, 1)
			go func() { shutdown <- scheduler.Shutdown(context.Background()) }()
			select {
			case <-retryStarted:
			case <-time.After(time.Second):
				t.Fatal("shutdown did not retry cleanup")
			}
			// Submit overlapping shutdowns through the actor boundary so all are
			// known to be pending before the single cleanup retry completes.
			replies := []chan error{}
			for range 8 {
				reply := make(chan error, 1)
				scheduler.shutdown <- shutdownRequest{reply: reply}
				replies = append(replies, reply)
				select {
				case err := <-reply:
					t.Fatalf("overlapping shutdown finished before cleanup: %v", err)
				default:
				}
			}
			close(retryAllowed)
			replies = append(replies, shutdown)
			for _, reply := range replies {
				select {
				case err := <-reply:
					if retryFails != errors.Is(err, stopErr) || (!retryFails && err != nil) {
						t.Fatalf("shutdown error=%v, retryFails=%t", err, retryFails)
					}
				case <-time.After(time.Second):
					t.Fatal("shutdown did not finish")
				}
			}
			if stopCalls.Load() != 2 {
				t.Fatalf("shutdown cleanup attempts=%d, want 2 total", stopCalls.Load())
			}
			if err := scheduler.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := scheduler.Acquire(context.Background(), AllocationLlamaCPP); !errors.Is(err, ErrShuttingDown) {
				t.Fatalf("post-shutdown acquire=%v", err)
			}
		})
	}
}
