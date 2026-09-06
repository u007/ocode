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
- **User messages persist via tail insert.** `AppendUserMessageForDir`
  (server `persistUserMessage`) does an append-only single-row INSERT at
  seq = stored count. It never reads or rewrites rows, so it cannot trip
  the overlap check even when the stored transcript holds rows the load
  path filters out (see next section). PK violation (another writer took
  the same seq) → bounded retry re-reads the count.
- **Turn-end saves reconcile.** Server `persistTurnTranscript` /
  `commitPartialTranscript` call `session.ReconcileAppend(ForDir)`:
  on conflict the raw disk transcript is reloaded and merged — foreign
  rows kept in place, our not-yet-stored suffix appended after them
  (`rebaseAppend` greedy-matches our own live-written rows in order).
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
same way. The tail insert dodges this for user messages; full-snapshot
saves from a filtered base (e.g. resume a session that closed mid-ask,
then let the next turn end) still conflict — pre-existing, mitigated by
turn-end reconcile/converge + logging. Proper fix is splitting the
transcript view (raw, for persistence) from the LLM view (filtered, for
requests) — follow-up.

## Test hygiene

Server tests that register well-known session ids ("sess-1", …) MUST
isolate storage: `t.Setenv("HOME", t.TempDir())` before `NewHandler()`
(see zz_repro_perm_test.go). Without it, tests share the real sessions dir
and leftovers from other tests/runs surface as cross-test transcript
pollution — previously masked by the shrink-delete nuking the file.
`TestLiveWorkerRetiresWhenIdle` mutates the global `liveIdleTimeout`;
workers capture the timeout at creation (`liveWriter.idleTimeout`) so a
still-running worker from an earlier test cannot race the global.
