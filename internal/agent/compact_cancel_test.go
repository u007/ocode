package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestDrainBufferedSummary pins the drain runSummary uses when its wait select
// observes ctx.Done().
//
// The drain exists because runSummary parks on the result channel and on
// ctx.Done() in ONE select, and Go picks uniformly at random when both are
// ready — so a summary landing in the same instant the deadline fires is
// discarded roughly half the time. That simultaneity is NOT reproducible from
// outside runSummary: the channel is created inside it, and cancelling strictly
// before a summary lands is a case where failing is correct, while cancelling
// strictly after is the case the normal `done` branch already handles. So this
// tests the drain directly, by handing it a channel that is already filled and a
// context that is already cancelled — the exact state the racing select would
// have seen.
//
// The residual gap is the CALL SITE: this test cannot prove runSummary invokes
// the drain on its ctx.Done() branch, because the only way to reach that branch
// with a pre-filled channel is the race itself.
func TestDrainBufferedSummary(t *testing.T) {
	valid := validSummaryText("kept")

	cancelled := func(t *testing.T) context.Context {
		t.Helper()
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(ErrCompactionTimeout)
		return ctx
	}

	t.Run("rescues an already-delivered valid summary", func(t *testing.T) {
		done := make(chan summaryResult, 1)
		done <- summaryResult{content: valid}

		got, err := drainBufferedSummary(done, cancelled(t))
		if err != nil {
			t.Fatalf("err = %v, want the buffered summary to win over the deadline", err)
		}
		if got != valid {
			t.Fatalf("summary = %q, want %q", got, valid)
		}
		if len(done) != 0 {
			t.Fatalf("drain left %d unread results, want the buffered one consumed", len(done))
		}
	})

	// These three all report the deadline: only a usable summary is rescued.
	for _, tc := range []struct {
		name  string
		fill  bool
		value summaryResult
	}{
		{"nothing delivered", false, summaryResult{}},
		{"template-invalid summary", true, summaryResult{content: "just some prose"}},
		{"transport error", true, summaryResult{err: errors.New("connection reset")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan summaryResult, 1)
			if tc.fill {
				done <- tc.value
			}

			got, err := drainBufferedSummary(done, cancelled(t))
			if err == nil {
				t.Fatalf("expected the compaction deadline to be reported, got summary %q", got)
			}
			if !errors.Is(err, ErrCompactionTimeout) {
				t.Fatalf("err = %v, want it to wrap ErrCompactionTimeout", err)
			}
			if got != "" {
				t.Fatalf("summary = %q, want empty", got)
			}
		})
	}

	t.Run("returns the buffered summary when the context is still live", func(t *testing.T) {
		done := make(chan summaryResult, 1)
		done <- summaryResult{content: valid}

		got, err := drainBufferedSummary(done, context.Background())
		if err != nil {
			t.Fatalf("err = %v, want nil while the context is live", err)
		}
		if got != valid {
			t.Fatalf("summary = %q, want %q", got, valid)
		}
	})

	t.Run("a non-timeout cancel is labelled cancelled, not timed out", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errors.New("provider hung up"))

		_, err := drainBufferedSummary(make(chan summaryResult, 1), ctx)
		if err == nil {
			t.Fatal("expected an error for a cancelled context")
		}
		if !strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "timed out") {
			t.Fatalf("err = %v, want a cancellation label rather than a timeout", err)
		}
	})
}

// blockingStubClient parks in Chat until the test finishes, so the only ready
// case in runSummary's wait select is the already-cancelled context.
//
// A client that returns immediately would race the drain: when it delivers a
// valid summary before runSummary reaches its select, the drain legitimately
// rescues it and the test would pass WITHOUT ever exercising the label it is
// meant to check. (In practice that window is narrow — a ready ctx.Done() case
// means select never parks — but the test must not depend on winning a race to
// be meaningful.) Closing release from t.Cleanup lets the worker goroutine
// return; `done` is buffered, so it never blocks and does not leak.
type blockingStubClient struct{ release chan struct{} }

func (c *blockingStubClient) Chat(_ []Message, _ []map[string]interface{}) (*Message, error) {
	<-c.release
	return &Message{Role: "assistant", Content: validSummaryText("unreachable")}, nil
}

func (c *blockingStubClient) GetProvider() string { return "mock" }
func (c *blockingStubClient) GetModel() string    { return "mock-compact" }

// TestUsableSummary pins the accept/reject contract the drain relies on.
func TestUsableSummary(t *testing.T) {
	valid := validSummaryText("kept")

	got, ok := usableSummary(summaryResult{content: valid})
	if !ok {
		t.Fatal("a template-valid summary must be usable")
	}
	if got != valid {
		t.Fatalf("usableSummary returned %q, want the input %q", got, valid)
	}

	tests := []struct {
		name string
		res  summaryResult
	}{
		{"transport error", summaryResult{err: errors.New("connection reset")}},
		{"error alongside content", summaryResult{content: valid, err: errors.New("truncated stream")}},
		{"empty content", summaryResult{content: ""}},
		{"blank content", summaryResult{content: "   \n\t "}},
		{"template sections missing", summaryResult{content: "just some prose"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := usableSummary(tc.res); ok {
				t.Fatalf("usableSummary(%+v) = (%q, true), want not usable", tc.res, got)
			}
		})
	}
}

// TestRunSummaryLabelsNonTimeoutCancelAsCancelled guards the diagnostic half of
// the fix at the runSummary level. A cancellation that is not ErrCompactionTimeout
// must not be reported as a timeout: the two causes have different remedies
// (raise the deadline vs find whatever cancelled the pass). Reporting every
// cancellation as "timed out" is what produced the self-contradicting
// "compact: summary timed out: context canceled" that sent a real session's
// failure down the wrong path.
func TestRunSummaryLabelsNonTimeoutCancelAsCancelled(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("provider hung up"))

	_, err := runSummary(ctx, &blockingStubClient{release: release}, "summarise this", 0, nil)
	if err == nil {
		t.Fatal("expected an error for an already-cancelled context")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("error = %v, want it to report a cancellation rather than a timeout", err)
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, must not label a non-timeout cancel as a timeout", err)
	}
}

// TestRunSummaryLabelsCompactionDeadlineAsTimeout is the positive counterpart:
// compaction's own deadline must still read as a timeout, so the two classes
// stay distinguishable in the transcript.
func TestRunSummaryLabelsCompactionDeadlineAsTimeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrCompactionTimeout)

	_, err := runSummary(ctx, &blockingStubClient{release: release}, "summarise this", 0, nil)
	if err == nil {
		t.Fatal("expected an error for an already-cancelled context")
	}
	if !strings.HasPrefix(err.Error(), "compact: summary timed out:") {
		t.Fatalf("error = %v, want compaction's own deadline under the canonical timeout label", err)
	}
	if !errors.Is(err, ErrCompactionTimeout) {
		t.Fatalf("error = %v, want it to wrap ErrCompactionTimeout", err)
	}
}

// malformedSignallingClient returns a template-invalid summary and closes
// signal, which lets the test cancel the context only AFTER attempt 0 has
// already been served from the result channel.
type malformedSignallingClient struct{ signal chan struct{} }

func (c *malformedSignallingClient) Chat(_ []Message, _ []map[string]interface{}) (*Message, error) {
	close(c.signal)
	return &Message{Role: "assistant", Content: "no sections here"}, nil
}

func (c *malformedSignallingClient) GetProvider() string { return "mock" }
func (c *malformedSignallingClient) GetModel() string    { return "mock-compact" }

// TestRunSummaryLabelsRetryBackoffCancellation pins the retry-backoff wait,
// which used to be the one place still labelling a cause independently of
// summaryContextErr ("compact: context cancelled during retry", regardless of
// whether the cause was the sentinel).
//
// The cancel is deliberately deferred until after attempt 0 is served from the
// result channel. Cancelling during the first call would make the context
// already-done at attempt 0, where the cancellation drain produces the label —
// masking this site entirely. Instead the malformed summary is served with a
// live context (so the `done` branch is the only ready case), and the cancel
// then lands in attempt 1's backoff wait, whose 500ms timer the cancel beats by
// orders of magnitude.
func TestRunSummaryLabelsRetryBackoffCancellation(t *testing.T) {
	signal := make(chan struct{})
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	go func() {
		<-signal
		cancel(ErrCompactionTimeout)
	}()

	client := &malformedSignallingClient{signal: signal}
	// maxRetries=1 => two attempts: the first records a malformed summary and
	// continues, the second finds the context done while waiting to back off.
	_, err := runSummary(ctx, client, "summarise this", 1, nil)
	if err == nil {
		t.Fatal("expected an error once the context ended during retry backoff")
	}
	if !strings.HasPrefix(err.Error(), "compact: summary timed out:") {
		t.Fatalf("error = %v, want the canonical timeout label from the retry-backoff site (not a substring match: the cause's own text is %q)", err, ErrCompactionTimeout)
	}
	if !errors.Is(err, ErrCompactionTimeout) {
		t.Fatalf("error = %v, want it to wrap ErrCompactionTimeout", err)
	}
}
