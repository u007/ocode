package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// TestBtwResultMsgLiveUpdatesAppendAndFinalReplaces verifies the /btw popup
// update flow: live tool-activity lines and streamed text append while the
// dialog stays loading; the final result replaces the accumulated body and
// clears loading. Stale generations are ignored.
func TestBtwResultMsgLiveUpdatesAppendAndFinalReplaces(t *testing.T) {
	m := model{
		styles:        ApplyThemeColors("tokyonight"),
		btwCh:         make(chan btwResultMsg, 64),
		btwGen:        7,
		width:         120,
		height:        40,
		showBtwDialog: true,
		btwLoading:    true,
		input:         newTestTextarea(),
		viewport:      fastviewport.New(80, 20),
	}
	_ = m.layout

	// Live tool-activity line.
	got, _ := m.Update(btwResultMsg{gen: 7, text: "→ read: internal/agent/ask.go\n", live: true, activity: true})
	m2 := got.(model)
	if !m2.btwLoading {
		t.Fatal("live update must keep the dialog loading")
	}
	if !strings.Contains(m2.btwActivity, "→ read: internal/agent/ask.go") {
		t.Fatalf("btwActivity = %q, want the tool-activity line", m2.btwActivity)
	}

	// Streamed text token.
	got, _ = m2.Update(btwResultMsg{gen: 7, text: "Think", live: true})
	m2 = got.(model)
	if !strings.Contains(m2.btwAnswer, "Think") {
		t.Fatalf("btwAnswer = %q, want streamed text appended", m2.btwAnswer)
	}

	// Stale generation is dropped.
	got, _ = m2.Update(btwResultMsg{gen: 8, text: "stale", live: true})
	m2 = got.(model)
	if strings.Contains(m2.btwAnswer, "stale") {
		t.Fatal("stale-generation live update mutated the popup")
	}

	// Final result replaces the accumulated body and stops loading.
	got, _ = m2.Update(btwResultMsg{gen: 7, text: "final answer"})
	m2 = got.(model)
	if m2.btwLoading {
		t.Fatal("final result must clear loading")
	}
	if m2.btwAnswer != "final answer" {
		t.Fatalf("btwAnswer = %q, want the final answer to replace accumulated text", m2.btwAnswer)
	}
	// Tool activity survives the final result (the answer references it).
	if !strings.Contains(m2.btwActivity, "→ read:") {
		t.Fatalf("btwActivity = %q, want activity preserved after the final result", m2.btwActivity)
	}

	// Final error surfaces.
	got, _ = m2.Update(btwResultMsg{gen: 7, err: "boom"})
	m2 = got.(model)
	if !strings.Contains(m2.btwAnswer, "boom") {
		t.Fatalf("btwAnswer = %q, want error text", m2.btwAnswer)
	}
}

// TestBtwDismissCancelsRunningLoop verifies dismissing the /btw popup
// (enter/esc) calls the stored cancel func so the side-query loop stops.
func TestBtwDismissCancelsRunningLoop(t *testing.T) {
	cancelled := false
	m := model{
		styles:        ApplyThemeColors("tokyonight"),
		showBtwDialog: true,
		btwLoading:    true,
		btwQuestion:   "why?",
		btwAnswer:     "partial",
		btwActivity:   "→ read: x\n",
		btwCancel: func() {
			cancelled = true
		},
		btwCh: make(chan btwResultMsg, 64),
	}

	got, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	m2 := got.(model)
	if !cancelled {
		t.Fatal("dismissing the popup must cancel the running side-query loop")
	}
	if m2.showBtwDialog {
		t.Fatal("popup stayed open after dismiss")
	}
	if m2.btwLoading {
		t.Fatal("dismiss must clear loading")
	}
	if m2.btwCancel != nil {
		t.Fatal("dismiss must nil the stored cancel func")
	}
	if m2.btwAnswer != "" || m2.btwActivity != "" || m2.btwQuestion != "" {
		t.Fatalf("dismiss must clear popup state: answer=%q activity=%q question=%q", m2.btwAnswer, m2.btwActivity, m2.btwQuestion)
	}
}

// TestBtwEscDismissesWithoutCancellingNil verifies Esc dismiss works when no
// loop is running (btwCancel nil) — no panic, state cleared.
func TestBtwEscDismissesWithoutCancellingNil(t *testing.T) {
	m := model{
		styles:        ApplyThemeColors("tokyonight"),
		showBtwDialog: true,
		btwLoading:    false,
		btwAnswer:     "done",
		btwCh:         make(chan btwResultMsg, 64),
	}
	got, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEscape}, nil, nil)
	m2 := got.(model)
	if m2.showBtwDialog {
		t.Fatal("esc must dismiss the popup")
	}
	if m2.btwAnswer != "" {
		t.Fatal("esc must clear the popup body")
	}
}

// TestBtwToolsExcludesInteractiveAndDispatch verifies the /btw tool set drops
// question (would block on a dialog), the task family (recursive dispatch),
// todo writes (main-agent-owned), plan files, and wait.
func TestBtwToolsExcludesInteractiveAndDispatch(t *testing.T) {
	m := model{styles: ApplyThemeColors("tokyonight")}
	tools := m.btwTools()
	names := map[string]bool{}
	for _, tt := range tools {
		names[tt.Name()] = true
	}
	for _, excluded := range []string{"question", "task", "task_status", "agent_status", "task_cancel", "wait", "todo_write", "todo_update", "plan_enter", "plan_exit", "discover_more"} {
		if names[excluded] {
			t.Errorf("btw tool set must exclude %q, got it", excluded)
		}
	}
	// The exploration core must be present.
	for _, want := range []string{"read", "grep", "glob", "list", "bash", "webfetch"} {
		if !names[want] {
			t.Errorf("btw tool set must include %q", want)
		}
	}
}

// TestFormatBtwActivity verifies activity-line formatting for tool calls.
func TestFormatBtwActivity(t *testing.T) {
	tc := agent.ToolCall{ID: "c1", Type: "function"}
	tc.Function.Name = "read"
	tc.Function.Arguments = `{"path":"internal/agent/ask.go"}`
	am := agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}}
	line := formatBtwActivity(am)
	if !strings.Contains(line, "→ read") || !strings.Contains(line, "internal/agent/ask.go") {
		t.Fatalf("activity line = %q", line)
	}
	if formatBtwActivity(agent.Message{Role: "user", Content: "hi"}) != "" {
		t.Fatal("non-assistant message must produce no activity line")
	}
}

// TestBtwViewportSyncedDuringLayout verifies the /btw popup body is wrapped
// and loaded into a scrollable viewport during layout: content is populated,
// a long answer exceeds the viewport height (scrollable), and the body wraps
// at the dialog width.
func TestBtwViewportSyncedDuringLayout(t *testing.T) {
	// Long enough to wrap well past btwDialogMaxBodyLines (16 at height 40).
	longAnswer := strings.Repeat("the quick brown fox jumps over the lazy dog ", 120)
	m := model{
		ready:         true,
		width:         120,
		height:        40,
		showBtwDialog: true,
		btwLoading:    false,
		btwQuestion:   "what is the answer?",
		btwAnswer:     longAnswer,
		styles:        ApplyThemeColors("tokyonight"),
		input:         newTestTextarea(),
		viewport:      fastviewport.New(80, 20),
		btwViewport:   viewport.New(viewport.WithWidth(1), viewport.WithHeight(1)),
	}

	m.layout()

	if got := m.btwViewport.TotalLineCount(); got == 0 {
		t.Fatal("expected btw viewport content to be populated during layout")
	}
	if got := m.btwViewport.TotalLineCount(); got <= m.btwViewport.Height() {
		t.Fatalf("expected long answer to overflow the viewport (lines=%d height=%d)", got, m.btwViewport.Height())
	}
	// The body must be wrapped at the dialog content width, not one line per
	// raw newline — a single-line answer should produce multiple wrapped rows.
	single := model{
		ready:         true,
		width:         120,
		height:        40,
		showBtwDialog: true,
		btwLoading:    false,
		btwQuestion:   "q",
		btwAnswer:     strings.Repeat("word ", 30), // ~150 chars, no newlines
		styles:        ApplyThemeColors("tokyonight"),
		input:         newTestTextarea(),
		viewport:      fastviewport.New(80, 20),
		btwViewport:   viewport.New(viewport.WithWidth(1), viewport.WithHeight(1)),
	}
	single.layout()
	if got := single.btwViewport.TotalLineCount(); got < 2 {
		t.Fatalf("expected the long single-line answer to wrap into %d lines, want >= 2", got)
	}
}

// TestBtwViewportScrollKeys verifies Up/Down/j/k scroll the popup body and
// Enter/Esc still dismiss (no conflict between scrolling and dismissing).
func TestBtwViewportScrollKeys(t *testing.T) {
	longAnswer := strings.Repeat("line of text to scroll through ", 40)
	m := model{
		styles:        ApplyThemeColors("tokyonight"),
		showBtwDialog: true,
		btwLoading:    false,
		btwQuestion:   "q",
		btwAnswer:     longAnswer,
		scrollSpeed:   3,
		btwViewport:   viewport.New(viewport.WithWidth(40), viewport.WithHeight(3)),
		input:         newTestTextarea(),
		viewport:      fastviewport.New(80, 20),
	}
	m.syncBtwViewport(38)
	if m.btwViewport.TotalLineCount() <= m.btwViewport.Height() {
		t.Skip("answer too short to scroll in this test environment")
	}
	before := m.btwViewport.YOffset()

	// Down scrolls, Esc dismisses.
	got, _ := m.handleChatKeys(tea.KeyPressMsg{Code: 'j'}, nil, nil)
	m2 := got.(model)
	if m2.btwViewport.YOffset() <= before {
		t.Fatal("pressing j should scroll the btw popup down")
	}
	got, _ = m2.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEscape}, nil, nil)
	m2 = got.(model)
	if m2.showBtwDialog {
		t.Fatal("esc should still dismiss the popup after scrolling")
	}
}
