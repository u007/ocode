package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// TestActivityRowGrowthStaysWithinHeight reproduces the bug where the activity
// row wraps to multiple lines once several tools run concurrently. On a short
// terminal the transcript viewport is already floored, so the wrapped chrome
// pushed the status bar below the bottom of the screen. The row must stay a
// single line so renderContent keeps the whole view within the terminal height.
func TestActivityRowGrowthStaysWithinHeight(t *testing.T) {
	m := model{
		ready:     true,
		width:     80, // below sidebarMinWidth (120) → sidebar off, the affected path
		height:    13, // short terminal (e.g. a split pane); transcript viewport floors at 1. 13 rows = 12 chrome + 1 viewport at the floor.
		sessionID: strings.Repeat("session-", 12),
		input:     textarea.New(),
		viewport:  fastviewport.New(76, 7),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line that should stay in the viewport\n", 80),
		}},
	}
	m.input.SetHeight(3) // mirror the real app's input height
	m.input.SetValue("draft input")

	// layout() runs while the activity row is empty (one reserved line).
	m.activityRowReserved = true
	m.layout()

	// A handful of tools start running — without this fix the joined tool list
	// wraps the activity row to 2-3 lines, and no layout() runs before the next
	// render frame.
	tools := make([]agent.ToolActivity, 4)
	for i := range tools {
		tools[i] = agent.ToolActivity{Name: "bash_command", StartedAt: time.Now()}
	}
	m.lastActivity = agent.ActivitySnapshot{LLMRunning: true, ActiveTools: tools}

	if got := lipgloss.Height(m.renderActivityRow()); got != 1 {
		t.Fatalf("activity row should stay one line, got %d lines", got)
	}

	content := m.renderContent()
	if got := lipgloss.Height(content); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d:\n%s", got, m.height, content)
	}
	if !strings.Contains(content, "Agent:") {
		t.Fatalf("expected status line to remain visible, got:\n%s", content)
	}
}
