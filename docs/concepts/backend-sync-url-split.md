---
type: Concept
title: Backend and Sync URL Split (2026-09-03)
description: backend_url is local-dev only; sync_url is the dedicated config/auth sync channel, and changing it signs this machine out. Migration steps for existing hub users.
resource: CLAUDE.md
tags:
  - config
  - sync
  - migration
timestamp: 2026-11-22T18:18:58Z
---
# Backend and Sync URL Split (2026-09-03)

`backend_url` (`config.NormalizeBackendURL`) is now **local-dev-only**: empty
(same-origin) or `http://localhost[:port]` / `http://127.0.0.1[:port]`. The
production hub (`https://hub.mercstudio.com`) is no longer accepted there.

The dedicated config/auth sync channel is the new `sync_url` field
(`config.NormalizeSyncURL`): any `https://` origin plus `http://localhost` /
`http://127.0.0.1`; empty falls back to `OCODE_SYNC_URL`, then the production hub
(`sync.DefaultBaseURL` is now `https://hub.mercstudio.com`, up from
`http://localhost:3201`). `internal/sync` resolves via `sync.ResolveBaseURL`;
a one-time diagnostic (`sync.logBaseURLNotice`) is emitted at client
construction so an unconfigured local kakiit dev machine that silently
reaches production after the flip is visibly flagged.

## Changing sync_url signs this machine out

`HandleSetSyncURLConfig` (`internal/server/handler_sync.go:136`) compares the normalized
new `sync_url` with the previous one. A different URL runs `signOutSync`
(`internal/server/handler_sync.go:351`) **before** the new value is saved:

1. `detachSyncClientForLogout` (`internal/server/handler_sync.go:86`) stops the background
   watcher and clears the in-memory client.
2. A best-effort `Revoke` of the old token runs on the old server, outside any lock
   (`internal/server/handler_sync.go:356`).
3. `ocodesync.ClearToken()` clears the token locally. If that fails, the request answers
   500 and the URL is not saved.

Saving the URL that is already set changes nothing and keeps the token. A machine that is
not logged in only has its client detached.

The order matters. The stored token was issued by the old server, and `syncClient()`
(`internal/server/handler_sync.go:61`) rebuilds the client against the new URL. A Sync
after the change would send the old bearer token and the config and auth blobs to the new
server. An in-memory "suspended" flag was rejected: the server's watcher only starts on a
login in the same process, so nothing would survive a restart. After a URL change, Sync
stays logged out until the user logs in against the new server.

**Known gap:** after a process restart no client exists, so no revoke is sent. The token is
cleared locally but stays live on the old server. Logout has the same gap
(`HandleSyncLogout`, `internal/server/handler_sync.go:335`).

Tests: `TestSetSyncURLChangeSignsOutAndSuspendsSync` and `TestSetSyncURLSameURLKeepsToken`
(`internal/server/handler_sync_test.go:508` and `:578`).

**Migration for existing hub users:** an on-disk `backend_url:
"https://hub.mercstudio.com"` is preserved verbatim in `ocodeconfig.json`
(not silently dropped) and logged as a warning on load, but resolves to
same-origin at runtime. To restore hub connectivity, move the value to
`sync_url` (Settings > Backend > Sync server, or
`PUT /api/config/ocode/sync-url`). A logged-in machine is signed out by that change (see
above) and must log in again. Posting the legacy hub to
`PUT /api/config/ocode/backend` returns **400** with a `use sync_url instead`
hint. See `CHANGES.md` `[Unreleased]` for the full list of affected symbols.

When diagnosing a user whose hub routing silently broke after an upgrade, check
`ocodeconfig.json` for a `backend_url: "https://hub.mercstudio.com"` entry and
migrate it to `sync_url`.
