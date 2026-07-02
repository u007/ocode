package desktop

import (
	"context"
	"time"

	"github.com/u007/ocode/internal/server"
)

// Summary is one observed change in the run-state world, consumed by the
// desktop shell's badge and notification logic.
type Summary struct {
	RunningCount int
	// PendingAsks is the number of sessions blocked on a permission prompt.
	PendingAsks int
	// Finished lists runs that transitioned running→ended since the
	// previous poll. Nil prev (first poll) yields none: startup must not
	// replay history as notifications.
	Finished []server.RunState
}

// runKey identifies a run across sessions. Run IDs are only unique per
// session registry ("agent-run-<n>" restarts at 1 in every session), so the
// session must be part of the key.
type runKey struct{ session, id string }

// Diff compares two RunStates snapshots. PendingAsks is not derived here —
// Watch fills it from its source.
func Diff(prev, cur []server.RunState) Summary {
	sum := Summary{}
	prevRunning := make(map[runKey]bool, len(prev))
	for _, p := range prev {
		if !p.Ended {
			prevRunning[runKey{p.SessionID, p.ID}] = true
		}
	}
	for _, c := range cur {
		if !c.Ended {
			sum.RunningCount++
			continue
		}
		if prevRunning[runKey{c.SessionID, c.ID}] {
			sum.Finished = append(sum.Finished, c)
		}
	}
	return sum
}

// Watch polls source on interval and invokes onChange when the running
// count or pending-prompt count changed, or runs finished — the same
// poll-and-diff pattern HandleRunsStream uses over SSE. It always emits one
// baseline summary on the first poll, then stays quiet while nothing
// changes. Blocks until ctx is done.
func Watch(ctx context.Context, interval time.Duration, source func() ([]server.RunState, int), onChange func(Summary)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var prev []server.RunState
	lastRunning, lastPending := -1, -1 // sentinels: force the baseline emit
	for {
		cur, pending := source()
		sum := Diff(prev, cur)
		sum.PendingAsks = pending
		if sum.RunningCount != lastRunning || sum.PendingAsks != lastPending || len(sum.Finished) > 0 {
			onChange(sum)
			lastRunning = sum.RunningCount
			lastPending = sum.PendingAsks
		}
		prev = cur

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
