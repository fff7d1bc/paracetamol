package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func journalEntry(cursor, message string) []byte {
	raw, _ := json.Marshal(map[string]string{"__CURSOR": cursor, "_BOOT_ID": strings.Repeat("a", 32), "__REALTIME_TIMESTAMP": "1700000000123456", "MESSAGE": message})
	return append(raw, '\n')
}

type fakeKernel struct {
	mu           sync.Mutex
	calls        [][]string
	outputs      [][]byte
	errors       []error
	probeStarted chan struct{}
	block        bool
}

func (fake *fakeKernel) run(ctx context.Context, args ...string) ([]byte, error) {
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
		return nil, errors.New("missing bounded metadata context")
	}
	fake.mu.Lock()
	index := len(fake.calls)
	fake.calls = append(fake.calls, append([]string{}, args...))
	if index == 1 && fake.block {
		if fake.probeStarted != nil {
			close(fake.probeStarted)
			fake.probeStarted = nil
		}
		fake.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if index >= len(fake.outputs) {
		index = len(fake.outputs) - 1
	}
	raw := fake.outputs[index]
	var err error
	if index < len(fake.errors) {
		err = fake.errors[index]
	}
	fake.mu.Unlock()
	return raw, err
}

func TestKernelGuardOldFaultIgnoredNewFaultStops(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := journalEntry("s=old;i=1", "amdgpu GPU reset begin! /private/unrelated/path")
	next := append(append([]byte{}, old...), journalEntry("s=old;i=2", "amdgpu ring gfx timeout; private process /private/test")...)
	fake := &fakeKernel{outputs: [][]byte{old, next, journalEntry("s=old;i=2", "amdgpu ring gfx timeout")}}
	guarded, stop, err := startKernelGuardWith(context.Background(), dir, fake.run, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-guarded.Done():
	case <-time.After(time.Second):
		t.Fatal("new fault did not stop guard")
	}
	err = stop()
	var classified *kernelGuardError
	if !errors.As(err, &classified) || classified.Reason != "amd-ring-timeout" || !errors.As(context.Cause(guarded), &classified) {
		t.Fatalf("cause=%v stop=%v", context.Cause(guarded), err)
	}
	if err := stop(); err == nil {
		t.Fatal("second stop discarded guard failure")
	}
	fake.mu.Lock()
	calls := append([][]string{}, fake.calls...)
	fake.mu.Unlock()
	want := []string{"--dmesg", "--boot", "--no-pager", "--all", "--quiet", "--output=json", "--lines=1"}
	if len(calls) != 3 || !reflect.DeepEqual(calls[0], want) || !reflect.DeepEqual(calls[1], append(want[:6:6], "--cursor", "s=old;i=1")) || !reflect.DeepEqual(calls[2], append(want[:6:6], "--cursor", "s=old;i=2")) {
		t.Fatalf("unexpected journal arguments: %v", calls)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 3 {
		t.Fatalf("files=%v error=%v", files, err)
	}
	for _, entry := range files {
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("private")) || bytes.Contains(raw, []byte("MESSAGE")) {
			t.Fatalf("leaked raw kernel message: %s", raw)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe evidence permissions: %v", err)
		}
	}
}

func TestKernelGuardFailClosedPreflight(t *testing.T) {
	valid := journalEntry("s=old;i=1", "normal historical message")
	tests := []struct {
		name string
		raw  []byte
		err  error
	}{
		{"empty", nil, nil}, {"permission", nil, errors.New("permission denied: /private/name")},
		{"overflow", bytes.Repeat([]byte("x"), kernelReadLimit+1), nil},
		{"overflow-command", nil, kernelOverflow}, {"invalid-json", []byte("journal unavailable"), nil},
		{"missing-boot", []byte(`{"__CURSOR":"s=old","__REALTIME_TIMESTAMP":"1","MESSAGE":"x"}`), nil},
		{"missing-cursor", []byte(`{"_BOOT_ID":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","__REALTIME_TIMESTAMP":"1","MESSAGE":"x"}`), nil},
		{"multiple-baseline", append(append([]byte{}, valid...), valid...), nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeKernel{outputs: [][]byte{test.raw}, errors: []error{test.err}}
			guarded, stop, err := startKernelGuardWith(context.Background(), "", fake.run, time.Millisecond)
			if err == nil || guarded != nil || stop != nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("guarded=%v stop=%v error=%v", guarded, stop != nil, err)
			}
		})
	}
}

func TestKernelGuardCursorLossRebootAndUnreadableStop(t *testing.T) {
	old := journalEntry("s=old;i=1", "normal")
	reboot := bytes.ReplaceAll(old, []byte(strings.Repeat("a", 32)), []byte(strings.Repeat("b", 32)))
	for name, test := range map[string]struct {
		raw []byte
		err error
	}{
		"cursor-lost": {journalEntry("s=old;i=2", "normal"), nil},
		"reboot":      {reboot, nil}, "vacuum-empty": {nil, nil},
		"permission-lost": {nil, errors.New("permission denied")}, "overflow": {nil, kernelOverflow},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeKernel{outputs: [][]byte{old, test.raw}, errors: []error{nil, test.err}}
			guarded, stop, err := startKernelGuardWith(context.Background(), "", fake.run, time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-guarded.Done():
			case <-time.After(time.Second):
				t.Fatal("lost visibility did not stop guard")
			}
			if stop() == nil {
				t.Fatal("lost kernel visibility silently passed")
			}
		})
	}
}

func TestKernelGuardStopJoinsReadWithoutFaultOrFurtherWrites(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	fake := &fakeKernel{outputs: [][]byte{journalEntry("s=old;i=1", "old fault: amdgpu reset")}, probeStarted: started, block: true}
	guarded, stop, err := startKernelGuardWith(context.Background(), dir, fake.run, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	if err := stop(); err != nil {
		t.Fatalf("normal stop became guard fault: %v", err)
	}
	if guarded.Err() == nil {
		t.Fatal("stopped context remained active")
	}
	fake.mu.Lock()
	count := len(fake.calls)
	fake.mu.Unlock()
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%v error=%v", files, err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != count {
		t.Fatal("worker remained active after stop")
	}
}

func TestKernelGuardParentCausePropagatesWithoutFalseFault(t *testing.T) {
	parent, cancel := context.WithCancelCause(context.Background())
	fake := &fakeKernel{outputs: [][]byte{journalEntry("s=old;i=1", "normal")}}
	guarded, stop, err := startKernelGuardWith(parent, "", fake.run, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expected := errors.New("campaign interruption")
	cancel(expected)
	<-guarded.Done()
	if context.Cause(guarded) != expected || stop() != nil {
		t.Fatalf("cause=%v", context.Cause(guarded))
	}
}

func TestKernelGuardFinalDrainFindsFaultBeforeFirstTick(t *testing.T) {
	old := journalEntry("s=old;i=1", "healthy baseline")
	fault := append(append([]byte{}, old...), journalEntry("s=old;i=2", "amdgpu GPU reset begin!")...)
	fake := &fakeKernel{outputs: [][]byte{old, fault}}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	guarded, stop, err := startKernelGuardWith(context.Background(), dir, fake.run, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var classified *kernelGuardError
	if err := stop(); !errors.As(err, &classified) || classified.Reason != "amd-gpu-reset" {
		t.Fatalf("final fault hidden: %v", err)
	}
	if !errors.As(context.Cause(guarded), &classified) || classified.Reason != "amd-gpu-reset" {
		t.Fatalf("final cause=%v", context.Cause(guarded))
	}
	if stop() == nil {
		t.Fatal("idempotent stop lost final fault")
	}
	fake.mu.Lock()
	calls := len(fake.calls)
	fake.mu.Unlock()
	if calls != 2 {
		t.Fatalf("final scan executed %d calls, expected baseline+one drain", calls)
	}
}

func TestKernelGuardFinalVisibilityLossBlocks(t *testing.T) {
	fake := &fakeKernel{outputs: [][]byte{journalEntry("s=old;i=1", "normal"), nil}, errors: []error{nil, errors.New("permission denied")}}
	guarded, stop, err := startKernelGuardWith(context.Background(), "", fake.run, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var classified *kernelGuardError
	if err := stop(); !errors.As(err, &classified) || classified.Reason != "journal-unreadable" {
		t.Fatalf("lost final visibility passed: %v", err)
	}
	if !errors.As(context.Cause(guarded), &classified) {
		t.Fatalf("final cause=%v", context.Cause(guarded))
	}
}

func TestKernelGuardConcurrentStopDrainsExactlyOnce(t *testing.T) {
	fake := &fakeKernel{outputs: [][]byte{journalEntry("s=old;i=1", "normal")}}
	_, stop, err := startKernelGuardWith(context.Background(), "", fake.run, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var calls sync.WaitGroup
	for range 10 {
		calls.Go(func() {
			if err := stop(); err != nil {
				t.Error(err)
			}
		})
	}
	calls.Wait()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != 2 {
		t.Fatalf("stop ran more than one final read: %d", len(fake.calls))
	}
}

func TestKernelGuardFinalDrainSurvivesParentInterruption(t *testing.T) {
	parent, cancel := context.WithCancelCause(context.Background())
	old := journalEntry("s=old;i=1", "normal")
	fault := append(append([]byte{}, old...), journalEntry("s=old;i=2", "amdgpu GPU fault")...)
	fake := &fakeKernel{outputs: [][]byte{old, fault}}
	guarded, stop, err := startKernelGuardWith(parent, "", fake.run, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("campaign interrupted")
	cancel(interrupted)
	<-guarded.Done()
	if err := stop(); err == nil {
		t.Fatal("parent cancellation hid final fault")
	}
	if context.Cause(guarded) != interrupted {
		t.Fatalf("parent cause was replaced: %v", context.Cause(guarded))
	}
}

func TestKernelGuardEvidenceCollisionFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "kernel-guard-000001.json"), 0700); err != nil {
		t.Fatal(err)
	}
	fake := &fakeKernel{outputs: [][]byte{journalEntry("s=old;i=1", "normal")}}
	if _, _, err := startKernelGuardWith(context.Background(), dir, fake.run, time.Hour); err == nil {
		t.Fatal("overwrote kernel evidence")
	}
}

func TestKernelFaultClassification(t *testing.T) {
	for message, want := range map[string]string{
		"amdgpu GPU reset begin!": "amd-gpu-reset", "amdgpu ring gfx timed out": "amd-ring-timeout",
		"amdkfd: GPU memory fault": "amd-gpu-fault", "svm_range_map_to_gpu failed": "amd-svm-map-failed",
		"amdgpu detected non-finite value": "amd-nonfinite", "amdgpu NaN in hardware state": "amd-nonfinite",
		"mce: [Hardware Error]: Machine check events logged": "hardware-error",
		"llama-server[1234]: segfault at 1":                  "llama-segfault", "Out of memory: Killed process 1234 (llama-server)": "llama-oom",
		"oom-kill: task=llama-cli,pid=1234": "llama-oom", "amdgpu initialized Infinity Cache": "",
		"unrelated process segfault": "", "nvme timeout": "", "kernel normal note": "",
	} {
		if got := kernelFault(message); got != want {
			t.Errorf("%q: got %s want %s", message, got, want)
		}
	}
}

func TestKernelOutputIsBounded(t *testing.T) {
	canceled := false
	output := &kernelOutput{cancel: func() { canceled = true }}
	if _, err := output.Write(bytes.Repeat([]byte("x"), kernelReadLimit+1)); !errors.Is(err, kernelOverflow) || !canceled || output.Len() > kernelReadLimit {
		t.Fatalf("overflow was not bounded: %v", err)
	}
	if !output.overflow {
		t.Fatal("lost overflow classification")
	}
}
