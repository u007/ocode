package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestBorderedPaneContentWidth pins lipgloss v2's Width semantics for a bordered,
// padded pane: the value passed to Width() is the full frame width — border (2)
// and horizontal padding (2) are counted inside it. So a pane rendered with
// Width(paneW) only fits paneW-4 columns of content before wrapping.
//
// The files-tab listing relies on this: rows are truncated to treeW-6 because the
// tree pane uses Width(treeW-2). If the content budget is off by even one column,
// long rows wrap onto a second line.
func TestBorderedPaneContentWidth(t *testing.T) {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)

	const paneW = 40
	const fits = paneW - 4 // border 2 + padding 2

	if h := lipgloss.Height(borderStyle.Width(paneW).Render(strings.Repeat("a", fits))); h != 3 {
		t.Errorf("content of width %d in Width(%d) wrapped: height=%d, want 3 (top+content+bottom)", fits, paneW, h)
	}
	if h := lipgloss.Height(borderStyle.Width(paneW).Render(strings.Repeat("a", fits+1))); h != 4 {
		t.Errorf("content of width %d in Width(%d) did not wrap: height=%d, want 4", fits+1, paneW, h)
	}
}
