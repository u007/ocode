# Per-Session Changes Tab — Design

- **Date:** 2026-07-22
- **Status:** Approved (design) — pending implementation plan
- **Goal:** A new TUI tab that lists every file the current chat session has
  added or edited (main agent + sub-agents), shows a unified diff per file
  against its pre-session state, and offers undo for an entire file (restore
  pre-session) or for the most recent tool call on a file (block-level). The
  list is **not git-based** — it derives from the existing
  `internal/snapshot.Store` plus a small pre/post-stat hook on the bash tool.

---

## 1. User-facing summary

A new **"changes"** tab between `files` and `git` lists the files this chat
session has touched. Each row is one file; multiple edits to the same file
collapse into a single row whose right-pane preview is a unified diff of
`pre-session bytes → current bytes`. The user can:

- `u` — undo the entire file (restore pre-session state)
- `U` — undo the most recent tool call on the selected file (block-level)
- `/` — fuzzy-filter the file list
- `enter` — open the diff in the right pane
- `r` — refresh from the live registry
- `y` — copy the diff to the clipboard

`/new` resets the list (the registry is per-session and lives on the `Agent`).
The list is **persisted with the session** — closing and re-opening a session
re-hydrates the rows. Files whose only change came from a bash invocation
(heredoc, `cat >`, `sed -i`, `rm`, etc.) appear in the list but are marked
`(bash)` and **cannot be undone from the tab** (the bash path didn't go
through the snapshot store, so there is no backup to restore from).

## 2. Decisions (confirmed with user + advisor)

| Topic | Decision |
|-------|----------|
| Tab scope | **Per-session, persisted** — list lives on the `Agent`, serialized into the session JSON, re-hydrated on resume. |
| `/new` | **Resets the list** (new `Agent` = new empty registry). |
| Change sources | **Snapshot-tracked tools + bash detection** (pre/post stat). Bash-only entries are visible but marked non-undoable. |
| Block granularity | **Per file, merged** — one diff per file; undo restores pre-session state. Per-call undo remains available via the existing `undo_file_change` LLM tool. |
| Sub-agent writes | **Main agent + all sub-agents**, aggregated by the registry. The main agent's rows sort first. |
| Code organization | **New `internal/changes` package**, separate from `internal/snapshot`. TUI is a thin renderer on top. |
| Tab position | **Between `files` and `git`** (6th slot; `git` and `log` indices shift by one). |
| Undo keys | **`u` = whole file**, **`U` = latest block** (capitalized = narrower). |
| Web SPA | **Out of scope v1**. The package API is shaped to allow `/api/changes` later without breaking changes. |
| Renames / moves | **Out of scope v1**. |
| Filesystem watcher | **Out of scope v1**. Bash pre/post stat is the v1 approximation. |

## 3. Architecture overview

```
┌──────────────────────────────────────────────────────────────────────┐
│  TUI  ─ new tabChanges → changesModel {View, Update, …}                │
│                              │   queries on every render              │
│                              ▼                                        │
│  internal/changes.Registry  (per-session; owned by Agent)              │
│   ├─ FileChange{…} list                                                │
│   ├─ Subscribe/Unsubscribe API for snapshot events                      │
│   └─ Subscribe API for bash writes (heuristic path extraction)          │
│                              │   reads/writes                            │
│                              ▼                                          │
│  internal/snapshot.Store  (per-agent; already exists)                  │
│   └─ events: Backup / RegisterWrite                                     │
│                                                                          │
│  internal/tool/bash.go  ─────► on each invocation:                       │
│   └─ onPostExec hook → registry.NotifyBashWrite(path, ts, op)            │
│                                                                          │
│  internal/session/  ─────► on save: serialize Registry.FileChange list   │
│                            on load: re-hydrate from session JSON          │
│                            (re-bind to live snapshot stores on resume)   │
│                                                                          │
│  Session lifecycle:                                                      │
│   • new Agent() / /new       → Registry is constructed (empty)            │
│   • session save             → Registry snapshot serialized to session   │
│   • session load / /resume   → new Agent() + Registry replay from disk   │
└──────────────────────────────────────────────────────────────────────┘
```

### 3.1 Session lifecycle

- `/new` in the TUI is the existing instant command. It tears down the current
  `Agent` and constructs a new one. Because the `Registry` is held by the
  `Agent`, the new agent has a new empty registry. The old session's changes,
  if it had been saved, persist as part of the old session's JSON.
- Switching sessions (`/sessions`, `/resume`) follows the same rule: the new
  session's `Agent` is constructed fresh, and the `Registry` is re-hydrated
  from the saved session JSON.
- The registry is **never** shared across sessions. Sub-agents spawned via
  the `task` tool have their own registry; their writes are aggregated into
  the main agent's view (see §4).
- An in-memory `Registry` is automatically GC'd when the `Agent` is destroyed
  (Go GC). The on-disk backup files in
  `<GlobalDataDir>/project/{slug}/snapshots/` are governed by the existing
  `snapshot.Store` lifetime and are unaffected.

## 4. `internal/changes` package (new)

### 4.1 Types

```go
// FileChange is one row in the changes tab. It represents the cumulative
// effect of every write the session has made to OriginalPath, merged into a
// single view against the pre-session snapshot.
type FileChange struct {
    OriginalPath     string         // absolute path; never empty
    Status           FileStatus     // Added | Modified | Deleted
    FirstBackupPath  string         // "" for files created in-session with no pre-session backup
    Undoable         bool           // false for bash-only changes (no backup to restore)
    UndoAllTCID      string         // tool_call_id of the FIRST snapshot; used for "undo all"
    ChangeCount      int            // number of distinct tool calls touching this file
    Authors          []ChangeAuthor // ordered: main agent first, then sub-agents
    CreatedAt        time.Time      // first change
    UpdatedAt        time.Time      // most recent change
}

type FileStatus int
const (
    FileAdded    FileStatus = iota // file did not exist pre-session; first snapshot had empty backup
    FileModified                   // file existed pre-session; backup != current
    FileDeleted                    // bash `rm` or delete-tool removed the file
)

type ChangeAuthor struct {
    AgentID   string // "main" or sub-agent id (e.g. "a1")
    AgentName string // human-readable (e.g. "build", "scout", "explore")
    Changes   int    // how many tool calls this author made on this file
}
```

### 4.2 Why `FirstBackupPath` is the only hard pointer

The registry stores only the path to the **pre-session** backup and the
**first tool call id**. Everything between the first snapshot and the most
recent write is reconstructed on demand by walking the relevant
`snapshot.Store` for `(OriginalPath, AgentID)` in chronological order. We do
NOT duplicate snapshot metadata into the registry — this keeps the registry
serialization cheap and avoids drift between two sources of truth.

### 4.3 Why `UndoAllTCID` is the FIRST tool call

`Store.UndoByToolCallID` is conflict-guarded: it refuses if a newer
still-active write exists, so it must always be called on the oldest write
first to succeed. The tab's "undo this file" action calls
`UndoByToolCallID(FirstToolCallID)` (per-agent) for each author that
touched the file, oldest-author-first.

### 4.4 `Registry`

```go
type Registry struct {
    mu       sync.Mutex
    files    map[string]*FileChange       // path → entry; lazily populated
    byAgent  map[string]*snapshot.Store   // agentID → store to subscribe to
    bashHook BashRecorder                 // nil if not wired
}

func NewRegistry() *Registry

// AttachSnapshotStore is called by Agent.New (and the task tool, when a
// sub-agent is spawned) so the registry knows to read from that store.
func (r *Registry) AttachSnapshotStore(agentID string, store *snapshot.Store) error

// DetachSnapshotStore is called when a sub-agent is torn down. Its writes
// remain visible (and undoable, as long as the underlying backup files
// exist on disk) — only the live subscription is removed.
func (r *Registry) DetachSnapshotStore(agentID string)

// NotifyBashWrite is called by the bash tool on every invocation. The
// registry's BashRecorder extracts the touched paths; the bash tool just
// hands over the command + working dir + the list of files the shell
// actually touched.
func (r *Registry) NotifyBashWrite(event BashWriteEvent) error

// List returns the current snapshot of FileChange, sorted by UpdatedAt desc.
func (r *Registry) List() []FileChange

// UndoFile restores path to its pre-session state. Calls UndoByToolCallID
// for each author, oldest first, so conflict guards are satisfied.
func (r *Registry) UndoFile(path string) error

// UndoBlock undoes a single tool call. Looks up the (path, toolCallID) pair
// in the relevant store and calls UndoByToolCallID.
func (r *Registry) UndoBlock(path, toolCallID string) error
```

### 4.5 Bash detection (v1)

The bash tool's post-exec hook hands the registry:

```go
type BashWriteEvent struct {
    Command  string
    WorkDir  string
    ExitCode int
    // Files the shell touched, detected by:
    //   1. Pre/post stat: snapshot the working dir before exec, re-stat
    //      after exec, diff. Skip .git/, node_modules/, .opencode/,
    //      <GlobalDataDir>/, build outputs.
    //   2. Conservative command scan: extract path-like tokens from the
    //      command and intersect with the diff set (so a comment that
    //      mentions a path doesn't count, but a real `cat > foo` that
    //      creates `foo` does).
    TouchedPaths []string // resolved absolute paths
    TouchedOps   []BashOp // added | modified | deleted
}

type BashOp int
const (
    BashAdded BashOp = iota
    BashModified
    BashDeleted
)
```

**Algorithm** (v1, conservative):

1. **Pre-exec stat** of the working dir tree (skip noise dirs).
2. Run the command.
3. **Post-exec stat** the same tree.
4. Diff the two snapshots → set of `created`, `modified`, `deleted` files.
5. Conservative command scan (paths after `>`, `>>`, `tee`, `sed -i`, `mv`,
   `cp`, `rm`, `mkdir -p`, `touch`, `cat <<EOF >`); intersect with the diff
   set.
6. For each path in the intersection, call `registry.NotifyBashWrite` with
   the inferred op.

**What v1 explicitly does NOT do**: install inotify/FSEvents watchers, parse
shell scripts, sandbox the command. Those are the right ideas for v2 if
users want fuller coverage.

### 4.6 Persistence

`Registry.MarshalJSON` / `UnmarshalJSON` round-trip the `List()`. On
re-hydration, the new `Agent` constructs a fresh `Registry`, calls
`AttachSnapshotStore` for itself, then the `session` package re-attaches
each sub-agent's store and replays the saved `FileChange` list as a hint for
the initial render — but every subsequent render goes back to live stores
as the source of truth. The saved list is allowed to be stale (e.g. a
sub-agent's store has been GC'd): the live store query is the truth.

**Implication**: a closed-and-reopened session that lost its sub-agent
stores will show fewer files than when it was saved — but the user's own
changes (the main agent's) survive, because the main agent's store is
rebuilt from disk on re-hydration by the existing `Agent.New` logic.

## 5. TUI surface

### 5.1 Tab bar

Current tabs: `chat | agents | files | git | log` (5 tabs).
New layout: `chat | agents | files | changes | git | log` (6 tabs).

- New constant `tabChanges = 4` in `internal/tui/tabs.go`.
- `tabCount = 6`; `tabGit` becomes 4, `tabLog` becomes 5.
- `renderTabBar` labels list: `["chat", "agents", "files", "changes", "git", "log"]`.
- Hotkey rotation is `% tabCount`-driven, so it auto-extends. Explicit
  arms are needed in `renderTabContent` and the per-tab Update switches.

### 5.2 Layout (changes tab)

```
┌──────────────────────────────────────────────────────────────────────┐
│ (top pad)                                                             │
│ ◆ ocode · 6:changes  chat  agents  files  changes  git  log    ✕ exit │  ← appHeaderHeight = 2
│ ╭──── changes (bordered) ───────╮ ╭──── preview (bordered) ─────────╮ │
│ │  A foo.go            (build)  │ │ diff foo.go (pre-session → now) │ │
│ │  M bar.py            (a1)     │ │                                  │ │
│ │  M baz.ts            (main)   │ │ - old line                        │ │
│ │  + new_file.md       (bash) ⚠ │ │ + new line                        │ │
│ │  …                             │ │ …                                │ │
│ │                                │ │                                  │ │
│ │  u: undo file   U: undo block  │ │                                  │ │
│ │  / : filter     enter: open    │ │                                  │ │
│ ╰────────────────────────────────╯ ╰──────────────────────────────────╯ │
│   [ ⟳ LLM  │ ⚙ edit, write … ]                                       │
│   [ LLM: ●●○ · Agent: build · … ]                                     │
└──────────────────────────────────────────────────────────────────────┘
```

Two-pane, same as the files tab:

- **Left pane** — file list, ~35% width. Each row: `[status icon] [path]   ([author · n edits])`. Status icons: `+` (added), `M` (modified), `-` (deleted). Bash-only entries get a trailing `⚠` and `(bash)` suffix; their "Undo File" key is a no-op with a status-bar message.
- **Right pane** — unified diff of the selected file, pre-session bytes vs current bytes. Lazy-loaded when a row is selected (matches the files tab's `previewLoadMessage` pattern). Sticky scroll.
- **Footer hints** rendered inside the left pane bottom: the active keybindings for the current selection.

### 5.3 Keybindings (changes tab only)

| Key | Action | Notes |
|-----|--------|-------|
| `j` / `k` or `↓` / `↑` | Move file list cursor | |
| `enter` | Open file in the right pane (select) | |
| `u` | **Undo entire file** (oldest-first across authors) | Confirmation prompt |
| `U` | **Undo one block** (the most recent tool call on the selected file) | Confirmation prompt |
| `/` | Filter (path fuzzy) | Same UX as the files tab |
| `r` | Refresh from the live registry | |
| `y` | Copy the file's diff to the clipboard | |
| `g` / `G` | Jump to top / bottom of file list | |
| `J` / `K` (shift) or `PgDn` / `PgUp` | Scroll the preview pane | |
| `esc` | Clear filter, or leave the tab | |
| `tab` | Move focus between file list and preview | |
| `1`…`6` | Jump to tab n | Already a convention; auto-extends |

Both `u` and `U` show a confirm dialog: `undo foo.go to pre-session state?
[y/N]`. The dialog reuses the existing confirm-dialog component
(`internal/tui/component_dialog.go`).

### 5.4 Empty state

When `Registry.List()` is empty:
- Left pane: a centered hint: `no changes in this session yet.`
- Right pane: blank, with a faint hint: `files the agent edits will appear here.`
- A second-line note: `changes are not git-tracked and persist with this session.`

### 5.5 Visual style

Reuse the existing `Styles` palette:

- `+` (added) → `successStyle.Render("+")`
- `M` (modified) → `headerStyle.Render("M")` (or a new single-char style)
- `-` (deleted) → `errorStyle.Render("-")`
- `⚠` on bash entries → `hintStyle.Render("⚠")` (single-width; per AGENTS.md
  TUI rules, no wide emoji as inline status prefixes)
- Selected row → `selectedStyle.Render(...)` (already used by every list)
- Authors column → `dimStyle` for sub-agents, default for main

This matches the chrome in the files and git tabs, so the changes tab will
feel native.

### 5.6 Mouse support

Per the AGENTS.md TUI Mouse pattern: mouse capture is on, so we implement
selection in-app. The file list supports click-to-select and click-and-drag
for multi-select (matches the files tab). The right preview pane supports
click-and-drag to select text (reuses `selectionState` +
`applySelectionHighlight` from `internal/tui/selection.go`). Undo is
keybind-only in v1 — no clickable undo button.

### 5.7 ListBox adoption

The file list uses the existing `listbox.NewListBox` component (see
`internal/tui/component_listbox.go`, plan in `PLAN-listbox-component.md`).
This is the natural third adopter: it gives us correct mouse hit-test
geometry for free, prevents the files-tab narrow-screen hit-box bug class,
and shares its in-app selection recipe with the rest of the TUI.

## 6. Files

### 6.1 New files

| File | Purpose |
|------|---------|
| `internal/changes/changes.go` | Package doc, `FileChange`, `FileStatus`, `ChangeAuthor` types, `Registry` type, `NewRegistry`, `AttachSnapshotStore`, `DetachSnapshotStore`, `NotifyBashWrite`, `List`, `UndoFile`, `UndoBlock` |
| `internal/changes/diff.go` | `RenderDiff(path, backupPath, currentPath string) (string, error)` — produces the unified diff for the preview pane, reusing the existing diff infrastructure the git tab uses |
| `internal/changes/bash.go` | `BashRecorder` interface + the pre/post stat implementation behind it |
| `internal/changes/changes_test.go` | Table-driven tests: registry attaches, sub-agent detached, undo oldest-first, conflict guard, bash-only path, persistence round-trip |
| `internal/changes/diff_test.go` | Diff rendering golden tests against a small fixed fixture |
| `internal/tui/changes_model.go` | `changesModel` — the new TUI sub-model. Uses `listbox.NewListBox` for the file list, bubbles `viewport` for the preview, the existing confirm dialog for undo |
| `internal/tui/changes_model_test.go` | Update/View/key tests using the `derefTestModel` pattern from `slash_popup_test.go:308-321` |
| `internal/tui/changes_kbd.go` (or inline) | Keybinding table for the changes tab |
| `docs/changes-tab.md` | Concept doc — how the changes tab works, what it tracks, what it doesn't, and how undo interacts with the snapshot store |
| `PLAN-changes-tab.md` | Implementation plan (companion to this spec, follows the same phased structure as `PLAN-files-tab-search.md` and `PLAN-live-preview.md`) |

### 6.2 Modified files

| File | Change |
|------|--------|
| `internal/tui/tabs.go` | Add `tabChanges = 4`, increment `tabCount = 6`, append `"changes"` to the labels list (between `files` and `git`) |
| `internal/tui/model.go` | `m.changes = NewChangesModel(m.agent, …)` in `newModel`; `case tabChanges: …` arms in `renderTabContent` and the per-tab Update switches; hotkey-rotation arithmetic auto-extends because it's `% tabCount`; the chrome-gating early-return checks add a `tabChanges` arm where they currently check `tabGit`/`tabFiles` |
| `internal/agent/agent.go` | Construct the `*changes.Registry` in `Agent.New`, call `AttachSnapshotStore("main", a.snapshotStore)`. Expose `a.changes() *changes.Registry`. Wire the bash tool to call `registry.NotifyBashWrite` on every invocation via the existing `a.toolCtx` or a new hook on the tool runner |
| `internal/tool/bash.go` | Add a post-exec hook that captures `WorkDir`, runs pre/post stat via `changes.NewBashRecorder(workDir)`, and forwards the resulting `BashWriteEvent` to the agent's registry. Pre/post stat are cheap (mtime+size filter, then sha256 only on changed mtime) |
| `internal/agent/subagent.go` (the `task` tool) | When a sub-agent is created, call `parentAgent.changes().AttachSnapshotStore(subID, subAgent.SnapshotStore())`; on teardown, call `DetachSnapshotStore(subID)` |
| `internal/session/session.go` | On save, marshal `agent.changes().List()` into the session JSON. On load, after constructing the new agent, replay the saved list as a hint and call `AttachSnapshotStore` for each known sub-agent |
| `docs/file-edit-snapshot.md` | Add a "see also" reference to `docs/changes-tab.md` |
| `docs/index.md` (knowledge bundle) | Add the new spec + `docs/changes-tab.md` concept doc |
| `AGENTS.md` | Add a one-line cross-reference in the TUI section pointing to the new tab and the new concept doc |

## 7. Test strategy

- **Unit tests for `internal/changes`**: registry attach/detach, undo
  oldest-first, undo conflict refusal, bash-only path (no backup →
  `Undoable: false`), persistence round-trip via `MarshalJSON` /
  `UnmarshalJSON`.
- **TUI tests for `internal/tui/changes_model.go`**: arrow key navigation,
  `u` triggers confirm dialog, `U` triggers confirm dialog on the most
  recent snapshot, `esc` clears filter, `enter` selects. Use the existing
  test scaffold (`newTestTextarea`, `derefTestModel`).
- **Integration test**: spin up a fake `snapshot.Store`, attach it to a
  registry, run a `BashRecorder` against a temp dir with `cp foo bar`,
  verify the registry reports `bar` as added with `Undoable: false`.
- **Manual smoke**: load a session, edit a file via `edit`, open the
  changes tab, see the file, hit `u`, confirm the file is restored.
- **Layout test**: borrow `TestActivityRowGrowthStaysWithinHeight` pattern
  from `internal/tui/overflow_repro_test.go` to ensure the new 2-pane
  changes tab doesn't blow the 13-row safety net.

## 8. Phasing (for the plan doc)

1. **Phase 0** — `internal/changes` package skeleton + types.
2. **Phase 1** — `Registry` with `AttachSnapshotStore` + `List` reading
   from one store.
3. **Phase 2** — Multi-agent aggregation (`AttachSnapshotStore` for
   sub-agents).
4. **Phase 3** — Undo (`UndoFile` oldest-first; `UndoBlock`
   latest-call).
5. **Phase 4** — Bash detection (pre/post stat + `NotifyBashWrite`).
6. **Phase 5** — TUI surface — `tabChanges`, `changesModel`, View,
   Update, keybindings, dialog.
7. **Phase 6** — ListBox adoption (swap the hand-rolled file list for
   `listbox.NewListBox`).
8. **Phase 7** — Diff rendering in the right pane.
9. **Phase 8** — Session persistence (save/load round-trip).
10. **Phase 9** — Wire into the bash tool post-exec hook and the `task`
    sub-agent lifecycle.
11. **Phase 10** — Docs — `docs/changes-tab.md` concept + index update +
    AGENTS.md cross-link.

This phasing is sequenced so each phase is independently buildable and
testable. The 5 validation gates (`go build`, `go vet`, `go test`, manual
smoke, layout test) apply at every phase.

## 9. Out of scope (deferred)

- **Web SPA exposure.** A `/api/changes` endpoint and a web equivalent of
  the tab. The package API is shaped to allow this without breaking
  changes.
- **Per-hunk undo UI in the tab.** Fine-grained per-tool-call undo remains
  available through the existing `undo_file_change` LLM tool.
- **Stash / re-do from the tab.** Already covered by the existing TUI
  `u`/`r` shortcuts and the snapshot store's redo stack.
- **Renames / moves detection.** Not in v1.
- **Inotify/FSEvents watcher for non-snapshot changes.** Bash detection
  is the v1 approximation; watcher is the v2 upgrade if it proves
  insufficient.
- **Cross-session diffing.** "Show me all files I changed across every
  session today" is a follow-up.

## 10. See also

- `docs/file-edit-snapshot.md` — the existing snapshot mechanism the new
  tab builds on.
- `internal/snapshot/snapshot.go` — the source of truth for "what
  changed."
- `internal/tui/tabs.go` — the tab bar the new tab slots into.
- `internal/tui/files_model.go` — the closest existing TUI surface
  (two-pane list + preview); the changes tab follows the same pattern.
- `internal/tui/component_listbox.go` — the ListBox component the file
  list adopts.
- `PLAN-listbox-component.md` — the ListBox plan, whose drop-in goal this
  design exercises.
- `PLAN-live-preview.md` and `docs/superpowers/specs/2026-07-11-live-preview-design.md`
  — the most recent spec/plan pair, mirrored in structure here.
