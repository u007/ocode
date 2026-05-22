# Ambient File Context Injection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically inject the user's current file/line selection from the Files and Git tabs as a system message on every LLM send, with no extra keypress required.

**Architecture:** Add `selectedFiles` multi-select to `filesModel` (matching git tab's existing pattern). Add `buildSelectionContext() string` to `model` that reads selection state from both tabs and formats it. Inject into the three live-request builders (`askAgent`, `sendCustomCommandPrompt`, `reExecutePendingTool`) via a shared `appendSelectionMsg` helper. Esc peels layers: highlight first, then multi-select, then normal esc.

**Tech Stack:** Go, Bubble Tea TUI, `internal/tui` package

---

## File Map

| File | Change |
|---|---|
| `internal/tui/files_model.go` | Add `selectedFiles map[int]bool`, space-toggle, shift+↑↓ extend, `selectedFilePaths()`, status bar update |
| `internal/tui/model.go` | Add `buildSelectionContext()`, `appendSelectionMsg()`, inject in 3 send sites, update esc handlers |
| `internal/tui/files_model_test.go` | Tests for space-toggle, shift+↑↓, selectedFilePaths, esc clear |
| `internal/tui/model_test.go` | Tests for buildSelectionContext, appendSelectionMsg |

---

## Task 1: Add `selectedFiles` field and `selectedFilePaths()` to filesModel

**Files:**
- Modify: `internal/tui/files_model.go`
- Test: `internal/tui/files_model_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/files_model_test.go`:

```go
func TestFilesSelectedFilePaths(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.nodes = []fileNode{
		{path: "/a/foo.go", name: "foo.go"},
		{path: "/a/bar.go", name: "bar.go"},
		{path: "/a/baz/", name: "baz", isDir: true},
	}
	// nothing selected
	if got := m.selectedFilePaths(); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
	// select index 0 and 1
	m.selectedFiles = map[int]bool{0: true, 1: true}
	paths := m.selectedFilePaths()
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %v", paths)
	}
	// dirs must be excluded even if in selectedFiles
	m.selectedFiles = map[int]bool{2: true}
	if got := m.selectedFilePaths(); len(got) != 0 {
		t.Fatalf("expected dirs excluded, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/ -run TestFilesSelectedFilePaths -v
```

Expected: FAIL — `selectedFilePaths undefined`

- [ ] **Step 3: Add field and method to filesModel**

In `internal/tui/files_model.go`, add the field to the struct (after `previewRawLines []string`):

```go
selectedFiles map[int]bool
```

Add the method after the `extractSelectionText` method:

```go
func (m filesModel) selectedFilePaths() []string {
	if len(m.selectedFiles) == 0 {
		return nil
	}
	paths := make([]string, 0, len(m.selectedFiles))
	for idx := range m.selectedFiles {
		if idx >= 0 && idx < len(m.nodes) && !m.nodes[idx].isDir {
			paths = append(paths, m.nodes[idx].path)
		}
	}
	return paths
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui/ -run TestFilesSelectedFilePaths -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/files_model.go internal/tui/files_model_test.go
git commit -m "feat: add selectedFiles field and selectedFilePaths() to filesModel"
```

---

## Task 2: Space-key toggles file selection in files tree

**Files:**
- Modify: `internal/tui/files_model.go`
- Test: `internal/tui/files_model_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestFilesSpaceTogglesSelection(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.nodes = []fileNode{
		{path: "/a/foo.go", name: "foo.go"},
		{path: "/a/bar.go", name: "bar.go"},
	}
	m.cursor = 0
	m.panel = filesPanelTree

	// space selects file at cursor
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRune, Text: " "}, 80, 24)
	got := m2.(filesModel)
	if !got.selectedFiles[0] {
		t.Fatal("expected index 0 to be selected after space")
	}

	// space again deselects
	m3, _ := got.Update(tea.KeyPressMsg{Code: tea.KeyRune, Text: " "}, 80, 24)
	got2 := m3.(filesModel)
	if got2.selectedFiles[0] {
		t.Fatal("expected index 0 to be deselected after second space")
	}
}

func TestFilesSpaceOnDirTogglesExpand(t *testing.T) {
	dir := t.TempDir()
	m := newFilesModel(dir)
	m.nodes = []fileNode{
		{path: dir + "/sub/", name: "sub", isDir: true},
	}
	m.cursor = 0
	m.panel = filesPanelTree

	// space on dir must still toggle expand, not add to selectedFiles
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRune, Text: " "}, 80, 24)
	got := m2.(filesModel)
	if got.selectedFiles[0] {
		t.Fatal("space on dir must not add to selectedFiles")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/tui/ -run "TestFilesSpaceToggle|TestFilesSpaceOnDir" -v
```

Expected: FAIL

- [ ] **Step 3: Update space case in updateTree**

In `internal/tui/files_model.go`, find `updateTree`. The existing space case is:

```go
case "enter", "ctrl+j", "ctrl+m", "space":
    if m.cursor < len(m.nodes) {
        n := &m.nodes[m.cursor]
        if n.isDir {
            m.toggleDir(m.cursor)
        } else {
            return m, m.openInEditor(n.path)
        }
    }
```

Split into separate cases:

```go
case "enter", "ctrl+j", "ctrl+m":
    if m.cursor < len(m.nodes) {
        n := &m.nodes[m.cursor]
        if n.isDir {
            m.toggleDir(m.cursor)
        } else {
            return m, m.openInEditor(n.path)
        }
    }
case " ":
    if m.cursor < len(m.nodes) {
        n := &m.nodes[m.cursor]
        if n.isDir {
            m.toggleDir(m.cursor)
        } else {
            if m.selectedFiles == nil {
                m.selectedFiles = make(map[int]bool)
            }
            if m.selectedFiles[m.cursor] {
                delete(m.selectedFiles, m.cursor)
            } else {
                m.selectedFiles[m.cursor] = true
            }
        }
    }
```

- [ ] **Step 4: Run to verify passing**

```bash
go test ./internal/tui/ -run "TestFilesSpaceToggle|TestFilesSpaceOnDir" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/files_model.go internal/tui/files_model_test.go
git commit -m "feat: space toggles file selection in files tree (dirs still expand)"
```

---

## Task 3: Shift+↑/↓ extends selection in files tree

**Files:**
- Modify: `internal/tui/files_model.go`
- Test: `internal/tui/files_model_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestFilesShiftDownExtendsSelection(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.nodes = []fileNode{
		{path: "/a/a.go", name: "a.go"},
		{path: "/a/b.go", name: "b.go"},
		{path: "/a/c.go", name: "c.go"},
	}
	m.cursor = 0
	m.panel = filesPanelTree

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, 80, 24)
	got := m2.(filesModel)
	if !got.selectedFiles[0] || !got.selectedFiles[1] {
		t.Fatalf("shift+down should select 0 and 1, got %v", got.selectedFiles)
	}
	if got.cursor != 1 {
		t.Fatalf("cursor should move to 1, got %d", got.cursor)
	}
}

func TestFilesShiftUpExtendsSelection(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.nodes = []fileNode{
		{path: "/a/a.go", name: "a.go"},
		{path: "/a/b.go", name: "b.go"},
	}
	m.cursor = 1
	m.panel = filesPanelTree

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}, 80, 24)
	got := m2.(filesModel)
	if !got.selectedFiles[0] || !got.selectedFiles[1] {
		t.Fatalf("shift+up should select 0 and 1, got %v", got.selectedFiles)
	}
	if got.cursor != 0 {
		t.Fatalf("cursor should move to 0, got %d", got.cursor)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/tui/ -run "TestFilesShift" -v
```

Expected: FAIL

- [ ] **Step 3: Add shift+↑↓ cases to updateTree**

In `updateTree`, after the existing `"k", "up"` case, add:

```go
case "shift+down":
    if m.cursor < len(m.nodes)-1 {
        if m.selectedFiles == nil {
            m.selectedFiles = make(map[int]bool)
        }
        m.selectedFiles[m.cursor] = true
        m.cursor++
        m.selectedFiles[m.cursor] = true
        if m.cursor < len(m.nodes) {
            return m, loadPreviewCmd(m.nodes[m.cursor])
        }
    }
case "shift+up":
    if m.cursor > 0 {
        if m.selectedFiles == nil {
            m.selectedFiles = make(map[int]bool)
        }
        m.selectedFiles[m.cursor] = true
        m.cursor--
        m.selectedFiles[m.cursor] = true
        if m.cursor < len(m.nodes) {
            return m, loadPreviewCmd(m.nodes[m.cursor])
        }
    }
```

Also clear selectedFiles on plain j/k nav (matching git tab). In the existing `"j", "down"` and `"k", "up"` cases, add before the cursor move:

```go
m.selectedFiles = nil
```

- [ ] **Step 4: Run to verify passing**

```bash
go test ./internal/tui/ -run "TestFilesShift" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/files_model.go internal/tui/files_model_test.go
git commit -m "feat: shift+↑↓ extends file selection in files tree"
```

---

## Task 4: Esc clears file selection layers in files tab

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/files_model_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/model_test.go`:

```go
func TestEscClearsFilesHighlightFirst(t *testing.T) {
	m := model{}
	m.activeTab = tabFiles
	m.filesSel = selectionState{active: true, startLine: 0, endLine: 3}
	m.files.selectedFiles = map[int]bool{0: true}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := updated.(model)

	// highlight cleared, but selectedFiles still present
	if got.filesSel.active {
		t.Fatal("expected filesSel to be cleared by first esc")
	}
	if len(got.files.selectedFiles) == 0 {
		t.Fatal("expected selectedFiles to survive first esc (highlight cleared first)")
	}
}

func TestEscClearsFilesSelectedFilesSecond(t *testing.T) {
	m := model{}
	m.activeTab = tabFiles
	m.filesSel = selectionState{} // no highlight
	m.files.selectedFiles = map[int]bool{0: true}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := updated.(model)

	if len(got.files.selectedFiles) != 0 {
		t.Fatal("expected selectedFiles cleared when no highlight active")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/tui/ -run "TestEscClearsFiles" -v
```

Expected: FAIL

- [ ] **Step 3: Update esc handler for files tab in model.go**

Find the esc handler for `tabFiles` (around line 821):

```go
if msg.String() == "esc" && !m.filesHasActiveFocus() {
    return m.handleEscKey()
}
```

Replace with:

```go
if msg.String() == "esc" && !m.filesHasActiveFocus() {
    if m.filesSel.active {
        m.filesSel = selectionState{}
        m.files.clearSelectionHighlight()
        return m, nil
    }
    if len(m.files.selectedFiles) > 0 {
        m.files.selectedFiles = nil
        return m, nil
    }
    return m.handleEscKey()
}
```

- [ ] **Step 4: Run to verify passing**

```bash
go test ./internal/tui/ -run "TestEscClearsFiles" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: esc peels file selection layers (highlight → selectedFiles → normal)"
```

---

## Task 5: Esc clears git selection layers in git tab

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestEscClearsGitHighlightFirst(t *testing.T) {
	m := model{}
	m.activeTab = tabGit
	m.gitSel = selectionState{active: true, startLine: 0, endLine: 2}
	m.git.selectedFiles = map[int]bool{0: true}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := updated.(model)

	if got.gitSel.active {
		t.Fatal("expected gitSel cleared by first esc")
	}
	if len(got.git.selectedFiles) == 0 {
		t.Fatal("expected git selectedFiles to survive first esc")
	}
}

func TestEscClearsGitSelectedFilesSecond(t *testing.T) {
	m := model{}
	m.activeTab = tabGit
	m.gitSel = selectionState{}
	m.git.selectedFiles = map[int]bool{0: true}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := updated.(model)

	if len(got.git.selectedFiles) != 0 {
		t.Fatal("expected git selectedFiles cleared when no highlight active")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/tui/ -run "TestEscClearsGit" -v
```

Expected: FAIL

- [ ] **Step 3: Update esc handler for git tab in model.go**

Find the esc handler for `tabGit` (around line 834):

```go
if msg.String() == "esc" && !m.gitHasActiveFocus() {
    return m.handleEscKey()
}
```

Replace with:

```go
if msg.String() == "esc" && !m.gitHasActiveFocus() {
    if m.gitSel.active {
        m.gitSel = selectionState{}
        m.git.clearDiffSelectionHighlight()
        return m, nil
    }
    if len(m.git.selectedFiles) > 0 {
        m.git.selectedFiles = nil
        return m, nil
    }
    return m.handleEscKey()
}
```

- [ ] **Step 4: Run to verify passing**

```bash
go test ./internal/tui/ -run "TestEscClearsGit" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: esc peels git selection layers (highlight → selectedFiles → normal)"
```

---

## Task 6: buildSelectionContext() assembles the system message content

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBuildSelectionContextEmpty(t *testing.T) {
	m := model{}
	if got := m.buildSelectionContext(); got != "" {
		t.Fatalf("expected empty with no selection, got %q", got)
	}
}

func TestBuildSelectionContextFilesOnly(t *testing.T) {
	m := model{}
	m.workDir = "/proj"
	m.files.nodes = []fileNode{
		{path: "/proj/main.go", name: "main.go"},
		{path: "/proj/foo.go", name: "foo.go"},
	}
	m.files.selectedFiles = map[int]bool{0: true, 1: true}

	got := m.buildSelectionContext()
	if got == "" {
		t.Fatal("expected non-empty context")
	}
	if !strings.Contains(got, "main.go") || !strings.Contains(got, "foo.go") {
		t.Fatalf("expected both file paths in context, got:\n%s", got)
	}
}

func TestBuildSelectionContextFilesHighlight(t *testing.T) {
	m := model{}
	m.workDir = "/proj"
	m.files.previewPath = "/proj/main.go"
	m.files.previewRawLines = []string{"package main", "func main() {}", "}"}
	m.filesSel = selectionState{
		active:    true,
		startLine: 0, startCol: 0,
		endLine: 1, endCol: 99,
	}

	got := m.buildSelectionContext()
	if !strings.Contains(got, "main.go") {
		t.Fatalf("expected file path in context, got:\n%s", got)
	}
	if !strings.Contains(got, "1:") || !strings.Contains(got, "2:") {
		t.Fatalf("expected line numbers in context, got:\n%s", got)
	}
	if !strings.Contains(got, "package main") {
		t.Fatalf("expected line content in context, got:\n%s", got)
	}
}

func TestBuildSelectionContextGitFiles(t *testing.T) {
	m := model{}
	m.git.unstagedFiles = []gitFile{{path: "internal/foo.go", status: "M"}}
	m.git.selectedFiles = map[int]bool{0: true}

	got := m.buildSelectionContext()
	if !strings.Contains(got, "internal/foo.go") {
		t.Fatalf("expected git file path in context, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/tui/ -run "TestBuildSelectionContext" -v
```

Expected: FAIL — `buildSelectionContext undefined`

- [ ] **Step 3: Implement buildSelectionContext in model.go**

Add this method to `model.go` (near the other context-building helpers, e.g., after `filesAddToContext`):

```go
// buildSelectionContext returns a system message body describing the user's
// current file/line selection in the Files and Git tabs. Returns "" if nothing
// is selected.
func (m model) buildSelectionContext() string {
	var b strings.Builder

	// --- Files tab ---
	filePaths := m.files.selectedFilePaths()
	hasFilesHighlight := m.filesSel.active && m.files.previewPath != ""

	if len(filePaths) > 0 || hasFilesHighlight {
		b.WriteString("## Files tab\n")
		for _, p := range filePaths {
			rel, err := filepath.Rel(m.workDir, p)
			if err != nil {
				rel = p
			}
			b.WriteString("- " + rel + "\n")
		}
		if hasFilesHighlight {
			rel, err := filepath.Rel(m.workDir, m.files.previewPath)
			if err != nil {
				rel = m.files.previewPath
			}
			sl, sc, el, ec := normaliseSelection(
				m.filesSel.startLine, m.filesSel.startCol,
				m.filesSel.endLine, m.filesSel.endCol,
			)
			b.WriteString("\nHighlighted lines — " + rel + ":\n")
			for lineIdx := sl; lineIdx <= el; lineIdx++ {
				if lineIdx < 0 || lineIdx >= len(m.files.previewRawLines) {
					continue
				}
				line := m.files.previewRawLines[lineIdx]
				// trim to selection columns on first/last line
				cs, ce := 0, len(line)
				if lineIdx == sl {
					cs = visualColToRuneIdx(line, sc)
				}
				if lineIdx == el {
					ce = visualColToRuneIdx(line, ec)
				}
				if cs > ce {
					cs = ce
				}
				b.WriteString(fmt.Sprintf("  %d: %s\n", lineIdx+1, line[cs:ce]))
			}
		}
	}

	// --- Git tab ---
	gitFiles := m.git.currentFileList()
	var selectedGitPaths []string

	// current diff file (always include if diff panel visible)
	if len(gitFiles) > 0 && m.git.filesCursor >= 0 && m.git.filesCursor < len(gitFiles) {
		f := gitFiles[m.git.filesCursor]
		selectedGitPaths = append(selectedGitPaths, fmt.Sprintf("%s (%s)", f.path, gitStatusLabel(f.status, f.staged)))
	}
	// space-toggled files
	for idx := range m.git.selectedFiles {
		if idx >= 0 && idx < len(gitFiles) && idx != m.git.filesCursor {
			f := gitFiles[idx]
			selectedGitPaths = append(selectedGitPaths, fmt.Sprintf("%s (%s)", f.path, gitStatusLabel(f.status, f.staged)))
		}
	}

	hasGitHighlight := m.gitSel.active && len(m.git.diffRawLines) > 0

	if len(selectedGitPaths) > 0 || hasGitHighlight {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Git tab\n")
		for _, p := range selectedGitPaths {
			b.WriteString("- " + p + "\n")
		}
		if hasGitHighlight {
			b.WriteString("\nHighlighted diff lines:\n")
			sl, sc, el, ec := normaliseSelection(
				m.gitSel.startLine, m.gitSel.startCol,
				m.gitSel.endLine, m.gitSel.endCol,
			)
			for lineIdx := sl; lineIdx <= el; lineIdx++ {
				if lineIdx < 0 || lineIdx >= len(m.git.diffRawLines) {
					continue
				}
				line := m.git.diffRawLines[lineIdx]
				cs, ce := 0, len(line)
				if lineIdx == sl {
					cs = visualColToRuneIdx(line, sc)
				}
				if lineIdx == el {
					ce = visualColToRuneIdx(line, ec)
				}
				if cs > ce {
					cs = ce
				}
				b.WriteString(fmt.Sprintf("  %d: %s\n", lineIdx+1, line[cs:ce]))
			}
		}
	}

	if b.Len() == 0 {
		return ""
	}
	return "[Selected context]\n\n" + b.String()
}

// gitStatusLabel returns a human-readable status string for a git file.
func gitStatusLabel(status string, staged bool) string {
	switch status {
	case "M":
		if staged {
			return "staged"
		}
		return "modified"
	case "A":
		return "added"
	case "D":
		return "deleted"
	case "R":
		return "renamed"
	case "?":
		return "untracked"
	default:
		return status
	}
}
```

- [ ] **Step 4: Run to verify passing**

```bash
go test ./internal/tui/ -run "TestBuildSelectionContext" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: buildSelectionContext assembles ambient file/git selection system message"
```

---

## Task 7: appendSelectionMsg helper + inject into all 3 send sites

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestAppendSelectionMsgSkipsWhenEmpty(t *testing.T) {
	m := model{} // no selection
	msgs := []agent.Message{{Role: "user", Content: "hello"}}
	result := m.appendSelectionMsg(msgs)
	if len(result) != 1 {
		t.Fatalf("expected no msg added when selection empty, got %d msgs", len(result))
	}
}

func TestAppendSelectionMsgAddsSystemMsg(t *testing.T) {
	m := model{}
	m.workDir = "/proj"
	m.files.nodes = []fileNode{{path: "/proj/main.go", name: "main.go"}}
	m.files.selectedFiles = map[int]bool{0: true}

	msgs := []agent.Message{{Role: "user", Content: "hello"}}
	result := m.appendSelectionMsg(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 1 msg added, got %d msgs", len(result))
	}
	if result[0].Role != "system" {
		t.Fatalf("expected selection msg to be first (system), got role %q", result[0].Role)
	}
	if !strings.Contains(result[0].Content, "main.go") {
		t.Fatalf("expected file path in selection msg, got:\n%s", result[0].Content)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/tui/ -run "TestAppendSelectionMsg" -v
```

Expected: FAIL — `appendSelectionMsg undefined`

- [ ] **Step 3: Add appendSelectionMsg to model.go**

```go
// appendSelectionMsg prepends a system message with current file/line selection
// context to msgs, if any selection is active. Returns msgs unchanged if not.
func (m model) appendSelectionMsg(msgs []agent.Message) []agent.Message {
	ctx := m.buildSelectionContext()
	if ctx == "" {
		return msgs
	}
	selMsg := agent.Message{Role: "system", Content: ctx}
	return append([]agent.Message{selMsg}, msgs...)
}
```

- [ ] **Step 4: Inject into askAgent() (line ~3287)**

Find in `askAgent()`:

```go
ctx := agent.LoadContext()
if ctx != "" {
    agentMsgs = append(agentMsgs, agent.Message{Role: "system", Content: "Context and rules:\n" + ctx})
}
```

After that block, add:

```go
agentMsgs = m.appendSelectionMsg(agentMsgs)
```

- [ ] **Step 5: Inject into sendCustomCommandPrompt() (line ~2897)**

Find in `sendCustomCommandPrompt()`:

```go
ctx := agent.LoadContext()
if ctx != "" {
    agentMsgs = append(agentMsgs, agent.Message{Role: "system", Content: "Context and rules:\n" + ctx})
}
```

After that block, add:

```go
agentMsgs = m.appendSelectionMsg(agentMsgs)
```

- [ ] **Step 6: Inject into reExecutePendingTool() (line ~3408)**

Find in `reExecutePendingTool()`:

```go
ctx := agent.LoadContext()
if ctx != "" {
    agentMsgs = append(agentMsgs, agent.Message{Role: "system", Content: "Context and rules:\n" + ctx})
}
```

After that block, add:

```go
agentMsgs = m.appendSelectionMsg(agentMsgs)
```

- [ ] **Step 7: Run all tests**

```bash
go test ./internal/tui/ -run "TestAppendSelectionMsg" -v
go test ./internal/tui/ -v
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: inject ambient selection context into all LLM send paths"
```

---

## Task 8: Update files tab status bar to show selection count

**Files:**
- Modify: `internal/tui/files_model.go`
- Test: `internal/tui/files_model_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestFilesStatusBarShowsSelectionCount(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.nodes = []fileNode{
		{path: "/a/foo.go", name: "foo.go"},
		{path: "/a/bar.go", name: "bar.go"},
	}
	m.selectedFiles = map[int]bool{0: true, 1: true}

	// The status bar hint method should mention the count
	hint := m.statusBarHint()
	if !strings.Contains(hint, "2 selected") {
		t.Fatalf("expected '2 selected' in status bar hint, got %q", hint)
	}
}
```

- [ ] **Step 2: Find the status bar / hint method for files**

```bash
grep -n "func.*statusBar\|func.*hint\|func.*Hint\|func.*status\b" /Users/james/www/ocode/internal/tui/files_model.go
```

Note the actual method name and update the test to match.

- [ ] **Step 3: Run to verify failure**

```bash
go test ./internal/tui/ -run "TestFilesStatusBarShowsSelectionCount" -v
```

Expected: FAIL

- [ ] **Step 4: Add selection count to status bar**

Find the status bar / hint rendering in `files_model.go`. Locate where it builds the hint string for `filesPanelTree`. Add to the front of the hint (matching git tab's pattern at line ~1318):

```go
if len(m.selectedFiles) > 0 {
    return fmt.Sprintf("%d selected — space toggle  shift+↑↓ extend  esc clear  |  ", len(m.selectedFiles)) + existingHint
}
```

Adapt exact placement to match the existing control flow.

- [ ] **Step 5: Run to verify passing**

```bash
go test ./internal/tui/ -run "TestFilesStatusBarShowsSelectionCount" -v
```

Expected: PASS

- [ ] **Step 6: Run all tests**

```bash
go test ./internal/tui/ -v
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/files_model.go internal/tui/files_model_test.go
git commit -m "feat: show selected file count in files tab status bar"
```

---

## Task 9: Manual smoke test

- [ ] **Step 1: Build and run**

```bash
go build ./... && go run ./cmd/ocode
```

- [ ] **Step 2: Test files tab multi-select**

1. Navigate to Files tab
2. Move cursor to a file, press space → file should show as selected in status bar
3. Shift+↓ → selection extends, status bar shows count
4. Type a message and send → verify system prompt contains `[Selected context]` with the file paths (check by reading the session log or adding a debug log temporarily)
5. Press Esc → selection clears

- [ ] **Step 3: Test files tab line highlight**

1. Navigate to a file's preview pane
2. Click-drag to highlight lines
3. Send a message → context should include `Highlighted lines — <path>:` with numbered lines

- [ ] **Step 4: Test git tab**

1. Navigate to Git tab
2. Navigate to a modified file (diff shows)
3. Space-toggle a second file
4. Send a message → context should include `## Git tab` with both files
5. Highlight diff lines → context should include `Highlighted diff lines:`
6. Press Esc → clears highlight; Esc again → clears selectedFiles

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: ambient file context injection complete"
```
