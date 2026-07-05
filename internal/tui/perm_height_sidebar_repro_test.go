package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/u007/ocode/internal/agent"
)

// Regression: with the sidebar enabled (width >= 120), the rendered view must
// never exceed the terminal height. The sidebar renders below the 2-row app
// header, so its column budget is m.height - appHeaderHeight; when that term
// was missing, a saturated sidebar overflowed the terminal by exactly 2 rows,
// tripping the View() safety net which silently shrank the chat transcript on
// every frame (visibly on each permission popup) and clipped the sidebar's
// pinned bottom rows.
func TestSidebarViewHeightMatchesTerminal(t *testing.T) {
	m := newModel()
	m.ready = true
	// Height 28 is short enough that the default sidebar sections (Git/TODO/
	// Tools/paths/usage) saturate the column, which is the condition that used
	// to overflow the terminal by appHeaderHeight rows.
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	m = upd.(model)

	if !m.sidebarEnabled() {
		t.Fatal("sidebar expected enabled at width 160")
	}

	// Give the sidebar enough content to fill (long transcript is irrelevant;
	// sidebar sections come from session state). Render as-is first.
	baseView := lipgloss.Height(m.renderContent())
	sidebarH := lipgloss.Height(m.renderSidebar())
	t.Logf("baseline: view=%d term=%d sidebar=%d vp=%d", baseView, m.height, sidebarH, m.viewport.Height())
	if sidebarH > m.height-appHeaderHeight {
		t.Errorf("sidebar height %d exceeds available %d (term %d - header %d)",
			sidebarH, m.height-appHeaderHeight, m.height, appHeaderHeight)
	}
	if baseView > m.height {
		t.Errorf("view height %d exceeds terminal %d without any dialog", baseView, m.height)
	}

	req := agent.PermissionRequest{ToolName: "bash", Command: "touch marker.txt", Rule: "bash(touch*)"}
	for i := 0; i < 3; i++ {
		m.showPermDialog = true
		m.permConfirm = ""
		m.pendingPermission = req
		m.pendingToolName = req.ToolName
		m.layout()
		openView := lipgloss.Height(m.renderContent())
		t.Logf("iter %d open:  vp=%d view=%d", i, m.viewport.Height(), openView)

		m.showPermDialog = false
		m.layout()
		closeView := lipgloss.Height(m.renderContent())
		t.Logf("iter %d close: vp=%d view=%d", i, m.viewport.Height(), closeView)
		if closeView != baseView {
			t.Errorf("iter %d: view height %d != baseline %d after close", i, closeView, baseView)
		}
	}
}
