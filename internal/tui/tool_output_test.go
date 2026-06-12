package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
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
		viewport:            fastviewport.New(96, 24),
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
		viewport:  fastviewport.New(96, 24),
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

func TestClickToolOutputExpandsInlineAfterPrecedingMessages(t *testing.T) {
	// Regression: when messages precede a tool output, separator accounting
	// must use wrapped-line counts. The old code added 2 to nlAcc per separator
	// but only 1 wrapped line, causing startLine to drift up by 1 per preceding
	// message, making the first N lines of the tool output non-clickable.
	content := strings.Repeat("process output line\n", 600)
	text := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))
	toolID := "tool-after"
	precedingMessages := []message{
		{role: roleUser, text: "msg1"},
		{role: roleAssistant, text: "response1"},
		{role: roleUser, text: "msg2"},
		{role: roleAssistant, text: "response2"},
		{role: roleUser, text: "msg3"},
	}
	m := model{
		ready:    true,
		width:    100,
		height:   50,
		input:    textarea.New(),
		viewport: fastviewport.New(96, 44),
		styles:   ApplyThemeColors("tokyonight"),
		messages: append(precedingMessages, message{
			role: roleAssistant,
			text: text,
			raw:  &agent.Message{Role: "tool", ToolID: toolID, Content: content},
		}),
		sessionID: "test",
	}
	m.renderTranscript()

	if len(m.toolOutputRegions) != 1 {
		t.Fatalf("expected one tool output region, got %d", len(m.toolOutputRegions))
	}

	region := m.toolOutputRegions[0]
	// The region's startLine must actually point to a line that contains part of
	// the tool box, not to a separator or a preceding message's line.
	if region.startLine >= len(m.transcriptLines) {
		t.Fatalf("startLine %d out of range (transcriptLines len=%d)", region.startLine, len(m.transcriptLines))
	}
	firstLine := m.transcriptLines[region.startLine]
	if strings.Contains(firstLine, "msg1") || strings.Contains(firstLine, "response1") {
		t.Fatalf("startLine %d points to a preceding message line: %q", region.startLine, firstLine)
	}

	m.viewport.GotoBottom()
	y := region.startLine - m.viewport.YOffset() + appHeaderHeight + 1
	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: y})
	updated, _ = updated.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: 4, Y: y})
	got := derefTestModel(t, updated)
	if !got.expandedToolOutputs[len(precedingMessages)] {
		t.Fatalf("expected click to expand tool output at index %d", len(precedingMessages))
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
		viewport:  fastviewport.New(96, 24),
		styles:    ApplyThemeColors("tokyonight"),
		messages:  []message{{role: roleAssistant, text: text, raw: &agent.Message{Role: "tool", ToolID: toolID, Content: content}}},
		sessionID: "test",
	}
	m.renderTranscript()

	if strings.Count(m.viewport.View(), "progress line") > toolOutputPreviewLines {
		t.Fatalf("expected sanitized collapsed output to stay within preview lines, got view:\n%s", m.viewport.View())
	}
}

func TestRenderReadToolOutputBoxExpandsInline(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("alpha line %02d", i+1)
	}
	content := strings.Join(lines, "\n")
	m := model{
		viewport: fastviewport.New(96, 24),
		styles:   ApplyThemeColors("tokyonight"),
	}

	collapsed := m.renderToolOutputBox("read", content, false)
	if got := strings.Count(collapsed, "alpha line"); got != toolOutputPreviewLines {
		t.Fatalf("expected collapsed read output box to show %d lines, got %d:\n%s", toolOutputPreviewLines, got, collapsed)
	}
	if !strings.Contains(collapsed, "alpha line 09") || !strings.Contains(collapsed, "alpha line 20") || strings.Contains(collapsed, "alpha line 08") {
		t.Fatalf("expected collapsed read output box to show only the last %d lines:\n%s", toolOutputPreviewLines, collapsed)
	}

	expanded := m.renderToolOutputBox("read", content, true)
	if got := strings.Count(expanded, "alpha line"); got != len(lines) {
		t.Fatalf("expected expanded read output box to show %d lines, got %d:\n%s", len(lines), got, expanded)
	}
	if !strings.Contains(expanded, "alpha line 20") {
		t.Fatalf("expected expanded read output box to include the full result:\n%s", expanded)
	}
}
