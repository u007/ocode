# Inline Vim File Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a minimal vim-like inline editor to the Files tab preview pane for editable text files.

**Architecture:** Keep all feature state inside `filesModel`. Add a focused `inlineFileEditor` helper in the TUI package for buffer, cursor, mode, and command handling, then route Files tab keys to it only while `filesModeEdit` is active. Keep the external editor flow (`e`, `enter`, tmux modes, editor picker) unchanged.

**Tech Stack:** Go 1.23+, Bubble Tea v2 key messages, Lipgloss rendering, standard `os` file APIs, existing `go test ./internal/tui` test style.

---

## File Structure

- Modify `internal/tui/files_model.go`: add `filesModeEdit`, wire `i`, route edit mode updates, perform start/save/quit flows, render edit view and hints.
- Create `internal/tui/inline_file_editor.go`: implement the minimal vim-like editor buffer, cursor movement, insert/normal/command modes, command parsing, and view text.
- Create `internal/tui/inline_file_editor_test.go`: unit-test editor buffer behavior without filesystem.
- Modify `internal/tui/files_model_test.go`: test Files tab integration, file save/quit behavior, stale disk protection, edit refusal, and hints.
- Modify `docs/superpowers/specs/2026-05-19-inline-file-editor-design.md` only if implementation reveals a necessary spec correction.

## Task 1: Inline Editor Core

**Files:**
- Create: `internal/tui/inline_file_editor.go`
- Create: `internal/tui/inline_file_editor_test.go`

- [ ] **Step 1: Write failing tests for editor mode, insertion, movement, and commands**

Add `internal/tui/inline_file_editor_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestInlineFileEditorInsertAndMovement(t *testing.T) {
	ed := newInlineFileEditor("alpha\nbeta\n")

	ed = ed.update(tea.KeyPressMsg{Code: 'j'})
	if ed.cursorRow != 1 {
		t.Fatalf("expected cursor row 1, got %d", ed.cursorRow)
	}

	ed = ed.update(tea.KeyPressMsg{Code: '$'})
	if ed.cursorCol != len("beta")-1 {
		t.Fatalf("expected cursor at end of line, got %d", ed.cursorCol)
	}

	ed = ed.update(tea.KeyPressMsg{Code: 'a'})
	if ed.mode != inlineEditorInsert {
		t.Fatalf("expected insert mode after a, got %v", ed.mode)
	}

	ed = ed.update(tea.KeyPressMsg{Code: '!', Text: "!"})
	ed = ed.update(tea.KeyPressMsg{Code: tea.KeyEsc})

	if got := ed.content(); got != "alpha\nbeta!\n" {
		t.Fatalf("expected appended content, got %q", got)
	}
	if !ed.dirty {
		t.Fatal("expected editor to be dirty")
	}
	if ed.mode != inlineEditorNormal {
		t.Fatalf("expected normal mode after esc, got %v", ed.mode)
	}
}

func TestInlineFileEditorCommandMode(t *testing.T) {
	ed := newInlineFileEditor("hello\n")
	ed = ed.update(tea.KeyPressMsg{Code: ':'})
	if ed.mode != inlineEditorCommand {
		t.Fatalf("expected command mode, got %v", ed.mode)
	}

	ed = ed.update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	ed = ed.update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	ed = ed.update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if ed.lastCommand != "wq" {
		t.Fatalf("expected wq command, got %q", ed.lastCommand)
	}
	if ed.mode != inlineEditorNormal {
		t.Fatalf("expected normal mode after command submit, got %v", ed.mode)
	}
}

func TestInlineFileEditorDirtyQuitRules(t *testing.T) {
	ed := newInlineFileEditor("hello\n")
	ed = ed.update(tea.KeyPressMsg{Code: 'i'})
	ed = ed.update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	ed = ed.update(tea.KeyPressMsg{Code: tea.KeyEsc})

	ed = ed.update(tea.KeyPressMsg{Code: ':'})
	ed = ed.update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	ed = ed.update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if ed.lastCommand != "q" {
		t.Fatalf("expected q command, got %q", ed.lastCommand)
	}
	if !ed.dirty {
		t.Fatal("expected q command to leave dirty buffer intact")
	}
}

func TestInlineFileEditorViewShowsModeAndCommand(t *testing.T) {
	ed := newInlineFileEditor("hello\n")
	ed = ed.update(tea.KeyPressMsg{Code: ':'})
	ed = ed.update(tea.KeyPressMsg{Code: 'w', Text: "w"})

	view := ed.view(40, 10)
	for _, want := range []string{"-- COMMAND --", ":w", "hello"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestInlineFileEditor'`

Expected: FAIL with errors like `undefined: newInlineFileEditor` and `undefined: inlineEditorInsert`.

- [ ] **Step 3: Implement minimal inline editor core**

Create `internal/tui/inline_file_editor.go`:

```go
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type inlineEditorMode int

const (
	inlineEditorNormal inlineEditorMode = iota
	inlineEditorInsert
	inlineEditorCommand
)

type inlineFileEditor struct {
	lines       []string
	trailingNL  bool
	cursorRow   int
	cursorCol   int
	mode        inlineEditorMode
	command     string
	lastCommand string
	dirty       bool
}

func newInlineFileEditor(content string) inlineFileEditor {
	ed := inlineFileEditor{mode: inlineEditorNormal, trailingNL: strings.HasSuffix(content, "\n")}
	trimmed := strings.TrimSuffix(content, "\n")
	ed.lines = strings.Split(trimmed, "\n")
	if len(ed.lines) == 0 {
		ed.lines = []string{""}
	}
	return ed
}

func (e inlineFileEditor) update(msg tea.KeyPressMsg) inlineFileEditor {
	e.lastCommand = ""
	switch e.mode {
	case inlineEditorInsert:
		return e.updateInsert(msg)
	case inlineEditorCommand:
		return e.updateCommand(msg)
	default:
		return e.updateNormal(msg)
	}
}

func (e inlineFileEditor) updateNormal(msg tea.KeyPressMsg) inlineFileEditor {
	switch msg.String() {
	case "i":
		e.mode = inlineEditorInsert
	case "a":
		if e.cursorCol < len(e.currentLine()) {
			e.cursorCol++
		}
		e.mode = inlineEditorInsert
	case ":":
		e.mode = inlineEditorCommand
		e.command = ""
	case "h", "left":
		if e.cursorCol > 0 {
			e.cursorCol--
		}
	case "l", "right":
		if e.cursorCol < len(e.currentLine())-1 {
			e.cursorCol++
		}
	case "j", "down":
		if e.cursorRow < len(e.lines)-1 {
			e.cursorRow++
			e.clampCursorCol()
		}
	case "k", "up":
		if e.cursorRow > 0 {
			e.cursorRow--
			e.clampCursorCol()
		}
	case "0":
		e.cursorCol = 0
	case "$":
		lineLen := len(e.currentLine())
		if lineLen > 0 {
			e.cursorCol = lineLen - 1
		}
	}
	return e
}

func (e inlineFileEditor) updateInsert(msg tea.KeyPressMsg) inlineFileEditor {
	switch msg.String() {
	case "esc":
		e.mode = inlineEditorNormal
		if e.cursorCol > 0 {
			e.cursorCol--
		}
		return e
	case "enter", "ctrl+j", "ctrl+m":
		line := e.currentLine()
		before := line[:e.cursorCol]
		after := line[e.cursorCol:]
		e.lines[e.cursorRow] = before
		e.lines = append(e.lines[:e.cursorRow+1], append([]string{after}, e.lines[e.cursorRow+1:]...)...)
		e.cursorRow++
		e.cursorCol = 0
		e.dirty = true
		return e
	case "backspace":
		if e.cursorCol > 0 {
			line := e.currentLine()
			e.lines[e.cursorRow] = line[:e.cursorCol-1] + line[e.cursorCol:]
			e.cursorCol--
			e.dirty = true
		}
		return e
	}
	if msg.Text != "" {
		line := e.currentLine()
		e.lines[e.cursorRow] = line[:e.cursorCol] + msg.Text + line[e.cursorCol:]
		e.cursorCol += len(msg.Text)
		e.dirty = true
	}
	return e
}

func (e inlineFileEditor) updateCommand(msg tea.KeyPressMsg) inlineFileEditor {
	switch msg.String() {
	case "esc":
		e.mode = inlineEditorNormal
		e.command = ""
	case "enter", "ctrl+j", "ctrl+m":
		e.lastCommand = e.command
		e.command = ""
		e.mode = inlineEditorNormal
	case "backspace":
		if len(e.command) > 0 {
			e.command = e.command[:len(e.command)-1]
		}
	default:
		if msg.Text != "" {
			e.command += msg.Text
		}
	}
	return e
}

func (e inlineFileEditor) content() string {
	content := strings.Join(e.lines, "\n")
	if e.trailingNL {
		content += "\n"
	}
	return content
}

func (e inlineFileEditor) view(width int, height int) string {
	if height < 1 {
		height = 1
	}
	visible := e.lines
	if len(visible) > height-1 {
		visible = visible[:height-1]
	}
	status := "-- NORMAL --"
	if e.mode == inlineEditorInsert {
		status = "-- INSERT --"
	}
	if e.mode == inlineEditorCommand {
		status = "-- COMMAND -- :" + e.command
	}
	if e.dirty {
		status += " [+]"
	}
	return strings.Join(append(visible, status), "\n")
}

func (e inlineFileEditor) currentLine() string {
	if e.cursorRow < 0 || e.cursorRow >= len(e.lines) {
		return ""
	}
	return e.lines[e.cursorRow]
}

func (e *inlineFileEditor) clampCursorCol() {
	lineLen := len(e.currentLine())
	if lineLen == 0 {
		e.cursorCol = 0
		return
	}
	if e.cursorCol >= lineLen {
		e.cursorCol = lineLen - 1
	}
}

func (e *inlineFileEditor) markClean() {
	e.dirty = false
}
```

- [ ] **Step 4: Run core editor tests**

Run: `go test ./internal/tui -run 'TestInlineFileEditor'`

Expected: PASS.

- [ ] **Step 5: Commit core editor**

Run:

```bash
git add internal/tui/inline_file_editor.go internal/tui/inline_file_editor_test.go
git commit -m "feat: add inline file editor core"
```

Expected: commit succeeds. If not explicitly authorized to commit in the current session, skip this step and report the intended commit message.

## Task 2: Files Tab Edit Mode Integration

**Files:**
- Modify: `internal/tui/files_model.go`
- Modify: `internal/tui/files_model_test.go`

- [ ] **Step 1: Write failing Files tab edit-mode tests**

Append to `internal/tui/files_model_test.go`:

```go
func TestFilesInlineVimEditWriteQuit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	if m.mode != filesModeEdit {
		t.Fatalf("expected edit mode, got %v", m.mode)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'a'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: '!', Text: "!"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: ':'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}, 100, 30)
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)

	if m.mode != filesModeNormal {
		t.Fatalf("expected normal mode after :wq, got %v", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected refresh preview command after :wq")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello!\n" {
		t.Fatalf("expected saved edit, got %q", string(data))
	}
}

func TestFilesInlineVimQuitRefusesDirtyBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'a'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: '!', Text: "!"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: ':'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)

	if m.mode != filesModeEdit {
		t.Fatalf("expected to remain in edit mode, got %v", m.mode)
	}
	if !strings.Contains(m.statusMsg, "unsaved") {
		t.Fatalf("expected unsaved status, got %q", m.statusMsg)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: ':'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: '!', Text: "!"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)

	if m.mode != filesModeNormal {
		t.Fatalf("expected forced quit to return to normal mode, got %v", m.mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("expected file unchanged after :q!, got %q", string(data))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestFilesInlineVim'`

Expected: FAIL with `undefined: filesModeEdit` or edit-mode assertions failing.

- [ ] **Step 3: Add edit-mode fields and start flow**

Modify `internal/tui/files_model.go`:

```go
type filesMode int

const (
	filesModeNormal filesMode = iota
	filesModePrompt
	filesModeDeleteConfirm
	filesModeEdit
)

type filesModel struct {
	// existing fields unchanged
	panel           filesPanel
	inlineEditor    inlineFileEditor
	inlineEditPath  string
	inlineEditMtime int64
	inlineEditSize  int64
}
```

Add to `Update` after delete-confirm handling and before fuzzy handling:

```go
if m.mode == filesModeEdit {
	return m.updateInlineEdit(msg)
}
```

Add `i` handling to both `updateTree` and `updatePreview`:

```go
case "i":
	return m.startInlineEdit()
```

- [ ] **Step 4: Implement start, update, save, and quit flow**

Add to `internal/tui/files_model.go` near prompt/delete helpers:

```go
func (m filesModel) startInlineEdit() (filesModel, tea.Cmd) {
	n, ok := m.selectedNode()
	if !ok {
		m.statusMsg = "no file selected"
		return m, nil
	}
	if n.isDir {
		m.statusMsg = "cannot edit directory"
		return m, nil
	}
	if !m.previewEditable || m.previewPath != n.path {
		m.statusMsg = "file is not editable"
		return m, nil
	}
	info, err := os.Stat(n.path)
	if err != nil {
		m.statusMsg = "edit stat failed: " + err.Error()
		return m, nil
	}
	data, err := os.ReadFile(n.path)
	if err != nil {
		m.statusMsg = "edit read failed: " + err.Error()
		return m, nil
	}
	m.mode = filesModeEdit
	m.inlineEditor = newInlineFileEditor(string(data))
	m.inlineEditPath = n.path
	m.inlineEditMtime = info.ModTime().UnixNano()
	m.inlineEditSize = info.Size()
	m.statusMsg = "vim edit: i/a insert  :w save  :q quit  :q! discard  :wq save+quit"
	return m, nil
}

func (m filesModel) updateInlineEdit(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	m.inlineEditor = m.inlineEditor.update(msg)
	cmd := m.inlineEditor.lastCommand
	if cmd == "" {
		return m, nil
	}
	switch cmd {
	case "w":
		return m.saveInlineEdit(false)
	case "wq":
		return m.saveInlineEdit(true)
	case "q":
		if m.inlineEditor.dirty {
			m.statusMsg = "unsaved changes: use :w to save or :q! to discard"
			return m, nil
		}
		m.mode = filesModeNormal
		m.statusMsg = "edit closed"
		return m, nil
	case "q!":
		m.mode = filesModeNormal
		m.statusMsg = "edit discarded"
		return m, m.refreshPreviewCmd()
	default:
		m.statusMsg = "unknown command: " + cmd
		return m, nil
	}
}

func (m filesModel) saveInlineEdit(exit bool) (filesModel, tea.Cmd) {
	info, err := os.Stat(m.inlineEditPath)
	if err != nil {
		m.statusMsg = "edit stat failed: " + err.Error()
		return m, nil
	}
	if info.ModTime().UnixNano() != m.inlineEditMtime || info.Size() != m.inlineEditSize {
		m.statusMsg = "file changed on disk; reload before saving"
		return m, nil
	}
	if err := os.WriteFile(m.inlineEditPath, []byte(m.inlineEditor.content()), info.Mode()); err != nil {
		m.statusMsg = "edit save failed: " + err.Error()
		return m, nil
	}
	info, err = os.Stat(m.inlineEditPath)
	if err != nil {
		m.statusMsg = "edit stat failed: " + err.Error()
		return m, nil
	}
	m.inlineEditor.markClean()
	m.inlineEditMtime = info.ModTime().UnixNano()
	m.inlineEditSize = info.Size()
	m.refreshGitStatus()
	m.statusMsg = "saved: " + filepath.Base(m.inlineEditPath)
	if exit {
		m.mode = filesModeNormal
	}
	return m, m.refreshPreviewCmd()
}
```

- [ ] **Step 5: Run edit-mode integration tests**

Run: `go test ./internal/tui -run 'TestFilesInlineVim'`

Expected: PASS.

- [ ] **Step 6: Commit Files tab integration**

Run:

```bash
git add internal/tui/files_model.go internal/tui/files_model_test.go
git commit -m "feat: wire inline vim editor into files tab"
```

Expected: commit succeeds. If not explicitly authorized to commit in the current session, skip this step and report the intended commit message.

## Task 3: Rendering, Hints, and Refusal Cases

**Files:**
- Modify: `internal/tui/files_model.go`
- Modify: `internal/tui/files_model_test.go`

- [ ] **Step 1: Write failing tests for hints, render mode, and edit refusal**

Append to `internal/tui/files_model_test.go`:

```go
func TestFilesInlineVimViewAndHints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	m.editorMode = "external"
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	view := m.View(100, 30, ApplyThemeColors("tokyonight"), false)
	if !strings.Contains(view, "i vim edit") {
		t.Fatalf("expected vim edit hint, got:\n%s", view)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	view = m.View(100, 30, ApplyThemeColors("tokyonight"), false)
	for _, want := range []string{"hello", "-- NORMAL --", ":w save"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected edit view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestFilesInlineVimRefusesDirectoryAndNonEditablePreview(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	if m.mode == filesModeEdit {
		t.Fatal("expected directory edit to be refused")
	}
	if !strings.Contains(m.statusMsg, "directory") {
		t.Fatalf("expected directory status, got %q", m.statusMsg)
	}

	filePath := filepath.Join(dir, "binary.bin")
	if err := os.WriteFile(filePath, []byte{0, 1, 2}, 0644); err != nil {
		t.Fatal(err)
	}
	m = newFilesModel(dir)
	m.navigateTo("binary.bin")
	if result, ok := loadPreviewCmd(m.nodes[m.cursor])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	if m.mode == filesModeEdit {
		t.Fatal("expected binary edit to be refused")
	}
	if !strings.Contains(m.statusMsg, "not editable") {
		t.Fatalf("expected not editable status, got %q", m.statusMsg)
	}
}

func TestFilesInlineVimSaveRefusesDiskChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'a'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: '!', Text: "!"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}, 100, 30)
	if err := os.WriteFile(path, []byte("external\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: ':'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)

	if m.mode != filesModeEdit {
		t.Fatalf("expected stale save to remain in edit mode, got %v", m.mode)
	}
	if !strings.Contains(m.statusMsg, "changed on disk") {
		t.Fatalf("expected stale file status, got %q", m.statusMsg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external\n" {
		t.Fatalf("expected external content preserved, got %q", string(data))
	}
}
```

- [ ] **Step 2: Run tests to verify render/refusal gaps**

Run: `go test ./internal/tui -run 'TestFilesInlineVim(View|Refuses|SaveRefuses)'`

Expected: FAIL until view and hints are wired.

- [ ] **Step 3: Render inline editor and update hints**

Modify `View` in `internal/tui/files_model.go` around preview content creation:

```go
previewBody := m.preview.View()
if m.mode == filesModeEdit {
	previewBody = m.inlineEditor.view(previewW-7, h-5)
}
previewContent := lipgloss.JoinHorizontal(lipgloss.Top, previewBody, previewSB)
```

Update normal preview hints:

```go
if m.mode == filesModeNormal && m.previewEditable {
	hint := "tab jump  i vim edit  e external  E choose editor  /editor set default"
	if isTmuxMode(m.editorMode) {
		hint = "tab jump  i vim edit  e " + m.tmuxOpenHint() + "  E choose editor  /editor set default"
	}
	previewContent = hintStyle.Render(hint) + "\n" + previewContent
}
```

Add edit-mode help before status handling:

```go
if m.mode == filesModeEdit {
	previewContent = hintStyle.Render("vim edit: i/a insert  esc normal  :w save  :q quit  :q! discard  :wq save+quit") + "\n" + previewContent
}
```

- [ ] **Step 4: Run render/refusal tests**

Run: `go test ./internal/tui -run 'TestFilesInlineVim(View|Refuses|SaveRefuses)'`

Expected: PASS.

- [ ] **Step 5: Commit rendering and edge cases**

Run:

```bash
git add internal/tui/files_model.go internal/tui/files_model_test.go
git commit -m "test: cover inline vim editor safeguards"
```

Expected: commit succeeds. If not explicitly authorized to commit in the current session, skip this step and report the intended commit message.

## Task 4: Full Verification and Documentation Check

**Files:**
- Modify: `README.md` if hints/user-facing editor behavior has diverged from documented editor workflow.

- [ ] **Step 1: Run focused TUI tests**

Run: `go test ./internal/tui`

Expected: PASS.

- [ ] **Step 2: Run full Go test suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3: Check docs consistency**

Read `README.md` editor section and confirm it still accurately describes `e`/external editor behavior. If adding inline vim edit should be documented, update the Files tab/editor section with this exact bullet:

```markdown
- In the Files tab, `i` opens a minimal vim-like inline editor for editable text files. It supports `i`/`a` insert, `esc` normal mode, `:w`, `:q`, `:q!`, and `:wq`. Use `e` or `enter` for the configured external editor.
```

- [ ] **Step 4: Run full verification after docs change**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit final docs/verification change**

Run:

```bash
git add README.md docs/superpowers/specs/2026-05-19-inline-file-editor-design.md docs/superpowers/plans/2026-05-19-inline-vim-file-editor.md
git commit -m "docs: describe inline vim file editor"
```

Expected: commit succeeds. If not explicitly authorized to commit in the current session, skip this step and report the intended commit message.

## Self-Review

- Spec coverage: The plan covers vim-like edit entry, normal/insert/command modes, `i`, `a`, `esc`, `:w`, `:q`, `:q!`, `:wq`, movement, unchanged external editor behavior, edit refusal, dirty-state protection, stale disk protection, hints, and tests.
- Placeholder scan: No placeholders remain. Advanced Vim features are explicitly excluded by the spec and not planned.
- Type consistency: `filesModeEdit`, `inlineFileEditor`, `inlineEditorNormal`, `inlineEditorInsert`, and `inlineEditorCommand` are introduced before integration tasks use them.
