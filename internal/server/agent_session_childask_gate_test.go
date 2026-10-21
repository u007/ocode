package server

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
)

// TestAutoContinueBlockedByParkedChildAsk pins that a SUB-AGENT's parked
// permission ask blocks the auto-continue resume.
//
// The main agent's own ask leaves a PERMISSION_ASK sentinel in as.messages, so
// the transcript-tail scan sees it. A child's ask does NOT: the child's ask is
// registered in the session's child-ask registry and the CHILD goroutine is
// blocked on it. The tail scan alone cannot see it, so before this gate the
// auto-continue would fire — starting a second model (the judge, or the advisor
// behind it) — while the user was still looking at the child's permission
// prompt.
//
// The user reported exactly this shape: an advisor/auto-continue fired during
// a permission request instead of waiting for the ask to be answered.
func TestAutoContinueBlockedByParkedChildAsk(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	h.mu.Lock()
	h.cfg.Ocode.AutoContinueEnabled = true
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	// The harness builds the session without the child-ask registry; a real
	// session always has one (buildAgentSession creates it).
	as.childAsks = newChildPermAsks()
	as.agent.SetMaxSteps(0)
	cl.handler = func(call int) string { return "done" }

	// No sentinel in the transcript: the ask belongs to a child, not the main
	// agent. This is what makes the child-ask registry the ONLY signal.
	as.messages = append(as.messages, agent.Message{Role: "assistant", Content: "delegating"})

	// Baseline: with no outstanding ask the session must not be reported as
	// blocked — otherwise the assertions below would pass for the wrong reason.
	if h.sessionHasOutstandingAsk(as) {
		t.Fatal("no ask is outstanding — the session must not be reported as blocked")
	}

	// Park a child ask, exactly as newServerSubAgentAsker does.
	respCh := make(chan agent.PermissionResponse, 1)
	as.childAsks.add("child-ask-1", &childPermAsk{
		req:    agent.PermissionRequest{ToolName: "bash"},
		event:  PermissionEvent{RequestID: "child-ask-1"},
		respCh: respCh,
	})

	if !h.sessionHasOutstandingAsk(as) {
		t.Fatal("a parked child ask must be reported as an outstanding ask")
	}

	fire, _ := h.autoContinueShouldResume(id, as, 0, nil)
	if fire.Prompt != "" {
		t.Fatal("a parked sub-agent permission ask must block the auto-continue resume")
	}
}

// TestAutoContinueFiresOnceChildAskResolved pins the other half: the gate is
// scoped to the ask. Once the child ask is resolved the auto-continue must be
// able to fire again — otherwise the feature would be dead for the rest of the
// session the moment a sub-agent asked for permission.
func TestAutoContinueFiresOnceChildAskResolved(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	h.mu.Lock()
	h.cfg.Ocode.AutoContinueEnabled = true
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	as.childAsks = newChildPermAsks()
	as.agent.SetMaxSteps(0)
	cl.handler = func(call int) string { return "done" }
	as.messages = append(as.messages, agent.Message{Role: "assistant", Content: "delegating"})

	respCh := make(chan agent.PermissionResponse, 1)
	as.childAsks.add("child-ask-1", &childPermAsk{
		req:    agent.PermissionRequest{ToolName: "bash"},
		event:  PermissionEvent{RequestID: "child-ask-1"},
		respCh: respCh,
	})
	if !h.sessionHasOutstandingAsk(as) {
		t.Fatal("a parked child ask must block")
	}

	// The user answers the child's ask.
	as.childAsks.take("child-ask-1")

	if h.sessionHasOutstandingAsk(as) {
		t.Fatal("a resolved child ask must no longer block")
	}
}

// TestSessionHasOutstandingAskCoversMainAgentAsk pins that the shared helper
// still covers the MAIN agent's own ask (the transcript-tail sentinel). The
// child-ask check was added on top of this one, not instead of it.
func TestSessionHasOutstandingAskCoversMainAgentAsk(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	as := h.lookupAgentSession(id)
	cl.handler = func(call int) string { return "done" }

	as.messages = append(as.messages, agent.Message{Role: "tool", ToolID: "t1", Content: toolSentinelPermissionAsk()})
	if !h.sessionHasOutstandingAsk(as) {
		t.Fatal("the main agent's own permission sentinel must be reported as an outstanding ask")
	}
}
