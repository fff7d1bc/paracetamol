package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"paracetamol/internal/content"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/podman"
	"paracetamol/internal/process"
	"paracetamol/internal/storage"
)

// LaunchOptions describes a trusted command emitted by this package. Prepare
// runs after validation, under the same reservation as container publication.
type LaunchOptions struct {
	DataRoot, Application, Role, Image, LaunchID string
	Command                                      []string
	Prepare                                      func() error
	Streams                                      podman.Streams
}

// Launch publishes a stopped, ownership-verified container before releasing the
// data mutation reservation. Cleanup therefore sees either the reservation or
// the container, never an unregistered workload using persistent mounts. The
// reservation is not held during inference or an interactive shell.
func Launch(ctx context.Context, runner process.Runner, options LaunchOptions) (result process.Result, returned error) {
	result, _, returned = LaunchOwned(ctx, runner, options)
	return result, returned
}

// LaunchOwned also returns the verified immutable identity for detached workload
// liveness and cleanup. Callers must not rediscover it by its reusable name.
func LaunchOwned(ctx context.Context, runner process.Runner, options LaunchOptions) (result process.Result, target *podman.OwnedContainer, returned error) {
	create, detached, interactive, err := launchCreateCommand(options.Command, options.Image, options.LaunchID)
	if err != nil {
		return result, nil, err
	}
	client := podman.Client{Runner: runner}
	err = content.WithMutation(ctx, options.DataRoot, func() (returned error) {
		if err := (storage.Layout{Root: options.DataRoot}).PrepareRuntime(options.Application); err != nil {
			return err
		}
		if options.Prepare != nil {
			if err := options.Prepare(); err != nil {
				return err
			}
		}
		defer func() {
			if returned == nil {
				return
			}
			// Creation can succeed even if its client is interrupted or returns
			// malformed output. Discover only to find this nonce, then retain
			// the ordinary exact-ID ownership check before removal.
			cleanupContext := context.WithoutCancel(ctx)
			names, err := client.ManagedContainerNames(cleanupContext, options.Application)
			if err != nil {
				returned = errors.Join(returned, err)
				return
			}
			for _, name := range names {
				returned = errors.Join(returned, client.RemoveLaunchContainer(cleanupContext, name, options.Application, options.Role, options.LaunchID, 1))
			}
		}()
		created, err := runner.Run(ctx, process.Command{Name: "podman", Args: create, Stderr: options.Streams.Stderr})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if created.Status != 0 {
			return launchStatus(created)
		}
		id := strings.TrimSpace(string(created.Stdout))
		// Inspect by the returned immutable ID, never by a reusable name. A
		// malformed response cannot authorize starting any container.
		if len(id) != 64 || strings.ContainsAny(id, " \t\r\n") {
			return fmt.Errorf("Podman create returned an invalid container ID")
		}
		target, err = client.InspectOwnedContainer(ctx, id, options.Application, options.Role)
		if err != nil {
			return err
		}
		if target == nil || target.ID != id || target.Labels[podman.ManagedLaunchLabel] != options.LaunchID {
			return fmt.Errorf("created container does not match its managed launch identity")
		}
		return nil
	})
	if err != nil {
		return result, nil, err
	}
	arguments := []string{"start"}
	if !detached {
		arguments = append(arguments, "--attach")
	}
	if interactive {
		arguments = append(arguments, "--interactive")
	}
	arguments = append(arguments, target.ID)
	result, err = runner.Run(ctx, process.Command{Name: "podman", Args: arguments, Stdin: options.Streams.Stdin, Stdout: options.Streams.Stdout, Stderr: options.Streams.Stderr})
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && result.Status != 0 {
		err = launchStatus(result)
	}
	if err != nil {
		cleanupErr := client.RemoveOwnedContainer(context.WithoutCancel(ctx), *target, 1, podman.Streams{})
		return result, nil, errors.Join(err, cleanupErr)
	}
	return result, target, nil
}

func launchStatus(result process.Result) error {
	message := fmt.Sprintf("command failed with exit status %d", result.Status)
	if detail := strings.TrimSpace(string(result.Stderr)); detail != "" {
		message += ": " + detail
	}
	return &controlerr.Error{Message: message, Status: result.Status}
}

// Only parse the launcher-owned option grammar. The image terminates it; model
// prompts and other opaque application arguments must never become run flags.
func launchCreateCommand(command []string, image, launchID string) ([]string, bool, bool, error) {
	if err := podman.ValidateLaunchIdentity(command, launchID); err != nil {
		return nil, false, false, err
	}
	create := []string{"create"}
	detached, interactive := false, false
	for index := 2; index < len(command); index++ {
		argument := command[index]
		if !strings.HasPrefix(argument, "-") {
			if argument != image {
				return nil, false, false, fmt.Errorf("managed launch image does not match its command")
			}
			return append(create, command[index:]...), detached, interactive, nil
		}
		switch argument {
		case "--detach":
			detached = true
		case "--interactive", "-it":
			interactive = true
			create = append(create, argument)
		case "--rm", "--read-only", "--tty":
			create = append(create, argument)
		case "--label", "--userns", "--umask", "--name", "--network", "--publish", "--cap-drop", "--security-opt", "--pids-limit", "--ulimit", "--shm-size", "--tmpfs", "--volume", "--env", "--device", "--entrypoint":
			if index+1 >= len(command) {
				return nil, false, false, fmt.Errorf("missing managed launch option value")
			}
			create = append(create, argument, command[index+1])
			index++
		default:
			return nil, false, false, fmt.Errorf("unsupported managed launch option %s", argument)
		}
	}
	return nil, false, false, fmt.Errorf("managed launch has no image")
}
