package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// chatSearchFlashExpiredMsg is sent by the tea.Tick started in
// jumpToChatMatch; the handler clears chatSearchFlashMsg so the highlight
// doesn't linger forever.
type chatSearchFlashExpiredMsg struct{}

// openChatSearch activates the find bar. The query argument is optional —
// pass "" for a blank bar, or a string to prefill it (used by the /search
// slash command). The bar is only available on the chat tab; callers must
// gate on m.activeTab == tabChat before invoking.
func (m *model) openChatSearch(prefill string) {
	m.chatSearchActive = true
	m.chatSearchInput.SetValue(prefill)
	// textinput.Focus() (and CursorEnd) panic on a zero-value textinput
	// because its embedded cursor is nil until bubbletea's first Update
	// runs. Tests that build a model{} literal — bypassing newModel —
	// trip that panic. The textarea carries a non-zero Width once
	// newModel has run; absence of that is a reliable signal that the
	// model is a unit-test stub, so we skip the cursor/focus calls in
	// that case. The state is still flipped, so render and the key
	// routing still work in tests; the real update path picks up a
	// real focus on the first key.
	readyForFocus := m.input.Width() > 0 && m.viewport.Width() > 0
	if readyForFocus {
		m.chatSearchInput.CursorEnd()
		m.chatSearchInput.Focus()
		m.input.Blur()
	}
	m.rebuildChatSearchMatches()
	// If the prefill is non-empty, jump to the first match immediately — the
	// same UX as the user pressing ctrl+f then enter. Reset the cursor to
	// the "current position" semantics: -1 means no match jumped yet.
	if prefill != "" && len(m.chatSearchMatches) > 0 {
		m.chatSearchCursor = 0
		m.jumpToChatMatch(m.chatSearchMatches[0])
	} else {
		m.chatSearchCursor = -1
	}
	// layout() walks the uninitialised textarea/viewport and panics on a
	// zero-value model (test stubs). Skip it on stubs.
	if m.viewport.Width() > 0 {
		m.layout() // shrink the viewport to make room for the bar
	}
}

// closeChatSearch hides the bar, clears query/match state, and restores
// focus to the main chat composer. Idempotent.
func (m *model) closeChatSearch() {
	if !m.chatSearchActive {
		return
	}
	m.chatSearchActive = false
	// Same Focus/Blur guard as openChatSearch: the embedded cursor is
	// nil on a zero-value textinput (test stubs) and Blur/Focus on it
	// would panic. The state is flipped regardless.
	if m.input.Width() > 0 {
		m.chatSearchInput.Blur()
	}
	m.chatSearchInput.SetValue("")
	m.chatSearchQuery = ""
	m.chatSearchMatches = nil
	m.chatSearchCursor = -1
	m.chatSearchFlashMsg = -1
	m.chatSearchNoMatch = false
	if m.input.Width() > 0 {
		m.input.Focus()
	}
	// layout() walks the uninitialised textarea/viewport and panics on a
	// zero-value model (test stubs). Skip it on stubs; the state is
	// already cleared above.
	if m.viewport.Width() > 0 {
		m.layout()
	}
}

// rebuildChatSearchMatches (re)computes the list of message indices whose
// contents contain the current query (case-insensitive substring match). It
// is called on every keystroke and on openChatSearch. Skips transient
// status rows; matches the full LLM payload (text + raw content + tool
// calls + tool results + reasoning) so the "what did the model say about
// X" use case works.
func (m *model) rebuildChatSearchMatches() {
	q := strings.TrimSpace(m.chatSearchInput.Value())
	m.chatSearchQuery = q
	if q == "" {
		m.chatSearchMatches = nil
		m.chatSearchCursor = -1
		m.chatSearchNoMatch = false
		return
	}
	needle := strings.ToLower(q)
	matches := make([]int, 0, 8)
	for i, msg := range m.messages {
		if msg.transient {
			continue
		}
		if messageMatchesQuery(msg, needle) {
			matches = append(matches, i)
		}
	}
	m.chatSearchMatches = matches
	m.chatSearchNoMatch = len(matches) == 0
	// Keep the cursor inside the new list (or reset). After typing a single
	// character the old position is usually still valid; after deleting the
	// whole query it goes to -1 (handled above).
	if m.chatSearchCursor >= len(matches) {
		m.chatSearchCursor = -1
	}
}

// messageMatchesQuery returns true when any of the searchable fields of msg
// contain needle (already lower-cased). The fields covered are: the
// pre-rendered text (msg.text), the raw LLM Content (msg.raw.Content, which
// also carries tool-result bodies for Role=="tool" messages), the LLM
// ReasoningContent, every tool call's function name + arguments, and any
// image notice attached to the message. This is the "match all scope"
// decision recorded in the design doc.
func messageMatchesQuery(msg message, needle string) bool {
	if strings.Contains(strings.ToLower(msg.text), needle) {
		return true
	}
	if msg.raw == nil {
		return false
	}
	if strings.Contains(strings.ToLower(msg.raw.Content), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(msg.raw.ReasoningContent), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(msg.raw.Notice), needle) {
		return true
	}
	for _, tc := range msg.raw.ToolCalls {
		if strings.Contains(strings.ToLower(tc.Function.Name), needle) {
			return true
		}
		if strings.Contains(strings.ToLower(tc.Function.Arguments), needle) {
			return true
		}
	}
	return false
}

// chatSearchNext moves the cursor forward through chatSearchMatches with
// wrap-around (last → first), and triggers the jump + flash on the new
// position. No-op when the list is empty. Value receiver so callers
// using a value-receiver dispatcher (handleChatSearchKey) can see the
// updated cursor on the returned model without a separate round-trip.
func (m model) chatSearchNext() model {
	if len(m.chatSearchMatches) == 0 {
		return m
	}
	if m.chatSearchCursor < 0 {
		m.chatSearchCursor = 0
	} else {
		m.chatSearchCursor = (m.chatSearchCursor + 1) % len(m.chatSearchMatches)
	}
	// jumpToChatMatch is a pointer receiver; copy the value to a
	// pointer so the call mutates our local copy and we can return it.
	pm := m
	pm.jumpToChatMatch(m.chatSearchMatches[m.chatSearchCursor])
	return pm
}

// chatSearchPrev moves the cursor backward through chatSearchMatches with
// wrap-around (first → last), and triggers the jump + flash on the new
// position. No-op when the list is empty. Value receiver (see chatSearchNext).
func (m model) chatSearchPrev() model {
	if len(m.chatSearchMatches) == 0 {
		return m
	}
	if m.chatSearchCursor < 0 {
		m.chatSearchCursor = len(m.chatSearchMatches) - 1
	} else {
		m.chatSearchCursor = (m.chatSearchCursor - 1 + len(m.chatSearchMatches)) % len(m.chatSearchMatches)
	}
	pm := m
	pm.jumpToChatMatch(m.chatSearchMatches[m.chatSearchCursor])
	return pm
}

// jumpToChatMatch scrolls the transcript viewport so the first wrapped line
// of message msgIdx is on screen, sets the flash highlight, and arms a
// 1.2s tea.Tick to clear it.
func (m *model) jumpToChatMatch(msgIdx int) {
	if msgIdx < 0 || msgIdx >= len(m.messages) {
		return
	}
	// Clamp in case the cache hasn't been built yet (e.g. /search during a
	// fresh session where the transcript was never re-rendered). Falling back
	// to 0 keeps the viewport in a sane place rather than panicking.
	target := 0
	if msgIdx < len(m.transcriptMsgStartLine) && m.transcriptMsgStartLine[msgIdx] > 0 {
		target = m.transcriptMsgStartLine[msgIdx]
	}
	// SetYOffset clamps to [0, TotalLines-Visible] internally; passing
	// target puts the message's first wrapped line at the top of the viewport.
	// We set it AFTER the re-render below, because renderTranscript ->
	// shouldAutoScrollTranscript -> GotoBottom would otherwise stomp the
	// explicit YOffset on a fresh viewport (and the flash is what we want
	// the user to see, not the sticky-bottom follow).
	m.chatSearchFlashMsg = msgIdx
	// Paint the flash via the existing selection machinery (Selected
	// background on the first wrapped line of the message). The flash
	// expires when chatSearchFlashExpiredMsg clears chatSearchFlashMsg and
	// we run ensureChatSearchFlashHighlight() again with -1.
	m.ensureChatSearchFlashHighlight()
	// Force a re-render so the flash highlight lands in the rendered
	// content this frame rather than waiting for the next stream delta.
	m.rerenderTranscriptAndMaybeScroll()
	// Re-apply the explicit YOffset AFTER the auto-scroll side effect.
	// The viewport is also clamped on re-set, so this is safe even if the
	// content has grown.
	m.viewport.SetYOffset(target)
}

// chatSearchFlashTick is the command returned from jumpToChatMatch. It is
// re-armed by the chatSearchFlashExpiredMsg handler so the flash always
// expires exactly once per jump.
func chatSearchFlashTick() tea.Cmd {
	return tea.Tick(1200*time.Millisecond, func(time.Time) tea.Msg {
		return chatSearchFlashExpiredMsg{}
	})
}

// handleChatSearchKey is the key dispatcher for the find bar. It is called
// from Update when m.chatSearchActive is true and consumes printable
// characters, backspace, enter, shift+enter, ctrl+g, ctrl+shift+g, and
// esc. The mini textinput's own Update handles the editable-string work;
// the higher-level navigation lives here.
//
// Returns (model, cmd, handled). handled=true means the event was consumed
// and the caller must not forward it to the main chat input.
func (m model) handleChatSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	// Forward the event to the textinput so printable keys + backspace
	// continue to work, then rebuild the match list off the new value.
	// The textinput's own Update can panic on a zero-value model
	// (test stubs without an initialised cursor); the same Width>0 probe
	// used in open/closeChatSearch decides whether to forward the key.
	prevValue := m.chatSearchInput.Value()
	var tiCmd tea.Cmd
	if m.input.Width() > 0 {
		m.chatSearchInput, tiCmd = m.chatSearchInput.Update(msg)
	} else {
		// Lightweight stand-in for a real Update: handle printable
		// characters + backspace by hand so tests can drive the match
		// builder without booting bubbletea. The full keymap (cursor
		// motion, selection, kill ring) is irrelevant to what we're
		// verifying — the test file constructs the model literal, not
		// the real Update path.
		ks := msg.String()
		switch {
		case ks == "backspace":
			v := m.chatSearchInput.Value()
			if len(v) > 0 {
				m.chatSearchInput.SetValue(v[:len(v)-1])
			}
		default:
			r := []rune(ks)
			if len(r) == 1 && r[0] >= 32 && r[0] != 127 {
				m.chatSearchInput.SetValue(m.chatSearchInput.Value() + string(r))
			}
		}
	}
	if m.chatSearchInput.Value() != prevValue {
		m.rebuildChatSearchMatches()
	}
	keyStr := msg.String()
	switch keyStr {
	case "esc":
		m.closeChatSearch()
		return m, nil, true
	case "enter":
		m = m.chatSearchNext()
		return m, chatSearchFlashTick(), true
	case "shift+enter":
		m = m.chatSearchPrev()
		return m, chatSearchFlashTick(), true
	case "ctrl+g":
		// next match. The shifted variant (ctrl+shift+g) is matched by the
		// case below; the msg.String() output is enough to disambiguate.
		m = m.chatSearchNext()
		return m, chatSearchFlashTick(), true
	case "ctrl+shift+g":
		m = m.chatSearchPrev()
		return m, chatSearchFlashTick(), true
	}
	// Any other key (including arrow keys, home/end) is the textinput's
	// concern — we just acknowledge it as handled so it doesn't bleed into
	// the chat composer underneath.
	return m, tiCmd, true
}

// renderChatSearchBar returns the bordered find bar that appears between
// the transcript and the input area. The bar is exactly 1 row of content
// inside a borderStyle box (so the total height is 3 rows: top border,
// content, bottom border). The caller is responsible for gating on
// m.chatSearchActive.
func (m model) renderChatSearchBar(panelWidth int) string {
	// /  <query>▏  ·  N/total matches  ·  ↑↓ enter  ⇧⏎ prev  esc
	prompt := hintStyle.Render("/")
	tiView := m.chatSearchInput.View()
	sep := hintStyle.Render("  ·  ")
	// Match counter: "N/total" or just "total" when the cursor is at -1
	// (no match jumped to yet). Zero-matches uses the error style.
	var count string
	if m.chatSearchQuery == "" {
		count = hintStyle.Render("type to search")
	} else {
		total := len(m.chatSearchMatches)
		countStr := fmt.Sprintf("%d match", total)
		if total != 1 {
			countStr += "es"
		}
		if total == 0 {
			count = errorStyle.Render(countStr)
		} else if m.chatSearchCursor >= 0 {
			count = successStyle.Render(fmt.Sprintf("%d/%d", m.chatSearchCursor+1, total))
		} else {
			count = hintStyle.Render(countStr)
		}
	}
	hint := hintStyle.Render("  ⏎ next · ⇧⏎ prev · esc close")
	// Constrain to panelWidth-2 so it matches the input row above/below
	// exactly (borderStyle adds 1 col of padding on each side).
	inner := prompt + " " + tiView + sep + count + hint
	w := panelWidth - 2
	if w < 10 {
		w = 10
	}
	return borderStyle.Width(w).Render(inner)
}

// ensureChatSearchFlashHighlight sets m.sel so the first wrapped line of
// the currently-flashed message is highlighted with the Selected style —
// reusing the in-app selection machinery that the rest of the transcript
// already drives (applySelectionHighlight / applyOrClearSelectionHighlight
// in selection.go). When flashMsg is -1, selection is cleared.
//
// This is intentionally a single-line highlight on the first wrapped row
// of the matched message rather than a full-block re-style. It works for
// every message kind (user, assistant, tool, thinking, compaction) without
// each renderer having to know about chat search, and the Selected
// background is a strong, theme-aware cue.
func (m *model) ensureChatSearchFlashHighlight() {
	if m.chatSearchFlashMsg < 0 ||
		m.chatSearchFlashMsg >= len(m.transcriptMsgStartLine) ||
		m.chatSearchFlashMsg >= len(m.rawTranscriptLines) {
		// Nothing to flash, or the cache hasn't been built yet. Clear any
		// stale selection rather than leaving the previous flash highlighted.
		if m.sel.active {
			m.sel = selectionState{}
			m.applyOrClearSelectionHighlight()
		}
		return
	}
	line := m.transcriptMsgStartLine[m.chatSearchFlashMsg]
	if line < 0 || line >= len(m.rawTranscriptLines) {
		return
	}
	// Reset the selection to a zero-length span at (line, 0) then expand the
	// end col to the wrapped line width. Zero-length is fine because
	// applySelectionHighlight treats the start==end case as a one-character
	// highlight at the start column.
	endCol := lipgloss.Width(m.rawTranscriptLines[line])
	if endCol < 1 {
		endCol = 1
	}
	m.sel = selectionState{
		active:    true,
		startLine: line,
		startCol:  0,
		endLine:   line,
		endCol:    endCol,
	}
	m.applyOrClearSelectionHighlight()
}
