# Parallel Tool/Agent Execution + Activity Indicators Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run tool calls and sub-agent calls concurrently when the LLM returns multiple tool calls in one response, and display a live activity indicator row in the TUI showing what is running.

**Architecture:** Add `Parallel() bool` to the `Tool` interface to partition tool calls into safe-concurrent vs sequential buckets. Add an `ActivityTracker` to `Agent` that pushes state snapshots via a channel to the TUI. The TUI renders a second status row (visible only when active) from those snapshots.

**Tech Stack:** Go, BubbleTea (`charm.land/bubbletea/v2`), lipgloss (`charm.land/lipgloss/v2`), `sync.WaitGroup`, `sync.Mutex`

---

## File Map

| File | Change |
|------|--------|
| `internal/tool/tool.go` | Add `Parallel() bool` to `Tool` interface |
| `internal/tool/file.go` | Implement `Parallel()` on `ReadTool`, `WriteTool`, `EditTool` |
| `internal/tool/search.go` | Implement `Parallel()` on `GlobTool`, `GrepTool`, `ListTool` |
| `internal/tool/exec.go` | Implement `Parallel()` on `BashTool` |
| `internal/tool/web.go` | Implement `Parallel()` on `WebFetchTool`, `WebSearchTool` |
| `internal/tool/patch.go` | Implement `Parallel()` on `PatchTool`, `TodoWriteTool`, `TodoReadTool` |
| `internal/tool/misc.go` | Implement `Parallel()` on `SkillTool`, `QuestionTool` |
| `internal/tool/lsp.go` | Implement `Parallel()` on `LSPTool` |
| `internal/tool/formatter.go` | Implement `Parallel()` on `FormatTool` |
| `internal/tool/github.go` | Implement `Parallel()` on any tools there |
| `internal/tool/diff.go` | Implement `Parallel()` on any tools there |
| `internal/tool/custom.go` | Implement `Parallel()` on `CustomTool` (false — unknown) |
| `internal/agent/activity.go` | New file: `ActivityTracker`, `ActivitySnapshot` |
| `internal/agent/agent.go` | Wire `ActivityTracker`; parallel execution in `Step()` loop; instrument `AgentTool` |
| `internal/agent/subagent.go` | Instrument `TaskTool.Execute()` with activity tracker |
| `internal/tui/model.go` | Add `lastActivity`, `activityRowReserved`; `activityUpdateMsg`; `listenActivity` cmd; `renderActivityRow()`; update `layout()` and `renderContent()` |

---

## Task 1: Add `Parallel()` to Tool interface

**Files:**
- Modify: `internal/tool/tool.go`

- [ ] **Step 1: Add `Parallel() bool` to the `Tool` interface**

Open `internal/tool/tool.go`. The current interface is:
```go
type Tool interface {
    Name() string
    Description() string
    Definition() map[string]interface{}
    Execute(args json.RawMessage) (string, error)
}
```

Change it to:
```go
type Tool interface {
    Name() string
    Description() string
    Definition() map[string]interface{}
    Execute(args json.RawMessage) (string, error)
    Parallel() bool
}
```

- [ ] **Step 2: Verify the build breaks on missing implementations**

```bash
cd /Users/james/www/ocode && go build ./...
```
Expected: compilation errors listing every tool type that doesn't implement `Parallel()`. This confirms the interface change took effect.

---

## Task 2: Implement `Parallel()` on all built-in tools

**Files:**
- Modify: `internal/tool/file.go`, `internal/tool/search.go`, `internal/tool/exec.go`, `internal/tool/web.go`, `internal/tool/patch.go`, `internal/tool/misc.go`, `internal/tool/lsp.go`, `internal/tool/formatter.go`, `internal/tool/diff.go`, `internal/tool/github.go`, `internal/tool/custom.go`

**Parallel-safe = true:** read-only tools and sub-agent tools  
**Sequential = false:** anything that writes files, runs shell commands, or has unknown safety

- [ ] **Step 1: Add `Parallel()` to file tools (`internal/tool/file.go`)**

For each tool in the file, add below its `Description()` method:

```go
// ReadTool — read-only, safe to run concurrently
func (t ReadTool) Parallel() bool { return true }

// WriteTool — mutates files, must be sequential
func (t WriteTool) Parallel() bool { return false }

// EditTool — mutates files, must be sequential  
func (t EditTool) Parallel() bool { return false }
```

(Check actual struct names in the file with `grep "^type.*struct" internal/tool/file.go` and add `Parallel()` to each one found.)

- [ ] **Step 2: Add `Parallel()` to search tools (`internal/tool/search.go`)**

```go
func (t GlobTool) Parallel() bool  { return true }
func (t GrepTool) Parallel() bool  { return true }
func (t ListTool) Parallel() bool  { return true }
```

- [ ] **Step 3: Add `Parallel()` to exec tools (`internal/tool/exec.go`)**

```go
// BashTool can mutate state — conservative default
func (t BashTool) Parallel() bool { return false }
```

- [ ] **Step 4: Add `Parallel()` to web tools (`internal/tool/web.go`)**

```go
func (t WebFetchTool) Parallel() bool  { return true }
func (t WebSearchTool) Parallel() bool { return true }
```

- [ ] **Step 5: Add `Parallel()` to patch tools (`internal/tool/patch.go`)**

```go
func (t PatchTool) Parallel() bool     { return false }
func (t TodoWriteTool) Parallel() bool { return false }
func (t TodoReadTool) Parallel() bool  { return true }
```

- [ ] **Step 6: Add `Parallel()` to misc tools (`internal/tool/misc.go`)**

```go
func (t SkillTool) Parallel() bool    { return true }
func (t QuestionTool) Parallel() bool { return false }
```

- [ ] **Step 7: Add `Parallel()` to LSP tool (`internal/tool/lsp.go`)**

```go
func (t *LSPTool) Parallel() bool { return true }
```

- [ ] **Step 8: Add `Parallel()` to remaining tools**

For `formatter.go`, `diff.go`, `github.go`, `custom.go` — check struct names with:
```bash
grep "^type.*struct" internal/tool/formatter.go internal/tool/diff.go internal/tool/github.go internal/tool/custom.go
```
Then add `Parallel() bool` to each. Use `false` for anything that writes or has unknown behavior; `true` for read-only. `CustomTool` should be `false` (unknown profile).

- [ ] **Step 9: Verify build passes**

```bash
cd /Users/james/www/ocode && go build ./...
```
Expected: no errors.

- [ ] **Step 10: Run existing tests**

```bash
cd /Users/james/www/ocode && go test ./internal/tool/... -v 2>&1 | tail -20
```
Expected: all pass.

- [ ] **Step 11: Commit**

```bash
git add internal/tool/
git commit -m "feat: add Parallel() bool to Tool interface"
```

---

## Task 3: Create ActivityTracker

**Files:**
- Create: `internal/agent/activity.go`

- [ ] **Step 1: Write failing test**

Create `internal/agent/activity_test.go`:

```go
package agent

import (
    "testing"
    "time"
)

func TestActivityTracker_LLMRunning(t *testing.T) {
    tr := newActivityTracker()
    tr.setLLMRunning(true)
    select {
    case snap := <-tr.notify:
        if !snap.LLMRunning {
            t.Fatal("expected LLMRunning=true")
        }
    case <-time.After(100 * time.Millisecond):
        t.Fatal("no snapshot received")
    }
}

func TestActivityTracker_ToolTracking(t *testing.T) {
    tr := newActivityTracker()
    tr.toolStarted("bash")
    drain(tr)
    tr.toolDone("bash")
    snap := drain(tr)
    if len(snap.ActiveTools) != 0 {
        t.Fatalf("expected no active tools, got %v", snap.ActiveTools)
    }
}

func TestActivityTracker_AgentTracking(t *testing.T) {
    tr := newActivityTracker()
    tr.agentStarted("explore")
    snap := drain(tr)
    if len(snap.ActiveAgents) != 1 || snap.ActiveAgents[0] != "explore" {
        t.Fatalf("expected [explore], got %v", snap.ActiveAgents)
    }
    tr.agentDone("explore")
    snap = drain(tr)
    if len(snap.ActiveAgents) != 0 {
        t.Fatalf("expected no active agents, got %v", snap.ActiveAgents)
    }
}

// drain reads one snapshot from notify, or returns zero snapshot after timeout.
func drain(tr *ActivityTracker) ActivitySnapshot {
    select {
    case s := <-tr.notify:
        return s
    case <-time.After(100 * time.Millisecond):
        return ActivitySnapshot{}
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/... -run TestActivity -v
```
Expected: compile error — `ActivityTracker`, `newActivityTracker` not defined.

- [ ] **Step 3: Implement `internal/agent/activity.go`**

```go
package agent

import "sync"

// ActivitySnapshot is a point-in-time view of what the agent is doing.
type ActivitySnapshot struct {
    LLMRunning   bool
    ActiveTools  []string
    ActiveAgents []string
}

// ActivityTracker tracks running LLM calls, tools, and sub-agents.
// State changes are sent non-blocking to the Notify channel (buffered 1).
type ActivityTracker struct {
    mu           sync.Mutex
    llmRunning   bool
    activeTools  []string
    activeAgents []string
    notify       chan ActivitySnapshot
}

func newActivityTracker() *ActivityTracker {
    return &ActivityTracker{notify: make(chan ActivitySnapshot, 1)}
}

func (t *ActivityTracker) setLLMRunning(v bool) {
    t.mu.Lock()
    t.llmRunning = v
    snap := t.snapshot()
    t.mu.Unlock()
    t.send(snap)
}

func (t *ActivityTracker) toolStarted(name string) {
    t.mu.Lock()
    t.activeTools = append(t.activeTools, name)
    snap := t.snapshot()
    t.mu.Unlock()
    t.send(snap)
}

func (t *ActivityTracker) toolDone(name string) {
    t.mu.Lock()
    t.activeTools = remove(t.activeTools, name)
    snap := t.snapshot()
    t.mu.Unlock()
    t.send(snap)
}

func (t *ActivityTracker) agentStarted(name string) {
    t.mu.Lock()
    t.activeAgents = append(t.activeAgents, name)
    snap := t.snapshot()
    t.mu.Unlock()
    t.send(snap)
}

func (t *ActivityTracker) agentDone(name string) {
    t.mu.Lock()
    t.activeAgents = remove(t.activeAgents, name)
    snap := t.snapshot()
    t.mu.Unlock()
    t.send(snap)
}

// snapshot returns a copy of current state. Must be called with mu held.
func (t *ActivityTracker) snapshot() ActivitySnapshot {
    tools := make([]string, len(t.activeTools))
    copy(tools, t.activeTools)
    agents := make([]string, len(t.activeAgents))
    copy(agents, t.activeAgents)
    return ActivitySnapshot{
        LLMRunning:   t.llmRunning,
        ActiveTools:  tools,
        ActiveAgents: agents,
    }
}

// send delivers a snapshot non-blocking (drops if channel full).
func (t *ActivityTracker) send(snap ActivitySnapshot) {
    select {
    case t.notify <- snap:
    default:
        // drain stale snapshot and replace with latest
        select {
        case <-t.notify:
        default:
        }
        select {
        case t.notify <- snap:
        default:
        }
    }
}

// remove removes the first occurrence of name from slice.
func remove(s []string, name string) []string {
    for i, v := range s {
        if v == name {
            return append(s[:i], s[i+1:]...)
        }
    }
    return s
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/... -run TestActivity -v
```
Expected: all 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/activity.go internal/agent/activity_test.go
git commit -m "feat: add ActivityTracker for live agent/tool state"
```

---

## Task 4: Wire ActivityTracker into Agent and implement parallel execution

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add `activity` field to `Agent` struct and wire in `NewAgent`**

In `internal/agent/agent.go`, add `activity *ActivityTracker` to the `Agent` struct:

```go
type Agent struct {
    client      LLMClient
    tools       map[string]tool.Tool
    mcpTools    map[string]struct{}
    mcpErrors   []string
    config      *config.Config
    mode        Mode
    spec        *AgentSpec
    permissions *PermissionManager
    OnMessage   func(Message)
    activity    *ActivityTracker
}
```

In `NewAgent`, initialize it:
```go
a := &Agent{
    // ... existing fields ...
    activity: newActivityTracker(),
}
```

Add an `Activity()` accessor so the TUI can get the tracker:
```go
func (a *Agent) Activity() *ActivityTracker {
    return a.activity
}
```

- [ ] **Step 2: Instrument LLM call in `Step()`**

In `Step()`, wrap `a.client.Chat(...)` with tracker calls:

```go
a.activity.setLLMRunning(true)
resp, err := a.client.Chat(messages, toolDefs)
a.activity.setLLMRunning(false)
if err != nil {
    return nil, err
}
```

- [ ] **Step 3: Replace sequential tool execution with parallel+sequential partition**

Replace the existing `for _, tc := range resp.ToolCalls { ... }` block (lines ~147–165) with:

```go
if len(resp.ToolCalls) > 0 {
    type tcResult struct {
        idx int
        msg Message
    }

    // Partition into parallel-safe and sequential
    var parallelTCs, sequentialTCs []int
    for i, tc := range resp.ToolCalls {
        t, ok := a.tools[tc.Function.Name]
        if ok && t.Parallel() {
            parallelTCs = append(parallelTCs, i)
        } else {
            sequentialTCs = append(sequentialTCs, i)
        }
    }

    results := make([]Message, len(resp.ToolCalls))

    // Run parallel group concurrently
    if len(parallelTCs) > 0 {
        var wg sync.WaitGroup
        var mu sync.Mutex
        _ = mu // used via closure captures on results slice by index (no race — each writes unique index)
        for _, i := range parallelTCs {
            wg.Add(1)
            go func(idx int, tc ToolCall) {
                defer wg.Done()
                a.activity.toolStarted(tc.Function.Name)
                result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
                a.activity.toolDone(tc.Function.Name)
                if err != nil {
                    result = fmt.Sprintf("Error: %v", err)
                }
                results[idx] = Message{Role: "tool", ToolID: tc.ID, Content: result}
            }(i, resp.ToolCalls[i])
        }
        wg.Wait()
    }

    // Run sequential group one at a time
    for _, i := range sequentialTCs {
        tc := resp.ToolCalls[i]
        a.activity.toolStarted(tc.Function.Name)
        result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
        a.activity.toolDone(tc.Function.Name)
        if err != nil {
            result = fmt.Sprintf("Error: %v", err)
        }
        results[i] = Message{Role: "tool", ToolID: tc.ID, Content: result}
    }

    // Append in original order
    for _, toolMsg := range results {
        newMsgs = append(newMsgs, toolMsg)
        messages = append(messages, toolMsg)
        if a.OnMessage != nil {
            a.OnMessage(toolMsg)
        }
        if toolMsg.Content == "WAITING_FOR_USER_RESPONSE" {
            return newMsgs, nil
        }
    }
}
```

Add `"sync"` to the import block.

- [ ] **Step 4: Instrument `AgentTool.Execute()` with activity tracking**

`AgentTool` calls `t.mainAgent.Step()` directly. Wrap it:

```go
func (t AgentTool) Execute(args json.RawMessage) (string, error) {
    // ... existing param parsing ...
    t.mainAgent.activity.agentStarted("agent")
    resp, err := t.mainAgent.Step(subAgentMsgs)
    t.mainAgent.activity.agentDone("agent")
    // ... existing result building ...
}
```

- [ ] **Step 5: Build**

```bash
cd /Users/james/www/ocode && go build ./internal/agent/...
```
Expected: no errors.

- [ ] **Step 6: Run tests**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/... -v 2>&1 | tail -30
```
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat: parallel tool execution and activity tracking in agent Step loop"
```

---

## Task 5: Instrument TaskTool with activity tracking

**Files:**
- Modify: `internal/agent/subagent.go`

- [ ] **Step 1: Instrument `TaskTool.Execute()`**

In `TaskTool.Execute()`, after `spec` is resolved, add tracker calls:

```go
agentLabel := spec.Name
t.mainAgent.activity.agentStarted(agentLabel)
resp, err := subAgent.Step(subAgentMsgs)
t.mainAgent.activity.agentDone(agentLabel)
```

Full modified `Execute()`:
```go
func (t TaskTool) Execute(args json.RawMessage) (string, error) {
    var params struct {
        Prompt  string `json:"prompt"`
        Agent   string `json:"agent"`
        Context string `json:"context"`
    }
    if err := json.Unmarshal(args, &params); err != nil {
        return "", err
    }
    if params.Prompt == "" {
        return "", fmt.Errorf("prompt is required")
    }

    spec := FindSubAgentSpec(params.Agent)
    if spec == nil {
        spec = &DefaultSubAgents[0]
    }

    tools := t.getToolsForSubAgent(spec)
    subAgent := NewAgent(t.mainAgent.client, tools, t.mainAgent.config)
    subAgent.mode = t.mainAgent.mode

    systemPrompt := spec.SystemPrompt
    if params.Context != "" {
        systemPrompt += "\nBackground Context: " + params.Context
    }

    subAgentMsgs := []Message{
        {Role: "system", Content: systemPrompt},
        {Role: "user", Content: params.Prompt},
    }

    t.mainAgent.activity.agentStarted(spec.Name)
    resp, err := subAgent.Step(subAgentMsgs)
    t.mainAgent.activity.agentDone(spec.Name)
    if err != nil {
        return "", err
    }

    var b strings.Builder
    for _, m := range resp {
        if m.Role == "assistant" && m.Content != "" {
            b.WriteString(m.Content)
        }
    }
    return b.String(), nil
}
```

- [ ] **Step 2: Also make `TaskTool` and `AgentTool` return `Parallel() bool = true`**

Add to `agent.go`:
```go
func (t AgentTool) Parallel() bool { return true }
```

Add to `subagent.go`:
```go
func (t TaskTool) Parallel() bool { return true }
```

- [ ] **Step 3: Build and test**

```bash
cd /Users/james/www/ocode && go build ./... && go test ./internal/agent/... -v 2>&1 | tail -20
```
Expected: no errors, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/subagent.go
git commit -m "feat: instrument TaskTool with activity tracking, mark agent tools as parallel-safe"
```

---

## Task 6: TUI — subscribe to ActivityTracker and render indicator row

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add fields and message type to model**

In the `model` struct, add after `streaming bool`:
```go
lastActivity         agent.ActivitySnapshot
activityRowReserved  bool
```

Add a new message type near the other message types (around line 80):
```go
type activityUpdateMsg struct {
    snap agent.ActivitySnapshot
}
```

- [ ] **Step 2: Add `listenActivity` command**

Add this function near the other tea.Cmd helpers in `model.go`:

```go
func listenActivity(tracker *agent.ActivityTracker) tea.Cmd {
    return func() tea.Msg {
        snap := <-tracker.notify
        return activityUpdateMsg{snap: snap}
    }
}
```

- [ ] **Step 3: Wire `listenActivity` into `Update()`**

Find where `streamStartedMsg` is handled (around line 796). After starting the stream, also start listening:

```go
case streamStartedMsg:
    m.streaming = true
    m.cancelStream = msg.cancel
    if m.agent != nil {
        return m, tea.Batch(listenActivity(m.agent.Activity()))
    }
    return m, nil
```

Also handle `activityUpdateMsg` in the `Update()` switch:

```go
case activityUpdateMsg:
    m.lastActivity = msg.snap
    if !m.activityRowReserved {
        m.activityRowReserved = true
        m.layout() // re-layout to reserve the row height
    }
    // keep listening
    if m.agent != nil {
        return m, listenActivity(m.agent.Activity())
    }
    return m, nil
```

- [ ] **Step 4: Reset `lastActivity` on stream done and cancel**

In the `streamDoneMsg` case, add:
```go
case streamDoneMsg:
    m.streaming = false
    m.lastActivity = agent.ActivitySnapshot{}
    // ... existing error handling ...
```

- [ ] **Step 5: Update `layout()` to account for activity row**

In `layout()` (line 1664), change the height calculation:

```go
func (m *model) layout() {
    // ... existing width setup ...
    activityRowHeight := 0
    if m.activityRowReserved {
        activityRowHeight = 1
    }
    newHeight := m.height - inputHeight - headerHeight - 2 - activityRowHeight
    if newHeight < 1 {
        newHeight = 1
    }
    m.viewport.SetHeight(newHeight)
    m.renderTranscript()
}
```

- [ ] **Step 6: Implement `renderActivityRow()`**

Add this method to `model.go`:

```go
func (m model) renderActivityRow() string {
    if !m.activityRowReserved {
        return ""
    }
    snap := m.lastActivity
    if !snap.LLMRunning && len(snap.ActiveTools) == 0 && len(snap.ActiveAgents) == 0 {
        // reserved but idle — render blank line to hold height
        return m.styles.Status.Width(m.panelWidth()).Render("")
    }
    var parts []string
    if snap.LLMRunning {
        parts = append(parts, "⟳ LLM")
    }
    if len(snap.ActiveTools) > 0 {
        parts = append(parts, "⚙ "+strings.Join(snap.ActiveTools, ", "))
    }
    if len(snap.ActiveAgents) > 0 {
        parts = append(parts, "🤖 "+strings.Join(snap.ActiveAgents, ", "))
    }
    return m.styles.Status.Width(m.panelWidth()).Render(" " + strings.Join(parts, "  │  "))
}
```

Make sure `"strings"` is in the import block (it already is).

- [ ] **Step 7: Insert activity row into `renderContent()`**

In `renderContent()`, find where `leftParts` is assembled (around line 1808):

```go
leftParts := []string{header, transcript}
if m.showSlashPopup {
    leftParts = append(leftParts, m.renderSlashPopup())
}
leftParts = append(leftParts, input)
if row := m.renderActivityRow(); row != "" {
    leftParts = append(leftParts, row)
}
leftParts = append(leftParts, status)
```

- [ ] **Step 8: Build**

```bash
cd /Users/james/www/ocode && go build ./...
```
Expected: no errors.

- [ ] **Step 9: Run all tests**

```bash
cd /Users/james/www/ocode && go test ./... 2>&1 | tail -30
```
Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: activity indicator row showing LLM/tool/agent status in TUI"
```

---

## Task 7: Manual smoke test

- [ ] **Step 1: Run ocode**

```bash
cd /Users/james/www/ocode && go run . 
```

- [ ] **Step 2: Send a prompt that triggers tool use**

Type: `list all go files in internal/tool/`

Expected: while the agent runs, the activity row appears below the input showing `⟳ LLM` then `⚙ glob` or `⚙ list`, then disappears (row stays as blank line preserving height).

- [ ] **Step 3: Send a prompt that triggers a sub-agent**

Type: `use the explore agent to find all struct definitions in internal/agent/`

Expected: activity row shows `🤖 explore` while the sub-agent runs.

- [ ] **Step 4: Verify no viewport jump**

Watch the transcript panel while the activity row appears. It should not jump or resize — the height is pre-reserved.

- [ ] **Step 5: Final commit if any fixups were needed**

```bash
git add -p
git commit -m "fix: activity indicator smoke test fixups"
```
