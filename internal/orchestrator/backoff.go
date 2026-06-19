package orchestrator

import (
	"math"
	"time"
)

// BackoffPolicy defines exponential backoff with jitter for compile retries.
// Used only in --no-worktree mode where another agent may transiently break the build.
type BackoffPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
	JitterFactor float64
}

// DefaultBackoff is the policy applied in --no-worktree mode.
var DefaultBackoff = BackoffPolicy{
	InitialDelay: 20 * time.Second,
	MaxDelay:     120 * time.Second,
	MaxAttempts:  5,
	JitterFactor: 0.3,
}

// Delay returns the sleep duration for the given attempt (0-indexed).
// seed must be in [0,1] — callers provide math/rand.Float64() or a fixed value
// for tests. Delay never sleeps itself so callers control timing.
func (b BackoffPolicy) Delay(attempt int, seed float64) time.Duration {
	base := float64(b.InitialDelay) * math.Pow(2, float64(attempt))
	jitter := 1 + b.JitterFactor*(seed*2-1)
	d := time.Duration(base * jitter)
	if d > b.MaxDelay {
		d = b.MaxDelay
	}
	return d
}
