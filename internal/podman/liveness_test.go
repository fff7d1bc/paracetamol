package podman

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"paracetamol/internal/process"
)

func TestOwnedContainerLivenessUsesOneBoundedIdentitySnapshot(t *testing.T) {
	id := strings.Repeat("a", 64)
	target := OwnedContainer{ID: id, Name: "reusable", Labels: ownedLabels()}
	for _, mode := range []string{"running", "renamed", "exited", "missing", "wrong ID", "wrong nonce", "wrong application", "wrong role", "missing state", "malformed", "inspection failure"} {
		t.Run(mode, func(t *testing.T) {
			labels := maps.Clone(target.Labels)
			name, returnedID := target.Name, target.ID
			switch mode {
			case "renamed":
				name = "renamed-original"
			case "wrong ID":
				returnedID = strings.Repeat("c", 64)
			case "wrong nonce":
				labels[ManagedLaunchLabel] = strings.Repeat("d", 32)
			case "wrong application":
				labels[ManagedApplicationLabel] = "llama-cpp"
			case "wrong role":
				labels[ManagedRoleLabel] = "benchmark"
			}
			record := map[string]any{"Id": returnedID, "Name": name, "Config": map[string]any{"Labels": labels}}
			if mode != "missing state" {
				record["State"] = map[string]any{"Running": mode != "exited", "ExitCode": 17}
			}
			data, _ := json.Marshal([]any{record})
			runner := &ownershipRunner{run: func(ctx context.Context, command process.Command) (process.Result, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 5*time.Second || command.Args[len(command.Args)-1] != id {
					t.Fatalf("unbounded or redirected metadata probe: %v deadline=%v", command.Args, deadline)
				}
				if command.Args[1] == "exists" {
					if mode == "missing" {
						return process.Result{Status: 1}, nil
					}
					return process.Result{}, nil
				}
				if mode == "missing" || mode == "inspection failure" {
					return process.Result{}, errors.New("fixture inspect failed")
				}
				if mode == "malformed" {
					data = []byte(`[]`)
				}
				return process.Result{Stdout: data}, nil
			}}
			err := (Client{Runner: runner}).RequireOwnedContainerRunning(context.Background(), target)
			if (err == nil) != (mode == "running" || mode == "renamed") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if mode != "missing" && mode != "inspection failure" && len(runner.commands) != 1 {
				t.Fatalf("expected one inspect snapshot: %v", runner.commands)
			}
			if mode == "exited" && !strings.Contains(err.Error(), "status 17") {
				t.Fatalf("lost exit status: %v", err)
			}
		})
	}
}

func TestOwnedContainerLivenessRefusesUncapturedIdentity(t *testing.T) {
	for _, target := range []OwnedContainer{{ID: "reusable", Labels: ownedLabels()}, {ID: strings.Repeat("a", 64)}, {ID: strings.Repeat("a", 64), Labels: map[string]string{ManagedContainerLabel: "true", ManagedApplicationLabel: "comfyui", ManagedRoleLabel: "benchmark"}}} {
		runner := &ownershipRunner{run: func(context.Context, process.Command) (process.Result, error) {
			t.Fatal("invalid liveness request reached Podman")
			return process.Result{}, nil
		}}
		if err := (Client{Runner: runner}).RequireOwnedContainerRunning(context.Background(), target); err == nil {
			t.Fatal("accepted invalid captured identity")
		}
	}
}
