---
type: Gotcha
title: Shared tabs.json is written by every ocode server process — whole-map replace dropped projects
description: Multiple ocode server processes share one tabs.json. A whole-map PUT on a debounced write clobbered another process's projects; the fix is merge + explicit empty-list deletion under a cross-process file lock.
tags:
  - tabs
  - persistence
  - concurrency
  - multi-process
  - clobber
timestamp: 2026-09-22T06:37:36Z
---
## Symptom

A user shared their desktop session (Share → LAN/Tailscale URL) and opened it in a browser; project `mail-archive` showed 0 open tabs, while the desktop app still had its chat + terminal open. `~/.local/share/opencode/tabs.json` had lost the mail-archive entry entirely; both the desktop server (`:59658`) and a dev server (`:4096`) returned `GET /api/tabs` without it and `GET /api/tabs?path=/Users/james/www/mail-archive` → `{"tabs":null}`.

## Root Cause

- `tabs.json` lives in `paths.GlobalDataDir()` and is shared by **every** ocode server process on the machine (desktop `.app`, `make dev`/`go run` server, extra `ocode serve` instances, worktree servers). Each process had its own `Store` with an in-memory cache loaded once at boot (`internal/tabs/tabs.go`).
- `PUT /api/tabs` with `{projects:{...}}` was a whole-map **REPLACE** (`Store.ReplaceAll`) with documented/tested "projects absent are dropped" semantics.
- The web store (`web/src/stores/projectStore.tsx`) PUTs its whole `tabsByProject` map on a 400ms debounce. A window/process that never knew about another's project overwrote the file and dropped it.
- The `tabs_changed` event is per-process (in-process bus), so other processes never learned; their caches stayed divergent. File writes were non-atomic `os.WriteFile`, and there was no cross-process lock.

## Fix (shipped)

- **`internal/tabs/tabs.go`**: the bulk operation is now `ApplyBulk(patch)` = **MERGE**. Each provided project replaces its entry; an empty `tabs` list **DELETES** that project (explicit deletion signal); projects absent from the payload are **PRESERVED**. The read-modify-write runs under `filelock.WithFileLock` (cross-process), reloads the latest on-disk state first (`reloadIfChangedLocked` via an mtime+size stamp), and saves atomically (temp file + `os.Rename`). `Get`/`All` also reload when another process changed the file. `Set(path, empty)` clears a project; `ReplaceAll` was removed.
- **`internal/server/handler_tabs.go`**: `PUT /api/tabs` bulk path calls `ApplyBulk`; per-path path calls `Set`. Comments now document the merge + explicit-delete contract.
- **`web/src/stores/projectStore.tsx`**: `toServerTabs` emits an explicit empty entry for a known project whose last real tab closed (so deletion persists), and `REKEY_TABS` keeps the old path as `[]` instead of deleting the key (so the server gets the deletion).

## Rule to Capture

**Never make tab state a whole-map replacement across clients/processes.** A client can only vouch for the projects it knows; absence must mean "no change", and deletion must be explicit (empty list). Any new bulk/multi-writer JSON store in ocode must use `internal/filelock` + atomic temp→rename, and reload-on-change for reads.

## Related

- General lesson (may already exist elsewhere): multiple ocode server processes share the global data dir; per-process caches + whole-file writes are a recurring clobber class — see `docs/gotchas/session-writers-conflict-recovery.md` and `docs/concepts/cross-process-session-sync.md`.
