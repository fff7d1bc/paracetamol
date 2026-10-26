package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type streamTestRead struct {
	data string
	err  error
}

func readStreamAsync(body io.Reader, size int) <-chan streamTestRead {
	result := make(chan streamTestRead, 1)
	go func() {
		buffer := make([]byte, size)
		count, err := body.Read(buffer)
		result <- streamTestRead{string(buffer[:count]), err}
	}()
	return result
}

func receiveStreamRead(t *testing.T, result <-chan streamTestRead, expected string) {
	t.Helper()
	select {
	case got := <-result:
		if got.data != expected || got.err != nil {
			t.Fatalf("read=%q, %v; want %q", got.data, got.err, expected)
		}
	case <-time.After(time.Second):
		t.Fatal("stream read did not finish")
	}
}

func tickStream(t *testing.T, ticks chan<- time.Time) {
	t.Helper()
	select {
	case ticks <- time.Now():
	case <-time.After(time.Second):
		t.Fatal("stream did not consume keepalive tick")
	}
}

func TestStreamKeepaliveWaitsForEventBoundary(t *testing.T) {
	for _, fragments := range [][]string{
		{"data: {\"choices\":", "[]}\n", "\n"},
		{"data: one\r", "\n", "\r", "\n"},
		{"data: one\n", "data: two\n", "\n"},
		{": native ping\n", "\n"},
	} {
		t.Run(strings.Join(fragments, "|"), func(t *testing.T) {
			reader, writer := io.Pipe()
			defer writer.Close()
			ticks := make(chan time.Time)
			body := newStreamKeepaliveBody(reader, ticks, func() {})
			defer body.Close()
			result := readStreamAsync(body, streamReadBufferBytes)
			for index, fragment := range fragments {
				if _, err := io.WriteString(writer, fragment); err != nil {
					t.Fatal(err)
				}
				receiveStreamRead(t, result, fragment)
				result = readStreamAsync(body, streamReadBufferBytes)
				tickStream(t, ticks)
				if index < len(fragments)-1 {
					select {
					case got := <-result:
						t.Fatalf("comment split an incomplete event: %+v", got)
					default:
					}
				} else {
					receiveStreamRead(t, result, streamKeepaliveComment)
				}
			}
		})
	}
}

func TestStreamKeepalivePreservesBytewiseReadsAndObservation(t *testing.T) {
	const original = "data: {\"choices\":[{\"delta\":{\"content\":\"private answer\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\r\n\r\ndata: [DONE]\n\n"
	observer := newResponseObserver("text/event-stream")
	body := newStreamKeepaliveBody(&observedBody{ReadCloser: io.NopCloser(strings.NewReader(original)), observer: observer}, nil, func() {})
	defer body.Close()
	var output strings.Builder
	for {
		var one [1]byte
		count, err := body.Read(one[:])
		output.Write(one[:count])
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
	}
	if output.String() != original {
		t.Fatal("bytewise reads changed upstream bytes")
	}
	got := observer.Finish()
	if got.FirstOutputAt.IsZero() || got.Tokens.Input == nil || *got.Tokens.Input != 10 || got.Tokens.Output == nil || *got.Tokens.Output != 2 || got.Completion.StreamDone == nil || !*got.Completion.StreamDone {
		t.Fatalf("observation=%+v", got)
	}
}

func TestStreamKeepaliveDoesNotObserveCommentsAndStopsBlockedRead(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	observer := newResponseObserver("text/event-stream")
	ticks := make(chan time.Time)
	stopped := false
	body := newStreamKeepaliveBody(&observedBody{ReadCloser: reader, observer: observer}, ticks, func() { stopped = true })
	defer body.Close()
	for range 4 {
		result := readStreamAsync(body, streamReadBufferBytes)
		tickStream(t, ticks)
		receiveStreamRead(t, result, streamKeepaliveComment)
	}
	result := readStreamAsync(body, streamReadBufferBytes)
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil || !stopped {
		t.Fatalf("close=%v, timer stopped=%t", err, stopped)
	}
	select {
	case got := <-result:
		if got.err == nil {
			t.Fatal("closed stream read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("close did not release blocked read")
	}
	select {
	case <-body.done:
	default:
		t.Fatal("reader goroutine outlived Close")
	}
	got := observer.Finish()
	if !got.FirstOutputAt.IsZero() || got.Tokens.Input != nil || got.Tokens.Output != nil {
		t.Fatalf("keepalives invented observations: %+v", got)
	}
}

func TestKeepStreamingResponseAliveOnlyWrapsSuccessfulPlainSSE(t *testing.T) {
	for _, test := range []struct {
		status   int
		kind     string
		encoding string
		wrapped  bool
	}{
		{200, "text/event-stream", "", true},
		{201, "text/event-stream; charset=utf-8", "", true},
		{400, "text/event-stream", "", false},
		{503, "application/json", "", false},
		{200, "application/json", "", false},
		{200, "text/event-stream", "gzip", false},
	} {
		response := &http.Response{StatusCode: test.status, Header: make(http.Header), ContentLength: 4, Body: io.NopCloser(strings.NewReader("body"))}
		response.Header.Set("Content-Type", test.kind)
		response.Header.Set("Content-Encoding", test.encoding)
		response.Header.Set("Content-Length", "4")
		keepStreamingResponseAlive(response)
		_, wrapped := response.Body.(*streamKeepaliveBody)
		if wrapped != test.wrapped || response.StatusCode != test.status || response.Header.Get("Content-Type") != test.kind || (response.ContentLength == -1) != test.wrapped || (response.Header.Get("Content-Length") == "") != test.wrapped {
			t.Fatalf("test=%+v response=%+v", test, response)
		}
		contents, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(contents) != "body" {
			t.Fatalf("body=%q err=%v", contents, err)
		}
	}
	if len(streamKeepaliveComment) < 32 || float64(len(streamKeepaliveComment))/streamKeepaliveInterval.Seconds() <= 1 {
		t.Fatal("keepalives do not exceed the client's one-byte/second threshold")
	}
}

func TestStreamKeepaliveHTTPDisconnectCancelsUpstream(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.(http.Flusher).Flush()
		close(started)
		<-request.Context().Done()
		close(canceled)
	}))
	defer upstream.Close()
	server, _, _ := testGatewayServer(t, upstream)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"fixture","stream":true}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	response.Body.Close()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("downstream cancellation did not reach silent upstream")
	}
}

func TestStreamKeepalivePreservesReadError(t *testing.T) {
	want := errors.New("fixture upstream failed")
	reader := &streamErrorReader{err: want}
	body := newStreamKeepaliveBody(reader, nil, func() {})
	defer body.Close()
	got, err := io.ReadAll(body)
	if !bytes.Equal(got, []byte("partial")) || !errors.Is(err, want) {
		t.Fatalf("bytes=%q err=%v", got, err)
	}
}

type streamErrorReader struct {
	err error
}

func (reader *streamErrorReader) Read(value []byte) (int, error) {
	return copy(value, "partial"), reader.err
}
func (*streamErrorReader) Close() error { return nil }
