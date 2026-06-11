# File Tab Scrollbars Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add interactive vertical scrollbars to the file list and ensure preview scrollbar is interactive.

**Architecture:** Add `treeScrollY` field to track vertical offset, render only visible lines in `View()`, add scrollbar rendering via `JoinHorizontal`, wire mouse hit-tests in `handleMouseAction()` before node-click handlers, reuse existing `renderScrollbar()` and `scrollbarSetOffset()` helpers.

**Tech Stack:** Go, Bubble Tea, Lipgloss, existing scrollbar utilities

---

## File Structure

**Files to modify:**
- `internal/tui/files_model.go` — Add `treeScrollY`, update `View()` to render visible lines, add scrollbar rendering
- `internal/tui/model.go` — Add scrollbar hit-test in `handleMouseAction()`, update motion/release handlers for tree scrollbar drag

---

## Task 1: Add `treeScrollY` field and scrollbar drag state

**Files:**
- Modify: `internal/tui/files_model.go:183` (add field after `treeScrollX`)
- Modify: `internal/tui/model.go:~200` (add scrollbar drag case, find the `scrollbarDrag` enum)

**Steps:**

- [ ] **Step 1.1: Find the scrollbarDrag enum in model.go**

Run: `grep -n "const.*scrollbarDrag" /Users/james/www/ocode/internal/tui/model.go`

Expected: Returns line number of the enum definition (looks like `scrollbarDrag = iota` constants)

- [ ] **Step 1.2: Add scrollbarDragFilesTree case to enum**

Open `internal/tui/model.go` at the `scrollbarDrag` enum (likely near line 200-300). Add after `scrollbarDragLog`:

```go
scrollbarDragFilesTree
```

- [ ] **Step 1.3: Add treeScrollY field to filesModel**

Open `internal/tui/files_model.go` at line 183 (after `treeScrollX int`). Add:

```go
treeScrollY int // vertical scroll offset in the tree panel (lines)
```

- [ ] **Step 1.4: Initialize treeScrollY in filesModel constructor**

Find where `filesModel` is initialized (search for `filesModel{` in files_model.go). Ensure `treeScrollY: 0,` is present (it will default to 0, but explicit is clearer). If not present, add it.

Run: `grep -n "filesModel{" /Users/james/www/ocode/internal/tui/files_model.go | head -5`

- [ ] **Step 1.5: Commit**

```bash
git add internal/tui/files_model.go internal/tui/model.go
git commit -m "feat(files): add treeScrollY field and scrollbarDragFilesTree state

Add vertical scroll tracking for file tree pane and scrollbar drag state.
No functional changes yet — fields initialized to zero."
```

---

## Task 2: Update View() to render only visible lines and add scrollbar

**Files:**
- Modify: `internal/tui/files_model.go:1776-1880` (the View() method)

**Context:** The current View() renders all tree lines in `treeLines`. We need to:
1. Slice `treeLines` to only visible lines based on `treeScrollY` and available height
2. Render a scrollbar showing the current position
3. Join scrollbar with content via `JoinHorizontal`

**Steps:**

- [ ] **Step 2.1: Calculate tree pane height**

In `View()`, after line 1878 (where `treePane` is created), capture the height:

Find the line: `treePane := focusBorder(...).Width(treeW - 2).Height(h - 4).Render(treeContent)`

This already sets `Height(h - 4)`. Calculate the actual content height (height available for lines):

```go
treeContentHeight := h - 4 - 2 // h - 4 is pane height, -2 for borders
if treeContentHeight < 1 {
	treeContentHeight = 1
}
```

Add this right before the `treePane :=` line.

- [ ] **Step 2.2: Apply vertical scroll to treeLines**

Before the `treeContent := strings.Join(treeLines, "\n")` line (around line 1861), slice `treeLines` to only visible rows:

Replace:
```go
treeContent := strings.Join(treeLines, "\n")
```

With:
```go
// Clamp treeScrollY to valid range
maxScrollY := len(treeLines) - treeContentHeight
if maxScrollY < 0 {
	maxScrollY = 0
}
if m.treeScrollY > maxScrollY {
	m.treeScrollY = maxScrollY
}
if m.treeScrollY < 0 {
	m.treeScrollY = 0
}

// Slice visible lines
visibleStart := m.treeScrollY
visibleEnd := m.treeScrollY + treeContentHeight
if visibleEnd > len(treeLines) {
	visibleEnd = len(treeLines)
}
visibleLines := treeLines[visibleStart:visibleEnd]

// Pad with empty lines if needed to fill viewport
for len(visibleLines) < treeContentHeight {
	visibleLines = append(visibleLines, "")
}

treeContent := strings.Join(visibleLines, "\n")
```

- [ ] **Step 2.3: Render scrollbar and join with content**

After the `treeContent` is built, before the header is prepended (around line 1864), render the scrollbar:

```go
treeSB := renderScrollbar(treeContentHeight, len(treeLines), treeContentHeight, m.treeScrollY)
```

Then, after `treePane := focusBorder(...).Render(treeContent)` (line 1878), replace with:

```go
treeContent = m.fuzzyPopupView(...) // if in fuzzy mode
if m.mode != filesModeNormal {
    // ... existing mode-specific logic
} else {
    // Join tree content with scrollbar
    treeContentWithScroll := lipgloss.JoinHorizontal(lipgloss.Top, treeContent, treeSB)
    treePane = focusBorder(...).Width(treeW - 2).Height(h - 4).Render(treeContentWithScroll)
}
```

Actually, let me reconsider the order. Let me look at the current structure more carefully.

- [ ] **Step 2.3 (revised): Add scrollbar rendering in the correct location**

Read the current View() method starting at line 1776. The structure is:
1. Build raw/styled lines for all nodes
2. Apply horizontal scroll
3. Build final tree lines with selection highlight
4. Prepend header rows if any
5. Create treePane

We need to add scrollbar AFTER slicing visible lines but BEFORE creating the bordered pane. After you calculate `treeContentHeight` in step 2.1 and apply vertical scroll in step 2.2, add:

```go
// Render scrollbar for tree pane
treeSB := renderScrollbar(treeContentHeight, len(treeLines), treeContentHeight, m.treeScrollY)
```

Then modify the `treePane` creation line to join the scrollbar:

Find (around line 1878):
```go
treePane := focusBorder(m.panel == filesPanelPicker).Width(treeW - 2).Height(h - 4).Render(treeContent)
```

Before this line, add:
```go
// Join tree content with scrollbar
treeContentFull := lipgloss.JoinHorizontal(lipgloss.Top, treeContent, treeSB)
```

Then replace the treePane line with:
```go
treePane := focusBorder(m.panel == filesPanelPicker).Width(treeW - 2).Height(h - 4).Render(treeContentFull)
```

- [ ] **Step 2.4: Run the TUI and verify tree scrolls**

```bash
go run ./cmd/ocode/main.go
```

Navigate to the files tab with a directory containing many files (or create a test file list). Verify:
- File list appears at top of tree pane
- No errors in the TUI
- Scrollbar appears on right side of tree pane (may be thin and not visually obvious yet)

Press Ctrl+C to exit.

- [ ] **Step 2.5: Commit**

```bash
git add internal/tui/files_model.go
git commit -m "feat(files): add vertical scroll and scrollbar rendering to tree pane

- Slice tree lines to visible range based on treeScrollY
- Render scrollbar showing scroll position
- Clamp treeScrollY to valid bounds on resize"
```

---

## Task 3: Add scrollbar hit-test in handleMouseAction()

**Files:**
- Modify: `internal/tui/model.go:4267` (files tab mouse handling section)

**Context:** We need to add a scrollbar hit-test BEFORE the tree node click handler (line 4308 `treeNodeForClick`). The hit-test checks if the click is at the right edge of the tree pane (the scrollbar column).

**Steps:**

- [ ] **Step 3.1: Understand the tree pane boundaries**

Read lines 4267-4340 of model.go to understand the layout:
- `treeW := m.width * 35 / 100` — tree pane width
- `m.files.treeNodeForClick()` — existing node click handler at line 4308
- Preview scrollbar hit-test at lines 4331-4339 as a reference pattern

- [ ] **Step 3.2: Add tree scrollbar hit-test before tree node click**

Insert the following BEFORE line 4308 (`if idx, ok := m.files.treeNodeForClick(...)`):

```go
// Tree scrollbar hit-test
treeW := m.width * 35 / 100
treeScrollbarX := treeW - 2 - 1 // right edge of tree pane (inside border)
treeTrackTop := appHeaderHeight + 1
treeTrackH := h - 4 - m.files.treeHeaderRowCount() // viewport height minus header
if treeTrackH < 1 {
	treeTrackH = 1
}
if mouse.X == treeScrollbarX && mouse.Y >= treeTrackTop && mouse.Y < treeTrackTop+treeTrackH {
	if thumbOffset, ok := scrollbarThumbOffset(mouse.Y, treeTrackTop, treeTrackH, len(m.files.treeLines), treeTrackH, m.files.treeScrollY); ok {
		m.scrollbarDrag = scrollbarDragFilesTree
		m.scrollbarDragOffset = thumbOffset
	} else {
		// Click on track, jump to that position
		// We'll handle this via scrollbarSetOffset on the tree's scroll position
		newY := (mouse.Y - treeTrackTop) * len(m.files.treeLines) / treeTrackH
		m.files.treeScrollY = newY
	}
	return m, nil, true
}
```

Wait, there's a problem: `scrollbarThumbOffset` expects a viewport, not raw values. Let me check how it's called for the preview.

- [ ] **Step 3.1 (revised): Check scrollbarThumbOffset signature**

Run: `grep -A5 "func scrollbarThumbOffset" /Users/james/www/ocode/internal/tui/model.go`

Expected output shows the function signature. It likely takes (mouseY, trackTop, trackHeight, totalLines, visibleLines, yOffset) and returns (thumbOffset, ok bool).

- [ ] **Step 3.2 (revised): Add tree scrollbar hit-test**

Before line 4308 (the `treeNodeForClick` call), add:

```go
// Tree scrollbar hit-test (before node click)
treeW := m.width * 35 / 100
treeScrollbarX := treeW - 2 - 1 // right edge inside border
treeTrackTop := appHeaderHeight + 1
treeTrackH := h - 4 - 2 // pane height minus border rows, approximation
if treeTrackH < 1 {
	treeTrackH = 1
}
if mouse.X == treeScrollbarX && mouse.Y >= treeTrackTop && mouse.Y < treeTrackTop+treeTrackH {
	if thumbOffset, ok := scrollbarThumbOffset(mouse.Y, treeTrackTop, treeTrackH, len(m.files.treeLines), treeTrackH, m.files.treeScrollY); ok {
		m.scrollbarDrag = scrollbarDragFilesTree
		m.scrollbarDragOffset = thumbOffset
	} else {
		// Click on track: calculate new scroll position
		scrollbarSetOffset(&scrollableTree{m.files}, mouse.Y, treeTrackTop, treeTrackH)
	}
	return m, nil, true
}
```

Hmm, `scrollbarSetOffset` expects a viewport-like interface. Let me check its signature.

- [ ] **Step 3.1 (re-revised): Check scrollbarSetOffset signature**

Run: `grep -A10 "func scrollbarSetOffset" /Users/james/www/ocode/internal/tui/model.go`

- [ ] **Step 3.2 (re-revised): Implement tree scrollbar hit-test correctly**

After checking the signatures, the scrollbar functions expect specific interfaces. For now, let's do manual calculation:

Before line 4308, add:

```go
// Tree scrollbar hit-test (before node click)
treeW := m.width * 35 / 100
treeScrollbarX := treeW - 2 - 1 // scrollbar is at right edge of tree pane
treeTrackTop := appHeaderHeight + 1
treeTrackHeight := h - 4 // pane height
if mouse.X == treeScrollbarX && mouse.Y >= treeTrackTop && mouse.Y < treeTrackTop+treeTrackHeight {
	// Check if clicking the scrollbar thumb or track
	totalLines := len(m.files.treeLines)
	visibleLines := treeTrackHeight
	if visibleLines < 1 {
		visibleLines = 1
	}
	yOffset := m.files.treeScrollY
	
	// Calculate thumb bounds: where does it sit on the track
	if totalLines <= visibleLines {
		// No scrollbar needed, but if we got here, treat as no-op
		return m, nil, false
	}
	
	thumbSize := visibleLines * treeTrackHeight / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	thumbTop := yOffset * (treeTrackHeight - thumbSize) / (totalLines - visibleLines)
	thumbBottom := thumbTop + thumbSize
	
	relY := mouse.Y - treeTrackTop
	if relY >= thumbTop && relY < thumbBottom {
		// Clicked the thumb, start drag
		m.scrollbarDrag = scrollbarDragFilesTree
		m.scrollbarDragOffset = relY - thumbTop
	} else {
		// Clicked the track, jump to that position
		newOffset := relY * totalLines / treeTrackHeight
		m.files.treeScrollY = newOffset
	}
	return m, nil, true
}
```

- [ ] **Step 3.3: Run and test tree scrollbar clicks**

```bash
go run ./cmd/ocode/main.go
```

In the files tab, click on the right edge of the tree pane (where the scrollbar should be). Test:
- Click on lower part of scrollbar track → tree should jump down
- Click on upper part → tree should jump up
- TUI should not crash

Press Ctrl+C to exit.

- [ ] **Step 3.4: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(files): add tree scrollbar hit-test in handleMouseAction()

- Add scrollbar click detection at tree pane right edge
- Thumb click: start drag, Track click: jump to position
- Hit-test runs before node-click handler to take precedence"
```

---

## Task 4: Add mouse motion handler for scrollbar drag

**Files:**
- Modify: `internal/tui/model.go:4711` (handleMouseMotion function)

**Context:** When the user drags the scrollbar thumb, we need to update `treeScrollY` based on mouse movement. This is handled in `handleMouseMotion()`.

**Steps:**

- [ ] **Step 4.1: Find handleMouseMotion and the scrollbar drag cases**

Run: `grep -n "func.*handleMouseMotion\|scrollbarDrag ==" /Users/james/www/ocode/internal/tui/model.go | head -20`

Expected: Shows line number of `handleMouseMotion` and existing scrollbar drag cases.

- [ ] **Step 4.2: Add tree scrollbar motion case**

Inside `handleMouseMotion()`, find the existing `if m.scrollbarDrag == scrollbarDragLog` case (or similar). Add a new case for tree:

```go
if m.scrollbarDrag == scrollbarDragFilesTree {
	treeW := m.width * 35 / 100
	treeTrackTop := appHeaderHeight + 1
	treeTrackHeight := m.height - 4 // pane height
	totalLines := len(m.files.treeLines)
	visibleLines := treeTrackHeight
	if visibleLines < 1 {
		visibleLines = 1
	}
	if totalLines > visibleLines {
		// Calculate new offset based on thumb position and drag offset
		thumbSize := visibleLines * treeTrackHeight / totalLines
		if thumbSize < 1 {
			thumbSize = 1
		}
		maxThumbPos := treeTrackHeight - thumbSize
		newThumbPos := mouse.Y - treeTrackTop - m.scrollbarDragOffset
		if newThumbPos < 0 {
			newThumbPos = 0
		}
		if newThumbPos > maxThumbPos {
			newThumbPos = maxThumbPos
		}
		newOffset := newThumbPos * (totalLines - visibleLines) / (treeTrackHeight - thumbSize)
		m.files.treeScrollY = newOffset
	}
	return m, nil, true
}
```

- [ ] **Step 4.3: Run and test scrollbar drag**

```bash
go run ./cmd/ocode/main.go
```

In the files tab, drag the tree scrollbar thumb up and down. Verify:
- Tree scrolls smoothly while dragging
- Scroll position matches thumb position
- No crashes

Press Ctrl+C to exit.

- [ ] **Step 4.4: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(files): add tree scrollbar drag motion handler

Update treeScrollY while user drags scrollbar thumb"
```

---

## Task 5: Add arrow key scrolling for tree

**Files:**
- Modify: `internal/tui/files_model.go:556` (updateTree function)

**Context:** Arrow up/down already move the cursor. We should also update `treeScrollY` so the tree scrolls when the cursor moves near the top/bottom of the viewport. Currently, `treeScrollX` is reset when cursor moves (line 563), but we should do similar vertical scrolling.

**Steps:**

- [ ] **Step 5.1: Read updateTree to understand cursor movement**

Read lines 556-650 of files_model.go. See that:
- `case "down"`: increments `m.cursor`, resets `m.treeScrollX = 0`
- `case "up"`: decrements `m.cursor`, resets `m.treeScrollX = 0`
- Cursor can be 0 to len(nodes)-1

- [ ] **Step 5.2: Add scroll-to-visible logic**

After the cursor is updated (in both "up" and "down" cases), add logic to ensure the cursor row is visible:

In the "down" case (after `m.cursor++`), add:

```go
// Ensure cursor is visible in viewport
treeViewportHeight := 20 // approximate, will be refined in View()
if m.cursor-m.treeScrollY >= treeViewportHeight {
	m.treeScrollY = m.cursor - treeViewportHeight + 1
}
if m.cursor < m.treeScrollY {
	m.treeScrollY = m.cursor
}
```

Similarly for "up" case.

Actually, we don't know the viewport height in `updateTree()` — it's only available in `View()`. So we can't implement this properly here. Instead, we'll rely on View() to keep the cursor visible.

Better approach: In View(), after calculating the visible line range, adjust `treeScrollY` to ensure the cursor is visible:

- [ ] **Step 5.2 (revised): Adjust View() to keep cursor visible**

In View(), after applying vertical scroll (from Task 2), add:

```go
// Ensure cursor is within visible range
if m.cursor < m.treeScrollY {
	m.treeScrollY = m.cursor
}
if m.cursor >= m.treeScrollY+treeContentHeight {
	m.treeScrollY = m.cursor - treeContentHeight + 1
}
if m.treeScrollY < 0 {
	m.treeScrollY = 0
}
```

This should be added right after the clamping logic from Task 2.

- [ ] **Step 5.3: Run and test arrow key scrolling**

```bash
go run ./cmd/ocode/main.go
```

In files tab with many files:
- Press down arrow repeatedly → cursor moves down, tree scrolls to keep cursor visible
- Press up arrow → cursor moves up, tree scrolls up
- Verify cursor is always visible

Press Ctrl+C to exit.

- [ ] **Step 5.4: Commit**

```bash
git add internal/tui/files_model.go
git commit -m "feat(files): keep cursor visible when scrolling tree

Auto-adjust treeScrollY in View() to ensure cursor row is always within viewport"
```

---

## Task 6: Manual test — all scrolling features

**No code changes.** Functional testing.

**Steps:**

- [ ] **Step 6.1: Open large directory**

```bash
go run ./cmd/ocode/main.go
```

Navigate to a directory with 50+ files (or create test files: `touch /tmp/test/file_{1..100}.txt`). Open that directory in the files tab.

- [ ] **Step 6.2: Test arrow key scrolling**

Press up/down arrows repeatedly. Verify:
- Cursor moves one file at a time
- Tree scrolls when cursor approaches viewport edge
- Scrollbar on right side moves to show current position
- No crashes or rendering glitches

- [ ] **Step 6.3: Test scrollbar clicks**

Click on the scrollbar track (to the right of the file list):
- Click lower on track → tree jumps down
- Click upper on track → tree jumps up
- Scrollbar thumb position matches tree scroll position

- [ ] **Step 6.4: Test scrollbar drag**

Click and drag the scrollbar thumb (the highlighted part of the scrollbar):
- Tree follows the drag smoothly
- Release mouse → tree stays at that position

- [ ] **Step 6.5: Test tree node selection**

While scrolling, test that tree node clicks still work:
- Click on a file name → file is selected and preview loads
- Click on directory name → directory expands/collapses
- Scrollbar clicks don't interfere with node clicks

- [ ] **Step 6.6: Test preview text selection**

In the preview pane (right side):
- Try to select text by clicking and dragging
- Selection should still work (text is highlighted)
- Scrollbar clicks on preview should not select text
- Text copy to clipboard should work

- [ ] **Step 6.7: Test window resize**

Resize the terminal window smaller and larger:
- Tree adjusts viewport height
- Scrollbar size changes accordingly
- Scroll position clamps to valid range
- No off-by-one errors or rendering corruption

- [ ] **Step 6.8: Test with small directory**

Load a directory with < 10 files:
- Scrollbar should disappear (no overflow)
- Tree displays all files at once
- Scrolling still works (but does nothing if < viewport height)

- [ ] **Step 6.9: Commit test results**

No code changes, but document any issues found:

```bash
# If no issues:
git status
# (no changes)

# If issues found, fix them in a new task
```

---

## Task 7: Verify preview scrollbar still works and text selection is safe

**Files:**
- No code changes.

**Context:** The preview already had a scrollbar and text selection. We need to verify our changes didn't break either.

**Steps:**

- [ ] **Step 7.1: Open a large file**

```bash
go run ./cmd/ocode/main.go
```

Navigate to a file with 100+ lines (or use `/etc/passwd` or another system file).

- [ ] **Step 7.2: Test preview scrollbar**

In the preview pane (right side):
- Verify scrollbar appears on the right edge
- Scroll down using arrow keys → scrollbar position updates
- Click scrollbar track → jumps to that position
- Drag scrollbar thumb → file preview scrolls smoothly

- [ ] **Step 7.3: Test preview text selection**

- Click and drag in the file content area → text is highlighted
- Make sure the highlight is visible
- Release mouse → text should be copied to clipboard (check if it worked)
- Verify scrollbar clicks don't interfere (click scrollbar shouldn't start selection)

- [ ] **Step 7.4: Test selection on scrollbar boundary**

- Click near the right edge of the preview (where scrollbar is)
- Verify scrollbar click is detected (not a selection start)
- Click in content area near the right edge (but left of scrollbar)
- Verify selection works

- [ ] **Step 7.5: Document results**

No commit needed unless issues found. If issues found, create a new task to fix them.

---

## Task 8: Edge cases and cleanup

**Files:**
- `internal/tui/files_model.go` (any fixes)
- `internal/tui/model.go` (any fixes)

**Context:** Test and fix edge cases identified in the spec.

**Steps:**

- [ ] **Step 8.1: Test empty directory**

```bash
mkdir /tmp/empty_dir
go run ./cmd/ocode/main.go
# Navigate to /tmp/empty_dir
```

Verify:
- No files listed
- Scrollbar does not render (no overflow)
- No crashes

- [ ] **Step 8.2: Test very long filenames**

Create a file with a very long name:

```bash
touch "/tmp/test/this_is_a_very_long_filename_that_might_wrap_or_truncate_in_the_tree_pane_when_rendered_with_icons_and_badges.txt"
```

Verify:
- Long filename truncates correctly
- Scrollbar still works
- Horizontal scroll still works (left/right arrows)

- [ ] **Step 8.3: Test expand/collapse with scrolling**

In a directory with many nested files:
- Expand a directory → tree grows
- Cursor may move out of view; verify tree scrolls to keep it visible
- Collapse → tree shrinks, scrollbar adjusts

- [ ] **Step 8.4: Test rapid resize**

While viewing the tree:
- Rapidly resize the terminal (make it smaller, larger, smaller)
- Verify no crashes or rendering artifacts
- Scrollbar bounds clamp correctly

- [ ] **Step 8.5: Review code for hairwire/output issues**

Check files_model.go View() and model.go handleMouseAction/Motion for any:
- Direct `fmt.Print*` or `os.Stderr.Write` (should be `log.Printf`)
- Unhandled edge cases
- Off-by-one errors in scrollbar math

- [ ] **Step 8.6: Final commit**

If no changes needed:

```bash
git status
# (should be clean)
```

If fixes were made:

```bash
git add internal/tui/files_model.go internal/tui/model.go
git commit -m "fix(files): edge case handling for scrollbars

- Handle empty directory (no scrollbar)
- Clamp scroll on resize
- Keep cursor visible when tree size changes"
```

---

## Summary Checklist

- [ ] Tree renders with `treeScrollY` field (Task 1)
- [ ] Tree scrolls vertically via arrow keys (Task 5)
- [ ] Tree scrollbar renders and is clickable (Task 3)
- [ ] Tree scrollbar is draggable (Task 4)
- [ ] Tree node clicks still work (Task 6)
- [ ] Preview text selection still works (Task 7)
- [ ] Edge cases handled (Task 8)
- [ ] All code committed with clear messages
