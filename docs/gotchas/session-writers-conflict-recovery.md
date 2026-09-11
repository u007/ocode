---
title: Concurrent session writers — conflict semantics and recovery
date: 2026-09-06
status: resolved-with-follow-ups
tags: [session, sqlite, concurrency, persistence]
---

# Concurrent session writers — conflict semantics and recovery

Multiple ocode processes (TUI, desktop, web server) can write the same
session file: they share the per-project sessions dir, and the same session
id can be open in more than one process at once. In-process writers
serialize on the per-session stripe mutex (`lockFor`, live.go); across
processes, coordination is the SQLite write lock plus the overlap check in
`appendSqliteSessionOnce` (internal/session/sqlitestore.go).

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
  → silent drop (by design; the turn-end sync save is authoritative).
  Live writes decide their suffix via `liveAppendStart` (2026-09-09): the
  stored rows, or the loader's filtered view of them, must be a
  byte-identical prefix of the snapshot; the rows past that prefix are
  appended at the raw tail. Any other shape drops.
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
  only a tool row still holding an ask sentinel for that tool-call id at
  that seq. Without it every later save overlapped the stored sentinel with
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
  keeps the raw rows in place and appends the unsaved suffix after them,
  so the loader view of the result equals the caller's transcript.
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
update dodges it for per-session model overrides; turn-end saves from a
filtered base (resume a session that closed mid-ask, then let the next
turn end) reconcile through the loader-view match in `rebaseAppend`.

Incident (2026-09-09, ses_2026-09-09-111131-f098f1cb and
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

## Test hygiene

Server tests that register well-known session ids ("sess-1", …) MUST
isolate storage: `t.Setenv("HOME", t.TempDir())` before `NewHandler()`
(see zz_repro_perm_test.go). Without it, tests share the real sessions dir
and leftovers from other tests/runs surface as cross-test transcript
pollution — previously masked by the shrink-delete nuking the file.
`TestLiveWorkerRetiresWhenIdle` mutates the global `liveIdleTimeout`;
workers capture the timeout at creation (`liveWriter.idleTimeout`) so a
still-running worker from an earlier test cannot race the global.
