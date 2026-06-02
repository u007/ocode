package tui

import (
	"fmt"
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

func TestRenderToolResultHidesTruncationFooter(t *testing.T) {
	content := fmt.Sprintf(
		"line 1\nline 2\n\n[output truncated: showing 100/139 lines, 14/14 chars]\nFull output saved to: /tmp/out.txt\nRetrieve remaining content with:\n  read tool: {\"path\": %q, \"start_line\": 101, \"end_line\": <n>}\n  or bash:   sed -n '101,139p' /tmp/out.txt",
		"/tmp/out.txt",
	)

	got := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))

	if strings.Contains(got, "[output truncated:") ||
		strings.Contains(got, "Full output saved to:") ||
		strings.Contains(got, "Retrieve remaining content with:") ||
		strings.Contains(got, "read tool:") ||
		strings.Contains(got, "or bash:") {
		t.Fatal("expected rendered tool result to hide truncation footer")
	}
	if !strings.Contains(got, "line 1") || !strings.Contains(got, "line 2") {
		t.Fatal("expected rendered tool result to preserve visible tool output")
	}
}

func TestRenderToolResultHidesTruncationFooterPrefixOnly(t *testing.T) {
	content := "[output truncated: showing 100/200 lines, 1000/2000 chars]\nFull output saved to: /tmp/out.txt"

	got := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))

	if strings.Contains(got, "[output truncated:") || strings.Contains(got, "Full output saved to:") {
		t.Fatal("expected rendered tool result to hide prefix-only truncation footer")
	}
}

func TestToolOutputBoxHidesTruncationFooter(t *testing.T) {
	content := fmt.Sprintf(
		"line 1\nline 2\n\n[output truncated: showing 100/139 lines, 14/14 chars]\nFull output saved to: /tmp/out.txt\nRetrieve remaining content with:\n  read tool: {\"path\": %q, \"start_line\": 101, \"end_line\": <n>}\n  or bash:   sed -n '101,139p' /tmp/out.txt",
		"/tmp/out.txt",
	)
	text := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))
	toolID := "tool-expand-1"
	m := model{
		ready:               true,
		width:               100,
		height:              30,
		input:               textarea.New(),
		viewport:            viewport.New(viewport.WithWidth(96), viewport.WithHeight(24)),
		styles:              ApplyThemeColors("tokyonight"),
		messages:            []message{{role: roleAssistant, text: text, raw: &agent.Message{Role: "tool", ToolID: toolID, Content: content}}},
		sessionID:           "test",
		expandedToolOutputs: map[int]bool{0: true},
	}
	m.renderTranscript()

	view := m.viewport.View()
	if strings.Contains(view, "[output truncated:") ||
		strings.Contains(view, "Full output saved to:") ||
		strings.Contains(view, "Retrieve remaining content with:") {
		t.Fatal("expected expanded tool output box to hide truncation footer")
	}
	if !strings.Contains(view, "line 1") || !strings.Contains(view, "line 2") {
		t.Fatal("expected expanded tool output box to preserve visible output")
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

	// Screen Y of the top of the transcript viewport: app header (2 rows:
	// top pad + title) + the panel's top border (1 row).
	y := m.toolOutputRegions[0].startLine - m.viewport.YOffset() + appHeaderHeight + 1
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

func TestToolOutputPreviewRespectsSanitizedLineCount(t *testing.T) {
	content := strings.Repeat("progress line\r", toolOutputPreviewLines+8)
	text := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))
	toolID := "tool-cr"
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

	if strings.Count(m.viewport.View(), "progress line") > toolOutputPreviewLines {
		t.Fatalf("expected sanitized collapsed output to stay within preview lines, got view:\n%s", m.viewport.View())
	}
}
