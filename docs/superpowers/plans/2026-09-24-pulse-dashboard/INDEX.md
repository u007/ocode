# Pulse Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan part-by-part. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A full-page "Pulse" view listing every live chat session across all
local projects as an urgency-sorted card grid, with hover-to-expand detail and
one-click jump to the session (project terminals, browser tabs, side pane
restored).

**Architecture:** A new `GET /api/pulse` endpoint derives one row per session
from the live `SessionManager` registry, running agents, pending asks and the
per-session todo file. A new `todo_updated` SSE event plus existing
turn/activity/ask events keep an app-level `pulseStore` current for **all**
sessions (not just open tabs). A new `"pulse"` value of App's `activeView`
renders the grid; a jump helper chains `selectProject` → `openSessionTab`.

**Tech Stack:** Go (net/http mux, EventBus), React 19 + TypeScript, Tailwind,
shadcn/Radix `components/ui`, Vitest, Wails v3 (desktop).

**Spec:** `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`

Plan style follows the user's global rule: high-level, names files and
functions, no code snippets. Parts are self-contained; each restates the
interfaces it consumes.

## Spec deltas discovered while planning

- **No `/pulse` URL route.** The web app has no client router; views are the
  `activeView` state in `web/src/App.tsx:248`. Pulse is a new `activeView`
  value `"pulse"`. Spec updated in Part 10.
- **Live tail for idle sessions.** `SessionManager.setTurnActive` clears
  buffered live frames when a turn ends, so an idle card's tail comes from
  the last assistant message, not buffered frames.
- **`todo_updated` is published from the server's tool-result broadcast**,
  not from `internal/tool` (the tool package has no EventBus access). Tool
  messages carry only `ToolID`, so the name is resolved from the preceding
  assistant `ToolCalls`.

## Global Constraints

- Status enum, exact strings: `needs_permission`, `needs_question`,
  `running`, `error`, `idle`.
- Sort: rank (`needs_permission`/`needs_question`=0, `running`=1,
  `error`/`idle`=2), then `updated_at` desc, then `session_id` asc.
- `scope`: `live` (default) | `all`; anything else → HTTP 400.
- `limit`: 1–100, default 50; out of range → HTTP 400.
- Live window 24h; All window 7 days.
- Hover expand delay 150ms; stream tail ~6 lines.
- Child sessions are never rows; counted as `child_count` on the parent.
- Endpoint failure shows a visible error banner with Retry; no stale fallback.
- Status never conveyed by color alone; pulsing respects
  `prefers-reduced-motion`.
- Every host/project-scoped web call threads host + project path per
  `docs/concepts/web-session-host-scoping.md`.
- No `??`/`||` defaults, no empty catch, caught errors logged with context
  (user global rules).
- `scope=live` p50 < 200ms.

## Review Focus

1. **Session with a pending ask *and* a running turn** → must show
   `needs_permission`/`needs_question`, never `running` (precedence test in
   Part 03).
2. **Event for a session with no open tab and not yet in the store** → store
   refetches (debounced), never fabricates a row (Part 05).
3. **SSE reconnect after missed `turn_done`** → store reseeds, card leaves
   Running (Part 05).
4. **Jump to a session in a different project on a remote-or-local host** →
   `selectProject` strictly before `openSessionTab`, host passed through
   (Part 06).
5. **Malformed/missing todo file** → row has `todo: null`, request still 200,
   warn logged with path (Part 01, Part 04).

## Parts (execute in order)

| # | File | Deliverable |
|---|------|-------------|
| 01 | `01-todo-reader-and-event.md` | Per-session todo reader + `todo_updated` SSE event |
| 02 | `02-last-turn-error.md` | `sessionEntry` records last turn error |
| 03 | `03-pulse-rows.md` | Pure row builder: status, current task, sort, cursor |
| 04 | `04-pulse-endpoint.md` | `GET /api/pulse` handler, live + all scope |
| 05 | `05-pulse-store.md` | Web API client + app-level `pulseStore` |
| 06 | `06-jump-helper.md` | `jumpToSession` helper |
| 07 | `07-pulse-card.md` | `PulseCard` collapsed/expanded + live tail |
| 08 | `08-pulse-view-entry.md` | `PulseView`, tab-bar entry, header badge, ⌘J |
| 09 | `09-desktop-open-pulse.md` | Desktop opens Pulse from dock/tray |
| 10 | `10-docs.md` | Concept doc, index, CHANGES, TODO, spec delta |

## Verification (whole branch)

- `go test ./internal/server/... ./internal/tool/...`
- `cd web && pnpm test && pnpm typecheck`
- Manual: run web, start turns in two projects, confirm Needs-you/Running
  sections, hover expand, click jump restores terminals + browser tabs.
