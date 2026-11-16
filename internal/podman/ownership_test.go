package podman

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/process"
)

type ownershipRunner struct {
	commands []process.Command
	run      func(context.Context, process.Command) (process.Result, error)
}

func (runner *ownershipRunner) Run(ctx context.Context, command process.Command) (process.Result, error) {
	runner.commands = append(runner.commands, command)
	return runner.run(ctx, command)
}
func (*ownershipRunner) LookPath(string) (string, error) { return "podman", nil }

func ownedLabels() map[string]string {
	return map[string]string{ManagedContainerLabel: "true", ManagedApplicationLabel: "comfyui", ManagedRoleLabel: "application", ManagedLaunchLabel: strings.Repeat("b", 32)}
}

func ownershipJSON(t *testing.T, id, name string, labels map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal([]any{map[string]any{"Id": id, "Name": name, "Config": map[string]any{"Labels": labels}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInspectOwnedContainerRejectsUnreviewedIdentity(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"invalid JSON", []byte("not-json")},
		{"empty", []byte("[]")},
		{"ambiguous", []byte("[{},{}]")},
		{"missing labels", ownershipJSON(t, id, "owned", nil)},
		{"wrong name", ownershipJSON(t, id, "other", ownedLabels())},
		{"short ID", ownershipJSON(t, "a123", "owned", ownedLabels())},
		{"uppercase ID", ownershipJSON(t, strings.Repeat("A", 64), "owned", ownedLabels())},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &ownershipRunner{run: func(_ context.Context, command process.Command) (process.Result, error) {
				if command.Args[1] == "exists" {
					return process.Result{}, nil
				}
				return process.Result{Stdout: test.data}, nil
			}}
			if target, err := (Client{Runner: runner}).InspectOwnedContainer(context.Background(), "owned", "comfyui", "application"); err == nil || target != nil {
				t.Fatalf("target=%v err=%v", target, err)
			}
		})
	}
	for _, change := range []map[string]string{
		{ManagedContainerLabel: "false"}, {ManagedApplicationLabel: "foreign"}, {ManagedRoleLabel: "unknown"}, {ManagedRoleLabel: "gateway-backend"},
	} {
		labels := ownedLabels()
		maps.Copy(labels, change)
		runner := &ownershipRunner{run: func(_ context.Context, command process.Command) (process.Result, error) {
			if command.Args[1] == "exists" {
				return process.Result{}, nil
			}
			return process.Result{Stdout: ownershipJSON(t, id, "owned", labels)}, nil
		}}
		if _, err := (Client{Runner: runner}).InspectOwnedContainer(context.Background(), "owned", "comfyui", "application"); err == nil {
			t.Fatalf("accepted labels %v", labels)
		}
	}
}

func TestRemoveOwnedContainerUsesCapturedIDAndRechecksOwnership(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, changed := range []bool{false, true} {
		labels := ownedLabels()
		runner := &ownershipRunner{run: func(_ context.Context, command process.Command) (process.Result, error) {
			if command.Args[0] == "rm" {
				if got := command.Args[len(command.Args)-1]; got != id {
					t.Fatalf("removal used reusable reference %q", got)
				}
				return process.Result{}, nil
			}
			if command.Args[1] == "exists" {
				return process.Result{}, nil
			}
			current := maps.Clone(labels)
			if changed {
				current[ManagedLaunchLabel] = strings.Repeat("c", 32)
			}
			return process.Result{Stdout: ownershipJSON(t, id, "renamed", current)}, nil
		}}
		err := (Client{Runner: runner}).RemoveOwnedContainer(context.Background(), OwnedContainer{ID: id, Name: "owned", Labels: labels}, 2, Streams{})
		if (err != nil) != changed {
			t.Fatalf("changed=%t err=%v", changed, err)
		}
		if changed && len(runner.commands) != 2 {
			t.Fatalf("ownership mismatch reached removal: %v", runner.commands)
		}
	}
}

func TestRemoveOwnedContainerHandlesAbsenceAndRemovalErrors(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, mode := range []string{"already absent", "removed despite error", "still present", "probe error"} {
		t.Run(mode, func(t *testing.T) {
			removed := false
			runner := &ownershipRunner{run: func(_ context.Context, command process.Command) (process.Result, error) {
				if command.Args[0] == "rm" {
					removed = true
					return process.Result{Status: 1}, nil
				}
				if command.Args[1] == "exists" {
					if removed && mode == "probe error" {
						return process.Result{}, errors.New("probe failed")
					}
					if mode == "already absent" || (removed && mode == "removed despite error") {
						return process.Result{Status: 1}, nil
					}
					return process.Result{}, nil
				}
				return process.Result{Stdout: ownershipJSON(t, id, "owned", ownedLabels())}, nil
			}}
			err := (Client{Runner: runner}).RemoveOwnedContainer(context.Background(), OwnedContainer{ID: id, Name: "owned", Labels: ownedLabels()}, 0, Streams{})
			wantError := mode == "still present" || mode == "probe error"
			if (err != nil) != wantError {
				t.Fatalf("err=%v wantError=%t", err, wantError)
			}
		})
	}
}

func TestRemoveOwnedContainerRejectsNameMasqueradingAsCapturedID(t *testing.T) {
	id := strings.Repeat("a", 64)
	runner := &ownershipRunner{run: func(_ context.Context, command process.Command) (process.Result, error) {
		if command.Args[0] == "rm" {
			t.Fatal("removed a different object with the captured ID as its name")
		}
		if command.Args[1] == "exists" {
			return process.Result{}, nil
		}
		return process.Result{Stdout: ownershipJSON(t, strings.Repeat("b", 64), id, ownedLabels())}, nil
	}}
	if err := (Client{Runner: runner}).RemoveOwnedContainer(context.Background(), OwnedContainer{ID: id, Name: "owned", Labels: ownedLabels()}, 0, Streams{}); err == nil {
		t.Fatal("accepted name masquerading as immutable ID")
	}
}

func TestFailedLaunchCleanupPreservesReplacementAfterMatchingLabelProbe(t *testing.T) {
	token := strings.Repeat("b", 32)
	labels := ownedLabels()
	labels[ManagedLaunchLabel] = strings.Repeat("c", 32)
	runner := &ownershipRunner{run: func(_ context.Context, command process.Command) (process.Result, error) {
		if command.Args[0] == "rm" {
			t.Fatal("removed a replacement that appeared after the initial launch-label probe")
		}
		if command.Args[0] == "inspect" {
			// The first object was ours, but it disappears before the full
			// identity capture. Its replacement has identical generic labels.
			return process.Result{Stdout: []byte(`{"` + ManagedLaunchLabel + `":"` + token + `"}`)}, nil
		}
		if command.Args[1] == "exists" {
			return process.Result{}, nil
		}
		return process.Result{Stdout: ownershipJSON(t, strings.Repeat("d", 64), "owned", labels)}, nil
	}}
	if err := (Client{Runner: runner}).RemoveLaunchContainer(context.Background(), "owned", "comfyui", token, 1); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchIdentityIsUniqueOnceOnlyAndPreservesOpaqueWorkload(t *testing.T) {
	base := []string{"podman", "run", "--rm", "image", "--prompt", "--label=" + ManagedLaunchLabel + "=bad", "--label", ManagedLaunchLabel + "=also-workload"}
	first, token, err := WithLaunchIdentity(base)
	if err != nil || !validLaunchID(token) {
		t.Fatalf("token=%q err=%v", token, err)
	}
	if !reflect.DeepEqual(first[4:], base[2:]) {
		t.Fatalf("changed workload arguments: %v", first)
	}
	if err := ValidateLaunchIdentity(first, token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WithLaunchIdentity(first); err == nil {
		t.Fatal("allowed a second launch identity to be prepended")
	}
	_, different, err := WithLaunchIdentity(base)
	if err != nil || different == token {
		t.Fatalf("nonce reuse: %q %v", different, err)
	}
	for _, invalid := range [][]string{
		nil,
		{"podman", "exec", "image"},
		{"podman", "run", "--label", ManagedLaunchLabel + "=bad", "image"},
		{"podman", "run", "--label", ManagedLaunchLabel, "image"},
	} {
		if _, _, err := WithLaunchIdentity(invalid); err == nil {
			t.Fatalf("accepted invalid command %v", invalid)
		}
	}
	for _, invalid := range []struct {
		command []string
		token   string
	}{{first, "bad"}, {first, strings.Repeat("f", 32)}, {base, token}, {nil, token}} {
		if err := ValidateLaunchIdentity(invalid.command, invalid.token); err == nil {
			t.Fatalf("accepted mismatched launch identity %v %q", invalid.command, invalid.token)
		}
	}
}
