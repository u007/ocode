# Session Storage: `.ojsonl` Append-Only Format — Design

## Overview

Add a new on-disk session format, `.ojsonl`, for ocode's own sessions
(`internal/session/session.go`). New sessions are written in this format;
existing `.json` sessions are left untouched and remain readable forever
through the current code path. There is no in-place migration.

## Motivation

`Save()` (session.go:126) currently reads the whole session file, replaces
the entire `Messages` slice, and rewrites the whole file with
`json.MarshalIndent` on every single save — cost grows with total
conversation size, not with what changed. It also uses a plain
`os.WriteFile`, so a write interrupted partway (crash, power loss, disk
full) can corrupt the *entire* document, including messages that were
already durably saved before this call.

`.ojsonl` fixes both: saves cost is proportional to what's new (new
messages + one metadata line), and a torn write only risks the last
line, not the whole file.

## Format

One file per session, `ses_xxx.ojsonl`. Line 1 is a header record,
rewritten only when the title changes:

```json
{"v":1,"id":"ses_...","created_at":"2026-07-21T...","title":"...","title_generated":true}
```

Every subsequent line is one of two record types, each a single JSON
object terminated by `\n`:

```json
{"type":"msg","role":"user","content":"...", ...agent.Message fields}
{"type":"meta","metadata":{...}}
```

`updated_at` is not stored anywhere in the file — it is derived from the
file's `mtime` via `os.Stat`, matching the fallback `readOcodeMeta`
(session.go:531) already uses when the field is absent.

## Write path (`Save`)

An in-process cache, `map[sessionID]persistedCount`, tracks how many
messages have already been written to disk for a session.

- First `Save()` for a session ID in this process (e.g. right after
  resuming) bootstraps the count with a cheap line-count scan (count
  lines, don't unmarshal each one).
- Every `Save()` after that appends `messages[persistedCount:]` as `msg`
  lines, appends one `meta` line if `metadata` is non-nil, and advances
  the cached count. This is a pure append — no read of existing content.
- If `title` changes, line 1 is rewritten: read the file, replace line 1,
  write the result to a temp file in the same directory, then
  `os.Rename` into place. Rename is atomic on the same filesystem, so a
  crash during this path leaves either the old or the new file intact,
  never a half-written one. This path is rare (title is normally set
  once, at auto-title or explicit `/title`).
- Each appended line is built fully in memory and written with a single
  `Write()` call on a file opened with `O_APPEND`. POSIX guarantees the
  kernel serializes concurrent `O_APPEND` writes from different processes
  at the syscall level, so as long as each line is one write call,
  concurrent appends from two processes cannot interleave mid-line.

## Read path (`Load`)

Stream all lines in order. `msg` lines append to the message slice;
each `meta` line replaces the metadata map wholesale (last one wins),
matching the current `Save()` semantics (`if metadata != nil { s.Metadata
= metadata }`). Line 1 supplies `id`/`title`/`created_at`.

If the last line is incomplete (truncated JSON, e.g. from a crash
mid-append), it is dropped and a warning is logged; the rest of the
session loads normally. This is a strict improvement over today's
behavior, where a torn write corrupts the whole JSON document and the
entire session becomes unreadable.

## Listing (`ListRefs`, `ListRefsPaginated`)

Read only line 1 (header) plus `os.Stat` for `mtime`. No scanning past
line 1 is needed — cheaper than the current `readOcodeMeta`, which must
still stream past the (potentially large) `messages` array to reach
later JSON keys.

## Format detection

Existing code paths branch on file extension: `.json` uses the current
whole-document struct path unchanged; `.ojsonl` uses the new streaming
path. `index.json` behavior is unchanged. New sessions always write
`.ojsonl`. Old `.json` sessions are read and, if resumed, continue to be
written in the old `.json` format (no cross-format conversion in this
change).

## Known limitation: concurrent writers to the same session

This is a real, new failure mode worth naming rather than silently
shipping. Today, two processes saving the same session concurrently
race on read-modify-write: the last writer's full snapshot silently wins
and the other's messages are dropped, but the file itself stays
structurally valid. With `.ojsonl`, two processes each holding a stale
`persistedCount` could both append their own version of "the next
message" — no data is lost, but the result can contain duplicate or
conflicting entries at what was meant to be the same position.

This is not solved in this change (no file locking is introduced) — it
matches the existing single-writer-per-session assumption elsewhere in
the codebase (see the `index.json` race already tracked separately) and
is documented here so it isn't mistaken for a regression later.

## Testing

- Round-trip save/load parity against existing `session_test.go` cases.
- Corrupt/truncated last-line recovery: session still loads, warning
  logged, only the incomplete line is dropped.
- Bootstrap-count-from-existing-file correctness after a simulated
  process restart.
- Header rewrite (title change) survives a simulated crash between temp
  write and rename (old file remains intact until rename completes).
- Listing reads only line 1 + `stat()` — assert no message/meta lines
  are parsed, to guard the perf property.

## Out of scope

- Converting existing `.json` sessions to `.ojsonl`.
- File locking / true concurrent-writer safety (see limitation above).
- Any move to SQLite. `opencode` (the sst/opencode project, unrelated to
  this codebase's `AppName` constant of the same name) migrated its own
  session storage to SQLite for exactly this concurrent-write and
  indexed-query problem — a `session` table with structured columns plus
  a `message` table with one JSON-blob row per message. That's a
  materially larger change (new dependency, schema/migration tooling,
  rewriting every read/write path) and is a candidate for a future,
  separate spec if concurrent-write safety becomes a priority — not part
  of this change.
