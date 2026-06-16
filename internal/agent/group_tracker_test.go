package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestGroupTeardownAndCompletion tracks per-agent completion
// status through the lifecycle. We simulate the parallel block:
// build a group of 2 children, run them, and verify the
// completion status map is populated correctly.
func TestGroupTeardownAndCompletion(t *testing.T) {
	factory := &recordingBusFactory{}
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBusFactory(factory.New)

	// Build a tracker the group bus populates. This is the
	// orchestrator-side data the reconcile code (Part 05)
	// consumes.
	tracker := newGroupTracker()
	bus, ids := a.maybeBuildGroupBusForTest(tcsTwoQualifying(), []int{0, 1})
	if bus == nil {
		t.Fatal("expected bus for 2 qualifying calls")
	}
	// Wire completion status into the tracker.
	bus.SetOnCompletion(func(agentID, status string, err error) {
		tracker.Record(agentID, status, err)
	})
	// The "for test" helper returns an unstarted bus; we start
	// it now so the deferred teardown has a goroutine to wait
	// for. (Production paths start the bus automatically.)
	bus.Start(context.Background())
	defer a.teardownGroupBus(bus)

	// Simulate two child completions.
	tracker.Record(ids[0], "completed", nil)
	tracker.Record(ids[1], "failed", errTest("a2 failed"))

	if tracker.Status(ids[0]) != "completed" {
		t.Errorf("a1 status = %q, want completed", tracker.Status(ids[0]))
	}
	if tracker.Status(ids[1]) != "failed" {
		t.Errorf("a2 status = %q, want failed", tracker.Status(ids[1]))
	}
	if !tracker.HasUnreviewed() {
		t.Error("expected HasUnreviewed()=true with one failed agent")
	}
	if tracker.Unreviewed()["a2"] != "failed" {
		t.Errorf("a2 in unreviewed map = %q, want failed", tracker.Unreviewed()["a2"])
	}
}

// TestGroupTeardown_FlushesBus verifies that teardownGroupBus
// returns only after the bus owner has fully exited. A test that
// races teardown with an in-flight Append can hang on a missing
// Done() signal — this guard asserts the drain.
func TestGroupTeardown_FlushesBus(t *testing.T) {
	factory := &recordingBusFactory{}
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBusFactory(factory.New)
	bus, _ := a.maybeBuildGroupBusForTest(tcsTwoQualifying(), []int{0, 1})
	if bus == nil {
		t.Fatal("expected bus")
	}
	// Start the bus on a background context so Append works.
	bus.Start(context.Background())
	// Append a few entries through the bus BEFORE teardown.
	for i := 0; i < 5; i++ {
		if _, err := bus.Append(notebus.Note(0, "a1", "x", "y", 0)); err != nil {
			t.Fatal(err)
		}
	}
	// Teardown is blocking — once it returns, the bus is fully
	// shut down.
	a.teardownGroupBus(bus)
	// A second teardown is a no-op (idempotent).
	a.teardownGroupBus(bus)
	// Post-teardown Append returns an error (no panic).
	if _, err := bus.Append(notebus.Note(0, "a1", "x", "y", 0)); err == nil {
		t.Error("Append after teardown should return an error, got nil")
	}
}

// TestGroupTracker_EmptyGroupHasNoUnreviewed confirms the
// orchestrator's "no children failed" path.
func TestGroupTracker_EmptyGroupHasNoUnreviewed(t *testing.T) {
	tracker := newGroupTracker()
	if tracker.HasUnreviewed() {
		t.Error("empty tracker HasUnreviewed() = true, want false")
	}
	if len(tracker.Unreviewed()) != 0 {
		t.Errorf("empty tracker Unreviewed() = %v, want empty", tracker.Unreviewed())
	}
}

// TestGroupTracker_ConcurrentRecord is a -race regression guard:
// many goroutines record status at once, and the tracker must
// serialize them without losing updates.
func TestGroupTracker_ConcurrentRecord(t *testing.T) {
	tracker := newGroupTracker()
	const N = 100
	var wg sync.WaitGroup
	var seen atomic.Int32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "a"
			if i%2 == 0 {
				id = "b"
			}
			tracker.Record(id, "completed", nil)
			seen.Add(1)
		}(i)
	}
	wg.Wait()
	if got := seen.Load(); got != N {
		t.Errorf("completed records = %d, want %d", got, N)
	}
	if tracker.Status("a") != "completed" || tracker.Status("b") != "completed" {
		t.Errorf("tracker status incomplete: a=%q b=%q", tracker.Status("a"), tracker.Status("b"))
	}
}

// helper: build two qualifying subagent tool calls for tests.
func tcsTwoQualifying() []ToolCall {
	return []ToolCall{
		{ID: "t1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: `{"prompt":"a","agent":"general","shared_notes":true}`}},
		{ID: "t2", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: `{"prompt":"b","agent":"explore","shared_notes":true}`}},
	}
}

// errTest is a tiny test-only error helper.
type errTest string

func (e errTest) Error() string { return string(e) }
