package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"paracetamol/tools/llama-acceptance/internal/contract"
	"paracetamol/tools/llama-acceptance/internal/scenarios"
	"paracetamol/tools/llama-acceptance/internal/transport"
)

func TestShortParentInterruptionsRemainBlocked(t *testing.T) {
	for _, cause := range []string{"memory", "kernel", "user"} {
		for _, withID := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/request-id-%t", cause, withID), func(t *testing.T) {
				parent, cancel := context.WithCancelCause(context.Background())
				defer cancel(nil)
				ctx := context.Context(parent)
				if cause == "memory" {
					var stop context.CancelFunc
					ctx, stop = memoryGuard(parent, math.MaxInt64)
					defer stop()
					if ctx.Err() == nil || !strings.Contains(context.Cause(ctx).Error(), "memory guard") {
						t.Fatalf("memory guard did not cancel: %v", context.Cause(ctx))
					}
				}
				calls := newResultCalls(ctx, func(callCtx context.Context, _ string, _ map[string]any) (contract.Response, error) {
					if cause == "kernel" {
						cancel(kernelBlocked("gpu-reset"))
					} else if cause == "user" {
						cancel(context.Canceled)
					}
					response := contract.Response{}
					if withID {
						response.RequestID = "interrupted-request"
					}
					return response, &transport.Error{Kind: transport.Infrastructure, Err: callCtx.Err()}
				}, io.Discard)
				results := scenarios.Short(ctx, contract.Options{}, calls.call)
				for i := range results {
					calls.classify(&results[i])
				}
				if len(results) != 1 || results[0].Status != contract.Blocked || contract.Worst(results) != contract.Blocked {
					t.Fatalf("interruption became a model failure: %+v", results)
				}
			})
		}
	}
}

func TestCancellationCannotHideIndependentTransportFailures(t *testing.T) {
	for _, kind := range []string{transport.Policy, transport.Protocol, transport.Infrastructure} {
		t.Run(kind, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := newResultCalls(parent, func(context.Context, string, map[string]any) (contract.Response, error) {
				cancel()
				return contract.Response{RequestID: "interrupted-request"}, errors.Join(context.Canceled, &transport.Error{Kind: kind, Err: errors.New("independent validation or persistence failure")})
			}, io.Discard)
			results := scenarios.Short(parent, contract.Options{}, calls.call)
			calls.classify(&results[0])
			want := contract.Fail
			if kind == transport.Infrastructure {
				want = contract.Blocked
			}
			if results[0].Status != want {
				t.Fatalf("%s hidden by cancellation: %+v", kind, results)
			}
			// Even scenarios that report parent interruption as BLOCKED must not
			// conceal an independently observed policy/protocol violation.
			result := contract.Result{Name: "composite", Status: contract.Blocked, RequestIDs: []string{"interrupted-request"}}
			calls.classify(&result)
			if result.Status != want {
				t.Fatalf("parent-blocked scenario hid independent failure: %+v", result)
			}
		})
	}
}

func TestIntentionalChildCancellationRequiresVerificationAndAllowsRecovery(t *testing.T) {
	for _, verified := range []bool{false, true} {
		t.Run(fmt.Sprintf("verified-%t", verified), func(t *testing.T) {
			parent := context.Background()
			child, cancel := context.WithCancel(parent)
			defer cancel()
			invocations := 0
			calls := newResultCalls(parent, func(ctx context.Context, name string, _ map[string]any) (contract.Response, error) {
				invocations++
				if name == "recovery" {
					return contract.Response{Text: "RECOVERED", Finish: "stop"}, nil
				}
				cancel()
				return contract.Response{RequestID: "intentional", CancellationVerified: verified}, &transport.Error{Kind: transport.Infrastructure, Err: ctx.Err()}
			}, io.Discard)
			response, err := calls.call(child, "cancel-stream-false", nil)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("lost intentional cancellation: %v", err)
			}
			result := contract.Result{Name: "cancel-stream-false", Status: contract.Fail, Detail: "transport did not verify cancellation", RequestIDs: []string{response.RequestID}}
			if response.CancellationVerified {
				result.Status = contract.Pass
			}
			calls.classify(&result)
			if verified {
				if result.Status != contract.Pass {
					t.Fatalf("verified child inherited blocked status: %+v", result)
				}
				recovery, err := calls.call(parent, "recovery", nil)
				if err != nil || recovery.Text != "RECOVERED" || invocations != 2 {
					t.Fatalf("verified cancellation poisoned recovery: %+v %v", recovery, err)
				}
			} else if result.Status == contract.Pass {
				t.Fatalf("unverified cancellation passed: %+v", result)
			}
		})
	}
}

func TestEarlierOracleFailureSurvivesParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := newResultCalls(parent, func(ctx context.Context, name string, _ map[string]any) (contract.Response, error) {
		if name == "completed" {
			return contract.Response{RequestID: name, Text: "WRONG", Finish: "stop"}, nil
		}
		cancel()
		return contract.Response{RequestID: name}, &transport.Error{Kind: transport.Infrastructure, Err: ctx.Err()}
	}, io.Discard)
	_, _ = calls.call(parent, "completed", nil)
	_, err := calls.call(parent, "interrupted", nil)
	results := []contract.Result{
		{Name: "completed", Status: contract.Fail, Detail: "exact answer oracle rejected response", RequestIDs: []string{"completed"}},
		{Name: "interrupted", Status: contract.Fail, Detail: err.Error(), RequestIDs: []string{"interrupted"}},
		{Name: "composite", Status: contract.Fail, Detail: "exact answer oracle rejected response", RequestIDs: []string{"completed", "interrupted"}},
	}
	for i := range results {
		calls.classify(&results[i])
	}
	if results[0].Status != contract.Fail || results[1].Status != contract.Blocked || results[2].Status != contract.Fail || contract.Worst(results) != contract.Fail {
		t.Fatalf("parent cancellation erased deterministic evidence: %+v", results)
	}
}

func TestIndependentOracleFailureSurvivesSiblingInfrastructureError(t *testing.T) {
	calls := newResultCalls(context.Background(), func(context.Context, string, map[string]any) (contract.Response, error) {
		return contract.Response{RequestID: "unavailable"}, &transport.Error{Kind: transport.Infrastructure, Err: errors.New("report was not retained")}
	}, io.Discard)
	_, _ = calls.call(context.Background(), "worker-1", nil)
	result := contract.Result{Name: "four-worker-isolation", Status: contract.Fail, Detail: "exact answer oracle rejected response", RequestIDs: []string{"completed", "unavailable"}}
	calls.classify(&result)
	if result.Status != contract.Fail {
		t.Fatalf("independent answer miss hidden by unavailable sibling: %+v", result)
	}
}

func TestIncompleteOperationBlocksReviewButKeepsIndependentFailure(t *testing.T) {
	setupErr := errors.New("required observation unavailable")
	for _, status := range []contract.Status{contract.Pass, contract.Review, contract.Blocked, contract.Fail} {
		results := []contract.Result{{Name: "retained-case", Status: status}}
		want := contract.Blocked
		if status == contract.Fail {
			want = contract.Fail
		}
		if got := operationStatus(results, setupErr); got != want {
			t.Fatalf("setup interruption changed %s to %s, expected %s", status, got, want)
		}
		if got := operationStatus(results, nil); got != status {
			t.Fatalf("complete operation changed %s to %s", status, got)
		}
	}
}

func TestCompositeCancellationBeforeRequestIDIsBlocked(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := newResultCalls(parent, func(ctx context.Context, _ string, _ map[string]any) (contract.Response, error) {
		cancel()
		return contract.Response{}, &transport.Error{Kind: transport.Infrastructure, Err: ctx.Err()}
	}, io.Discard)
	_, err := calls.call(parent, "nested-tool-stream-false-call", nil)
	result := contract.Result{Name: "nested-tool-stream-false", Status: contract.Fail, Detail: err.Error()}
	calls.classify(&result)
	if result.Status != contract.Blocked {
		t.Fatalf("composite with no request ID lost interruption attribution: %+v", result)
	}
}

func TestVerifiedCancellationFlagCannotHidePolicyOrProtocolError(t *testing.T) {
	for _, kind := range []string{transport.Policy, transport.Protocol} {
		t.Run(kind, func(t *testing.T) {
			child, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := newResultCalls(context.Background(), func(context.Context, string, map[string]any) (contract.Response, error) {
				cancel()
				return contract.Response{RequestID: "intentional", CancellationVerified: true}, errors.Join(context.Canceled, &transport.Error{Kind: kind, Err: errors.New("independent validation failure")})
			}, io.Discard)
			_, _ = calls.call(child, "cancel-stream-false", nil)
			result := contract.Result{Name: "cancel-stream-false", Status: contract.Pass, RequestIDs: []string{"intentional"}}
			calls.classify(&result)
			if result.Status != contract.Fail {
				t.Fatalf("verification flag hid independent %s failure: %+v", kind, result)
			}
		})
	}
}
