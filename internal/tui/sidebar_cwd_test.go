package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// TestSidebarCWDRowClickMatchesRenderedRow verifies the "cwd:" sidebar row is
// clickable (opens the working dir) and that the hit-test resolves the exact
// rendered row — no off-by-one under both overflow and non-overflow heights.
func TestSidebarCWDRowClickMatchesRenderedRow(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ocode-cwd-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	for _, h := range []int{40, 24, 18, 16} {
		t.Run(fmt.Sprintf("height=%d", h), func(t *testing.T) {
			m := model{
				ready:        true,
				width:        140,
				height:       h,
				showSidebar:  true,
				sessionTitle: "my session title",
				workDir:      tmpDir,
				styles:       ApplyThemeColors("tokyonight"),
				input:        textarea.New(),
				viewport:     fastviewport.New(100, h),
				agent:        agent.NewAgent(retryTestClient{}, nil, nil, nil),
				config:       &config.Config{Model: "gpt-4o"},
			}
			m.layout()

			rows := strings.Split(stripANSI(m.renderContent()), "\n")
			sidebarX := m.panelWidth() + 1

			cwdY := -1
			for y, r := range rows {
				if strings.Contains(r, "cwd:") {
					cwdY = y
					break
				}
			}
			if cwdY < 0 {
				t.Fatal("cwd: row not found in rendered sidebar")
			}

			// Plain click on the rendered cwd row opens the working dir.
			path, ok := m.sidebarCWDForClick(tea.Mouse{X: sidebarX, Y: cwdY})
			if !ok {
				t.Errorf("cwd row rendered at Y=%d but hit-test rejected that row", cwdY)
			}
			if ok && path != tmpDir {
				t.Errorf("cwd hit-test returned %q, want %q", path, tmpDir)
			}

			// Off-by-one guards: rows immediately above/below must NOT match.
			if _, ok := m.sidebarCWDForClick(tea.Mouse{X: sidebarX, Y: cwdY - 1}); ok {
				t.Errorf("cwd hit-test wrongly accepted Y=%d (one above rendered row)", cwdY-1)
			}
			if _, ok := m.sidebarCWDForClick(tea.Mouse{X: sidebarX, Y: cwdY + 1}); ok {
				t.Errorf("cwd hit-test wrongly accepted Y=%d (one below rendered row)", cwdY+1)
			}

			// Plain hover over the cwd row sets the underline flag and requests
			// a re-render (so the underline actually paints).
			mm, _, changed := m.handleMouseMotion(tea.Mouse{X: sidebarX, Y: cwdY, Button: tea.MouseNone})
			if !mm.(model).hoverSidebarCWD {
				t.Errorf("hover over cwd row did not set hoverSidebarCWD")
			}
			if !changed {
				t.Errorf("hover over cwd row should request a re-render")
			}

			// A click outside the sidebar column must not resolve as a cwd click.
			if _, ok := m.sidebarCWDForClick(tea.Mouse{X: 5, Y: cwdY}); ok {
				t.Errorf("cwd hit-test wrongly accepted an X outside the sidebar column")
			}
		})
	}
}

// TestSidebarCWDDragStillSelects ensures a click-drag on the cwd row does NOT
// open the directory: the press/release handler copies the selection instead.
// This guards the "don't break select text on sidebar" requirement.
func TestSidebarCWDDragStillSelects(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ocode-cwd-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	m := model{
		ready:        true,
		width:        140,
		height:       40,
		showSidebar:  true,
		sessionTitle: "my session title",
		workDir:      tmpDir,
		styles:       ApplyThemeColors("tokyonight"),
		input:        textarea.New(),
		viewport:     fastviewport.New(100, 40),
		agent:        agent.NewAgent(retryTestClient{}, nil, nil, nil),
		config:       &config.Config{Model: "gpt-4o"},
	}
	m.layout()

	rows := strings.Split(stripANSI(m.renderContent()), "\n")
	sidebarX := m.panelWidth() + 1
	cwdY := -1
	for y, r := range rows {
		if strings.Contains(r, "cwd:") {
			cwdY = y
			break
		}
	}
	if cwdY < 0 {
		t.Fatal("cwd: row not found in rendered sidebar")
	}

	// Press inside the cwd row → selection drag begins.
	mm, _, handled := m.handleMouseAction(tea.Mouse{X: sidebarX, Y: cwdY, Button: tea.MouseLeft}, true)
	if !handled {
		t.Fatal("expected the cwd-row press to be handled as a selection drag")
	}
	// Move within the cwd row → drag becomes active (a selection, not a click).
	mm2, _, _ := mm.(model).handleMouseMotion(tea.Mouse{X: sidebarX + 3, Y: cwdY, Button: tea.MouseLeft})
	if !mm2.(model).sidebarSel.active {
		t.Fatal("expected an active selection after dragging within the cwd row")
	}
	// Release → selection is copied, the directory is NOT opened.
	_, cmd, handled := mm2.(model).handleMouseAction(tea.Mouse{X: sidebarX + 3, Y: cwdY, Button: tea.MouseLeft}, false)
	if !handled {
		t.Fatal("expected the release to be handled")
	}
	if cmd != nil {
		t.Errorf("dragging on the cwd row should not open the directory (got a non-nil cmd)")
	}
}
