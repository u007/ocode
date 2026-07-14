package tui

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
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
	a := agent.NewAgent(nil, nil, nil, nil)
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
	if !strings.Contains(strip, "more agent") {
		t.Fatal("expected a 'more agents below' indicator with 20 runs")
	}
}

// TestAgentStripScrollVisibility verifies the selected run stays inside the
// visible window after clamping.
func TestAgentStripScrollVisibility(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
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

// TestAgentsTabListsRunsNewestFirstAndClickOpens verifies the agents tab lists
// every run newest-first and that clicking a card opens its transcript drill-in.
func TestAgentsTabListsRunsNewestFirstAndClickOpens(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	r1 := a.Runs().New("alpha")
	r2 := a.Runs().New("beta") // newest

	m := model{
		agent:     a,
		width:     100,
		height:    40,
		activeTab: tabAgents,
		input:     newTestTextarea(),
		viewport:  fastviewport.New(96, 20),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.layoutAgentsViewport() // sizes the viewport and rebuilds content + click map

	content := stripANSI(m.agentsViewport.View())
	if !strings.Contains(content, "alpha") || !strings.Contains(content, "beta") {
		t.Fatalf("expected both runs listed, got:\n%s", content)
	}
	if strings.Index(content, "beta") > strings.Index(content, "alpha") {
		t.Fatalf("expected newest-first order (beta before alpha):\n%s", content)
	}

	if len(m.agentsBlocks) < 2 {
		t.Fatalf("expected 2 click blocks, got %d", len(m.agentsBlocks))
	}
	if id := blockAtRow(m.agentsBlocks, m.agentsBlocks[0].rowStart); id != r2.ID {
		t.Fatalf("expected top card to map to newest run %s, got %s", r2.ID, id)
	}

	// A click on the top card row opens that run's drill-in (YOffset is 0 here).
	top := m.agentsContentTopY()
	updated, _, ok := m.handleMouseAction(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: top}, true)
	if !ok {
		t.Fatal("expected the click to be handled")
	}
	gm := derefTestModel(t, updated)
	dv, present := gm.detail.top()
	if !present {
		t.Fatal("expected a detail view after clicking a card")
	}
	if dv.runID != r2.ID {
		t.Fatalf("expected drill-in for newest run %s, got %s", r2.ID, dv.runID)
	}
	_ = r1
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
	a := agent.NewAgent(nil, nil, nil, nil)
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

// TestDetailScrollbarColumnMatchesHitTest guards the agent-detail scrollbar
// drag: the column where renderDetailView paints the scrollbar must equal the
// column detailScrollbarX() hit-tests, or pressing the scrollbar never starts a
// drag (the click falls through and is swallowed).
func TestDetailScrollbarColumnMatchesHitTest(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	run := a.Runs().New("worker")
	var msgs []agent.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs, agent.Message{Role: "assistant", Content: "transcript line content"})
	}
	setRunTranscriptForTest(run, msgs...)

	m := model{
		agent:  a,
		width:  80,
		height: 24,
		styles: ApplyThemeColors("tokyonight"),
	}
	m.openAgentDetail(run.ID)

	rendered := stripANSI(m.renderDetailView(m.detail[0]))
	col := -1
	for _, ln := range strings.Split(rendered, "\n") {
		idx := strings.IndexAny(ln, scrollbarThumb+scrollbarTrack)
		if idx >= 0 {
			col = lipgloss.Width(ln[:idx])
			break
		}
	}
	if col < 0 {
		t.Fatalf("scrollbar glyph not found in detail view:\n%s", rendered)
	}
	if col != m.detailScrollbarX() {
		t.Fatalf("scrollbar rendered at column %d but detailScrollbarX()=%d", col, m.detailScrollbarX())
	}
}

// TestDetailViewMouseStartsTextSelection guards that pressing on detail-view
// content begins a text-selection drag (previously the press was swallowed
// before the selection-start code ran, so text could not be selected).
func TestDetailViewMouseStartsTextSelection(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	run := a.Runs().New("worker")
	var msgs []agent.Message
	for i := 0; i < 40; i++ {
		msgs = append(msgs, agent.Message{Role: "assistant", Content: "transcript line content"})
	}
	setRunTranscriptForTest(run, msgs...)

	m := model{ready: true, width: 80, height: 24, activeTab: tabChat, input: newTestTextarea(), styles: ApplyThemeColors("tokyonight"), agent: a}
	m.openAgentDetail(run.ID)

	contentTop := m.detailViewportContentTopY()
	pressY := contentTop + 1
	x := detailContentLeftX + 2

	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: pressY})
	got := derefTestModel(t, updated)
	top := got.detail[len(got.detail)-1]
	if !top.sel.dragging {
		t.Fatal("press on detail content should start a selection drag")
	}

	updated, _ = got.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: x + 5, Y: pressY + 2})
	got = derefTestModel(t, updated)
	top = got.detail[len(got.detail)-1]
	if !top.sel.active {
		t.Fatal("dragging across rows should produce an active selection")
	}
	if top.sel.startLine == top.sel.endLine && top.sel.startCol == top.sel.endCol {
		t.Fatalf("selection did not extend: start=(%d,%d) end=(%d,%d)", top.sel.startLine, top.sel.startCol, top.sel.endLine, top.sel.endCol)
	}
}

// TestDetailSelectionSurvivesLiveRefresh guards that a live transcript tick
// (refreshTopDetailView) does not wipe an in-progress selection drag — the
// case that matters for a streaming orchestral sub-agent the user is watching.
func TestDetailSelectionSurvivesLiveRefresh(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	run := a.Runs().New("worker")
	var msgs []agent.Message
	for i := 0; i < 40; i++ {
		msgs = append(msgs, agent.Message{Role: "assistant", Content: "transcript line content"})
	}
	setRunTranscriptForTest(run, msgs...)

	m := model{ready: true, width: 80, height: 24, activeTab: tabChat, input: newTestTextarea(), styles: ApplyThemeColors("tokyonight"), agent: a}
	m.openAgentDetail(run.ID)

	pressY := m.detailViewportContentTopY() + 1
	x := detailContentLeftX + 2
	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: pressY})
	m = derefTestModel(t, updated)
	if !m.detail[len(m.detail)-1].sel.dragging {
		t.Fatal("precondition: press should start a selection drag")
	}

	// A live refresh tick must not clear the drag-in-progress selection.
	m.refreshTopDetailView()
	if !m.detail[len(m.detail)-1].sel.dragging {
		t.Fatal("live refresh wiped the in-progress selection drag")
	}
}

// TestDetailScrollbarThumbDragScrolls proves the column fix actually enables
// dragging: pressing the thumb then moving the mouse changes the viewport
// offset.
func TestDetailScrollbarThumbDragScrolls(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	run := a.Runs().New("worker")
	var msgs []agent.Message
	for i := 0; i < 200; i++ {
		msgs = append(msgs, agent.Message{Role: "assistant", Content: "transcript line content"})
	}
	setRunTranscriptForTest(run, msgs...)

	m := model{ready: true, width: 80, height: 24, activeTab: tabChat, input: newTestTextarea(), styles: ApplyThemeColors("tokyonight"), agent: a}
	m.openAgentDetail(run.ID)
	top := &m.detail[len(m.detail)-1]
	top.vp.GotoTop()
	startOffset := top.vp.YOffset()

	trackTop, _ := m.detailScrollbarMetrics()
	sbX := m.detailScrollbarX()

	// Press on the thumb (top of the track, where the thumb sits at offset 0).
	updated, _, ok := m.handleMouseAction(tea.MouseClickMsg{Button: tea.MouseLeft, X: sbX, Y: trackTop}.Mouse(), true)
	if !ok {
		t.Fatal("press on scrollbar thumb was not handled")
	}
	m = derefTestModel(t, updated)
	if m.scrollbarDrag != scrollbarDragDetail {
		t.Fatalf("thumb press should start a detail scrollbar drag, got %v", m.scrollbarDrag)
	}

	// Drag downward — the viewport must scroll.
	updated, _, ok = m.handleMouseMotion(tea.MouseMotionMsg{Button: tea.MouseLeft, X: sbX, Y: trackTop + 8}.Mouse())
	if !ok {
		t.Fatal("scrollbar drag motion was not handled")
	}
	m = derefTestModel(t, updated)
	if got := m.detail[len(m.detail)-1].vp.YOffset(); got <= startOffset {
		t.Fatalf("dragging thumb down should increase offset: start=%d got=%d", startOffset, got)
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
	a := agent.NewAgent(nil, nil, nil, nil)
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

// TestRenderRunTranscriptWrapsMarkdownToWidth guards against the detail-view
// markdown wrap regression: assistant/user message bodies containing long
// lines (e.g. markdown tables) were rendered via renderMarkdownInLine — which
// is a single-line renderer — on multi-line text, then appended WITHOUT
// wrapping. lipgloss pads multi-line renders into a rectangular block, so a
// short trailing line ("| ") got padded out and the following bold cell was
// pushed far to the right, producing over-width lines and mangled output.
// Every rendered row must fit within the requested width.
func TestRenderRunTranscriptWrapsMarkdownToWidth(t *testing.T) {
	run := &agent.AgentRun{ID: "agent-1", Name: "worker", Status: agent.RunRunning}
	table := strings.Join([]string{
		"Here is the plan:",
		"",
		"| Feature Area | What's Tested |",
		"|---|---|",
		"| **Order Management** | Create, update, cancel, track medical supply orders; backorders; partial fulfilment; order status lifecycle |",
		"| **Logistics & Routing** | Route planning, delivery scheduling, fleet assignment, last-mile tracking, proof of delivery |",
	}, "\n")
	setRunTranscriptForTest(run,
		agent.Message{Role: "user", Content: "make a plan"},
		agent.Message{Role: "assistant", Content: table},
	)
	const width = 100
	rendered := stripANSI(renderRunTranscript(run, width))
	for _, ln := range strings.Split(rendered, "\n") {
		if lipgloss.Width(ln) > width {
			t.Fatalf("detail row exceeds width %d (got %d): %q", width, lipgloss.Width(ln), ln)
		}
	}
}

func setRunTranscriptForTest(run *agent.AgentRun, msgs ...agent.Message) {
	v := reflect.ValueOf(run).Elem().FieldByName("transcript")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().Set(reflect.ValueOf(msgs))
}

// TestAgentStripClickOpensDetail verifies the agent preview strip near the
// bottom of the chat tab is clickable: the screen-Y the click handler derives
// from agentStripTopY must match where View() actually paints the strip, and a
// click there must open the run's detail view.
func TestAgentStripClickOpensDetail(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	run := a.Runs().New("worker")
	setRunTranscriptForTest(run, agent.Message{Role: "assistant", Content: "did some work"})

	m := model{
		ready:       true,
		width:       120,
		height:      40,
		activeTab:   tabChat,
		input:       newTestTextarea(),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
		agent:       a,
	}
	m.viewport = fastviewport.New(80, 20)
	m.layout()

	strip, blocks := m.renderAgentStrip()
	if strip == "" || len(blocks) == 0 {
		t.Fatalf("expected non-empty agent strip, got strip=%q blocks=%d", strip, len(blocks))
	}

	// Where the click handler thinks the first run block sits.
	clickY := m.agentStripTopY() + blocks[0].rowStart

	// Where View() actually paints the run header line.
	lines := strings.Split(m.renderContent(), "\n")
	headerY := -1
	for i, ln := range lines {
		if strings.Contains(stripANSI(ln), "▸ worker") {
			headerY = i
			break
		}
	}
	if headerY < 0 {
		t.Fatal("could not find run header in rendered content")
	}
	if headerY != clickY {
		t.Fatalf("geometry mismatch: View paints strip header at screen Y=%d but click handler targets Y=%d", headerY, clickY)
	}

	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: clickY})
	got := derefTestModel(t, updated)
	if len(got.detail) == 0 {
		t.Fatalf("expected click at strip Y=%d to open agent detail, but detail stack is empty", clickY)
	}
}

// TestAgentStripClickableAfterStripGrows reproduces the streaming regression:
// the strip is sized once at layout() time, but a sub-agent run grows it
// afterwards (the 400ms dotTick that drives the strip never re-runs layout).
// The grown strip overflows m.height, renderContent's safety net shrinks the
// transcript viewport and paints the strip higher, while the click handler's
// agentStripTopY still uses the stale (larger) viewport height — so a click on
// the visible strip lands above where the handler looks and is swallowed by the
// transcript-selection handler.
func TestAgentStripClickableAfterStripGrows(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)

	m := model{
		ready:       true,
		width:       120,
		height:      24,
		activeTab:   tabChat,
		input:       newTestTextarea(),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
		agent:       a,
	}
	m.viewport = fastviewport.New(80, 20)
	// Fill the transcript so the viewport wants the whole screen — any strip
	// growth then overflows m.height and trips the safety net.
	var content []string
	for i := 0; i < 200; i++ {
		content = append(content, "transcript filler line")
	}
	m.transcriptLines = content
	m.viewport.SetContentLines(m.transcriptLines)

	// One small run present when layout() sizes the viewport.
	first := a.Runs().New("worker")
	setRunTranscriptForTest(first, agent.Message{Role: "assistant", Content: "step one"})
	m.layout()

	// Now several more runs appear (sub-agent fan-out) WITHOUT a re-layout,
	// growing the strip by several rows — exactly what dotTick does.
	for i := 0; i < 5; i++ {
		r := a.Runs().New("worker")
		setRunTranscriptForTest(r, agent.Message{Role: "assistant", Content: "more work"})
	}

	_, blocks := m.renderAgentStrip()
	if len(blocks) == 0 {
		t.Fatal("expected agent strip blocks after growth")
	}

	// A real user clicks the row they SEE. Find where View() actually paints the
	// first run-header line and click there.
	lines := strings.Split(m.renderContent(), "\n")
	paintedY := -1
	for i, ln := range lines {
		if strings.Contains(stripANSI(ln), "▸ worker") {
			paintedY = i
			break
		}
	}
	if paintedY < 0 {
		t.Fatal("strip not painted in rendered content")
	}

	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: paintedY})
	got := derefTestModel(t, updated)
	if len(got.detail) == 0 {
		t.Fatalf("strip not clickable after growth: click on the visible strip header (screen Y=%d) "+
			"did not open detail; handler's agentStripTopY=%d drifted from the painted position "+
			"because viewport height is stale at %d while the safety net shrank it for paint",
			paintedY, m.agentStripTopY(), m.viewport.Height())
	}
}
