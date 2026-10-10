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
timestamp: 2026-10-01T06:27:17Z
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
   Sub-agents spawned via the `task` tool and `ask` side queries share
   the parent's store **and** registry (`Agent.shareChangeTrackingFrom`),
   which also re-points their bash recorder at the shared registry —
   sharing the store alone captured write/edit tool writes but left
   sub-agent bash edits in a private registry nobody rendered.

   The store is bound to the chat session with `Store.SwitchSession`
   (via `Agent.SetChangesSession`) so backups journal to
   `snapshots.sqlite` and rehydrate after a rebuild. Every agent
   replacement in the TUI (`installAgent`, `rebuildAgentClient`) re-binds
   the fresh store; a store bound to a different session drops its live
   rows and rehydrates the target session's, so `/session load` on a
   reused agent shows the loaded session's files rather than the old ones.

3. **Bash detection (`internal/changes/bash.go`).** A `StatBashRecorder`
   runs a pre/post stat-walk around the bash tool's execution. `Pre()`
   returns a per-call `BashBaseline` that the caller hands back to
   `Post(base, …)` — one `BashTool` serves parallel tool calls, so the
   baseline must not live on the recorder. The baseline is either
   **complete** (Post may diff it) or **flagged** (`BashBaseline.complete`);
   diffing a partial walk is actively wrong, so `Post` declines and drops
   the event instead. Both walks run through one shared `fingerprint()`
   method that reports a `walkStatus`. `Post` compares file mtime
   (nanosecond) and size before and after, then intersects with path-shaped
   tokens extracted from the command string (`targetTokens`, which drops
   `cd`-style location tokens — see "Bash detection guards"). Detected
   changes are added to the registry with `Undoable: false`; an event that
   fails a guard is dropped **whole** and recorded as a `SkipNotice`.

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
    LastBashCommand string         // the most recent write from bash, when it is the row's newest event
    LastBashExitCode int
}
```

## Status semantics

`FileStatus` (Added / Modified / Deleted) is derived from the **live
filesystem** at render time, not from a stored op flag: the registry runs an
`os.Stat` per row on every `List()` and re-derives the status each time.
This is load-bearing for interleaved snapshot/bash sequences — a file that a
bash `rm` deleted is re-derived as Modified as soon as a newer snapshot
write or an undo puts it back on disk, instead of lingering as `-` forever.

Bash metadata (`LastBashCommand`, `LastBashExitCode`, `UpdatedAt`) is merged
into a snapshot-backed row **only when the bash event is at least as recent
as the newest snapshot for that path** — `LastBashCommand` documents the
write that came most recently from bash, so an older bash touch must not
claim a row a newer snapshot write owns. Deleting a file via bash does not
remove its snapshot backups, so such rows stay `Undoable`; if an undo then
consumes every snapshot, the row falls back to bash-only (non-undoable) and
`Status` reflects whatever the filesystem currently says.

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
- Intersects the stat diff with tokens from `pathTokenRegex`, a single
  regex (not a shell parser): any run of `[A-Za-z0-9_./~+-]` containing
  at least one slash. It has no verb awareness (`tee`/`sed -i`/`mv`/`cp`/
  `rm`/`touch` are never matched as verbs), and a bare slashless filename
  (`TODO.md`, `Makefile`) produces no token at all. `targetTokens` /
  `isLocationToken` first drop tokens naming the workDir or an ancestor of
  it (the `cd <workdir> && …` prefix); subdirectory targets are kept. See
  "Bash detection guards" for the fail-open this triggers and the `cd`-token
  rule.
- A file whose mtime or size moved is reported as modified. The
  pre-walk records no content hash (hashing every file per bash call is
  too slow), so a touched-but-identical file cannot be told apart from a
  real edit; an earlier version hashed the live file against itself and
  therefore dropped every same-size edit (`sed -i`, in-place rewrites).
- Misses: commands that touch files through the command's subprocesses
  (e.g. `make`), renames without a rename pattern, files modified via
  soft links outside the working dir.
- V2 may upgrade to FSNotifier (inotify/FSEvents) if the heuristic
  proves insufficient.

## Bash detection guards

Three guards stop one command from claiming files it never wrote. All live
in `internal/changes` (`bash.go`, plus `bash_registry.go` for the ceiling).
A guarded event is dropped **whole** — a row is never partially listed —
and the drop is recorded as a `SkipNotice`.

### 1. A baseline is complete or flagged

Diffing an incomplete pre-walk is actively wrong: every file the walk never
reached looks newly created (one truncated walk produced 4,214 phantom
"added" rows). `Pre`'s old contract — "the post-exec walk will still
produce a useful diff against an empty baseline" — was wrong and is
replaced: a baseline is either complete or flagged.

- `BashBaseline` carries `applicable`, `complete`, and `skipReason`.
- `applicable` is false only when `Pre` ran **no** walk (no workDir bound,
  or `unsafeWalkRoot`); `Post` then stays silent — that is a configuration
  state, not a dropped event.
- `walkStatus` (`walkDisabled` / `walkTruncated` / `walkComplete`) is
  returned by the single shared method
  `func (r *StatBashRecorder) fingerprint() (map[string]fileFingerprint, walkStatus)`,
  which replaced the previously duplicated Pre and Post walk bodies.
- The walk budget is a per-recorder `budget` field whose default is
  `defaultWalkBudget` (2s; formerly the bare `walkBudget` constant).
- A pre- or post-walk that hits the budget → `Post` drops the whole event
  with `SkipIncompleteBaseline`.

### 2. A `cd` is a location, not a target

`targetTokens()` wraps `pathTokensFromCommand` and drops every token for
which `isLocationToken(tok, workDir)` is true — i.e. the token **equals the
workDir or is an ancestor of it**. Touch matching is deliberately permissive
(a candidate matches as a substring of the path, either direction), so a
directory token admits every file beneath it; since virtually every command
begins `cd <workdir> && …`, an undropped location token made the candidate
intersection a no-op — which is how one command claimed thousands of rows it
never wrote. Subdirectory targets inside the workDir are deliberately
**kept** (`cp -r <workdir>/docs /tmp/x` names a real target).

### 3. Bounds

- `maxTouchesPerEvent = 200` — one event may contribute at most this many
  paths; above the cap the event is dropped whole (`SkipTooManyTouches`).
- `maxTrackedFiles = 5000` — registry-wide ceiling, enforced in
  `Registry.NotifyBashWrite`. It gates **new** paths only: a touch on an
  already-tracked path still refreshes `LastBashCommand` /
  `LastBashExitCode`. Refused paths are reported as `SkipRegistryCeiling`.
- `maxSkipNotices = 20` — size of the retained `SkipNotice` ring.

### Bounded fail-open (unchanged in spirit)

A command that names no slash-bearing path token — `cat >> TODO.md` yields
no token (the token regex requires a slash), and the same is true of most
heredocs and in-place `sed`s — leaves its diff unfiltered, but only while
the diff stays at or under `maxTouchesPerEvent`. That cap is what keeps
heredocs and in-place `sed`s visible in the tab; unbounded, this branch is
how a concurrent writer's files were attributed to an unrelated command.

### Skip visibility

A skipped event is **silent in the changes tab** but never invisible:

- `SkipNotice{Reason, Detail, Command, ExitCode, Paths, At}` with reasons
  `SkipIncompleteBaseline`, `SkipTooManyTouches`, `SkipRegistryCeiling`.
- `NewStatBashRecorder` takes a variadic `RecorderOption`;
  `WithSkipNotice(fn)` registers the callback. All four
  `changes.NewStatBashRecorder(...)` construction sites in
  `internal/agent/agent.go` pass `changes.WithSkipNotice(a.noteBashChangeSkip)`,
  which emits a `WARN` debug-panel line (the command echoed through
  `truncateForDebug`, capped at 120 bytes).
- The registry retains the notices (`NotifyBashSkipped` /
  `notifyBashSkippedLocked`, readable via `Registry.BashSkips()`),
  exposed agent-side as `Agent.BashChangeSkips()`.

A bash change row remains `Undoable: false`, guarded or not.

## See also

- `docs/superpowers/specs/2026-07-22-changes-tab-design.md` — the approved design.
- `PLAN-changes-tab.md` — the 16-phase implementation plan.
- `docs/file-edit-snapshot.md` — the snapshot mechanism the tab builds on.
- `internal/snapshot/snapshot.go` — the source of truth.
- `internal/changes/` — the registry, bash detection, and diff package.
- `internal/tui/changes_model.go` — the TUI changes tab model.