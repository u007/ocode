---
type: Gotcha
title: 'Port forwards Disable/Enable: URL composed past query + supervisor retained-terminal collision'
description: 'Two bugs broke Port forwards Disable/Enable: URL helper returned query-terminated string callers appended path segments onto (port landed inside project param), and process supervisor retained terminal records blocking stable-ID restart. Includes the test blind spot where widget API mocks can never catch malformed URLs.'
resource: web/src/api/client.ts; internal/tool/process_supervisor.go; internal/remote/portmap.go
tags:
  - port-forwards
  - url-composition
  - process-supervisor
  - web-api-client
  - gotcha
timestamp: 2026-09-16T16:32:06Z
---
# Port forwards Disable/Enable: URL composed past query + supervisor retained-terminal collision

**Type:** Gotcha  
**Description:** Two independent bugs broke the Port forwards panel's Disable/Enable/Remove on the project-scoped family (`/api/portmaps?host=&project=`): a URL helper returned a full query-terminated string that callers appended path segments onto (putting the port inside `project`), and the process supervisor retained terminal records that blocked restarting a stable-ID forward. Includes the test blind spot where widget-level API mocks can never catch malformed URLs.  
**Resource:** web/src/api/client.ts; internal/tool/process_supervisor.go; internal/remote/portmap.go  
**Tags:** port-forwards, url-composition, process-supervisor, web-api-client, gotcha  

---

# Port forwards Disable/Enable: URL composed past query + supervisor retained-terminal collision

Fixed 2026-09-16. Symptom: toggling a forward in the **Port forwards** panel answered
`host/project_path is not a remote project registered with this server`. The desktop
remote-workspace family (`/api/desktop/portmaps*`, no query) was unaffected — only the
project-scoped family hit both bugs.

## KEY LESSONS / GENERAL RULES

1. **Never append a path segment to a helper that returns a full URL-with-query.** If a
   helper builds `/api/resource?key=val`, appending `/${id}/disable` produces
   `/api/resource?key=val/3510/disable` — the segment lands *inside* the last query value,
   not in the path. The fix is to give the helper an explicit `suffix` parameter inserted
   *before* the query string.

2. **A stopped supervised process with a stable ID cannot be restarted by default.** The
   process supervisor retains terminal (exited/killed/failed) records, so `Start` with the
   same ID hits "already registered". Any manager that deliberately reuses an ID across
   Stop→Start cycles must opt in to `ProcessRegistration.ReplaceTerminal`.

3. **Widget tests that mock the API module assert call arguments, not the real fetch URL.**
   If the API module is `vi.mock`-ed, no test ever constructs the actual URL string, so
   composition bugs pass silently. URL-level tests must stub `global.fetch` and assert the
   requested URL/method against the real client code.

## 1. URL composition — suffix appended past the query

`portMapsPath(target)` in `web/src/api/client.ts` returned a URL that already contained
a query string (`/api/portmaps?host=…&project=~/www/app`). `removePortMap` and
`setPortMapEnabled` then concatenated `/${remotePort}` / `/${remotePort}/enable|disable`
*after* it, producing:

```
/api/portmaps?host=…&project=~/www/app/3510/disable
```

The server reads `project` as the full value `~/www/app/3510/disable` and 400s; the POST
even lands on the *add* route, not the enable route. List and Add were correct (no suffix),
so the panel rendered and a forward could be added — only Disable/Enable/Remove broke.

**Fix:** `portMapsPath(target, suffix)` now takes the trailing path segment so it is
inserted **before** the query:

```ts
// BEFORE (broken):
portMapsPath(target) + `/${port}/disable`   // → …?project=~/app/3510/disable

// AFTER (fixed):
portMapsPath(target, `/${port}/disable`)    // → …/3510/disable?project=~/app
```

**Where the seam is:** the helper is `web/src/api/client.ts:1713`
(`portMapsPath`). Callers are `removePortMap` (`:1695`) and `setPortMapEnabled`
(`:1697`). The server-side route is `internal/server/handler_portmaps.go`.

## 2. Process supervisor — terminal record blocks stable-ID restart

`ForwardManager` in `internal/remote/portmap.go` registers each forward under a stable
supervisor ID `remote-portmap-<port>`. `ForwardManager.Stop` marks that record terminal.
The process supervisor (`internal/tool/process_supervisor.go`) retains terminal records,
so the next `Start` for the same port hits `process "remote-portmap-3510" already
registered` — surfaced as 502 `enabled, but failed to open now`.

Pre-existing: also reachable via `internal/desktop/portmaps.go` and `ocode remote --web`
`/port disable` → `/port enable`. Only exposed by the new panel because forwards are now
toggled frequently.

**Fix:** `ProcessRegistration.ReplaceTerminal` (new, opt-in). When set and the existing
record for the ID is **terminal**, `StartSupervised` replaces it instead of failing; a
still-running record is never replaced. The forward registration opts in:

```go
tool.ProcessRegistration{
    ID:               forwardRegistrationID(pm.RemotePort),
    ReplaceTerminal:  true,   // allow Disable → Enable restart
    ...
}
```

**Where the seam is:** the replacement logic is `internal/tool/process_supervisor.go:213-234`
(`StartSupervised` registration fallback). The `canReplace` predicate is:

```go
canReplace := isTerminal && (
    reg.ReplaceTerminal ||
    reg.ID == "browse-chrome" ||
    reg.Kind == ProcessKindBrowser || recSnap.Kind == ProcessKindBrowser ||
    reg.Kind == ProcessKindHTR    || recSnap.Kind == ProcessKindHTR,
)
```

Generic `proc-N` collisions still error — only deliberately stable IDs opt in.

## 3. Test blind spot — widget mocks vs URL-level assertions

`web/src/components/Layout/PortMapsWidget.test.tsx` mocks `../../api/client` entirely, so
it asserts call *arguments* (e.g. "called with port 3510") but never constructs the actual
fetch URL. The composition bug was invisible to this test.

**Fix:** `web/src/api/client.portmaps.test.ts` stubs `global.fetch` and asserts the real
URL/method for list/add/remove/enable/disable with and without a target. This catches
composition bugs because the test exercises the real `portMapsPath` code path. Mirror this
pattern for new API endpoints that build URLs with query + path parameters.

## Regression tests

- `web/src/api/client.portmaps.test.ts` — URL/method assertions for all portmap operations.
- `internal/remote/portmap_test.go:TestForwardManagerRestartAfterStop` — fake `ssh` + open
  local port drive Start → Stop → Start; fails with the collision when `ReplaceTerminal`
  is removed.
- `internal/tool/process_test.go:TestStartSupervised_ReplaceTerminalAllowsRestartOfStableID`
  — opt-in replaces terminal records, default still collides, running records are never
  replaced.

All verified to fail with the fix reverted.
