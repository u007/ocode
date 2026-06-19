package orchestrator

import (
	"testing"
	"time"
)

func TestBackoffDelay_firstAttempt(t *testing.T) {
	b := DefaultBackoff
	d := b.Delay(0, 0.5) // seed 0.5 = no jitter (middle)
	// 20s * 2^0 * (1 + 0.3*(0.5*2-1)) = 20s * 1 * 1.0 = 20s
	if d < 18*time.Second || d > 22*time.Second {
		t.Errorf("first attempt delay = %v, want ~20s", d)
	}
}

func TestBackoffDelay_clampsToMax(t *testing.T) {
	b := DefaultBackoff
	d := b.Delay(10, 1.0) // very high attempt — should clamp to MaxDelay
	if d != b.MaxDelay {
		t.Errorf("delay = %v, want %v (MaxDelay)", d, b.MaxDelay)
	}
}

func TestBackoffDelay_jitterRange(t *testing.T) {
	b := DefaultBackoff
	low := b.Delay(0, 0.0)
	high := b.Delay(0, 1.0)
	if low >= high {
		t.Errorf("jitter not applied: low=%v high=%v", low, high)
	}
	// Range: 20s * (1 ± 0.3) = [14s, 26s]
	if low < 13*time.Second || high > 27*time.Second {
		t.Errorf("jitter out of expected range: low=%v high=%v", low, high)
	}
}

func TestBackoffPolicy_maxAttempts(t *testing.T) {
	b := DefaultBackoff
	if b.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", b.MaxAttempts)
	}
}
