---
type: Gotcha
title: Remote session listing 404s for tilde-keyed projects
description: 'Remote session listing 404''d for every tilde-keyed project: desktop registers ~/path verbatim, host registry holds the expanded path, exact-match gate in HandleListProjectSessions 404''d; fixed via resolveRegisteredProjectRoot (host-side binary).'
tags:
  - remote
  - ssh
  - "404"
  - tilde
  - sessions
  - projects
  - web
  - gotcha
timestamp: 2026-09-29T17:38:07Z
---
# Remote session listing 404s for tilde-keyed projects (fixed 2026-09-30)

## Symptom — a 404, not a hang

The "Sessions — \<project\>" dialog for a remote SSH project spins forever (or, after the companion client fix, shows an error panel instead of a list). Crucially this was **never a hang**: every request completed in <100 ms.

**Live evidence** against `james@217.216.72.49` (all four tilde-keyed projects affected — `~/www/aimsai2`, `~/www/kakiit`, `~/www/nanobot`, `~/www/prop`):

```
GET /api/remote/james%40217.216.72.49/api/projects/sessions?path=%7E%2Fwww%2Faimsai2
→ 404 {"error":"project not found in saved list"}   (76 ms)

GET /api/remote/james%40217.216.72.49/api/projects
→ 200, the same project reported as /home/james/www/aimsai2
```

The two responses naming the same project differently in the same round trip is the fingerprint of this bug.

## Root cause: the same request carries two different path spellings

Three facts stack:

1. **The desktop registers the project verbatim.** `projects.AddRemote` deliberately does *not* expand `~` — the separator and `~` belong to the remote shell.
2. **The host expands it.** The remote machine runs its own `ocode serve --remote`, which saved the project through the LOCAL branch of `HandleAddProject`, which calls `projects.ExpandHome`. The host registry therefore holds `/home/james/www/aimsai2`.
3. **The client uses the path prefix, not a `?host=` query param.** `api.listProjectSessions(path, host)` (`web/src/api/client.ts`) builds `${remoteApiBase(host)}/api/projects/sessions?path=<encoded>` — a `/api/remote/{host}` PREFIX. So on the host the request arrives with `host == ""` and `path=~/www/aimsai2`.

`HandleListProjectSessions` (`internal/server/handler_projects.go`) then compared `p.Path == projectPath` **exactly** against the host's expanded registry → no match → `404 {"error":"project not found in saved list"}`.

### Why it hits the *listing* specifically

The mismatch class is old and was already closed for other endpoints (see Cross-references): terminal history/WS got `ExpandHome` on the host (403 class), git/fs got `resolveRegisteredProjectRoot` (400 "unknown project" class). The sessions listing had its own exact-match gate in its own handler, so it kept its own failure mode: a 404 on `/api/projects/sessions` while `/api/projects` (which does no path comparison) happily reported the project. An endpoint that *lists by path* but never *compares against a registry* can answer 200 for the project and 404 for its sessions in the same instant.

## Fix (HOST-side)

`HandleListProjectSessions` now resolves the query path through the pre-existing helper `resolveRegisteredProjectRoot` (`internal/server/handler_git.go`) — verbatim form first, `projects.ExpandHome` form as fallback — and scans the **resolved** path with `session.ListRefsForDir`.

- The `?host=` branch (remote-entry lookup on a *local* server) keeps its exact `(host, verbatim path)` match — a remote entry's path belongs to another machine and must never be expanded locally.
- The fallback **narrows rather than widens** the accepted set: unsaved paths and `~user` forms (`ExpandHome` leaves those alone) still 404. Same trust decision the git/fs endpoints already make.

**This fix lives in the host's `ocode serve --remote` binary.** It only takes effect once the version bump provisions the new binary onto the host (`EnsureRemoteServer` / `ensureBinary` → `EnsureBinary`, per `remote-terminal-502-provisioning.md`) — until the host is upgraded, the desktop's newer local server cannot help, because the failing comparison runs on the host.

**Tests:** `TestHandleListProjectSessionsResolvesTildeProject`, `TestHandleListProjectSessionsTildeFallbackStillRequiresSavedProject` in `internal/server/handler_projects_test.go` — the first is mutation-verified (reverting the resolution fails it).

## Companion client bug (why the spinner stuck)

A failed listing also stranded the dialog's loading spinner: a deduped background hover-warm could own the in-flight fetch a foreground click joined, and its `background` flag gated the `sessionsLoading` reset — so the error path skipped the reset forever. That is a separate client-side defect with its own fix (spinner ownership: the caller that RAISED the flag clears it, `SET_ACTIVE_PROJECT` resets it, failures now render a `role="alert"` panel instead of "No sessions yet"). See `concepts/web-tab-loading-indicators.md` — "A spinner must resolve even when the fetch is shared."

## Cross-references

- `gotchas/remote-terminal-502-provisioning.md` — root cause #3: the same tilde/expanded mismatch fixed for **terminal** endpoints (403 class).
- `concepts/web-session-host-scoping.md` — the backend half of the mismatch class as fixed for **git/fs/uploads** (400 "unknown project" class) via `resolveRegisteredProjectRoot`.
- `gotchas/remote-project-path-trust-boundary.md` — why the resolution still only ever returns a *saved* project root, and why the `?host=` branch must not expand.
- `concepts/web-tab-loading-indicators.md` — the spinner-ownership half of this incident.
- `gotchas/project-endpoint-isolation.md` — why this symptom is NOT the "everything hangs" class.
