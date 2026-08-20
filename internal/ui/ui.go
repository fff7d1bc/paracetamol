// Package ui owns semantic terminal presentation. Callers express intent and
// never embed ANSI escapes; redirected output remains stable plain text.
package ui

import (
	"io"
	"os"
)

type Terminal struct {
	Writer io.Writer
	color  bool
}

func New(writer io.Writer, environment map[string]string) Terminal {
	color := IsTerminal(writer) && environment["NO_COLOR"] == "" && environment["TERM"] != "dumb"
	return Terminal{Writer: writer, color: color}
}

func IsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	status, err := file.Stat()
	return err == nil && status.Mode()&os.ModeCharDevice != 0
}

func (terminal Terminal) Heading(value string) string { return terminal.style("1;36", value) }
func (terminal Terminal) Success(value string) string { return terminal.style("32", value) }
func (terminal Terminal) Warning(value string) string { return terminal.style("1;33", value) }
func (terminal Terminal) Muted(value string) string   { return terminal.style("2", value) }

func (terminal Terminal) style(code, value string) string {
	if !terminal.color {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
