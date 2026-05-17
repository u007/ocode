# Tabs, File Explorer & Git Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add tmux-style numbered tabs to ocode's TUI with a full file explorer (tree + preview, opens in external editor) and a lazygit-style git browser (Changes/Log/Stash/Branches with diff viewing and git operations).

**Architecture:** Add `activeTab int` and two sub-models (`filesModel`, `gitModel`) to the root `model` struct. Global messages (agent events, permission prompts) are always processed by root; only tab-local key input is forwarded to the active sub-model. Existing modals (`showPicker`, `showPalette`, etc.) render on top of any tab unchanged.

**Tech Stack:** Go, charm.land/bubbletea v2, charm.land/lipgloss v2, charm.land/bubbles v2 (viewport, textarea), os/exec for git commands.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/tui/tabs.go` | Create | Tab constants, tab bar rendering |
| `internal/tui/files_model.go` | Create | `filesModel` — file tree + preview |
| `internal/tui/git_model.go` | Create | `gitModel` — 4-section git browser |
| `internal/tui/model.go` | Modify | Add `activeTab`, `files`, `git`; update routing, header, status |
| `internal/config/ocodeconfig.go` | Modify | Add `Editor string` field + resolution helper |

---

## Task 1: Tab constants and tab bar rendering

**Files:**
- Create: `internal/tui/tabs.go`
- Modify: `internal/tui/model.go` (add `activeTab int` to `model` struct)

- [ ] **Step 1: Create `internal/tui/tabs.go`**

```go
package tui

const (
	tabChat  = 0
	tabFiles = 1
	tabGit   = 2
)

// renderTabBar returns the tab bar string rendered into the header line.
// unread=true adds a bullet to the chat label when on a non-chat tab.
func renderTabBar(active int, unread bool) string {
	labels := []string{"1:chat", "2:files", "3:git"}
	if unread && active != tabChat {
		labels[0] = "1:chat●"
	}
	out := ""
	for i, label := range labels {
		if i == active {
			out += lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1).Render(label)
		} else {
			out += hintStyle.Padding(0, 1).Render(label)
		}
	}
	return out
}
```

- [ ] **Step 2: Add `activeTab` and `chatUnread` to `model` struct in `internal/tui/model.go`**

In the `model` struct, after the `ready bool` field add:

```go
activeTab  int
chatUnread bool
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/tabs.go internal/tui/model.go
git commit -m "feat: add tab constants and tab bar renderer"
```

---

## Task 2: Wire tab switching into model.Update and renderContent

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add tab switching in `Update` — global key handler**

In `model.Update`, inside `case tea.KeyPressMsg:`, before the `m.showPicker` block, add:

```go
// Global tab switching — always handled regardless of active tab
switch keyStr {
case "ctrl+1":
    m.activeTab = tabChat
    m.chatUnread = false
    return m, nil
case "ctrl+2":
    m.activeTab = tabFiles
    return m, nil
case "ctrl+3":
    m.activeTab = tabGit
    return m, nil
}
```

- [ ] **Step 2: Set `chatUnread` when agent/stream messages arrive while not on chat tab**

Find the `case streamMsgEvent:` handler (around line 1560). After any append to `m.messages`, add:

```go
if m.activeTab != tabChat {
    m.chatUnread = true
}
```

Do the same in `case permissionAskMsg:` — when `m.showPermDialog = true` is set, also force `m.activeTab = tabChat` so the user sees the permission prompt:

```go
m.activeTab = tabChat
m.chatUnread = false
```

- [ ] **Step 3: Update `renderContent` to route to tab views**

In `renderContent()`, after the existing modal guard blocks (`showFullToolOutput`, `showPicker`, `showConnect`, `showPalette`) and before the `header` line, add:

```go
// Route non-modal views by active tab
switch m.activeTab {
case tabFiles:
    return m.files.View(m.width, m.height, m.styles)
case tabGit:
    return m.git.View(m.width, m.height, m.styles)
}
// tabChat falls through to existing rendering below
```

- [ ] **Step 4: Update the header to embed the tab bar**

Find:
```go
header := m.styles.Header.Render("◆ ocode") + hintStyle.Render("  ·  opencode clone v"+version.Version)
```

Replace with:
```go
tabBar := renderTabBar(m.activeTab, m.chatUnread)
headerLeft := m.styles.Header.Render("◆ ocode") + hintStyle.Render("  ·  opencode clone v"+version.Version)
headerRight := tabBar
headerPad := m.panelWidth() - lipgloss.Width(headerLeft) - lipgloss.Width(headerRight)
if headerPad < 0 {
    headerPad = 0
}
header := headerLeft + strings.Repeat(" ", headerPad) + headerRight
```

- [ ] **Step 5: Update `renderStatus` to show per-tab hints**

In `renderStatus()`, find the `suffix` variable. Wrap in a tab check:

```go
var suffix string
switch m.activeTab {
case tabFiles:
    suffix = " | e: open in editor | /: search | ctrl+1-3: switch tab"
case tabGit:
    suffix = " | tab: cycle panel | s: stage | u: unstage | c: commit | ctrl+1-3: switch tab"
default:
    suffix = " | tab: agent | ctrl+p: palette | ctrl+x: leader | ctrl+o: yolo | ctrl+y: retry"
    if m.ctrlCPressed {
        suffix = " | ctrl+c again to quit"
    } else if m.streaming {
        suffix = " | esc: stop"
    }
}
```

- [ ] **Step 6: Forward tab-local key input to sub-models**

At the very end of the `case tea.KeyPressMsg:` block (just before the final `m.input.Update(msg)` / textarea section), add:

```go
if m.activeTab == tabFiles {
    var cmd tea.Cmd
    m.files, cmd = m.files.Update(msg, m.width, m.height)
    return m, cmd
}
if m.activeTab == tabGit {
    var cmd tea.Cmd
    m.git, cmd = m.git.Update(msg, m.width, m.height)
    return m, cmd
}
```

This must be placed **after** the global key handling (ctrl+1/2/3, ctrl+p, ctrl+x, etc.) so globals take priority.

- [ ] **Step 7: Forward WindowSizeMsg to sub-models**

In the `case tea.WindowSizeMsg:` handler (around line 531), after `m.layout()`:

```go
m.files.Resize(m.width, m.height)
m.git.Resize(m.width, m.height)
```

- [ ] **Step 8: Initialize sub-models in `newModel` or wherever `model` is constructed**

Find where `model{}` is constructed (grep for `model{` in `tui.go`). Add:

```go
m.files = newFilesModel(workDir)
m.git   = newGitModel(workDir)
```

Where `workDir` is the working directory string already passed to the model.

- [ ] **Step 9: Compile check**

```bash
go build ./internal/tui/...
```

Expected: compile errors for undefined `filesModel` and `gitModel` — that's fine, they come in Tasks 3 and 4. Fix any other errors now.

- [ ] **Step 10: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: wire tab switching, routing, and sub-model delegation"
```

---

## Task 3: Editor config field

**Files:**
- Modify: `internal/config/ocodeconfig.go`

- [ ] **Step 1: Add `Editor` field to `OcodeConfig` and file struct**

In `ocodeconfig.go`, add to `OcodeConfig`:

```go
Editor string
```

Add to `ocodeConfigFile`:

```go
Editor string `json:"editor,omitempty"`
```

- [ ] **Step 2: Load `editor` field in `loadOcodeConfigFile`**

After the `permissions` block in `loadOcodeConfigFile`, add:

```go
if _, ok := raw["editor"]; ok {
    var file struct {
        Editor string `json:"editor"`
    }
    if err := json.Unmarshal(cleanData, &file); err != nil {
        return err
    }
    if file.Editor != "" {
        cfg.Editor = file.Editor
    }
    delete(raw, "editor")
}
```

- [ ] **Step 3: Add `ResolveEditor` helper to `ocodeconfig.go`**

```go
// ResolveEditor returns the editor to use for opening files.
// Priority: ocodeconfig.json "editor" field > $VISUAL > $EDITOR > "vi"
func ResolveEditor(cfg *OcodeConfig) string {
    if cfg != nil && cfg.Editor != "" {
        return cfg.Editor
    }
    if v := os.Getenv("VISUAL"); v != "" {
        return v
    }
    if v := os.Getenv("EDITOR"); v != "" {
        return v
    }
    return "vi"
}
```

- [ ] **Step 4: Write test**

In `internal/config/ocodeconfig_test.go`, add:

```go
func TestResolveEditor(t *testing.T) {
    t.Run("config wins", func(t *testing.T) {
        cfg := &OcodeConfig{Editor: "nvim"}
        t.Setenv("VISUAL", "emacs")
        if got := ResolveEditor(cfg); got != "nvim" {
            t.Fatalf("want nvim got %s", got)
        }
    })
    t.Run("VISUAL fallback", func(t *testing.T) {
        t.Setenv("VISUAL", "emacs")
        t.Setenv("EDITOR", "nano")
        if got := ResolveEditor(nil); got != "emacs" {
            t.Fatalf("want emacs got %s", got)
        }
    })
    t.Run("EDITOR fallback", func(t *testing.T) {
        t.Setenv("VISUAL", "")
        t.Setenv("EDITOR", "nano")
        if got := ResolveEditor(nil); got != "nano" {
            t.Fatalf("want nano got %s", got)
        }
    })
    t.Run("vi default", func(t *testing.T) {
        t.Setenv("VISUAL", "")
        t.Setenv("EDITOR", "")
        if got := ResolveEditor(nil); got != "vi" {
            t.Fatalf("want vi got %s", got)
        }
    })
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/config/... -run TestResolveEditor -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/ocodeconfig.go internal/config/ocodeconfig_test.go
git commit -m "feat: add editor config field and ResolveEditor helper"
```

---

## Task 4: File Explorer (`filesModel`)

**Files:**
- Create: `internal/tui/files_model.go`

- [ ] **Step 1: Write the struct and constructor**

```go
package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type fileNode struct {
	path     string // absolute path
	name     string // display name
	isDir    bool
	depth    int
	expanded bool
	loaded   bool   // children have been read
}

type filesModel struct {
	workDir  string
	nodes    []fileNode
	cursor   int
	scroll   int
	preview  viewport.Model
	fuzzy    bool
	query    string
	allPaths []string // flat list for fuzzy search
	width    int
	height   int
}

func newFilesModel(workDir string) filesModel {
	m := filesModel{workDir: workDir}
	m.preview = viewport.New(0, 0)
	m.nodes = loadDirChildren(workDir, 0)
	return m
}

func loadDirChildren(dir string, depth int) []fileNode {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	nodes := make([]fileNode, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue // skip dot files/dirs
		}
		nodes = append(nodes, fileNode{
			path:  filepath.Join(dir, name),
			name:  name,
			isDir: e.IsDir(),
			depth: depth,
		})
	}
	return nodes
}
```

- [ ] **Step 2: Add `Resize`, `Update`, `View` methods**

```go
func (m *filesModel) Resize(w, h int) {
	m.width = w
	m.height = h
	treeW := w * 35 / 100
	previewW := w - treeW - 3
	previewH := h - 3 // header + status
	if previewH < 1 {
		previewH = 1
	}
	m.preview.SetWidth(previewW)
	m.preview.SetHeight(previewH)
}

type filesModel struct { /* ... same as above ... */ }

func (m filesModel) Update(msg tea.Msg, w, h int) (filesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.fuzzy {
			return m.updateFuzzy(msg)
		}
		return m.updateTree(msg, w, h)
	}
	return m, nil
}

func (m filesModel) updateTree(msg tea.KeyPressMsg, w, h int) (filesModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "j", "down":
		if m.cursor < len(m.nodes)-1 {
			m.cursor++
			m.loadPreview()
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.loadPreview()
		}
	case "enter", "space":
		if m.cursor < len(m.nodes) {
			n := &m.nodes[m.cursor]
			if n.isDir {
				m.toggleDir(m.cursor)
			} else {
				return m, m.openInEditor(n.path)
			}
		}
	case "e":
		if m.cursor < len(m.nodes) && !m.nodes[m.cursor].isDir {
			return m, m.openInEditor(m.nodes[m.cursor].path)
		}
	case "/":
		m.fuzzy = true
		m.query = ""
		m.buildAllPaths()
	}
	return m, nil
}

func (m filesModel) updateFuzzy(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.fuzzy = false
		m.query = ""
	case "enter":
		results := fuzzyFilter(m.allPaths, m.query)
		if len(results) > 0 {
			m.navigateTo(results[0])
		}
		m.fuzzy = false
		m.query = ""
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
		}
	default:
		if len(msg.Text) > 0 {
			m.query += msg.Text
		}
	}
	return m, nil
}

func (m *filesModel) toggleDir(idx int) {
	n := &m.nodes[idx]
	if n.expanded {
		// collapse: remove children
		depth := n.depth
		end := idx + 1
		for end < len(m.nodes) && m.nodes[end].depth > depth {
			end++
		}
		m.nodes = append(m.nodes[:idx+1], m.nodes[end:]...)
		n.expanded = false
	} else {
		children := loadDirChildren(n.path, n.depth+1)
		newNodes := make([]fileNode, 0, len(m.nodes)+len(children))
		newNodes = append(newNodes, m.nodes[:idx+1]...)
		newNodes = append(newNodes, children...)
		newNodes = append(newNodes, m.nodes[idx+1:]...)
		m.nodes = newNodes
		n = &m.nodes[idx]
		n.expanded = true
		n.loaded = true
	}
}

func (m *filesModel) loadPreview() {
	if m.cursor >= len(m.nodes) {
		return
	}
	n := m.nodes[m.cursor]
	if n.isDir {
		m.preview.SetContent("[directory]")
		return
	}
	f, err := os.Open(n.path)
	if err != nil {
		m.preview.SetContent("[cannot read file]")
		return
	}
	defer f.Close()

	buf := make([]byte, 1024*1024+1) // 1MB + 1 to detect overflow
	nr, _ := f.Read(buf)
	data := buf[:nr]

	// Binary detection: null byte in first 512 bytes
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		m.preview.SetContent("[binary file]")
		return
	}

	content := string(data)
	if nr > 1024*1024 {
		content = string(data[:1024*1024]) + "\n[truncated — 1MB limit]"
	}
	m.preview.SetContent(content)
	m.preview.GotoTop()
}

func (m *filesModel) buildAllPaths() {
	m.allPaths = nil
	_ = filepath.Walk(m.workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := filepath.Base(path)
		if strings.HasPrefix(name, ".") && path != m.workDir {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(m.workDir, path)
			m.allPaths = append(m.allPaths, rel)
		}
		return nil
	})
}

func (m *filesModel) navigateTo(relPath string) {
	// Reset tree to root and expand dirs along the path
	m.nodes = loadDirChildren(m.workDir, 0)
	parts := strings.Split(relPath, string(filepath.Separator))
	current := m.workDir
	for i, part := range parts {
		for idx, n := range m.nodes {
			if n.name == part && n.path == filepath.Join(current, part) {
				if i < len(parts)-1 && n.isDir {
					m.toggleDir(idx)
				} else {
					m.cursor = idx
					m.loadPreview()
				}
				break
			}
		}
		current = filepath.Join(current, part)
	}
}

func (m filesModel) openInEditor(path string) tea.Cmd {
	// Editor resolution happens at call site via config; we use ExecProcess.
	// The caller (model.Update) must have access to config to call ResolveEditor.
	// We store the resolved editor in filesModel via SetEditor.
	editor := m.editor
	if editor == "" {
		editor = "vi"
	}
	return tea.ExecProcess(func() *tea.ExternalCommand {
		return &tea.ExternalCommand{
			Cmd:    editor,
			Args:   []string{path},
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}
	}, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}
```

> **Note on `openInEditor`:** check the bubbletea v2 API for `tea.ExecProcess` / `tea.ExternalCommand` in your version. The exact API may differ slightly. Run `go doc charm.land/bubbletea/v2 ExecProcess` to verify the signature before coding this step.

- [ ] **Step 3: Add `editor string` field to `filesModel` struct and `SetEditor` method**

Add to the `filesModel` struct:

```go
editor string
```

Add method:

```go
func (m *filesModel) SetEditor(e string) { m.editor = e }
```

Call `m.files.SetEditor(config.ResolveEditor(m.config.Ocode))` when the model initializes and when config reloads.

- [ ] **Step 4: Add `View` method**

```go
func (m filesModel) View(w, h int, styles Styles) string {
	treeW := w * 35 / 100
	previewW := w - treeW - 3

	// Build tree lines
	treeLines := make([]string, 0, len(m.nodes))
	for i, n := range m.nodes {
		indent := strings.Repeat("  ", n.depth)
		icon := "  "
		if n.isDir {
			if n.expanded {
				icon = "▾ "
			} else {
				icon = "▸ "
			}
		}
		line := indent + icon + n.name
		if i == m.cursor {
			line = lipgloss.NewStyle().Reverse(true).Width(treeW - 2).Render(line)
		}
		treeLines = append(treeLines, line)
	}
	treeContent := strings.Join(treeLines, "\n")
	treePane := borderStyle.Width(treeW - 2).Height(h - 3).Render(treeContent)

	previewPane := borderStyle.Width(previewW - 2).Render(m.preview.View())

	row := lipgloss.JoinHorizontal(lipgloss.Top, treePane, previewPane)

	header := styles.Header.Render("◆ ocode  Files") + "  " + renderTabBar(tabFiles, false)

	fuzzyBar := ""
	if m.fuzzy {
		results := fuzzyFilter(m.allPaths, m.query)
		preview := ""
		if len(results) > 3 {
			results = results[:3]
		}
		preview = strings.Join(results, "  ")
		fuzzyBar = hintStyle.Render("/ "+m.query+"  "+preview)
	}

	parts := []string{header, row}
	if fuzzyBar != "" {
		parts = append(parts, fuzzyBar)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
```

- [ ] **Step 5: Compile check**

```bash
go build ./internal/tui/...
```

Fix any API mismatches (especially `tea.ExecProcess` signature).

- [ ] **Step 6: Manual smoke test**

Run ocode, press `ctrl+2` to switch to Files tab. Verify:
- File tree renders
- `j`/`k` navigate
- `enter` on a dir expands it
- `e` on a file opens your `$EDITOR`
- `/` shows a fuzzy bar at bottom

- [ ] **Step 7: Commit**

```bash
git add internal/tui/files_model.go internal/tui/model.go
git commit -m "feat: add file explorer tab with tree, preview, and editor open"
```

---

## Task 5: Git Browser — `gitModel` scaffold and Changes section

**Files:**
- Create: `internal/tui/git_model.go`

- [ ] **Step 1: Write the struct, section constants, and constructor**

```go
package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type gitSection int

const (
	gitSectionChanges  gitSection = iota
	gitSectionLog
	gitSectionStash
	gitSectionBranches
)

type gitPanel int

const (
	gitPanelSections gitPanel = iota
	gitPanelFiles
	gitPanelDiff
)

type gitFile struct {
	status string // M, A, D, ?, etc.
	path   string
	staged bool
}

type gitCommit struct {
	hash    string
	subject string
	author  string
	age     string
}

type gitModel struct {
	workDir     string
	width       int
	height      int
	section     gitSection
	panel       gitPanel
	// Changes
	stagedFiles   []gitFile
	unstagedFiles []gitFile
	untrackedFiles []gitFile
	filesCursor   int
	// Log
	commits     []gitCommit
	commitCursor int
	commitFiles []string
	// Stash
	stashes     []string
	stashCursor int
	// Branches
	branches    []string
	branchCursor int
	// Shared diff viewport
	diff        viewport.Model
	// Commit input
	committing   bool
	commitInput  textarea.Model
	// Status message
	statusMsg   string
}

func newGitModel(workDir string) gitModel {
	m := gitModel{workDir: workDir}
	m.diff = viewport.New(0, 0)
	ci := textarea.New()
	ci.Placeholder = "Commit message..."
	ci.SetHeight(3)
	m.commitInput = ci
	m.refresh()
	return m
}
```

- [ ] **Step 2: Add `Resize` method**

```go
func (m *gitModel) Resize(w, h int) {
	m.width = w
	m.height = h
	sectW := w * 20 / 100
	filesW := w * 30 / 100
	diffW := w - sectW - filesW - 4
	diffH := h - 5
	if diffH < 1 {
		diffH = 1
	}
	m.diff.SetWidth(diffW)
	m.diff.SetHeight(diffH)
	m.commitInput.SetWidth(sectW + filesW)
}
```

- [ ] **Step 3: Add `refresh` to load git state**

```go
func (m *gitModel) refresh() {
	m.loadChanges()
	m.loadLog()
	m.loadStash()
	m.loadBranches()
}

func (m *gitModel) gitRun(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = m.workDir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (m *gitModel) loadChanges() {
	out, err := m.gitRun("status", "--porcelain")
	if err != nil {
		return
	}
	m.stagedFiles = nil
	m.unstagedFiles = nil
	m.untrackedFiles = nil
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		x, y, path := string(line[0]), string(line[1]), strings.TrimSpace(line[2:])
		switch {
		case x == "?" && y == "?":
			m.untrackedFiles = append(m.untrackedFiles, gitFile{status: "?", path: path})
		default:
			if x != " " && x != "?" {
				m.stagedFiles = append(m.stagedFiles, gitFile{status: x, path: path, staged: true})
			}
			if y != " " && y != "?" {
				m.unstagedFiles = append(m.unstagedFiles, gitFile{status: y, path: path})
			}
		}
	}
}

func (m *gitModel) loadLog() {
	out, err := m.gitRun("log", "--oneline", "--format=%h\t%s\t%an\t%cr", "-50")
	if err != nil {
		return
	}
	m.commits = nil
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) == 4 {
			m.commits = append(m.commits, gitCommit{
				hash:    parts[0],
				subject: parts[1],
				author:  parts[2],
				age:     parts[3],
			})
		}
	}
}

func (m *gitModel) loadStash() {
	out, _ := m.gitRun("stash", "list")
	if out == "" {
		m.stashes = nil
		return
	}
	m.stashes = strings.Split(out, "\n")
}

func (m *gitModel) loadBranches() {
	out, _ := m.gitRun("branch", "-a")
	m.branches = nil
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		m.branches = append(m.branches, strings.TrimSpace(strings.TrimPrefix(line, "*")))
	}
}
```

- [ ] **Step 4: Write test for `loadChanges` parsing**

In `internal/tui/git_model_test.go`:

```go
package tui

import (
	"testing"
)

func TestGitStatusParsing(t *testing.T) {
	// Simulate what loadChanges does with a fake porcelain output
	lines := []string{
		"M  internal/tui/model.go",  // staged modify
		" M main.go",                // unstaged modify
		"?? newfile.go",             // untracked
		"A  added.go",               // staged add
	}
	m := gitModel{}
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		x, y, path := string(line[0]), string(line[1]), strings.TrimSpace(line[2:])
		switch {
		case x == "?" && y == "?":
			m.untrackedFiles = append(m.untrackedFiles, gitFile{status: "?", path: path})
		default:
			if x != " " && x != "?" {
				m.stagedFiles = append(m.stagedFiles, gitFile{status: x, path: path, staged: true})
			}
			if y != " " && y != "?" {
				m.unstagedFiles = append(m.unstagedFiles, gitFile{status: y, path: path})
			}
		}
	}
	if len(m.stagedFiles) != 2 {
		t.Fatalf("want 2 staged got %d", len(m.stagedFiles))
	}
	if len(m.unstagedFiles) != 1 {
		t.Fatalf("want 1 unstaged got %d", len(m.unstagedFiles))
	}
	if len(m.untrackedFiles) != 1 {
		t.Fatalf("want 1 untracked got %d", len(m.untrackedFiles))
	}
}
```

- [ ] **Step 5: Run test**

```bash
go test ./internal/tui/... -run TestGitStatusParsing -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/git_model.go internal/tui/git_model_test.go
git commit -m "feat: git browser scaffold, Changes refresh, and status parsing"
```

---

## Task 6: Git Browser — Update (keyboard handling + operations)

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `Update` method**

```go
func (m gitModel) Update(msg tea.Msg, w, h int) (gitModel, tea.Cmd) {
	if m.committing {
		return m.updateCommitInput(msg)
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg, w, h)
	}
	return m, nil
}

func (m gitModel) updateCommitInput(msg tea.Msg) (gitModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.committing = false
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.commitInput.Value())
			if text != "" {
				if _, err := m.gitRun("commit", "-m", text); err != nil {
					m.statusMsg = "commit failed: " + err.Error()
				} else {
					m.statusMsg = "committed"
					m.committing = false
					m.commitInput.Reset()
					m.refresh()
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	return m, cmd
}

func (m gitModel) handleKey(msg tea.KeyPressMsg, w, h int) (gitModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "tab":
		m.panel = (m.panel + 1) % 3
		return m, nil
	}

	switch m.panel {
	case gitPanelSections:
		return m.handleSectionKey(key)
	case gitPanelFiles:
		return m.handleFilesKey(key)
	case gitPanelDiff:
		return m.handleDiffKey(key)
	}
	return m, nil
}

func (m gitModel) handleSectionKey(key string) (gitModel, tea.Cmd) {
	sections := []gitSection{gitSectionChanges, gitSectionLog, gitSectionStash, gitSectionBranches}
	cur := int(m.section)
	switch key {
	case "j", "down":
		if cur < len(sections)-1 {
			m.section = sections[cur+1]
			m.filesCursor = 0
			m.loadDiff()
		}
	case "k", "up":
		if cur > 0 {
			m.section = sections[cur-1]
			m.filesCursor = 0
			m.loadDiff()
		}
	case "enter":
		m.panel = gitPanelFiles
	}
	return m, nil
}

func (m gitModel) handleFilesKey(key string) (gitModel, tea.Cmd) {
	files := m.currentFileList()
	switch key {
	case "j", "down":
		if m.filesCursor < len(files)-1 {
			m.filesCursor++
			m.loadDiff()
		}
	case "k", "up":
		if m.filesCursor > 0 {
			m.filesCursor--
			m.loadDiff()
		}
	case "s":
		if m.section == gitSectionChanges && m.filesCursor < len(m.unstagedFiles)+len(m.untrackedFiles) {
			f := m.allUnstagedAndUntracked()[m.filesCursor]
			m.gitRun("add", f.path)
			m.statusMsg = "staged " + f.path
			m.refresh()
			m.loadDiff()
		}
	case "u":
		if m.section == gitSectionChanges && m.filesCursor < len(m.stagedFiles) {
			f := m.stagedFiles[m.filesCursor]
			m.gitRun("restore", "--staged", f.path)
			m.statusMsg = "unstaged " + f.path
			m.refresh()
			m.loadDiff()
		}
	case "d":
		if m.section == gitSectionChanges {
			// Show confirmation — for now set a flag and require second 'd'
			if m.statusMsg == "press d again to discard" {
				f := m.allUnstagedAndUntracked()[m.filesCursor]
				m.gitRun("restore", f.path)
				m.statusMsg = "discarded " + f.path
				m.refresh()
				m.loadDiff()
			} else {
				m.statusMsg = "press d again to discard"
			}
		} else if m.section == gitSectionStash && m.stashCursor < len(m.stashes) {
			if m.statusMsg == "press D again to drop stash" {
				ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
				m.gitRun("stash", "drop", ref)
				m.statusMsg = "stash dropped"
				m.refresh()
			} else {
				m.statusMsg = "press D again to drop stash"
			}
		}
	case "D":
		if m.section == gitSectionStash && m.stashCursor < len(m.stashes) {
			if m.statusMsg == "press D again to drop stash" {
				ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
				m.gitRun("stash", "drop", ref)
				m.statusMsg = "stash dropped"
				m.refresh()
			} else {
				m.statusMsg = "press D again to drop stash"
			}
		}
	case "c":
		if m.section == gitSectionChanges {
			m.committing = true
			m.commitInput.Reset()
			m.commitInput.Focus()
		}
	case "a":
		if m.section == gitSectionStash && m.stashCursor < len(m.stashes) {
			ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
			if _, err := m.gitRun("stash", "apply", ref); err != nil {
				m.statusMsg = "stash apply failed: " + err.Error()
			} else {
				m.statusMsg = "stash applied"
				m.refresh()
			}
		}
	case "enter":
		if m.section == gitSectionBranches && m.branchCursor < len(m.branches) {
			branch := m.branches[m.branchCursor]
			if _, err := m.gitRun("checkout", branch); err != nil {
				m.statusMsg = "checkout failed: " + err.Error()
			} else {
				m.statusMsg = "switched to " + branch
				m.refresh()
			}
		}
	}
	return m, nil
}

func (m gitModel) handleDiffKey(key string) (gitModel, tea.Cmd) {
	switch key {
	case "j", "down":
		m.diff.LineDown(1)
	case "k", "up":
		m.diff.LineUp(1)
	}
	return m, nil
}

func (m *gitModel) currentFileList() []gitFile {
	switch m.section {
	case gitSectionChanges:
		var all []gitFile
		all = append(all, m.stagedFiles...)
		all = append(all, m.allUnstagedAndUntracked()...)
		return all
	}
	return nil
}

func (m *gitModel) allUnstagedAndUntracked() []gitFile {
	var out []gitFile
	out = append(out, m.unstagedFiles...)
	out = append(out, m.untrackedFiles...)
	return out
}

func (m *gitModel) loadDiff() {
	switch m.section {
	case gitSectionChanges:
		files := m.currentFileList()
		if m.filesCursor >= len(files) {
			m.diff.SetContent("")
			return
		}
		f := files[m.filesCursor]
		var out string
		var err error
		if f.staged {
			out, err = m.gitRun("diff", "--cached", f.path)
		} else {
			out, err = m.gitRun("diff", f.path)
		}
		if err != nil {
			out = "error: " + err.Error()
		}
		m.diff.SetContent(out)
		m.diff.GotoTop()
	case gitSectionLog:
		if m.commitCursor >= len(m.commits) {
			m.diff.SetContent("")
			return
		}
		c := m.commits[m.commitCursor]
		out, err := m.gitRun("show", "--stat", c.hash)
		if err != nil {
			out = "error: " + err.Error()
		}
		m.diff.SetContent(out)
		m.diff.GotoTop()
	case gitSectionStash:
		if m.stashCursor >= len(m.stashes) {
			m.diff.SetContent("")
			return
		}
		ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
		out, _ := m.gitRun("stash", "show", "-p", ref)
		m.diff.SetContent(out)
		m.diff.GotoTop()
	case gitSectionBranches:
		if m.branchCursor >= len(m.branches) {
			m.diff.SetContent("")
			return
		}
		out, _ := m.gitRun("log", "--oneline", "-20", m.branches[m.branchCursor])
		m.diff.SetContent(out)
		m.diff.GotoTop()
	}
}
```

- [ ] **Step 2: Compile check**

```bash
go build ./internal/tui/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "feat: git browser keyboard handling and git operations"
```

---

## Task 7: Git Browser — View rendering

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `View` method**

```go
func (m gitModel) View(w, h int, styles Styles) string {
	sectW := w * 20 / 100
	filesW := w * 30 / 100
	diffW := w - sectW - filesW - 4

	focusBorder := func(focused bool) lipgloss.Style {
		if focused {
			return borderStyle.BorderForeground(lipgloss.Color("#7AA2F7"))
		}
		return borderStyle
	}

	// Sections panel
	sectionNames := []string{"Changes", "Log", "Stash", "Branches"}
	sectionLines := make([]string, len(sectionNames))
	for i, name := range sectionNames {
		if gitSection(i) == m.section {
			sectionLines[i] = lipgloss.NewStyle().Reverse(true).Width(sectW - 4).Render(name)
		} else {
			sectionLines[i] = name
		}
	}
	sectPane := focusBorder(m.panel == gitPanelSections).Width(sectW - 2).Height(h - 4).Render(
		strings.Join(sectionLines, "\n"),
	)

	// Files panel
	fileLines := m.renderFileList(filesW - 4)
	filesPane := focusBorder(m.panel == gitPanelFiles).Width(filesW - 2).Height(h - 4).Render(
		strings.Join(fileLines, "\n"),
	)

	// Diff panel
	diffPane := focusBorder(m.panel == gitPanelDiff).Width(diffW - 2).Height(h - 4).Render(
		m.diff.View(),
	)

	row := lipgloss.JoinHorizontal(lipgloss.Top, sectPane, filesPane, diffPane)

	header := styles.Header.Render("◆ ocode  Git") + "  " + renderTabBar(tabGit, false)

	status := hintStyle.Render(m.statusMsg)

	parts := []string{header, row}
	if m.committing {
		parts = append(parts, borderStyle.Width(sectW+filesW-2).Render(m.commitInput.View()))
	}
	parts = append(parts, status)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m gitModel) renderFileList(width int) []string {
	var lines []string
	switch m.section {
	case gitSectionChanges:
		if len(m.stagedFiles) > 0 {
			lines = append(lines, hintStyle.Render("● staged"))
			for _, f := range m.stagedFiles {
				lines = append(lines, "  "+f.status+" "+f.path)
			}
		}
		if len(m.unstagedFiles)+len(m.untrackedFiles) > 0 {
			lines = append(lines, hintStyle.Render("○ unstaged/untracked"))
			for _, f := range m.allUnstagedAndUntracked() {
				lines = append(lines, "  "+f.status+" "+f.path)
			}
		}
	case gitSectionLog:
		for i, c := range m.commits {
			line := fmt.Sprintf("%s  %s  %s", c.hash, c.subject, c.age)
			if i == m.commitCursor {
				line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	case gitSectionStash:
		for i, s := range m.stashes {
			line := s
			if i == m.stashCursor {
				line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	case gitSectionBranches:
		for i, b := range m.branches {
			line := b
			if i == m.branchCursor {
				line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	}
	return lines
}
```

- [ ] **Step 2: Compile check**

```bash
go build ./internal/tui/...
```

- [ ] **Step 3: Manual smoke test**

Run ocode, press `ctrl+3`. Verify:
- 3-panel layout renders
- `tab` cycles panel focus (border highlights)
- `j`/`k` navigate sections and file lists
- Diff pane updates when file selected
- `s` stages a file, list refreshes
- `c` opens commit input, `enter` commits

- [ ] **Step 4: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "feat: git browser View rendering"
```

---

## Task 8: Integration wiring and editor open in model.go

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add `files filesModel` and `git gitModel` to `model` struct**

In the `model` struct, add after `ready bool`:

```go
files filesModel
git   gitModel
```

- [ ] **Step 2: Initialize in model constructor**

Find where `model{}` is built (in `tui.go`, function `newModel` or similar). Add:

```go
m.files = newFilesModel(workDir)
m.files.SetEditor(config.ResolveEditor(m.config.Ocode))
m.git = newGitModel(workDir)
```

- [ ] **Step 3: Handle `editorFinishedMsg` to resume after editor exits**

In `model.Update`, add a case:

```go
case editorFinishedMsg:
    // TUI resumed after external editor closed. Re-layout.
    m.layout()
    if msg.err != nil {
        m.err = msg.err
    }
    return m, nil
```

- [ ] **Step 4: Compile and full build**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 5: Run all tests**

```bash
go test ./...
```

Expected: all pass. Fix any failures before proceeding.

- [ ] **Step 6: Final smoke test**

- `ctrl+1`: chat tab — verify chat works normally, streaming, permissions unaffected
- `ctrl+2`: files tab — navigate tree, open a file in editor, return to ocode
- `ctrl+3`: git tab — stage a file, write a commit message
- Switch back to `ctrl+1` mid-stream — verify agent output still appears
- Trigger a permission prompt while on files tab — verify auto-switch to chat tab

- [ ] **Step 7: Final commit**

```bash
git add internal/tui/model.go internal/tui/tui.go
git commit -m "feat: wire files and git sub-models into root model"
```

---

## Self-Review Checklist

- [x] Tab constants, bar rendering — Task 1
- [x] `ctrl+1/2/3` switching — Task 2
- [x] Chat unread badge — Task 2
- [x] Permission prompt auto-switches to chat — Task 2
- [x] Modal overlays render on top (unchanged guard order) — Task 2
- [x] `tab` key is context-sensitive per active tab — Task 2
- [x] `WindowSizeMsg` propagated to sub-models — Task 2
- [x] `editor` config field + `ResolveEditor` — Task 3
- [x] Editor resolution order: config > VISUAL > EDITOR > vi — Task 3
- [x] File tree lazy-load, expand/collapse — Task 4
- [x] Binary file detection + 1MB cap — Task 4
- [x] `/` fuzzy search — Task 4
- [x] `tea.ExecProcess` for editor open — Task 4
- [x] `editorFinishedMsg` handler — Task 8
- [x] Git Changes: stage/unstage/discard/commit — Task 6
- [x] Git Log: commit list + show stat — Task 6
- [x] Git Stash: list/apply/drop — Task 6
- [x] Git Branches: list/checkout — Task 6
- [x] Auto-refresh after mutations — Task 6 (`m.refresh()` calls)
- [x] Git error surfaced as status message — Task 6
- [x] 3-panel View with focus borders — Task 7
- [x] Status bar per-tab hints — Task 2
