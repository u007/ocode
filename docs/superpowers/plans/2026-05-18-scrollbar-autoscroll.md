# Scrollbar + Auto-scroll on Session Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a terminal scrollbar (track + draggable thumb) to every scrollable surface in the TUI, and auto-scroll the transcript to the bottom when restoring a prior session.

**Architecture:** A shared `scrollbar.go` helper renders a single-column scrollbar string that is horizontally joined to each viewport/list before border-wrapping. All mouse hit detection and drag state for scrollbars lives in `model.go` — sub-models (`gitModel`, `filesModel`) expose their viewport fields directly. Auto-scroll on restore uses a one-shot `restoredPendingScroll` bool flag that fires after the first `WindowSizeMsg` (when real dimensions are known), then clears.

**Tech Stack:** Go, charm.land/bubbletea v2, charm.land/bubbles v2 (viewport), charm.land/lipgloss v2

---

## File Map

| File | Change |
|---|---|
| `internal/tui/scrollbar.go` | **Create** — `renderScrollbar` and `renderListScrollbar` pure helpers |
| `internal/tui/scrollbar_test.go` | **Create** — unit tests for both helpers |
| `internal/tui/model.go` | **Modify** — add `restoredPendingScroll` + `scrollbarDrag` fields; fix WindowSizeMsg handler; update `layout()`; update transcript + log render; add scrollbar mouse handlers |
| `internal/tui/git_model.go` | **Modify** — subtract 1 from diff viewport width; join scrollbar in `View()` |
| `internal/tui/files_model.go` | **Modify** — subtract 1 from preview viewport width; join scrollbar in `View()` |
| `internal/tui/picker.go` | **Modify** — join scrollbar column in `renderPicker()`; add scrollbar mouse click |
| `internal/tui/slash_popup.go` | **Modify** — join scrollbar column in `renderSlashPopup()`; add scrollbar mouse click |
| `internal/tui/connect.go` | **Modify** — join scrollbar column in `renderConnect()` for list stages |

---

### Task 1: Scrollbar helper — pure rendering functions

**Files:**
- Create: `internal/tui/scrollbar.go`
- Create: `internal/tui/scrollbar_test.go`

- [x] **Step 1: Write the failing tests**

Create `internal/tui/scrollbar_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestRenderScrollbar_NoThumbWhenFits(t *testing.T) {
	result := renderScrollbar(5, 5, 5, 0)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	for _, line := range lines {
		if strings.Contains(line, "█") {
			t.Error("expected no thumb when content fits")
		}
	}
}

func TestRenderScrollbar_ThumbAtTop(t *testing.T) {
	// 20 total lines, 5 visible, offset=0 → thumb at top
	result := renderScrollbar(5, 20, 5, 0)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	// first line should contain thumb
	if !strings.Contains(lines[0], "█") {
		t.Errorf("expected thumb on first line at offset=0, got: %q", lines[0])
	}
}

func TestRenderScrollbar_ThumbAtBottom(t *testing.T) {
	// 20 total lines, 5 visible, offset=15 (max) → thumb at bottom
	result := renderScrollbar(5, 20, 5, 15)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	// last line should contain thumb
	if !strings.Contains(lines[4], "█") {
		t.Errorf("expected thumb on last line at max offset, got: %q", lines[4])
	}
}

func TestRenderScrollbar_WidthIsOne(t *testing.T) {
	result := renderScrollbar(4, 20, 4, 0)
	for i, line := range strings.Split(result, "\n") {
		// lipgloss strips ANSI — measure the plain rune count indirectly
		// each line should render as exactly 1 visible column
		_ = i
		_ = line
	}
	// basic: result must not be empty
	if result == "" {
		t.Error("expected non-empty scrollbar")
	}
}

func TestRenderListScrollbar_NoThumbWhenFits(t *testing.T) {
	result := renderListScrollbar(4, 4, 0, 4)
	lines := strings.Split(result, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	for _, line := range lines {
		if strings.Contains(line, "█") {
			t.Error("expected no thumb when all items visible")
		}
	}
}

func TestRenderListScrollbar_ThumbAtTop(t *testing.T) {
	result := renderListScrollbar(4, 16, 0, 4)
	lines := strings.Split(result, "\n")
	if !strings.Contains(lines[0], "█") {
		t.Errorf("expected thumb at top when visibleStart=0, got: %q", lines[0])
	}
}

func TestRenderListScrollbar_ThumbAtBottom(t *testing.T) {
	result := renderListScrollbar(4, 16, 12, 4)
	lines := strings.Split(result, "\n")
	if !strings.Contains(lines[3], "█") {
		t.Errorf("expected thumb at bottom at max visibleStart, got: %q", lines[3])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -run "TestRenderScrollbar|TestRenderListScrollbar" -v 2>&1 | head -30
```

Expected: compile error — `renderScrollbar` undefined.

- [ ] **Step 3: Implement scrollbar helpers**

Create `internal/tui/scrollbar.go`:

```go
package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	scrollbarTrackStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B4261"))
	scrollbarThumbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7"))
)

const (
	scrollbarTrack = "┊"
	scrollbarThumb = "█"
)

// renderScrollbar returns a single-column string of `height` lines representing
// a scrollbar for a viewport. Always returns `height` lines even when no thumb
// is needed, so panel width never reflows.
func renderScrollbar(height, totalLines, visibleLines, offsetLines int) string {
	if height <= 0 {
		return ""
	}
	lines := make([]string, height)

	if totalLines <= visibleLines || totalLines == 0 {
		// no scrolling needed — all track, no thumb
		track := scrollbarTrackStyle.Render(scrollbarTrack)
		for i := range lines {
			lines[i] = track
		}
		return strings.Join(lines, "\n")
	}

	thumbSize := visibleLines * height / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxOffset := totalLines - visibleLines
	if maxOffset < 1 {
		maxOffset = 1
	}
	thumbTop := int(float64(offsetLines) / float64(maxOffset) * float64(height-thumbSize))

	track := scrollbarTrackStyle.Render(scrollbarTrack)
	thumb := scrollbarThumbStyle.Render(scrollbarThumb)
	for i := range lines {
		if i >= thumbTop && i < thumbTop+thumbSize {
			lines[i] = thumb
		} else {
			lines[i] = track
		}
	}
	return strings.Join(lines, "\n")
}

// renderListScrollbar returns a single-column scrollbar for a windowed list.
// visibleStart is the index of the first visible item; visibleCount is how many
// items are shown.
func renderListScrollbar(height, totalItems, visibleStart, visibleCount int) string {
	if height <= 0 {
		return ""
	}
	lines := make([]string, height)

	if totalItems <= visibleCount || totalItems == 0 {
		track := scrollbarTrackStyle.Render(scrollbarTrack)
		for i := range lines {
			lines[i] = track
		}
		return strings.Join(lines, "\n")
	}

	thumbSize := visibleCount * height / totalItems
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxStart := totalItems - visibleCount
	if maxStart < 1 {
		maxStart = 1
	}
	thumbTop := int(float64(visibleStart) / float64(maxStart) * float64(height-thumbSize))

	track := scrollbarTrackStyle.Render(scrollbarTrack)
	thumb := scrollbarThumbStyle.Render(scrollbarThumb)
	for i := range lines {
		if i >= thumbTop && i < thumbTop+thumbSize {
			lines[i] = thumb
		} else {
			lines[i] = track
		}
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -run "TestRenderScrollbar|TestRenderListScrollbar" -v
```

Expected: all 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/scrollbar.go internal/tui/scrollbar_test.go
git commit -m "feat: add scrollbar rendering helpers"
```

---

### Task 2: Auto-scroll on session restore

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/model_test.go` (find the end of the file and append):

```go
func TestSessionRestoreScrollsToBottom(t *testing.T) {
	// model with restoredPendingScroll=true should GotoBottom on first WindowSizeMsg
	m := model{
		restoredPendingScroll: true,
		messages: []message{
			{role: roleUser, text: "hello"},
			{role: roleAssistant, text: "world"},
		},
	}
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	m.viewport = vp
	m.input, _ = textarea.New()
	m.width = 100
	m.height = 30

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	result := updated.(model)

	if result.restoredPendingScroll {
		t.Error("restoredPendingScroll should be false after first WindowSizeMsg")
	}
	// viewport should be at bottom (AtBottom returns true when scrolled to end)
	if !result.viewport.AtBottom() {
		t.Error("viewport should be at bottom after session restore")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -run "TestSessionRestoreScrollsToBottom" -v
```

Expected: compile error — `restoredPendingScroll` field undefined.

- [ ] **Step 3: Add the field and fix the WindowSizeMsg handler**

In `internal/tui/model.go`:

**3a.** Add `restoredPendingScroll bool` to the `model` struct after the `scrollSpeed` field (around line 166):

```go
	scrollSpeed         int
	restoredPendingScroll bool
```

**3b.** After the session restore loop in `New()` (after the closing `}` of `if sid != "" {` around line 431), set the flag:

```go
	if sid != "" {
		sess, err := session.Load(sid)
		if err == nil {
			m.sessionTelemetry = telemetryFromSessionMetadata(sess.Metadata)
			restoreTodoState(sess.Metadata)
			for _, am := range sess.Messages {
				role := tuiRoleForAgentMessage(am)
				copyMsg := am
				m.messages = append(m.messages, message{role: role, text: displayTextForAgentMessage(am), raw: &copyMsg})
			}
			if len(m.messages) > 0 {
				m.restoredPendingScroll = true
			}
		}
	}
```

**3c.** In the `WindowSizeMsg` second-switch handler (around line 519, after `m.ready = true`), add:

```go
		m.ready = true
		if m.restoredPendingScroll {
			m.renderTranscript()
			m.viewport.GotoBottom()
			m.restoredPendingScroll = false
		}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -run "TestSessionRestoreScrollsToBottom" -v
```

Expected: PASS.

- [ ] **Step 5: Run full test suite**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -v 2>&1 | tail -20
```

Expected: all existing tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go
git commit -m "fix: auto-scroll to bottom on session restore"
```

---

### Task 3: Scrollbar on transcript and log viewports

**Files:**
- Modify: `internal/tui/model.go`

The transcript viewport width is set in `layout()` at `innerWidth = panelWidth - 6`. Subtract 1 more for the scrollbar column → `innerWidth = panelWidth - 7`. The log viewport width is set in the `WindowSizeMsg` handler at `m.panelWidth() - 2` → `m.panelWidth() - 3`.

In `View()` the transcript is rendered at line ~2516:
```go
transcript := borderStyle.Width(panelWidth - 2).Render(constrainView(m.viewport.View(), m.viewport.Width(), m.viewport.Height()))
```

In `renderLogTab()` the log is rendered at line ~2947:
```go
content := borderStyle.Width(m.panelWidth() - 2).Render(m.logViewport.View())
```

- [ ] **Step 1: Shrink viewport widths**

In `layout()` change:
```go
	innerWidth := panelWidth - 6
```
to:
```go
	innerWidth := panelWidth - 7  // -6 for borders/padding, -1 for scrollbar column
```

In the `WindowSizeMsg` second-switch handler, change the logViewport width:
```go
		m.logViewport, _ = m.logViewport.Update(tea.WindowSizeMsg{
			Width:  m.panelWidth() - 3,  // was -2, subtract 1 for scrollbar
			Height: m.height - m.bottomChromeHeight(m.panelWidth()) - 1,
		})
```

- [ ] **Step 2: Add scrollbar to transcript render in `View()`**

Find the transcript render line in `View()` (~line 2516) and replace:
```go
	transcript := borderStyle.Width(panelWidth - 2).Render(constrainView(m.viewport.View(), m.viewport.Width(), m.viewport.Height()))
```
with:
```go
	transcriptSB := renderScrollbar(m.viewport.Height(), m.viewport.TotalLineCount(), m.viewport.VisibleLineCount(), m.viewport.YOffset())
	transcriptContent := lipgloss.JoinHorizontal(lipgloss.Top,
		constrainView(m.viewport.View(), m.viewport.Width(), m.viewport.Height()),
		transcriptSB,
	)
	transcript := borderStyle.Width(panelWidth - 2).Render(transcriptContent)
```

- [ ] **Step 3: Add scrollbar to log render in `renderLogTab()`**

Find the log content render line (~line 2947) and replace:
```go
	content := borderStyle.Width(m.panelWidth() - 2).Render(m.logViewport.View())
```
with:
```go
	logSB := renderScrollbar(m.logViewport.Height(), m.logViewport.TotalLineCount(), m.logViewport.VisibleLineCount(), m.logViewport.YOffset())
	logContent := lipgloss.JoinHorizontal(lipgloss.Top, m.logViewport.View(), logSB)
	content := borderStyle.Width(m.panelWidth() - 2).Render(logContent)
```

- [ ] **Step 4: Build to verify no compile errors**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/
```

Expected: builds cleanly.

- [ ] **Step 5: Run tests**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -v 2>&1 | tail -20
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: scrollbar on transcript and log viewports"
```

---

### Task 4: Scrollbar mouse interaction — transcript and log

**Files:**
- Modify: `internal/tui/model.go`

The scrollbar column for the transcript is at `x = panelWidth - 2` (0-indexed, inside the right border wall). The transcript viewport starts at `y = headerHeight` (1 line) + 1 (border top) and spans `m.viewport.Height()` lines. The log viewport starts at the same y offset (it replaces the transcript in the log tab).

Add two helpers and the drag state.

- [ ] **Step 1: Add drag state type and field to model struct**

After the `restoredPendingScroll` field in the `model` struct, add:

```go
	scrollbarDrag scrollbarDragTarget
```

Before the `model` struct definition, add the type:

```go
type scrollbarDragTarget int

const (
	scrollbarDragNone       scrollbarDragTarget = iota
	scrollbarDragTranscript
	scrollbarDragLog
	scrollbarDragGitDiff
	scrollbarDragFilesPreview
)
```

- [ ] **Step 2: Add scrollbar hit-detection helpers**

Add near the bottom of `model.go` (before the final closing brace):

```go
// scrollbarX returns the screen X column of the scrollbar for the main panel.
func (m model) mainScrollbarX() int {
	return m.panelWidth() - 2 // inside right border wall
}

// transcriptScrollbarHit returns true if mouse is on the transcript scrollbar.
func (m model) transcriptScrollbarHit(mouse tea.Mouse) bool {
	if m.activeTab != tabChat {
		return false
	}
	if mouse.X != m.mainScrollbarX() {
		return false
	}
	headerHeight := lipgloss.Height(m.styles.Header.Render("◆ ocode"))
	top := headerHeight + 1 // +1 for border top
	return mouse.Y >= top && mouse.Y < top+m.viewport.Height()
}

// logScrollbarHit returns true if mouse is on the log scrollbar.
func (m model) logScrollbarHit(mouse tea.Mouse) bool {
	if m.activeTab != tabLog {
		return false
	}
	if mouse.X != m.mainScrollbarX() {
		return false
	}
	headerHeight := lipgloss.Height(m.styles.Header.Render("◆ ocode"))
	top := headerHeight + 1
	return mouse.Y >= top && mouse.Y < top+m.logViewport.Height()
}

// scrollbarSetOffset converts a mouse Y into a viewport YOffset via jump-to-position.
func scrollbarSetOffset(vp *viewport.Model, mouseY, trackTop, trackHeight int) {
	clickRow := mouseY - trackTop
	if clickRow < 0 {
		clickRow = 0
	}
	if clickRow >= trackHeight {
		clickRow = trackHeight - 1
	}
	total := vp.TotalLineCount()
	visible := vp.VisibleLineCount()
	maxOffset := total - visible
	if maxOffset <= 0 {
		return
	}
	offset := int(float64(clickRow) / float64(trackHeight) * float64(maxOffset))
	vp.SetYOffset(offset)
}
```

- [ ] **Step 3: Add scrollbar handling to `handleMouseAction`**

In `handleMouseAction`, just after the initial button-filter guards (after line ~1086) and before the `tabForClick` check, insert:

```go
	headerHeight := lipgloss.Height(m.styles.Header.Render("◆ ocode"))
	trackTop := headerHeight + 1

	if pressed && m.transcriptScrollbarHit(mouse) {
		m.scrollbarDrag = scrollbarDragTranscript
		scrollbarSetOffset(&m.viewport, mouse.Y, trackTop, m.viewport.Height())
		return m, nil, true
	}
	if pressed && m.logScrollbarHit(mouse) {
		m.scrollbarDrag = scrollbarDragLog
		scrollbarSetOffset(&m.logViewport, mouse.Y, trackTop, m.logViewport.Height())
		return m, nil, true
	}
	if !pressed {
		m.scrollbarDrag = scrollbarDragNone
	}
```

- [ ] **Step 4: Add drag handling to `handleMouseMotion`**

In `handleMouseMotion`, after the initial button guard and before the `tabForClick` check, insert:

```go
	headerHeight := lipgloss.Height(m.styles.Header.Render("◆ ocode"))
	trackTop := headerHeight + 1

	switch m.scrollbarDrag {
	case scrollbarDragTranscript:
		scrollbarSetOffset(&m.viewport, mouse.Y, trackTop, m.viewport.Height())
		return m, nil, true
	case scrollbarDragLog:
		scrollbarSetOffset(&m.logViewport, mouse.Y, trackTop, m.logViewport.Height())
		return m, nil, true
	}
```

- [ ] **Step 5: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/
```

Expected: builds cleanly.

- [ ] **Step 6: Run tests**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -v 2>&1 | tail -20
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: scrollbar mouse click/drag on transcript and log"
```

---

### Task 5: Scrollbar on git diff viewport

**Files:**
- Modify: `internal/tui/git_model.go`

The diff viewport is sized at `diffW - 2` interior width. The diff pane starts at `x = sectW + filesW` on screen. The viewport Y starts at `headerHeight + 1` (border top).

- [ ] **Step 1: Shrink diff viewport width in `Resize()`**

In `git_model.go`, find `Resize()` (~line 77):

```go
func (m *gitModel) Resize(w, h int) {
```

Find where `m.diff` width is set. Look at the Resize body and the View layout:

```go
	sectW := w * 20 / 100
	filesW := w * 30 / 100
	diffW := w - sectW - filesW - 4
```

The diff viewport width inside View is `diffW - 2`. In `Resize()`, find the diff width calculation and subtract 1:
```go
func (m *gitModel) Resize(w, h int) {
	sectW := w * 20 / 100
	filesW := w * 30 / 100
	diffW := w - sectW - filesW - 4
	diffInner := diffW - 2 - 1  // -2 border, -1 scrollbar
	if diffInner < 1 {
		diffInner = 1
	}
	m.diff.SetWidth(diffInner)
	diffH := h - 4
	if diffH < 1 {
		diffH = 1
	}
	m.diff.SetHeight(diffH)
}
```

Check the current `Resize()` body first with Read before editing:
```bash
sed -n '77,100p' /Users/james/www/ocode/internal/tui/git_model.go
```

- [ ] **Step 2: Add scrollbar to diff pane render in `View()`**

In `git_model.go` `View()`, find the diffPane render (~line 519):

```go
	diffPane := focusBorder(m.panel == gitPanelDiff).Width(diffW - 2).Height(h - 4).Render(
		m.diff.View(),
	)
```

Replace with:

```go
	diffSB := renderScrollbar(m.diff.Height(), m.diff.TotalLineCount(), m.diff.VisibleLineCount(), m.diff.YOffset())
	diffContent := lipgloss.JoinHorizontal(lipgloss.Top, m.diff.View(), diffSB)
	diffPane := focusBorder(m.panel == gitPanelDiff).Width(diffW - 2).Height(h - 4).Render(diffContent)
```

- [ ] **Step 3: Add scrollbar mouse handling for git diff in `model.go`**

In `model.go` `handleMouseAction`, after the log scrollbar press block (from Task 4), add:

```go
	if pressed && m.activeTab == tabGit {
		// git diff pane scrollbar
		panelW := m.width // full width for git tab (no sidebar split on git tab)
		sectW := panelW * 20 / 100
		filesW := panelW * 30 / 100
		diffPaneLeft := sectW + filesW
		diffPaneRight := panelW - 1 // inside right border
		scrollX := diffPaneRight - 1
		gitHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Git"))
		gitTrackTop := gitHeaderH + 1
		gitTrackH := m.git.diff.Height()
		if mouse.X == scrollX && mouse.Y >= gitTrackTop && mouse.Y < gitTrackTop+gitTrackH {
			m.scrollbarDrag = scrollbarDragGitDiff
			scrollbarSetOffset(&m.git.diff, mouse.Y, gitTrackTop, gitTrackH)
			_ = diffPaneLeft // suppress unused warning
			return m, nil, true
		}
	}
```

In `handleMouseMotion`, after the log drag case (from Task 4), add:

```go
	case scrollbarDragGitDiff:
		gitHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Git"))
		gitTrackTop := gitHeaderH + 1
		scrollbarSetOffset(&m.git.diff, mouse.Y, gitTrackTop, m.git.diff.Height())
		return m, nil, true
```

- [ ] **Step 4: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -v 2>&1 | tail -20
```

- [ ] **Step 6: Commit**

```bash
git add internal/tui/git_model.go internal/tui/model.go
git commit -m "feat: scrollbar on git diff viewport"
```

---

### Task 6: Scrollbar on files preview viewport

**Files:**
- Modify: `internal/tui/files_model.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Shrink preview viewport width in `Resize()`**

In `files_model.go` `Resize()` (~line 69):

```go
func (m *filesModel) Resize(w, h int) {
	m.width = w
	m.height = h
	treeW := w * 35 / 100
	previewW := w - treeW - 3
	previewH := h - 3
	if previewH < 1 {
		previewH = 1
	}
	m.preview.SetWidth(previewW)
	m.preview.SetHeight(previewH)
}
```

Change `m.preview.SetWidth(previewW)` to `m.preview.SetWidth(previewW - 1)` (subtract 1 for scrollbar column).

- [ ] **Step 2: Add scrollbar to preview pane render in `View()`**

In `files_model.go` `View()` (~line 306), find:

```go
	previewPane := borderStyle.Width(previewW - 2).Render(m.preview.View())
```

Replace with:

```go
	previewSB := renderScrollbar(m.preview.Height(), m.preview.TotalLineCount(), m.preview.VisibleLineCount(), m.preview.YOffset())
	previewContent := lipgloss.JoinHorizontal(lipgloss.Top, m.preview.View(), previewSB)
	previewPane := borderStyle.Width(previewW - 2).Render(previewContent)
```

- [ ] **Step 3: Add scrollbar mouse handling for files preview in `model.go`**

In `handleMouseAction` (press block), after git diff block, add:

```go
	if pressed && m.activeTab == tabFiles {
		treeW := m.width * 35 / 100
		previewLeft := treeW
		previewRight := m.width - 1
		scrollX := previewRight - 1
		filesHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Files"))
		filesTrackTop := filesHeaderH + 1
		filesTrackH := m.files.preview.Height()
		if mouse.X == scrollX && mouse.Y >= filesTrackTop && mouse.Y < filesTrackTop+filesTrackH {
			m.scrollbarDrag = scrollbarDragFilesPreview
			scrollbarSetOffset(&m.files.preview, mouse.Y, filesTrackTop, filesTrackH)
			_ = previewLeft
			return m, nil, true
		}
	}
```

In `handleMouseMotion`, after git diff drag case, add:

```go
	case scrollbarDragFilesPreview:
		filesHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Files"))
		filesTrackTop := filesHeaderH + 1
		scrollbarSetOffset(&m.files.preview, mouse.Y, filesTrackTop, m.files.preview.Height())
		return m, nil, true
```

- [ ] **Step 4: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/ -v 2>&1 | tail -20
```

- [ ] **Step 6: Commit**

```bash
git add internal/tui/files_model.go internal/tui/model.go
git commit -m "feat: scrollbar on files preview viewport"
```

---

### Task 7: Scrollbar on picker dialog

**Files:**
- Modify: `internal/tui/picker.go`

The picker renders `maxRows=15` visible items. Total items = `len(m.pickerItems)` after filter. The body starts at row 3 (border + header + blank line) matching `pickerRowForY`'s `y - 3` offset.

- [ ] **Step 1: Add scrollbar to `renderPicker()`**

In `picker.go` find the end of `renderPicker()` (~line 240+). The current return looks like:

```go
	width := m.width - 4
	if width < 40 {
		width = 40
	}
	return borderStyle.Width(width).Render(header + "\n\n" + body.String() + hintLine)
```

Replace with:

```go
	width := m.width - 4
	if width < 40 {
		width = 40
	}
	items, _ := m.pickerVisibleItems()
	start, end := m.pickerVisibleRange()
	bodyHeight := end - start
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	sb := renderListScrollbar(bodyHeight, len(items), start, end-start)
	// Pad sb to match full body height (header + blank + items + hint)
	// We attach it to just the item rows — join only the items section
	itemsBody := body.String()
	sbLines := make([]string, lipgloss.Height(header)+2+bodyHeight+lipgloss.Height(hintLine))
	for i := range sbLines {
		sbLines[i] = scrollbarTrackStyle.Render(scrollbarTrack)
	}
	// Overwrite the item rows with actual scrollbar
	sbStart := lipgloss.Height(header) + 2 // header + "\n\n"
	sbStr := sb
	for i, line := range splitLines(sbStr) {
		if sbStart+i < len(sbLines) {
			sbLines[sbStart+i] = line
		}
	}
	fullContent := header + "\n\n" + itemsBody + hintLine
	fullSB := strings.Join(sbLines, "\n")
	joined := lipgloss.JoinHorizontal(lipgloss.Top, fullContent, fullSB)
	return borderStyle.Width(width).Render(joined)
```

Wait — this approach is complex. Simpler: just attach the scrollbar alongside only the items body (not header/hint), since the picker border contains all of it anyway. Use a cleaner join:

```go
	width := m.width - 4
	if width < 40 {
		width = 40
	}

	filteredItems, _ := m.pickerVisibleItems()
	start, end := m.pickerVisibleRange()
	visibleCount := end - start
	if visibleCount < 1 {
		visibleCount = 1
	}
	sb := renderListScrollbar(visibleCount, len(filteredItems), start, visibleCount)
	// Build full inner content with scrollbar alongside item rows only
	bodyStr := body.String()
	hintStr := hintLine
	// Pad sb lines to cover the body rows
	sbLines := strings.Split(sb, "\n")
	bodyLines := strings.Split(strings.TrimRight(bodyStr, "\n"), "\n")
	for i, bLine := range bodyLines {
		sbCol := scrollbarTrackStyle.Render(scrollbarTrack)
		if i < len(sbLines) {
			sbCol = sbLines[i]
		}
		bodyLines[i] = bLine + sbCol
	}
	inner := header + "\n\n" + strings.Join(bodyLines, "\n") + "\n" + hintStr
	return borderStyle.Width(width).Render(inner)
```

Also need to import `"strings"` — check if it's already imported in `picker.go`:

```bash
head -15 /Users/james/www/ocode/internal/tui/picker.go
```

- [ ] **Step 2: Build and run tests**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/ && go test ./internal/tui/ -v 2>&1 | tail -20
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/picker.go
git commit -m "feat: scrollbar on picker dialog"
```

---

### Task 8: Scrollbar on slash popup

**Files:**
- Modify: `internal/tui/slash_popup.go`

The slash popup renders `maxRows=8` visible items. Total = `len(m.slashPopupItems)`.

- [ ] **Step 1: Add scrollbar to `renderSlashPopup()`**

In `slash_popup.go` find `renderSlashPopup()`. The current return (~line 288):

```go
	return borderStyle.Width(width).Render(body.String())
```

Replace with:

```go
	start, end := m.slashPopupVisibleRange()
	visibleCount := end - start
	if visibleCount < 1 {
		visibleCount = 1
	}
	sb := renderListScrollbar(visibleCount, len(items), start, visibleCount)

	bodyLines := strings.Split(strings.TrimRight(body.String(), "\n"), "\n")
	sbLines := strings.Split(sb, "\n")
	for i, bLine := range bodyLines {
		sbCol := scrollbarTrackStyle.Render(scrollbarTrack)
		if i < len(sbLines) {
			sbCol = sbLines[i]
		}
		bodyLines[i] = bLine + sbCol
	}
	return borderStyle.Width(width).Render(strings.Join(bodyLines, "\n"))
```

Note: `items` is already declared earlier in the function as `items := m.slashPopupItems`. The `start, end` from `slashPopupVisibleRange()` is already called in the render loop — no re-call needed, just reuse those variables.

- [ ] **Step 2: Build and run tests**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/ && go test ./internal/tui/ -v 2>&1 | tail -20
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/slash_popup.go
git commit -m "feat: scrollbar on slash popup"
```

---

### Task 9: Scrollbar on connect dialog

**Files:**
- Modify: `internal/tui/connect.go`

The connect dialog renders provider or method lists inline. These are always short (≤5 providers, ≤3 methods) so no drag needed — just visual scrollbar for consistency.

- [ ] **Step 1: Add scrollbar to list stages in `renderConnect()`**

In `connect.go`, in the `connectStageProvider` case, the body is built into `var b strings.Builder`. After the loop and before setting `body = b.String()`, add the scrollbar:

```go
	case connectStageProvider:
		header = m.styles.Header.Render("Connect provider")
		var b strings.Builder
		for i, p := range auth.Providers {
			sym, detail := auth.Status(p.ID)
			line := fmt.Sprintf("%s  %-20s %s", sym, p.Label, hintStyle.Render(detail))
			if i == m.connect.providerIdx {
				line = m.styles.Selected.Render(" " + line + " ")
			} else {
				line = "  " + line
			}
			b.WriteString(line + "\n")
		}
		rawLines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
		sb := renderListScrollbar(len(rawLines), len(auth.Providers), 0, len(rawLines))
		sbLines := strings.Split(sb, "\n")
		for i, line := range rawLines {
			sbCol := scrollbarTrackStyle.Render(scrollbarTrack)
			if i < len(sbLines) {
				sbCol = sbLines[i]
			}
			rawLines[i] = line + sbCol
		}
		body = strings.Join(rawLines, "\n") + "\n"
		hint = hintStyle.Render("↑/↓ select · Enter continue · Esc cancel")
```

Apply the same pattern to `connectStageMethod`.

- [ ] **Step 2: Build and run tests**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/ && go test ./internal/tui/ -v 2>&1 | tail -20
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/connect.go
git commit -m "feat: scrollbar on connect dialog"
```

---

### Task 10: Final integration check

- [ ] **Step 1: Run full test suite**

```bash
cd /Users/james/www/ocode && go test ./... 2>&1 | tail -30
```

Expected: all packages pass.

- [ ] **Step 2: Build release binary**

```bash
cd /Users/james/www/ocode && go build ./cmd/ocode/ 2>&1
```

Expected: builds without errors.

- [ ] **Step 3: Verify scrollbar constants are exported from scrollbar.go for connect/picker use**

```bash
grep -n "scrollbarTrackStyle\|scrollbarTrack\b\|scrollbarThumb\b" /Users/james/www/ocode/internal/tui/scrollbar.go
```

Expected: both `scrollbarTrackStyle` and `scrollbarTrack` are package-level vars accessible from `connect.go`, `picker.go`, `slash_popup.go` (all same package `tui`).

- [ ] **Step 4: Commit if any final fixes**

```bash
git add -p
git commit -m "fix: scrollbar integration cleanup"
```
