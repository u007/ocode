---
type: Gotcha
title: Subagent Feedback-Loop Guard (task tool)
description: The task/subagent dispatch refuses consecutive same-type launches without new user input to break runaway feedback loops; vary the agent type or wait for user input.
tags:
  - subagent
  - task
  - feedback-loop
  - guard
  - dispatch
timestamp: 2026-07-11T03:55:35Z
---
# Subagent Feedback-Loop Guard

## What happens

When a subagent (e.g. via the `task` tool) is dispatched repeatedly with the
**same agent name** and **no new user input** in between, ocode's dispatch logic
refuses the launch with an error like:

> Error: refusing to dispatch subagent "X" — it has been launched N times in a
> row without any new user input. This usually means the conversation is in a
> feedback loop. Wait for the user to provide new direction before retrying.

## Why

A small model can interpret every job-completion notification as a fresh
request and loop forever, re-launching the same subagent in response to its own
completion messages. This guard breaks that runaway loop.

## How it works (verified in source)

- `internal/agent/agent.go:388` defines `const subagentDispatchLimit = 3`.
- `Agent.NoteSubagentDispatch(name)` (`internal/agent/agent.go:393`) increments
  a counter keyed by the **agent name** for consecutive identical dispatches.
  A different name resets the counter to 1 for that name.
- `TaskTool.Execute` (`internal/agent/subagent.go:257-265`) refuses dispatch
  when `count > subagentDispatchLimit` — i.e. the **4th** consecutive identical
  launch with no intervening user input.
- `Agent.ResetSubagentDispatch` (`internal/agent/agent.go:408`) clears the
  counter. It is called from the TUI on every new user message
  (`internal/tui/model.go`, `commands.go`, `doc_sync.go`, `learn.go`), so
  legitimate repeated dispatches **across turns** are always allowed.

## Workarounds

1. **Wait for / send new user input.** The intended resolution — the guard is
   keyed off "since the last user input", so a fresh user message resets the
   counter and re-enables dispatch.
2. **Vary the agent type.** Because the counter is keyed by `spec.Name`,
   dispatching a *different* agent name instead of the same one resets the
   per-name counter and escapes the guard. Use this when you genuinely need to
   chain different specialized agents back-to-back without user input.

## Caveats

- The counter is per-main-agent and per-name; mixing agent types in a loop
  avoids the guard but does not stop an actual logic loop — prefer fixing the
  root cause (the model's completion interpretation) when possible.
- The limit is the number of *consecutive identical* dispatches; the refusal
  fires only after the 3rd (at count `> 3`).
