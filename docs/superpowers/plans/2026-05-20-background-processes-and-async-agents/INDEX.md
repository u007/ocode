# Background Processes & Async Agents — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let ocode run shell commands and subagents as background jobs, surface
their status/logs in the TUI with recursive drill-in views, and have the main
LLM auto-continue when a job completes.

**Architecture:** Two registries — `ProcessRegistry` (in `internal/tool`) for
background shell processes and `AgentRunRegistry` (in `internal/agent`) for async
subagents. Each `Agent` owns one of each, so a subagent's processes are scoped to
it. `bash`/`task` gain a `run_in_background` flag; new poll tools (`bash_output`,
`kill_shell`, `agent_status`) and a `wait` tool are added. Job completion is
pushed to the TUI as a Bubble Tea message, which injects a synthetic message and
auto-resumes a `Step`. The TUI gains a recursive view-stack for drill-in.

**Tech Stack:** Go, Bubble Tea / lipgloss (charm.land v2), `os/exec` with process
groups, `encoding/json` tool args.

**Spec:** `docs/superpowers/specs/2026-05-20-background-processes-and-async-agents-design.md`

---

## Execution order

Parts are sequential — later parts depend on types defined earlier.

| Part | File | Produces |
|------|------|----------|
| 1 | `01-process-registry.md` | `ProcessRegistry`, `bash` background mode, `bash_output`, `kill_shell` |
| 2 | `02-agent-run-registry.md` | `AgentRunRegistry`, `task` background mode, `agent_status`, agent cancel |
| 3 | `03-wait-tool.md` | `wait` tool (duration + join-on-job) |
| 4 | `04-completion-and-lifecycle.md` | Completion push to TUI, synthetic-message inject, auto-resume, teardown |
| 5 | `05-tui-views.md` | Status-bar counts, agent strip, recursive drill-in views, mouse |

## Conventions

- Run `go build ./...` and `go test ./...` after each task.
- Each task ends with a `git commit`.
- Each part file is self-contained — no cross-references.

## Architecture decisions locked in

- `ProcessRegistry` lives in `internal/tool` (the `tool` package cannot import
  `agent`; `agent` imports `tool`). `AgentRunRegistry` and `WaitTool` live in
  `internal/agent` because they spawn subagents.
- `NewAgent` creates a fresh `*tool.ProcessRegistry` and **overwrites** the
  `bash` tool with a fresh `&BashTool{Procs: registry}` instance — this
  guarantees per-agent process isolation even though subagents are otherwise
  handed shared tool instances.
- Best-effort agent cancellation: `Agent` gets a `stopCh`; `Step` checks it at
  the top of each loop iteration. An in-flight LLM HTTP call is not interrupted,
  but the loop stops before the next call.
