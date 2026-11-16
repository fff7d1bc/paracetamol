package cli

import (
	"fmt"
	"math"

	"paracetamol/internal/controlerr"
)

// The counter is checkpointed before starting a workload. Never reuse it even
// when a child result was published without the parent's completion checkpoint.
func checkpointAttemptID(suiteID string, attempt int) (string, error) {
	if !managedRunID.MatchString(suiteID) || attempt < 1 || attempt >= math.MaxInt {
		return "", controlerr.New("checkpoint has invalid or exhausted attempt identity")
	}
	return fmt.Sprintf("%s-attempt-%d", suiteID, attempt), nil
}

func nextCheckpointAttemptID(suiteID string, previous int) (string, error) {
	if previous < 0 || previous == math.MaxInt {
		return "", controlerr.New("checkpoint has invalid or exhausted attempt counter")
	}
	return checkpointAttemptID(suiteID, previous+1)
}
