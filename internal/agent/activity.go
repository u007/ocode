package agent

import (
	"sync"
	"time"
)

// ToolActivity holds a running tool's name and when it started.
type ToolActivity struct {
	Name      string
	StartedAt time.Time
}

// ActivitySnapshot is a point-in-time view of what the agent is doing.
type ActivitySnapshot struct {
	LLMRunning   bool
	ActiveTools  []ToolActivity
	ActiveAgents []string
}

// ActivityTracker tracks running LLM calls, tools, and sub-agents.
type ActivityTracker struct {
	mu           sync.Mutex
	llmRunning   bool
	activeTools  []ToolActivity
	activeAgents []string
	notify       chan ActivitySnapshot
}

func newActivityTracker() *ActivityTracker {
	return &ActivityTracker{notify: make(chan ActivitySnapshot, 1)}
}

func (t *ActivityTracker) setLLMRunning(v bool) {
	t.mu.Lock()
	t.llmRunning = v
	snap := t.snapshot()
	t.publishLocked(snap)
	t.mu.Unlock()
}

func (t *ActivityTracker) toolStarted(name string) {
	t.mu.Lock()
	t.activeTools = append(t.activeTools, ToolActivity{Name: name, StartedAt: time.Now()})
	snap := t.snapshot()
	t.publishLocked(snap)
	t.mu.Unlock()
}

func (t *ActivityTracker) toolDone(name string) {
	t.mu.Lock()
	for i, ta := range t.activeTools {
		if ta.Name == name {
			t.activeTools = append(t.activeTools[:i], t.activeTools[i+1:]...)
			break
		}
	}
	snap := t.snapshot()
	t.publishLocked(snap)
	t.mu.Unlock()
}

func (t *ActivityTracker) agentStarted(name string) {
	t.mu.Lock()
	t.activeAgents = append(t.activeAgents, name)
	snap := t.snapshot()
	t.publishLocked(snap)
	t.mu.Unlock()
}

func (t *ActivityTracker) agentDone(name string) {
	t.mu.Lock()
	for i, v := range t.activeAgents {
		if v == name {
			t.activeAgents = append(t.activeAgents[:i], t.activeAgents[i+1:]...)
			break
		}
	}
	snap := t.snapshot()
	t.publishLocked(snap)
	t.mu.Unlock()
}

// snapshot returns a copy of current state. Must be called with mu held.
func (t *ActivityTracker) snapshot() ActivitySnapshot {
	tools := make([]ToolActivity, len(t.activeTools))
	copy(tools, t.activeTools)
	agents := make([]string, len(t.activeAgents))
	copy(agents, t.activeAgents)
	return ActivitySnapshot{
		LLMRunning:   t.llmRunning,
		ActiveTools:  tools,
		ActiveAgents: agents,
	}
}

// Snapshot returns a copy of the current activity state. Safe to call from
// any goroutine: it holds t.mu only long enough to copy the three fields, and
// the returned slices never alias the tracker's internals.
//
// This is the PULL half of the activity feed. The TUI is a PUSH consumer — it
// blocks on Notify() and re-broadcasts each change as a full TUIStatus (see
// listenActivity in the TUI model). Headless web/desktop has no TUI to do that,
// so the server PULLs this snapshot to stamp the same fields into the status
// snapshots and agent_activity events that drive its status bar. Pulling never
// competes with a Notify() reader, so both halves are safe at once.
func (t *ActivityTracker) Snapshot() ActivitySnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshot()
}

// Notify returns the channel TUI consumers read snapshots from.
func (t *ActivityTracker) Notify() chan ActivitySnapshot {
	return t.notify
}

// publishLocked drains any stale snapshot and pushes the latest. Must be
// called with t.mu held so concurrent senders cannot interleave their
// drain/replace operations and drop updates.
func (t *ActivityTracker) publishLocked(snap ActivitySnapshot) {
	select {
	case <-t.notify:
	default:
	}
	select {
	case t.notify <- snap:
	default:
	}
}
