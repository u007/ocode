---
type: Concept
title: Per-Chat MCP Toggle
description: 'Per-chat MCP server on/off toggle in the web/desktop chat sidebar: session-scoped list/toggle, process-wide persist + per-session override, and the mcpCache rebuild gotcha.'
tags:
  - mcp
  - sidebar
  - web
  - desktop
  - session
  - agent-rebuild
  - caching
  - gotcha
timestamp: 2026-09-25T06:23:58Z
---
# Per-Chat MCP Toggle

**Type:** Concept
**Status:** Current as of 2026-09-24
**Files:** `internal/server/handler_mcp.go`, `internal/server/mcp_cache.go`, `internal/server/agent_session.go`, `web/src/components/Layout/CoworkSidebar.tsx`, `web/src/api/client.ts`

## Overview

The web/desktop chat sidebar (`CoworkSidebar.tsx`) has a collapsible **MCP** section that lists every configured MCP server with an on/off switch and an `enabled/total` count. The toggle is **per chat session**: it flips one server for the chat you toggled it in, immediately rebuilds that chat's agent so the new tool set takes effect, and leaves every other open chat — and the process-wide default — coherent. This page records how it works and the architectural gotcha that made the feature non-trivial.

## Surface

- **Section:** collapsible, default collapsed; persistence uses the same localStorage key as the other sidebar section states, `ocode.ui.sidebar.v2` (`SIDEBAR_SECTIONS_KEY`, `web/src/components/Layout/CoworkSidebar.tsx:66`).
- **Data:** the server list is fetched session-scoped — `GET /api/mcp?session_id=` — so the switch shows *this* chat's effective state, not the process-wide one.
- **API client** (`web/src/api/client.ts`):
  - `api.getMCP(host?, sessionId?)`
  - `api.setMCPEnabled(name, enabled, host?, sessionId?)`
- The active `sessionId` must always be threaded (undefined only for a `new-*` draft tab), and both the list and the toggle use the same scope. Remote projects thread `host` as usual (`docs/concepts/web-session-host-scoping.md` — its `/mcp` row predates this change and still shows only `getMCP(host)`).

## Server semantics (`internal/server/handler_mcp.go`)

`HandleListMCP` and `HandleSetMCPEnabled` accept an optional `?session_id=`.

A toggle with a session id does **both** of the following:

1. **Persists process-wide** to `opencode.json` via `config.SaveMCPEnabled` and updates the in-memory `h.cfg.MCP` — deliberately matching the TUI `/mcp` semantics, so the choice is durable and *new* sessions inherit it.
2. **Records a per-session override** in `h.mcpSessionOverrides` (an `RWMutex`-guarded `map[sessionID]map[serverName]bool`, `mcpSessionOverridesMu`) and calls `rebuildAgentForMCP(sessionID)` so only that chat's agent is rebuilt.

Without a `session_id` the call keeps the legacy process-global behavior (the Settings form relies on this).

## The key gotcha: why a rebuild is mandatory

MCP tools are enumerated **once per process** into a process-wide `mcpCache` warmed at Handler boot (`h.mcpCache.warm(cfg)` in `internal/server/handler.go`, type in `internal/server/mcp_cache.go`). `buildAgentSession` (`internal/server/agent_session.go`) reads that cache once at agent construction.

**Before this feature, a toggle updated config but no live session ever picked it up — it was dead until app restart.** Any future config toggle whose effect flows through a boot-time cache has the same failure mode: updating the config is not enough, you must rebuild the consumer.

`rebuildAgentForMCP` (`handler_mcp.go`) forces a rebuild regardless of profile/model state (unlike `reconcileProfileAgent`), because the *tool set* changed, not the client. It:

- no-ops when the session has no live agent;
- **defers when a turn is active** (`h.sessions.IsTurnActive`) — tearing the agent down mid-turn would disturb in-flight work, so the running turn keeps its tool set and the *next* turn rebuilds. The deferral is logged as a `debuglog` entry with the new kind `debuglog.KindMCP` (`internal/debuglog/debuglog.go`) so it is visible in the Logs tab;
- otherwise builds a replacement agent from the same model/messages (carrying over the advisor guard) via `replaceAgentSession`.

## Fast path vs. re-enumeration (`mcpToolsForSession`)

`mcpToolsForSession(cfg, sessionID)` (called from `buildAgentSession`, `internal/server/agent_session.go:558`):

- **No overrides** (the overwhelmingly common case): returns nil so the builder keeps the process-wide `mcpCache` fast path — sessions that never toggled anything pay zero extra MCP enumeration cost.
- **Has overrides**: re-enumerates MCP tools from a **cloned** copy of the session's effective config produced by `applyMCPSessionOverrides`, which honors the per-window profile's `delta.MCP`.

**Invariant:** `applyMCPSessionOverrides` must never mutate the shared `h.cfg.MCP` — it shallow-copies the config and clones the MCP map. Mutating the shared map would leak one chat's toggle into every other chat and into the persisted process default (pinned by a test that fails if the shared config is mutated).

## Lifecycle

Overrides are **in-memory only** (the durable record is the process-wide `opencode.json` write) and are cleared on session release: the registry `onEvict` hook calls `clearMCPSessionOverrides(sessionID)` (`internal/server/handler.go:502`) so the map does not grow one entry per session id the process has ever served.

## `/mcp` web command

The web chat `/mcp` command now passes the active session id, and its message points at the sidebar MCP toggle. It previously advertised `enable`/`disable` subcommands that did not exist.

## Unaffected by the remote MCP OAuth compatibility fix (2026-09-25)

The remote MCP OAuth compatibility work — dual-schema `mcp-auth.json` reads, URL-bound token attachment, RFC 9728/8414 discovery refresh, status-before-decode HTTP errors — lives entirely below this feature: it changes *how a remote request is authenticated*, not which servers are enabled or how tools are cached. `handler_mcp.go` and `mcp_cache.go` were not touched; the design's non-goals explicitly preserve "enabled" semantics and this toggle/cache behavior. The only observable interaction is benign: an upstream-authorized remote server now enumerates successfully during `mcpCache` warm where it previously failed with 401. See `docs/concepts/remote-mcp-oauth-compat.md`.

## Tests

All mutation-verified (each fix reverted → test fails):

- `internal/server/handler_mcp_session_test.go` — 5 tests: per-session override vs process-wide fallback, shared-config non-mutation, fast path for override-less sessions, clear-on-release, and the rebuild.
- `web/src/components/Layout/CoworkSidebar.mcp.test.tsx` — 4 tests: section rendering, session-scoped fetch, toggle write, enabled/total count.
- `web/src/components/Chat/commands.hostScope.test.tsx` — `/mcp` threads the session id.

## Live-verified behavior

With process-wide `alpha` enabled, a session that toggles it off reports `disabled` via `GET /api/mcp?session_id=`, while the unscoped `GET /api/mcp` and other sessions keep the process-wide value (`enabled`).

## Related

- `docs/architecture/sidebar-tui-parity-gaps.md` — sidebar section inventory (its "Tools/MCP" row predates per-chat scoping).
- `docs/superpowers/specs/2026-09-18-per-session-sidebar-settings-design.md` — the broader design spec for per-session sidebar settings (MCP is one instance of the pattern: session-scoped copy of config + next-turn rebuild, not runtime atomics).
- `skills/ocode-web/SKILL.md` item 32 — web-side rules for this toggle.
- `docs/concepts/remote-mcp-oauth-compat.md` — remote MCP OAuth compatibility (dual-schema auth, URL binding, discovery refresh); confirms this page's toggle/cache semantics are unchanged.