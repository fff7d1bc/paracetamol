package gateway

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
)

// AtomicLogWriter keeps one logical log write intact when gateway controller
// and backend-follow output share stderr. Callers should share one instance;
// wrapping the same destination independently would provide separate locks.
type AtomicLogWriter struct {
	mu     sync.Mutex
	output io.Writer
}

func NewAtomicLogWriter(output io.Writer) *AtomicLogWriter {
	if existing, ok := output.(*AtomicLogWriter); ok {
		return existing
	}
	return &AtomicLogWriter{output: output}
}

func (writer *AtomicLogWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.output.Write(value)
}

func atomicLogWriter(output io.Writer) io.Writer {
	if _, ok := output.(*AtomicLogWriter); ok {
		return output
	}
	return NewAtomicLogWriter(output)
}

func logLine(output io.Writer, format string, values ...any) {
	line := fmt.Sprintf(format, values...)
	line = strings.NewReplacer("\r", " ", "\n", " ").Replace(line)
	_, _ = io.WriteString(output, line+"\n")
}

// prefixedLineWriter buffers partial backend writes until a physical line is
// complete. Prefix and content then reach the shared atomic writer in one
// write, so concurrent controller output cannot splice itself into a line.
type prefixedLineWriter struct {
	mu      sync.Mutex
	output  io.Writer
	prefix  []byte
	pending []byte
}

func newPrefixedLineWriter(output io.Writer, prefix string) *prefixedLineWriter {
	return &prefixedLineWriter{output: output, prefix: []byte(prefix)}
}

func (writer *prefixedLineWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	original := len(value)
	writer.pending = append(writer.pending, value...)
	for {
		newline := bytes.IndexByte(writer.pending, '\n')
		if newline < 0 {
			break
		}
		line := make([]byte, 0, len(writer.prefix)+newline+1)
		line = append(line, writer.prefix...)
		line = append(line, writer.pending[:newline+1]...)
		if _, err := writer.output.Write(line); err != nil {
			return 0, err
		}
		writer.pending = append(writer.pending[:0], writer.pending[newline+1:]...)
	}
	return original, nil
}

func (writer *prefixedLineWriter) finish() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.pending) == 0 {
		return
	}
	line := make([]byte, 0, len(writer.prefix)+len(writer.pending)+1)
	line = append(line, writer.prefix...)
	line = append(line, writer.pending...)
	line = append(line, '\n')
	_, _ = writer.output.Write(line)
	writer.pending = nil
}
