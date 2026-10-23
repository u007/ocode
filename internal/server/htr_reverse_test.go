package server

import (
	"errors"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

func TestLocalHTRPortDisabledExposesNothing(t *testing.T) {
	if port, ok := localHTRPort(config.BrowserConfig{HTREnabled: false, HTRPort: 3845}); ok || port != 0 {
		t.Fatalf("disabled HTR must not be exposed, got port=%d ok=%v", port, ok)
	}
}

// fakeNow lets tests move a controlled clock forward without sleeping.
type fakeNow struct {
	now time.Time
}

func (f *fakeNow) Now() time.Time          { return f.now }
func (f *fakeNow) Advance(d time.Duration) { f.now = f.now.Add(d) }

func TestHTRHostBackoffThrottlesRepeatedFailures(t *testing.T) {
	clk := &fakeNow{now: time.Now()}
	hh := &htrHost{now: clk.Now}

	// First attempt for port 3845 is allowed (no backoff yet).
	if !hh.shouldRetry(3845) {
		t.Fatal("first attempt should be allowed")
	}

	// A failure schedules the next attempt after the base interval.
	hh.record(3845, errors.New("remote port forwarding failed"))
	if hh.shouldRetry(3845) {
		t.Fatal("should not retry inside the base backoff")
	}
	clk.Advance(htrAutoRetryInterval - time.Second)
	if hh.shouldRetry(3845) {
		t.Fatal("should not retry just before the base backoff elapses")
	}
	clk.Advance(time.Second)
	if !hh.shouldRetry(3845) {
		t.Fatal("should retry after the base backoff elapses")
	}

	// A second failure backs off exponentially to 30s.
	hh.record(3845, errors.New("remote port forwarding failed"))
	if hh.shouldRetry(3845) {
		t.Fatal("should not retry inside the second backoff")
	}
	clk.Advance(htrBackoff(2) - time.Second)
	if hh.shouldRetry(3845) {
		t.Fatal("should not retry just before the second backoff elapses")
	}
	clk.Advance(time.Second)
	if !hh.shouldRetry(3845) {
		t.Fatal("should retry after the second backoff elapses")
	}
}

func TestHTRHostPortChangeResetsBackoff(t *testing.T) {
	clk := &fakeNow{now: time.Now()}
	hh := &htrHost{now: clk.Now}

	hh.record(3845, errors.New("remote port forwarding failed"))
	if hh.shouldRetry(3845) {
		t.Fatal("should not retry 3845 during backoff")
	}
	// A new port (e.g. the user changed htr_port) gets a fresh attempt immediately.
	if !hh.shouldRetry(3846) {
		t.Fatal("new port should retry immediately despite old port's backoff")
	}
	// The old port is still backed off.
	if hh.shouldRetry(3845) {
		t.Fatal("old port should still be backed off")
	}
}

func TestHTRHostSuccessClearsBackoff(t *testing.T) {
	clk := &fakeNow{now: time.Now()}
	hh := &htrHost{now: clk.Now}

	// Two failures escalate to the 30s backoff.
	hh.record(3845, errors.New("remote port forwarding failed"))
	hh.record(3845, errors.New("remote port forwarding failed"))
	if hh.shouldRetry(3845) {
		t.Fatal("should not retry during the escalated backoff")
	}
	clk.Advance(htrBackoff(2))
	if !hh.shouldRetry(3845) {
		t.Fatal("should retry once the escalated backoff elapses")
	}
	// A success resets the failure ladder: the next failure starts at the
	// base interval again rather than continuing to escalate. (The forward
	// isn't actually live here — htr just isn't running — so a success still
	// imposes the base cooldown rather than allowing immediate retry.)
	hh.record(3845, nil)
	if hh.shouldRetry(3845) {
		t.Fatal("success should impose the base cooldown, not allow immediate retry")
	}
	clk.Advance(htrAutoRetryInterval)
	if !hh.shouldRetry(3845) {
		t.Fatal("should retry after the success cooldown elapses")
	}
	// One failure after a success uses the base interval (ladder reset), not
	// the escalated 30s.
	hh.record(3845, errors.New("remote port forwarding failed"))
	if hh.shouldRetry(3845) {
		t.Fatal("should not retry during the base backoff after a success")
	}
	clk.Advance(htrBackoff(1))
	if !hh.shouldRetry(3845) {
		t.Fatal("should retry at the base interval after the success reset the ladder")
	}
}

func TestHTRBackoffIsCapped(t *testing.T) {
	// The saturated failure count lands exactly on the documented cap; that is
	// what makes the cap branch in htrBackoff reachable (and worth testing).
	if got := htrBackoff(htrAutoMaxFails); got != htrAutoRetryMax {
		t.Fatalf("backoff(%d) = %v, want the %v cap", htrAutoMaxFails, got, htrAutoRetryMax)
	}
	if got := htrBackoff(htrAutoMaxFails + 5); got != htrAutoRetryMax {
		t.Fatalf("backoff past the cap = %v, want %v", got, htrAutoRetryMax)
	}
	if got := htrBackoff(1); got != htrAutoRetryInterval {
		t.Fatalf("backoff(1) = %v, want %v", got, htrAutoRetryInterval)
	}
	if got := htrBackoff(2); got != 2*htrAutoRetryInterval {
		t.Fatalf("backoff(2) = %v, want %v", got, 2*htrAutoRetryInterval)
	}
}

// TestHTRHostFailuresSaturateAtTheCap drives record() through more failures than
// the ladder has steps and asserts the scheduled wait stops at the cap instead
// of growing without bound (or being clipped below it).
func TestHTRHostFailuresSaturateAtTheCap(t *testing.T) {
	clk := &fakeNow{now: time.Now()}
	hh := &htrHost{now: clk.Now}

	for i := 0; i < htrAutoMaxFails+3; i++ {
		hh.record(3845, errors.New("remote port forwarding failed"))
	}
	hh.mu.Lock()
	failures, wait := hh.failures, hh.nextTry.Sub(hh.lastAttempt)
	hh.mu.Unlock()
	if failures != htrAutoMaxFails {
		t.Fatalf("failures = %d, want the %d cap", failures, htrAutoMaxFails)
	}
	if wait != htrAutoRetryMax {
		t.Fatalf("scheduled wait = %v, want %v", wait, htrAutoRetryMax)
	}
	// The cap is honoured by shouldRetry, not just by the arithmetic.
	clk.Advance(htrAutoRetryMax - time.Second)
	if hh.shouldRetry(3845) {
		t.Fatal("should not retry one second before the cap elapses")
	}
	clk.Advance(time.Second)
	if !hh.shouldRetry(3845) {
		t.Fatal("should retry once the capped interval elapses")
	}
}
