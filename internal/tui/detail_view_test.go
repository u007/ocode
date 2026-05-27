package tui

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

func TestDetailStackPushPop(t *testing.T) {
	var s detailStack
	if !s.empty() {
		t.Fatal("new stack should be empty")
	}
	s.push(detailView{kind: detailAgentRun, runID: "agent-1"})
	s.push(detailView{kind: detailProcessLog, procID: "proc-2"})
	if s.empty() {
		t.Fatal("stack should be non-empty")
	}
	top, ok := s.top()
	if !ok || top.kind != detailProcessLog || top.procID != "proc-2" {
		t.Fatalf("bad top: %+v ok=%v", top, ok)
	}
	s.pop()
	top, ok = s.top()
	if !ok || top.kind != detailAgentRun || top.runID != "agent-1" {
		t.Fatalf("after pop, bad top: %+v", top)
	}
	s.pop()
	if !s.empty() {
		t.Fatal("stack should be empty after popping all")
	}
	s.pop() // pop on empty must not panic
}

func TestBlockAtRow(t *testing.T) {
	blocks := []agentStripBlock{
		{runID: "agent-1", rowStart: 0, rowEnd: 3},
		{runID: "agent-2", rowStart: 3, rowEnd: 6},
	}
	if id := blockAtRow(blocks, 4); id != "agent-2" {
		t.Fatalf("row 4 → %q, want agent-2", id)
	}
	if id := blockAtRow(blocks, 1); id != "agent-1" {
		t.Fatalf("row 1 → %q, want agent-1", id)
	}
	if id := blockAtRow(blocks, 99); id != "" {
		t.Fatalf("row 99 → %q, want empty", id)
	}
}

// TestAgentStripRowCap verifies the agent strip never renders more than
// agentStripMaxRows worth of run/indicator rows even with many runs.
func TestAgentStripRowCap(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	for i := 0; i < 20; i++ {
		a.Runs().New("worker")
	}
	m := model{agent: a, width: 100}

	strip, blocks := m.renderAgentStrip()
	lines := strings.Split(strip, "\n")
	if len(lines) > agentStripMaxRows {
		t.Fatalf("strip rendered %d rows, cap is %d", len(lines), agentStripMaxRows)
	}
	if len(blocks) == 0 {
		t.Fatal("expected at least one rendered block")
	}
	if !strings.Contains(strip, "↓ more") {
		t.Fatal("expected a '↓ more' indicator with 20 runs")
	}
}

// TestAgentStripScrollVisibility verifies the selected run stays inside the
// visible window after clamping.
func TestAgentStripScrollVisibility(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	for i := 0; i < 20; i++ {
		a.Runs().New("worker")
	}
	m := model{agent: a, width: 100, agentStripFocused: true, agentStripSelected: 18}
	m.clampAgentStrip()

	count := m.agentStripVisibleCount(m.agentStripOffset)
	if m.agentStripSelected < m.agentStripOffset || m.agentStripSelected >= m.agentStripOffset+count {
		t.Fatalf("selected=%d not in visible window [%d,%d)", m.agentStripSelected, m.agentStripOffset, m.agentStripOffset+count)
	}
}

func TestRenderRunTranscriptUsesSingleSpacingBetweenSectionsAndEvents(t *testing.T) {
	run := &agent.AgentRun{
		ID:     "agent-1",
		Name:   "worker",
		Status: agent.RunDone,
		Result: "done",
	}
	setRunTranscriptForTest(run,
		agent.Message{Role: "user", Content: "first task"},
		agent.Message{Role: "assistant", Content: "first reply"},
	)

	rendered := stripANSI(renderRunTranscript(run, 80))
	if strings.Contains(rendered, "Timeline\n\n•") {
		t.Fatalf("timeline bullets should be single-spaced, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "• Task: first task\n\n• Agent: first reply") {
		t.Fatalf("agent messages should be single-spaced, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "Result\n\ndone") {
		t.Fatalf("result section should be single-spaced, got:\n%s", rendered)
	}
}

func TestDetailAgentViewFitsPanelWidth(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	run := a.Runs().New("worker")
	setRunTranscriptForTest(run,
		agent.Message{Role: "user", Content: strings.Repeat("x", 120)},
		agent.Message{Role: "assistant", Content: strings.Repeat("y", 120)},
	)

	m := model{
		agent:  a,
		width:  80,
		height: 24,
		styles: ApplyThemeColors("tokyonight"),
	}
	m.openAgentDetail(run.ID)
	if len(m.detail) != 1 {
		t.Fatalf("expected detail view to open, got %d entries", len(m.detail))
	}

	rendered := stripANSI(m.renderDetailView(m.detail[0]))
	for _, line := range strings.Split(rendered, "\n") {
		if got := lipgloss.Width(line); got > m.panelWidth() {
			t.Fatalf("detail line width %d exceeds panel width %d: %q", got, m.panelWidth(), line)
		}
	}
}

func TestRenderRunTranscriptShowsThinkingLLMToolRequestAndToolResult(t *testing.T) {
	run := &agent.AgentRun{
		ID:     "agent-1",
		Name:   "worker",
		Status: agent.RunDone,
	}
	setRunTranscriptForTest(run,
		agent.Message{Role: "assistant", ReasoningContent: "step 1\nstep 2\nstep 3\nstep 4", Content: "done thinking", ToolCalls: []agent.ToolCall{makeAgentToolCall("call-1", "bash", `{"command":"printf one\\ntwo\\nthree\\nfour\\nfive\\nsix\\nseven\\neight\\nnine"}`)}},
		agent.Message{Role: "tool", ToolID: "call-1", Content: strings.Repeat("tool line\n", 20)},
	)

	rendered := stripANSI(renderRunTranscript(run, 80))
	for _, want := range []string{"⟁ thinking", "LLM message", "tool request · bash", "tool result · bash"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered transcript to contain %q, got:\n%s", want, rendered)
		}
	}
	if !strings.Contains(rendered, "click to expand") {
		t.Fatalf("expected collapsed expandable sections, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "tool line\ntool line\ntool line\ntool line\ntool line\ntool line\ntool line\ntool line\ntool line\ntool line\ntool line\ntool line\ntool line") {
		t.Fatalf("expected collapsed tool output preview, got full content:\n%s", rendered)
	}
}

func TestAgentDetailClickTogglesExpandableTranscriptSection(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	run := a.Runs().New("worker")
	setRunTranscriptForTest(run,
		agent.Message{Role: "assistant", ReasoningContent: strings.Join([]string{"line 1", "line 2", "line 3", "line 4", "line 5", "line 6", "line 7", "line 8", "line 9"}, "\n")},
	)

	m := model{ready: true, width: 100, height: 28, activeTab: tabChat, input: newTestTextarea(), styles: ApplyThemeColors("tokyonight"), agent: a}
	m.openAgentDetail(run.ID)
	top := m.detail[len(m.detail)-1]
	if len(top.regions) == 0 {
		t.Fatal("expected clickable expandable region in detail view")
	}
	row := top.regions[0].rowStart

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: 2, Y: m.detailViewportContentTopY() + row})
	got := derefTestModel(t, updated)
	top = got.detail[len(got.detail)-1]
	if !top.expanded[top.regions[0].id] {
		t.Fatal("expected detail transcript region to expand after click")
	}
	if !strings.Contains(stripANSI(top.vp.View()), "click to collapse") {
		t.Fatalf("expected expanded detail transcript to show collapse affordance, got:\n%s", stripANSI(top.vp.View()))
	}
}

func setRunTranscriptForTest(run *agent.AgentRun, msgs ...agent.Message) {
	v := reflect.ValueOf(run).Elem().FieldByName("transcript")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().Set(reflect.ValueOf(msgs))
}
