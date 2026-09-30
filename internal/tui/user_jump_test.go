package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// buildUserJumpTestModel returns a model wired the way handleChatKeys needs
// (real textarea + real fastviewport).
func buildUserJumpTestModel(msgs []message) model {
	ta := newTestTextarea()
	ta.SetWidth(40)
	vp := fastviewport.New(80, 2)
	const contentLines = 200
	lines := make([]string, contentLines)
	for i := range lines {
		lines[i] = "line"
	}
	vp.SetContentLines(lines)
	return model{
		ready:              true,
		width:              120,
		height:             40,
		activeTab:          tabChat,
		messages:           msgs,
		transcriptLines:    lines,
		rawTranscriptLines: lines,
		// -1 for every message means "not currently rendered", which makes
		// flashAndScrollToMessage's target fall back to 0 instead of jumping
		// to a stale preset offset. The preset would be thrown away anyway:
		// the first jump calls rerenderTranscriptAndMaybeScroll, and the
		// real renderTranscript recomputes this whole table from the actual
		// markdown-wrapped lines. So the table is deliberately NOT asserted
		// on across jumps — see assertJumpedTo.
		transcriptMsgStartLine: make([]int, len(msgs)),
		userJumpCursor:         -1,
		input:                  ta,
		viewport:               vp,
	}
}

// assertJumpedTo checks that the jump resolved to message index wantMsgIdx.
//
// The assertion target is chatSearchFlashMsg, not viewport.YOffset: a jump
// re-renders the transcript (flashAndScrollToMessage calls
// rerenderTranscriptAndMaybeScroll so the highlight lands this frame), and
// that re-render legitimately rewrites both the content and the scroll
// position. YOffset is therefore only meaningful on a freshly built fixture,
// which is what TestUserJumpScrollsViewportOnFirstJump covers. The flash
// target is the stable statement of "which message did this jump choose".
func assertJumpedTo(t *testing.T, m model, wantMsgIdx int) {
	t.Helper()
	if m.chatSearchFlashMsg != wantMsgIdx {
		t.Fatalf("jump targeted message %d, want %d", m.chatSearchFlashMsg, wantMsgIdx)
	}
}

func TestUserMessageIndicesSkipsSlashCommandsAndTransient(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "real one"},
		{role: roleAssistant, text: "reply"},
		{role: roleUser, text: "/theme", skipLLM: true},
		{role: roleUser, text: "transient notice", transient: true},
		{role: roleUser, text: "real two"},
	}
	got := userMessageIndices(msgs)
	want := []int{0, 4}
	if len(got) != len(want) {
		t.Fatalf("userMessageIndices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("userMessageIndices[%d] = %d, want %d (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestUserJumpScrollsViewportOnFirstJump(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "one"},
		{role: roleAssistant, text: "r1"},
		{role: roleUser, text: "two"},
		{role: roleAssistant, text: "r2"},
		{role: roleUser, text: "three"},
		{role: roleAssistant, text: "r3"},
	}
	m := buildUserJumpTestModel(msgs)
	m.viewport.SetYOffset(0)
	// Pre-render so transcriptMsgStartLine holds realistic values, the way a
	// real session does long before the user reaches for alt+up.
	//
	// This step is load-bearing for the assertion below, and the reason is a
	// known limitation of the shared flashAndScrollToMessage core: it reads
	// transcriptMsgStartLine BEFORE calling rerenderTranscriptAndMaybeScroll.
	// With a never-rendered (all-zero) table the target degrades to 0 — the
	// documented "cache hasn't been built yet" fallback — and the viewport
	// lands at the top instead of the message. In production the table is
	// always populated by the preceding frames, so the fallback is only
	// reachable in a fresh session or a unit test.
	m.rerenderTranscriptAndMaybeScroll()

	m.userJumpPrev()
	// The newest user message is message index 4. flashAndScrollToMessage
	// re-applies SetYOffset(transcriptMsgStartLine[msgIdx]) AFTER the
	// re-render, so the contract is that the final offset equals the final
	// table entry for the flashed message.
	if m.chatSearchFlashMsg != 4 {
		t.Fatalf("first jump should flash message 4, got %d", m.chatSearchFlashMsg)
	}
	if got, want := m.viewport.YOffset(), m.transcriptMsgStartLine[4]; got != want {
		t.Fatalf("first jump should leave YOffset at message 4's start line %d, got %d", want, got)
	}
	if m.viewport.YOffset() == 0 {
		t.Fatalf("first jump should have scrolled away from the top, YOffset is still 0")
	}
}

func TestUserJumpPrevSeedsAtNewestThenWalksBack(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "one"},
		{role: roleAssistant, text: "r1"},
		{role: roleUser, text: "two"},
		{role: roleAssistant, text: "r2"},
		{role: roleUser, text: "three"},
		{role: roleAssistant, text: "r3"},
	}
	m := buildUserJumpTestModel(msgs)

	// First alt+up from the bottom lands on the MOST RECENT user message
	// (index 4), not the oldest.
	m.userJumpPrev()
	if m.userJumpCursor != 2 {
		t.Fatalf("first prev should seed at the newest user message (cursor 2), got %d", m.userJumpCursor)
	}
	assertJumpedTo(t, m, 4)
	if m.userJumpTotal != 3 {
		t.Fatalf("expected total 3 user messages, got %d", m.userJumpTotal)
	}
	if got := m.userJumpIndicator(); got != "msg 3/3" {
		t.Fatalf("indicator = %q, want %q", got, "msg 3/3")
	}

	m.userJumpPrev()
	if m.userJumpCursor != 1 {
		t.Fatalf("second prev should reach cursor 1, got %d", m.userJumpCursor)
	}
	assertJumpedTo(t, m, 2)
	if got := m.userJumpIndicator(); got != "msg 2/3" {
		t.Fatalf("indicator = %q, want %q", got, "msg 2/3")
	}

	m.userJumpPrev()
	if m.userJumpCursor != 0 {
		t.Fatalf("third prev should reach cursor 0, got %d", m.userJumpCursor)
	}
	assertJumpedTo(t, m, 0)
	if got := m.userJumpIndicator(); got != "msg 1/3" {
		t.Fatalf("indicator = %q, want %q", got, "msg 1/3")
	}
}

func TestUserJumpClampsAtOldestWithoutWrapping(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "one"},
		{role: roleAssistant, text: "r1"},
		{role: roleUser, text: "two"},
		{role: roleAssistant, text: "r2"},
	}
	m := buildUserJumpTestModel(msgs)
	m.userJumpPrev()
	m.userJumpPrev()
	m.userJumpPrev() // already at the oldest; must clamp, NOT wrap to the newest
	if m.userJumpCursor != 0 {
		t.Fatalf("prev at the oldest must clamp at cursor 0, got %d (wrapping would have put it at %d)", m.userJumpCursor, len(msgs)-1)
	}
	assertJumpedTo(t, m, 0)
}

func TestUserJumpNextFromBottomEntersAtTheNewestMessage(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "one"},
		{role: roleAssistant, text: "r1"},
		{role: roleUser, text: "two"},
		{role: roleAssistant, text: "r2"},
	}
	m := buildUserJumpTestModel(msgs)

	// The first press of EITHER key enters the list at the newest message —
	// the nearest entry point from the bottom. alt+down must therefore land on
	// the newest user message, not the oldest, and must not be a silent no-op
	// that leaves the user with no feedback that the key registered.
	m.userJumpNext()
	if m.userJumpCursor != 1 {
		t.Fatalf("first next should enter at the newest user message (cursor 1), got %d", m.userJumpCursor)
	}
	assertJumpedTo(t, m, 2)
	if got := m.userJumpIndicator(); got != "msg 2/2" {
		t.Fatalf("indicator = %q, want %q", got, "msg 2/2")
	}

	// A second alt+down is already at the newest and must clamp, not wrap.
	m.userJumpNext()
	if m.userJumpCursor != 1 {
		t.Fatalf("next at the newest must clamp at cursor 1, got %d", m.userJumpCursor)
	}
	assertJumpedTo(t, m, 2)
}

func TestUserJumpNextWalksForwardAfterAPrev(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "one"},
		{role: roleAssistant, text: "r1"},
		{role: roleUser, text: "two"},
		{role: roleAssistant, text: "r2"},
		{role: roleUser, text: "three"},
		{role: roleAssistant, text: "r3"},
	}
	m := buildUserJumpTestModel(msgs)
	m.userJumpPrev() // cursor 2 (newest)
	m.userJumpPrev() // cursor 1 (middle)
	if m.userJumpCursor != 1 {
		t.Fatalf("setup: expected cursor 1, got %d", m.userJumpCursor)
	}

	m.userJumpNext() // back to cursor 2
	if m.userJumpCursor != 2 {
		t.Fatalf("next should return to cursor 2, got %d", m.userJumpCursor)
	}
	assertJumpedTo(t, m, 4)
}

func TestUserJumpWithNoUserMessagesIsInert(t *testing.T) {
	msgs := []message{
		{role: roleAssistant, text: "hello"},
		{role: roleAssistant, text: "world"},
	}
	m := buildUserJumpTestModel(msgs)
	// Sentinel: a jump with nothing to jump to must leave the existing flash
	// alone rather than clearing or moving it. (0 is not a valid "no flash"
	// marker — Go's zero value — so the assertion has to use a sentinel
	// rather than comparing against -1.)
	m.chatSearchFlashMsg = 7
	m.userJumpPrev()
	if m.userJumpCursor != -1 {
		t.Fatalf("no user messages should leave the cursor inactive, got %d", m.userJumpCursor)
	}
	if m.userJumpIndicator() != "" {
		t.Fatalf("no user messages should leave the indicator empty, got %q", m.userJumpIndicator())
	}
	if m.chatSearchFlashMsg != 7 {
		t.Fatalf("no user messages should leave the existing flash untouched, got %d", m.chatSearchFlashMsg)
	}
}

func TestUserJumpIndicatorEmptyWhenInactive(t *testing.T) {
	msgs := []message{{role: roleUser, text: "only"}}
	m := buildUserJumpTestModel(msgs)
	if got := m.userJumpIndicator(); got != "" {
		t.Fatalf("inactive indicator = %q, want empty", got)
	}
	m.userJumpPrev()
	m.clearUserJump()
	if got := m.userJumpIndicator(); got != "" {
		t.Fatalf("indicator after clearUserJump = %q, want empty", got)
	}
	if m.userJumpCursor != -1 {
		t.Fatalf("clearUserJump should reset the cursor to -1, got %d", m.userJumpCursor)
	}
}

// TestUserJumpKeyDispatch is the wiring test: it proves bubbletea actually
// reports alt+up / alt+down as the key strings the handler switches on.
//
// Without this, the whole feature could compile, pass every unit test above,
// and still be dead in the real app — if msg.String() rendered the combo as
// "alt+↑" or "option+up" instead, no case would ever match and there would be
// no failure anywhere else to notice.
func TestUserJumpKeyDispatch(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "one"},
		{role: roleAssistant, text: "r1"},
		{role: roleUser, text: "two"},
		{role: roleAssistant, text: "r2"},
		{role: roleUser, text: "three"},
	}
	m := buildUserJumpTestModel(msgs)
	m.rerenderTranscriptAndMaybeScroll()

	up := tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt}
	if got := up.String(); got != "alt+up" {
		t.Fatalf("bubbletea renders alt+up as %q, but the handler switches on \"alt+up\"", got)
	}
	down := tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt}
	if got := down.String(); got != "alt+down" {
		t.Fatalf("bubbletea renders alt+down as %q, but the handler switches on \"alt+down\"", got)
	}

	out, cmd := m.handleChatKeys(up, nil, nil)
	got := derefTestModel(t, out)
	if got.userJumpCursor != 2 {
		t.Fatalf("alt+up should land on the newest user message (cursor 2), got %d", got.userJumpCursor)
	}
	if got.userJumpIndicator() != "msg 3/3" {
		t.Fatalf("alt+up indicator = %q, want %q", got.userJumpIndicator(), "msg 3/3")
	}
	if cmd == nil {
		t.Fatal("alt+up should return a flash-expiry tick so the highlight clears")
	}

	out, _ = got.handleChatKeys(up, nil, nil)
	got = derefTestModel(t, out)
	if got.userJumpCursor != 1 {
		t.Fatalf("second alt+up should reach cursor 1, got %d", got.userJumpCursor)
	}

	out, _ = got.handleChatKeys(down, nil, nil)
	got = derefTestModel(t, out)
	if got.userJumpCursor != 2 {
		t.Fatalf("alt+down should walk forward to cursor 2, got %d", got.userJumpCursor)
	}
}

// TestUserJumpClearedByAnUnrelatedKey guards the staleness rule: any other key
// ends the walk, so the status readout can never keep claiming a position
// after the user has moved on (and the next alt+up re-seeds from the newest).
func TestUserJumpClearedByAnUnrelatedKey(t *testing.T) {
	msgs := []message{
		{role: roleUser, text: "one"},
		{role: roleAssistant, text: "r1"},
		{role: roleUser, text: "two"},
	}
	m := buildUserJumpTestModel(msgs)
	m.rerenderTranscriptAndMaybeScroll()

	out, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt}, nil, nil)
	got := derefTestModel(t, out)
	if got.userJumpCursor == -1 {
		t.Fatal("setup: alt+up should have activated the walk")
	}

	out, _ = got.handleChatKeys(tea.KeyPressMsg{Code: 'x', Text: "x"}, nil, nil)
	got = derefTestModel(t, out)
	if got.userJumpCursor != -1 {
		t.Fatalf("an ordinary keypress should clear the walk, cursor = %d", got.userJumpCursor)
	}
	if got.userJumpIndicator() != "" {
		t.Fatalf("an ordinary keypress should clear the indicator, got %q", got.userJumpIndicator())
	}
}
