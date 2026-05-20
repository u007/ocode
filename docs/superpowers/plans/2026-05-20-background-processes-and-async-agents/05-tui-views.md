# Part 5 — TUI: Counts, Agent Strip & Drill-in Views

> REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans.

**Produces:** status-bar job counts, the live agent strip above the input
prompt, a recursive view-stack for drill-in (agent transcript + process logs),
mouse hit-testing, and modal-over-stack precedence.

**Prerequisite:** Parts 1–4 merged (`Agent.Procs()`, `Agent.Runs()` with
`Snapshot()` / `RunningCount()`).

**Key files:**
- Create: `internal/tui/detail_view.go`
- Create: `internal/tui/detail_view_test.go`
- Modify: `internal/tui/model.go` (model field, render routing, esc, mouse)

Bubble Tea / lipgloss are from `charm.land/...v2` (see existing imports in
`model.go`). `viewport.Model` is the scrollable primitive already used for the
transcript.

---

## Task 1: View-stack data structures + push/pop

**Files:**
- Create: `internal/tui/detail_view.go`
- Test: `internal/tui/detail_view_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/detail_view_test.go`:

```go
package tui

import "testing"

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestDetailStack -v`
Expected: FAIL — `detailStack`, `detailView`, kind constants undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/detail_view.go`:

```go
package tui

import "charm.land/bubbles/v2/viewport"

// detailViewKind enumerates the recursive drill-in screens.
type detailViewKind int

const (
	detailAgentRun     detailViewKind = iota // one async subagent's transcript
	detailProcessList                        // list of processes in a registry
	detailProcessLog                         // one process's streamed output
)

// detailView is one entry on the drill-in stack.
type detailView struct {
	kind   detailViewKind
	runID  string // set for detailAgentRun and process views scoped to a run
	procID string // set for detailProcessLog
	vp     viewport.Model
}

// detailStack is a LIFO stack of drill-in views. The empty stack means the
// normal tabbed UI is showing.
type detailStack []detailView

func (s detailStack) empty() bool { return len(s) == 0 }

func (s detailStack) top() (detailView, bool) {
	if len(s) == 0 {
		return detailView{}, false
	}
	return s[len(s)-1], true
}

func (s *detailStack) push(v detailView) { *s = append(*s, v) }

func (s *detailStack) pop() {
	if len(*s) == 0 {
		return
	}
	*s = (*s)[:len(*s)-1]
}
```

If the `viewport` import path differs, copy it verbatim from the top of
`internal/tui/model.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestDetailStack -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/detail_view.go internal/tui/detail_view_test.go
git commit -m "feat(tui): add recursive detail-view stack"
```

---

## Task 2: Status-bar job counts

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add the model field**

In the `model` struct, add:

```go
	detail detailStack
```

- [ ] **Step 2: Add a counts helper**

Near `renderActivityRow` in `model.go`, add:

```go
// jobCounts returns the number of running background processes and agent runs.
func (m model) jobCounts() (procs, agents int) {
	if m.agent == nil {
		return 0, 0
	}
	if m.agent.Procs() != nil {
		procs = m.agent.Procs().RunningCount()
	}
	if m.agent.Runs() != nil {
		agents = m.agent.Runs().RunningCount()
	}
	return procs, agents
}

// renderJobCounts renders the "▣ N bg · M agents" segment, or "" when idle.
func (m model) renderJobCounts() string {
	procs, agents := m.jobCounts()
	if procs == 0 && agents == 0 {
		return ""
	}
	return fmt.Sprintf("▣ %d bg · %d agents", procs, agents)
}
```

- [ ] **Step 3: Render it in the status bar**

In `renderStatus` (search for `func (m model) renderStatus`), append the job
counts segment to the status line. Use the same separator style the function
already uses for other segments — if `renderJobCounts()` returns non-empty,
join it with the existing content. Keep it on the right side of the status bar.

Example (adapt to the actual `renderStatus` body):

```go
	if jc := m.renderJobCounts(); jc != "" {
		segments = append(segments, jc)
	}
```

- [ ] **Step 4: Build to verify**

Run: `go build ./...`
Expected: build OK. Run the app, start a backgrounded `bash sleep 20`, confirm
`▣ 1 bg · 0 agents` appears in the status bar.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): show background job counts in status bar"
```

---

## Task 3: Agent strip above the input prompt

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add the strip renderer**

Near `renderActivityRow`, add:

```go
// agentStripBlock holds the screen rows occupied by one strip block, for
// mouse hit-testing.
type agentStripBlock struct {
	runID    string
	rowStart int // inclusive, screen-relative within the strip
	rowEnd   int // exclusive
}

// renderAgentStrip renders one block per running agent run: a header line plus
// the last 2 transcript lines. Returns the rendered string and the per-block
// row ranges (relative to the strip's first row).
func (m model) renderAgentStrip() (string, []agentStripBlock) {
	if m.agent == nil || m.agent.Runs() == nil {
		return "", nil
	}
	runs := m.agent.Runs().Snapshot()
	if len(runs) == 0 {
		return "", nil
	}
	width := m.panelWidth() - 2
	var b strings.Builder
	var blocks []agentStripBlock
	row := 0
	frame := spinnerFrames[m.dotFrame%len(spinnerFrames)]
	for _, ri := range runs {
		start := row
		status := string(ri.Status)
		icon := frame
		if ri.Status == agent.RunDone {
			icon = "✓"
		} else if ri.Status == agent.RunFailed {
			icon = "✗"
		}
		head := fmt.Sprintf("▸ %-10s %s %s", ri.Name, icon, status)
		b.WriteString(truncateToWidth(head, width) + "\n")
		row++
		for _, ln := range ri.LastLines {
			b.WriteString(hintStyle.Render("  │ " + truncateToWidth(ln, width-4)) + "\n")
			row++
		}
		blocks = append(blocks, agentStripBlock{runID: ri.ID, rowStart: start, rowEnd: row})
	}
	return strings.TrimRight(b.String(), "\n"), blocks
}
```

If the project has no `spinnerFrames` / `truncateToWidth`, define them locally
in this file:

```go
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func truncateToWidth(s string, w int) string {
	if w < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}
```

(Check `model.go` first — a dot/spinner frame set and a truncation helper may
already exist; reuse them instead of redefining.)

- [ ] **Step 2: Store blocks on the model for hit-testing**

In the `model` struct add:

```go
	agentStripBlocks []agentStripBlock
	agentStripRow0   int // screen row where the strip starts
```

- [ ] **Step 3: Insert the strip into `renderContent`**

In `renderContent`, in the chat-tab section, build `leftParts`. Immediately
**before** `leftParts = append(leftParts, inputArea)`, add:

```go
	if strip, blocks := m.renderAgentStrip(); strip != "" {
		leftParts = append(leftParts, strip)
		// store blocks; agentStripRow0 is computed in layout()
		// (handled via a model pointer method below)
	}
```

Because `renderContent` has a value receiver, store the blocks via a small
change: make `renderAgentStrip` results assigned in `layout()` instead. In
`layout()` (which has a pointer receiver), add:

```go
	_, m.agentStripBlocks = m.renderAgentStrip()
```

and in `renderContent`, call `m.renderAgentStrip()` only for the string.

- [ ] **Step 4: Build and verify**

Run: `go build ./...`
Expected: build OK. With a backgrounded agent running, confirm a strip block
with the agent name and 2 streaming lines appears directly above the input box.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): live agent strip above the input prompt"
```

---

## Task 4: Agent drill-in detail view

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/detail_view.go`

- [ ] **Step 1: Add a transcript renderer for a run**

In `internal/tui/detail_view.go`, add:

```go
import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"github.com/jamesmercstudio/ocode/internal/agent"
)

// renderRunTranscript formats an AgentRun's transcript for the detail viewport.
func renderRunTranscript(run *agent.AgentRun) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Agent %s (%s) — %s\n\n", run.ID, run.Name, run.Status))
	for _, m := range run.TranscriptPublic() {
		switch m.Role {
		case "assistant":
			if m.Content != "" {
				b.WriteString(m.Content + "\n\n")
			}
			for _, tc := range m.ToolCalls {
				b.WriteString("  ⚙ " + tc.Function.Name + " " + tc.Function.Arguments + "\n")
			}
		case "tool":
			b.WriteString("  ↳ " + truncateToWidth(strings.ReplaceAll(m.Content, "\n", " "), 200) + "\n")
		case "user", "system":
			b.WriteString("» " + m.Content + "\n\n")
		}
	}
	if run.Status == agent.RunDone {
		b.WriteString("\n── result ──\n" + run.Result + "\n")
	} else if run.Status == agent.RunFailed {
		b.WriteString("\n── error ──\n" + run.Err + "\n")
	}
	return b.String()
}
```

`update`/`view` for the viewport are handled by the model (Task 6).

- [ ] **Step 2: Expose the transcript on `AgentRun`**

In `internal/agent/agent_runs.go`, add a public accessor (the `transcript`
field is unexported):

```go
// TranscriptPublic returns a copy of the run's transcript.
func (r *AgentRun) TranscriptPublic() []Message { return r.transcriptCopy() }
```

- [ ] **Step 3: Push an agent detail view**

In `model.go`, add a method:

```go
// openAgentDetail pushes a drill-in view for the given run id.
func (m *model) openAgentDetail(runID string) {
	if m.agent == nil || m.agent.Runs() == nil {
		return
	}
	run, ok := m.agent.Runs().Get(runID)
	if !ok {
		return
	}
	vp := viewport.New()
	vp.SetWidth(m.panelWidth() - 4)
	vp.SetHeight(m.height - 6)
	vp.SetContent(renderRunTranscript(run))
	m.detail.push(detailView{kind: detailAgentRun, runID: runID, vp: vp})
}
```

(Match the `viewport.New(...)` signature used elsewhere in `model.go` — if it
takes width/height args, use that form instead of the setters.)

- [ ] **Step 4: Route rendering to the detail view**

In `renderContent`, at the very top after the `!m.ready` check and **after** the
modal checks (`showPicker`/`showConnect`/`showPalette`), add:

```go
	if top, ok := m.detail.top(); ok {
		return m.renderDetailView(top)
	}
```

Add `renderDetailView`:

```go
func (m model) renderDetailView(d detailView) string {
	var title string
	switch d.kind {
	case detailAgentRun:
		title = "Agent " + d.runID
	case detailProcessList:
		title = "Background processes"
	case detailProcessLog:
		title = "Process " + d.procID
	}
	header := m.styles.Header.Render("◆ " + title)
	hint := hintStyle.Render("  esc: back · j/k: scroll")
	body := borderStyle.Width(m.panelWidth() - 2).Render(d.vp.View())
	return lipgloss.JoinVertical(lipgloss.Left, header+hint, body)
}
```

- [ ] **Step 5: Build and verify**

Run: `go build ./...`
Expected: build OK. (Drill-in interaction is wired in Task 6.)

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/detail_view.go internal/agent/agent_runs.go
git commit -m "feat(tui): agent drill-in detail view"
```

---

## Task 5: Process list & process log views

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/detail_view.go`

- [ ] **Step 1: Add process renderers**

In `internal/tui/detail_view.go`, add:

```go
import "github.com/jamesmercstudio/ocode/internal/tool"

// renderProcessList formats a registry's processes for the list view.
func renderProcessList(reg *tool.ProcessRegistry) string {
	var b strings.Builder
	b.WriteString("Background processes (enter/click to open):\n\n")
	for _, pi := range reg.Snapshot() {
		line := fmt.Sprintf("  %-8s %-9s %s", pi.ID, pi.Status, pi.Command)
		if pi.Status != tool.ProcRunning {
			line += fmt.Sprintf("  (exit %d)", pi.ExitCode)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// renderProcessLog formats one process's current output.
func renderProcessLog(reg *tool.ProcessRegistry, id string) string {
	text, status, code, err := reg.Output(id)
	if err != nil {
		return "Error: " + err.Error()
	}
	header := fmt.Sprintf("Process %s — %s", id, status)
	if status != tool.ProcRunning {
		header += fmt.Sprintf(" (exit %d)", code)
	}
	return header + "\n\n" + text
}
```

Note: `reg.Output` advances the read cursor. For a persistent log view, add a
non-advancing reader to `ProcessRegistry` in `internal/tool/process.go`:

```go
// Dump returns the full current buffer without advancing any cursor.
func (r *ProcessRegistry) Dump(id string) (string, ProcStatus, int, error) {
	r.mu.Lock()
	p, ok := r.procs[id]
	r.mu.Unlock()
	if !ok {
		return "", "", 0, fmt.Errorf("unknown process id %q", id)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := ""
	if p.dropped > 0 {
		out = fmt.Sprintf("[…truncated %d bytes]\n", p.dropped)
	}
	out += string(p.buf)
	return out, p.Status, p.ExitCode, nil
}
```

Then make `renderProcessLog` call `reg.Dump(id)` instead of `reg.Output(id)`.

- [ ] **Step 2: Add push helpers in `model.go`**

```go
func (m *model) openProcessList() {
	if m.agent == nil || m.agent.Procs() == nil {
		return
	}
	vp := viewport.New()
	vp.SetWidth(m.panelWidth() - 4)
	vp.SetHeight(m.height - 6)
	vp.SetContent(renderProcessList(m.agent.Procs()))
	m.detail.push(detailView{kind: detailProcessList, vp: vp})
}

func (m *model) openProcessLog(procID string) {
	if m.agent == nil || m.agent.Procs() == nil {
		return
	}
	vp := viewport.New()
	vp.SetWidth(m.panelWidth() - 4)
	vp.SetHeight(m.height - 6)
	vp.SetContent(renderProcessLog(m.agent.Procs(), procID))
	m.detail.push(detailView{kind: detailProcessLog, procID: procID, vp: vp})
}
```

- [ ] **Step 3: Refresh detail viewports on tick**

Detail views must update live. In the `dotTickMsg` case in `Update`, after the
frame increment, refresh the top detail view content:

```go
		if top, ok := m.detail.top(); ok {
			switch top.kind {
			case detailAgentRun:
				if run, ok := m.agent.Runs().Get(top.runID); ok {
					m.detail[len(m.detail)-1].vp.SetContent(renderRunTranscript(run))
				}
			case detailProcessList:
				m.detail[len(m.detail)-1].vp.SetContent(renderProcessList(m.agent.Procs()))
			case detailProcessLog:
				m.detail[len(m.detail)-1].vp.SetContent(renderProcessLog(m.agent.Procs(), top.procID))
			}
		}
```

Also ensure the `dotTickMsg` self-reschedule condition keeps ticking while a
detail view is open: change its `if` guard to also include
`|| !m.detail.empty()`.

- [ ] **Step 4: Build and verify**

Run: `go build ./... && go test ./...`
Expected: build OK, tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/detail_view.go internal/tool/process.go
git commit -m "feat(tui): process list and process log detail views"
```

---

## Task 6: Navigation — keys, mouse, esc, modal precedence

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/detail_view_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/detail_view_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestBlockAtRow -v`
Expected: FAIL — `blockAtRow` undefined.

- [ ] **Step 3: Implement `blockAtRow`**

In `internal/tui/detail_view.go`:

```go
// blockAtRow returns the run id of the strip block containing the given
// strip-relative row, or "" if none.
func blockAtRow(blocks []agentStripBlock, row int) string {
	for _, b := range blocks {
		if row >= b.rowStart && row < b.rowEnd {
			return b.runID
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestBlockAtRow -v`
Expected: PASS.

- [ ] **Step 5: Esc with modal precedence**

In `handleEscKey`, add a detail-stack pop **after** the streaming-cancel block
and **before** the double-esc message-picker logic:

```go
	if !m.detail.empty() {
		m.detail.pop()
		return m, nil
	}
```

Modal precedence is automatic: modal overlays (`showPicker`/`showConnect`/
`showPalette`) are checked first in `renderContent` and their key handlers run
in `handleModalKeys` before `handleEscKey` is reached, so a modal's `esc`
closes the modal, not the detail stack.

- [ ] **Step 6: Block pushing a detail view while a modal is open**

Add a guard helper and use it in the open* methods:

```go
func (m model) modalOpen() bool {
	return m.showPicker || m.showConnect || m.showPalette || m.showPermDialog
}
```

At the start of `openAgentDetail`, `openProcessList`, `openProcessLog`, add:

```go
	if m.modalOpen() {
		return
	}
```

- [ ] **Step 7: Keyboard shortcut to open the process list**

In `handleChatKeys` (the chat-tab key handler), add a binding — use a key not
already bound; `ctrl+b` ("background"):

```go
	case "ctrl+b":
		m.openProcessList()
		return m, nil
```

(If `ctrl+b` is taken, pick another free key and note it in the help text at
model.go:3494-area.)

- [ ] **Step 8: Scroll keys inside a detail view**

In `Update`, in the key-handling path, before normal chat keys, add: if
`!m.detail.empty()`, route `j`/`k`/up/down/pageup/pagedown to the top viewport:

```go
	if !m.detail.empty() {
		switch msg.String() {
		case "j", "down":
			m.detail[len(m.detail)-1].vp.ScrollDown(m.scrollSpeed)
			return m, nil
		case "k", "up":
			m.detail[len(m.detail)-1].vp.ScrollUp(m.scrollSpeed)
			return m, nil
		case "esc":
			return m.handleEscKey()
		}
	}
```

Place this guard where other key routing happens in `Update`/`handleChatKeys`,
ahead of the normal chat key switch.

- [ ] **Step 9: Mouse hit-testing**

In `handleMouseAction` (model.go:1417), in the `pressed && m.activeTab == tabChat`
section, add hit-testing for the agent strip and the status-bar counts:

```go
	if pressed && m.activeTab == tabChat && m.detail.empty() {
		// Agent strip: rows [agentStripRow0, agentStripRow0+len(stripLines)).
		if id := blockAtRow(m.agentStripBlocks, mouse.Y-m.agentStripRow0); id != "" {
			m.openAgentDetail(id)
			return m, nil, true
		}
		// Status-bar job counts: the status row is the last screen row.
		if mouse.Y == m.height-1 && m.renderJobCounts() != "" {
			m.openProcessList()
			return m, nil, true
		}
	}
```

`m.agentStripRow0` must be set in `layout()` to the screen row where the strip
begins. Compute it from the cumulative height of `header`, `transcript`, and any
slash-popup/queue/stopped rows that precede the strip — `layout()` already
measures these for the activity row; reuse the same accumulation and record the
offset just before the strip is appended.

Inside a detail view, route mouse wheel to the top viewport: in
`handleMouseWheel`/`MouseWheelMsg` handling, if `!m.detail.empty()`, scroll
`m.detail[len(m.detail)-1].vp` instead of the transcript.

Clicking a row in `detailProcessList` opens that process's log: in
`handleMouseAction`, when `top.kind == detailProcessList`, map `mouse.Y` to a
process line (account for the 2-line header in `renderProcessList`) and call
`m.openProcessLog(id)`.

- [ ] **Step 10: Build, regression, manual check**

Run: `go build ./... && go test ./...`
Expected: build OK, tests PASS.

Manual: with a backgrounded agent and a backgrounded process running —
- click the agent strip block → agent transcript opens, scrollable, `esc` returns;
- press `ctrl+b` → process list opens; click a process → its log opens;
- `esc` pops back one level each press;
- open the model picker while a detail view is open → `esc` closes the picker
  first, the detail view second.

- [ ] **Step 11: Commit**

```bash
git add internal/tui/model.go internal/tui/detail_view.go internal/tui/detail_view_test.go
git commit -m "feat(tui): drill-in navigation via keys, mouse, and esc stack"
```

---

## Done

After Part 5, run the full suite once more:

```bash
go build ./... && go test ./...
```

The feature is complete: background bash + async agents, poll/wait tools,
push-on-completion with auto-resume, and recursive TUI drill-in with a live
agent strip and job counts.
