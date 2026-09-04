package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// TestAgentStripSidebarXGuard ensures clicks at the strip's screen rows but in
// the sidebar column (X >= panelWidth) never open the agent detail view.
// This is the direct unit guard: the old handler checked Y only.
func TestAgentStripSidebarXGuard(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	run := a.Runs().New("worker")
	setRunTranscriptForTest(run, agent.Message{Role: "assistant", Content: "did some work"})

	m := model{
		ready:       true,
		width:       140,
		height:      40,
		activeTab:   tabChat,
		showSidebar: true,
		input:       newTestTextarea(),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
		agent:       a,
	}
	m.viewport = fastviewport.New(80, 20)
	m.layout()

	_, blocks := m.renderAgentStrip()
	if len(blocks) == 0 {
		t.Fatal("expected non-empty agent strip")
	}
	stripY := m.agentStripTopY() + blocks[0].rowStart
	sidebarX := m.panelWidth() + 1

	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: sidebarX, Y: stripY})
	got := derefTestModel(t, updated)
	if len(got.detail) != 0 {
		t.Fatalf("sidebar-column click at strip Y=%d opened agent detail", stripY)
	}

	// Sanity: the same row in the left panel still opens the detail view.
	updated, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: stripY})
	got = derefTestModel(t, updated)
	if len(got.detail) == 0 {
		t.Fatalf("left-panel click at strip Y=%d should open agent detail", stripY)
	}
}

// TestAgentStripDoesNotStealSidebarPermClick is a regression test for the
// report: "once agent is visible at chat session on tui, clicking on the
// permission mode will trigger the agent view instead of the sidebar
// permission".
//
// Root cause: the agent-strip press handler in handleMouseAction checked only
// the Y range (no X bound), so any click at the strip's screen rows — including
// clicks in the sidebar (X >= panelWidth) — opened the agent detail view and
// never reached the sidebar "Allowed" permission handler.
func TestAgentStripDoesNotStealSidebarPermClick(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	run := a.Runs().New("worker")
	setRunTranscriptForTest(run, agent.Message{Role: "assistant", Content: "did some work"})

	m := model{
		ready:       true,
		width:       140,
		height:      40,
		activeTab:   tabChat,
		showSidebar: true,
		input:       newTestTextarea(),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
		agent:       a,
	}
	m.viewport = fastviewport.New(80, 20)
	m.layout()

	strip, blocks := m.renderAgentStrip()
	if strip == "" || len(blocks) == 0 {
		t.Fatalf("expected non-empty agent strip, got strip=%q blocks=%d", strip, len(blocks))
	}

	// Find where the sidebar "Allowed" header actually paints.
	rows := strings.Split(stripANSI(m.renderContent()), "\n")
	sidebarX := m.panelWidth() + 1
	allowedY := -1
	for y, r := range rows {
		if strings.Contains(r, "Allowed") {
			allowedY = y
			break
		}
	}
	if allowedY < 0 {
		t.Fatal("Allowed header not found in rendered sidebar")
	}
	if !m.sidebarAllowedHeaderForClick(tea.Mouse{X: sidebarX, Y: allowedY}) {
		t.Fatalf("test setup broken: hit-test rejects Allowed row Y=%d", allowedY)
	}

	// Press + release on the sidebar Allowed header must NOT open agent detail.
	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: sidebarX, Y: allowedY})
	got := derefTestModel(t, updated)
	if len(got.detail) != 0 {
		t.Fatalf("sidebar Allowed click at Y=%d opened agent detail (strip stole the click)", allowedY)
	}
	updated, _ = got.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: sidebarX, Y: allowedY})
	got = derefTestModel(t, updated)
	if len(got.detail) != 0 {
		t.Fatalf("sidebar Allowed click release at Y=%d opened agent detail", allowedY)
	}
}

// TestAgentStripDoesNotStealStatusPermClick ensures a click on the status-bar
// permission text still cycles the permission mode (press starts a status drag,
// release cycles) instead of opening the agent detail view.
func TestAgentStripDoesNotStealStatusPermClick(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate persist writes

	a := agent.NewAgent(nil, nil, nil, nil)
	run := a.Runs().New("worker")
	setRunTranscriptForTest(run, agent.Message{Role: "assistant", Content: "did some work"})

	m := model{
		ready:       true,
		width:       120,
		height:      40,
		activeTab:   tabChat,
		showSidebar: true,
		input:       newTestTextarea(),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
		agent:       a,
	}
	m.viewport = fastviewport.New(80, 20)
	m.layout()
	m.renderStatus()
	// Start from a visible permission label so the click region is populated.
	m.agent.Permissions().SetAutoPermissionEnabled(true)
	m.renderStatus()

	statusTop := m.statusBarTopY()
	x := m.statusPermColStart + 1
	if x >= m.statusPermColEnd {
		t.Fatalf("test setup broken: perm cols [%d,%d)", m.statusPermColStart, m.statusPermColEnd)
	}

	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: statusTop})
	got := derefTestModel(t, updated)
	if len(got.detail) != 0 {
		t.Fatalf("status permission press at Y=%d opened agent detail (strip stole the click)", statusTop)
	}
	beforeAuto := got.agent.Permissions().AutoPermissionEnabled()
	updated, _ = got.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: x, Y: statusTop})
	got = derefTestModel(t, updated)
	if len(got.detail) != 0 {
		t.Fatalf("status permission release at Y=%d opened agent detail", statusTop)
	}
	// Release on the permission text cycles normal+auto -> yolo.
	if got.agent.Permissions().AutoPermissionEnabled() == beforeAuto && beforeAuto {
		t.Fatalf("status permission click did not cycle permission mode (auto still %v)", beforeAuto)
	}

	// Sidebar column at the status rows must not arm a status drag (the old
	// press handler had no upper X bound and swallowed sidebar clicks there).
	sidebarX := m.panelWidth() + 1
	updated, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: sidebarX, Y: statusTop})
	got = derefTestModel(t, updated)
	if got.statusSel.dragging {
		t.Fatalf("sidebar-column click at status Y=%d armed a status drag", statusTop)
	}
	if len(got.detail) != 0 {
		t.Fatalf("sidebar-column click at status Y=%d opened agent detail", statusTop)
	}
}
