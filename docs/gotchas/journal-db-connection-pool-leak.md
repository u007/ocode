---
type: Gotcha
title: Journal DB Connection Pool Leak
description: 'Architectural gotcha: journal.go caches *sql.DB connection pools in a module-global journalCache but never closes them, causing a slow connection-pool leak per distinct project dir in long-running processes.'
tags:
  - architecture
  - database
  - connection-pool
  - leak
  - gotcha
timestamp: 2026-08-30T15:21:58Z
---
## The Problem

`internal/snapshot/journal.go` creates `*sql.DB` connection pools via `journalFor` and caches them in a module-global `journalCache` map, but never calls `db.Close()`. GC only prunes rows and files — the connection pools themselves are never released.

This causes a slow connection-pool leak: every distinct project directory that triggers a journal write opens a new SQLite connection pool (typically 1–5 connections), and those pools persist for the lifetime of the process.

## Scope

- Each `*sql.DB` in `journalCache` holds its own connection pool, keyed by `baseDir`.
- In a long-running process (web/desktop server, TUI session), `journalFor` may be called for multiple project directories over time.
- The pool is never pruned — `journalCache` grows monotonically.
- GC handles row/file cleanup but does not close DB handles.

## Impact

- **Long-lived processes**: The web server and desktop app accumulate pools. With many project roots touched, this is unbounded.
- **SQLite file locks**: Each open pool holds a connection (and potentially a WAL lock) even when idle, which can cause contention if multiple processes access the same journal file.
- **Memory**: Each pool allocates connection state, prepared statements, and WAL buffers.

## Mitigation Options

1. **LRU eviction**: Add an LRU or size-cap eviction to `journalCache` — when the cache exceeds N entries, close and evict the oldest. This bounds pool count without requiring explicit lifecycle management.
2. **Process-shutdown cleanup**: Add a `CloseAll()` that iterates `journalCache`, closes every `*sql.DB`, and clears the map. Call it from process shutdown hooks (TUI cleanup, web server shutdown).
3. **Lazy close-on-idle**: Track last-access time per pool; a background reaper goroutine closes pools idle for >N minutes.
4. **At minimum**: Document the lifetime. If the current behavior is intentional (process-scoped), add a comment explaining why `db.Close()` is omitted and noting the constraint.

## Severity

**Medium** — slow leak that matters in long-running processes with many distinct project roots. Not a correctness bug; journals still work. But unbounded pool accumulation is an architectural concern.

## References

- `internal/snapshot/journal.go:45-71` — `journalCache` map and `journalFor` function
- GC handles row/file cleanup but not DB pool lifecycle
- `docs/gotchas/` — related architectural gotchas