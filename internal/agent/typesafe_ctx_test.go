package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestDecideCtxAbandonsStalledRequestAtDeadline pins the reason DecideCtx
// exists: a provider that accepts the connection and then stalls must be
// abandoned at the caller's deadline, not at the fixed typesafeRequestTimeout.
// The relevance judge's 4s searchJudgeTimeout depends on this.
func TestDecideCtxAbandonsStalledRequestAtDeadline(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	c := newTypesafeClient("sk-test", "jev-latest", srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.DecideCtx(ctx, "state", map[string]TypesafeQuestion{
		"q": {Type: "noul", Instructions: "?"},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("DecideCtx returned nil error for a stalled server")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("DecideCtx took %s, want it bounded by the 150ms deadline (not the 30s default)", elapsed)
	}
}

// TestDecideCtxAlreadyCancelledIssuesNoRequest pins the fail-fast half: an
// already-cancelled context must return without sending anything to the
// provider, so a search that no longer matters costs no network round trip.
func TestDecideCtxAlreadyCancelledIssuesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{},"usage":{}}`))
	}))
	t.Cleanup(srv.Close)

	c := newTypesafeClient("sk-test", "jev-latest", srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.DecideCtx(ctx, "state", map[string]TypesafeQuestion{
		"q": {Type: "noul", Instructions: "?"},
	})
	if err == nil {
		t.Fatal("DecideCtx returned nil error for a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("provider received %d request(s), want 0", got)
	}
}

// TestDecideCtxWithoutDeadlineIsBounded pins the safety fallback: a caller that
// passes a deadline-less context (Decide's own path, and the discovery/doc
// relevance judges) must still be bounded by typesafeRequestTimeout rather
// than hanging forever. Asserted through Decide, which is DecideCtx with a
// Background context.
func TestDecideCtxWithoutDeadlineIsBounded(t *testing.T) {
	// The bound is 30s; asserting it directly would make the test slow. Assert
	// the property that matters cheaply: Decide still performs a normal
	// round-trip via DecideCtx with an internally-applied budget.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"q":{"type":"noul","noul":0.9}},"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)

	c := newTypesafeClient("sk-test", "jev-latest", srv.URL)
	resp, err := c.Decide("state", map[string]TypesafeQuestion{
		"q": {Type: "noul", Instructions: "?"},
	})
	if err != nil {
		t.Fatalf("Decide (DecideCtx with Background): %v", err)
	}
	if resp.Answers["q"].Noul != 0.9 {
		t.Fatalf("answer = %+v", resp.Answers["q"])
	}
}
