# Per-Session Changes Tab — Implementation Plan

> Companion to `docs/superpowers/specs/2026-07-22-changes-tab-design.md` (approved).
> The `writing-plans` skill is not installed in this repo, so this plan is authored directly
> following the same structure (phased, file-level, with validation gates).

## Executive Summary

Implement a new **"changes"** TUI tab that lists every file the current chat session
has added or edited (main agent + sub-agents), shows a unified diff per file against
its pre-session state, and offers undo for an entire file (restore pre-session) or
for the most recent tool call on a file (block-level). The list is **not git-based**
— it derives from the existing `internal/snapshot.Store` plus a small pre/post-stat
hook on the bash tool.

The work splits cleanly into three independent layers:

1. **Domain package** `internal/changes` — owns the per-session `Registry`, file
   change list, undo, and bash detection. TUI is a thin renderer on top.
2. **TUI surface** — new `tabChanges` slot in `internal/tui/tabs.go`, new
   `changesModel` sub-model, two-pane layout (file list + diff preview).
3. **Wiring** — construct the registry on `Agent.New`, hook the bash post-exec,
   wire sub-agent attach/detach via the `task` tool, persist on session save/load.

Phases are ordered so each is independently buildable and testable. Validation
gates use `go build`, `go vet`, `go test`, manual smoke (real session edit +
changes-tab undo), and the layout-overflow regression test from
`internal/tui/overflow_repro_test.go`.

## Current State Analysis (verified)

- **Snapshot store** (`internal/snapshot/snapshot.go`): per-agent `Store` with
  `Backup`, `RegisterWrite`, `ChangedFiles` (deduplicated), `UndoByToolCallID`
  (conflict-guarded, max-age 2 agent steps). Already has the cross-agent
  write registry (`fileWrites` map) and `crossAgentWriteAfterSeq` helper.
- **Agent owns the store** (`internal/agent/agent.go:312`): `a.snapshotStore`,
  constructed in `Agent.New` at line 610 (`snapshot.NewStore(snapshot.NewAgentID(),
  snapDir)`), wired into the tool context at line 3222
  (`toolCtx = snapshot.WithStore(toolCtx, a.snapshotStore)`).
  Exposes `a.ChangedFiles()` (line 319).
- **Bash tool** (`internal/tool/exec.go`): `BashTool` struct with `Procs
  *ProcessRegistry` field (line 24). `ExecuteStreamCtx` is the workhorse
  (`agent.go:3265` calls it). It already takes a `ctx` and reads the
  snapshot store + tool call id from it. **This is the seam for the
  post-exec hook** — the bash tool already has the command, working
  dir, and exit code at the end of execution. We add a `Recorder`
  field to `BashTool` and call it from `ExecuteStreamCtx` after
  the command returns.
- **TUI tabs** (`internal/tui/tabs.go`): constants `tabChat=0`, `tabAgents=1`,
  `tabFiles=2`, `tabGit=3`, `tabLog=4`, `tabCount=5`. `renderTabBar` returns the
  rendered string. Hotkey rotation is `% tabCount`-driven. Adding a 6th tab
  is mostly mechanical.
- **Per-tab render** (`internal/tui/model.go:15715` `renderTabContent`) routes
  by `m.activeTab` with explicit `case tab…` arms. The same per-tab
  routing exists in the per-tab `Update` switch at lines 2503-2530, the
  keypress handling at lines 2192-2427, and the mouse-click handling at
  lines 3320-3323.
- **App header** (`model.go:15662` `renderAppHeader`): always 2 rows
  (`appHeaderHeight` constant). All Y math uses this constant.
- **ListBox component** (`internal/tui/component_listbox.go`): ready to use,
  adopted by the sidebar already. Provides `SetData(count, renderRow)`,
  `Selected`, `SetSelected`, `HitTest(x, y)`, `Render`, `SetHeaderRows`,
  `SetFilterRow` — the file list will use it.
- **Files tab pattern** (`internal/tui/files_model.go`): two-pane layout
  (tree + preview), `View(w, h, styles, chatUnread, exitPending)` signature,
  per-tab `Update(msg, w, h)` with early-return by mode. The changes tab
  follows the same pattern but simpler (no directory tree).
- **Confirm dialog** (`internal/tui/component_dialog.go`): reusable
  Y/N confirmation. The `u` and `U` handlers reuse this.
- **Session persistence** (`internal/session/`): per-project session JSON
  under `~/.local/share/opencode/project/{slug}/sessions/`. Save/load
  hooks are the right place to round-trip the registry.
- **Layout safety net** (`internal/tui/model.go:10979`): if the rendered
  output exceeds `m.height`, the viewport shrinks and the layout
  re-renders. The 13-row regression test
  `TestActivityRowGrowthStaysWithinHeight` is the canary.
- **Existing global keybindings to shadow** (per-tab local wins):
  `u` → `/undo` (`model.go:4573`), `r` → `/redo` (`:4576`),
  `y` → copy session id (`:4588`), `g`/`G` → log tab top/bottom
  (`:17753`/`:17780`), `j`/`k` → chat input history (`:4705`-ish),
  `/` → files-tab fuzzy filter. Shadowing is per the existing TUI
  convention; the changes tab's `case tabChanges` arm in the per-tab
  Update switch dispatches before the global handlers.

---

## Implementation-level decisions surfaced for review

These are the design choices I'd most like your sign-off on BEFORE the plan is
committed. Each is a small enough decision that it didn't warrant its own
brainstorming question, but together they shape ~30% of the implementation
surface.

### D1. Bash post-exec hook seam — `BashTool.Recorder` field vs. a new `ToolHook` on the agent

**Option A (recommended)**: add a `Recorder BashRecorder` field to
`BashTool`. The agent sets `bashTool.Recorder = changes.NewBashRecorder(workDir)`
during tool wiring (alongside `Procs`). The recorder is called from
inside `ExecuteStreamCtx` after the command returns, with the
command string, working dir, and exit code. The recorder does pre/post
stat internally and forwards the diff to the registry.

**Option B**: add a generic "post-tool hook" mechanism to the agent's
tool runner, where the registry registers a callback for `name == "bash"`.
The hook fires in `agent.go:3275` after `RunPostHook`.

**Why A**: the pre/post stat needs the bash tool's `WorkDir`, which is
already inside the tool's execution. Option B would require the agent
to reach into the bash args (`args.Workdir`) — but bash has no
`workdir` arg today (it uses the agent's workdir), and the agent
already has that. Both work; A is more cohesive. **Confirm A.**

### D2. Bash pre/post stat implementation — walk the working dir tree, or use mtime+size only?

**Option A (recommended)**: walk the working dir tree before and after,
with mtime+size as the "did this change" filter, then sha256 only on
size-or-mtime-different files. Skip the standard noise dirs
(`.git/`, `node_modules/`, `.opencode/`, `build/`, `dist/`).

**Option B**: use a `fsnotify`/`inotify` watcher.

**Why A**: no new dependency, deterministic cost (bounded by directory
size), works in the existing environment. B is more complete but adds
a dependency and a moving part that must be cleaned up on every
session. **Confirm A for v1.** B is in the "out of scope" list already.

### D3. `Registry.List()` rebuild cost — eager vs. lazy

**Decision**: eager. The registry rebuilds `files` map on every
`AttachSnapshotStore`, `DetachSnapshotStore`, and `NotifyBashWrite` call.
For a session with N files and K agents, `List()` is `O(N)` map
iteration + `O(N log N)` sort. N is small (typically <100 changed
files per session). The cost is paid on every TUI render — render is
60Hz at most, so <1ms is acceptable.

**Confirm**: the spec already locks this in (L1 fix). Just flagging that
the alternative — caching the last List() result and invalidating on
mutation — is unnecessary.

### D4. Diff rendering — reuse git tab's diff or write a new one?

**Option A (recommended)**: reuse the existing git tab's diff renderer
(`internal/tui/git_model.go` already produces unified diffs via
`git diff --no-color` + parsing, or via `internal/diffx` if present).

**Option B**: write a new Go-only diff (`internal/changes/diff.go`)
using `github.com/sergi/go-diff` or similar.

**Why A**: the git tab's diff path is already battle-tested and
uses the same unified-diff shape the user expects. We pass it two
files (the backup file and the current file) and render the result.
**Confirm A.**

### D5. Per-tab shadowing of `u`, `r`, `y`, `g`/`G`, `j`/`k`, `/` — accept or remap?

**Decision (from spec §5.3)**: accept the per-tab shadowing. The
active keybindings are always shown in the left-pane footer hints
so the shadowing is discoverable.

**Alternative considered**: use `ctrl+x` leader prefix for the
undo keys (`ctrl+x u` / `ctrl+x U`). This would not collide with
anything but feels heavier than the natural `u` mnemonic. The
AGENTS.md rule is *"Avoid introducing raw shortcuts that are likely
to conflict with host terminals; prefer `ctrl+x` leader sequences
for non-essential UI toggles."* — undo IS essential, so `u` is
justified.

**Confirm**: shadowing accepted, `u`/`U` stay direct.

### D6. Pre-session baseline semantics — first snapshot vs. session-start

**Decision (from spec §4.6)**: diff baseline is the agent's first
snapshot for that file (i.e. bytes-on-disk immediately before the
session's first write to it), not the session-start state.

**Implication**: the user can have a stale row if they edited a
file outside ocode during the session — the right pane shows the
agent's pre-edit state vs current. **This is locked in the spec.**
No change.

### D7. Test scaffolding — borrow from `slash_popup_test.go` and `overflow_repro_test.go`

**Decision**: use `newTestTextarea` + `derefTestModel` from
`slash_popup_test.go:308-321` for the TUI tests; use the
`TestActivityRowGrowthStaysWithinHeight` pattern from
`overflow_repro_test.go` for the layout regression test.

**Confirm**: no new test infrastructure. Reuse what exists.

---

## Phase 0 — `internal/changes` package skeleton

**New files:**
- `internal/changes/changes.go` — package doc, `FileChange`, `FileStatus`,
  `ChangeAuthor`, `BashOp`, `BashTouch`, `BashWriteEvent`, `Registry`,
  `NewRegistry`, `AttachSnapshotStore`, `DetachSnapshotStore`,
  `NotifyBashWrite`, `List`, `UndoFile`, `UndoBlock`, `LatestToolCall`,
  `ErrNotUndoable`, `ErrNoChanges`.
- `internal/changes/changes_test.go` — table-driven unit tests for
  the type constructors and the error sentinels.

**Validation:** `go build ./internal/changes/...` compiles; `go vet`
clean; `go test ./internal/changes/` green (the tests are minimal —
just constructor and error returns).

## Phase 1 — `Registry` with single-store reads

**New files (in `internal/changes/`):**
- `internal/changes/registry.go` — `Registry` implementation. `List()` walks
  `byAgent[agentID].ChangedFiles()` for each attached store, deduplicates
  by path, and returns `[]FileChange` sorted by `UpdatedAt` desc. The
  `UpdatedAt` is approximated as `time.Now()` for now (we'll fix this
  in Phase 3 when we add the per-snapshot walk).

**`internal/changes/changes_test.go`:** extend with
`TestRegistryAttachAndList` — create a real `snapshot.Store`, attach it,
write a file via the store, call `List()`, assert the entry is present
with the right path and `Undoable: true`.

**Validation:** `go test ./internal/changes/ -run Registry` green.

## Phase 2 — Multi-agent aggregation

**`internal/changes/registry.go`:** `List()` already iterates `byAgent`;
no change. The tests in Phase 1 already cover the multi-agent case
(just call `AttachSnapshotStore` twice with different agent IDs and
different files).

**`internal/changes/changes_test.go`:** `TestRegistryMultiAgent` —
two stores, each writes one file to a different path; `List()` returns
both; detaching one removes its entries from subsequent `List()` calls
but the file on disk is unaffected.

**Validation:** `go test ./internal/changes/ -run MultiAgent` green.

## Phase 3 — `FileChange` materialization (per-snapshot walk)

**`internal/changes/registry.go`:** add `materialize(path string)
*FileChange` (private). For each attached store, walk the
`store.snapshots` slice in reverse to find all entries for `path`;
pick the first one (chronologically oldest) as `FirstBackupPath` and
`UndoAllTCID`; collect the rest into a list for `ChangeCount` and
`Authors`. `List()` calls `materialize` under the lock for every
distinct path. `UpdatedAt` is the timestamp of the most recent
snapshot for that path.

**`internal/changes/changes_test.go`:** `TestMaterializeFirstBackup`
— write 3 times to the same file (3 snapshots), assert the entry's
`FirstBackupPath` is the first backup, `UndoAllTCID` is the first
tool call id, `ChangeCount == 3`, and `UpdatedAt` is the timestamp
of the third write.

**Validation:** `go test ./internal/changes/` green.

## Phase 4 — Undo (`UndoFile` oldest-first; `UndoBlock` latest-call)

**`internal/changes/registry.go`:** implement
`UndoFile(path string) error` and `UndoBlock(path, toolCallID string) error`
and `LatestToolCall(path string) (string, error)`.

- `UndoFile` walks the `Authors` list oldest-first and calls
  `store.UndoByToolCallID(agentFirstTCID, maxAgeDelta=2)` on each.
  Returns `ErrNotUndoable` if `FileChange.Undoable == false`.
- `UndoBlock` looks up the (path, toolCallID) in the right store and
  calls `store.UndoByToolCallID(toolCallID, 2)`.
- `LatestToolCall` walks the `Authors` list newest-first and returns
  the latest tool call id. Returns `ErrNotUndoable` if `Undoable == false`
  and `ErrNoChanges` if no attached store has a snapshot for `path`.

**`internal/changes/changes_test.go`:**
- `TestUndoFileOldestFirst` — three writes; undo file; verify the
  final state matches the first backup.
- `TestUndoBlockLatest` — three writes; undo latest block; verify
  the second snapshot is now current.
- `TestUndoNotUndoable` — bash-only entry; assert `UndoFile`
  returns `ErrNotUndoable`.
- `TestUndoConflictGuard` — three writes; advance agent step >2;
  attempt `UndoFile`; assert it returns the "expired" error from
  the snapshot store.

**Validation:** `go test ./internal/changes/` green.

## Phase 5 — Bash detection

**New files:**
- `internal/changes/bash.go` — `BashRecorder` interface and
  `StatBashRecorder` implementation. The recorder's `Pre()` method
  walks the working dir and captures `(path, mtime, size, sha256)`.
  The recorder's `Post(command, exitCode)` method walks the working
  dir again, diffs against `Pre`, intersects with a path-extraction
  regex over `command` (paths after `>`, `>>`, `tee`, `sed -i`,
  `mv`, `cp`, `rm`, `mkdir -p`, `touch`, `cat <<EOF >`), and
  calls `registry.NotifyBashWrite(BashWriteEvent{Command, WorkDir,
  ExitCode, Touches})` for each result.
- `internal/changes/bash_test.go` — `TestBashRecorderDetectsCreate`
  (pre = empty dir, run `echo hi > foo`, post = `foo` exists;
  recorder reports `BashTouch{Path: foo, Op: BashAdded}`);
  `TestBashRecorderNoFalsePositiveOnPathInComment` (run
  `echo "this mentions /etc/passwd but doesn't touch it"`;
  recorder reports zero touches);
  `TestBashRecorderSkipsNoiseDirs` (touch `node_modules/x`;
  recorder reports zero touches).

**`internal/tool/exec.go`:** add `Recorder changes.BashRecorder` field
to `BashTool`. Call `recorder.Pre()` at the start of
`ExecuteStreamCtx` (after the command is parsed), `recorder.Post(command,
exitCode)` at the end. Only call if `Recorder != nil`.

**`internal/agent/agent.go`:** in `NewAgent`, when constructing the
bash tool, set `bashTool.Recorder = changes.NewStatBashRecorder(workDir,
a.changes)`. (This needs the registry to be constructed first — see
Phase 9 for the wiring order.)

**`internal/changes/registry.go`:** `NotifyBashWrite(event)` materializes
a new `FileChange{OriginalPath, Status, Undoable: false, ...}` for each
`event.Touches[*].Path`. If the path already exists in the registry,
update it; else add.

**Validation:** `go test ./internal/changes/ -run Bash` green;
manual smoke: run `echo hi > /tmp/x`, verify the changes tab reports
`/tmp/x` as added with `(bash)` marker.

## Phase 6 — TUI surface — tab registration

**`internal/tui/tabs.go`:** add `tabChanges = 4`, increment
`tabCount = 6`, update `tabGit` to 4, `tabLog` to 5. Update
`renderTabBar` labels list to `["chat", "agents", "files", "changes",
"git", "log"]`.

**`internal/tui/model.go`:** the constants `tabGit` and `tabLog` are
referenced in many places; the existing `case tabGit` and `case tabLog`
arms need to be re-pointed to the new indices. The hotkey rotation is
`% tabCount`, so it auto-extends.

**New arms in:**
- `renderTabContent` (`model.go:15715`) — `case tabChanges: return m.changes.View(w, h, styles, chatUnread, exitPending)`
- The per-tab Update switch (`model.go:2503-2530`) — `case tabChanges: ... return m.changes.Update(msg, w, h)`
- The chrome-gating early-returns in keypress handling (`model.go:2192-2427`) — add `tabChanges` arms where they currently check `tabFiles`/`tabGit`
- The mouse-click handling (`model.go:3320-3323`) — add `case tabChanges:` arms where they check `tabGit`/`tabFiles`

**New files:**
- `internal/tui/changes_model.go` — `changesModel` struct, `NewChangesModel(agent, workDir)`, `Update`, `View`, mouse handling, key handling.

**Validation:** `go build ./...`; manual smoke: launch the TUI;
verify the tab bar shows six tabs; arrow keys cycle through them
including the new one; the new tab renders the empty state.

## Phase 7 — `changesModel` View + Update (without ListBox yet)

**`internal/tui/changes_model.go`:**

```go
type changesModel struct {
    agent       *agent.Agent
    workDir     string
    width       int
    height      int
    files       []changes.FileChange // snapshot of Registry.List() at last render
    cursor      int
    selected    int
    diffViewport viewport.Model
    diffCache   map[string]string   // path → rendered diff (lazy)
    showDetails bool                // ? key toggles
    confirm     *confirmState       // nil when not in confirm dialog
    statusMsg   string              // bottom status line; cleared on next key
}

func NewChangesModel(a *agent.Agent, workDir string) *changesModel
func (m changesModel) Update(msg tea.Msg, w, h int) (changesModel, tea.Cmd)
func (m changesModel) View(w, h int, styles Styles, chatUnread, exitPending bool) string
```

Keybindings implemented in this phase:
- `j` / `k` / `↓` / `↑` — move cursor
- `enter` — open the diff in the right pane
- `g` / `G` — jump top/bottom
- `?` — toggle per-row details
- `esc` — clear details, or leave the tab (handled by the global model)

The two-pane layout is hand-rolled in this phase (no ListBox yet).

**`internal/tui/changes_model_test.go`:** `TestChangesModelCursor`,
`TestChangesModelKeybindings`, `TestChangesModelEmptyState`. Use the
existing `newTestTextarea` + `derefTestModel` scaffold from
`slash_popup_test.go:308-321`.

**Validation:** `go test ./internal/tui/ -run ChangesModel` green;
manual smoke: edit a file via the chat tab, switch to changes tab,
see the file, press `enter`, see the diff.

## Phase 8 — ListBox adoption

**`internal/tui/changes_model.go`:** swap the hand-rolled file list
for `listbox.NewListBox`. The listbox handles selection, scrolling,
click-to-select, and the in-app selection recipe. The
`changesModel` keeps the `files []changes.FileChange` and the
`showDetails bool`; the listbox takes over cursor + scroll state.

**`internal/tui/changes_model_test.go`:** add a hit-test test that
clicks at known screen Y coordinates and asserts the listbox selects
the right row — this exercises the geometry invariant the ListBox
plan was designed to enforce.

**Validation:** `go test ./internal/tui/ -run ChangesModel` green;
manual smoke: click rows in the file list; verify selection moves
correctly regardless of header height.

## Phase 9 — Undo wiring (`u` / `U` + confirm dialog)

**`internal/tui/changes_model.go`:** add `u` and `U` key handlers.

- `u`: call `agent.changes().UndoFile(files[selected].OriginalPath)`.
  On `ErrNotUndoable`, set `statusMsg = "this file's only change came
  from a bash command and cannot be undone from the changes tab"`.
  On success, refresh `files` from `Registry.List()`. On any other
  error, set `statusMsg = err.Error()`.
- `U`: call `agent.changes().LatestToolCall(files[selected].OriginalPath)`
  first, then `UndoBlock(path, tcid)`. Same error handling.

Both keys open a confirm dialog before executing. Reuse the existing
`internal/tui/component_dialog.go` `Confirm` API. The dialog
component already handles `y` / `n` / `esc`; wire it to the model.

**`internal/tui/changes_model_test.go`:** `TestChangesModelUndo`,
`TestChangesModelUndoBashOnly` (asserts status message, no
confirm dialog). Use a fake `changes.Registry` to avoid the
dependency on the real agent.

**Validation:** `go test ./internal/tui/ -run ChangesModel` green;
manual smoke: edit a file via the chat tab, switch to changes,
press `u`, confirm, verify the file is restored.

## Phase 10 — Diff rendering in the right pane

**`internal/changes/diff.go`:** `RenderDiff(path, backupPath, currentPath
string) (string, error)` — uses the existing git tab's diff
infrastructure. Open the two files as `*os.File`, hand them to the
same diff function the git tab uses, return the rendered string.

**`internal/changes/diff_test.go`:** golden tests with a small fixed
fixture (3-line file with one change).

**`internal/tui/changes_model.go`:** the `diffCache` field stores
the rendered diff keyed by path. The right pane viewport
(`viewport.Model`) is updated when the user presses `enter` or
`j`/`k` moves to a new row. Lazy loading: only render the diff
when the user opens the file, not eagerly on every keystroke.

**Validation:** `go test ./internal/changes/ -run Diff` green;
manual smoke: open a file in the changes tab, see the unified
diff with `-` and `+` lines.

## Phase 11 — Agent wiring (`Agent.New` constructs the Registry)

**`internal/agent/agent.go`:**
- Add `changes *changes.Registry` field.
- In `Agent.New` (around line 610 where `a.snapshotStore` is
  constructed), construct `a.changes = changes.NewRegistry()` and
  call `a.changes.AttachSnapshotStore("main", a.snapshotStore)`.
- Expose `func (a *Agent) Changes() *changes.Registry`.
- Wire the bash tool's `Recorder` field to call
  `a.changes.NotifyBashWrite`. The recorder's
  `NewStatBashRecorder(workDir, a.changes)` is the factory.
- On agent teardown, the registry is GC'd with the agent.

**Validation:** `go build ./...`; manual smoke: launch TUI,
edit a file, verify it shows up in the changes tab.

## Phase 12 — Sub-agent attach/detach via the `task` tool

**`internal/agent/subagent.go`:** where sub-agents are spawned
(around the `task` tool's `Execute`), call
`parentAgent.Changes().AttachSnapshotStore(subID, subAgent.SnapshotStore())`
when the sub-agent is created, and
`parentAgent.Changes().DetachSnapshotStore(subID)` when the
sub-agent completes (success or failure).

**`internal/changes/changes_test.go`:** `TestRegistrySubAgentLifecycle`
— attach a sub-agent's store, write a file via that store, assert
the file appears in the main registry's `List()`. Detach the
sub-agent, write another file, assert the second file does NOT
appear.

**Validation:** `go test ./internal/changes/ -run SubAgent` green;
manual smoke: in the chat tab, run a `task` that writes a file;
switch to the changes tab; verify the sub-agent's file is listed.

## Phase 13 — Session persistence

**`internal/session/session.go`:** on save, marshal
`agent.Changes().List()` into the session JSON under a new
`"changes"` key. On load, after constructing the new agent
(Phase 11), re-hydrate by:
1. Call `AttachSnapshotStore` for the main agent (already done in
   Phase 11).
2. Iterate the saved `[]FileChange` and call
   `AttachSnapshotStore` for any sub-agent referenced.
3. Replay the saved list as a hint for the first render.

The saved list is allowed to be stale; live stores are the source
of truth on every subsequent render (per spec §4.6).

**Validation:** `go test ./internal/session/ -run Changes` green
(new test); manual smoke: edit a file, close the session, reopen
it, verify the file is still in the changes tab.

## Phase 14 — Layout safety + manual smoke

**`internal/tui/changes_model.go`:** add a regression test
`TestChangesModelLayoutWithinHeight` (modeled on
`TestActivityRowGrowthStaysWithinHeight` from
`internal/tui/overflow_repro_test.go`). Use a 13-row terminal,
instantiate a `changesModel` with 3 changed files, render, assert
`lipgloss.Height(result) <= 13`.

**Manual smoke (full):**
1. Launch TUI in a project.
2. In the chat tab, ask the agent to add a new file.
3. Switch to the changes tab. Verify the file is listed with
   `+` status.
4. Press `enter`. Verify the diff is shown.
5. Press `u`. Confirm. Verify the file is deleted (it was added
   in-session, so undo = delete).
6. Run a bash command that creates a file. Switch to the
   changes tab. Verify the file is listed with `+` and `(bash)`
   marker.
7. Press `u` on the bash entry. Verify the status message
   "this file's only change came from a bash command..." appears.
8. Run `/new`. Verify the changes tab is empty.
9. Close the TUI, reopen, resume the same session. Verify the
   changes are still listed.

## Phase 15 — Docs + AGENTS.md cross-link

**New files:**
- `docs/changes-tab.md` — concept doc describing the changes tab,
  the per-file/merged model, undo semantics, and what is and isn't
  covered (bash detection limits, watcher not implemented, etc.).
  Follows the structure of `docs/file-edit-snapshot.md`.
- `docs/index.md` — add a line under "Concepts" for the new doc
  and a line under specs for the design doc.

**Modified files:**
- `AGENTS.md` — add a one-line cross-reference in the TUI section
  pointing to the changes tab. Update the "in scope" knowledge
  bundle to include `docs/changes-tab.md`.
- `docs/file-edit-snapshot.md` — add a "see also" link to
  `docs/changes-tab.md`.

**Validation:** docs render correctly when the knowledge bundle is
re-scanned (no broken links).

---

## Validation gates (per phase)

Each phase ends with:

1. `go build ./...` — clean.
2. `go vet ./...` — clean.
3. `go test ./internal/changes/ ./internal/tui/ ./internal/agent/ ./internal/tool/ ./internal/session/` — green.
4. For TUI-affecting phases (6, 7, 8, 9, 14): manual smoke
   in a real terminal (Ghostty, VS Code, iTerm2 — at least one
   host terminal) verifying the tab renders, keybindings work,
   and the layout doesn't overflow.
5. For phases that touch the bash tool (5, 11): manual smoke
   running a real bash command and verifying the changes tab
   picks it up.

## Out of scope (deferred — already locked in spec §9)

- Web SPA exposure of the changes API.
- Per-hunk undo UI (per-call undo remains via `undo_file_change`).
- Stash / re-do from the tab.
- Renames / moves detection.
- Filesystem watcher.
- Cross-session diffing.

## Risks and open questions

1. **Pre/post stat cost on large repos.** Phase 5 walks the working
   dir tree. A repo with 100K files could take >1s. Mitigation: scope
   the walk to the bash command's `WorkDir` (typically a subdir), and
   skip noise dirs aggressively. If still slow, fall back to a
   targeted path-extraction-only strategy (no stat, just regex on
   the command) and live with the false-negative tradeoff.
2. **Stale sub-agent stores on session re-hydration.** Spec §4.6
   accepts this trade-off. Phase 13 implements it as designed; the
   test `TestSessionChangesRehydration` asserts that the main
   agent's files survive a session close/reopen.
3. **Keybinding discoverability.** The per-tab shadowing of
   `u`/`r`/`y`/`g`/`G`/`j`/`k`/`/` is non-obvious. The active
   keybindings are always shown in the left-pane footer hints
   (Phase 7). The footer is part of the visual design, not an
   afterthought.
4. **Diff rendering performance.** The git tab's diff renderer
   is shell-out based (`git diff`). For the changes tab, we diff
   arbitrary file pairs (backup vs current), not git refs. We
   need a Go-only diff (Phase 10). If the chosen library is slow
   for large files, we add a 1MB line cap and warn the user.

## See also

- `docs/superpowers/specs/2026-07-22-changes-tab-design.md` — the
  approved design this plan implements.
- `docs/file-edit-snapshot.md` — the snapshot mechanism the
  changes tab builds on.
- `PLAN-live-preview.md` — the most recent plan, whose structure
  this plan mirrors.
- `PLAN-listbox-component.md` — the ListBox plan, whose goal the
  changes tab is the third adopter of.
- `internal/snapshot/snapshot.go` — the source of truth for
  "what changed."
- `internal/tui/tabs.go` — the tab bar the new tab slots into.
- `internal/tui/files_model.go` — the closest existing TUI
  surface; the changes tab follows the same pattern.
- `internal/tui/component_listbox.go` — the ListBox component.
- `internal/tui/component_dialog.go` — the confirm dialog.
- `internal/tui/overflow_repro_test.go` — the layout safety
  test pattern.
- `internal/session/` — the session persistence layer.
