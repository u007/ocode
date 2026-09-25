---
type: Concept
title: Host and project scoping for web session/project reads
description: Every session/project-scoped web read must carry host and project path; command context, resolved surfaces, and the threading rule
tags:
  - web
  - host
  - session
  - remote
  - project
timestamp: 2026-09-25T16:35:44Z
---
# Host and project scoping for web session/project reads

## Summary

Every session- or project-scoped read in the web/desktop UI **must**
carry the tab's `host` (and `project` when the endpoint selects a
repository). Without these, the local server answers the request instead
of forwarding to the correct remote host, or the request 404s on a
remote session id.

## Source of truth

- `resolveSessionHost(projectState, sessionId, {fallbackToActive:true})`
  (`web/src/hooks/useSessionHost.ts`) — resolves the host for a tab.
- `findProjectPathForTab(projectState, sessionId)` — resolves the project
  path.

## Command context

`CommandContext` (`web/src/components/Chat/commands.ts`) carries `host`
and `projectPath`. Commands that were previously host-less and are now
scoped:

| Command | Scoped call |
|---------|------------|
| `/lsp` | `api.getLSPStatuses(host)` |
| `/mcp` | `getMCP(host, sessionId)` (session-scoped so per-chat MCP overrides apply — see `per-chat-mcp-toggle.md`) |
| `/session list` | `listSessions(undefined, host)` |
| `/standup` · `/changes` · `/review` | `getCommandContext(name, args, project, host)` → `GET /api/command-context/{name}?args=&project=` |
| `/docs status\|init\|update\|cleanup` | project + host |

`App.tsx` builds the API closures with `targetHost` / `targetProjectPath`
and sets `projectPath` on the context.

## Surfaces already scoped

- Status panel: `getModifiedFiles`, `getLSPStatuses`, `getSpending`,
  `getMCP` (all take host).
- Transcript load: `getSession`.
- Turn reconcile/watchdog: `SessionEventRouter.hostFor`.
- Session prefetch: keyed `host\0id`.
- `getChangeDiff`, `listAgentRuns`, `getDiscoveryStatus`.
- `/api/uploads` (web Assets tab: list, upload, delete, file
  fetch/download) and its sibling callers — chat attachment upload
  (`ChatInput.tsx`) and drag-and-drop upload into a terminal
  (`TerminalPanel.tsx`) — prefix the path with `remoteApiBase(host)`
  (fixed; see Lesson).

## Known residual

`/api/files/modified` is TUI-bridge-only and returns empty for headless
sessions.

## Rule

Any **new** session/project-scoped web call must thread the tab's host
(and project path). A background refresh must not be the sole caller, and
prefetch caches must be host-keyed to avoid cross-host collisions.

### Lesson: a raw `fetch` dropped the host (fixed)

An endpoint which bypasses `apiPath`/`fetchJSON` in favour of a raw
`fetch` can silently lose host threading **even when its loading key is
already host-scoped** — `assetsLoadingKey` (`App.tsx`) was host-aware
while `AssetsPanel`'s fetch was not (`apiPath` only rewrites the origin;
the host prefix comes from `remoteApiBase(host)` or `fetchJSON`'s `host`
argument).

The web UI therefore sent a remote project's uploads request to the
LOCAL server with the remote project path, instead of through
`/api/remote/{host}/api/uploads`. The local symptom was an opaque HTTP
500 (`list failed: 500 Internal Server Error`), because the local server
tried to create the upload directory at the verbatim `~/www/aimsai2`
path — a leading `~` is not absolute, so it resolved relative to the
server process cwd.

Three call sites shared the defect and were all fixed:
`web/src/components/Assets/AssetsPanel.tsx` (list, upload, delete, file
fetch/download), `web/src/components/Chat/ChatInput.tsx` (chat
attachment upload), and `web/src/components/Terminal/TerminalPanel.tsx`
(drag-and-drop upload into a terminal).

There is a second, backend half. A remote project is registered verbatim
with a tilde (`projects.AddRemote` keeps `~/www/aimsai2` because the
separator and `~` belong to the remote shell), but the host-side
`ocode serve --remote` expands `~` when it saves that project
(`projects.Add`). So a request proxied through `/api/remote/{host}/`
arrives carrying the tilde form while the host's registry holds the
expanded path, and the exact-match trust gate `isRegisteredProjectRoot`
rejected it with 400 "unknown project". Fixed in
`internal/server/handler_git.go` by `resolveRegisteredProjectRoot`,
which falls back to the `projects.ExpandHome` form before rejecting. The
trust boundary is NOT widened: only a saved project root is ever
returned, and the expansion narrows rather than widens the accepted set
(`~user` and non-tilde paths pass through unchanged).

## Tests

- `web/src/components/Chat/commands.hostScope.test.tsx`
- `web/src/lib/sessionPrefetch.test.ts`
- `web/src/hooks/useTurnWatchdog.test.tsx`
- `web/src/lib/sessionEvents.test.ts`
- `internal/server/uploads_test.go` — tilde-form registered project lists
  200; unregistered tilde path still 400.
- `web/src/components/Assets/AssetsPanel.remoteHost.test.tsx` — listing
  URL is `/api/remote/<host>/api/uploads?project=...`.
