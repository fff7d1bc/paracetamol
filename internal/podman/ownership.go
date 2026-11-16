package podman

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"paracetamol/internal/controlerr"
	"paracetamol/internal/identity"
)

var ManagedLaunchLabel = identity.LabelNamespace + ".launch"

// OwnedContainer is a captured immutable identity, not permission to remove a
// reusable name. Keep its labels so a later mutation can revalidate the plan.
type OwnedContainer struct {
	ID     string
	Name   string
	Labels map[string]string
}

type ownedContainerRecord struct {
	ID     string `json:"Id"`
	Name   string
	Config struct{ Labels map[string]string }
	State  struct {
		Running  *bool
		ExitCode int
	}
}

// InspectOwnedContainer captures ID and ownership together. Absence is benign;
// malformed, foreign and ambiguous responses fail closed.
func (client Client) InspectOwnedContainer(ctx context.Context, reference, application, role string) (*OwnedContainer, error) {
	record, err := client.inspectOwnedContainerRecord(ctx, reference, application, role)
	if err != nil || record == nil {
		return nil, err
	}
	return &OwnedContainer{ID: record.ID, Name: strings.TrimPrefix(record.Name, "/"), Labels: record.Config.Labels}, nil
}

func (client Client) inspectOwnedContainerRecord(ctx context.Context, reference, application, role string) (*ownedContainerRecord, error) {
	present, err := client.Exists(ctx, "container", reference)
	if err != nil || !present {
		return nil, err
	}
	raw, err := client.Capture(ctx, []string{"container", "inspect", reference}, "cannot inspect container ownership for "+reference)
	if err != nil {
		// --rm and other owners can legitimately remove it between the probes.
		present, probeErr := client.Exists(ctx, "container", reference)
		if probeErr == nil && !present {
			return nil, nil
		}
		return nil, err
	}
	return decodeOwnedContainerRecord(raw, reference, application, role)
}

func decodeOwnedContainerRecord(raw, reference, application, role string) (*ownedContainerRecord, error) {
	var records []ownedContainerRecord
	if err := json.Unmarshal([]byte(raw), &records); err != nil || len(records) != 1 {
		return nil, controlerr.New("cannot decode unambiguous container ownership for %s", reference)
	}
	record := records[0]
	name := strings.TrimPrefix(record.Name, "/")
	if !validContainerID(record.ID) || name == "" || (reference != record.ID && reference != name) {
		return nil, controlerr.New("container identity does not match %s", reference)
	}
	labels := record.Config.Labels
	if !reviewedOwnership(labels) || (application != "" && labels[ManagedApplicationLabel] != application) || (role != "" && labels[ManagedRoleLabel] != role) {
		return nil, controlerr.New("refusing container %s: managed ownership labels do not match", reference)
	}
	return &record, nil
}

func reviewedOwnership(labels map[string]string) bool {
	if labels[ManagedContainerLabel] != "true" {
		return false
	}
	switch labels[ManagedApplicationLabel] {
	case "comfyui", "llama-cpp":
		switch labels[ManagedRoleLabel] {
		case "application", "benchmark", "shell":
			return true
		case "gateway-backend":
			return labels[ManagedApplicationLabel] == "llama-cpp"
		}
	case "":
		return labels[ManagedRoleLabel] == "download" || labels[ManagedRoleLabel] == "diagnostic"
	}
	return false
}

func validContainerID(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}

// RequireOwnedContainerRunning bounds only the small metadata probe, never
// model loading or inference. A reused name cannot redirect this inspection.
func (client Client) RequireOwnedContainerRunning(ctx context.Context, target OwnedContainer) error {
	if !validContainerID(target.ID) || !reviewedOwnership(target.Labels) || !validLaunchID(target.Labels[ManagedLaunchLabel]) {
		return fmt.Errorf("invalid owned container liveness request")
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := client.Capture(probe, []string{"container", "inspect", target.ID}, "cannot inspect backend container")
	if err != nil {
		present, probeErr := client.Exists(probe, "container", target.ID)
		if probeErr == nil && !present {
			return fmt.Errorf("%s backend container is missing or exited", target.Labels[ManagedApplicationLabel])
		}
		return err
	}
	current, err := decodeOwnedContainerRecord(raw, target.ID, target.Labels[ManagedApplicationLabel], target.Labels[ManagedRoleLabel])
	if err != nil {
		return err
	}
	if current.ID != target.ID || !maps.Equal(current.Config.Labels, target.Labels) {
		return fmt.Errorf("backend container ownership changed after launch")
	}
	if current.State.Running == nil {
		return fmt.Errorf("cannot decode backend container running state")
	}
	if !*current.State.Running {
		return fmt.Errorf("%s backend container exited (status %d)", target.Labels[ManagedApplicationLabel], current.State.ExitCode)
	}
	return nil
}

// RemoveOwnedContainer revalidates the captured object and removes only its
// immutable ID, so reuse of the original name cannot redirect the operation.
func (client Client) RemoveOwnedContainer(ctx context.Context, target OwnedContainer, graceSeconds int, streams Streams) error {
	if !validContainerID(target.ID) || !reviewedOwnership(target.Labels) {
		return fmt.Errorf("invalid owned container removal request")
	}
	current, err := client.InspectOwnedContainer(ctx, target.ID, target.Labels[ManagedApplicationLabel], target.Labels[ManagedRoleLabel])
	if err != nil || current == nil {
		return err
	}
	if current.ID != target.ID {
		return controlerr.New("container %s no longer resolves to its captured ID", target.Name)
	}
	if !maps.Equal(current.Labels, target.Labels) {
		return controlerr.New("container %s ownership changed after planning", target.Name)
	}
	if err := client.RemoveContainer(ctx, target.ID, graceSeconds, streams); err != nil {
		present, probeErr := client.Exists(ctx, "container", target.ID)
		if probeErr == nil && !present {
			return nil
		}
		return err
	}
	return nil
}

// WithLaunchIdentity adds a nonce once to a trusted runtime command, before its
// Podman options. Never scan the opaque workload arguments for option-like text.
// Callers retain the returned token through dry-run printing and execution.
func WithLaunchIdentity(command []string) ([]string, string, error) {
	if len(command) < 2 || command[0] != "podman" || command[1] != "run" {
		return nil, "", fmt.Errorf("invalid managed launch command")
	}
	prefix := ManagedLaunchLabel + "="
	if len(command) >= 4 && command[2] == "--label" && (command[3] == ManagedLaunchLabel || strings.HasPrefix(command[3], prefix)) {
		return nil, "", fmt.Errorf("managed launch identity already supplied")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, "", fmt.Errorf("generate managed launch identity: %w", err)
	}
	token := hex.EncodeToString(nonce[:])
	result := append([]string{}, command[:2]...)
	result = append(result, "--label", prefix+token)
	return append(result, command[2:]...), token, nil
}

// ValidateLaunchIdentity checks only the structured prefix emitted above, not
// arbitrary application arguments after the image boundary.
func ValidateLaunchIdentity(command []string, token string) error {
	if !validLaunchID(token) || len(command) < 4 || command[0] != "podman" || command[1] != "run" || command[2] != "--label" || command[3] != ManagedLaunchLabel+"="+token {
		return fmt.Errorf("managed launch command does not match its identity")
	}
	return nil
}

func validLaunchID(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16 && strings.ToLower(value) == value
}

// RemoveLaunchContainer never treats generic labels or a canonical name as
// evidence that a failed command created the current occupant.
func (client Client) RemoveLaunchContainer(ctx context.Context, name, application, role, launchID string, graceSeconds int) error {
	if !validLaunchID(launchID) || !reviewedOwnership(map[string]string{ManagedContainerLabel: "true", ManagedApplicationLabel: application, ManagedRoleLabel: role}) {
		return fmt.Errorf("invalid managed launch identity or ownership")
	}
	// Inspect ownership only after checking the launch label. A foreign winner
	// is not ours to remove and is not an application cleanup failure.
	present, err := client.Exists(ctx, "container", name)
	if err != nil || !present {
		return err
	}
	labels, err := client.ContainerLabels(ctx, name)
	if err != nil {
		present, probeErr := client.Exists(ctx, "container", name)
		if probeErr == nil && !present {
			return nil
		}
		return err
	}
	if labels[ManagedLaunchLabel] != launchID {
		return nil
	}
	target, err := client.InspectOwnedContainer(ctx, name, application, role)
	if err != nil || target == nil {
		return err
	}
	if target.Labels[ManagedLaunchLabel] != launchID {
		return nil
	}
	return client.RemoveOwnedContainer(ctx, *target, graceSeconds, Streams{})
}
