package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

// TestSidebarClickMatchesRenderedRowUnderOverflow renders the full chat content
// (header + transcript + sidebar) and verifies that a click at the screen row
// where a file / the Allowed header actually renders resolves through the
// sidebar hit-tests. The regression: when the sidebar overflows the available
// height, renderSidebar trims the scroll box (constrainViewPreservingBottom) and
// every section below shifts up, but the hit-tests kept using the untrimmed
// scroll height — so the click target sat 1-2 rows below the rendered row and
// the user had to "click N lines up".
func TestSidebarClickMatchesRenderedRowUnderOverflow(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	snapshot.Reset()
	tool.SetTodoSession("probe")
	tool.ResetTodoState()
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("changed-%02d.go", i)
		if err := os.WriteFile(name, []byte("package main\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := snapshot.Backup(name); err != nil {
			t.Fatal(err)
		}
	}

	// Heights chosen to exercise no-overflow (40, 24) and overflow (18, 16) paths.
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
				viewport:     viewport.New(viewport.WithWidth(100), viewport.WithHeight(h)),
				agent:        agent.NewAgent(retryTestClient{}, nil, nil),
				config:       &config.Config{Model: "gpt-4o"},
			}
			m.layout()

			rows := strings.Split(stripANSI(m.renderContent()), "\n")
			sidebarX := m.panelWidth() + 1

			// Allowed header: find its rendered row, click it, expect a hit.
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
				t.Errorf("Allowed header rendered at Y=%d but hit-test rejected that row", allowedY)
			}
			// And the rows immediately around it must NOT register (off-by-one guard).
			if m.sidebarAllowedHeaderForClick(tea.Mouse{X: sidebarX, Y: allowedY+1}) {
				t.Errorf("Allowed hit-test wrongly accepted Y=%d (one below rendered row)", allowedY+1)
			}

			// First visible changed file: find its rendered row, click it.
			for i := 0; i < 8; i++ {
				name := fmt.Sprintf("changed-%02d.go", i)
				fileY := -1
				for y, r := range rows {
					if strings.Contains(r, name) {
						fileY = y
						break
					}
				}
				if fileY < 0 {
					continue // trimmed out of the visible scroll window
				}
				if path, ok := m.sidebarFileForClick(tea.Mouse{X: sidebarX, Y: fileY}); !ok || path != name {
					t.Errorf("file %s rendered at Y=%d but hit-test returned (%q,%v)", name, fileY, path, ok)
				}
				break
			}
		})
	}
}
