package loopguard

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
)

// upstreamResponse is a response from the downstream handler, read as it is
// written.
type upstreamResponse struct {
	status int
	header http.Header
	body   *bufio.Reader

	pr       *io.PipeReader
	cancel   context.CancelFunc
	done     chan struct{}
	panicked any
}

// startUpstream runs next with a copy of r carrying body, in its own
// cancellable child of r's context, and returns once response headers are
// available.
func startUpstream(r *http.Request, next http.Handler, body []byte) *upstreamResponse {
	ctx, cancel := context.WithCancel(r.Context())
	req := r.Clone(ctx)
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Del("Transfer-Encoding")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	// The guard parses the stream: let the transport negotiate and decode.
	req.Header.Del("Accept-Encoding")

	pr, pw := io.Pipe()
	rw := &pipeResponseWriter{header: http.Header{}, pw: pw, ready: make(chan struct{})}
	u := &upstreamResponse{
		body:   bufio.NewReaderSize(pr, 64*1024),
		pr:     pr,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() {
		defer close(u.done)
		defer pw.Close()
		defer rw.WriteHeader(http.StatusOK) // a handler that wrote nothing
		defer func() {
			// A reverse proxy panics with http.ErrAbortHandler when a copy is
			// cut short; that is a normal disconnect here. Anything else is
			// re-raised on the request goroutine by close.
			if rec := recover(); rec != nil && rec != http.ErrAbortHandler {
				u.panicked = rec
			}
		}()
		next.ServeHTTP(rw, req)
	}()
	<-rw.ready
	u.status = rw.status
	u.header = rw.snapshot
	return u
}

// close cancels the upstream request and waits for its handler to return.
func (u *upstreamResponse) close() {
	u.cancel()
	u.pr.CloseWithError(context.Canceled)
	<-u.done
	if u.panicked != nil {
		panic(u.panicked)
	}
}

// pipeResponseWriter is the ResponseWriter handed to the downstream handler.
type pipeResponseWriter struct {
	header   http.Header
	snapshot http.Header
	status   int
	pw       *io.PipeWriter
	once     sync.Once
	ready    chan struct{}
}

func (w *pipeResponseWriter) Header() http.Header { return w.header }

func (w *pipeResponseWriter) WriteHeader(code int) {
	w.once.Do(func() {
		w.status = code
		w.snapshot = w.header.Clone()
		close(w.ready)
	})
}

func (w *pipeResponseWriter) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.pw.Write(b)
}

// Flush satisfies http.Flusher; the pipe delivers every write immediately.
func (w *pipeResponseWriter) Flush() {}

// sseEvent is one SSE event block (lines up to a blank line).
type sseEvent struct {
	block []byte // the event's lines, without the trailing blank line
	data  []byte // the joined data: payload, nil when the event has none
}

// readEvent returns the next SSE event, or io.EOF.
func readEvent(r *bufio.Reader) (sseEvent, error) {
	var ev sseEvent
	for {
		line, err := r.ReadBytes('\n')
		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 {
			if len(ev.block) > 0 {
				return ev, nil
			}
			if err != nil {
				return ev, err
			}
			continue
		}
		if len(ev.block) > 0 {
			ev.block = append(ev.block, '\n')
		}
		ev.block = append(ev.block, trimmed...)
		if payload, ok := bytes.CutPrefix(trimmed, []byte("data:")); ok {
			payload = bytes.TrimPrefix(payload, []byte(" "))
			if ev.data != nil {
				ev.data = append(ev.data, '\n')
			}
			ev.data = append(append([]byte{}, ev.data...), payload...)
		}
		if err != nil {
			// A final event without its blank line.
			return ev, nil
		}
	}
}
