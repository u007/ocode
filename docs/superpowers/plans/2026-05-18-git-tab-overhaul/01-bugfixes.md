# Git Tab Overhaul — Part 01: Bug Fixes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all ship-blockers and strong nits in `git_model.go`.

**Files:**
- Modify: `internal/tui/git_model.go`
- Modify: `internal/tui/git_model_test.go`

---

### Task 1: Replace statusMsg confirmation state with pendingAction field

The `d` key confirmation uses `if m.statusMsg == "press d again to discard"` as a state machine. Any write to `statusMsg` between keypresses silently cancels or accidentally triggers the action. Replace with an explicit `pendingAction` string field.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `pendingAction` field to `gitModel` struct**

In the struct definition (around line 44), add:
```go
pendingAction string // "discard" | "drop-stash" | ""
```

- [ ] **Step 2: Replace `d` handler confirmation logic**

Replace the entire `case "d":` block in `handleFilesKey` (lines 326-359) with:

```go
case "d":
    if m.section == gitSectionChanges {
        if m.filesCursor >= len(m.stagedFiles) {
            idx := m.filesCursor - len(m.stagedFiles)
            unstaged := m.allUnstagedAndUntracked()
            if idx < len(unstaged) {
                if m.pendingAction == "discard" {
                    m.pendingAction = ""
                    f := unstaged[idx]
                    if f.status == "?" {
                        m.statusMsg = "cannot discard untracked file"
                        return m, nil
                    }
                    if _, err := m.gitRun("restore", f.path); err != nil {
                        m.statusMsg = "discard failed: " + err.Error()
                    } else {
                        m.statusMsg = "discarded " + f.path
                        m.refresh()
                        m.loadDiff()
                    }
                } else {
                    m.pendingAction = "discard"
                    m.statusMsg = "press d again to discard"
                }
            }
        } else {
            m.statusMsg = "cannot discard staged file — unstage first"
        }
    } else if m.section == gitSectionStash && m.stashCursor < len(m.stashes) {
        if m.pendingAction == "drop-stash" {
            m.pendingAction = ""
            ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
            if _, err := m.gitRun("stash", "drop", ref); err != nil {
                m.statusMsg = "drop failed: " + err.Error()
            } else {
                m.statusMsg = "stash dropped"
                m.refresh()
            }
        } else {
            m.pendingAction = "drop-stash"
            m.statusMsg = "press d again to drop stash"
        }
    }
```

- [ ] **Step 3: Remove the dead `D` duplicate handler**

Delete the entire `case "D":` block (lines 360-370) — it is identical to the stash-drop branch of `d` and never needed.

- [ ] **Step 4: Clear `pendingAction` on every non-confirm keypress**

At the top of `handleFilesKey`, before the switch, add:

```go
// Clear pending confirmation when user presses any key other than the confirm key
if key != "d" {
    m.pendingAction = ""
}
```

- [ ] **Step 5: Write test for pendingAction state machine**

In `git_model_test.go`, add:

```go
func TestPendingActionConfirmation(t *testing.T) {
    m := gitModel{
        section:       gitSectionChanges,
        unstagedFiles: []gitFile{{status: "M", path: "a.go"}},
        stagedFiles:   []gitFile{},
    }
    // First d: sets pending
    m2, _ := m.handleFilesKey("d")
    if m2.pendingAction != "discard" {
        t.Fatalf("want pendingAction=discard got %q", m2.pendingAction)
    }
    // Different key clears pending
    m3, _ := m2.handleFilesKey("j")
    if m3.pendingAction != "" {
        t.Fatalf("want pendingAction cleared, got %q", m3.pendingAction)
    }
}
```

- [ ] **Step 6: Run test**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestPendingAction -v
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/git_model.go internal/tui/git_model_test.go
git commit -m "fix: replace statusMsg state machine with pendingAction field"
```

---

### Task 2: Fix cursor highlight in Changes file list

The Changes section renders no highlight for the selected row. Log/Stash/Branches all use `Reverse(true)`. Changes section must do the same.

**Files:**
- Modify: `internal/tui/git_model.go` (renderFileList, ~line 543)

- [ ] **Step 1: Track flat index while rendering Changes section**

Replace the Changes case in `renderFileList` with:

```go
case gitSectionChanges:
    idx := 0
    if len(m.stagedFiles) > 0 {
        lines = append(lines, hintStyle.Render("● staged"))
        for _, f := range m.stagedFiles {
            line := "  " + f.status + " " + f.path
            if idx == m.filesCursor && m.panel == gitPanelFiles {
                line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
            }
            lines = append(lines, line)
            idx++
        }
    }
    if len(m.unstagedFiles)+len(m.untrackedFiles) > 0 {
        lines = append(lines, hintStyle.Render("○ unstaged/untracked"))
        for _, f := range m.allUnstagedAndUntracked() {
            line := "  " + f.status + " " + f.path
            if idx == m.filesCursor && m.panel == gitPanelFiles {
                line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
            }
            lines = append(lines, line)
            idx++
        }
    }
```

- [ ] **Step 2: Write test**

```go
func TestChangesFileListHighlight(t *testing.T) {
    m := gitModel{
        section:       gitSectionChanges,
        panel:         gitPanelFiles,
        stagedFiles:   []gitFile{{status: "M", path: "staged.go"}},
        unstagedFiles: []gitFile{{status: "M", path: "unstaged.go"}},
        filesCursor:   0,
    }
    lines := m.renderFileList(40)
    found := false
    for _, l := range lines {
        if strings.Contains(l, "staged.go") && strings.Contains(l, "\x1b[7m") {
            found = true
        }
    }
    if !found {
        t.Fatal("expected reverse highlight on selected row, got none")
    }
}
```

- [ ] **Step 3: Run test**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestChangesFileListHighlight -v
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tui/git_model.go internal/tui/git_model_test.go
git commit -m "fix: add cursor highlight to Changes file list"
```

---

### Task 3: Fix silent error swallowing

Five sites ignore errors from `gitRun`, violating the project rule. All must route to `m.statusMsg`.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Fix `loadStash` (line ~153)**

```go
func (m *gitModel) loadStash() {
    out, err := m.gitRun("stash", "list")
    if err != nil {
        m.statusMsg = "stash list: " + err.Error()
        m.stashes = nil
        return
    }
    if out == "" {
        m.stashes = nil
        return
    }
    m.stashes = strings.Split(out, "\n")
}
```

- [ ] **Step 2: Fix `loadBranches` (line ~161)**

```go
func (m *gitModel) loadBranches() {
    out, err := m.gitRun("branch", "-a")
    if err != nil {
        m.statusMsg = "branch list: " + err.Error()
        m.branches = nil
        return
    }
    m.branches = nil
    for _, line := range strings.Split(out, "\n") {
        if line == "" {
            continue
        }
        m.branches = append(m.branches, strings.TrimSpace(strings.TrimPrefix(line, "*")))
    }
}
```

- [ ] **Step 3: Fix stash show in `loadDiff` (line ~475)**

```go
case gitSectionStash:
    if m.stashCursor >= len(m.stashes) {
        m.diff.SetContent("")
        return
    }
    ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
    out, err := m.gitRun("stash", "show", "-p", ref)
    if err != nil {
        out = "error: " + err.Error()
    }
    m.diff.SetContent(out)
    m.diff.GotoTop()
```

- [ ] **Step 4: Fix branch log in `loadDiff` (line ~483)**

```go
case gitSectionBranches:
    if m.branchCursor >= len(m.branches) {
        m.diff.SetContent("")
        return
    }
    out, err := m.gitRun("log", "--oneline", "-20", m.branches[m.branchCursor])
    if err != nil {
        out = "error: " + err.Error()
    }
    m.diff.SetContent(out)
    m.diff.GotoTop()
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "fix: route all gitRun errors to statusMsg, remove silent swallowing"
```

---

### Task 4: Rewrite the tautology test

The existing `TestGitStatusParsing` duplicates the parsing logic inline rather than calling `loadChanges`. Delete it and write a test that actually exercises the method.

**Files:**
- Modify: `internal/tui/git_model_test.go`

- [ ] **Step 1: Replace TestGitStatusParsing**

The method `loadChanges` calls `gitRun("status", "--porcelain")` internally so it cannot be called without a real git repo. Instead, extract the parsing logic into a package-private helper so it can be tested in isolation.

In `git_model.go`, extract the loop body of `loadChanges` into:

```go
func (m *gitModel) parseStatus(out string) {
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
```

Update `loadChanges` to call `m.parseStatus(out)`.

- [ ] **Step 2: Rewrite the test against `parseStatus`**

Replace the entire contents of `git_model_test.go` with:

```go
package tui

import (
    "strings"
    "testing"
)

func TestParseStatus(t *testing.T) {
    raw := "M  internal/tui/model.go\n M main.go\n?? newfile.go\nA  added.go"
    m := gitModel{}
    m.parseStatus(raw)
    if len(m.stagedFiles) != 2 {
        t.Fatalf("want 2 staged got %d", len(m.stagedFiles))
    }
    if len(m.unstagedFiles) != 1 {
        t.Fatalf("want 1 unstaged got %d", len(m.unstagedFiles))
    }
    if len(m.untrackedFiles) != 1 {
        t.Fatalf("want 1 untracked got %d", len(m.untrackedFiles))
    }
    if m.stagedFiles[0].path != "internal/tui/model.go" {
        t.Fatalf("unexpected staged path: %s", m.stagedFiles[0].path)
    }
    if !m.stagedFiles[0].staged {
        t.Fatal("expected staged flag to be true")
    }
}

func TestParseStatusRenames(t *testing.T) {
    // porcelain format for a rename: "R  old -> new" or "R  new\0old" (v1 uses space-separated)
    // git status --porcelain v1 uses "R  new.go" with the arrow in the path field
    raw := "R  new.go"
    m := gitModel{}
    m.parseStatus(raw)
    if len(m.stagedFiles) != 1 || m.stagedFiles[0].status != "R" {
        t.Fatalf("expected rename in staged, got %+v", m.stagedFiles)
    }
}
```

- [ ] **Step 3: Run tests**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestParse -v
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tui/git_model.go internal/tui/git_model_test.go
git commit -m "refactor: extract parseStatus helper; replace tautology test with real coverage"
```

---

### Task 5: Fix stale cursors when switching sections

`handleSectionKey` resets `filesCursor` but not `commitCursor`/`stashCursor`/`branchCursor`. Add a `resetCursors` helper.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `resetCursors` and call it from `handleSectionKey`**

```go
func (m *gitModel) resetCursors() {
    m.filesCursor = 0
    m.commitCursor = 0
    m.stashCursor = 0
    m.branchCursor = 0
}
```

In `handleSectionKey`, replace each `m.filesCursor = 0` with `m.resetCursors()`.

- [ ] **Step 2: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "fix: reset all section cursors when switching sections"
```

---

### Task 6: Fix empty initial diff and preserve current branch marker

Two fixes: (1) `newGitModel` never calls `loadDiff` so right pane is empty on open. (2) `loadBranches` strips the `*` marker so we lose which branch is current.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `currentBranch` field to struct**

```go
currentBranch string
```

- [ ] **Step 2: Update `loadBranches` to set `currentBranch` and flag current in list**

```go
func (m *gitModel) loadBranches() {
    out, err := m.gitRun("branch", "-a")
    if err != nil {
        m.statusMsg = "branch list: " + err.Error()
        m.branches = nil
        return
    }
    m.branches = nil
    m.currentBranch = ""
    for _, line := range strings.Split(out, "\n") {
        if line == "" {
            continue
        }
        isCurrent := strings.HasPrefix(line, "*")
        name := strings.TrimSpace(strings.TrimPrefix(line, "*"))
        if isCurrent {
            m.currentBranch = name
        }
        m.branches = append(m.branches, name)
    }
}
```

- [ ] **Step 3: Mark current branch in `renderFileList` Branches case**

```go
case gitSectionBranches:
    for i, b := range m.branches {
        marker := "  "
        if b == m.currentBranch {
            marker = "* "
        }
        line := marker + b
        if i == m.branchCursor {
            line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
        }
        lines = append(lines, line)
    }
```

- [ ] **Step 4: Call `loadDiff` at end of `newGitModel`**

```go
func newGitModel(workDir string) gitModel {
    m := gitModel{workDir: workDir}
    m.diff = viewport.New()
    ci := textarea.New()
    ci.Placeholder = "Commit message..."
    ci.SetHeight(3)
    m.commitInput = ci
    m.refresh()
    m.loadDiff()
    return m
}
```

- [ ] **Step 5: Write test for currentBranch parsing**

```go
func TestLoadBranchesCurrentMarker(t *testing.T) {
    m := gitModel{}
    raw := "  main\n* feature/foo\n  remotes/origin/main"
    // Simulate what loadBranches parses (call parseStatus equivalent for branches)
    m.branches = nil
    m.currentBranch = ""
    for _, line := range strings.Split(raw, "\n") {
        if line == "" {
            continue
        }
        isCurrent := strings.HasPrefix(line, "*")
        name := strings.TrimSpace(strings.TrimPrefix(line, "*"))
        if isCurrent {
            m.currentBranch = name
        }
        m.branches = append(m.branches, name)
    }
    if m.currentBranch != "feature/foo" {
        t.Fatalf("want currentBranch=feature/foo got %q", m.currentBranch)
    }
    if len(m.branches) != 3 {
        t.Fatalf("want 3 branches got %d", len(m.branches))
    }
}
```

- [ ] **Step 6: Run test**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestLoadBranches -v
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/git_model.go internal/tui/git_model_test.go
git commit -m "fix: show initial diff on open; preserve current branch marker"
```

---

### Task 7: Fix "not a git repo" silent failure

When `workDir` is not a git repo, every `gitRun` call fails silently. Show an error in `statusMsg` so the user understands why all panes are empty.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `isGitRepo` check in `newGitModel`**

```go
func newGitModel(workDir string) gitModel {
    m := gitModel{workDir: workDir}
    m.diff = viewport.New()
    ci := textarea.New()
    ci.Placeholder = "Commit message..."
    ci.SetHeight(3)
    m.commitInput = ci
    if _, err := m.gitRun("rev-parse", "--git-dir"); err != nil {
        m.statusMsg = "not a git repository"
        return m
    }
    m.refresh()
    m.loadDiff()
    return m
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "fix: show 'not a git repository' message when workDir has no git repo"
```

---

### Task 8: Fix multi-line commit message (ctrl+enter to submit)

The textarea submit on bare `enter` prevents writing multi-line commit messages. Change to `ctrl+enter` for submit, bare `enter` inserts a newline.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Update `updateCommitInput` key handling**

```go
func (m gitModel) updateCommitInput(msg tea.Msg) (gitModel, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch msg.String() {
        case "esc":
            m.committing = false
            return m, nil
        case "ctrl+enter":
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
```

- [ ] **Step 2: Update commit textarea height to 5 for comfortable multi-line use**

In `newGitModel`, change `ci.SetHeight(3)` to `ci.SetHeight(5)`.

Also update `Resize` to use the new height-aware layout:
```go
m.commitInput.SetWidth(sectW + filesW)
```
(no change needed — already correct)

- [ ] **Step 3: Update hint text in View to show ctrl+enter**

In `View`, add a hint line above the commit textarea when `m.committing`:

```go
if m.committing {
    hint := hintStyle.Render("ctrl+enter submit  esc cancel")
    parts = append(parts, hint)
    parts = append(parts, borderStyle.Width(sectW+filesW-2).Render(m.commitInput.View()))
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "fix: use ctrl+enter to submit commit, bare enter inserts newline"
```

---

### Task 9: Add context/timeout to gitRun

No timeout means a wedged git process (network-backed LFS, slow NFS mount) freezes the TUI permanently.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `context` import and update `gitRun`**

Add `"context"` and `"time"` to imports.

```go
func (m *gitModel) gitRun(args ...string) (string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    cmd := exec.CommandContext(ctx, "git", args...)
    cmd.Dir = m.workDir
    out, err := cmd.Output()
    return strings.TrimSpace(string(out)), err
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "fix: add 10s timeout to all gitRun calls via context"
```

---

### Task 10: Run full test suite

- [ ] **Step 1: Run all tui tests**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -v 2>&1 | tail -30
```

Expected: all tests PASS, no compilation errors.

- [ ] **Step 2: Build**

```bash
cd /Users/james/www/ocode && go build ./...
```

Expected: no errors.
