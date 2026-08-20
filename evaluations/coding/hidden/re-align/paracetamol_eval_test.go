package main

import (
	"strings"
	"testing"
)

func TestParacetamolEvalProbeResultColumnStaysAligned(t *testing.T) {
	opts := defaultProbeOptions()
	resultColumn := strings.Index(formatProbeAttemptHeader(), "result")
	tests := []struct {
		name          string
		predictedSize int64
		wantSize      string
	}{
		{name: "ordinary MiB", predictedSize: 2403 * 1024 * 1024 / 10, wantSize: "240.3 MiB"},
		{name: "four digit MiB", predictedSize: 10002 * 1024 * 1024 / 10, wantSize: "1000.2 MiB"},
		{name: "upper MiB boundary", predictedSize: 10239 * 1024 * 1024 / 10, wantSize: "1023.9 MiB"},
		{name: "GiB rollover", predictedSize: 1024 * 1024 * 1024, wantSize: "1.0 GiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := formatProbeAttemptLine(ProbeAttempt{
				CRF:              24.25,
				Score:            96.56,
				WorstSampleScore: 94.01,
				EncodedPercent:   16,
				PredictedSize:    tt.predictedSize,
			}, 95, opts)
			if !strings.Contains(line, tt.wantSize) {
				t.Fatalf("missing %q in %q", tt.wantSize, line)
			}
			if got := strings.Index(line, "pass"); got != resultColumn {
				t.Fatalf("result column = %d, want %d in %q", got, resultColumn, line)
			}
		})
	}
}
