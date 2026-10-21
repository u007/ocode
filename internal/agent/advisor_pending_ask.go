package agent

import (
	"sync/atomic"

	"github.com/u007/ocode/internal/tool"
)

// messagesHavePendingAsk reports whether msgs ends with a permission or question
// dialog the user has not answered — i.e. whether the client is showing an ask
// right now. It is one half of the gate the advisor checkpoints use so a second
// model never starts underneath a dialog that is waiting on a human.
//
// The scan walks BACKWARDS and stops at the first assistant row. An assistant
// row means the model has already spoken since that ask was raised, so the round
// that produced it has been answered and its sentinel row is stale history
// (reached when a user sends a new message instead of clicking Allow, which
// also stops the desktop/web app rendering the dialog — its pending_asks scan
// covers the trailing tool run only). Stopping at the first assistant row is
// what makes that case return false instead of muting the advisor forever.
//
// Scanning backwards rather than from "the last user message" is deliberate:
// Step's tail injectors (injectTodoTail, injectDirMDTail, injectLSPDelta) append
// user-role rows AFTER the ask, so a last-user-message scan would miss the very
// ask it is looking for.
//
// The predicate is tool.UnansweredAsk, the same one the server uses to decide
// what to render, so "the advisor is waiting" and "a dialog is on screen" cannot
// drift apart.
//
// IMPORTANT: this scan is only sound when the ask SENTINEL is still in the slice
// the agent was handed. The TUI strips PERMISSION_ASK / QUESTION_PROMPT rows
// before calling Step (buildAgentMessagesSnapshot), so there this scan sees
// nothing and the host MUST say so explicitly — see SetPendingAsk and
// pendingAskSignal.
func messagesHavePendingAsk(msgs []Message) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return false
		}
		if tool.UnansweredAsk(msgs[i].Content) {
			return true
		}
	}
	return false
}

// pendingAskSignal is the host-declared "a dialog is on screen right now" flag.
//
// WHY IT EXISTS: the TUI removes the PERMISSION_ASK / QUESTION_PROMPT sentinel
// rows from the transcript BEFORE handing it to Step — deliberately, because a
// raw sentinel in the LLM input is a malformed tool-call/tool-result pairing
// that the model cannot recover from (see buildAgentMessagesSnapshot in
// internal/tui/model.go). That strip is correct for the LLM, but it means the
// agent's own transcript scan (messagesHavePendingAsk) can no longer see the
// ask, so the advisor checkpoint gate was INERT in the TUI: a plan/done
// checkpoint could start a second model underneath a permission dialog the user
// was still looking at.
//
// The host therefore declares the ask explicitly. The flag is advisory on top
// of the scan: the scan wins when it finds an ask (the server keeps the
// sentinel in as.messages), and the flag covers the hosts that strip it.
//
// It is a per-agent atomic rather than a package global so two sessions in the
// same process never affect each other.
type pendingAskSignal struct {
	pending atomic.Bool
}

// SetPendingAsk declares whether the host is currently showing an unanswered
// permission or question dialog for this agent. Pass true while a dialog is on
// screen and false once every ask in the round has been answered.
//
// The host MUST clear it: the advisor checkpoints treat a set flag as "stand
// down for this Step", and a stale true would mute the advisor for the rest of
// the session. The TUI sets it from anyAskPending() and clears it on every
// path that resolves the last outstanding ask.
//
// Calling this is optional. An agent whose transcript still carries the ask
// sentinel (the server) needs no call — the scan finds it on its own.
func (a *Agent) SetPendingAsk(pending bool) {
	a.pendingAskSignal.pending.Store(pending)
}

// PendingAsk reports the host-declared pending-ask state.
func (a *Agent) PendingAsk() bool {
	return a.pendingAskSignal.pending.Load()
}
