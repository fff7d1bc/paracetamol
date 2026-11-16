package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/controlerr"
)

// Derive both destinations once, before probes or workloads. In particular,
// resuming a JSON checkpoint named *.md must never replace it with its report.
func resolveAcceptancePaths(value string, policy atomicfile.Policy) (checkpoint, report string, returned error) {
	checkpoint, err := filepath.Abs(value)
	if err != nil {
		return "", "", err
	}
	extension := filepath.Ext(checkpoint)
	if !strings.EqualFold(extension, ".json") {
		return "", "", controlerr.Usage("acceptance checkpoint must have a .json extension")
	}
	report = strings.TrimSuffix(checkpoint, extension) + ".md"
	if err := atomicfile.ValidateDestination(checkpoint, policy); err != nil {
		return "", "", fmt.Errorf("acceptance checkpoint: %w", err)
	}
	if err := atomicfile.ValidateDestination(report, policy); err != nil {
		return "", "", fmt.Errorf("acceptance report: %w", err)
	}
	return checkpoint, report, nil
}
