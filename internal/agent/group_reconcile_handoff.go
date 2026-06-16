package agent

import (
	"github.com/u007/ocode/internal/notebus"
)

// reconcileHandoffMessage runs the mechanical pre-pass on
// the bus log + per-agent completion status and returns a
// single system message that the orchestrator (main LLM)
// reads as the strong-model reconcile handoff. The
// orchestrator then performs the actual judgment —
// resolving contradictions, ranking severity, deciding
// leads-vs-real-findings — and produces the final report.
//
// A nil bus or empty tracker returns nil (no handoff). The
// caller treats that as "no group" and skips appending any
// message.
//
// The hand-off text is byte-stable for the same input
// (ReconcilePrepass is deterministic, RenderReconcile is
// pure), so the orchestrator's prompt cache hits across
// runs that produce the same log.
func reconcileHandoffMessage(bus *notebus.Bus, tracker *groupTracker) *Message {
	if bus == nil || tracker == nil {
		return nil
	}
	snap := bus.Snapshot()
	statuses := tracker.Statuses()
	if len(snap) == 0 && len(statuses) == 0 {
		// Nothing to reconcile. The orchestrator does not
		// need a hand-off for an empty group.
		return nil
	}
	out := notebus.ReconcilePrepass(snap, statuses)
	rendered := notebus.RenderReconcile(out)
	// Wrap in [ocode:reconcile] so the orchestrator's
	// prompt assembler recognizes it. Same pattern as
	// [ocode:notes] in the bus injection path.
	return &Message{
		Role:    "system",
		Content: "[ocode:reconcile]\n" + rendered,
	}
}
