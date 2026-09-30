package tui

import (
	"fmt"
)

// User-message jump navigation (alt+up / alt+down).
//
// This is the TUI half of a two-surface feature: web/desktop has the same
// binding. It is deliberately NOT the same thing as the composer's plain
// up/down input-history walk (see the "up"/"down" cases in handleChatKey) —
// that one moves the caret through what you *typed*; this one moves the
// viewport through what you *sent*, across the whole transcript.
//
// Counting rule: a "user message" is isVisibleUserMessage — roleUser, not
// transient, and not a slash-command echo. Slash commands such as /theme are
// roleUser too but are chrome rather than conversation, so including them
// would make "msg 3/17" disagree with what the user perceives as their
// prompts. Reusing the existing predicate keeps the TUI's count, the session
// title generation, and the terminal window title in agreement.

// userMessageIndices returns the indices into m.messages of every countable
// user message, oldest first. Indices (not positions) are returned because
// the transcript render window can hide a prefix of m.messages; the indices
// stay valid regardless of what is currently rendered.
func userMessageIndices(msgs []message) []int {
	out := make([]int, 0, 8)
	for i := range msgs {
		if isVisibleUserMessage(msgs[i]) {
			out = append(out, i)
		}
	}
	return out
}

// userJumpIndicator is the status-bar label shown while a jump is active,
// e.g. "msg 3/17". Empty when no jump is in progress.
func (m *model) userJumpIndicator() string {
	if m.userJumpCursor < 0 || m.userJumpTotal <= 0 {
		return ""
	}
	return fmt.Sprintf("msg %d/%d", m.userJumpCursor+1, m.userJumpTotal)
}

// userJumpPrev walks to the previous (older) user message. Bound to alt+up.
func (m *model) userJumpPrev() { m.userJumpStep(-1) }

// userJumpNext walks to the next (newer) user message. Bound to alt+down.
func (m *model) userJumpNext() { m.userJumpStep(1) }

// userJumpStep moves the cursor by dir (-1 older, +1 newer) with clamping at
// both ends and NO wrap-around. Wrapping (what the ctrl+f find bar does) is
// disorienting when walking a document in one direction: alt+up past the
// oldest message teleports you to the newest, and the next alt+up appears to
// do nothing. Clamping makes the ends feel like ends.
//
// The seed: cursor == -1 means "not in the list yet", and the first press in
// EITHER direction enters at the newest message. The user is at the bottom of
// the conversation, so the newest user message is the nearest entry point;
// making both keys start there keeps them symmetric and means neither key is
// ever a silent no-op on first press.
func (m *model) userJumpStep(dir int) {
	list := userMessageIndices(m.messages)
	if len(list) == 0 {
		m.clearUserJump()
		return
	}
	cursor := m.userJumpCursor
	if cursor < 0 {
		cursor = len(list) - 1
	} else {
		cursor += dir
		if cursor < 0 {
			cursor = 0
		}
		if cursor > len(list)-1 {
			cursor = len(list) - 1
		}
	}
	// Pressing into an already-clamped end keeps the cursor put, but we still
	// refresh the indicator and flash so the key reads as received rather than
	// silently dead.
	m.userJumpCursor = cursor
	m.userJumpTotal = len(list)
	m.flashAndScrollToMessage(list[cursor])
}

// clearUserJump drops the jump cursor and its status label. Called when the
// context changes underneath it (new message sent, tab or session switch) so
// the label can never describe a position that no longer exists.
func (m *model) clearUserJump() {
	m.userJumpCursor = -1
	m.userJumpTotal = 0
}

// Note on flash expiry: the highlight itself is owned by
// chatSearchFlashMsg and expires via the existing chatSearchFlashTick /
// chatSearchFlashExpiredMsg pair, which jumpToChatMatch already arms. The key
// handler therefore returns chatSearchFlashTick() alongside the jump rather
// than introducing a second expiry message type for the same highlight.
//
// The cursor/indicator deliberately outlives that 1.2s: it is a position
// readout for a walk in progress, and is cleared by clearUserJump on typing,
// sending, or a tab/session switch — not by the highlight expiring.
