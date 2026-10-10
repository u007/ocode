package agent

import "github.com/u007/ocode/internal/tool"

// messagesHavePendingAsk reports whether msgs ends with a permission or question
// dialog the user has not answered — i.e. whether the client is showing an ask
// right now. It is the gate the advisor checkpoints use so a second model never
// starts underneath a dialog that is waiting on a human.
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
