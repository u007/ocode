---
type: Gotcha
title: Session-scoped config helpers must pass the session's remote host
description: Session-scoped config helpers in web/src/api/client.ts must pass the session's remote host so they route to /api/remote/<host>/…; omitting it writes the local server's config and the remote sidebar refetch shows no change.
tags:
  - gotcha
  - remote
  - ssh
  - host
  - routing
  - web-api-client
  - config
  - sidebar
timestamp: 2026-09-28T12:02:36Z
---
# Session-scoped config helpers must pass the session's remote `host`

**Type:** Gotcha
**Description:** `getConfigModel`, permission toggles, and other session-scoped config/state helpers in `web/src/api/client.ts` take an optional `host`; omitting it for a remote (SSH/WSL) session writes the LOCAL server's config, so the remote session's sidebar refetch shows no change — the "toggle does nothing" symptom on remote SSH.
**Tags:** gotcha, remote, ssh, host, routing, web-api-client, config, sidebar

---

## Symptom

On a remote SSH project, flipping a session-scoped config toggle in the sidebar
(e.g. model selector, permission mode toggle, small-model switch, thinking
budget) appears to do nothing: the toggle snaps back / stays unchanged after
the sidebar refetches, while the local server's config may have been silently
mutated instead.

## Root cause

`web/src/api/client.ts` config/state helpers such as `getConfigModel`,
`setConfigModel`, `setSessionModel`, `clearSessionModel`, permission-model
toggles (`getPermissionModel`/`setPermissionModel`/`setPermissionModelEnabled`),
small-model, thinking-budget, explorer/context/recap/compact/speech helpers,
etc. all take an **optional trailing `host?: string`** parameter.

`fetchJSON(path, init, host)` (`web/src/api/client.ts`) only prefixes the path
with `remoteApiBase(host)` = `/api/remote/<encoded-host>` when `host` is
truthy:

```ts
const prefixed = host ? `${remoteApiBase(host)}${path}` : path;
```

Omitting `host` sends the request to the **local** server's plain
`/api/config/...` endpoint. But the agent that consumes these process-global
gates runs on the **session's own host** (the remote `ocode serve --remote`),
so:

1. The write lands in the **local** server's config — wrong process, wrong
   machine.
2. The remote session's sidebar refetch reads from the **remote** host (via
   `/api/remote/<host>/…`) and sees no change → "toggle does nothing."

Both halves of the bug are silent: no error, no 4xx — just a local config
mutation the user didn't ask for and a remote UI that never updates.

## Rule

**Any config/state helper in `web/src/api/client.ts` that takes an optional
`host` must be passed the active session's remote host** whenever the call
originates from a remote (SSH/WSL) session context. This applies broadly —
not just `getConfigModel`, but every helper whose trailing parameter is
`host?: string`: permission toggles, small-model, thinking budget,
explorer/context/recap/compact/speech config, advisor, discovery config, MCP,
terminal config, spending, etc.

When adding a new config/state helper to `client.ts`:

- Thread the optional `host` through `fetchJSON` (third arg) — never hardcode
  a bare path for state the remote agent reads.
- Ensure the caller passes the session's `projectHost` (the same host already
  threaded through `api.chat`/`api.sendMessage` in `web/src/hooks/useChat.ts`).

## Relation to existing remote-ssh gotchas

Complements, does not duplicate:

- `gotchas/remote-ssh-chat-profile-not-applied.md` — profile/key sync: the
  proxy stamps the authoritative window→profile mapping; chat request host
  threading is context there.
- `gotchas/remote-project-path-trust-boundary.md` — `(host, path)` identity
  and server-side local-vs-remote endpoint mode selection (a server concern,
  not the web client's fetch routing).
- `gotchas/remote-terminal-custom-port-omitted.md`,
  `gotchas/remote-terminal-502-provisioning.md` — terminal-specific remote
  routing issues.

This gotcha is the **web API client** half: the client-side optional-`host`
parameter that silently falls back to the local server when omitted.

## Code reference

- `web/src/api/client.ts:755-759` — inline comment on `getConfigModel`
  documenting exactly this ("the 'toggle does nothing' symptom on remote
  SSH").
- `web/src/api/client.ts` `fetchJSON` / `remoteApiBase` — the conditional
  `/api/remote/<host>` prefix.
