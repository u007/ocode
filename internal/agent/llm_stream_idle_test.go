package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// shrinkIdleTimeout overrides llmStreamIdleTimeout for the duration of a test.
func shrinkIdleTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := llmStreamIdleTimeout
	llmStreamIdleTimeout = d
	t.Cleanup(func() { llmStreamIdleTimeout = orig })
}

// TestIdleAbortTransportStallsMidStream pins the core fix for
// "net/http: request canceled (Client.Timeout or context cancellation while
// reading body)": a response whose body goes silent must abort with the typed,
// RETRYABLE streamStalledError instead of hanging forever or dying with an
// unretryable blanket-client-timeout kill.
func TestIdleAbortTransportStallsMidStream(t *testing.T) {
	shrinkIdleTimeout(t, 80*time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Headers sent; body goes silent until aborted (or safety timeout).
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer srv.Close()

	client := &http.Client{Transport: &idleAbortTransport{next: http.DefaultTransport}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	start := time.Now()
	_, err = io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected stalled-body error, got nil")
	}
	if !errors.Is(err, errStreamStalled) {
		t.Fatalf("expected errStreamStalled, got %v", err)
	}
	var netErr interface{ Timeout() bool }
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected Timeout()-typed error for legacy classification, got %v (%T)", err, err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stall abort took %v, watchdog did not fire promptly", elapsed)
	}
}

// TestIdleAbortTransportActiveStreamNotAborted verifies that a slow-but-alive
// stream (bytes keep trickling in slower than the idle window would allow a
// silent connection to survive) is never killed — the whole point of replacing
// the blanket Client.Timeout with per-read idleness.
func TestIdleAbortTransportActiveStreamNotAborted(t *testing.T) {
	shrinkIdleTimeout(t, 120*time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		for i := 0; i < 12; i++ {
			if _, err := w.Write([]byte("x")); err != nil {
				return
			}
			f.Flush()
			time.Sleep(25 * time.Millisecond) // 300ms total >> any single gap is fine; gaps < idle
		}
	}))
	defer srv.Close()

	client := &http.Client{Transport: &idleAbortTransport{next: http.DefaultTransport}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("active stream should never be aborted, got %v", err)
	}
	if string(body) != strings.Repeat("x", 12) {
		t.Fatalf("unexpected body %q", body)
	}
}

// TestStreamStalledErrorClassification checks both classification paths:
// the explicit sentinel match and the legacy net.Error Timeout() probe used by
// isRetryableLLMClientError.
func TestStreamStalledErrorClassification(t *testing.T) {
	wrapped := &streamStalledError{after: time.Second, cause: "context canceled"}
	if !errors.Is(wrapped, errStreamStalled) {
		t.Fatal("streamStalledError must match errStreamStalled")
	}
	if !isRetryableLLMClientError(wrapped) {
		t.Fatal("streamStalledError must be classified retryable")
	}
	var netErr netErrorProbe
	if !errors.As(error(wrapped), &netErr) || !netErr.Timeout() {
		t.Fatal("streamStalledError must satisfy net.Error-style Timeout()")
	}
}

type netErrorProbe interface {
	error
	Timeout() bool
	Temporary() bool
}

type idleTestRoundTripper func(*http.Request) (*http.Response, error)

func (f idleTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type contextBlockingBody struct {
	ctx context.Context
}

func (b contextBlockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (contextBlockingBody) Close() error { return nil }

// TestIdleAbortTransportReleasesLocalSlot exercises the production body
// layering used for managed local models: an idle abort closes the inner slot
// body, releases its lock, and allows the next request to acquire the slot.
func TestIdleAbortTransportReleasesLocalSlot(t *testing.T) {
	shrinkIdleTimeout(t, 60*time.Millisecond)
	dir := t.TempDir()
	slotPath := dir + "/local_test-model.slot0.lock"
	if err := os.WriteFile(slotPath, nil, 0644); err != nil {
		t.Fatalf("create lock: %v", err)
	}

	var released atomic.Int32
	transport := &idleAbortTransport{next: idleTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		inner := newSlotReleasingBody(contextBlockingBody{ctx: req.Context()}, func() {
			released.Add(1)
			_ = os.Remove(slotPath)
		}, slotPath)
		return &http.Response{StatusCode: http.StatusOK, Body: inner, Request: req}, nil
	})}

	req := httptest.NewRequest(http.MethodGet, "http://local.test/", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, errStreamStalled) {
		t.Fatalf("expected stalled stream, got %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	if got := released.Load(); got != 1 {
		t.Fatalf("expected one slot release, got %d", got)
	}
	if _, err := os.Stat(slotPath); !os.IsNotExist(err) {
		t.Fatalf("slot lock still exists after idle abort: err=%v", err)
	}

	release, _, err := acquireLocalModelSlot(context.Background(), "local/test-model", 1, dir)
	if err != nil {
		t.Fatalf("next request could not acquire released slot: %v", err)
	}
	release()
}

// TestChatContextCancellationDoesNotRetry pins the distinction between a
// caller cancellation and the idle watchdog's private cancellation. User
// cancellation must pass through unchanged and must not trigger a retry.
func TestChatContextCancellationDoesNotRetry(t *testing.T) {
	shrinkIdleTimeout(t, time.Second)
	var calls atomic.Int32
	requestSeen := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		requestSeen <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: srv.URL}
	done := make(chan error, 1)
	go func() {
		_, err := client.ChatWithContext(ctx, []Message{{Role: "user", Content: "hi"}}, nil)
		done <- err
	}()

	<-requestSeen
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected cancellation to avoid retry, got %d attempts", got)
	}
}

// TestChatRetriesStalledStreamThenSucceeds is the end-to-end proof of
// "when timeout, pls retry": attempt 1 sends headers then goes silent (the
// watchdog aborts it with streamStalledError), attempt 2 returns a valid SSE
// stream, and Chat retries automatically instead of failing the turn.
func TestChatRetriesStalledStreamThenSucceeds(t *testing.T) {
	shrinkIdleTimeout(t, 60*time.Millisecond)
	origDelay := llmRetryBaseDelay
	llmRetryBaseDelay = 0
	t.Cleanup(func() { llmRetryBaseDelay = origDelay })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			// Attempt 1: headers arrive, then the body goes silent forever.
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			return
		}
		// Attempt 2: healthy SSE stream.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(openAIChatOKStream))
	}))
	defer srv.Close()

	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: srv.URL}
	msg, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected stalled first attempt to be retried to success, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", got)
	}
	if msg == nil || msg.Content != "ok" {
		t.Fatalf("expected final message content %q, got %+v", "ok", msg)
	}
}

// TestChatFailsAfterDeltasOnMidStreamStall pins the documented invariant: once
// partial deltas have been rendered, a mid-stream failure must NOT be retried
// (a fresh generation would duplicate already-rendered text), even though the
// stall itself is timeout-classified. The turn fails fast with the typed
// error instead of silently duplicating output.
func TestChatFailsAfterDeltasOnMidStreamStall(t *testing.T) {
	shrinkIdleTimeout(t, 60*time.Millisecond)
	origDelay := llmRetryBaseDelay
	llmRetryBaseDelay = 0
	t.Cleanup(func() { llmRetryBaseDelay = origDelay })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"partial rendered output here\"}}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer srv.Close()

	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: srv.URL}
	// Mirror production: the TUI/web attach an OnDelta sink, which arms the
	// retry loop's anti-duplication gate. Without a sink the loop cannot know
	// deltas were rendered and would retry.
	client.OnDelta = func(kind, text string) {}
	_, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected mid-stream stall after deltas to fail the turn")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected no retry after emitted deltas, got %d attempts", got)
	}
	if !strings.Contains(err.Error(), "llm stream stalled") ||
		!strings.Contains(err.Error(), "llm request failed after 1 attempt(s)") {
		t.Fatalf("unexpected error format: %v", err)
	}
}
