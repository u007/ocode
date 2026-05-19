package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/jamesmercstudio/ocode/internal/agent"
)

func TestRenderToolResultPreservesFullOutput(t *testing.T) {
	content := strings.Repeat("line\n", 1000)

	got := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))

	if got != ApplyThemeColors("tokyonight").Text.Render(content) {
		t.Fatal("expected rendered tool result to preserve full output")
	}
}

func TestClickToolOutputExpandsInline(t *testing.T) {
	content := strings.Repeat("process output line\n", 600)
	text := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))
	toolID := "tool-1"
	m := model{
		ready:     true,
		width:     100,
		height:    30,
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(96), viewport.WithHeight(24)),
		styles:    ApplyThemeColors("tokyonight"),
		messages:  []message{{role: roleAssistant, text: text, raw: &agent.Message{Role: "tool", ToolID: toolID, Content: content}}},
		sessionID: "test",
	}
	m.renderTranscript()

	if len(m.toolOutputRegions) != 1 {
		t.Fatalf("expected one clickable tool output region, got %d", len(m.toolOutputRegions))
	}
	if strings.Count(m.viewport.View(), "process output line") > toolOutputPreviewLines {
		t.Fatal("expected collapsed tool output to show at most preview lines")
	}

	y := m.toolOutputRegions[0].startLine - m.viewport.YOffset() + 2
	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: y})
	updated, _ = updated.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: 4, Y: y})
	got := derefTestModel(t, updated)
	if !got.expandedToolOutputs[0] {
		t.Fatal("expected click to expand tool output inline")
	}
	if strings.Count(got.viewport.View(), "process output line") <= toolOutputPreviewLines {
		t.Fatal("expected expanded output to show more than preview lines")
	}
}
