# Git Tab Overhaul — Part 02: New Features

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> **Prerequisite:** Part 01 (bug fixes) must be complete.

**Goal:** Add push/pull/fetch, branch create/delete, stash push/pop, ahead/behind indicators, diff syntax coloring, and hunk-level staging.

**Files:**
- Modify: `internal/tui/git_model.go`
- Modify: `internal/tui/git_model_test.go`

---

### Task 1: Async git operations via tea.Cmd

Currently `refresh()` runs 4 synchronous git commands on the bubbletea update goroutine, causing jank on larger repos. Introduce a `gitResultMsg` to allow async execution.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Define message types**

Add near the top of `git_model.go` (after imports):

```go
type gitRefreshMsg struct {
    staged    []gitFile
    unstaged  []gitFile
    untracked []gitFile
    commits   []gitCommit
    stashes   []string
    branches  []string
    currentBranch string
    aheadBehind   string
}

type gitDiffMsg struct {
    content string
}

type gitStatusMsg struct {
    text string
}
```

- [ ] **Step 2: Add `cmdRefresh` that returns a `tea.Cmd`**

```go
func (m *gitModel) cmdRefresh() tea.Cmd {
    workDir := m.workDir
    return func() tea.Msg {
        tmp := gitModel{workDir: workDir}
        tmp.loadChanges()
        tmp.loadLog()
        tmp.loadStash()
        tmp.loadBranches()
        ab := tmp.aheadBehindString()
        return gitRefreshMsg{
            staged:        tmp.stagedFiles,
            unstaged:      tmp.unstagedFiles,
            untracked:     tmp.untrackedFiles,
            commits:       tmp.commits,
            stashes:       tmp.stashes,
            branches:      tmp.branches,
            currentBranch: tmp.currentBranch,
            aheadBehind:   ab,
        }
    }
}
```

- [ ] **Step 3: Handle `gitRefreshMsg` in `Update`**

Add a new case in `Update` (before the `tea.KeyPressMsg` case):

```go
case gitRefreshMsg:
    m.stagedFiles = msg.staged
    m.unstagedFiles = msg.unstaged
    m.untrackedFiles = msg.untracked
    m.commits = msg.commits
    m.stashes = msg.stashes
    m.branches = msg.branches
    m.currentBranch = msg.currentBranch
    m.aheadBehind = msg.aheadBehind
    m.loadDiff()
    return m, nil
```

- [ ] **Step 4: Add `aheadBehind` field to struct and `aheadBehindString` helper**

```go
aheadBehind string // e.g. "↑2 ↓1"
```

```go
func (m *gitModel) aheadBehindString() string {
    ahead, err1 := m.gitRun("rev-list", "--count", "@{u}..HEAD")
    behind, err2 := m.gitRun("rev-list", "--count", "HEAD..@{u}")
    if err1 != nil || err2 != nil {
        return "" // no upstream configured
    }
    ahead = strings.TrimSpace(ahead)
    behind = strings.TrimSpace(behind)
    if ahead == "0" && behind == "0" {
        return "✓"
    }
    parts := []string{}
    if ahead != "0" {
        parts = append(parts, "↑"+ahead)
    }
    if behind != "0" {
        parts = append(parts, "↓"+behind)
    }
    return strings.Join(parts, " ")
}
```

- [ ] **Step 5: Replace synchronous `m.refresh()` calls with `m.cmdRefresh()`**

All places that currently call `m.refresh()` should return `m.cmdRefresh()` as a `tea.Cmd` instead. Update `handleFilesKey` and other callers to return the cmd:

```go
// Instead of:
m.refresh()
m.loadDiff()
return m, nil

// Use:
return m, m.cmdRefresh()
```

Note: `newGitModel` is a constructor (not in the tea.Cmd flow), so it may still call `m.refresh()` synchronously for initial load.

- [ ] **Step 6: Show ahead/behind in header**

In `View`, update the header left section:

```go
ab := ""
if m.aheadBehind != "" {
    ab = "  " + m.aheadBehind
}
headerLeft := styles.Header.Render("◆ ocode  Git" + ab)
```

- [ ] **Step 7: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "feat: async git refresh via tea.Cmd; add ahead/behind indicator"
```

---

### Task 2: Push, pull, fetch

Add `P` for push, `p` for pull, `f` for fetch in the Changes/Branches sections.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add network ops to `handleFilesKey`**

Add these cases to `handleFilesKey`:

```go
case "f":
    if m.section == gitSectionChanges || m.section == gitSectionBranches {
        m.statusMsg = "fetching..."
        if _, err := m.gitRun("fetch", "--all"); err != nil {
            m.statusMsg = "fetch failed: " + err.Error()
        } else {
            m.statusMsg = "fetched"
        }
        return m, m.cmdRefresh()
    }
case "p":
    if m.section == gitSectionChanges || m.section == gitSectionBranches {
        m.statusMsg = "pulling..."
        if _, err := m.gitRun("pull"); err != nil {
            m.statusMsg = "pull failed: " + err.Error()
        } else {
            m.statusMsg = "pulled"
        }
        return m, m.cmdRefresh()
    }
case "P":
    if m.section == gitSectionChanges || m.section == gitSectionBranches {
        m.statusMsg = "pushing..."
        if _, err := m.gitRun("push"); err != nil {
            m.statusMsg = "push failed: " + err.Error()
        } else {
            m.statusMsg = "pushed"
        }
        return m, m.cmdRefresh()
    }
```

Note: these run synchronously on the update goroutine for now (network calls can be slow). A follow-up can make them async; for MVP this is acceptable and keeps error reporting simple.

- [ ] **Step 2: Increase gitRun timeout for network ops**

Push/pull/fetch can take longer than 10s on slow connections. Add a `gitRunTimeout` variant:

```go
func (m *gitModel) gitRunTimeout(timeout time.Duration, args ...string) (string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    cmd := exec.CommandContext(ctx, "git", args...)
    cmd.Dir = m.workDir
    out, err := cmd.Output()
    return strings.TrimSpace(string(out)), err
}
```

Update push/pull/fetch to use `m.gitRunTimeout(60*time.Second, ...)`.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "feat: add push (P), pull (p), fetch (f) keybindings"
```

---

### Task 3: Branch create and delete

Add `n` to create a new branch from current HEAD, `x` to delete a branch (with confirmation).

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `branchInput` prompt mode to struct**

```go
branchInputMode bool   // true when prompting for new branch name
branchInputText string // the branch name being typed
```

- [ ] **Step 2: Add branch input handling to `Update`**

In `Update`, add handling before key dispatch:

```go
if m.branchInputMode {
    return m.updateBranchInput(msg)
}
```

```go
func (m gitModel) updateBranchInput(msg tea.Msg) (gitModel, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch msg.String() {
        case "esc":
            m.branchInputMode = false
            m.branchInputText = ""
            return m, nil
        case "enter":
            name := strings.TrimSpace(m.branchInputText)
            if name != "" {
                if _, err := m.gitRun("checkout", "-b", name); err != nil {
                    m.statusMsg = "create branch failed: " + err.Error()
                } else {
                    m.statusMsg = "created and switched to " + name
                }
            }
            m.branchInputMode = false
            m.branchInputText = ""
            return m, m.cmdRefresh()
        case "backspace":
            if len(m.branchInputText) > 0 {
                m.branchInputText = m.branchInputText[:len(m.branchInputText)-1]
            }
            return m, nil
        default:
            if len(msg.String()) == 1 {
                m.branchInputText += msg.String()
            }
            return m, nil
        }
    }
    return m, nil
}
```

- [ ] **Step 3: Add `n` and `x` to `handleFilesKey` Branches section**

```go
case "n":
    if m.section == gitSectionBranches {
        m.branchInputMode = true
        m.branchInputText = ""
        m.statusMsg = "new branch name:"
    }
case "x":
    if m.section == gitSectionBranches && m.branchCursor < len(m.branches) {
        branch := m.branches[m.branchCursor]
        if branch == m.currentBranch {
            m.statusMsg = "cannot delete current branch"
            return m, nil
        }
        if m.pendingAction == "delete-branch" {
            m.pendingAction = ""
            if _, err := m.gitRun("branch", "-d", branch); err != nil {
                m.statusMsg = "delete failed: " + err.Error()
            } else {
                m.statusMsg = "deleted " + branch
            }
            return m, m.cmdRefresh()
        }
        m.pendingAction = "delete-branch"
        m.statusMsg = "press x again to delete " + branch
    }
```

Update the `key != "d"` guard at the top of `handleFilesKey` to also clear `pendingAction` on keys other than the confirm key for the current pending action:

```go
if key != "d" && key != "x" {
    m.pendingAction = ""
}
```

- [ ] **Step 4: Render branch input prompt in `View`**

```go
if m.branchInputMode {
    prompt := hintStyle.Render("New branch: ") + m.branchInputText + "█"
    parts = append(parts, borderStyle.Width(sectW+filesW-2).Render(prompt))
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "feat: add branch create (n) and delete (x) in Branches section"
```

---

### Task 4: Stash push and pop

Add `S` to stash current changes (with optional message), `enter` in Stash section to pop (apply + drop).

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add stash input mode to struct**

```go
stashInputMode bool
stashInputText string
```

- [ ] **Step 2: Add stash input handling in `Update`**

```go
if m.stashInputMode {
    return m.updateStashInput(msg)
}
```

```go
func (m gitModel) updateStashInput(msg tea.Msg) (gitModel, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch msg.String() {
        case "esc":
            m.stashInputMode = false
            m.stashInputText = ""
            return m, nil
        case "enter":
            args := []string{"stash", "push"}
            if msg := strings.TrimSpace(m.stashInputText); msg != "" {
                args = append(args, "-m", msg)
            }
            if _, err := m.gitRun(args...); err != nil {
                m.statusMsg = "stash failed: " + err.Error()
            } else {
                m.statusMsg = "stashed"
            }
            m.stashInputMode = false
            m.stashInputText = ""
            return m, m.cmdRefresh()
        case "backspace":
            if len(m.stashInputText) > 0 {
                m.stashInputText = m.stashInputText[:len(m.stashInputText)-1]
            }
            return m, nil
        default:
            if len(msg.String()) == 1 {
                m.stashInputText += msg.String()
            }
            return m, nil
        }
    }
    return m, nil
}
```

- [ ] **Step 3: Add `S` for stash push and `enter` for stash pop**

In `handleFilesKey`, add:

```go
case "S":
    if m.section == gitSectionChanges {
        m.stashInputMode = true
        m.stashInputText = ""
        m.statusMsg = "stash message (optional):"
    }
```

In the `case "enter":` branch within `handleFilesKey`, add a `gitSectionStash` case:

```go
case "enter":
    switch m.section {
    case gitSectionBranches:
        // ... existing checkout logic ...
    case gitSectionStash:
        if m.stashCursor < len(m.stashes) {
            ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
            if _, err := m.gitRun("stash", "pop", ref); err != nil {
                m.statusMsg = "pop failed: " + err.Error()
            } else {
                m.statusMsg = "stash popped"
            }
            return m, m.cmdRefresh()
        }
    }
```

- [ ] **Step 4: Render stash input prompt in `View`**

```go
if m.stashInputMode {
    prompt := hintStyle.Render("Stash message: ") + m.stashInputText + "█"
    parts = append(parts, borderStyle.Width(sectW+filesW-2).Render(prompt))
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "feat: add stash push (S) and stash pop (enter) in Stash section"
```

---

### Task 5: Diff syntax coloring

Pass `--color=always` to `git diff` and `git show` so ANSI escape codes are present in output. The viewport renders plain text; use `lipgloss.Color`-aware rendering by passing the raw ANSI through (bubbletea viewports pass ANSI sequences through correctly).

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Update diff commands to use `--color=always`**

In `loadDiff`, update the diff commands:

```go
case gitSectionChanges:
    // ...
    if f.staged {
        out, err = m.gitRun("diff", "--color=always", "--cached", f.path)
    } else {
        out, err = m.gitRun("diff", "--color=always", f.path)
    }
```

```go
case gitSectionLog:
    out, err := m.gitRun("show", "--color=always", "--stat", c.hash)
```

```go
case gitSectionStash:
    out, err := m.gitRun("stash", "show", "--color=always", "-p", ref)
```

- [ ] **Step 2: Verify ANSI sequences pass through the viewport**

The bubbletea viewport renders content as-is including ANSI sequences. No additional code is needed — `SetContent` with ANSI-colored text renders with color in the terminal.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "feat: enable ANSI diff coloring via --color=always"
```

---

### Task 6: Hunk-level staging

Allow the user to stage/unstage individual diff hunks. When focused on the diff pane for an unstaged file, pressing `s` stages the hunk under the cursor; for a staged file, pressing `u` unstages the hunk under the cursor.

**Architecture:** Parse the diff output into hunks (split on `^@@` boundaries). Track the current hunk index. On `s`/`u`, write a minimal valid patch for that single hunk to a temp file and run `git apply --cached` (stage) or `git apply --cached --reverse` (unstage).

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add hunk parsing**

```go
type diffHunk struct {
    header string // the @@ ... @@ line
    body   string // the full hunk including header
    start  int    // line index in viewport where hunk starts
}

func parseHunks(diff string) []diffHunk {
    var hunks []diffHunk
    var current strings.Builder
    var header string
    lineIdx := 0
    startLine := 0
    for _, line := range strings.Split(diff, "\n") {
        // Strip ANSI for hunk boundary detection
        plain := stripANSI(line)
        if strings.HasPrefix(plain, "@@") {
            if current.Len() > 0 {
                hunks = append(hunks, diffHunk{header: header, body: current.String(), start: startLine})
                current.Reset()
            }
            header = plain
            startLine = lineIdx
        }
        current.WriteString(line + "\n")
        lineIdx++
    }
    if current.Len() > 0 && header != "" {
        hunks = append(hunks, diffHunk{header: header, body: current.String(), start: startLine})
    }
    return hunks
}

// stripANSI removes ANSI escape sequences from a string for pattern matching.
func stripANSI(s string) string {
    var b strings.Builder
    inEsc := false
    for _, r := range s {
        if r == '\x1b' {
            inEsc = true
            continue
        }
        if inEsc {
            if r == 'm' {
                inEsc = false
            }
            continue
        }
        b.WriteRune(r)
    }
    return b.String()
}
```

- [ ] **Step 2: Add hunk state to struct**

```go
hunks      []diffHunk
hunkCursor int
diffRaw    string // the raw diff string for hunk extraction
diffHeader string // the "diff --git ..." + "--- a/..." + "+++ b/..." prefix lines
```

- [ ] **Step 3: Populate hunks when diff is loaded**

At the end of the Changes case in `loadDiff`, after `m.diff.SetContent(out)`:

```go
m.diffRaw = out
m.hunks = parseHunks(out)
m.hunkCursor = 0
// Extract file header (lines before first @@)
m.diffHeader = extractDiffHeader(out)
```

```go
func extractDiffHeader(diff string) string {
    var lines []string
    for _, line := range strings.Split(diff, "\n") {
        plain := stripANSI(line)
        if strings.HasPrefix(plain, "@@") {
            break
        }
        lines = append(lines, line)
    }
    return strings.Join(lines, "\n") + "\n"
}
```

- [ ] **Step 4: Add hunk navigation in `handleDiffKey`**

```go
func (m gitModel) handleDiffKey(key string) (gitModel, tea.Cmd) {
    switch key {
    case "j", "down":
        m.diff.ScrollDown(1)
    case "k", "up":
        m.diff.ScrollUp(1)
    case "]":
        if m.hunkCursor < len(m.hunks)-1 {
            m.hunkCursor++
            m.diff.SetYOffset(m.hunks[m.hunkCursor].start)
        }
    case "[":
        if m.hunkCursor > 0 {
            m.hunkCursor--
            m.diff.SetYOffset(m.hunks[m.hunkCursor].start)
        }
    case "s":
        if m.section == gitSectionChanges && m.hunkCursor < len(m.hunks) {
            return m.applyHunk(false)
        }
    case "u":
        if m.section == gitSectionChanges && m.hunkCursor < len(m.hunks) {
            return m.applyHunk(true)
        }
    }
    return m, nil
}
```

- [ ] **Step 5: Implement `applyHunk`**

```go
func (m gitModel) applyHunk(reverse bool) (gitModel, tea.Cmd) {
    if m.hunkCursor >= len(m.hunks) {
        return m, nil
    }
    hunk := m.hunks[m.hunkCursor]
    patch := m.diffHeader + hunk.body
    // Write patch to temp file
    tmp, err := os.CreateTemp("", "ocode-hunk-*.patch")
    if err != nil {
        m.statusMsg = "hunk apply: " + err.Error()
        return m, nil
    }
    defer os.Remove(tmp.Name())
    if _, err := tmp.WriteString(patch); err != nil {
        m.statusMsg = "hunk apply: " + err.Error()
        return m, nil
    }
    tmp.Close()

    args := []string{"apply", "--cached", tmp.Name()}
    if reverse {
        args = []string{"apply", "--cached", "--reverse", tmp.Name()}
    }
    if _, err := m.gitRun(args...); err != nil {
        m.statusMsg = "hunk apply failed: " + err.Error()
        return m, nil
    }
    action := "staged hunk"
    if reverse {
        action = "unstaged hunk"
    }
    m.statusMsg = action
    return m, m.cmdRefresh()
}
```

Add `"os"` to imports.

- [ ] **Step 6: Write test for hunk parsing**

```go
func TestParseHunks(t *testing.T) {
    diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package main
+
 import "fmt"
 func main() {
@@ -10,3 +11,4 @@ func main() {
 	fmt.Println("hello")
+	fmt.Println("world")
 }
`
    hunks := parseHunks(diff)
    if len(hunks) != 2 {
        t.Fatalf("want 2 hunks got %d", len(hunks))
    }
    if !strings.HasPrefix(hunks[0].header, "@@ -1,3") {
        t.Fatalf("unexpected hunk 0 header: %s", hunks[0].header)
    }
}
```

- [ ] **Step 7: Run test**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestParseHunks -v
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/tui/git_model.go internal/tui/git_model_test.go
git commit -m "feat: hunk-level staging — s/u in diff pane, [/] to navigate hunks"
```

---

### Task 7: Update keybinding hints in View

The View renders no keybinding help for all the new actions. Add a contextual hint line that changes based on the active section and panel.

**Files:**
- Modify: `internal/tui/git_model.go`

- [ ] **Step 1: Add `renderHints` helper**

```go
func (m gitModel) renderHints() string {
    if m.committing {
        return "ctrl+enter commit  esc cancel"
    }
    if m.branchInputMode || m.stashInputMode {
        return "enter confirm  esc cancel"
    }
    base := "tab next panel  "
    switch m.panel {
    case gitPanelSections:
        return base + "j/k navigate  enter focus files"
    case gitPanelFiles:
        switch m.section {
        case gitSectionChanges:
            return base + "s stage  u unstage  d discard  S stash  c commit  f fetch  p pull  P push"
        case gitSectionLog:
            return base + "j/k navigate"
        case gitSectionStash:
            return base + "enter pop  a apply  d drop"
        case gitSectionBranches:
            return base + "enter checkout  n new  x delete  f fetch  p pull  P push"
        }
    case gitPanelDiff:
        if m.section == gitSectionChanges {
            return base + "j/k scroll  [/] prev/next hunk  s stage hunk  u unstage hunk"
        }
        return base + "j/k scroll"
    }
    return base
}
```

- [ ] **Step 2: Replace static `status` line in `View` with hints + status**

```go
hints := hintStyle.Render(m.renderHints())
status := ""
if m.statusMsg != "" {
    status = hintStyle.Foreground(lipgloss.Color("#F7768E")).Render(m.statusMsg)
}
bar := hints
if status != "" {
    bar = hints + "   " + status
}
parts = append(parts, bar)
```

Remove the previous `status := hintStyle.Render(m.statusMsg)` and `parts = append(parts, status)` lines.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/git_model.go
git commit -m "feat: add contextual keybinding hint bar to git tab"
```

---

### Task 8: Full build and test pass

- [ ] **Step 1: Run all tui tests**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -v 2>&1 | tail -40
```

Expected: all PASS, no compilation errors.

- [ ] **Step 2: Build**

```bash
cd /Users/james/www/ocode && go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit if clean**

```bash
git add -p && git commit -m "chore: final cleanup after git tab feature pass"
```
