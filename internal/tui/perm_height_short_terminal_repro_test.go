package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/u007/ocode/internal/agent"
)

// Regression: on short terminals the permission dialog's body cap (max of a
// fixed floor and 40% of terminal height) ignored the rest of the bottom
// chrome, so the total chrome exceeded the terminal. layout() clamped the
// transcript to 1 row and the View() safety net could not shrink it further,
// so every frame while the dialog was open rendered TALLER than the terminal.
// Each over-tall frame scrolls the real terminal up one row; the damage
// survives dialog dismissal, compounds with every popup, and only a window
// resize (full repaint) repairs it — i.e. "the chat tab keeps shrinking after
// every permission accept/deny".
func TestPermDialogNeverExceedsShortTerminal(t *testing.T) {
	longCmd := "python3 - <<'EOF'\nfor i in range(1, 6):\n    print(i)\nprint('a fairly long trailing line to force wrapping at narrow widths')\nEOF"
	req := agent.PermissionRequest{ToolName: "bash", Command: longCmd, Rule: "bash(python3*)"}

	for _, size := range []struct{ w, h int }{
		{120, 24}, // reproduced live: steady overflow=1 while dialog open
		{100, 20},
		{80, 15},
		{80, 10},
	} {
		m := newModel()
		m.ready = true
		upd, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m = upd.(model)

		// Real-usage chrome: a finished turn leaves the reserved activity row
		// behind for the rest of the session.
		m.activityRowReserved = true

		baseline := lipgloss.Height(m.renderContent())
		if baseline > m.height {
			t.Errorf("%dx%d: baseline view height %d exceeds terminal %d", size.w, size.h, baseline, m.height)
		}

		// Open the permission dialog (mirrors handleAgentMessage perm path).
		m.showPermDialog = true
		m.permConfirm = ""
		m.pendingPermission = req
		m.pendingToolName = req.ToolName
		m.layout()
		openView := lipgloss.Height(m.renderContent())
		t.Logf("%dx%d open: view=%d vp=%d permBody=%d", size.w, size.h, openView, m.viewport.Height(), m.permViewport.Height())
		if openView > m.height {
			t.Errorf("%dx%d: open view height %d exceeds terminal %d — over-tall frames scroll the terminal and permanently corrupt the layout", size.w, size.h, openView, m.height)
		}

		// The always-allow confirm step re-renders the body; must still fit.
		m.permDialogInput("a")
		confView := lipgloss.Height(m.renderContent())
		if confView > m.height {
			t.Errorf("%dx%d: confirm view height %d exceeds terminal %d", size.w, size.h, confView, m.height)
		}
		m.permDialogInput("back")

		// Close and verify exact restoration.
		m.showPermDialog = false
		m.layout()
		closeView := lipgloss.Height(m.renderContent())
		if closeView != baseline {
			t.Errorf("%dx%d: view height %d != baseline %d after close", size.w, size.h, closeView, baseline)
		}
	}
}

// Regression: even when bottom-chrome state changes between layout() calls
// (queue/strip/popup rows appearing or the dialog itself), View() must never
// emit a frame taller than the terminal — over-tall frames scroll the real
// terminal and the corruption persists until a resize.
func TestRenderContentNeverTallerThanTerminal(t *testing.T) {
	m := newModel()
	m.ready = true
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	m = upd.(model)

	req := agent.PermissionRequest{
		ToolName: "bash",
		Command:  strings.Repeat("really-long-command --with-flags ", 20),
		Rule:     "bash(really-long-command*)",
	}
	// Worst case: dialog opened WITHOUT a layout() call (chrome drift).
	m.showPermDialog = true
	m.pendingPermission = req
	m.pendingToolName = req.ToolName

	if got := lipgloss.Height(m.renderContent()); got > m.height {
		t.Errorf("view height %d exceeds terminal %d with drifted chrome", got, m.height)
	}
}
