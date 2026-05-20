# Background Processes & Async Agents — Design

**Date:** 2026-05-20
**Status:** Approved (design)

## Goal

Let ocode run shell commands and subagents as background jobs, surface their
status and logs in the TUI, and let the user drill into any job to inspect it.
The main LLM is notified when a job completes and continues automatically.

Modelled on Claude Code: `Bash`/`Task` gain a `run_in_background` flag,
completion is **pushed** to the model, and **poll** tools (`bash_output`,
`agent_status`) plus a `wait` tool are also available.

## Scope

In scope:
- Background bash execution + process registry + poll/kill tools.
- Async subagent execution + agent-run registry + poll tool.
- A `wait` tool (plain duration or join-on-job).
- Push-on-completion notification into the main conversation.
- TUI: status-bar counts, agent strip above the prompt, recursive drill-in
  detail views for agents and processes.

Out of scope:
- Persisting background jobs across TUI restarts.
- Remote/distributed execution.
- A unified job abstraction (processes and agents stay separate).

## Part 1 — Backend

### 1.1 ProcessRegistry

A registry of background shell processes, owned by an `Agent`. Each `Agent`
(main and every subagent) has its own `ProcessRegistry` so a subagent's
processes are scoped to that subagent.

Per process record:
- `ID` — short stable id (e.g. `proc-1`, `proc-2`).
- `Command` — the command string.
- `Status` — `running` | `exited` | `killed`.
- `ExitCode` — set when `exited`.
- `StartedAt` / `EndedAt`.
- `Output` — a capped ring buffer of combined stdout+stderr (see 1.6).
- `readCursor` — byte offset of the last `bash_output` read, for incremental polling.

The `bash` tool gains a `run_in_background` boolean parameter:
- `false` / absent — current behaviour (synchronous `CombinedOutput`).
- `true` — start the process, register it, stream combined output into the
  ring buffer on a goroutine, and return immediately with a message containing
  the process `ID`.

When a backgrounded process exits, the registry marks status/exit code and
fires a completion event (see 1.5).

### 1.2 New process tools

- **`bash_output`** — params: `id`. Returns output appended since the last
  `bash_output` call for that id (advances `readCursor`), plus current status
  and exit code. If the process is unknown, returns an error.
- **`kill_shell`** — params: `id`. Sends SIGKILL to the process group, marks
  status `killed`. Idempotent: killing an already-finished process is a no-op
  that reports the final status.

### 1.3 AgentRunRegistry

A registry of async subagent runs, owned by the main `Agent`.

Per run record:
- `ID` — short stable id (e.g. `agent-1`).
- `Name` — subagent type (`explore`, `general`, …).
- `Status` — `running` | `done` | `failed`.
- `Result` — final assistant text, set on `done`.
- `Err` — error string, set on `failed`.
- `StartedAt` / `EndedAt`.
- `Transcript` — the run's live message list (assistant text + tool calls),
  capped per 1.6. Used for the agent strip last-2-lines and the drill-in view.
- `Processes` — the subagent's own `ProcessRegistry`.

The `task` tool gains a `run_in_background` boolean parameter:
- `false` / absent — current behaviour (synchronous `subAgent.Step`).
- `true` — run `subAgent.Step` on a goroutine, register the run, stream
  transcript updates into the record, and return immediately with the run `ID`.

On completion the run is marked `done`/`failed` and fires a completion event.

### 1.4 New agent tool + wait tool

- **`agent_status`** — params: `id`. Returns the run's status, and on
  completion the result text (or error). While running, returns status plus
  the last few transcript lines.
- **`wait`** — params: `seconds` (integer) **or** `minutes` (integer), and an
  optional `for` (a process or agent id).
  - Plain duration: sleeps the requested time, then returns.
  - Targeted (`for` set): blocks until that job completes, then returns its
    result/exit info directly. Short-circuits immediately if the job has
    already completed (no sleep).
  - A hard ceiling of **10 minutes** caps any wait so the session cannot hang.
    Requests above the ceiling are clamped and the response notes the clamp.

### 1.5 Completion notification (push)

Background jobs (process or agent) signal completion through a channel the TUI
listens on, surfaced as a Bubble Tea message (`jobCompletedMsg`).

The TUI owns turn orchestration, so injection happens there, not by reusing the
existing `queuedInputs` slice (which holds raw user text):
- On `jobCompletedMsg`, the TUI appends a **synthetic user-role message** to the
  conversation, prefixed e.g. `[Background agent agent-1 completed]` /
  `[Background process proc-2 exited, code 0]`, followed by the result/output.
- **Idle** = the current turn has finished, the agent is not mid-`Step`, and no
  user input is being composed-and-sent this tick. If idle, the TUI starts a
  new `Step` to consume the synthetic message — this renders as a **new turn**
  in the transcript with a visible marker so the user can see the LLM "woke up"
  on its own.
- If not idle (agent mid-turn), the synthetic message is buffered and consumed
  at the start of the next turn.
- If multiple jobs complete close together, each appends its own synthetic
  message; a single resumed `Step` consumes all buffered messages.

The agent's system prompt gains one line: trust the completion push; use
`agent_status` / `bash_output` only to check progress *before* completion (e.g.
after a `wait`).

### 1.6 Memory bounds

- Process `Output` ring buffer: capped at **256 KB** per process. On overflow,
  oldest bytes are dropped and a leading `[…truncated N bytes]` marker is kept
  current.
- Agent `Transcript`: capped at the **last 200 tool-call/message entries** per
  run. On overflow the oldest entries drop with a `[…truncated]` marker entry.
- These caps bound a noisy job's memory; the drill-in view shows whatever the
  buffer currently holds.

### 1.7 Permissions

A `bash` call with `run_in_background: true` goes through the **same permission
check** as a synchronous `bash` call, evaluated **before** the process is
started/backgrounded. Backgrounding never bypasses a permission gate. Under
YOLO / per-mode allow rules it inherits those rules exactly as synchronous
`bash` does.

### 1.8 Lifecycle / cleanup

- **Ctrl-C on a turn:** running background jobs keep running (they are detached
  by design); only the foreground turn is interrupted.
- **`/clear`:** all background processes are killed (SIGKILL to process groups)
  and agent-run goroutines are signalled to stop via a context cancel; both
  registries are emptied.
- **TUI exit:** same teardown as `/clear` — `ProcessRegistry` teardown kills
  every live process; agent contexts are cancelled. No zombie children, no
  orphan goroutines.
- Compaction does not touch background jobs; registries are independent of the
  message history.

## Part 2 — TUI

### 2.1 View stack

A new navigation stack (`[]detailView`) layered on top of the existing
chat/files/git/log tabs:
- Empty stack — normal tabbed UI.
- A pushed view renders full-screen. `Esc` pops one level.
- The stack is recursive: main → agent detail → that agent's process log.

Detail view kinds:
- `processListView` — list of processes in a given registry.
- `processLogView` — scrollable streamed output of one process + status line.
- `agentDetailView` — one agent run's full transcript.

### 2.2 Modal precedence

Existing modal overlays (`handleModalKeys`: picker, connect, palette, leader)
sit **on top of** the drill-in view stack. While a modal is open, keys route to
the modal and `Esc` closes the modal first; only once no modal is open does
`Esc` pop the view stack. Pushing a detail view is not allowed while a modal is
open.

### 2.3 Chat tab additions

- **Status-bar counts** — e.g. `▣ 2 bg · 3 agents`. Hidden when both counts are
  zero. Clickable and shortcut-accessible (opens the process list / agent list).
- **Agent strip** above the input prompt — one block per running agent:
  ```
  ▸ explore   ⠋ running   1m04s
    │ scanning internal/tui/model.go
    │ found 3 matches for "viewport"
  ```
  The two `│` lines are the last 2 transcript lines, updated live from the run
  record. A finished run shows `✓ done` / `✗ failed` briefly, then drops off
  the strip on the next render tick.

### 2.4 Drill-in

- Clicking an agent strip block (or a keyboard shortcut / picker) pushes an
  `agentDetailView`: the run's transcript rendered with the **same renderer as
  the main chat** — expandable tool calls, scrollable viewport, scrollbar — plus
  that agent's own bg-process count, itself drillable into a `processLogView`.
- Clicking the bg count (or shortcut) pushes a `processListView`; selecting a
  process pushes a `processLogView` with scrollable streamed output.
- `Esc` from any detail view pops one level back toward the main screen.

### 2.5 Mouse

The agent strip blocks and the status-bar counts are hit-tested in the existing
mouse handler. A click on a strip block pushes the matching `agentDetailView`;
a click on a count pushes the matching list view. Scroll events inside a detail
view scroll that view's viewport.

## Data flow

```
bash(run_in_background) ──► ProcessRegistry ──► goroutine streams ring buffer
                                   │                     │
task(run_in_background) ──► AgentRunRegistry ──► goroutine runs Step
                                   │                     │
                                   ▼                     ▼
                          ActivitySnapshot         jobCompletedMsg (tea.Msg)
                          (counts + strip data)          │
                                   │                     ▼
                                   ▼            TUI injects synthetic message,
                          TUI renders strip /   auto-resumes Step if idle
                          status bar / views
```

`ActivitySnapshot` is extended with background-process entries and richer
agent-run entries (id, name, status, last-2-lines) so the TUI can render the
strip and counts from the existing notify channel.

## Error handling

- Unknown id passed to `bash_output` / `kill_shell` / `agent_status` / `wait`
  (`for`) — the tool returns an error string naming the bad id; no panic.
- A backgrounded process that fails to start — registered immediately with
  status `exited`, exit code non-zero, and the start error captured in the
  output buffer.
- A subagent run that errors — status `failed`, `Err` populated, completion
  event still fired so the main LLM is notified of the failure.
- `wait` clamps over-ceiling requests rather than erroring.

## Testing

- `ProcessRegistry`: background start, incremental `bash_output` cursor, ring
  buffer truncation, `kill_shell` on running and finished processes.
- `AgentRunRegistry`: async run lifecycle, transcript cap, completion event on
  success and failure.
- `wait`: plain duration, targeted join, already-completed short-circuit,
  ceiling clamp.
- Completion injection: synthetic message appended; auto-resume only when idle;
  multiple completions consumed by one resumed Step.
- Permissions: backgrounded `bash` is gated identically to synchronous `bash`.
- Lifecycle: `/clear` and TUI exit kill processes and cancel agent contexts.
- TUI: view-stack push/pop, recursive drill-in, modal-over-stack precedence,
  mouse hit-testing of strip blocks and counts.
