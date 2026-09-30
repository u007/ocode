---
type: Concept
title: Persistent Todo Plan (todowrite / todoread / todo_update)
description: Per-session durable todo plan file format, revision-token concurrency, destructive-replacement guard, cache-safe user-role re-anchor injection, and resume semantics.
resource: CLAUDE.md
tags:
  - todo
  - agent
  - concurrency
  - prompt-cache
timestamp: 2026-09-30T07:37:09Z
---
# Persistent Todo Plan (todowrite / todoread / todo_update)

A per-session todo plan lives at `.ocode/todo/<session-id>.md` — the durable,
user-visible, git-diffable record the model uses to keep its own plan in scope
across long runs and post-compaction turns. The store lives in
`internal/tool/todo_store.go`; the re-anchor injection lives in
`internal/agent/todo_inject.go`.

- **File format** (strict):
  ```
  # Todo (revision 3)

  - [ ] t1 First item
  - [•] t2 Second item
  - [✓] t3 Third item
  ```
  Every item carries a stable `tN` id so targeted updates can land without a
  full rewrite. The revision is the file's optimistic-concurrency token.
- **In-memory copy is a cache, not a second source of truth.** `TodoState()`
  is on the TUI render path and must never touch the disk. The memory copy is
  populated only by (a) loading the file on `SetTodoSession`, or (b) a
  mutation that just succeeded in writing the file. If a write fails, the
  memory copy is not advanced and the error is returned.
- **The main agent is the only writer.** Subagents have `todoread` only
  (`filterMainOnlyTools` in `internal/agent/subagent.go` strips `todowrite`
  and `todo_update` from the subagent tool set). A child reports its outcome
  to its dispatcher; the dispatcher — which owns the plan — records it. This
  is the load-bearing rule that prevents in-process concurrent writers.
- **Mutations are serialized** by an advisory flock
  (`internal/filelock.WithFileLock(todoLockPath(), fn)`) so a second ocode
  instance, a resumed session, or the same agent writing twice cannot
  interleave. The cross-process lock catches contention; the revision
  protocol below catches stale reads.
  **The lock must span the whole read → verify revision → apply → write
  sequence, not just the read.** A lock released between the read and the
  write does not prevent two processes from both observing revision N, both
  passing the staleness check, and both writing N+1 — the second rename
  silently discards the first's items. The in-process mutex does not help;
  cross-process is the only case this lock exists for.
- **Optimistic concurrency via a revision token.** `todoread` returns
  `revision: N`. Every mutation (`todowrite`, `todo_update`) must cite the
  revision it was based on. A stale citation is rejected with the current
  content so the model re-reads and retries. This catches the cross-instance
  and resume cases that the lock alone cannot: the lock serializes writes,
  it does not stop a write based on stale reads.
- **Targeted updates via `todo_update`** (`set_status`, `edit_text`,
  `append`, `insert_after` by item id). A model that only wants to tick one
  box can no longer accidentally rewrite the whole plan. `todowrite` (full
  replace) survives only for creating or deliberately rewriting the list.
- **Destructive full replacements are rejected.** A `todowrite` that drops an
  existing item (particularly a completed one) is refused with a message
  pointing the model at `todo_update`. There is **no override flag** — the
  rejection is unconditional, and the rejection text is the fix.
  **Ids must be assigned *after* the guard runs** (`guardNoDroppedItems`, then
  `inheritTodoIDs`, then `assignTodoIDs`). Stamping positional `t1..tN` on the
  incoming items first makes the guard vacuous — every old id appears to
  survive because the new items were just handed those same ids — so a
  same-length replacement of unrelated items silently destroys completed work.
  `inheritTodoIDs` then lets each id-less new item adopt the id of the existing
  item with the same text, so a legal reorder keeps `t1` pointing at the same
  item and a later `todo_update` cannot mutate the wrong one.
- **Strict parse, never silent reset.** If the file fails to parse, writes
  are refused and the parse error (with the file path) is surfaced. The
  last-good file stays on disk for the user to fix or revert. `TodoState()`
  renders the parse error rather than returning `""` — otherwise the sidebar
  prints "No todo list yet" over a corrupt file, making the render surface the
  one place the failure is silent.
- **Both content shapes parse** (`parseTodoContent`): the canonical
  header+ids form, and legacy headerless raw text, which `restoreTodoState`
  feeds through `SetTodoState` for sessions predating the file store.
  Demanding the header would make `todoread` hard-fail on every call for any
  resumed pre-change session, and would make `baseItems` return nil — skipping
  the destructive guard, so the first `todowrite` would wipe the restored list.
- **Durable writes** (every mutation): lock → re-read → verify revision →
  apply → write to a temp file in the same directory → `fsync` → atomic
  rename → release. A crash mid-write leaves either the old file or the new
  one, never a half-written one.
- **Snapshot capture.** `TodoWriteTool` / `TodoUpdateTool` implement
  `ContextualTool` so the per-agent snapshot store sees the write; on a
  successful write the file is also `Backup`/`RegisterWrite`-ed, so
  `undo_file_change` can restore it by `tool_call_id`.
- **Re-anchor injection is user-role, not system-role** (`injectTodoTail` in
  `internal/agent/todo_inject.go`). The plan is injected on every Step at
  the very tail of the messages slice, after the discovery tail. It is
  wrapped in an `[ocode:todo]` marker so the model reads it as system-origin
  even though it is `role: "user"`. **It must not be `role: "system"`**:
  `collectAndRemoveSystemMessages` hoists every system-role message
  (including tail ones) into the cached `system` field, so a system-role
  block that grows with the plan would rewrite and bust the cached system
  prompt on every turn. The user-role tail rides the uncached suffix and
  coalesces with the current user turn — exactly what we want.
- **Inject only when the list is non-empty and has at least one open item.**
  A finished or absent list injects nothing. This is the cache-stability
  invariant: a no-op turn keeps the messages slice byte-identical so the
  cached prefix survives.
- **Session resume and `/new` semantics.** `SetTodoSession` reloads the file
  from disk (so resume survives a process restart). `ResetTodoState` (called
  by `/new` and `/clear`) clears the in-memory copy **only** — it must not
  delete the file, because the call sites move to a *different* session id,
  and deleting the outgoing session's plan would destroy exactly the state
  this mechanism exists to preserve.
