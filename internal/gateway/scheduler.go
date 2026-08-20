package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
)

type Allocation string

const (
	AllocationLlamaCPP  Allocation = "llama-cpp"
	AllocationDwarfStar Allocation = "dwarfstar"
)

type AllocationState string

const (
	StateUnloaded AllocationState = "unloaded"
	StateStarting AllocationState = "starting"
	StateReady    AllocationState = "ready"
	StateDraining AllocationState = "draining"
	StateStopping AllocationState = "stopping"
	StateFailed   AllocationState = "failed"
)

var (
	ErrQueueFull    = errors.New("gateway request queue is full")
	ErrShuttingDown = errors.New("gateway is shutting down")
)

type Lifecycle interface {
	Start(context.Context, Allocation) (*url.URL, error)
	Stop(context.Context, Allocation) error
}

type SchedulerStatus struct {
	Allocation     Allocation      `json:"active_allocation,omitempty"`
	State          AllocationState `json:"allocation_state"`
	ActiveRequests int             `json:"active_requests"`
	QueuedRequests int             `json:"queued_requests"`
	LastError      string          `json:"last_error,omitempty"`
}

type Scheduler struct {
	lifecycle Lifecycle
	queueMax  int
	limits    map[Allocation]int

	requests chan acquireRequest
	cancels  chan uint64
	releases chan leaseEvent
	failures chan failureEvent
	results  chan operationResult
	statuses chan statusRequest
	shutdown chan shutdownRequest
	done     chan struct{}
	nextID   atomic.Uint64
}

type acquireRequest struct {
	id         uint64
	ctx        context.Context
	allocation Allocation
	reply      chan acquireResult
}

type acquireResult struct {
	lease *Lease
	err   error
}

type waiter struct{ acquireRequest }

type leaseEvent struct {
	allocation Allocation
	generation uint64
}

type failureEvent struct {
	leaseEvent
	err error
}

type operationResult struct {
	kind       string
	allocation Allocation
	generation uint64
	upstream   *url.URL
	err        error
}

type statusRequest struct{ reply chan SchedulerStatus }
type shutdownRequest struct{ reply chan error }

type Lease struct {
	Upstream   *url.URL
	scheduler  *Scheduler
	allocation Allocation
	generation uint64
	release    sync.Once
	failure    sync.Once
}

func NewScheduler(lifecycle Lifecycle, queueMax int, limits map[Allocation]int) (*Scheduler, error) {
	if lifecycle == nil || queueMax < 1 {
		return nil, fmt.Errorf("invalid gateway scheduler configuration")
	}
	cloned := make(map[Allocation]int, len(limits))
	for allocation, limit := range limits {
		if allocation != AllocationLlamaCPP && allocation != AllocationDwarfStar || limit < 0 {
			return nil, fmt.Errorf("invalid concurrency limit for %q", allocation)
		}
		cloned[allocation] = limit
	}
	scheduler := &Scheduler{
		lifecycle: lifecycle, queueMax: queueMax, limits: cloned,
		requests: make(chan acquireRequest), cancels: make(chan uint64, queueMax),
		releases: make(chan leaseEvent, queueMax+1), failures: make(chan failureEvent, queueMax+1),
		results: make(chan operationResult, 1), statuses: make(chan statusRequest),
		shutdown: make(chan shutdownRequest), done: make(chan struct{}),
	}
	go scheduler.run()
	return scheduler, nil
}

func (scheduler *Scheduler) Acquire(ctx context.Context, allocation Allocation) (*Lease, error) {
	if allocation != AllocationLlamaCPP && allocation != AllocationDwarfStar {
		return nil, fmt.Errorf("unknown gateway allocation %q", allocation)
	}
	request := acquireRequest{id: scheduler.nextID.Add(1), ctx: ctx, allocation: allocation, reply: make(chan acquireResult)}
	select {
	case scheduler.requests <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-scheduler.done:
		return nil, ErrShuttingDown
	}
	select {
	case result := <-request.reply:
		return result.lease, result.err
	case <-ctx.Done():
		select {
		case scheduler.cancels <- request.id:
		case <-scheduler.done:
		}
		return nil, ctx.Err()
	case <-scheduler.done:
		return nil, ErrShuttingDown
	}
}

func (lease *Lease) Release() {
	if lease == nil || lease.scheduler == nil {
		return
	}
	lease.release.Do(func() {
		select {
		case lease.scheduler.releases <- leaseEvent{allocation: lease.allocation, generation: lease.generation}:
		case <-lease.scheduler.done:
		}
	})
}

func (lease *Lease) Fail(err error) {
	if lease == nil || lease.scheduler == nil || err == nil {
		return
	}
	lease.failure.Do(func() {
		select {
		case lease.scheduler.failures <- failureEvent{leaseEvent: leaseEvent{allocation: lease.allocation, generation: lease.generation}, err: err}:
		case <-lease.scheduler.done:
		}
	})
}

func (scheduler *Scheduler) Status(ctx context.Context) (SchedulerStatus, error) {
	request := statusRequest{reply: make(chan SchedulerStatus, 1)}
	select {
	case scheduler.statuses <- request:
	case <-ctx.Done():
		return SchedulerStatus{}, ctx.Err()
	case <-scheduler.done:
		return SchedulerStatus{State: StateUnloaded}, ErrShuttingDown
	}
	select {
	case status := <-request.reply:
		return status, nil
	case <-ctx.Done():
		return SchedulerStatus{}, ctx.Err()
	case <-scheduler.done:
		return SchedulerStatus{State: StateUnloaded}, ErrShuttingDown
	}
}

func (scheduler *Scheduler) Shutdown(ctx context.Context) error {
	request := shutdownRequest{reply: make(chan error, 1)}
	select {
	case scheduler.shutdown <- request:
	case <-scheduler.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.reply:
		return err
	case <-scheduler.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (scheduler *Scheduler) run() {
	state := StateUnloaded
	var current Allocation
	var upstream *url.URL
	var generation uint64
	active := 0
	queue := []*waiter{}
	operation := false
	var operationCancel context.CancelFunc
	shuttingDown := false
	var shutdownReply chan error
	lastError := ""

	failWaiter := func(waiting *waiter, err error) {
		select {
		case waiting.reply <- acquireResult{err: err}:
		case <-waiting.ctx.Done():
		}
	}
	prune := func() {
		kept := queue[:0]
		for _, waiting := range queue {
			if waiting.ctx.Err() == nil {
				kept = append(kept, waiting)
			}
		}
		queue = kept
	}
	start := func(allocation Allocation) {
		operation = true
		state = StateStarting
		current = allocation
		generation++
		operationContext, cancel := context.WithCancel(context.Background())
		operationCancel = cancel
		go func(selected Allocation, selectedGeneration uint64) {
			resolved, err := scheduler.lifecycle.Start(operationContext, selected)
			scheduler.results <- operationResult{kind: "start", allocation: selected, generation: selectedGeneration, upstream: resolved, err: err}
		}(allocation, generation)
	}
	stop := func() {
		operation = true
		state = StateStopping
		operationCancel = nil
		selected, selectedGeneration := current, generation
		go func() {
			err := scheduler.lifecycle.Stop(context.Background(), selected)
			scheduler.results <- operationResult{kind: "stop", allocation: selected, generation: selectedGeneration, err: err}
		}()
	}
	finish := func(err error) {
		if shutdownReply != nil {
			shutdownReply <- err
		}
		close(scheduler.done)
	}
	var progress func() bool
	progress = func() bool {
		prune()
		if shuttingDown {
			for _, waiting := range queue {
				failWaiter(waiting, ErrShuttingDown)
			}
			queue = nil
			if operation || active > 0 {
				if active > 0 && !operation {
					state = StateDraining
				}
				return false
			}
			if current != "" {
				stop()
				return false
			}
			finish(nil)
			return true
		}
		if operation {
			return false
		}
		if state == StateFailed && current != "" {
			if active == 0 {
				stop()
			}
			return false
		}
		if current == "" {
			state = StateUnloaded
			if len(queue) > 0 {
				start(queue[0].allocation)
			}
			return false
		}
		if len(queue) == 0 {
			state = StateReady
			return false
		}
		if queue[0].allocation != current {
			state = StateDraining
			if active == 0 {
				stop()
			}
			return false
		}
		state = StateReady
		limit := scheduler.limits[current]
		for len(queue) > 0 && queue[0].allocation == current && (limit == 0 || active < limit) {
			waiting := queue[0]
			queue = queue[1:]
			lease := &Lease{Upstream: upstream, scheduler: scheduler, allocation: current, generation: generation}
			select {
			case waiting.reply <- acquireResult{lease: lease}:
				active++
			case <-waiting.ctx.Done():
			}
		}
		if len(queue) > 0 && queue[0].allocation != current {
			state = StateDraining
		}
		return false
	}

	for {
		if progress() {
			return
		}
		select {
		case request := <-scheduler.requests:
			if shuttingDown {
				failWaiter(&waiter{request}, ErrShuttingDown)
			} else if len(queue) >= scheduler.queueMax {
				failWaiter(&waiter{request}, ErrQueueFull)
			} else {
				queue = append(queue, &waiter{request})
			}
		case id := <-scheduler.cancels:
			for index, waiting := range queue {
				if waiting.id == id {
					queue = append(queue[:index], queue[index+1:]...)
					break
				}
			}
		case event := <-scheduler.releases:
			if event.allocation == current && event.generation == generation && active > 0 {
				active--
			}
		case event := <-scheduler.failures:
			if event.allocation == current && event.generation == generation {
				state = StateFailed
				lastError = event.err.Error()
			}
		case result := <-scheduler.results:
			operation = false
			if operationCancel != nil {
				operationCancel()
				operationCancel = nil
			}
			if result.generation != generation || result.allocation != current {
				continue
			}
			switch result.kind {
			case "start":
				if result.err != nil || result.upstream == nil {
					if result.err == nil {
						result.err = errors.New("backend returned no upstream URL")
					}
					lastError = result.err.Error()
					failed := current
					current, upstream, state = "", nil, StateUnloaded
					kept := queue[:0]
					for _, waiting := range queue {
						if waiting.allocation == failed {
							failWaiter(waiting, fmt.Errorf("start %s: %w", failed, result.err))
						} else {
							kept = append(kept, waiting)
						}
					}
					queue = kept
				} else {
					upstream, state, lastError = result.upstream, StateReady, ""
				}
			case "stop":
				if result.err != nil {
					state, lastError = StateFailed, result.err.Error()
					for _, waiting := range queue {
						failWaiter(waiting, fmt.Errorf("stop %s: %w", current, result.err))
					}
					queue = nil
					if shuttingDown {
						finish(result.err)
						return
					}
				} else {
					current, upstream, state = "", nil, StateUnloaded
				}
			}
		case request := <-scheduler.statuses:
			request.reply <- SchedulerStatus{Allocation: current, State: state, ActiveRequests: active, QueuedRequests: len(queue), LastError: lastError}
		case request := <-scheduler.shutdown:
			if !shuttingDown {
				shuttingDown, shutdownReply = true, request.reply
				if operationCancel != nil {
					operationCancel()
				}
			} else {
				request.reply <- nil
			}
		}
	}
}
