// Package controlerr defines errors with intentional process exit semantics.
package controlerr

import "fmt"

// Error is a controlled user-facing failure. Status is returned by the CLI
// without exposing a Go stack or wrapping implementation detail.
type Error struct {
	Message string
	Status  int
}

func (e *Error) Error() string { return e.Message }

// New creates an operational failure with exit status one.
func New(format string, arguments ...any) error {
	return &Error{Message: fmt.Sprintf(format, arguments...), Status: 1}
}

// Usage creates an invalid-command failure with exit status two.
func Usage(format string, arguments ...any) error {
	return &Error{Message: fmt.Sprintf(format, arguments...), Status: 2}
}
