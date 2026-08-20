// Package ui owns semantic terminal presentation. Callers express intent and
// never embed ANSI escapes; redirected output remains stable plain text.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
)

type Role string

const (
	RoleHeading Role = "heading"
	RoleCommand Role = "command"
	RoleSuccess Role = "success"
	RoleWarning Role = "warning"
	RoleError   Role = "error"
	RoleInfo    Role = "info"
	RoleMuted   Role = "muted"
	RoleLabel   Role = "label"
	RolePrompt  Role = "prompt"
)

var roleCodes = map[Role]string{
	RoleHeading: "1;36", RoleCommand: "1;36", RoleSuccess: "1;32",
	RoleWarning: "1;33", RoleError: "1;31", RoleInfo: "1;34",
	RoleMuted: "2", RoleLabel: "1", RolePrompt: "1;36",
}

var successStates = stringSet("available", "built", "complete", "identical", "installed", "pass", "read/write", "ready", "running", "verified", "writable", "yes")
var warningStates = stringSet("download", "link-missing", "link-mismatch", "missing", "modified", "not created; parent writable", "partial", "repair link", "terms", "unverified")
var errorStates = stringSet("blocked", "broken", "conflict", "error", "failed", "hash-mismatch", "insufficient access", "link mismatch", "no access", "not a directory", "size-mismatch", "unexpected", "unavailable", "user-file")
var mutedStates = stringSet("absent", "not built", "not present", "skipped", "unloaded")

type Terminal struct {
	Writer io.Writer
	color  bool
	tty    bool
}

func New(writer io.Writer, environment map[string]string) Terminal {
	_, noColor := environment["NO_COLOR"]
	tty := IsTerminal(writer)
	return Terminal{Writer: writer, tty: tty, color: tty && !noColor && environment["TERM"] != "dumb"}
}

func IsTerminal(writer io.Writer) bool {
	if marker, ok := writer.(interface{ IsTerminal() bool }); ok {
		return marker.IsTerminal()
	}
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	status, err := file.Stat()
	return err == nil && status.Mode()&os.ModeCharDevice != 0
}

func (terminal Terminal) IsTerminal() bool            { return terminal.tty }
func (terminal Terminal) Heading(value string) string { return terminal.Style(RoleHeading, value) }
func (terminal Terminal) Command(value string) string { return terminal.Style(RoleCommand, value) }
func (terminal Terminal) Success(value string) string { return terminal.Style(RoleSuccess, value) }
func (terminal Terminal) Warning(value string) string { return terminal.Style(RoleWarning, value) }
func (terminal Terminal) Error(value string) string   { return terminal.Style(RoleError, value) }
func (terminal Terminal) Info(value string) string    { return terminal.Style(RoleInfo, value) }
func (terminal Terminal) Muted(value string) string   { return terminal.Style(RoleMuted, value) }
func (terminal Terminal) Label(value string) string   { return terminal.Style(RoleLabel, value) }
func (terminal Terminal) Prompt(value string) string  { return terminal.Style(RolePrompt, value) }

func (terminal Terminal) Style(role Role, value string) string {
	code, ok := roleCodes[role]
	if !ok || !terminal.color {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func (terminal Terminal) State(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	role := RoleLabel
	switch {
	case successStates[normalized]:
		role = RoleSuccess
	case errorStates[normalized]:
		role = RoleError
	case mutedStates[normalized]:
		role = RoleMuted
	case warningStates[normalized], strings.Contains(normalized, "required"), strings.Contains(normalized, "terms"), strings.Contains(normalized, "unverified"):
		role = RoleWarning
	}
	return terminal.Style(role, value)
}

func (terminal Terminal) PromptText(message string, leadingBlank bool) string {
	if leadingBlank {
		return "\n" + terminal.Prompt(message)
	}
	return terminal.Prompt(message)
}

func (terminal Terminal) Next(actions ...string) {
	if len(actions) == 0 {
		return
	}
	fmt.Fprintf(terminal.Writer, "\n%s\n", terminal.Heading("Next:"))
	for _, action := range actions {
		fmt.Fprintf(terminal.Writer, "    %s\n", terminal.Command(action))
	}
}

// RewriteLine updates one terminal line while preserving line-delimited logs
// when output is redirected. It returns the visible width of message.
func (terminal Terminal) RewriteLine(message string, previousWidth int, complete bool) int {
	width := DisplayWidth(message)
	if !terminal.tty {
		fmt.Fprintln(terminal.Writer, message)
		return width
	}
	padding := strings.Repeat(" ", max(0, previousWidth-width))
	ending := ""
	if complete {
		ending = "\n"
	}
	fmt.Fprintf(terminal.Writer, "\r%s%s%s", message, padding, ending)
	return width
}

func (terminal Terminal) FinishRewrite() {
	if terminal.tty {
		fmt.Fprintln(terminal.Writer)
	}
}

// Progress reports exact byte progress. Terminals receive at most one update
// per second on a rewritten line; redirected logs receive 5% or one-minute
// heartbeats so a long hash never looks stalled.
type Progress struct {
	terminal    Terminal
	expected    int64
	lastBytes   int64
	lastPercent int
	lastReport  time.Time
	lineWidth   int
	active      bool
	now         func() time.Time
}

// DownloadProgress reports an approximate staged byte count while an external
// downloader owns the file. Bounded downloads show a hard limit instead of
// implying that the source archive has an exact known size.
type DownloadProgress struct {
	terminal    Terminal
	expected    int64
	bounded     bool
	lastBytes   int64
	lastPercent int
	lastReport  time.Time
	lineWidth   int
	active      bool
	now         func() time.Time
}

func NewProgress(writer io.Writer, environment map[string]string, expected int64) *Progress {
	return &Progress{terminal: New(writer, environment), expected: expected, lastBytes: -1, lastPercent: -5, now: time.Now}
}

func NewDownloadProgress(writer io.Writer, environment map[string]string, expected int64, bounded bool) *DownloadProgress {
	return &DownloadProgress{terminal: New(writer, environment), expected: expected, bounded: bounded, lastBytes: -1, lastPercent: -5, now: time.Now}
}

func (progress *DownloadProgress) Update(current int64, force bool) {
	if current < 0 {
		current = 0
	}
	if current > progress.expected {
		current = progress.expected
	}
	percent := 100.0
	if progress.expected > 0 {
		percent = float64(current) * 100 / float64(progress.expected)
	}
	now := progress.now()
	due := progress.lastBytes < 0 || force
	if progress.terminal.tty {
		due = due || now.Sub(progress.lastReport) >= time.Second
	} else {
		due = due || int(percent) >= progress.lastPercent+5 || now.Sub(progress.lastReport) >= time.Minute
	}
	if force && !progress.terminal.tty && current == progress.lastBytes {
		return
	}
	if !due {
		return
	}
	var message string
	if progress.bounded {
		message = fmt.Sprintf("  %s %s %s; %s %s", progress.terminal.Info("Progress:"), progress.terminal.Label("staged"), HumanBytes(current), progress.terminal.Label("limit"), HumanBytes(progress.expected))
	} else {
		message = fmt.Sprintf("  %s %s  %s %s; %s %s", progress.terminal.Info("Progress:"), progress.terminal.Info(fmt.Sprintf("%.1f%%", percent)), progress.terminal.Label("staged"), HumanBytes(current), progress.terminal.Label("expected"), HumanBytes(progress.expected))
	}
	progress.lineWidth = progress.terminal.RewriteLine(message, progress.lineWidth, force)
	progress.lastBytes, progress.lastPercent, progress.lastReport = current, int(percent), now
	progress.active = progress.terminal.tty && !force
}

func (progress *DownloadProgress) Finish() {
	if progress.active {
		progress.terminal.FinishRewrite()
		progress.active = false
	}
}

func (progress *Progress) Update(item string, index, total int, current int64, force bool) {
	if current < 0 {
		current = 0
	}
	if progress.expected >= 0 && current > progress.expected {
		current = progress.expected
	}
	percent := 100.0
	if progress.expected > 0 {
		percent = float64(current) * 100 / float64(progress.expected)
	}
	now := progress.now()
	due := progress.lastBytes < 0 || force
	if progress.terminal.tty {
		due = due || now.Sub(progress.lastReport) >= time.Second
	} else {
		due = due || int(percent) >= progress.lastPercent+5 || now.Sub(progress.lastReport) >= time.Minute
	}
	if force && current == progress.lastBytes {
		if progress.active {
			progress.terminal.FinishRewrite()
			progress.active = false
		}
		return
	}
	if !due {
		return
	}
	position := ""
	if total > 1 {
		position = fmt.Sprintf("[%d/%d] ", index, total)
	}
	message := fmt.Sprintf("  %s %s%s  %s  %s %s of %s",
		progress.terminal.Info("Verifying:"), position, item,
		progress.terminal.Info(fmt.Sprintf("%.1f%%", percent)), progress.terminal.Label("hashed"),
		HumanBytes(current), HumanBytes(progress.expected))
	progress.lineWidth = progress.terminal.RewriteLine(message, progress.lineWidth, force)
	progress.lastBytes, progress.lastPercent, progress.lastReport = current, int(percent), now
	progress.active = progress.terminal.tty && !force
}

func (progress *Progress) Finish() {
	if progress.active {
		progress.terminal.FinishRewrite()
		progress.active = false
	}
}

func HumanBytes(value int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	number, unit := float64(value), 0
	for number >= 1024 && unit < len(units)-1 {
		number /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", number, units[unit])
}

func DisplayWidth(value string) int {
	plain := stripANSI(value)
	width := 0
	for _, character := range plain {
		if unicode.Is(unicode.Mn, character) || unicode.Is(unicode.Me, character) || unicode.Is(unicode.Cf, character) {
			continue
		}
		width++
	}
	return width
}

func stripANSI(value string) string {
	var output strings.Builder
	escape := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !escape && character == 0x1b && index+1 < len(value) && value[index+1] == '[' {
			escape = true
			index++
			continue
		}
		if escape {
			if character >= '@' && character <= '~' {
				escape = false
			}
			continue
		}
		output.WriteByte(character)
	}
	return output.String()
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
