package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/jamesmercstudio/ocode/internal/agent"
)

func TestRenderToolResultMarksTruncatedOutputExpandable(t *testing.T) {
	content := strings.Repeat("line\n", 1000)

	got := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))

	if !strings.Contains(got, fullToolOutputMarker) {
		t.Fatalf("expected expandable marker in truncated output, got %q", got)
	}
}

func TestClickTruncatedToolOutputOpensFullOutputAndBackReturns(t *testing.T) {
	content := strings.Repeat("process output line\n", 600)
	text := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))
	m := model{
		ready:     true,
		width:     100,
		height:    30,
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(96), viewport.WithHeight(24)),
		styles:    ApplyThemeColors("tokyonight"),
		messages:  []message{{role: roleAssistant, text: text, raw: &agent.Message{Role: "tool", Content: content}}},
		sessionID: "test",
	}
	m.renderTranscript()
	m.viewport.GotoBottom()

	markerLine := -1
	for line := range m.toolOutputLineMap {
		markerLine = line
		break
	}
	if markerLine < 0 {
		t.Fatal("expected marker line to be clickable")
	}

	y := markerLine - m.viewport.YOffset() + 2
	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: y})
	got := derefTestModel(t, updated)
	if !got.showFullToolOutput {
		t.Fatal("expected full output view to open")
	}
	if !strings.Contains(got.fullToolOutput.View(), "process output line") {
		t.Fatal("expected full output view to contain original output")
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got = derefTestModel(t, updated)
	if got.showFullToolOutput {
		t.Fatal("expected esc to return to main screen")
	}
}
