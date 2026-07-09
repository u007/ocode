package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
)

// TestGitTabRendersFullWidth is a regression test for the git tab column
// shrink bug. The git tab is a full-screen three-pane tool (sections / files /
// diff) and must render at the full window width, NOT the sidebar-constrained
// panelWidth(). A previous change passed panelWidth() (width - sidebarWidth)
// to the git view, squeezing all three columns by the sidebar width, and the
// pane/border widths under-allocated by 2 columns each, leaving a right margin.
func TestGitTabRendersFullWidth(t *testing.T) {
	const width = 200
	g := gitModel{
		width:          width,
		height:         50,
		section:        gitSectionChanges,
		panel:          gitPanelFiles,
		stagedFiles:    []gitFile{{status: "M", path: "internal/tui/git_model.go"}},
		unstagedFiles:  []gitFile{{status: "A", path: "cmd/ocode/main.go"}, {status: "D", path: "README.md"}},
		untrackedFiles: []gitFile{{status: "??", path: "newfile.txt"}},
		filesCursor:    0,
		selectedFiles:  map[int]bool{},
		diff:           viewport.New(),
		commitInput:    textarea.New(),
		currentBranch:  "main",
	}
	g.Resize(width, 50)

	styles := currentStyles()
	out := g.View(width, 50, styles, false, false)

	// Find the top border row of the three-pane layout (contains the box
	// corners). That row's visible width must equal the full window width:
	// sections pane + files pane + diff pane, joined with no squeeze or
	// trailing margin.
	var borderRow string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "╭") && strings.Contains(line, "╮") {
			borderRow = line
			break
		}
	}
	if borderRow == "" {
		t.Fatalf("could not find git pane border row in output:\n%s", out)
	}
	plain := stripANSI(borderRow)
	if got := lipgloss.Width(plain); got != width {
		t.Errorf("git tab rendered at width %d, want full window width %d (columns are squeezed by %d)", got, width, width-got)
	}

	// The right-most pane border must sit at the very last column, i.e. the
	// diff pane extends to the full window edge with no trailing margin.
	if !strings.HasSuffix(plain, "╮") {
		t.Errorf("git tab panes do not extend to the right window edge: %q", plain)
	}
}
