package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/u007/ocode/internal/server"
)

func rs(session, id string, ended bool) server.RunState {
	return server.RunState{SessionID: session, ID: id, Ended: ended}
}

// scriptedSource returns a Watch source that steps through the given states
// (with matching pending-ask counts), then keeps returning the last one.
func scriptedSource(states [][]server.RunState, pending []int) func() ([]server.RunState, int) {
	i := -1
	return func() ([]server.RunState, int) {
		if i < len(states)-1 {
			i++
		}
		return states[i], pending[i]
	}
}

func TestDiffCountsRunning(t *testing.T) {
	cur := []server.RunState{rs("s1", "a", false), rs("s1", "b", true), rs("s2", "c", false)}
	sum := Diff(nil, cur)
	if sum.RunningCount != 2 {
		t.Fatalf("RunningCount = %d, want 2", sum.RunningCount)
	}
	if len(sum.Finished) != 0 {
		t.Fatalf("nil prev must produce no Finished (first poll is baseline), got %d", len(sum.Finished))
	}
}

func TestDiffDetectsFinishedTransition(t *testing.T) {
	prev := []server.RunState{rs("s1", "a", false), rs("s1", "b", false)}
	cur := []server.RunState{rs("s1", "a", true), rs("s1", "b", false)}
	sum := Diff(prev, cur)
	if len(sum.Finished) != 1 || sum.Finished[0].ID != "a" {
		t.Fatalf("Finished = %+v, want exactly run a", sum.Finished)
	}
	if sum.RunningCount != 1 {
		t.Fatalf("RunningCount = %d, want 1", sum.RunningCount)
	}
}

func TestDiffKeysBySessionAndID(t *testing.T) {
	// Same run ID in two sessions must not cross-match.
	prev := []server.RunState{rs("s1", "a", false), rs("s2", "a", true)}
	cur := []server.RunState{rs("s1", "a", true), rs("s2", "a", true)}
	sum := Diff(prev, cur)
	if len(sum.Finished) != 1 || sum.Finished[0].SessionID != "s1" {
		t.Fatalf("Finished = %+v, want exactly s1/a", sum.Finished)
	}
}

func collectSummaries(t *testing.T, source func() ([]server.RunState, int)) chan Summary {
	t.Helper()
	got := make(chan Summary, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	go Watch(ctx, 10*time.Millisecond, source, func(s Summary) { got <- s })
	return got
}

func assertNoMoreEmits(t *testing.T, got chan Summary) {
	t.Helper()
	select {
	case extra := <-got:
		t.Fatalf("unexpected extra emit: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWatchEmitsOnChangeOnly(t *testing.T) {
	got := collectSummaries(t, scriptedSource(
		[][]server.RunState{
			{rs("s1", "a", false)}, // baseline: 1 running
			{rs("s1", "a", false)}, // no change → no emit
			{rs("s1", "a", true)},  // finished → emit
		},
		[]int{0, 0, 0},
	))

	first := <-got
	if first.RunningCount != 1 || len(first.Finished) != 0 {
		t.Fatalf("first emit = %+v, want RunningCount 1, no Finished", first)
	}
	second := <-got
	if second.RunningCount != 0 || len(second.Finished) != 1 {
		t.Fatalf("second emit = %+v, want RunningCount 0, 1 Finished", second)
	}
	assertNoMoreEmits(t, got)
}

func TestWatchEmitsWhenRunsVanish(t *testing.T) {
	// A running run disappearing from the snapshot (session deleted) must
	// still emit the count drop so the badge clears.
	got := collectSummaries(t, scriptedSource(
		[][]server.RunState{
			{rs("s1", "a", false)}, // baseline: 1 running
			{},                     // session gone: 0 running, no Finished
		},
		[]int{0, 0},
	))

	<-got // baseline: 1 running
	second := <-got
	if second.RunningCount != 0 || len(second.Finished) != 0 {
		t.Fatalf("second emit = %+v, want RunningCount 0 with no Finished", second)
	}
}

func TestWatchEmitsOnPendingAskChange(t *testing.T) {
	// A pending-permission change with no run activity must emit so the
	// badge picks it up, and resolve back to quiet when answered.
	got := collectSummaries(t, scriptedSource(
		[][]server.RunState{{}, {}, {}},
		[]int{0, 1, 1}, // baseline 0 → ask appears → unchanged
	))

	first := <-got
	if first.PendingAsks != 0 {
		t.Fatalf("first emit = %+v, want PendingAsks 0", first)
	}
	second := <-got
	if second.PendingAsks != 1 || second.RunningCount != 0 {
		t.Fatalf("second emit = %+v, want PendingAsks 1", second)
	}
	assertNoMoreEmits(t, got)
}
