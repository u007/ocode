---
title: Concurrent session writers — conflict semantics and recovery
date: 2026-09-06
status: resolved-with-follow-ups
tags: [session, sqlite, concurrency, persistence]
type: Gotcha
description: Concurrent session writers — conflict semantics and recovery (updated with 2026-11-05 dismissed-question incident)
timestamp: 2026-11-09T07:48:45Z
---
# Concurrent session writers — conflict semantics and recovery

## Incident (2026-09-06, ses_2026-09-06-021943-56a29b3c)

A desktop `persistUserMessage` (load disk → append user msg → save) raced a
TUI turn's live writes in the same session. The desktop's snapshot ended at
seq 24; the TUI landed seq 25 (assistant) and 26 (PERMISSION_ASK sentinel)
in the load→save window; the desktop's save diverged at seq 25 and errored
with "conflicting message at seq 25 (concurrent writers diverged)". The
user's typed message was rolled back and dropped. Fixed by the redesign
below.

## Current contract (after the 2026-09-06 hardening)

- **BEGIN IMMEDIATE, not deferred BEGIN.** `openDB`'s DSN carries
  `_txlock=immediate` (modernc.org/sqlite issues `begin immediate` for
  non-readonly BeginTx). The design comments in live.go always claimed
  this; the code had `db.Begin()` (deferred), which hits the classic
  SHARED→RESERVED upgrade deadlock that `busy_timeout` does not cover —
  two writers both hold SHARED and both try to upgrade, SQLite fails one
  immediately. With immediate txs the second writer blocks at BEGIN (up to
  the 5s busy_timeout) and the store's 8-attempt BUSY retry only sees
  genuine contention.
- **Divergent overlap conflicts (never silent).** Identical overlap
  converges (idempotent retry). Differing overlap: sync save →
  `ErrTranscriptConflict` (typed, via `session.IsConflictErr`); live write
  → drop (by design; the turn-end sync save is authoritative). Since
  2026-09-22 every live drop logs `session: live snapshot dropped for
  <id>: <reason>` (superseded generation / stored rows are not a prefix)
  and every replace logs `session: replace <id>: shrink|rewrite ...,
  history_gen N -> N+1`. Neither path is silent any more.
- **Ordinary saves never shrink.** A sync save with FEWER messages than
  stored conflicts (`ErrTranscriptConflict`). The old delete-all-and-rewrite
  for shorter snapshots silently destroyed another writer's appended rows
  cross-process. Only the explicit replace path may shrink.
- **Replace is explicit and owns the transcript.** `Replace` /
  `ReplaceForDir` (server `replaceSession`, TUI `m.replaceSession`) rewrite
  the message set wholesale and bump `history_gen` (queued pre-replacement
  live snapshots drop instead of resurrecting replaced history). A replace
  whose stored-prefix overlap converges byte-identically is a plain append
  (no gen bump). Callers that legitimately replace: /compact (TUI
  compaction save, server `applyCompactResult`), transcript truncation
  (`POST /api/sessions/{id}/truncate`), TUI message-picker rewind.
- **Metadata-only writes never touch rows.** `UpdateMetadataForDir`
  rewrites only `meta.metadata_json` (+ `updated_at`) in one immediate tx.
  Server `setSessionModelOverride` (PUT/DELETE `/api/sessions/{id}/model`)
  uses it. Before (2026-09-08) it did load→set `metadata["model"]`→
  `SaveForDir`, which hit the load-filter hazard below on any session whose
  stored transcript had filtered rows: every web/desktop model pick 404'd
  with "stale snapshot: 432 message(s) but 435 stored" and the sidebar
  never changed. Any new metadata mutation must use this path, not a
  full-snapshot save.
- **User messages persist via tail insert.** `AppendUserMessageForDir`
  (server `persistUserMessage`) does an append-only single-row INSERT at
  seq = stored count. It never reads or rewrites rows, so it cannot trip
  the overlap check even when the stored transcript holds rows the load
  path filters out (see next section). PK violation (another writer took
  the same seq) → bounded retry re-reads the count.
- **Ask resolutions rewrite the stored sentinel row in place.** Answering a
  permission or question prompt replaces the PERMISSION_ASK / QUESTION_PROMPT
  tool result's content in memory. `RewriteAskResultForDir` (server
  `rewriteAskResult`, called by both resolve handlers before the
  continuation Steps) applies the same rewrite to the stored row — guarded:
  only a tool row still holding an ask sentinel (PERMISSION_ASK /
  QUESTION_PROMPT) or a dismissed question (`tool.QuestionDismissedResult`)
  for that tool-call id at that seq. The dismissed case is the 2026-11-05
  fix: a dismissed question that the user then answered used to fail the
  guard, so the dismissal text stayed on disk (see that incident). Without
  the rewrite every later save overlapped the stored sentinel with
  different bytes: live snapshots dropped and the turn-end sync save
  conflicted (and the handlers discarded that error), so the disk transcript
  froze at the already-answered ask while memory moved on. Every disk-backed
  reload/reconcile (GET /api/sessions/:id, the turn watchdog, tab
  activation) then replayed the stale answered question and wiped the
  continuation — the 2026-09-10 "streamed, stopped, reverted to input, next
  question never prompted" desktop report (ses_2026-09-10-153254-b55bec37:
  memory 66 rows, disk stuck at 62). The resolve handlers now persist the
  continuation through `persistTurnTranscript` so conflicts are logged and
  re-synced like an ordinary turn.
- **The pre-persisted user message must serialize identically to the
  turn's in-memory copy.** Server `runTurn` re-appends the pending user
  message in memory stamped with `user_seq` (`nextUserSeq` =
  `session.NextUserSeq`, max stored user seq + 1). The tail insert stamps
  the same value (`MAX(json_extract(data,'$.user_seq'))+1` over stored
  user rows) so the two copies are byte-equal. Before this (2026-09-08)
  the tail insert wrote `{role,content}` only: every live save during the
  turn hit a differing overlap and was silently dropped, the turn-end
  save conflicted, `rebaseAppend` saw base divergence, and memory
  re-synced to the one-message disk copy — the streamed reply vanished
  from the desktop/web chat mid-turn ("session auto-reset") and the
  session showed only the user's line in history. Any new writer that
  appends a user message must derive its seq the same way.
- **Turn-end saves reconcile.** Server `persistTurnTranscript` /
  `commitPartialTranscript` call `session.ReconcileAppend(ForDir)`:
  on conflict the raw disk transcript is reloaded and merged — foreign
  rows kept in place, our not-yet-stored suffix appended after them
  (`rebaseAppend` greedy-matches our own live-written rows in order).
  A base that equals the LOADER's filtered view of the stored rows (see
  the next section) is not a divergence (2026-09-09): `rebaseAppend`
  retries the prefix match against `removeIncompleteToolRequests(stored)`,
  keeps the raw rows in place and appends the unsaved suffix after them.
  **The reconcile now returns the RAW merged view, not the loader's
  filtered view** — the old claim that "the loader view of the result
  equals the caller's transcript" is wrong. What must hold is only the
  cross-turn invariant: the NEXT turn's save still finds a byte-identical
  prefix on disk and converges (pinned by
  `TestReconcileAppendFilteredResidentStaysConvergentAcrossTurns`).
  Divergence INSIDE the base has no safe merge: memory re-syncs to disk
  and the dropped in-memory suffix is logged explicitly, so the session
  stays writable instead of every later save conflicting forever.

## The load-filter hazard (why "disk+1" snapshots can never converge)

`loadFromDir` applies `removeIncompleteToolRequests` for LLM-facing
history: it drops any role=tool message with an empty ToolID (orphans,
PERMISSION_ASK sentinels) and unmatched assistant tool calls. A stored
transcript containing such rows loads SHORTER and shifted, so a
full-snapshot "loaded view + new message" save compares a shifted sequence
against stored rows and conflicts forever — every retry re-filters the
same way. The tail insert dodges this for user messages and the metadata-only
update dodges this for per-session model overrides; turn-end saves from a
filtered base (resume a session that closed mid-ask, then let the next
turn end) reconcile through the loader-view match in `rebaseAppend`.

## Incident (2026-09-22, ses_2026-09-22-124117-caa39c49)

Desktop session, first message with two `@file` mentions. The agent ran
~40 steps (snapshot journal: edits at 12:43, 12:50, 12:51), the UI "reset
back to input msg", and the stored transcript held exactly one row (the
user message) with `history_gen=2`, `page_count=4` (16 KB). The 16 KB file
size with `auto_vacuum=0` proves the step rows were never inserted at all
(deleted rows would have left the file grown); `freelist_count=0` alone
does NOT prove that — deleted rows leave in-page free space, not freelist
pages.

What the disk shows: two authoritative replaces landed during the turn
(`history_gen` only bumps on the replace rewrite path), so every queued
live snapshot dropped on the generation guard, and the turn-end sync save
then either conflicted on seq 0 (stored user row rewritten: the meta title
carries the mentions once, the stored row three times) or was also
superseded. The debug ring (500 entries) had rotated, and both live drop
paths returned `false, nil` with no log line — so the trigger could not be
attributed. Note the "Restore to input" truncate is NOT the likely
trigger: `HandleTruncateSession` returns 409 while the turn is active and
the web client gates on `turnActive`/`isStreaming` as well.

Fix: the two live-drop paths and the replace path in
`appendSqliteSessionOnce` now log (reason, generations, row counts).
Regression: `TestLiveSnapshotsDroppedAfterMidTurnReplaceAreLogged`,
`TestReplaceShrinkIsLogged` (`internal/session/live_drop_log_test.go`).
Open lead: which caller rewrote seq 0 with the `@file` prefix duplicated —
look for that `replace ...: rewrite of overlapping rows` line next time.

## Incident (2026-09-20, ses_2026-09-18-233409-df1a92d3)

Reported session: `ses_2026-09-18-233409-df1a92d3` ("it stream and then
auto reset back to before my input", web/desktop). Stored transcript had 8
adjacent byte-identical assistant pairs at the tail, each immediately
followed by one `PERMISSION_ASK:` sentinel: `user, assistant(tool_calls),
assistant(identical), tool(ask)` per turn. The user kept nudging
("continue", "retry", "go on", "done?" ×4) while the agent re-raised a
fresh ask (new tool-call id) every turn.

Two coupled defects in the concurrent-writer reconcile (`internal/session/session.go`):

1. **`rebaseAppend` compared a filtered disk view against the caller's raw rows.**
   The caller's base could be the loader's view of disk
   (`loadFromDir` → `removeIncompleteToolRequests` dropped an unanswered ask
   round AND stripped assistant `tool_calls` whose result was still a
   sentinel). The merge compared that *filtered* stored row against the
   caller's *raw* in-memory row (calls intact) — never byte-equal — so it
   treated its own already-live-written assistant row as an unsaved suffix
   and appended a byte-identical duplicate.
   **Fix** (`rebaseAppend` at `internal/session/session.go:525`): new
   `stripUncompletedToolCalls` normalizes the caller's rows the same way
   the filter applies to stored rows (index-preserving) so the comparison
   is fair; the merge still appends the caller's real rows with
   `tool_calls` intact.

2. **`reconcileAppendToDirWithMessages` returned the filtered merge.**
   `rebaseAppend` produced a merge whose last rows were still the raw
   rows (calls intact), but `reconcileAppendToDirWithMessages` returned
   `removeIncompleteToolRequests(merged)` — the filtered view.
   `persistTurnTranscript` (`internal/server/agent_session.go:1748`; the adoption is at `:1759`) adopted
   it as `as.messages`, erasing the trailing PERMISSION_ASK sentinel + its
   tool-call from the resident transcript. `tailIsPermissionAsk` went
   false (the next user message started a fresh turn, the orphaned call
   was re-executed → a brand-new ask = the re-ask loop) and the resolve
   handlers could no longer find the sentinel to answer.
   **Fix**: return the merged transcript *unfiltered*; the non-rebase path
   already leaves the resident transcript untouched, so this restores
   parity with it.

Both fixes are covered by regression tests, each verified failing against
the pre-fix code by temporary revert:
- `internal/session/conflict_test.go`
  `TestReconcileAppendNoDuplicateWithUnresolvedAskCall` — fails pre-fix
  with the exact duplicate `[... running live check running live check PERMISSION_ASK ...]`.
- `internal/session/conflict_test.go`
  `TestReconcileAppendFilteredResidentStaysConvergentAcrossTurns` —
  filtered resident, consecutive ask-turns; fails pre-fix with both the
  duplicated `a-A` and the lost pending ask.
- `internal/server/agent_session_rebase_ask_test.go`
  `TestRebaseKeepsResidentPendingAsk` — fails pre-fix with the sentinel +
  call missing from the resident transcript.

**Durable rule:** any rebase that replaces a resident transcript must
preserve trailing unresolved ask sentinels (PERMISSION_ASK /
QUESTION_PROMPT) and their tool-calls — the rebase result must not pass
through `removeIncompleteToolRequests`; the next turn relies on them to
detect the pending ask and converge.

## Incident (2026-09-09, ses_2026-09-09-111131-f098f1cb and
ses_2026-09-09-102110-ab14279d): both sessions paused on a `question`
prompt, the user typed a chat message instead of answering, and the next
turn ran from an agent rebuilt off the filtered disk copy (9 rows on disk,
7 in memory). Every live save dropped on the differing overlap, the
turn-end save hit "base diverged", memory re-synced to the filtered disk
copy, and the `messages` snapshot wiped the streamed turn from the
desktop chat; each later turn repeated it ("adding new messages, stuck").
Fixed by the loader-view match above, for both the turn-end reconcile
and live saves (`TestLiveSaveFromFilteredBaseAppends`). Idle eviction and
`ReleaseAgent` now also keep a session paused on a `question` ask
resident (`tailIsPendingAsk`), matching the permission-ask exemption, so
the common way into a filtered base (30-min idle on an unanswered dialog)
is closed too. Splitting the transcript views — raw for persistence,
filtered for LLM requests — remains the structural follow-up.

## Incident (2026-11-05, ses_2026-11-05-100024-63d1c4b3, web/desktop): dismissed question answered, disk never rewritten

Symptom: the user answered a `question` prompt, the agent ran several steps,
then the chat reset to the question message. Every later answer in the same
session did the same.

Cause: the question's stored tool row had been rewritten to
`tool.QuestionDismissedResult` ("The user dismissed the question prompt
without answering."). The user then reopened the dismissed prompt from its
transcript card and answered it. `HandleAnswerQuestion`
(`internal/server/handler_questions.go`) deliberately accepts a dismissed
question, applies the answer in memory, and calls `rewriteAskResult`
(`internal/server/agent_session.go:2774`), which calls
`session.RewriteAskResultForDir` and then `rewriteAskResultRow`
(`internal/session/sqlitestore.go:721`). That guard accepted only ask
sentinels, so it refused with `serve: rewrite ask result for <id> seq N:
session: rewrite ask result <id> seq N: stored row is not a pending ask for
tool call "..."`. Disk kept the dismissal text while memory held the answer,
and every later save diverged from it.

Consequences, all visible in desktop.log for the session:

- every live snapshot was dropped with `session: live snapshot dropped for
  <id>: stored rows are not a prefix of the snapshot`. This fires even when
  the row counts match, because the bytes at the question's seq differ;
- the continuation's turn-end save logged `serve: question-continuation save
  for <id> diverged from a concurrent writer; re-synced to disk`, then `hit a
  transcript conflict; releasing the agent so the next turn re-bootstraps from
  disk`. The turn output was discarded and the next turn re-bootstrapped from
  disk, which ended at the question again;
- at 15:14 `session: replace <id>: shrink 71 -> 39 stored rows` cut the stored
  transcript back to the question.

It repeated at 12:07, 12:31, 14:00, 15:06 and 15:34.

Fix (uncommitted when written): `rewriteAskResultRow` now accepts
`isAskSentinel(stored.Content) || dismissedQuestion(stored.Content)`. The
helper was later deduped into `tool.QuestionDismissed` (`internal/tool/ask_sentinel.go:35`), and the guard now reads `isAskSentinel(stored.Content) || tool.QuestionDismissed(stored.Content)`.
The refusal text is now "stored row is not a pending or dismissed ask".
Regression tests are in `internal/session/ask_resolve_test.go`:
`TestRewriteAskResultAcceptsDismissedQuestion` and
`TestRewriteAskResultRefusesPlainToolRow`.

`HandleDismissQuestion` (`internal/server/handler_questions.go:123`; the log call is at `:140`) calls `slog.Info("question dismiss", "session", ..., "request_id", ..., "pulse", ..., "remote", ...)` to trace the caller. The repo sets no slog default, so slog's default handler writes the record through the stdlib `log` package, and `cmd/ocode-desktop` tees `log` output into `desktop.log`. The line reads `<YYYY/MM/DD> <HH:MM:SS> INFO question dismiss session=<id> request_id=<id> pulse=<true|false> remote=<addr>`. An empty value prints as `""`: `session=""` when the request omits `session_id`, and `remote=""` for a Pulse call, which has no `RemoteAddr`. `pulse` is `isPulseOrigin`, true for the in-process Pulse tool. User-Agent, Referer and X-Requested-With are deliberately not logged, because a Referer can carry a URL with tokens or paths. The original dismissal of this incident was not attributed. Candidate callers are
the dialog's "Don't answer" button (`useChat` `cancelQuestion`, which posts to
`/api/questions/cancel`), the Pulse tool `pulseQuestionDismissTool`
(`internal/server/pulse_tools_write.go:569`, reached through `pulseCall` and
identified by `isPulseOrigin`), and `web/src/components/Pulse/PulseFocusPane.tsx`.
The next dismissal logs whether it came from the Pulse tool (`pulse=true`) or a remote address. It does not separate the dialog's button from `PulseFocusPane`, since both send an ordinary browser request.

Follow-up, done: `isQuestionDismissed` in `handler_questions.go` and
`session.dismissedQuestion` were deduped into `tool.QuestionDismissed` (`internal/tool/ask_sentinel.go:35`). The handler, the sqlite guard and the transcript tail now share that one check.

Diagnostic recipe:

1. Open `desktop.log` in the logs directory (`paths.LogsDir()`, which is
   `~/.local/share/opencode/logs` on macOS) and grep for the session id
   together with the markers: `rewrite ask result`, `stored rows are not a
   prefix`, `question-continuation save`, `hit a transcript conflict`,
   `session: replace`, `question dismiss`.
2. A `rewrite ask result ... not a pending ask` line for the question's tool
   call is the signature of this incident. Before the fix it appears on every
   answer to a dismissed question. After the fix an answer to a dismissed
   question logs nothing here.
3. Compare disk with memory. Find the stored file with `find
   ~/.local/share/opencode/project -name '<id>.sqlite'` and run `sqlite3 <file>
   "select seq, json_extract(data,'$.role'), json_extract(data,'$.tool_call_id'),
   substr(json_extract(data,'$.content'),1,80) from messages order by seq desc
   limit 6"`. A question row that still reads "The user dismissed the question
   prompt without answering." while the snapshot in the drop line is ahead of
   it means the disk never received the answer.
4. Read the counts in the drop and conflict lines (`snapshot N msgs, stored M
   rows`). Equal counts with "not a prefix" mean the bytes at the same seq
   differ, which is this case. A count gap points at a different cause, such
   as the 2026-09-22 incident above.

## Test hygiene

Server tests that register well-known session ids ("sess-1", …) MUST
isolate storage: `t.Setenv("HOME", t.TempDir())` before `NewHandler()`
(see zz_repro_perm_test.go). Without it, tests share the real sessions dir
and leftovers from other tests/runs surface as cross-test transcript
pollution — previously masked by the shrink-delete nuking the file.
`TestLiveWorkerRetiresWhenIdle` mutates the global `liveIdleTimeout`;
workers capture the timeout at creation (`liveWriter.idleTimeout`) so a
still-running worker from an earlier test cannot race the global.

- `setHomeTree` (the package helper) also drains live session writes in
  `t.Cleanup` via `session.FlushAll`. A session worker captures its storage
  dir at **enqueue** time, so a queued snapshot write stays aimed at that
  test's home after the test body ends; without the drain it lands inside the
  tree `t.TempDir` is walking and an unrelated test fails with
  `TempDir RemoveAll cleanup: … directory not empty`. `t.TempDir` is created
  before the helper, so LIFO cleanup runs the flush before `RemoveAll`.

## Cross-process UI convergence (live-sync protocol)

The conflict contract above governs how two processes **write** to the
same file. It says nothing about how a client **reads** — specifically, a
client that has already loaded a transcript in **process A** will not see
a write made by **process B** (a `/compact`, a turn-end save, etc.)
without something pulling it in.

This is now handled by **client-side revision revalidation** — see the
concept doc at `../concepts/cross-process-session-sync.md` for the full
design, code paths, test coverage, and limits. The short version:

- The server exposes an opaque `revision` token on `GET /api/sessions/:id`
  and `GET /api/sessions/:id/state`, computed by
  `session.StoredRevisionForDir` (`internal/session/revision.go`) from
  `meta.updated_at` + `meta.history_gen` (or file mtime+size for legacy
  formats). It moves on any stored transcript change by any writer.
- The web client records the revision each transcript was fetched at
  (`web/src/lib/sessionRevision.ts`), and every open tab is polled every
  15s (`web/src/hooks/useSessionRevisionSync.ts`): when the token moved,
  the transcript is refetched and merged (`web/src/lib/sessionEvents.ts`
  `revalidateSession`).
- Baseline hygiene: cleared on tab close
  (`closeSessionBackend`) and on `/reset-id` (the old id's baseline in the
  `session_rekeyed` handler, `sessionEvents.ts`).

**Not covered here (deliberate non-goals):** cross-process **turn state**
is not synced — each server's session registry is per-process, so a turn
running in another process shows `turn_active:false` locally (the
transcript still converges as committed messages appear). Agent-memory
reconciliation after an out-of-process compaction
(`internal/server/agent_session.go applyCompactResult` — a resident agent
in the non-writing process still holds stale in-memory messages until its
next turn-end save surfaces `ErrTranscriptConflict`) is also out of scope;
that is a server-side concern, not a UI-convergence one.