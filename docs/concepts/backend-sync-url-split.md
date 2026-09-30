---
type: Concept
title: Backend and Sync URL Split (2026-09-03)
description: backend_url is local-dev only; sync_url is the dedicated config/auth sync channel. Migration steps for existing hub users.
resource: CLAUDE.md
tags:
  - config
  - sync
  - migration
timestamp: 2026-09-30T07:37:09Z
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

**Migration for existing hub users:** an on-disk `backend_url:
"https://hub.mercstudio.com"` is preserved verbatim in `ocodeconfig.json`
(not silently dropped) and logged as a warning on load, but resolves to
same-origin at runtime. To restore hub connectivity, move the value to
`sync_url` (Settings > Backend > Sync server, or
`PUT /api/config/ocode/sync-url`). Posting the legacy hub to
`PUT /api/config/ocode/backend` returns **400** with a `use sync_url instead`
hint. See `CHANGES.md` `[Unreleased]` for the full list of affected symbols.

When diagnosing a user whose hub routing silently broke after an upgrade, check
`ocodeconfig.json` for a `backend_url: "https://hub.mercstudio.com"` entry and
migrate it to `sync_url`.
