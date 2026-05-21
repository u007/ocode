package tui

import (
	"strings"
	"testing"

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
