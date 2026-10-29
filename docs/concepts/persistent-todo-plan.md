---
type: Concept
title: Persistent Todo Plan (todowrite / todoread / todo_update)
description: Per-session durable todo plan file format (with deps, timestamps and the task-tool run link), revision-token concurrency, destructive-replacement guard, cache-safe user-role re-anchor injection, and resume semantics.
resource: CLAUDE.md
tags:
  - todo
  - agent
  - concurrency
  - prompt-cache
timestamp: 2026-10-28T12:00:00Z
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
- **Item detail (continuation lines).** An item may be followed by indented
  lines, two spaces + `key: value`, each key at most once, always serialized in
  this order (`internal/tool/todo_meta.go`):
  ```
  - [✓] t3 Third item
    deps: t1, t2
    started: 2026-10-28T20:41:02+08:00
    finished: 2026-10-28T20:44:10+08:00
    run: agent-run-4
    result: Added the parser and 6 tests; go test ./internal/tool passes.
  ```
  `deps` = comma-separated item ids; `started`/`finished` = RFC3339 (kept as the
  validated string so a round trip is byte-identical); `run` = the agent run id;
  `result` = one line. Unknown keys, malformed values, duplicate keys and a
  detail line before the first item are parse errors (strict parse). Files with
  no detail lines are the legacy grammar and parse unchanged. The parser lives in
  `parseTodoItems`, so the legacy-metadata path (`SetTodoState`) accepts detail
  lines too. The TUI sidebar shows item lines only (`TodoState` drops detail
  lines); `todoread` shows everything; the re-anchor shows item lines plus a
  `blocked by …` note under an open item whose deps are open.
- **Dependencies.** Checked on every write (`validateTodoDeps`): each dep must
  exist, no self-reference, no cycle; the error names the ids. `set_status`
  `in_progress` or `done` is refused while a dep is not done (`t3 is blocked by
  t1, t2`); `pending` is always allowed. A `todowrite` applies the same rule to
  every item whose status it changes (`applyRewriteTransitions`), so a rewrite is
  not a back door; an unchanged item is never re-checked. In a `todowrite`, deps
  may only name ids already in the list (echoed or inherited by text): ids for
  new items are assigned afterwards, so a dep on one would bind to whichever item
  receives that id. Order is guard, `inheritTodoIDs`, `mergeTodoDetail`,
  `checkRewriteDeps`, `assignTodoIDs`, then `validateTodoDeps`.
- **Timestamps** are stamped by the store with `todoNow` (replaceable in tests):
  `started` on the first transition to `in_progress`, `finished` on the first
  transition to `done`. They are stamped once and never cleared, including by a
  status change back to pending.
- **`todo_update` ops** beyond the originals: `set_deps` (`item_id` + `deps`;
  `[]` clears, an absent `deps` is an error) and `set_result` (`item_id` + one
  line of `text`). `set_status` takes an optional `result`. All still cite the
  revision. A rewrite that echoes an item's id keeps its detail fields unless the
  rewrite sets them (`mergeTodoDetail`).
- **`task` tool link (`todo_item`).** `task` takes an optional `todo_item` (a `tN`
  id). Before anything is spawned `todoLinkPreflight` requires a run registry, a
  main-agent dispatcher (`Agent.isSubAgent` is false) and an existing, unblocked
  item. Once the run exists `linkRunToTodo` marks the item `in_progress`, records
  the run id and stamps `started` (`tool.LinkTodoItemToRun`). When the run ends,
  every exit of `runSyncDispatch` and `runBackgroundDispatch` (so also the
  wait-timeout and resume paths) calls `recordTodoOutcome` before the run's
  `Done()` closes: the first line of the child's result, capped at 300 runes
  with `…`, is written to `result`; success also sets `done` and stamps
  `finished`; failure or cancellation leaves `in_progress` with a `FAILED: ` /
  `CANCELLED: ` prefix and the main agent decides what next. A resume re-links
  the run's item first. A write-back failure is logged and appended to the task
  result as a note; it never replaces the child's output.
- **These are system mutations, not model writes** (`systemMutateTodo`): same
  flock and read → apply → validate → write sequence (`mutateTodo`, shared with
  `todo_update`), but no cited revision, because the harness is recording what
  it did rather than acting on a read of the plan. The main agent stays the only
  writer: `TaskTool` refuses `todo_item` when its dispatcher is a sub-agent, and
  a finish for a run the item is no longer linked to is refused. Each linked
  dispatch bumps the revision, so the model's next write may be rejected as stale
  and must `todoread` first; the task result names the new revision.
- **Every plan is addressed by a `TodoScope` (project root + session id),
  never by a process-global "current" session.** The agent builds the scope
  from its own `WorkDir` (enclosing project root via `tool.TodoProjectRootFor`;
  `""` = process cwd) and its conversation id (`Agent.OpenCodeSessionID`,
  which is the server's `SetSessionID` or the TUI's `SetOpenCodeSessionID`),
  attaches it to every tool call's context with `tool.WithTodoScope` (next to
  the snapshot store in `executeToolCallWithContext`), and uses the same
  `a.todoScope()` for the re-anchor (`injectTodoTail`) and the auto-continue
  `next` gate (`autoContinueTodoOpen`). `todowrite`/`todoread`/`todo_update`
  refuse to run without a scope in their context — there is deliberately no
  fallback to the sidebar's current session. Why: a web/desktop server hosts
  many sessions across many projects in one process and never chdir's per
  session; the old cwd + global-cursor store made every todo tool fail with
  "todo session not set" there, while the server's own readers
  (`publishTodoUpdated`, the Pulse board) were already addressing files by
  `(ProjectRoot, SessionID)`. Sub-agents inherit the parent's id
  (`SetSessionID`, or `SetOpenCodeSessionID` for a TUI parent) so their
  `todoread` sees the dispatcher's plan.
- **In-memory copy is a cache, not a second source of truth.** `TodoState()`
  is on the TUI render path and must never touch the disk. The memory copy is
  keyed by session id and populated only by (a) loading the file on
  `SetTodoSession` (the TUI sidebar's cursor), or (b) a mutation that just
  succeeded in writing the file. If a write fails, the memory copy is not
  advanced and the error is returned. `todoread` and `ReadTodoSnapshot` read
  the file itself (then the legacy memory-only copy when no file exists): a
  server session resumed in a fresh process has nothing in memory until its
  first mutation, and must still see its plan.
- **The main agent is the only writer.** Subagents have `todoread` only
  (`filterMainOnlyTools` in `internal/agent/subagent.go` strips `todowrite`
  and `todo_update` from the subagent tool set). A child reports its outcome
  to its dispatcher; the dispatcher — which owns the plan — records it. This
  is the load-bearing rule that prevents in-process concurrent writers.
- **Mutations are serialized** by an advisory flock
  (`internal/filelock.WithFileLock(todoLockPath(root), fn)`) so a second ocode
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
- **Tail directive (status nudge).** Only plan owners get it: `injectTodoTail`
  returns early unless `a.PlanPromptEnabled()` is true (sub-agents and ask/advisor
  helpers opt out and have `todo_update` stripped). For a primary agent with open
  items it appends one `Plan status:` line after the list and `Blocked:` line,
  computed by `todoStatusDirective` (`internal/agent/todo_inject.go:74`):
  - **No item in_progress** (and a ready item): `Plan status: no plan item is
    in_progress. If you are continuing this plan, before your next tool call run
    todo_update {"revision": N, "op": "set_status", "item_id": "tN",
    "status": "in_progress"} (run todoread first only if the plan changed since
    this snapshot). The next ready item is tN: <text>. Never batch status
    updates.` The first item NOT in `TodoSnapshot.BlockedIDs`
    (`internal/tool/todo_store.go:1084`, populated from the same `openTodoDeps`
    pass as the display strings) is recommended. Its text is collapsed to one
    line (`oneLineTodoText`, `internal/agent/todo_inject.go:117`, max 120 runes).
  - **An in-progress, unblocked item**: `Plan status: tN is in_progress. If you
    are working this plan, the moment the item is verified run todo_update
    {... "status": "done", "result": "<one-line outcome>"}, then cite the
    revision that call returns when you mark the next ready item in_progress.`
    (Multiple in-progress items are named together.)
  - **An in-progress but blocked item** (`set_deps` does not check deps, while a
    `done` transition does): names the `set_deps` recovery instead of a call the
    store would reject.
  The printed revision is the snapshot's and is valid for the FIRST mutation
  only — the next call must cite the revision the previous `todo_update`
  returned. The directive is user-role and derived only from the snapshot, so it
  is byte-identical for an unchanged plan; it is emitted on every Step while any
  item is open, and its "If you are continuing this plan" qualifier keeps a
  question-only turn from being pushed into plan work. Rationale: observed live
  sessions created a plan once and never called `todo_update`, leaving every
  item pending. The nudge names the exact next call; it complements the static
  `[ocode:plan]` fragment.
- **Session resume and `/new` semantics.** `SetTodoSession` reloads the file
  from disk into the sidebar copy (so the TUI sidebar survives a process
  restart; the tools and re-anchor read the file directly through their scope
  and need no load step). `ResetTodoState` (called by `/new` and `/clear`)
  clears the in-memory copy **only** — it must not delete the file, because
  the call sites move to a *different* session id, and deleting the outgoing
  session's plan would destroy exactly the state this mechanism exists to
  preserve.
- **`/reset-id` moves the plan on both hosts.** `tool.RekeyTodoSession(root,
  oldID, newID)` renames `<root>/.ocode/todo/<oldID>.md`; the TUI passes
  `TodoProjectRootFor(m.workDir)`, the server's `HandleResetSessionID` passes
  the session's project root. A plan left under the deleted id would read as
  "No todo list found" on the next turn.

## Live updates and surfaces

Plan writes push, not tool names. Every successful plan file write (`todowrite`,
`todo_update`, and the `task` link write-back `LinkTodoItemToRun` /
`FinishTodoItemRun`) calls the hooks registered with `tool.OnTodoWritten`, after
the file lock and `todoStoreState.mu` are released. The web server registers one
hook in `NewHandler` that dispatches `publishTodoUpdated` through `crashguard.Go`.
Pulse rows and the `todo_updated` event carry per-item `id`, `deps`, `blocked`
(derived: not done and a dep not done), `started`, `finished`, `run_id` and
`result`, each omitted when empty. The Pulse card and focus pane show one muted
line per item: "blocked by …", "running as <run_id>", or the result. The TUI
sidebar reads the in-memory copy that every write already updates; it marks a
blocked open item with `(blocked) ` after its id, never shows results, and clamps
each row to one line.

**Chat surfaces (web/desktop).** `PlanPanel` (`web/src/components/Chat/`) sits
above the agent rail. It loads from `GET /api/sessions/{id}/todo`, which returns
the `todo_updated` payload from the shared `readTodoUpdatedEvent` helper, then
follows the `todo_updated` event into `sessionPlan.ts` and reseeds on reconnect.
Both `PlanPanel` and the Cowork sidebar's TODO section read the same store
through the shared hook `useSessionPlanLive`
(`web/src/hooks/useSessionPlanLive.ts`): seed from
`GET /api/sessions/{id}/todo`, one in-flight request per host+session (a
reconnect reseed supersedes a stalled one), a refetch on `eventBus.onReconnect`,
and live updates through `todo_updated`. `setSessionPlan`
(`web/src/lib/sessionPlan.ts`) is the single write boundary: it rejects a
malformed body or a foreign session id, drops a lower revision, lets revision 0
clear a removed plan, and accepts `force` for a reconnect reseed (a recreated
file restarts at 1). The sidebar no longer hardcodes an empty placeholder and
renders `SidebarTodoSection`
(`web/src/components/Layout/SidebarTodoSection.tsx`) with the shared
`TodoPlanRow`/`visibleTodoItems` primitives (open items first, an 8-row cap with
"+N more"); the section is folded by default (`DEFAULT_SECTIONS.todo = false`),
so it must be expanded once. The chat display
setting `plan_panel` (`collapsed` | `5` | `10` | `all`, default `5`, under
`chat_verbosity.overrides`, edited in Settings → Chat display) sets only the
panel's initial state: a number starts it open with that many rows, open items
first, with a "+N more" toggle; the user's per-session fold always wins. A
`task`/`agent` call renders its run inline in the chat by matching the run's
`toolCallId`: collapsed by default with a live summary (agent, status, message
count, elapsed, plan item), expandable to the streaming transcript with
tail-follow, and child thinking routed through `ThinkingBlock` so it obeys the
chat's thinking setting. With no in-memory run (a transcript replayed after a
restart) the call renders as a plain tool block.

**Review fixes (2026-10-28).** The re-anchor block lists plan item lines only, so
echoing it into `todowrite` parses. Open items waiting on open dependencies appear
on one separate line after the list, `Blocked: t3 by t1, t2` (entries joined by
`; `), and nothing when none are blocked. `todo_updated`,
`GET /api/sessions/{id}/todo` and the Pulse todo carry `revision` from the plan
file header. The server publishes each write from its own goroutine, so both web
stores drop a frame whose revision is lower than the one held; equal or higher
replaces. `task` with `todo_item` refuses a done item ("already done; pick an open
item or append a new one"); resume may still reopen its own item, and links it only
after `beginResume` succeeds. Every run records its originating tool call id
(`ToolCallID`, set through `TaskTool.ExecuteCtx`), which the inline chat card uses
to find its run. A failed or cancelled run's item shows its `FAILED:` /
`CANCELLED:` result line, never "running as".

## Prompt

The model is told to use the plan by a static system fragment,
`planExecutePromptContent` in `internal/agent/prompt.go`, marker
`[ocode:plan]`. It asks for a `todowrite` plan before work of three or more
steps or two or more files, one item `in_progress` at a time with a one-line
result on `done`, `todo_update set_deps` for ordering, and `task(todo_item=tN)`
for self-contained or output-heavy items.

- **Placement:** directly after the recap fragment in `BasePromptMessages`.
  It is a const, so the cached prefix stays byte-stable; never make it vary per
  turn.
- **Gate:** primary agents only. `Agent.PlanPromptEnabled` defaults on in
  `NewAgent`; task sub-agents, ask helpers and the advisor call
  `SetPlanPromptEnabled(false)` beside their recap opt-out.
- **Override key:** `plan` (`PromptCatalog`, so the settings prompt editor
  picks it up). The override text is appended via `config.ApplyPromptOverride`.
- **Report contract:** the `general` sub-agent's prompt makes the first line of
  its report a one-sentence outcome, because the harness copies that line (cap
  300 characters) into the linked item's `result`.
