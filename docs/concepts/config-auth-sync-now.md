---
type: Concept
title: Config and credentials sync — Sync now and the status widget
description: POST /api/sync/sync (Sync now) answers 409 when logged out, 502 on a pull or push error and 200 with the status; it is single-flight, gives each blob a 30 s deadline, and the web SyncStatusWidget polls status and retries a failed sync.
resource: internal/server/handler_sync.go
tags:
  - sync
  - config
  - auth
  - server
  - web
  - widget
timestamp: 2026-11-25T17:20:34Z
status: active
---
# Config and credentials sync — Sync now and the status widget

Two blobs keep this machine and the sync server in step: `config` (ocodeconfig.json) and
`auth` (auth.json). The server is the one set by `sync_url`; see
[backend-sync-url-split.md](backend-sync-url-split.md), which also covers the sign-out when
that URL changes.

## Sync now: `POST /api/sync/sync`

Registered at `internal/server/server.go:757`. The wrapper `handleSyncSync`
(`internal/server/server.go:2229`) calls `Handler.HandleSyncNow`
(`internal/server/handler_sync.go:284`).

| Case | Answer | Contacts the hub |
|------|--------|------------------|
| No sync token | `409`, `not logged in to sync` | no |
| A pull or push fails for either blob | `502`, body `sync failed: <error>` | yes |
| Both blobs pulled and pushed | `200` with the sync status body (`loggedIn`, `config`, `auth`) | yes |
| A sync is already in flight | `200` with the current status; no second sync starts | no |

- **Both blobs are always tried**, config first, then auth. A config failure does not skip
  auth; the 502 reports the first error.
- **Each blob has its own 30 s deadline** (`syncBlobBoth`, `internal/server/handler_sync.go:259`,
  deadline at `:261`), so a slow pull of one blob cannot starve the other. Each blob runs a
  pull and then a push.
- **Single-flight.** `syncNowInFlight` is set under `syncMu` (`internal/server/handler_sync.go:296-304`);
  the network work runs outside that lock. A deferred reset clears the flag
  (`internal/server/handler_sync.go:306-310`), so it also clears after a failed pull.
- **Push cancellation.** The conflict-retry backoff in `Push` (`internal/sync/push.go:190-192`)
  stops when the context is cancelled and returns a wrapped `push <blob>: <context error>`
  instead of sleeping on.

## Status widget

`web/src/components/Layout/SyncStatusWidget.tsx` shows the sync state and the Sync now
button.

- **Two independent timers.** The 30 s status poll (`statusTimer`,
  `web/src/components/Layout/SyncStatusWidget.tsx:92-95`) and the device-login poll
  (`pollTimer`, `:164`, every `POLL_INTERVAL_MS` = 2 s, line 17) are separate. Each is
  cleared by its own cleanup; the login poll also stops on approval, expiry or dialog close.
- **Status is read through a ref.** The status interval is created in an effect with an
  empty dependency list, so its callback closes over the first render's state. It reads the
  latest status through `statusRef` (`:50`, written by `applyStatus`, `:57-60`), not React
  state.
- **Automatic retry.** Each status tick also calls `maybeAutoSync` (`:107-114`). When
  logged in and either blob is not synced, it starts a sync unless one was started less
  than `AUTO_SYNC_RETRY_MS` (30 s, `:55`) ago. Because the status tick is also 30 s, a failed
  sync effectively runs every second tick (about 60 s). A sync also starts once, 800 ms after
  mount, when a blob is not synced (`:117-123`).
- **Failure shown, status re-read.** A failed Sync now (`syncNow`, `:67-84`) shows the
  server's error text under the button, logs it to the console, and re-reads status, because
  one blob may have synced before the failure.

## Related

- [backend-sync-url-split.md](backend-sync-url-split.md): the sync URL, and the sign-out
  when it changes.
- [artifact-publishing.md](artifact-publishing.md): hub publishing needs a sync login.
