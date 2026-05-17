# Parallel Tool/Agent Execution + Activity Indicators

**Date:** 2026-05-17  
**Status:** Approved

---

## Overview

Two coupled features:
1. **Parallel execution** — tool calls and sub-agent calls run concurrently when the LLM returns multiple tool calls in a single response.
2. **Activity indicator row** — a second status bar row in the TUI showing what is currently running (LLM, tools, agents), appearing only when active.

---

## Feature 1: Parallel Tool & Sub-Agent Execution

### Current Behavior

`agent.go` `Step()` loop (line 147) iterates `resp.ToolCalls` sequentially — each tool call blocks until complete before the next starts.

`TaskTool.Execute()` and `AgentTool.Execute()` run a full sub-agent `Step()` call synchronously and return a string result.

### Target Behavior

When the LLM returns multiple tool calls in one response:
- Partition them into **parallel-safe** and **sequential** buckets based on `Tool.Parallel() bool`
- Run all parallel-safe calls concurrently via goroutines + `sync.WaitGroup`
- Run sequential calls one at a time after the parallel group completes
- Collect all results and append as tool messages before the next LLM turn

Sub-agents (`agent`, `task` tools) are treated as parallel-safe since each spawns an independent agent with its own context.

### Tool Interface Change

Add `Parallel() bool` to the `tool.Tool` interface:

```
tool.Tool interface gains:
  Parallel() bool
```

**Default:** `false` (safe default — opt-in to parallelism)

**Parallel-safe tools (Parallel() = true):**
- `read`, `glob`, `grep`, `list`, `lsp`, `webfetch`, `websearch`
- `agent`, `task` (sub-agents — independent context, no shared state)
- `bash` — **false** by default (bash can mutate state; LLM must be trusted not to conflict)

**Sequential tools (Parallel() = false):**
- `write`, `edit`, `patch`, `diff`
- `bash` (conservative default)
- Any MCP tool (unknown safety profile — default false)

### Execution Algorithm (Step loop change)

```
for each LLM response with ToolCalls:
  partition ToolCalls into parallelGroup, sequentialGroup
  
  run parallelGroup concurrently:
    for each tc in parallelGroup: go func { result = Execute(tc) }
    WaitGroup.Wait()
  
  run sequentialGroup one at a time:
    for each tc in sequentialGroup: result = Execute(tc)
  
  collect all results (maintain original order for LLM context)
  append tool messages
  next LLM turn
```

Results must be appended in the **original tool call order** regardless of completion order, to maintain a consistent message history for the LLM.

### Activity Tracking

Add `ActivityTracker` to the `Agent` struct:

```
ActivityTracker:
  mu          sync.Mutex
  llmRunning  bool
  activeTools []string   // tool names currently executing
  activeAgents []string  // sub-agent names currently executing (agent/task calls)
  notify      chan ActivitySnapshot
```

```
ActivitySnapshot:
  LLMRunning    bool
  ActiveTools   []string
  ActiveAgents  []string
```

The tracker is updated:
- `llmRunning = true` before `client.Chat()`, `false` after
- Tool name appended to `activeTools` before `Execute()`, removed after
- Agent name appended to `activeAgents` before sub-agent `Step()`, removed after

Each state change sends a non-blocking snapshot to `notify` channel (buffered, size 1 — drop if full to avoid blocking hot path).

The TUI subscribes to this channel via a `tea.Cmd` listener.

---

## Feature 2: Activity Indicator Row (TUI)

### Model Changes

```go
// added to model struct
lastActivity agent.ActivitySnapshot
activityRowReserved bool  // true once any activity has been seen in session
```

### Channel Subscription

A `tea.Cmd` (`listenActivity`) reads from `agent.ActivityTracker.notify` and returns `activityUpdateMsg{snapshot}` to the update loop. It re-subscribes after each message (standard BubbleTea pattern).

### Rendering

New `renderActivityRow() string` method.

Returns empty string if `!m.activityRowReserved`. Otherwise renders a 1-line row:

```
⟳ LLM  │  ⚙ bash, read  │  🤖 explore, general
```

- `⟳ LLM` — shown when `lastActivity.LLMRunning`
- `⚙ <names>` — shown when `len(lastActivity.ActiveTools) > 0`
- `🤖 <names>` — shown when `len(lastActivity.ActiveAgents) > 0`
- Segments joined by `  │  `, idle segments omitted
- When all idle and `activityRowReserved`: renders blank line (preserves height)
- Styled same as status bar

Row is inserted between `input` and `status` in `renderContent()`.

### Viewport Height

Once `activityRowReserved` becomes true, subtract 1 from viewport height permanently for the session. This avoids mid-stream viewport jumps. `activityRowReserved` is set `true` on first `activityUpdateMsg` received and never reset.

### Reset Conditions

`lastActivity` is zeroed on:
- `streamDoneMsg` received
- User cancels via `esc` (`cancelStream`)
- Session load (`/session load`)

`activityRowReserved` is NOT reset on these — once reserved, always reserved for the session.

---

## Files to Change

### Agent layer
- `internal/tool/tool.go` — add `Parallel() bool` to `Tool` interface
- `internal/tool/file.go` (and all other tool files) — implement `Parallel()` on each tool
- `internal/agent/agent.go` — add `ActivityTracker`, parallel execution in `Step()` loop, LLM running tracking
- `internal/agent/subagent.go` — instrument `TaskTool.Execute()` with activity tracker calls
- `internal/agent/agent.go` AgentTool — instrument with activity tracker calls

### TUI layer
- `internal/tui/model.go` — add `lastActivity`, `activityRowReserved` fields; add `listenActivity` cmd; add `activityUpdateMsg` case in `Update()`; add `renderActivityRow()`; update `renderContent()` and viewport height calc

---

## Out of Scope

- Cancelling individual parallel tool calls mid-flight (all-or-nothing cancel only)
- Limiting parallelism (no concurrency cap — LLM controls how many tool calls it returns)
- Persisting activity history
