---
type: Concept
title: Changes Tab
description: A per-session TUI tab listing files added or edited by the current chat session (main agent + sub-agents), with unified diffs and undo.
tags:
  - changes
  - undo
  - session
  - file-tracking
  - TUI
timestamp: 2026-07-22T16:15:00Z
status: draft
---

# Changes Tab

The **changes tab** is a per-session TUI surface that lists every file the
current chat session has added or edited (main agent + sub-agents), shows a
unified diff per file against its pre-session state, and offers undo for
the entire file or for the most recent tool call on a file.

The list is **not git-based** — it derives from the existing
`internal/snapshot.Store` (every write/edit/patch/delete tool backs up the
file before mutating) plus a small pre/post-stat hook on the bash tool that
detects file mutations made by the shell directly.

## How it works

1. **Snapshot store (`internal/snapshot.Store`).** Every tool that modifies
   a file (`write`, `edit`, `multi_edit`, `multi_file_edit`, `replace_lines`,
   `delete`, `patch`, `formatter`) calls `Store.Backup(path, toolCallID)`
   **before** mutating. The backup is written to disk at
   `<GlobalDataDir>/project/{slug}/snapshots/`. A metadata record
   (`{OriginalPath, BackupPath, ToolCallID, AgentStep, Timestamp, ...}`) is
   kept in memory.

2. **Changes registry (`internal/changes.Registry`).** On `Agent.New`, a
   `Registry` is constructed and attached to the agent's snapshot store.
   Sub-agents spawned via the `task` tool share the same store
   (`subagent.go:300`), so their writes are captured without extra plumbing.

3. **Bash detection (`internal/changes/bash.go`).** A `StatBashRecorder`
   runs a pre/post stat-walk around the bash tool's execution. It compares
   file mtime/size before and after, then intersects with path-shaped tokens
   extracted from the command string. Detected changes are added to the
   registry with `Undoable: false`.

4. **TUI changes model (`internal/tui/changes_model.go`).** The `changesModel`
   calls `Registry.List()` on every render to get the current file list.
   The left pane renders a `ListBox`-powered file list; the right pane
   renders a unified diff of the selected file (via shelling to `diff -u`).

## Data model

```go
// FileChange is one row in the changes tab. All writes to the same file
// in the session are merged into one entry.
type FileChange struct {
    OriginalPath    string         // absolute path
    Status          FileStatus     // Added | Modified | Deleted
    FirstBackupPath string         // "" for files created in-session
    Undoable        bool           // false for bash-only entries
    UndoAllTCID     string         // first tool call id; used for "undo all"
    ChangeCount     int            // number of tool calls touching this file
    Authors         []ChangeAuthor // ordered: main first, then sub-agents
    CreatedAt       time.Time
    UpdatedAt       time.Time
    LastBashCommand string         // the command that last touched this file via bash
    LastBashExitCode int
}
```

## Undo semantics

- **`u` (undo file):** Calls `Registry.UndoFile(path)` which walks every
  attached snapshot store, collects all tool call IDs for the path, sorts
  them newest-first (LIFO), and calls `Store.UndoByToolCallID` on each.
  The LIFO order satisfies the conflict guard in the snapshot store (it
  refuses if a newer active write still exists). After all calls are
  undone, the file matches its pre-session state.

- **`U` (undo block):** Calls `Registry.LatestToolCall(path)` to find the
  most recent tool call, then `Registry.UndoBlock(path, tcid)` to restore
  that single call's snapshot.

- **Bash-only entries** are not undoable from the tab (`Undoable: false`).
  They have no snapshot backup to restore from. The user sees a status-bar
  message: "this file's only change came from a bash command and cannot be
  undone from the changes tab."

## Bash detection limits (v1)

The pre/post stat-walk is conservative:
- Skips `.git/`, `node_modules/`, `.opencode/`, `build/`, `dist/`.
- Intersects the stat diff with path-shaped tokens extracted from the
  command string (paths after `>`, `>>`, `tee`, `sed -i`, `mv`, `cp`,
  `rm`, `mkdir -p`, `touch`, `cat <<EOF >`).
- Misses: commands that touch files through the command's subprocesses
  (e.g. `make`), renames without a rename pattern, files modified via
  soft links outside the working dir.
- V2 may upgrade to FSNotifier (inotify/FSEvents) if the heuristic
  proves insufficient.

## See also

- `docs/superpowers/specs/2026-07-22-changes-tab-design.md` — the approved design.
- `PLAN-changes-tab.md` — the 16-phase implementation plan.
- `docs/file-edit-snapshot.md` — the snapshot mechanism the tab builds on.
- `internal/snapshot/snapshot.go` — the source of truth.
- `internal/changes/` — the registry, bash detection, and diff package.
- `internal/tui/changes_model.go` — the TUI changes tab model.
