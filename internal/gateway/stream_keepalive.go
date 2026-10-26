package gateway

import (
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	streamKeepaliveInterval = 15 * time.Second
	// Tiny native pings can still fall below a client's 1 byte/second low-speed
	// threshold. This fixed, content-free comment exceeds that rate comfortably.
	streamKeepaliveComment = ": paracetamol gateway keepalive\n\n"
	streamReadBufferBytes  = 32 * 1024
)

type streamReadResult struct {
	count int
	err   error
}

// streamKeepaliveBody leaves ReverseProxy as the only response writer. One
// demand-driven read owns a fixed buffer; comments never split upstream frames.
type streamKeepaliveBody struct {
	upstream io.ReadCloser
	ticks    <-chan time.Time
	stopTick func()
	reads    chan struct{}
	results  chan streamReadResult
	closed   chan struct{}
	done     chan struct{}
	close    sync.Once
	closeErr error
	buffer   []byte
	pending  []byte
	err      error
	reading  bool
	framing  streamBoundary
}

func keepStreamingResponseAlive(response *http.Response) {
	if response.StatusCode < 200 || response.StatusCode >= 300 || !isEventStream(response.Header.Get("Content-Type")) || response.Header.Get("Content-Encoding") != "" {
		return
	}
	ticker := time.NewTicker(streamKeepaliveInterval)
	response.Body = newStreamKeepaliveBody(response.Body, ticker.C, ticker.Stop)
	// A finite upstream Content-Length cannot describe inserted comments.
	response.Header.Del("Content-Length")
	response.ContentLength = -1
}

func newStreamKeepaliveBody(upstream io.ReadCloser, ticks <-chan time.Time, stopTick func()) *streamKeepaliveBody {
	body := &streamKeepaliveBody{
		upstream: upstream, ticks: ticks, stopTick: stopTick,
		reads: make(chan struct{}, 1), results: make(chan streamReadResult), closed: make(chan struct{}), done: make(chan struct{}),
		buffer: make([]byte, streamReadBufferBytes), framing: streamBoundary{boundary: true},
	}
	go body.readUpstream()
	return body
}

func (body *streamKeepaliveBody) readUpstream() {
	defer close(body.done)
	for {
		select {
		case <-body.closed:
			return
		case <-body.reads:
		}
		count, err := body.upstream.Read(body.buffer)
		select {
		case <-body.closed:
			return
		case body.results <- streamReadResult{count: count, err: err}:
		}
	}
}

func (body *streamKeepaliveBody) Read(value []byte) (int, error) {
	if len(value) == 0 {
		return 0, nil
	}
	for {
		if len(body.pending) > 0 {
			count := copy(value, body.pending)
			body.framing.observe(body.pending[:count])
			body.pending = body.pending[count:]
			return count, nil
		}
		if body.err != nil {
			return 0, body.err
		}
		if !body.reading {
			select {
			case <-body.closed:
				return 0, io.ErrClosedPipe
			case body.reads <- struct{}{}:
				body.reading = true
			}
		}
		select {
		case <-body.closed:
			return 0, io.ErrClosedPipe
		case result := <-body.results:
			body.reading = false
			body.pending, body.err = body.buffer[:result.count], result.err
		case <-body.ticks:
			// A trailing CR may be half of CRLF: conservatively wait for LF.
			// This also skips keepalives on streams using only CR delimiters.
			if body.framing.boundary && !body.framing.afterCR {
				// Keep comments out of the observer wrapped around upstream bytes.
				body.pending = []byte(streamKeepaliveComment)
			}
		}
	}
}

func (body *streamKeepaliveBody) Close() error {
	body.close.Do(func() {
		close(body.closed)
		body.stopTick()
		// HTTP response bodies support Close concurrent with Read and unblock it.
		body.closeErr = body.upstream.Close()
		<-body.done
	})
	return body.closeErr
}

type streamBoundary struct {
	boundary bool
	lineData bool
	afterCR  bool
}

func (framing *streamBoundary) observe(value []byte) {
	for _, char := range value {
		if framing.afterCR && char == '\n' {
			framing.afterCR = false
			continue
		}
		framing.afterCR = false
		if char == '\r' || char == '\n' {
			framing.boundary = !framing.lineData
			framing.lineData = false
			framing.afterCR = char == '\r'
		} else {
			framing.boundary, framing.lineData = false, true
		}
	}
}
