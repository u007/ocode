package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// llmStreamIdleTimeout is how long a streaming LLM response body may receive
// no bytes at all before the connection is declared stalled and aborted with
// errStreamStalled. It replaces the blanket http.Client.Timeout that used to
// cap TOTAL request duration (including the streamed body) at
// llmRequestTimeout: any generation streaming longer than that cap died
// mid-flight with "net/http: request canceled (Client.Timeout or context
// cancellation while reading body)", and because deltas had already been
// rendered the retry loop refused to re-run the turn — an unrecoverable
// failure for long reasoning-model outputs. A healthy stream that keeps
// producing bytes is now never killed; only genuine silence trips the
// watchdog. Var (not const) so tests can shrink it.
var llmStreamIdleTimeout = 3 * time.Minute

// errStreamStalled is the sentinel carried by streamStalledError. It is
// classified as retryable by isRetryableLLMClientError.
var errStreamStalled = errors.New("llm stream stalled")

// llmHTTPBaseTransport bounds only the pre-stream phases of an LLM request:
// connection setup, TLS handshake, and waiting for response headers. It
// deliberately leaves the streamed response body unbounded in total duration —
// stalls are handled per-read by idleAbortTransport below.
var llmHTTPBaseTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = llmRequestTimeout
	if t.TLSHandshakeTimeout == 0 {
		t.TLSHandshakeTimeout = 30 * time.Second
	}
	return t
}()

// idleAbortTransport wraps every response body from next with an idle
// watchdog (see idleAbortBody). When the watchdog fires it cancels a private
// child of the request context, which unblocks any Read parked on the
// connection; the wrapper then surfaces a typed streamStalledError instead of
// the raw cancellation error. Cancelling the child context never touches the
// caller's context, so user-initiated aborts still propagate unchanged.
//
// Layering note: llmHTTPClient stacks this OUTSIDE localConcurrencyTransport,
// giving caller → idleAbortBody → slotReleasingBody → real connection. Close
// propagates inward, so the concurrency-slot release still fires exactly when
// the body is closed regardless of which layer closes first.
type idleAbortTransport struct {
	next http.RoundTripper
}

func (t *idleAbortTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	req = req.Clone(ctx)
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp == nil || resp.Body == nil {
		cancel()
		return resp, err
	}
	body := &idleAbortBody{rc: resp.Body, idle: llmStreamIdleTimeout, cancel: cancel}
	body.timer = time.AfterFunc(body.idle, body.abort)
	resp.Body = body
	return resp, nil
}

// idleAbortBody fails with streamStalledError if no bytes arrive within its
// idle window. Any successful Read pushes the window out, so a stream that is
// merely slow (but alive) runs indefinitely.
type idleAbortBody struct {
	rc     io.ReadCloser
	idle   time.Duration
	cancel context.CancelFunc
	timer  *time.Timer
	fired  atomic.Bool
}

// abort is the watchdog callback: mark fired, then cancel the request-scoped
// child context so the blocked Read returns immediately.
func (b *idleAbortBody) abort() {
	if b.fired.CompareAndSwap(false, true) {
		b.cancel()
	}
}

func (b *idleAbortBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 || err == nil {
		b.timer.Reset(b.idle)
	}
	if err != nil && b.fired.Load() {
		// Deliberately NOT wrapping the raw cause (a self-inflicted context
		// cancellation): unwrapping would expose context.Canceled and could
		// confuse user-abort handling upstream. The cause text is preserved
		// informationally.
		return n, &streamStalledError{after: b.idle, cause: err.Error()}
	}
	return n, err
}

func (b *idleAbortBody) Close() error {
	b.timer.Stop()
	err := b.rc.Close()
	// Release the child context resources even on the happy path; cancel is
	// idempotent.
	b.cancel()
	return err
}

// streamStalledError reports a stalled streaming response body. It implements
// net.Error-style Timeout()/Temporary() so legacy classification paths treat
// it as a transient network timeout, and matches errStreamStalled via
// errors.Is for the explicit check.
type streamStalledError struct {
	after time.Duration
	cause string
}

func (e *streamStalledError) Error() string {
	return fmt.Sprintf("%s: no data received for %s (%s)", errStreamStalled.Error(), e.after, e.cause)
}

func (e *streamStalledError) Is(target error) bool { return target == errStreamStalled }

func (e *streamStalledError) Timeout() bool { return true }

func (e *streamStalledError) Temporary() bool { return true }
