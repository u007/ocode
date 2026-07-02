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

func TestWatchEmitsOnChangeOnly(t *testing.T) {
	states := make(chan []server.RunState, 3)
	states <- []server.RunState{rs("s1", "a", false)} // baseline: 1 running
	states <- []server.RunState{rs("s1", "a", false)} // no change → no emit
	states <- []server.RunState{rs("s1", "a", true)}  // finished → emit

	var current []server.RunState
	source := func() []server.RunState {
		select {
		case s := <-states:
			current = s
		default: // keep returning last state once the script is exhausted
		}
		return current
	}

	got := make(chan Summary, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go Watch(ctx, 10*time.Millisecond, source, func(s Summary) { got <- s })

	first := <-got // baseline emit: RunningCount 1
	if first.RunningCount != 1 || len(first.Finished) != 0 {
		t.Fatalf("first emit = %+v, want RunningCount 1, no Finished", first)
	}
	second := <-got // the finish transition
	if second.RunningCount != 0 || len(second.Finished) != 1 {
		t.Fatalf("second emit = %+v, want RunningCount 0, 1 Finished", second)
	}
	select {
	case extra := <-got:
		t.Fatalf("unexpected third emit: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWatchEmitsWhenRunsVanish(t *testing.T) {
	// A running run disappearing from the snapshot (session deleted) must
	// still emit the count drop so the badge clears.
	states := make(chan []server.RunState, 2)
	states <- []server.RunState{rs("s1", "a", false)} // baseline: 1 running
	states <- []server.RunState{}                     // session gone: 0 running, no Finished

	var current []server.RunState
	source := func() []server.RunState {
		select {
		case s := <-states:
			current = s
		default:
		}
		return current
	}

	got := make(chan Summary, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go Watch(ctx, 10*time.Millisecond, source, func(s Summary) { got <- s })

	<-got // baseline: 1 running
	second := <-got
	if second.RunningCount != 0 || len(second.Finished) != 0 {
		t.Fatalf("second emit = %+v, want RunningCount 0 with no Finished", second)
	}
}
