package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// buildChatSearchTestModel is a small helper that constructs a chat-tab
// model with a controlled messages list. It does not run newModel —
// chat_search_test exercises state transitions, not bubbletea wiring.
// A real textarea is still installed so layout() can call SetWidth on
// it without panicking; the Focus/Blur-unsafe paths in openChatSearch
// are skipped because m.input.Width() returns 0 until layout runs.
// buildChatSearchTestModel is a small helper that constructs a chat-tab
// model with a controlled messages list. It does not run newModel —
// chat_search_test exercises state transitions, not bubbletea wiring.
// A real textarea, fastviewport, and textinput are all installed so the
// chat-search code paths that call Focus/SetValue/SetYOffset on them
// can run without nil-pointer panics.
func buildChatSearchTestModel(msgs []message) model {
	vp := fastviewport.New(80, 20)
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 200
	ta := newTestTextarea()
	// The fastviewport helper from the existing tests uses 80×20 by
	// default; give the chat input some width too so layout() is safe.
	ta.SetWidth(40)
	m := model{
		ready:              true,
		width:              120,
		height:             40,
		activeTab:          tabChat,
		messages:           msgs,
		transcriptLines:    []string{""},
		rawTranscriptLines: []string{""},
		input:              ta,
		viewport:           vp,
		chatSearchInput:    ti,
	}
	// initialise the start-line table so jumpToChatMatch is safe; tests
	// that need a non-trivial table build their own.
	m.transcriptMsgStartLine = make([]int, len(msgs))
	return m
}

// TestChatSearchMatchesSubstring verifies the match builder's case + scope
// behaviour: case-insensitive, matches text, raw content, reasoning, and
// tool-call args, and skips transient rows. This is the "match all scope"
// decision from the design doc.
func TestChatSearchMatchesSubstring(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "Build the docker image"},
		{role: roleAssistant, text: "Sure, kicking off the build."},
		{role: roleAssistant, text: "(thinking) ...", transient: true},
		{role: roleAssistant, text: "Build finished.", raw: &agent.Message{
			Role:            "assistant",
			Content:         "nope",
			ReasoningContent: "the build is the part to watch",
		}},
		{role: roleAssistant, text: "Tool result for build:", raw: &agent.Message{
			Role:    "assistant",
			Content: "no match here",
			ToolCalls: []agent.ToolCall{{
				ID: "1", Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "BASH", Arguments: `{"cmd":"build_docker"}`},
			}},
		}},
	}
	m := buildChatSearchTestModel(msgs)

	// "build" should hit indexes 0, 1, 3, 4 — not 2 (transient).
	m.chatSearchInput.SetValue("build")
	m.rebuildChatSearchMatches()
	got := m.chatSearchMatches
	want := []int{0, 1, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("expected %d matches, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("match[%d] = %d, want %d", i, got[i], want[i])
		}
	}

	// Case-insensitive: "BUILD" should yield the same list.
	m.chatSearchInput.SetValue("BUILD")
	m.rebuildChatSearchMatches()
	if len(m.chatSearchMatches) != len(want) {
		t.Fatalf("expected case-insensitive match, got %d matches", len(m.chatSearchMatches))
	}

	// Empty query → no matches, cursor reset.
	m.chatSearchInput.SetValue("")
	m.rebuildChatSearchMatches()
	if len(m.chatSearchMatches) != 0 {
		t.Fatalf("expected no matches for empty query, got %v", m.chatSearchMatches)
	}
	if m.chatSearchCursor != -1 {
		t.Fatalf("expected cursor reset to -1, got %d", m.chatSearchCursor)
	}

	// Whitespace-only query behaves the same.
	m.chatSearchInput.SetValue("   ")
	m.rebuildChatSearchMatches()
	if len(m.chatSearchMatches) != 0 {
		t.Fatalf("expected no matches for whitespace query, got %v", m.chatSearchMatches)
	}
}

// TestChatSearchNextWrapsAround exercises the wrap-around semantics: next
// after the last match lands on the first, prev before the first lands on
// the last, and starting from cursor=-1 enters at 0 / len-1 respectively.
func TestChatSearchNextWrapsAround(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "alpha"},
		{role: roleAssistant, text: "beta"},
		{role: roleUser, text: "gamma alpha"},
		{role: roleAssistant, text: "delta"},
	}
	m := buildChatSearchTestModel(msgs)
	m.transcriptMsgStartLine = []int{0, 1, 2, 3}

	// 3 messages contain "alpha": 0 and 2.
	m.chatSearchInput.SetValue("alpha")
	m.rebuildChatSearchMatches()
	if len(m.chatSearchMatches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(m.chatSearchMatches))
	}

	// Starting from -1 → next lands at 0.
	m.chatSearchCursor = -1
	m = m.chatSearchNext()
	if m.chatSearchCursor != 0 {
		t.Fatalf("next from -1 should land on 0, got %d", m.chatSearchCursor)
	}
	if m.chatSearchFlashMsg != m.chatSearchMatches[0] {
		t.Fatalf("next from -1 should flash match 0 (%d), got %d", m.chatSearchMatches[0], m.chatSearchFlashMsg)
	}

	// Forward two more times: 0 → 1, then 1 wraps to 0.
	m = m.chatSearchNext()
	if m.chatSearchCursor != 1 {
		t.Fatalf("second next should land on 1, got %d", m.chatSearchCursor)
	}
	m = m.chatSearchNext()
	if m.chatSearchCursor != 0 {
		t.Fatalf("next after last should wrap to 0, got %d", m.chatSearchCursor)
	}

	// prev from 0 wraps to 1.
	m.chatSearchCursor = 0
	m = m.chatSearchPrev()
	if m.chatSearchCursor != 1 {
		t.Fatalf("prev from 0 should wrap to last, got %d", m.chatSearchCursor)
	}
}

// TestChatSearchJumpScrollsViewport confirms that jumpToChatMatch uses
// the transcriptMsgStartLine cache to position the viewport's YOffset
// exactly at the matched message's first wrapped row.
func TestChatSearchJumpScrollsViewport(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "first"},
		{role: roleAssistant, text: "second"},
		{role: roleUser, text: "third contains needle"},
		{role: roleAssistant, text: "fourth"},
	}
	m := buildChatSearchTestModel(msgs)
	// Pretend the wrapped transcript placed each message on its own line,
	// so jumping to message 2 must scroll to row 2.
	m.transcriptMsgStartLine = []int{0, 1, 2, 3}

	// We need a viewport with a usable SetYOffset — fastviewport exposes
	// that. The viewport height must be smaller than the content length
	// (4 lines) for SetYOffset(2) to be a meaningful position. Build a
	// fresh one (2-row height), feed it 4 lines so its maxYOffset is 2,
	// and stash it on the model.
	vp := fastviewport.New(80, 2)
	vp.SetContentLines([]string{"first-line", "second-line", "third-line", "fourth-line"})
	m.viewport = vp
	vp.SetYOffset(99)
	m.transcriptLines = []string{"first-line", "second-line", "third-line", "fourth-line"}
	m.rawTranscriptLines = []string{"first-line", "second-line", "third-line", "fourth-line"}

	m.jumpToChatMatch(2)
	if got := m.viewport.YOffset(); got != 2 {
		t.Fatalf("jumpToChatMatch(2) should set YOffset=2, got %d", got)
	}
	if m.chatSearchFlashMsg != 2 {
		t.Fatalf("expected flash on message 2, got %d", m.chatSearchFlashMsg)
	}

	// Out-of-range is a no-op (no panic, no scroll).
	m.viewport.SetYOffset(0)
	m.jumpToChatMatch(99)
	if got := m.viewport.YOffset(); got != 0 {
		t.Fatalf("out-of-range jump should not scroll, got YOffset=%d", got)
	}
}

// TestChatSearchEscCloses verifies esc closes the bar and clears all
// search state. handleChatSearchKey is a value-receiver method, so we
// look at the returned model for the post-close state, not the local
// receiver.
func TestChatSearchEscCloses(t *testing.T) {
	m := buildChatSearchTestModel([]message{{role: roleUser, text: "needle"}})
	m.chatSearchActive = true
	m.chatSearchInput.SetValue("needle")
	m.chatSearchMatches = []int{0}
	m.chatSearchCursor = 0
	m.chatSearchFlashMsg = 0
	m.chatSearchNoMatch = false

	out, _, handled := m.handleChatSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled {
		t.Fatal("esc should be handled while bar is open")
	}
	got := derefTestModel(t, out)
	if got.chatSearchActive {
		t.Fatal("esc should close the bar")
	}
	if got.chatSearchQuery != "" {
		t.Fatalf("esc should clear the query, got %q", got.chatSearchQuery)
	}
	if len(got.chatSearchMatches) != 0 {
		t.Fatalf("esc should clear matches, got %v", got.chatSearchMatches)
	}
	if got.chatSearchCursor != -1 {
		t.Fatalf("esc should reset cursor to -1, got %d", got.chatSearchCursor)
	}
	if got.chatSearchFlashMsg != -1 {
		t.Fatalf("esc should clear flash, got %d", got.chatSearchFlashMsg)
	}
}

// TestChatSearchEnterGoesToNextMatch ensures enter triggers chatSearchNext
// (the "go" action), even when no printable character was typed.
func TestChatSearchEnterGoesToNextMatch(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "alpha"},
		{role: roleAssistant, text: "beta"},
		{role: roleUser, text: "alpha again"},
	}
	m := buildChatSearchTestModel(msgs)
	m.transcriptMsgStartLine = []int{0, 1, 2}
	// 2-row viewport so SetYOffset clamps to [0, 1] and "alpha again" is
	// reachable off-screen — the test only cares that YOffset is set.
	m.viewport = fastviewport.New(80, 2)
	m.transcriptLines = []string{"alpha", "beta", "alpha again"}
	m.rawTranscriptLines = []string{"alpha", "beta", "alpha again"}

	m.openChatSearch("alpha")
	if len(m.chatSearchMatches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(m.chatSearchMatches))
	}
	if m.chatSearchCursor != 0 {
		t.Fatalf("openChatSearch with prefill should land on 0, got %d", m.chatSearchCursor)
	}

	// Enter should advance to 1. handleChatSearchKey is a value receiver,
	// so the post-call state lives on the returned model, not the local
	// receiver.
	out, cmd, handled := m.handleChatSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter should be handled while bar is open")
	}
	if cmd == nil {
		t.Fatal("enter should arm a flash-expiration tick")
	}
	got := derefTestModel(t, out)
	if got.chatSearchCursor != 1 {
		t.Fatalf("enter should advance to 1, got %d", got.chatSearchCursor)
	}
}

// TestChatSearchPrefillViaSlashCommand exercises the /search command
// handler: with a prefill it must open the bar and jump to the first
// match immediately.
func TestChatSearchPrefillViaSlashCommand(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "where is the API key?"},
		{role: roleAssistant, text: "It lives in the auth.json file."},
	}
	m := buildChatSearchTestModel(msgs)
	m.transcriptMsgStartLine = []int{0, 1}
	m.viewport = fastviewport.New(80, 2)
	m.transcriptLines = []string{"where is the API key?", "It lives in the auth.json file."}
	m.rawTranscriptLines = m.transcriptLines

	runSearchCmd(&m, []string{"api"})
	if !m.chatSearchActive {
		t.Fatal("/search should activate the bar")
	}
	if m.chatSearchInput.Value() != "api" {
		t.Fatalf("expected bar to be prefilled with %q, got %q", "api", m.chatSearchInput.Value())
	}
	if len(m.chatSearchMatches) != 1 || m.chatSearchMatches[0] != 0 {
		t.Fatalf("expected to match message 0, got %v", m.chatSearchMatches)
	}
	if m.chatSearchFlashMsg != 0 {
		t.Fatalf("expected flash on message 0, got %d", m.chatSearchFlashMsg)
	}
}

// TestChatSearchCloseOnTabSwitch verifies the auto-close behaviour: when
// the user navigates away from the chat tab, the bar closes and the
// query is cleared.
func TestChatSearchCloseOnTabSwitch(t *testing.T) {
	m := buildChatSearchTestModel([]message{{role: roleUser, text: "x"}})
	m.chatSearchActive = true
	m.chatSearchInput.SetValue("hello")
	m.chatSearchMatches = []int{0}
	m.chatSearchCursor = 0

	// Simulate alt+] (next tab). The handler mutates activeTab, then we
	// call the close hook the same way handleGlobalTabKeys does.
	m.activeTab = tabFiles
	m.closeChatSearchIfLeavingChat()
	if m.chatSearchActive {
		t.Fatal("bar should close when leaving chat tab")
	}
	if m.chatSearchInput.Value() != "" {
		t.Fatalf("query should be cleared, got %q", m.chatSearchInput.Value())
	}
	if len(m.chatSearchMatches) != 0 {
		t.Fatalf("matches should be cleared, got %v", m.chatSearchMatches)
	}
}

// TestChatSearchRenderBarIncludesCount is a render smoke test: the bar
// must contain the match count string. A full golden render test is
// overkill for this feature — if the count disappears, the user notices
// in one second.
func TestChatSearchRenderBarIncludesCount(t *testing.T) {
	m := buildChatSearchTestModel([]message{{role: roleUser, text: "needle"}})
	m.styles = ApplyThemeColors("tokyonight")
	m.chatSearchActive = true
	m.chatSearchInput.SetValue("needle")
	// Drive the match builder so chatSearchQuery + chatSearchMatches
	// reflect the typed value, exactly the way the live key handler does.
	m.rebuildChatSearchMatches()
	if len(m.chatSearchMatches) != 1 {
		t.Fatalf("test setup: expected 1 match for %q, got %d", "needle", len(m.chatSearchMatches))
	}
	m.chatSearchCursor = 0
	view := m.renderChatSearchBar(78)
	if !strings.Contains(view, "1/1") {
		t.Fatalf("bar should include 1/1 match count, got %q", view)
	}
}
