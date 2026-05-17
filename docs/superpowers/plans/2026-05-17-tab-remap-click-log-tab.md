# Tab Remap, Click Fix & Debug Log Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `ctrl+1/2/3` tab shortcuts with `alt+[`/`alt+]` cycle keys, fix tab-bar mouse clicks, and add a new `4:log` debug tab that captures all agent/tool/LLM events for the session.

**Architecture:** A global `DebugLog` ring-buffer (new file `internal/tui/debuglog.go`) is appended to by the agent layer at key lifecycle points. To avoid a circular import (`agent` → `tui`), a package-level function var `agent.DebugAppend` is set at TUI startup. The TUI model subscribes via a channel and renders entries in a new scrollable log tab. Tab switching moves to `alt+[`/`alt+]` cycle keys; header-row mouse clicks detect X position to switch tabs directly.

**Tech Stack:** Go, Bubble Tea (github.com/charmbracelet/bubbletea), lipgloss

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `internal/tui/debuglog.go` | **Create** | Ring-buffer + channel notify for debug entries |
| `internal/agent/agent.go` | **Modify** | `DebugAppend` hook var + calls at LLM/tool lifecycle |
| `internal/tui/tabs.go` | **Modify** | Add `tabLog = 3`, `tabCount = 4`, update labels |
| `internal/tui/model.go` | **Modify** | Remap shortcuts, fix tab click, add log tab state + render |

---

## Task 1: Create the DebugLog ring-buffer

**Files:**
- Create: `internal/tui/debuglog.go`

- [ ] **Step 1: Write the file**

```go
package tui

import "sync"

type DebugEntryKind string

const (
	DebugKindLLM   DebugEntryKind = "LLM"
	DebugKindTool  DebugEntryKind = "TOOL"
	DebugKindAgent DebugEntryKind = "AGENT"
	DebugKindError DebugEntryKind = "ERROR"
)

type DebugEntry struct {
	Kind    DebugEntryKind
	Message string
}

const debugLogCap = 500

// DebugLog is the global session-scoped ring-buffer for debug entries.
var DebugLog = newDebugLog()

type debugLog struct {
	mu      sync.Mutex
	entries []DebugEntry
	notify  chan struct{}
}

func newDebugLog() *debugLog {
	return &debugLog{
		entries: make([]DebugEntry, 0, debugLogCap),
		notify:  make(chan struct{}, 1),
	}
}

func (d *debugLog) Append(e DebugEntry) {
	d.mu.Lock()
	if len(d.entries) >= debugLogCap {
		copy(d.entries, d.entries[1:])
		d.entries = d.entries[:debugLogCap-1]
	}
	d.entries = append(d.entries, e)
	d.mu.Unlock()
	select {
	case d.notify <- struct{}{}:
	default:
	}
}

func (d *debugLog) Snapshot() []DebugEntry {
	d.mu.Lock()
	out := make([]DebugEntry, len(d.entries))
	copy(out, d.entries)
	d.mu.Unlock()
	return out
}

func (d *debugLog) Clear() {
	d.mu.Lock()
	d.entries = d.entries[:0]
	d.mu.Unlock()
}

func (d *debugLog) Notify() chan struct{} {
	return d.notify
}
```

- [ ] **Step 2: Build to verify no syntax errors**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/...
```
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/tui/debuglog.go
git commit -m "feat: add DebugLog ring-buffer for session debug entries"
```

---

## Task 2: Instrument the agent layer

**Files:**
- Modify: `internal/agent/agent.go`

Key facts verified from the code:
- The LLM call is at `agent.go:135`: `resp, err := a.client.Chat(messages, toolDefs)`
- `resp` is `*Message`; `resp.Usage` is `*TokenUsage`
- `TokenUsage` fields are `PromptTokens *int64` and `CompletionTokens *int64` (from `telemetry.go:10-11`)
- Tool calls are dispatched in `executeToolCall` at line 328+

- [ ] **Step 1: Add the hook var and helper at the top of `agent.go`**

After the `package agent` declaration and imports, add these two items in the `var` block (or just below the existing `var llmHTTPClient` line if there is no var block):

```go
// DebugAppend is injected by the TUI layer at startup. Nil = no-op.
var DebugAppend func(kind, msg string)

func emitDebug(kind, msg string) {
	if DebugAppend != nil {
		DebugAppend(kind, msg)
	}
}
```

- [ ] **Step 2: Emit LLM start/done around the Chat call (line ~134-136)**

Find the block:
```go
a.activity.setLLMRunning(true)
resp, err := a.client.Chat(messages, toolDefs)
a.activity.setLLMRunning(false)
```

Replace with:
```go
emitDebug("LLM", fmt.Sprintf("→ %s/%s [%d msgs]", a.client.GetProvider(), a.client.GetModel(), len(messages)))
a.activity.setLLMRunning(true)
resp, err := a.client.Chat(messages, toolDefs)
a.activity.setLLMRunning(false)
if err != nil {
	emitDebug("ERROR", fmt.Sprintf("LLM error: %v", err))
} else if resp.Usage != nil {
	in, out := int64(0), int64(0)
	if resp.Usage.PromptTokens != nil {
		in = *resp.Usage.PromptTokens
	}
	if resp.Usage.CompletionTokens != nil {
		out = *resp.Usage.CompletionTokens
	}
	emitDebug("LLM", fmt.Sprintf("← tokens in=%d out=%d", in, out))
}
```

Note: the original `if err != nil { return nil, err }` check still exists a few lines below — do not remove it. The `emitDebug` calls above are additions before the existing error return.

- [ ] **Step 3: Emit tool start/done/error in `executeToolCall`**

Find the function `func (a *Agent) executeToolCall(name string, args json.RawMessage) (string, error)` (line ~328).

At the very start of the function body add:
```go
emitDebug("TOOL", fmt.Sprintf("→ %s %s", name, truncateDebugArgs(args, 120)))
```

Find where the function returns the result (the final `return result, err` or equivalent). Before that return, add:
```go
if err != nil {
	emitDebug("ERROR", fmt.Sprintf("tool %s: %v", name, err))
} else {
	emitDebug("TOOL", fmt.Sprintf("← %s (ok)", name))
}
```

Add the helper at the bottom of `agent.go`:
```go
func truncateDebugArgs(args json.RawMessage, max int) string {
	s := string(args)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
```

- [ ] **Step 4: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/agent/...
```
Expected: no output

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat: instrument agent with DebugAppend hooks for LLM and tool events"
```

---

## Task 3: Add tabLog and tabCount to tabs.go

**Files:**
- Modify: `internal/tui/tabs.go`

- [ ] **Step 1: Replace the entire file**

```go
package tui

import (
	"charm.land/lipgloss/v2"
)

const (
	tabChat  = 0
	tabFiles = 1
	tabGit   = 2
	tabLog   = 3
	tabCount = 4
)

func renderTabBar(active int, unread bool) string {
	labels := []string{"1:chat", "2:files", "3:git", "4:log"}
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

- [ ] **Step 2: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/...
```
Expected: no output

- [ ] **Step 3: Commit**

```bash
git add internal/tui/tabs.go
git commit -m "feat: add tabLog=3 and tabCount=4 constants, 4:log tab label"
```

---

## Task 4: Wire TUI to DebugAppend and subscribe to Notify

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add `logViewport` and `logEntries` to the `model` struct**

In the `model` struct (line ~118), add two fields after the existing `viewport` field:

```go
logViewport  viewport.Model
logEntries   []DebugEntry
```

- [ ] **Step 2: Initialize `logViewport` in the `New` function**

Find where `vp := viewport.New(...)` is called (line ~357). Right after that (or wherever the model struct is returned), initialize the log viewport:

```go
logVP := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
```

Then set it on the model before returning:
```go
m.logViewport = logVP
```

- [ ] **Step 3: Inject DebugAppend in the `New` function**

In the `New` function, after the model is constructed, add:

```go
agent.DebugAppend = func(kind, msg string) {
    DebugLog.Append(DebugEntry{Kind: DebugEntryKind(kind), Message: msg})
}
```

- [ ] **Step 4: Add `debugLogMsg` type**

Near the other message type definitions (around line 105), add:

```go
type debugLogMsg struct{}
```

- [ ] **Step 5: Add `waitForDebugLog` command function**

Add this near the other `listenActivity` style helpers:

```go
func waitForDebugLog() tea.Cmd {
    return func() tea.Msg {
        <-DebugLog.Notify()
        return debugLogMsg{}
    }
}
```

- [ ] **Step 6: Start the listener in `Init()`**

`Init()` is at line 418:
```go
func (m model) Init() tea.Cmd {
    return textarea.Blink
}
```

Replace with:
```go
func (m model) Init() tea.Cmd {
    return tea.Batch(textarea.Blink, waitForDebugLog())
}
```

- [ ] **Step 7: Handle `debugLogMsg` in `Update`**

In the `switch msg := msg.(type)` block, add:

```go
case debugLogMsg:
    m.logEntries = DebugLog.Snapshot()
    if m.activeTab == tabLog {
        m.refreshLogViewport()
        m.logViewport.GotoBottom()
    }
    return m, waitForDebugLog()
```

- [ ] **Step 8: Resize `logViewport` on `WindowSizeMsg`**

In `case tea.WindowSizeMsg:`, after `m.git.Resize(m.width, m.height)` (line ~514), add:

```go
m.logViewport, _ = m.logViewport.Update(tea.WindowSizeMsg{
    Width:  m.panelWidth() - 2,
    Height: m.height - m.bottomChromeHeight(m.panelWidth()) - 1,
})
```

- [ ] **Step 9: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/...
```
Expected: no output

- [ ] **Step 10: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: wire TUI to DebugLog notify channel, logViewport, and DebugAppend injection"
```

---

## Task 5: Remap tab shortcuts to alt+[ / alt+]

**Files:**
- Modify: `internal/tui/model.go`

Note: `ctrl+[` = `Esc` (0x1B) in standard terminals — do NOT use it. `alt+[` is distinct and safe.

- [ ] **Step 1: Replace the global tab switching block (lines ~519-531)**

Find:
```go
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

Replace with:
```go
switch keyStr {
case "alt+[":
    m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
    if m.activeTab == tabChat {
        m.chatUnread = false
    }
    return m, nil
case "alt+]":
    m.activeTab = (m.activeTab + 1) % tabCount
    if m.activeTab == tabChat {
        m.chatUnread = false
    }
    return m, nil
}
```

- [ ] **Step 2: Update status bar suffix strings in `renderStatus`**

Find (around line 2313):
```go
case tabFiles:
    suffix = " | e: open in editor | /: search | ctrl+1-3: switch tab"
case tabGit:
    suffix = " | tab: cycle panel | s: stage | u: unstage | c: commit | ctrl+1-3: switch tab"
default:
    suffix = " | tab: agent | ctrl+p: palette | ctrl+x: leader | ctrl+o: yolo | ctrl+y: retry"
```

Replace with:
```go
case tabFiles:
    suffix = " | e: open in editor | /: search | alt+[/]: switch tab"
case tabGit:
    suffix = " | tab: cycle panel | s: stage | u: unstage | c: commit | alt+[/]: switch tab"
case tabLog:
    suffix = " | j/k: scroll | c: clear | alt+[/]: switch tab"
default:
    suffix = " | tab: agent | ctrl+p: palette | ctrl+x: leader | ctrl+o: yolo | ctrl+y: retry"
```

- [ ] **Step 3: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/...
```
Expected: no output

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: remap tab switching from ctrl+1/2/3 to alt+[/alt+]"
```

---

## Task 6: Fix tab-bar mouse click detection

**Files:**
- Modify: `internal/tui/model.go`

The header row is row 0 but its rendered height may be >1. The tab bar is right-aligned within the panel width.

- [ ] **Step 1: Add `tabForClick` helper**

Add this method near `sidebarFileForClick` (line ~2621):

```go
// tabForClick returns the tab index if the click lands on the tab bar in the header row.
// The tab bar is right-aligned in row 0 of the left panel.
func (m model) tabForClick(msg tea.MouseClickMsg) (int, bool) {
    mouse := msg.Mouse()
    // Header occupies row 0 only (single-line header rendered by lipgloss join)
    headerHeight := lipgloss.Height(m.styles.Header.Render("◆ ocode"))
    if mouse.Y >= headerHeight {
        return 0, false
    }
    tabBar := renderTabBar(m.activeTab, m.chatUnread)
    barWidth := lipgloss.Width(tabBar)
    barStartX := m.panelWidth() - barWidth
    if mouse.X < barStartX {
        return 0, false
    }
    // Walk label widths to find which tab was clicked.
    // Use hintStyle width because inactive tabs use hintStyle; active uses reverse — same padding.
    labels := []string{"1:chat", "2:files", "3:git", "4:log"}
    x := barStartX
    for i, label := range labels {
        w := lipgloss.Width(hintStyle.Padding(0, 1).Render(label))
        if mouse.X < x+w {
            return i, true
        }
        x += w
    }
    return 0, false
}
```

- [ ] **Step 2: Handle tab click in `tea.MouseClickMsg`**

In `case tea.MouseClickMsg:`, inside `if msg.Button == tea.MouseLeft {`, add as the **first** check:

```go
if tab, ok := m.tabForClick(msg); ok {
    m.activeTab = tab
    if tab == tabChat {
        m.chatUnread = false
    }
    return m, nil
}
```

- [ ] **Step 3: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/...
```
Expected: no output

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go
git commit -m "fix: detect tab-bar mouse clicks and switch active tab"
```

---

## Task 7: Render the log tab

**Files:**
- Modify: `internal/tui/model.go`

Note: `View()` is a value receiver (`func (m model) View()`). All render helpers called from `View()` must also be value receivers to avoid mutating a copy. `refreshLogViewport` must be a pointer receiver (called from `Update`); `renderLogTab` must be a value receiver.

- [ ] **Step 1: Add `refreshLogViewport` pointer-receiver method**

This is called from `Update` (pointer context) to update the viewport content:

```go
func (m *model) refreshLogViewport() {
    kindColor := map[DebugEntryKind]string{
        DebugKindLLM:   "#7AA2F7",
        DebugKindTool:  "#E0AF68",
        DebugKindAgent: "#9ECE6A",
        DebugKindError: "#F7768E",
    }
    var lines []string
    for _, e := range m.logEntries {
        col, ok := kindColor[e.Kind]
        if !ok {
            col = "#565F89"
        }
        tag := lipgloss.NewStyle().Foreground(lipgloss.Color(col)).Bold(true).Render(fmt.Sprintf("%-5s", string(e.Kind)))
        lines = append(lines, tag+" "+e.Message)
    }
    if len(lines) == 0 {
        m.logViewport.SetContent(hintStyle.Render("  no debug entries yet"))
    } else {
        m.logViewport.SetContent(strings.Join(lines, "\n"))
    }
}
```

- [ ] **Step 2: Add `renderLogTab` value-receiver method**

```go
func (m model) renderLogTab() string {
    tabBar := renderTabBar(m.activeTab, m.chatUnread)
    headerLeft := m.styles.Header.Render("◆ ocode") + hintStyle.Render("  ·  debug log")
    headerPad := m.panelWidth() - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar)
    if headerPad < 0 {
        headerPad = 0
    }
    header := headerLeft + strings.Repeat(" ", headerPad) + tabBar
    content := borderStyle.Width(m.panelWidth()-2).Render(m.logViewport.View())
    status := m.renderStatus()
    left := lipgloss.JoinVertical(lipgloss.Left, header, content, status)
    if m.sidebarEnabled() {
        return lipgloss.JoinHorizontal(lipgloss.Top, left, m.renderSidebar())
    }
    return left
}
```

- [ ] **Step 3: Route the log tab in `View`**

In `View()`, in the `switch m.activeTab` block (around line 2264), add before `// tabChat falls through`:

```go
case tabLog:
    return m.renderLogTab()
```

- [ ] **Step 4: Handle `j`/`k`/`c` key input for log tab**

In `case tea.KeyPressMsg:`, after the `alt+[`/`alt+]` block and before further per-tab routing, add:

```go
if m.activeTab == tabLog {
    switch keyStr {
    case "j", "down":
        m.logViewport.ScrollDown(1)
        return m, nil
    case "k", "up":
        m.logViewport.ScrollUp(1)
        return m, nil
    case "c":
        DebugLog.Clear()
        m.logEntries = nil
        m.refreshLogViewport()
        return m, nil
    }
}
```

- [ ] **Step 5: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/tui/...
```
Expected: no output

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: render 4:log debug tab with colored entries, scroll, and clear"
```

---

## Task 8: Smoke test end-to-end

- [ ] **Step 1: Run ocode**

```bash
cd /Users/james/www/ocode && go run .
```

- [ ] **Step 2: Verify tab cycling**

Press `alt+]` repeatedly — should cycle: chat → files → git → log → chat (wraps).  
Press `alt+[` — should cycle backwards.

- [ ] **Step 3: Verify tab bar click**

Click on `1:chat`, `2:files`, `3:git`, `4:log` labels in the header — each should switch the active tab.

- [ ] **Step 4: Verify log tab content**

Send a message to the agent. Switch to `4:log`. Verify `LLM` and `TOOL` entries appear with correct colors (blue/yellow).

- [ ] **Step 5: Verify clear**

Press `c` in the log tab — entries clear, showing "no debug entries yet".

- [ ] **Step 6: Final fix commit if needed**

```bash
git add -p
git commit -m "fix: smoke test corrections for log tab"
```
