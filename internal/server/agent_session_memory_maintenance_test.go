package server

import (
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// memoryMaintenanceDequeued captures the maintenance worker's "no LLM client
// available" skip line on the debug sink, which the worker emits once it has
// dequeued a request. The agent has no LLM client, so this is the observable
// proof a request was queued.
func memoryMaintenanceDequeued(t *testing.T) chan string {
	t.Helper()
	dequeued := make(chan string, 8)
	prevSink := agent.DebugAppend
	agent.DebugAppend = func(kind, msg string) {
		if kind == "MEMORY" && strings.Contains(msg, "no LLM client available") {
			dequeued <- msg
		}
	}
	t.Cleanup(func() { agent.DebugAppend = prevSink })
	return dequeued
}

// TestHandleServerJobEventQueuesMemoryMaintenance pins that the web/desktop
// server runs memory maintenance after a durable job completion: a background
// agent run is injected as a turn, so it queues maintenance first.
func TestHandleServerJobEventQueuesMemoryMaintenance(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := NewHandler()

	const sessionID = "sess-memory-maintenance-queue"
	h.mu.Lock()
	h.agents[agentKey(sessionID)] = &agentSession{
		messages: []agent.Message{{Role: "user", Content: "remember this"}},
	}
	h.mu.Unlock()

	ag := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(ag.Shutdown)
	ag.SetWorkDir(t.TempDir())
	ag.SetMemoryEnabled(true)

	dequeued := memoryMaintenanceDequeued(t)

	h.handleServerJobEvent(sessionID, ag, agent.JobEvent{
		Kind:       "agent",
		ID:         "run-1",
		Name:       "explore",
		Status:     "done",
		Background: true,
	})

	select {
	case <-dequeued:
	case <-time.After(5 * time.Second):
		t.Fatal("background job event did not queue a memory maintenance request")
	}
}

// TestHandleServerJobEventSkipsMemoryMaintenanceForSyncAgentRun pins that a
// synchronous agent run, whose result already reached the parent through the
// task tool, does not queue memory maintenance.
func TestHandleServerJobEventSkipsMemoryMaintenanceForSyncAgentRun(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := NewHandler()

	const sessionID = "sess-memory-maintenance-sync"
	h.mu.Lock()
	h.agents[agentKey(sessionID)] = &agentSession{
		messages: []agent.Message{{Role: "user", Content: "remember this"}},
	}
	h.mu.Unlock()

	ag := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(ag.Shutdown)
	ag.SetWorkDir(t.TempDir())
	ag.SetMemoryEnabled(true)

	dequeued := memoryMaintenanceDequeued(t)

	h.handleServerJobEvent(sessionID, ag, agent.JobEvent{
		Kind:   "agent",
		ID:     "run-1",
		Name:   "explore",
		Status: "done",
	})

	select {
	case msg := <-dequeued:
		t.Fatalf("synchronous agent run queued memory maintenance: %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestJobMemoryMaintenanceDefersWhileTurnHoldsLock pins that a job event which
// lands while a turn holds as.mu is neither dropped nor queued mid-turn. It is
// recorded as one deferred marker (the latest job wins), and the turn-end flush
// queues exactly one memory maintenance pass for all of them.
func TestJobMemoryMaintenanceDefersWhileTurnHoldsLock(t *testing.T) {
	setHomeTree(t, t.TempDir())

	as := &agentSession{
		messages: []agent.Message{{Role: "user", Content: "remember this"}},
	}
	ag := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(ag.Shutdown)
	ag.SetWorkDir(t.TempDir())
	ag.SetMemoryEnabled(true)

	dequeued := memoryMaintenanceDequeued(t)

	as.mu.Lock()
	for _, id := range []string{"job-1", "job-2", "job-3"} {
		as.queueJobMemoryMaintenance(ag, agent.JobEvent{Kind: "process", ID: id, Status: "exited"})
	}
	as.memoryMaintMu.Lock()
	marker := as.memoryMaintDeferred
	as.memoryMaintMu.Unlock()

	// Checked while still holding as.mu: once it is released the waiter started
	// by queueJobMemoryMaintenance may legitimately run the pass.
	select {
	case msg := <-dequeued:
		as.mu.Unlock()
		t.Fatalf("job event queued memory maintenance while the turn held as.mu: %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
	as.mu.Unlock()
	if marker == nil || marker.ev.ID != "job-3" {
		t.Fatalf("deferred marker = %+v, want the latest job (job-3)", marker)
	}

	as.flushDeferredMemoryMaintenance()
	select {
	case <-dequeued:
	case <-time.After(5 * time.Second):
		t.Fatal("turn-end flush did not queue the deferred memory maintenance pass")
	}

	as.flushDeferredMemoryMaintenance()
	select {
	case msg := <-dequeued:
		t.Fatalf("second flush queued another pass for one deferred marker: %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestJobMemoryMaintenanceNotStrandedWhenLockReleasedWithoutTurnEnd pins that a
// job event deferred behind a lock holder that is not a turn (an HTTP
// compaction, recap or stream never flushes) still runs its pass once that
// holder releases as.mu, with no turn end and no flush call.
func TestJobMemoryMaintenanceNotStrandedWhenLockReleasedWithoutTurnEnd(t *testing.T) {
	setHomeTree(t, t.TempDir())

	as := &agentSession{
		messages: []agent.Message{{Role: "user", Content: "remember this"}},
	}
	ag := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(ag.Shutdown)
	ag.SetWorkDir(t.TempDir())
	ag.SetMemoryEnabled(true)

	dequeued := memoryMaintenanceDequeued(t)

	as.mu.Lock()
	as.queueJobMemoryMaintenance(ag, agent.JobEvent{Kind: "process", ID: "job-1", Status: "exited"})
	as.mu.Unlock()

	select {
	case <-dequeued:
	case <-time.After(5 * time.Second):
		t.Fatal("deferred memory maintenance was stranded after the lock holder released as.mu")
	}
}

// TestTakeDeferredMemoryMaintenanceReadsLiveTranscript pins that the deferred
// pass is built from the transcript as it stands when the turn ends, not as it
// stood when the job completed. It is taken exactly once, and a busy lock
// leaves the marker in place for a later take.
func TestTakeDeferredMemoryMaintenanceReadsLiveTranscript(t *testing.T) {
	setHomeTree(t, t.TempDir())

	ag := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(ag.Shutdown)

	as := &agentSession{
		messages: []agent.Message{{Role: "user", Content: "before the job"}},
	}
	// The marker is set directly: queueJobMemoryMaintenance also starts a waiter
	// that would take it the moment the lock is released, racing the take below.
	as.mu.Lock()
	as.memoryMaintMu.Lock()
	as.memoryMaintDeferred = &deferredMemoryMaint{ag: ag, ev: agent.JobEvent{Kind: "agent", ID: "run-1", Name: "explore", Status: "done", Background: true}}
	as.memoryMaintMu.Unlock()
	if _, _, ok := as.takeDeferredMemoryMaintenance(); ok {
		t.Fatal("take succeeded while as.mu was held")
	}
	as.messages = append(as.messages, agent.Message{Role: "assistant", Content: "written during the turn"})
	as.mu.Unlock()

	d, msgs, ok := as.takeDeferredMemoryMaintenance()
	if !ok || d.ev.ID != "run-1" || d.ag != ag {
		t.Fatalf("take after unlock: ok=%v marker=%+v, want the run-1 marker", ok, d)
	}
	if len(msgs) != 2 || msgs[1].Content != "written during the turn" {
		t.Fatalf("deferred messages = %v, want the transcript as it stands at turn end", msgs)
	}
	if _, _, ok := as.takeDeferredMemoryMaintenance(); ok {
		t.Fatal("second take returned the same deferred marker")
	}
}

// TestFlushDeferredMemoryMaintenanceWithoutPendingQueuesNothing pins that a
// turn end with no deferred job event queues no memory maintenance pass.
func TestFlushDeferredMemoryMaintenanceWithoutPendingQueuesNothing(t *testing.T) {
	setHomeTree(t, t.TempDir())

	as := &agentSession{
		messages: []agent.Message{{Role: "user", Content: "remember this"}},
	}
	ag := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(ag.Shutdown)
	ag.SetWorkDir(t.TempDir())
	ag.SetMemoryEnabled(true)

	dequeued := memoryMaintenanceDequeued(t)

	as.flushDeferredMemoryMaintenance()
	select {
	case msg := <-dequeued:
		t.Fatalf("flush with no deferred marker queued memory maintenance: %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestRecentMemoryContextKeepsLastUserAndAssistantTurns pins the message
// selection for memory maintenance: the last eight user or assistant messages
// with text, oldest first, skipping other roles and blank content.
func TestRecentMemoryContextKeepsLastUserAndAssistantTurns(t *testing.T) {
	var msgs []agent.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, agent.Message{Role: "user", Content: "u" + string(rune('a'+i))})
		msgs = append(msgs, agent.Message{Role: "tool", Content: "tool output"})
		msgs = append(msgs, agent.Message{Role: "assistant", Content: "   "})
	}

	got := recentMemoryContext(msgs)

	if len(got) != memoryMaintenanceRecentLimit {
		t.Fatalf("got %d messages, want %d", len(got), memoryMaintenanceRecentLimit)
	}
	for i, m := range got {
		if m.Role != "user" {
			t.Fatalf("message %d role = %q, want user", i, m.Role)
		}
	}
	if first, last := got[0].Content, got[len(got)-1].Content; first != "uc" || last != "uj" {
		t.Fatalf("window = %q..%q, want uc..uj (oldest first)", first, last)
	}
}

// TestHandleServerJobEventSkipsMemoryMaintenanceForPulseSession pins that a
// pulse assistant job never queues project memory maintenance. Pulse keeps its
// own memory store, and its agent's WorkDir is the pulse root.
func TestHandleServerJobEventSkipsMemoryMaintenanceForPulseSession(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := NewHandler()

	sessionID := pulseSessionPrefix + "memory-maintenance-skip"
	h.mu.Lock()
	h.agents[agentKey(sessionID)] = &agentSession{
		messages: []agent.Message{{Role: "user", Content: "remember this"}},
	}
	h.mu.Unlock()

	ag := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(ag.Shutdown)
	ag.SetWorkDir(t.TempDir())
	ag.SetMemoryEnabled(true)

	dequeued := make(chan string, 8)
	prevSink := agent.DebugAppend
	agent.DebugAppend = func(kind, msg string) {
		if kind == "MEMORY" {
			dequeued <- msg
		}
	}
	t.Cleanup(func() { agent.DebugAppend = prevSink })

	h.handleServerJobEvent(sessionID, ag, agent.JobEvent{
		Kind:   "agent",
		ID:     "run-1",
		Name:   "explore",
		Status: "done",
	})

	select {
	case msg := <-dequeued:
		t.Fatalf("pulse job event queued memory maintenance: %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
}
