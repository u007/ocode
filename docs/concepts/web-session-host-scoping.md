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
timestamp: 2026-09-19T13:00:25Z
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
| `/mcp` | `getMCP(host)` |
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

## Known residual

`/api/files/modified` is TUI-bridge-only and returns empty for headless
sessions.

## Rule

Any **new** session/project-scoped web call must thread the tab's host
(and project path). A background refresh must not be the sole caller, and
prefetch caches must be host-keyed to avoid cross-host collisions.

## Tests

- `web/src/components/Chat/commands.hostScope.test.tsx`
- `web/src/lib/sessionPrefetch.test.ts`
- `web/src/hooks/useTurnWatchdog.test.tsx`
- `web/src/lib/sessionEvents.test.ts`
