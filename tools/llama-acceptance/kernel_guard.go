package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"paracetamol/internal/atomicfile"
)

const kernelReadLimit = 1 << 20

type kernelCommand func(context.Context, ...string) ([]byte, error)
type kernelEntry struct {
	Cursor  string  `json:"__CURSOR"`
	Boot    string  `json:"_BOOT_ID"`
	Time    string  `json:"__REALTIME_TIMESTAMP"`
	Message *string `json:"MESSAGE"`
}
type kernelObservation struct {
	Reason string `json:"reason"`
	Cursor string `json:"cursor,omitempty"`
	Boot   string `json:"boot_id,omitempty"`
	Time   string `json:"realtime_microseconds,omitempty"`
}
type kernelGuardError struct{ Reason string }

func (e *kernelGuardError) Error() string { return "kernel guard BLOCKED: " + e.Reason }
func kernelBlocked(reason string) error   { return &kernelGuardError{Reason: reason} }

var kernelOverflow = errors.New("kernel journal output exceeded bound")
var kernelBootID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var kernelCursor = regexp.MustCompile(`^[A-Za-z0-9_=;:-]+$`)
var kernelNonfinite = regexp.MustCompile(`\b(nan|inf|non-?finite)\b`)

// startKernelGuard never clears the journal or changes access policy. A baseline
// fault is historical; only entries after its verified current-boot cursor can
// stop this campaign. Empty evidenceDir permits read-only preflight.
func startKernelGuard(ctx context.Context, evidenceDir string) (context.Context, func() error, error) {
	return startKernelGuardWith(ctx, evidenceDir, runKernelJournal, 2*time.Second)
}

func startKernelGuardWith(parent context.Context, dir string, command kernelCommand, interval time.Duration) (context.Context, func() error, error) {
	if dir != "" {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return nil, nil, kernelBlocked("evidence-directory-unavailable")
		}
	}
	sequence := 0
	save := func(reason string, entry kernelEntry) error {
		if dir == "" {
			return nil
		}
		sequence++
		observation := kernelObservation{Reason: reason, Cursor: entry.Cursor, Boot: entry.Boot, Time: entry.Time}
		if err := atomicfile.JSON(filepath.Join(dir, fmt.Sprintf("kernel-guard-%06d.json", sequence)), observation, 0600, atomicfile.Create); err != nil {
			return kernelBlocked("evidence-save-failed")
		}
		return nil
	}
	args := []string{"--dmesg", "--boot", "--no-pager", "--all", "--quiet", "--output=json"}
	baseline, err := readKernelEntries(parent, command, append(append([]string{}, args...), "--lines=1")...)
	if err != nil {
		return nil, nil, errors.Join(err, save("baseline-unavailable", kernelEntry{}))
	}
	if len(baseline) != 1 {
		err = kernelBlocked("baseline-cursor-unavailable")
		return nil, nil, errors.Join(err, save("baseline-unavailable", kernelEntry{}))
	}
	current := baseline[0]
	if err := save("baseline", current); err != nil {
		return nil, nil, err
	}
	guarded, cancel := context.WithCancelCause(parent)
	worker, stopWorker := context.WithCancel(guarded)
	done := make(chan struct{})
	var cause error
	fail := func(err error, reason string, entry kernelEntry) error { return errors.Join(err, save(reason, entry)) }
	scan := func(ctx context.Context) error {
		// Include the old cursor. --after-cursor cannot establish that a
		// vacuumed cursor still exists; exact first-record equality can.
		entries, err := readKernelEntries(ctx, command, append(append([]string{}, args...), "--cursor", current.Cursor)...)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fail(err, "journal-read-failed", current)
		}
		if len(entries) == 0 {
			return fail(kernelBlocked("cursor-lost"), "cursor-lost", current)
		}
		if entries[0].Boot != current.Boot {
			return fail(kernelBlocked("boot-changed"), "boot-changed", entries[0])
		}
		if entries[0].Cursor != current.Cursor {
			return fail(kernelBlocked("cursor-lost"), "cursor-lost", current)
		}
		seen := map[string]bool{current.Cursor: true}
		for _, entry := range entries[1:] {
			if entry.Boot != current.Boot || seen[entry.Cursor] {
				return fail(kernelBlocked("journal-continuity-lost"), "journal-continuity-lost", current)
			}
			seen[entry.Cursor] = true
			if reason := kernelFault(*entry.Message); reason != "" {
				current = entry
				return fail(kernelBlocked(reason), reason, entry)
			}
		}
		if len(entries) > 1 {
			current = entries[len(entries)-1]
			return save("cursor-advanced", current)
		}
		return nil
	}
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-worker.Done():
				return
			case <-tick.C:
			}
			if worker.Err() != nil {
				return
			}
			if err := scan(worker); err != nil {
				if worker.Err() != nil && errors.Is(err, worker.Err()) {
					return
				}
				cause = err
				cancel(cause)
				return
			}
		}
	}()
	var once sync.Once
	return guarded, func() error {
		once.Do(func() {
			stopWorker()
			<-done
			// Even a campaign shorter than one poll interval needs a final
			// drain. Parent interruption cannot erase this metadata check, and
			// cancel(nil) must not mask a fault discovered at this boundary.
			if err := scan(context.WithoutCancel(parent)); err != nil {
				cause = errors.Join(cause, err)
			} else {
				cause = errors.Join(cause, save("final-drain", current))
			}
			cancel(cause)
		})
		return cause
	}, nil
}

func readKernelEntries(ctx context.Context, command kernelCommand, args ...string) ([]kernelEntry, error) {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := command(probe, args...)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(raw) > kernelReadLimit || errors.Is(err, kernelOverflow) {
		return nil, kernelBlocked("journal-overflow")
	}
	if err != nil || probe.Err() != nil {
		return nil, kernelBlocked("journal-unreadable")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var entries []kernelEntry
	for {
		var entry kernelEntry
		if err := d.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			return nil, kernelBlocked("journal-json-invalid")
		}
		stamp, err := strconv.ParseUint(entry.Time, 10, 64)
		if len(entry.Cursor) > 4096 || !kernelCursor.MatchString(entry.Cursor) || !kernelBootID.MatchString(entry.Boot) || entry.Message == nil || err != nil || stamp == 0 {
			return nil, kernelBlocked("journal-identity-unavailable")
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func kernelFault(message string) string {
	text := strings.ToLower(message)
	amd := strings.Contains(text, "amdgpu") || strings.Contains(text, "amdkfd") || strings.Contains(text, "kfd:")
	if strings.Contains(text, "svm") && strings.Contains(text, "map") && (strings.Contains(text, "fail") || strings.Contains(text, "error")) {
		return "amd-svm-map-failed"
	}
	if amd {
		switch {
		case strings.Contains(text, "reset"):
			return "amd-gpu-reset"
		case strings.Contains(text, "ring") && (strings.Contains(text, "timeout") || strings.Contains(text, "timed out")):
			return "amd-ring-timeout"
		case strings.Contains(text, "fault"):
			return "amd-gpu-fault"
		case kernelNonfinite.MatchString(text):
			return "amd-nonfinite"
		}
	}
	if strings.Contains(text, "hardware error") {
		return "hardware-error"
	}
	if strings.Contains(text, "llama") {
		if strings.Contains(text, "segfault") || strings.Contains(text, "general protection fault") {
			return "llama-segfault"
		}
		if strings.Contains(text, "oom") || strings.Contains(text, "out of memory") || strings.Contains(text, "killed process") {
			return "llama-oom"
		}
	}
	return ""
}

type kernelOutput struct {
	bytes.Buffer
	cancel   context.CancelFunc
	overflow bool
}

func (output *kernelOutput) Write(p []byte) (int, error) {
	if output.Len()+len(p) > kernelReadLimit {
		output.overflow = true
		output.cancel()
		return 0, kernelOverflow
	}
	return output.Buffer.Write(p)
}

func runKernelJournal(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &kernelOutput{cancel: cancel}
	command := exec.CommandContext(ctx, "journalctl", args...)
	command.Stdout = output
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	err := command.Run()
	if output.overflow {
		return nil, kernelOverflow
	}
	return output.Bytes(), err
}
