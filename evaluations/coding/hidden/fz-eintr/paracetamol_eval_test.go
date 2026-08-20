package main

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParacetamolEvalRetryEINTRUntilSuccess(t *testing.T) {
	calls := 0
	got, err := retryEINTR(func() (int, error) {
		calls++
		if calls < 3 {
			return 0, unix.EINTR
		}
		return 42, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 || calls != 3 {
		t.Fatalf("result/calls = %d/%d, want 42/3", got, calls)
	}
}

func TestParacetamolEvalRetryEINTRReturnsOtherErrors(t *testing.T) {
	wantErr := errors.New("permanent failure")
	calls := 0
	_, err := retryEINTR(func() (int, error) {
		calls++
		return 0, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
