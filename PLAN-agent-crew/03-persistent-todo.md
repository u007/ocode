# 03 — Persistent todo (durable markdown plan file, Manus-style)

## How it currently works

`TodoWriteTool` and `TodoReadTool` live in `internal/tool/patch.go`. State is a
process-global `map[string]string` (`todoStates`) guarded by a `sync.RWMutex`,
keyed by a single global `currentTodoSession` set via `tool.SetTodoSession`,
which `internal/tui/model.go` calls on session start and session switch.

`todowrite` takes one argument, `todoText`, and **replaces the entire list**.
`todoread` returns the whole blob. `tool.TodoState()` is read by the TUI to
render the sidebar checklist.

Consequences:

- The list dies with the process. Resume a session and the plan is gone.
- It is not a file: the user cannot see it, edit it, or diff it.
- It is never re-injected into the model's context. After a compaction, the
  model's only memory of its own plan is whatever survived the summary.
- `currentTodoSession` is one global. Two sessions in one process share the
  cursor; a subagent writing while the main agent writes is a last-write-wins
  race with no detection.

## Where it breaks

The Manus pattern this is imitating works for one reason: **the todo is a file
that is re-read into the context window**, so a hundred-step run keeps
re-anchoring on its own plan instead of drifting. ocode has the tool surface but
not the mechanism — the list is a UI widget, not a control structure. On long
runs the model loses the plan at the first compaction and starts improvising.

Full-blob replacement is also the maximum-damage write shape: one confused
`todowrite` call silently erases every completed item with no trace.

## Storage decision: markdown file, not SQLite

**Decision: one markdown file per session, canonical, no database.**

Reasons, in order of weight:

1. **The file must go into the context window verbatim.** That is the entire
   mechanism. SQLite would require a render step on every injection, which means
   two representations of the same state and a rendering bug class between them.
   Markdown-canonical has one source of truth.
2. **The concurrency scale does not justify a database.** After the write-scope
   rule below, there is exactly one in-process writer. The remaining contention
   is cross-process (a second ocode instance on the same project, or a resumed
   session) — which is what advisory file locking is for, and the project
   already has a working flock helper with a bounded retry loop
   (`internal/knowledge/lock.go` + `lock_unix.go` / `lock_windows.go`).
3. **A SQLite dependency for one list per session is unjustified scope.** It
   brings a driver dependency, a schema, and migrations, to solve a problem
   that a lock and an atomic rename already solve.

It is also user-editable and git-diffable, which a database is not.

### Layout

- `.ocode/todo/<session-id>.md` — one file per session, project-local.
  `.ocode/` is the established home for tool- and agent-owned per-project state
  (`settings.json`, `uploads/`, `md-summaries.json`); `.opencode/` is
  user-authored config (rules, skills, plugins) plus the legacy snapshot dir,
  so it is the wrong neighbourhood for generated state. Note that `plan.go`'s
  `planDir()` writes to `.opencode/plans` — that is the older convention and
  should not be copied here.
- Add `.ocode/todo/` to `.gitignore` — this is developer-local state, not
  committed content. `.gitignore` currently lists only `.ocode/md-summaries.json`,
  not the `.ocode/` directory, so this is a real entry and not a no-op.
- The TUI file walkers (`internal/tui/model.go`, `slash_popup.go`) skip
  `.ocode/` outright, so the file will not surface in the file picker. That is
  acceptable — the sidebar renders it — but do not claim file-picker visibility
  as a benefit.
- A strict line grammar: one item per line, a status marker (`[ ]`, `[•]`,
  `[✓]`), a stable short item id, and the item text. The id is what makes
  targeted updates possible.
- A short header carrying a monotonically increasing **revision** number.

## Preventing concurrent damage

### 1. Only the main agent writes (this is the load-bearing rule)

Subagents get `todoread` only; `todowrite`/`todo_update` are removed from the
subagent tool set. A child reports its outcome to its dispatcher, and the
dispatcher — which owns the plan — records it.

This eliminates in-process concurrent writers outright. Everything below is
defense against the remaining cases: a second ocode instance, a resumed session,
and the main agent itself writing badly.

### 2. Targeted operations, not blob replacement

Add a `todo_update` tool that mutates *by item id*: set status, edit text,
append item, insert after. `todowrite` (full replace) survives only for creating
or deliberately rewriting the list, and gains the guard in point 4.

A model that only wants to tick one box can no longer accidentally rewrite the
whole plan. `todo_update` reports `Parallel() == false`, matching `todowrite` —
two mutations must never land in the same parallel batch. `todoread` stays
parallel-safe.

### 3. Optimistic concurrency via a revision token

`todoread` returns the current revision along with the content. Every mutation
must cite the revision it was based on. A stale revision is **rejected** with
the current content returned in the error, so the model re-reads and retries
against fresh state. This catches the cross-instance and resume cases that the
lock alone cannot (the lock serializes writes; it does not stop a write based on
stale reads).

### 4. Reject destructive full replacements

A `todowrite` that would drop items — particularly completed ones — is rejected
with a message telling the model to use `todo_update` for status changes. There
is **no override flag**: the rejection is unconditional, and the rejection text
is the fix. (An `allow_destructive`-style parameter would be a behavior-changing
optional flag, which project rules forbid, and would be set by exactly the
confused model the guard exists to stop.)

### 5. Strict parse, never silent reset

If the file fails to parse, **refuse writes** and surface the parse error with
the file path. Do not truncate, do not reset to empty, do not "recover" by
overwriting. The last-good file stays on disk for the user to fix or revert.

### 6. Durable writes

Every mutation: acquire the lock, re-read, verify revision, apply, write to a
temp file in the same directory, `fsync`, atomic rename, release. A crash
mid-write leaves either the old file or the new one, never a half-written one.

### 7. Undo already exists

Do not build a history directory. Todo-file writes go through the normal write
path, so `internal/snapshot.Store` captures them and `undo_file_change` (the
documented undo mechanism in `AGENTS.md`) restores them. Confirm the todo path
is actually covered by the snapshot store; if it is not, route it so it is.

## Re-anchoring into context

This is the point of the whole change.

- After a compaction, and on session resume, the current todo file content is
  re-injected into the turn.
- **It goes into the user-role volatile tail** — the `injectDiscoveryContext`
  pattern — wrapped in an `[ocode:todo]` marker so the model reads it as
  system-origin. It must **not** be a `system`-role message:
  `collectAndRemoveSystemMessages` hoists every system-role message, including
  tail ones, into the cached system block, so per-turn-varying todo content
  there would rewrite the entire cached prefix on every turn.
- Inject only when the list is non-empty and has open items. A finished or
  absent list injects nothing.

## Migration from the current implementation

### The file is durable state; memory stays the read path

`tool.TodoState()` is called from `renderSidebar` (`internal/tui/model.go`),
which is a **render path — it runs on every frame**. It must never touch the
disk. The store therefore keeps an in-memory copy per session that all reads
serve from; the file is the durable record, written on mutation and read on
session load/resume only.

This is a cache, not a second source of truth: the memory copy is only ever
populated by (a) loading the file, or (b) a mutation that just succeeded in
writing the file. There is no path that updates memory without updating disk.
If a write fails, the memory copy is not advanced and the error is returned.

### Signatures and lifecycle

- `tool.TodoState()` keeps its signature; serves from the in-memory copy.
- `tool.SetTodoSession` keeps its signature but now resolves the session's file
  path and loads it. Drop the process-global `currentTodoSession`/`todoStates`
  pair in favour of per-session state so two sessions in one process cannot
  collide.
- `tool.ResetTodoState()` (called by `/new`, `/clear`, and session switch)
  **clears the in-memory copy only — it must not delete the file.** Those call
  sites move to a *different* session id, and deleting the outgoing session's
  todo would destroy exactly the state this change exists to preserve.
- Filename is the session id verbatim. `NewSessionID()` produces
  `ses_YYYY-MM-DD-HHMMSS` — filesystem-safe, and its one-second granularity
  means two sessions created in the same second share an id. That collision
  already exists at the session-storage layer; this introduces no new failure
  mode and no separate mitigation.
- On first `todowrite` in a session with no file, create it. No migration of
  in-memory state is needed — none survives a restart today.

## Constraints and non-goals

- **Cache:** adding `todo_update` and a revision field to `todoread`'s result is
  a one-time tools-array change. Todo content never enters a tool description.
- **Not a project-wide task tracker.** Per session, not per repo. `TODO.md`
  remains the human backlog and is untouched by this.
- **No sync, no server, no cross-machine state.**

## Files touched

| File | Change |
|------|--------|
| `internal/tool/patch.go` | replace `todoStates` map with the file-backed store; add `todo_update`; revision on `todoread`; destructive-replace guard |
| `internal/tool/todo_store.go` (new) | path resolution, strict parse/serialize, lock + atomic write, revision handling |
| `internal/filelock/` (new) or shared helper | extract the flock helper — `knowledge.WithBundleLock` hardcodes `.okf.lock` in the bundle root and is not reusable as-is; generalize to `WithFileLock(lockPath, fn)` and have `WithBundleLock` delegate to it (DRY, no duplicate lock implementation) |
| `internal/agent/subagent.go` | remove write-class todo tools from subagent tool sets |
| `internal/agent/compact.go` + tail injection path | re-anchor the todo into the user-role volatile tail after compaction |
| `internal/tui/model.go` | `SetTodoSession` call sites; sidebar reads through the store |
| `.gitignore` / `watcher.ignore` | ignore `.ocode/todo/` |
| `AGENTS.md` | document storage location, the main-agent-only write rule, the revision protocol, and the user-role injection constraint |

## Tasks

1. Extract the flock helper into a shared package and re-point
   `knowledge.WithBundleLock` at it → verify: existing knowledge lock tests pass
   unchanged; new test asserts two goroutines contending on an arbitrary lock
   path serialize and the loser times out rather than proceeding.
2. Implement the todo store: strict parse, serialize, revision, lock + temp +
   fsync + rename → verify: round-trip test; malformed-file test asserts a write
   is refused and the file is untouched; concurrent-write test asserts no
   interleaved/partial file.
3. Repoint `todowrite`/`todoread`/`TodoState`/`SetTodoSession`/`ResetTodoState`
   at the store, removing the process globals → verify: existing TUI tests that
   call `SetTodoSession` pass; new test asserts two sessions in one process keep
   separate lists; test asserts `TodoState()` performs **no disk I/O** (it is on
   the render path); test asserts `ResetTodoState()` leaves the file on disk and
   the list reloads intact when that session is resumed.
4. Add `todo_update` (by item id) and the revision requirement on mutations
   → verify: stale-revision write is rejected and the error carries current
   content; targeted status change leaves all other items byte-identical.
5. Add the destructive-full-replace guard → verify: a `todowrite` dropping a
   completed item is rejected with the use-`todo_update` message; a legitimate
   first-write and a genuine append are accepted.
6. Remove write-class todo tools from subagent tool sets → verify: test asserts
   a dispatched subagent's tool list contains `todoread` and not `todowrite` /
   `todo_update`.
7. Re-inject the todo into the user-role volatile tail after compaction and on
   resume → verify: test asserts the injected block is user-role with the
   `[ocode:todo]` marker, that no system-role message is added, and that an
   empty/complete list injects nothing.
8. Confirm todo writes are captured by the snapshot store so `undo_file_change`
   works on them → verify: write, undo, assert prior content restored.
9. Ignore `.ocode/todo/`, and document the whole mechanism in `AGENTS.md`
   → verify: re-read the section against shipped behavior.
