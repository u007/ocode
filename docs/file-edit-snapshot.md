---
type: Concept
title: File-Edit Snapshot & Undo Mechanism
description: ocode takes a per-agent file snapshot before every write/edit/patch and provides an undo_file_change tool to revert by tool_call_id.
tags:
  - snapshot
  - backup
  - undo
  - file-edit
  - safety
timestamp: 2026-07-08T02:40:36Z
---
# File-Edit Snapshot & Undo Mechanism

ocode takes a **snapshot/backup of a file immediately before any modifying tool writes to it**, and exposes an `undo_file_change` tool to revert that tool call. This is the project's edit-safety mechanism — it does NOT rely on `git stash`/`git checkout --`/`git reset --hard` (those are explicitly forbidden in `AGENTS.md` as a coping strategy).

## Where the snapshots live
- Package: `internal/snapshot`.
- `Store.Backup(path, toolCallID)` reads the file's current bytes and writes a copy to the global **`<GlobalDataDir>/project/{slug}/snapshots/`** dir (ocode's global data dir, scoped per project by the git-root SHA-256 slug) on disk. Only metadata lives in RAM, so large files are safe (`internal/snapshot/snapshot.go:154-209`).
- Each `Snapshot` records `OriginalPath`, `BackupPath` (empty = file was new → undo = delete), `ToolCallID`, and `AgentStep`.

## What triggers a snapshot
Every modifying tool backs up *before* mutating:
- Write/edit/multi_edit/multi_file_edit/replace_lines/delete — `internal/tool/file.go` reads `prev` then calls `store.Backup(safe, tcID)` before `os.WriteFile`, and `store.RegisterWrite` after success (see `file.go:512-520`, `:538-547`; RegisterWrite call sites at lines 546, 661, 725, 811, 940, 1082).
- Patch tool — `internal/tool/patch.go:435` calls `snapshot.Backup` before applying.
- Formatter — `internal/tool/formatter.go:117` backs up before formatting.
- Config write-back — `internal/config/ocodeconfig.go:1071`.

## Undo
- `internal/tool/undo.go` exposes **`undo_file_change`** (name `undo_file_change`). Pass the original `tool_call_id` of a write/edit/multi_edit/multi_file_edit/replace_lines/delete; it restores all affected files to their pre-edit state via `Store.UndoByToolCallID`.
- `undoMaxAgeDelta = 2`: undo is refused once the snapshot is **more than 2 agent steps old** (`internal/tool/undo.go:14`).
- Conflict guards in `UndoByToolCallID` (`internal/snapshot/snapshot.go:219-248`): refuses if **another agent wrote the file after this agent's write**, or if **this agent made a newer still-active write** to the same file (within the undo window).
- A package-level `globalStore` powers backward-compatible **TUI undo/redo** and config backup (`internal/snapshot/snapshot.go:137-149`).

## Gotchas
- Snapshots are per-agent and expire by agent-step count, not wall-clock time. After 2 steps the backup is still on disk but can no longer be undone via the tool.
- Cross-agent writes to the same path block undo for the earlier agent (prevents clobbering another agent's work).
- Backups accumulate under `<GlobalDataDir>/project/{slug}/snapshots/`; there is no documented automatic GC beyond agent unregister (`UnregisterAgent`). Because the store lives outside the project working directory, it never enters the project's git tree.

## See also
- `AGENTS.md` — forbids `git stash`/`git reset --hard`/`git checkout -- <file>`/`git clean -fd` as a default coping strategy (the snapshot store is the supported alternative).
- `internal/tool/undo.go`, `internal/snapshot/snapshot.go`, `internal/tool/file.go`, `internal/tool/patch.go`
