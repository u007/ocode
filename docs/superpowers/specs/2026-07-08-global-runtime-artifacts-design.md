# Relocate ocode runtime artifacts to global config (project-scoped)

**Date:** 2026-07-08
**Status:** Implemented

## Goal
Move ocode's two local runtime artifacts **out of the project working directory**
so they can never pollute a user's git tree:

1. File-edit snapshot/backup store (powers `undo_file_change`, TUI undo/redo, config backup)
2. Markdown discovery summary cache (`md-summaries.json`)

Both now live under ocode's global data dir, scoped per project by the existing
git-root SHA-256 slug — the same slug sessions already use.

## Locations
Base `paths.GlobalDataDir()` → `~/.local/share/opencode` (macOS),
`$XDG_DATA_HOME/opencode` (Linux), `%LOCALAPPDATA%\opencode` (Windows):

- Snapshots → `GlobalDataDir()/project/{slug}/snapshots/`
- md-summaries → `GlobalDataDir()/project/{slug}/md-summaries.json`

## Why a new `paths` helper (not `session.ProjectSlug`)
`internal/session` imports `internal/agent` (uses `agent.Message`), so the agent
package **cannot** import session (cycle). Added a cycle-free
`paths.ProjectSlug(wd string) string` in `internal/paths` (mirrors session's
`git rev-parse --show-toplevel` + sha256, first 12 hex, Windows lowercasing).

## Changes
- `internal/paths/paths.go` — `ProjectSlug(wd)` + memoized `gitToplevel` mirror.
- `internal/snapshot/snapshot.go` —
  - `Store.baseDir` field; `NewStore(agentID, baseDir)` (empty → legacy
    `.opencode/snapshots` fallback, preserves tests + `globalStore`).
  - `Backup` writes to `baseDir`; backup filename includes `agentID` to avoid
    same-nanosecond same-basename collisions across agents in one project.
  - `Snapshot.BaseDir` records provenance so `Undo`/`Redo` synthesize redo paths
    against the snapshot's own dir, not the store's current dir (correct after
    `/cd`).
  - `SetBaseDir` + exported `snapshot.SetGlobalBaseDir`.
- `internal/agent/agent.go` — at construction compute
  `projectSnapshotsDir()` = `GlobalDataDir()/project/{slug}/snapshots`, pass to
  `NewStore`, and `snapshot.SetGlobalBaseDir(...)`. `SetWorkDir` re-points the
  per-agent store, `globalStore`, and clears `mdState` so `/cd` follows the new
  project (guarded against a nil per-agent store for minimal test agents).
- `internal/agent/md_discovery.go` — `mdSummaryCachePath(root)` helper returns
  the global project-scoped path; `ensureMDState` uses it.
- `internal/agent/prompt.go` — **reverted** the gitignore guard fragment added
  the previous turn (now moot; artifacts no longer live in the project).
- Docs — `docs/file-edit-snapshot.md` updated to the new global path.

## Decisions (user-approved)
- Layout: `project/{slug}/` (matches session storage).
- Gitignore prompt fragment: removed (moot).
- Old local `.opencode/snapshots` / `.ocode/md-summaries.json`: left orphaned
  (no migration; already gitignored in this repo).

## Testing
- `paths.ProjectSlug` covered by existing `paths_test.go` infra.
- `internal/snapshot/snapshot_test.go`, `internal/tool/undo_test.go` updated for
  the new `NewStore` signature; reset helper also clears `globalStore.baseDir`.
- `internal/agent/md_discovery_test.go` uses `mdSummaryCachePath(root)`.
- All green: `snapshot`, `paths`, `agent`, `tool` packages. `go build ./...`
  consumers (server, tui, config, root binary, desktop) compile.

## Residual notes
- `globalStore` (package-level, used by TUI undo/redo + config backup) defaults
  to the legacy relative path until `SetGlobalBaseDir` runs at agent creation.
  In normal ocode startup an agent exists before any config write-back, so the
  global dir is used. Pre-agent config backup (rare) would use the legacy
  fallback — acceptable; not worth extra startup wiring.
- Existing per-project caches are invalidated by the path move; first run after
  upgrade re-summarizes (blocking `mdSummarizePass`). By design (no migration).
