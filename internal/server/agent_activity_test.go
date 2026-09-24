package server

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/session"
)

// TestHeadlessTurnBroadcastsAgentActivity pins the live activity feed behind the
// web/desktop status bar's "⟳ llm · ⚙ tool [time · elapsed] · @ agent" row.
//
// The agent's ActivityTracker had exactly one consumer — the TUI — so a headless
// web/desktop session (both the browser chat and the desktop app are headless)
// had nobody to republish it and the bar could only ever show a bare in-flight
// tool name, or "working…". runTurn must now publish a session-tagged
// `agent_activity` frame as the agent loop progresses.
func TestHeadlessTurnBroadcastsAgentActivity(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour // no heartbeats in this test
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	blocking := newBlockingClient()
	as := newTestSession(h, id, blocking)

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := h.runTurn(id, as, "slow", turnOptions{}); err != nil {
			t.Errorf("runTurn: %v", err)
		}
	}()

	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn never started")
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Event != "agent_activity" || env.SessionID != id {
				continue
			}
			ev, ok := env.Data.(AgentActivityEvent)
			if !ok {
				t.Fatalf("agent_activity data is %T, want AgentActivityEvent", env.Data)
			}
			if ev.SessionID != id {
				t.Fatalf("activity session_id = %q, want %q", ev.SessionID, id)
			}
			if !ev.LLMRunning {
				t.Fatal("agent_activity reported llm_running=false while the LLM call was still in flight")
			}
			// The full `status` snapshot must agree with the live event. A mid-turn
			// `status` (model switch, title-gen, compact) replaces tuiStatus
			// wholesale client-side, so if it omitted activity it would blank the
			// row the event just filled in. Drive the real publisher rather than
			// the helper so the wiring in publishTurnStatusSnapshot is pinned too.
			h.publishTurnStatusSnapshot(id)
			statusDeadline := time.After(3 * time.Second)
			for sawStatus := false; !sawStatus; {
				select {
				case env := <-sub:
					if env.Event != "status" || env.SessionID != id {
						continue
					}
					snap, ok := env.Data.(TUIStatus)
					if !ok {
						t.Fatalf("status data is %T, want TUIStatus", env.Data)
					}
					if !snap.LLMRunning {
						t.Fatal("mid-turn status snapshot left llm_running=false; it would blank the activity row client-side")
					}
					sawStatus = true
				case <-statusDeadline:
					t.Fatal("publishTurnStatusSnapshot published no status frame during a live turn")
				}
			}

			close(blocking.release)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("turn did not finish after the client was released")
			}
			return
		case <-deadline:
			t.Fatal("headless turn published no agent_activity frame while its agent loop was in flight")
		}
	}
}

// TestAgentActivityBroadcastDefersToRCBridge pins the single-consumer rule.
// Activity().Notify() is one channel with one reader slot: while a TUI is
// attached it is the TUI that reads it and republishes the same fields through
// its own COMPLETE `status` snapshots. A second server-side reader would steal
// snapshots from it and leave the bridged status bar frozen — so with a bridge
// attached the server must publish nothing.
func TestAgentActivityBroadcastDefersToRCBridge(t *testing.T) {
	h := NewHandler()
	h.rc = &RCBridge{SessionID: "tui-sess", ResolveCh: make(chan RCResolution, 1)}
	h.turnHeartbeatInterval = time.Hour
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	blocking := newBlockingClient()
	as := newTestSession(h, id, blocking)

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := h.runTurn(id, as, "slow", turnOptions{}); err != nil {
			t.Errorf("runTurn: %v", err)
		}
	}()

	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn never started")
	}

	// Give the turn a moment to publish anything it shouldn't.
	drain := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case env := <-sub:
			if env.Event == "agent_activity" {
				t.Fatal("server published agent_activity while a TUI bridge owns the activity feed")
			}
		case <-drain:
			break loop
		}
	}

	close(blocking.release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not finish after the client was released")
	}
}

// TestApplySessionActivityNoLiveAgent keeps the full-snapshot stamper from
// inventing activity for a session with no resident agent (idle-evicted,
// restored from disk, or not yet built). Those have nothing running, and the
// status bar's base "working…" label is the correct display.
func TestApplySessionActivityNoLiveAgent(t *testing.T) {
	h := NewHandler()
	snap := h.buildStatusSnapshot()
	h.applySessionActivity(&snap, session.NewSessionID())
	if snap.LLMRunning || snap.ActiveTools != nil || snap.ActiveAgents != nil {
		t.Fatalf("activity stamped for a session with no agent: %+v", snap)
	}
}
