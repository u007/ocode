package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/u007/ocode/internal/agent"
)

// Regression: repeatedly opening/closing the permission dialog (including the
// always-allow confirm step) must not drift the chat render height or the
// transcript viewport height — every close must restore the exact baseline.
func TestPermDialogRepeatedOpenCloseHeightStable(t *testing.T) {
	m := newModel()
	m.ready = true
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = upd.(model)

	baselineVP := m.viewport.Height()
	baselineView := lipgloss.Height(m.renderContent())
	t.Logf("baseline: vp=%d view=%d term=%d", baselineVP, baselineView, m.height)

	longCmd := "python3 - <<'EOF'\n# comment line one\nimport os\nfor i in range(100):\n    print(i)\nprint('done with a fairly long trailing line to force wrapping at narrow widths')\nEOF"
	req := agent.PermissionRequest{ToolName: "bash", Command: longCmd, Rule: "bash(python3*)"}

	for i := 0; i < 5; i++ {
		// open (mirrors handleAgentMessage perm path)
		m.showPermDialog = true
		m.permConfirm = ""
		m.pendingPermission = req
		m.pendingToolName = req.ToolName
		m.layout()
		openView := lipgloss.Height(m.renderContent())
		openVP := m.viewport.Height()
		t.Logf("iter %d open:  vp=%d view=%d", i, openVP, openView)
		if openView > m.height {
			t.Errorf("iter %d: open view height %d exceeds terminal %d", i, openView, m.height)
		}

		// step to the always-allow confirm screen ('a'), then back
		m.permDialogInput("a")
		confirmView := lipgloss.Height(m.renderContent())
		t.Logf("iter %d conf:  vp=%d view=%d", i, m.viewport.Height(), confirmView)
		if confirmView > m.height {
			t.Errorf("iter %d: confirm view height %d exceeds terminal %d", i, confirmView, m.height)
		}
		m.permDialogInput("back")

		// close (mirrors handleChatKeys 'y' path)
		m.showPermDialog = false
		m.layout()
		m.rerenderTranscriptAndMaybeScroll()
		closeView := lipgloss.Height(m.renderContent())
		closeVP := m.viewport.Height()
		t.Logf("iter %d close: vp=%d view=%d", i, closeVP, closeView)
		if closeVP != baselineVP {
			t.Errorf("iter %d: viewport height %d != baseline %d after close", i, closeVP, baselineVP)
		}
		if closeView != baselineView {
			t.Errorf("iter %d: view height %d != baseline %d after close", i, closeView, baselineView)
		}
	}
}
