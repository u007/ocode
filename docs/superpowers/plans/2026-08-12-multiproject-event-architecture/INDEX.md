# Multi-Project Session & Event Architecture — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-08-12-multiproject-event-architecture-design.md`

**Goal:** One server process serves all registered projects with a session
registry, a single multiplexed event stream, async session bootstrap, a turn
state machine, and a frontend that derives status and streaming state from
server truth.

**Architecture:** A backend `SessionManager` becomes the single authority for
session → project/agent/turn state. A unified event bus tags every event with
`{project, session_id, seq}` and streams over one SSE endpoint. The frontend
replaces N EventSources + polling with one event-bus module, fetches status
per-session on tab activation, and clears streaming state via
heartbeat/reconcile. RC-bridge (TUI) sessions register into the same registry.

**Tech stack:** Go backend (`internal/server`, `internal/session`,
`internal/projects`), React/TypeScript frontend (`web/src`), SSE transport.

## Global Constraints

- Web + server ship together: breaking the web↔server HTTP API is allowed.
- On-disk session storage format is unchanged; existing sessions must load.
- Plans are executed with TDD: failing test → minimal implementation → pass →
  commit. Frontend type checking uses `bun run typecheck` (tsgo), never tsc.
- Fail loudly: every dropped/unroutable event logged (server structured log,
  client `console.warn`). No silent catch blocks.
- Fixed timeouts from the spec: MCP bootstrap wait 30s; turn heartbeat every
  ~10s; frontend stall watchdog 30s; idle agent eviction 30 min.
- Old endpoints/paths are deleted only in Part 06, after the frontend swap and
  RC unification both land.

## Execution Order

Parts are sequential; each is independently shippable and leaves the app
working.

| Part | File | Delivers |
|------|------|----------|
| 01 | `01-session-manager.md` | Backend SessionManager; cross-project session resolution; idle eviction. Fixes cross-project 404s. |
| 02 | `02-event-bus.md` | Unified tagged event bus + `/api/events` SSE endpoint (old endpoints intact). |
| 03 | `03-async-bootstrap-turn-state.md` | 202-immediately chat, bootstrap stage events, MCP timeout, turn heartbeat, reconcile + status endpoints. |
| 04 | `04-frontend-transport.md` | Single frontend eventBus; delete per-session EventSources and polling; reconcile on reconnect. |
| 05 | `05-frontend-status-streaming.md` | Status fetch on tab activation; single status source; streaming watchdog. |
| 06 | `06-rc-first-class-cleanup.md` | RC-bridge sessions registered first-class; delete forwarding/re-stamping and all legacy endpoints. |

## Verification (whole feature)

- `go test ./internal/...` green after every part.
- `cd web && bun run typecheck` green after parts 04–05.
- Manual QA (after 05): switch project → sidebar stays populated; new session
  in a second project → bootstrap stages visible, message round-trips; kill
  server mid-turn → spinner clears via reconcile.
- Manual QA (after 06): TUI `/rc` session drivable from web like any session.
