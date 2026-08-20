// Package process owns the external command boundary.
package process

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"syscall"
)

type Command struct {
	Name   string
	Args   []string
	Env    []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type Result struct {
	Stdout []byte
	Stderr []byte
	Status int
}

type Runner interface {
	Run(context.Context, Command) (Result, error)
	LookPath(string) (string, error)
}

type Executor interface {
	Exec(path string, arguments, environment []string) error
}

type OSExecutor struct{}

func (OSExecutor) Exec(path string, arguments, environment []string) error {
	return syscall.Exec(path, arguments, environment)
}

type OSRunner struct{}

func (OSRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (OSRunner) Run(ctx context.Context, command Command) (Result, error) {
	child := exec.CommandContext(ctx, command.Name, command.Args...)
	child.Dir = command.Dir
	if command.Env != nil {
		child.Env = command.Env
	}
	child.Stdin = command.Stdin
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if command.Stdout == nil {
		child.Stdout = &stdout
	} else {
		child.Stdout = command.Stdout
	}
	if command.Stderr == nil {
		child.Stderr = &stderr
	} else {
		child.Stderr = command.Stderr
	}
	err := child.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Status: 0}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.Status = exitError.ExitCode()
		return result, nil
	}
	return Result{}, err
}
