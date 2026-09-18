---
type: Concept
title: Discovery Web Surfaces
description: 'Discovery visibility in the web UI: live transient notices via SSE and runtime status endpoint, with map-race safety rule.'
tags:
  - discovery
  - web
  - sse
  - ui
  - architecture
timestamp: 2026-09-18T15:21:16Z
---
---
title: Discovery Web Surfaces
type: Concept
description: How discovery status and live notices are surfaced in the web/desktop UI, including the runtime endpoint contract and map-race safety.
tags:
  - discovery
  - web
  - sse
  - ui
  - architecture
---

# Discovery Web Surfaces

## Overview

Discovery was previously TUI-only — users had to run `/discovery status` in the terminal to see what was attached or judge-vetoed. This change makes two discovery surfaces available in the web/desktop UI:

1. **Live transient notices** — one-line informational banners that appear in real time as discovery events fire (new skills/MCP attached, project-doc summary generating).
2. **Runtime status endpoint** — a `GET /api/sessions/{id}/discovery` call that returns config + live agent state, consumed by the `/discover status` slash command.

## Live transient notices

### Agent callbacks

Two callback hooks on `Agent` fire during a turn:

- `OnDiscovery` (`internal/agent/discovery_glue.go`) — fires when turn ranking attaches new skills, MCP tools, or project docs.
- `OnMDIndexing` (`internal/agent/md_discovery.go`) — fires when a project-doc summary is being generated.

### Server → SSE

In headless/web mode, `wireHeadlessAgentCallbacks` (`internal/server/handler.go`) registers both callbacks and emits SSE events named `discovery` and `md_indexing` with a `TextDelta{delta}` payload (same wire shape as `text`/`thinking` deltas). Both events are registered in `sessionScopedEvents` (`internal/server/event_bus.go`), meaning they are routed only to the specific session that owns the agent — not broadcast globally.

### TUI mirror

In TUI mode, the `/rc` bridge mirrors these onto the unified bus. The deltaMsg branches at `internal/tui/model.go` call `m.broadcastRC("discovery"/"md_indexing", map[string]string{"delta": …})` alongside the existing transient transcript notice. RCBridge.Broadcast re-publishes onto the event bus, so web subscribers receive them too.

### Web rendering

- `sessionEvents.ts` routes both event names through `SESSION_SCOPED_EVENTS` and appends to an append-only `notice` LivePart (`{kind:"notice"; text}` in `web/src/api/types.ts`).
- `chatStore.tsx` handles the `LIVE_NOTICE` reducer action.
- `NoticeBlock` (`web/src/components/Chat/TurnParts.tsx`) renders the notice. It has **no spinner** (unlike `StatusBlock`) because it is informational, not work-in-flight.
- Text deltas are flushed first so a notice never splits a sentence mid-stream.

## Runtime status endpoint

### Route

`GET /api/sessions/{id}/discovery` (`internal/server/server.go` → `HandleSessionDiscovery` in `internal/server/handler.go`).

### Response shape

Returns a `discoveryStatusDTO` containing:

- **Config fields** — identical wire keys to `GET /api/config/ocode/discovery` (discovery on/off, type, index settings).
- **Agent fields** (only when a live agent is reachable) — `DiscoveryStatus()` output: `active`, `init_error`, `judge`, `judge_vetoed`, `mcp_total`, `skill_total`, `attached_skills`, `attached_mcp`, `attached_md`, `all_skills`, `all_mcp`, `all_md`, `md_pending`.
- `live: bool` — whether the agent was reachable at read time.

### Map-race safety rule

`DiscoveryStatus()` reads the agent's unsynchronised `mcpTools`/`tools` maps. Reading mid-turn can race with the agent writing to those maps (discovery attaches tools mid-turn with no lock). The endpoint therefore reads through `contextReportSource(id)` (`internal/server/handler.go`), which yields the agent **only when that read is safe** (between turns, no concurrent mutation). When no safe agent exists — idle session, evicted session, or a turn currently in flight — the response is config-only with `live: false`. Nil slices are normalised to `[]` by `nonNilStrings` so the web's `.length` never throws on `null`.

**Rule:** Always read `DiscoveryStatus` through `contextReportSource`. Never access the agent's tool maps directly from an HTTP handler.

### Web consumption

- `getDiscoveryStatus(id, host?)` (`web/src/api/client.ts`) returns a `DiscoveryStatus` interface.
- Wired through `App.tsx` CommandContext, consumed by `/discover status` (`web/src/components/Chat/commands.ts`).
- Renders a "Runtime status" section with the TUI's `●` (attached) / `○` (name-only) markers.
- Degrades gracefully to config-only when the endpoint returns an error or the agent is unreachable.

## Regression tests

| Area | Test file |
|---|---|
| Server endpoint | `internal/server/handler_discovery_test.go` |
| TUI notice delivery | `internal/tui/discovery_notice_test.go` |
| Web event routing | `web/src/lib/sessionEvents.test.ts` |
| Web slash command | `web/src/components/Chat/commands.discovery.test.tsx` |

## See also

- [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md) — the judge mechanism that determines which candidates get attached.
- [Web SSE Stream Silent Death Liveness](gotchas/web-sse-stream-silent-death-liveness.md) — liveness guarantees for the SSE transport that carries these events.