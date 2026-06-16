# Shared Agent Notes Bus — Implementation Plan (Index)

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Design:** `docs/superpowers/specs/2026-06-16-shared-agent-notes-bus-design.md`

**Goal:** Give a group of concurrently-spawned child agents a cheap, append-only,
cache-preserving shared-notes channel during a fan-out, plus a strong-model reconcile
step that dedups and resolves contradictions into one report.

**Architecture:** A per-group `notebus` goroutine owns an ordered append-only log
(notes + write-touches), assigns monotonic sequence numbers, and serves per-agent
deltas (`seq > lastSeen AND by != agent`). It is created when the agent `Step()`
parallel-tool block runs 2+ `task`/`agent` calls with notes enabled, and torn down
(flush + archive) when the group completes. Child contexts get only the new delta block
appended at the tail each loop, preserving the cached prefix. Persistence is an
append-only sidecar file. The orchestrator (main LLM) seeds the brief, stays passive
mid-flight, and runs reconcile.

**Tech Stack:** Go 1.23, existing `internal/agent` packages (`agent.go` Step loop,
`subagent.go` TaskTool, `context.go` context assembly, `redaction.go`), `internal/session`
persistence, `internal/tool` interfaces. No new external deps.

**Real integration points (verified):**
- `internal/agent/agent.go:538` `Step()`, parallel-tool block `:651`–`:690`
- `internal/agent/subagent.go` `TaskTool.Execute` `:175`, `executeSubAgentWithTranscript` `:373`, background `go func` `:318`
- `internal/agent/child_session.go` `childSessionID` / `childSessionMetadata`
- `internal/agent/context.go` context assembly (delta injection + append-stability)
- `internal/agent/redaction.go` secret redaction (must cover sidecar)
- `internal/session/session.go` session persistence (sidecar lives beside it)

---

## Execution Order

| Part | Title | Depends on |
|------|-------|-----------|
| [01](01-core-bus.md) | Core bus: log, seq, goroutine owner, watermarks, snapshots, persistence | — |
| [02](02-group-lifecycle.md) | Group detection, `shared_notes` toggle, lifecycle, touch hooks, failure/cancel/cap | 01 |
| [03](03-notes-emit-inject.md) | Note emit/parse/escape, resolve entries, delta injection, append-stability | 01, 02 |
| [04](04-prompts.md) | Child + orchestrator prompts (name, emit policy, leads-not-facts, seed, reconcile trigger) | 02, 03 |
| [05](05-reconcile.md) | Mechanical pre-pass, strong-model judgment, verify-agent escalation, coverage-gap report | 01–04 |

Each part file is self-contained: it restates the context it needs and lists its own
files, tests, and steps. Build in order — later parts assume the `notebus` package and
the lifecycle hook from earlier parts exist, but do not require re-reading them.

## Definition of Done (whole feature)

- A `shared_notes:true` group of concurrent child-runs sees peers' cross-agent notes as
  small per-loop deltas; nothing-new loops inject nothing (cached prefix unchanged).
- A peer `write`/`edit`/`apply_patch` surfaces as an `<oc-touch act="edit">` to others.
- The log persists to a sidecar and restores (with watermarks + recovered seq counter)
  on a mid-group reload.
- Reconcile (strong model) produces one deduped, contradiction-resolved report; an agent
  that died mid-group leaves its partition explicitly flagged as unreviewed.
- Note bodies from untrusted diffs cannot forge log entries (escaping verified by test).
- No raw stdout/stderr writes from any TUI-reachable path; secret redaction covers the
  sidecar.
