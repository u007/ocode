package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// recordingTitleClient captures the prompt passed to the LLM so we can assert
// which task the regenerated title was derived from.
type recordingTitleClient struct {
	lastPrompt string
}

func (c *recordingTitleClient) Chat(msgs []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	for _, m := range msgs {
		if m.Role == "user" {
			c.lastPrompt = m.Content
		}
	}
	return &agent.Message{Role: "assistant", Content: "Regenerated Title"}, nil
}
func (c *recordingTitleClient) GetProvider() string { return "test" }
func (c *recordingTitleClient) GetModel() string    { return "test-model" }

// TestRegenerateTitleUsesLatestTask guards the requirement that the sidebar
// "gen" button regenerates the title from the LATEST task in the conversation
// (most recent user message + latest assistant response), not the original
// request.
func TestRegenerateTitleUsesLatestTask(t *testing.T) {
	rec := &recordingTitleClient{}
	a := agent.NewAgent(rec, nil, &config.Config{}, nil)
	m := &model{
		agent:    a,
		titleCh:  make(chan titleResult, 4),
		titleGen: 0,
		messages: []message{
			{role: roleUser, text: "original request: implement feature A"},
			{role: roleAssistant, text: "Implemented feature A."},
			{role: roleUser, text: "now refactor feature A into B"},
			{role: roleAssistant, text: "Refactored into B."},
		},
	}
	m.sessionTitle = "Old Title"

	// The helpers feeding regeneration must reflect the latest task.
	if got := m.lastUserMessageText(); got != "now refactor feature A into B" {
		t.Errorf("lastUserMessageText = %q, want latest user msg", got)
	}
	if got := m.lastAssistantContent(); got != "Refactored into B." {
		t.Errorf("lastAssistantContent = %q, want latest assistant msg", got)
	}

	m.regenerateTitle()

	// Cleared so the result handler can apply the fresh title.
	if m.sessionTitle != "" {
		t.Errorf("sessionTitle not cleared for regeneration, got %q", m.sessionTitle)
	}
	if !m.titleRequested {
		t.Error("titleRequested should be true while regenerating")
	}
	if !m.titleRegenerating {
		t.Error("titleRegenerating should be true while regenerating")
	}

	// Wait for the async generation to deliver its result on titleCh.
	select {
	case res := <-m.titleCh:
		if res.gen != m.titleGen {
			t.Errorf("result gen %d != current %d", res.gen, m.titleGen)
		}
		if res.title == "" {
			t.Error("expected non-empty regenerated title")
		}
		if !strings.Contains(rec.lastPrompt, "now refactor feature A into B") {
			t.Errorf("title prompt does not reference the latest task: %q", rec.lastPrompt)
		}
		if strings.Contains(rec.lastPrompt, "original request: implement feature A") {
			t.Errorf("title prompt incorrectly references the original request: %q", rec.lastPrompt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for regenerated title result")
	}
}

// TestSidebarTitleGenForClickHitBox verifies the gen button hit-test lands on
// the header row at the right edge of the sidebar and nowhere else.
func TestSidebarTitleGenForClickHitBox(t *testing.T) {
	m := &model{}
	m.sessionTitle = "Some Title"
	// Force a realistic layout.
	m.width = 120
	m.height = 40
	m.showSidebar = true
	m.activeTab = tabChat

	// Header row is at appHeaderHeight+headerLines; the gen button rides the
	// LAST header row (a short title is a single row).
	headerY := appHeaderHeight + m.sidebarHeaderHeight()
	// panelWidth() = width - sidebarColumnWidth.
	innerRight := m.panelWidth() + sidebarColumnWidth - 2
	btnW := lipgloss.Width(sidebarTitleGenBtn)

	// A click on the button itself should hit.
	if !m.sidebarTitleGenForClick(tea.Mouse{X: innerRight - btnW, Y: headerY}) {
		t.Error("expected hit on button left edge")
	}
	if !m.sidebarTitleGenForClick(tea.Mouse{X: innerRight - 1, Y: headerY}) {
		t.Error("expected hit on button right edge")
	}
	// A click just left of the button (still in the title area) should miss.
	if m.sidebarTitleGenForClick(tea.Mouse{X: innerRight - btnW - 1, Y: headerY}) {
		t.Error("expected miss just left of the button")
	}
	// A click on a different row should miss.
	if m.sidebarTitleGenForClick(tea.Mouse{X: innerRight - 1, Y: headerY + 1}) {
		t.Error("expected miss on a non-header row")
	}
}

// TestSidebarTitleGenForClickHitBoxMultiLine verifies the gen button rides the
// LAST wrapped header row when the title spans multiple lines (capped at
// sidebarMaxTitleLines).
func TestSidebarTitleGenForClickHitBoxMultiLine(t *testing.T) {
	m := &model{}
	m.showSidebar = true
	m.width = 120
	m.height = 40
	m.activeTab = tabChat
	// A long title that wraps past the 3-line budget.
	m.sessionTitle = strings.Repeat("word ", 40)

	lines := m.sidebarHeaderHeight()
	if lines != sidebarMaxTitleLines {
		t.Fatalf("expected header to wrap to %d lines, got %d", sidebarMaxTitleLines, lines)
	}
	headerY := appHeaderHeight + lines
	innerRight := m.panelWidth() + sidebarColumnWidth - 2

	// Hit on the button (last header row).
	if !m.sidebarTitleGenForClick(tea.Mouse{X: innerRight - 1, Y: headerY}) {
		t.Error("expected hit on button for multi-line title (last row)")
	}
	// Miss on the first header row (title text, no button).
	if m.sidebarTitleGenForClick(tea.Mouse{X: innerRight - 1, Y: headerY - 1}) {
		t.Error("expected miss on a non-last header row")
	}
}

// TestSidebarTitleGenClickEndToEnd drives a real press+release through
// handleMouseAction (not just the hit-test helper) to guard against the
// wiring bug where the gen button's hit-test was correct but a click never
// reached regenerateTitle: the button rides the sidebar HEADER row, which
// sits above sidebarSelectableLines' range, so the press handler never armed
// sidebarSel.dragging and the release handler's dragging-gated click block
// never ran.
func TestSidebarTitleGenClickEndToEnd(t *testing.T) {
	rec := &recordingTitleClient{}
	m := &model{
		agent:    agent.NewAgent(rec, nil, &config.Config{}, nil),
		titleCh:  make(chan titleResult, 4),
		messages: []message{{role: roleUser, text: "do something"}},
	}
	m.sessionTitle = "Some Title"
	m.showSidebar = true
	m.width = 120
	m.height = 40
	m.activeTab = tabChat

	headerY := appHeaderHeight + m.sidebarHeaderHeight()
	innerRight := m.panelWidth() + sidebarColumnWidth - 2
	mouse := tea.Mouse{X: innerRight - 1, Y: headerY, Button: tea.MouseLeft}

	updated, _, handled := m.handleMouseAction(mouse, true)
	if !handled {
		t.Fatal("press on gen button was not handled")
	}
	m2 := updated.(model)
	if !m2.sidebarSel.dragging {
		t.Fatal("expected press on gen button to arm sidebarSel.dragging")
	}

	_, cmd, handled := m2.handleMouseAction(mouse, false)
	if !handled {
		t.Fatal("release on gen button was not handled")
	}
	if cmd == nil {
		t.Fatal("expected release on gen button to fire the regenerateTitle command")
	}
}

// TestSidebarHeaderRenderMatchesHeaderHeightMultiLine guards against the
// header/wrap-width bug where wordWrap sized each row to fill the full inner
// width, then renderSidebar prepended a 2-col "◆ "/indent prefix on top,
// overflowing the bordered box by 2 cols. Lipgloss silently wrapped that
// overflow onto a phantom extra physical line per row, desyncing the
// on-screen row count from sidebarHeaderHeight() — which made the gen
// button's Y hit-test land one or more rows off for any title spanning more
// than one wrapped line.
func TestSidebarHeaderRenderMatchesHeaderHeightMultiLine(t *testing.T) {
	m := &model{}
	m.showSidebar = true
	m.width = 120
	m.height = 40
	m.activeTab = tabChat
	m.sessionTitle = strings.Repeat("word ", 40)

	h := m.sidebarHeaderHeight()
	if h != sidebarMaxTitleLines {
		t.Fatalf("expected header to wrap to %d lines, got %d", sidebarMaxTitleLines, h)
	}

	out := m.renderSidebar()
	lines := strings.Split(out, "\n")
	// line 0 is the top border, lines 1..h are the header; line h+1 must be
	// the first pinned content row, not a phantom header-overflow line.
	firstContent := stripANSI(lines[h+1])
	if strings.Contains(firstContent, "word") {
		t.Errorf("header overflowed past its reported height into the content row: %q", firstContent)
	}
}

// TestSidebarTitleGenVisibleWithNoTitle is the regression test for the bug
// where the "✦ gen" title button disappeared entirely when a session had no
// title (and no first user prompt to derive one from). The button must remain
// visible and clickable via a placeholder title row.
func TestSidebarTitleGenVisibleWithNoTitle(t *testing.T) {
	m := &model{}
	m.showSidebar = true
	m.width = 120
	m.height = 40
	m.activeTab = tabChat
	// No session title and no messages => previously this yielded no header
	// row and the gen button vanished.
	if m.sessionTitle != "" {
		t.Fatalf("precondition: sessionTitle should be empty, got %q", m.sessionTitle)
	}
	if got := m.firstUserPromptText(); got != "" {
		t.Fatalf("precondition: firstUserPromptText should be empty, got %q", got)
	}

	// A title row must still exist so the gen button has somewhere to render.
	if got := m.sidebarDisplayTitle(); got != "Untitled" {
		t.Errorf("sidebarDisplayTitle = %q, want placeholder %q", got, "Untitled")
	}
	if h := m.sidebarHeaderHeight(); h != 1 {
		t.Errorf("sidebarHeaderHeight = %d, want 1 (placeholder row)", h)
	}

	// The gen button must be hit-testable on its placeholder row.
	headerY := appHeaderHeight + m.sidebarHeaderHeight()
	innerRight := m.panelWidth() + sidebarColumnWidth - 2
	if !m.sidebarTitleGenForClick(tea.Mouse{X: innerRight - 1, Y: headerY}) {
		t.Error("expected gen button hit on placeholder title row")
	}
	if m.sidebarTitleGenForClick(tea.Mouse{X: innerRight - 1, Y: headerY + 1}) {
		t.Error("expected miss below the placeholder title row")
	}
}
