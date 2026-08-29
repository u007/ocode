# SQLite Session Storage — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** No separate spec doc — this INDEX's Architecture and Global
Constraints sections are the spec. Background: `docs/superpowers/plans/2026-07-21-session-storage-ojsonl.md`
introduced the current `.ojsonl` format this plan adds a third format
alongside; this plan follows directly from a live memory-profiling
investigation (73GB/100min heap churn traced to `session.ListRefsForDir` /
`readOcodeMeta` re-scanning thousands of session files per listing call,
5,538 sessions in this project's own `.ojsonl` directory alone, 34,102
total across all projects on this machine).

**Goal:** Add SQLite as a third, lazily-adopted session storage format.
Every *new* session is created directly as `.sqlite`. An existing `.json`
or `.ojsonl` session is migrated to `.sqlite` — and its old file deleted —
the next time it is written to (i.e. loaded into chat and resumed), never
in bulk. A small shared per-project `index.sqlite` gets a row for every
migrated session, so listing (the project sidebar, the session picker) can
serve migrated sessions from one indexed query instead of opening and
parsing each session file — the actual fix for the memory/CPU cost, which
scales with total session count today regardless of format.

**Architecture:** One new file, `internal/session/sqlitestore.go`, holds
all SQLite-specific code: connection/schema helpers, per-session
read/write (`writeSqliteSessionFull` for a from-scratch write,
`appendSqliteSession` for the common incremental-append case — mirrors
`ojsonl.go`'s append-only design so a long session doesn't pay to rewrite
its whole history every turn — and `readSqliteSession`), and the shared
per-project index (`upsertIndexRow` / `deleteIndexRow` / `queryIndexMetas`).
`session.go`'s `saveToDir`, `Load`, `LoadForDir`, `List`, `ListRefsForDir`,
`ListRefsPaginated`, and `Delete` get minimal, additive dispatch: check for
`.sqlite` first, migrate-on-write for an existing legacy file, fall back to
today's `.json`/`.ojsonl` paths unchanged otherwise. No caller outside
`internal/session` changes — `Save`, `Load`, `List*`, `Delete` keep their
exact signatures, so the TUI, desktop shell, and web server (all three
already share this one package) pick up the fix with zero changes on their
side.

**Tech Stack:** `modernc.org/sqlite` (pure Go, no CGO — verified it
cross-compiles clean for `linux/amd64`, `windows/amd64`, `darwin/arm64`,
matching `Makefile`'s `build-all`, which has no CGO cross-toolchain
configured) via `database/sql`. WAL journal mode + a 5s busy-timeout
pragma on every connection, since multiple `ocode` processes (TUI, desktop,
web) can point at the same project's session directory concurrently.

## Global Constraints

- New sessions are created directly as `.sqlite` (confirmed with the user
  — no reason to create a new session in the old format just to migrate it
  immediately).
- An existing `.json`/`.ojsonl` session only migrates to `.sqlite` on a
  *write* that follows it having been loaded — never on a read-only Load,
  never in a bulk/background pass. This is a deliberate, permanent design
  choice (confirmed with the user), not a temporary rollout shim: the
  legacy read/list code paths in `session.go` stay in the codebase
  indefinitely, shrinking in relevance only as individual sessions get
  resumed and written to over time. Do not remove `.json`/`.ojsonl`
  handling as part of this plan or a future cleanup pass without an
  explicit new decision from the user.
- Migration safety: `migrateToSqlite` writes the new `.sqlite` file and
  reads it back (`readSqliteSession`) to confirm it round-trips *before*
  deleting the original `.json`/`.ojsonl` file. If verification fails, the
  original is left in place and the save returns an error — a transcript
  is never lost to a bad migration.
- Crash safety: if the process dies between "new `.sqlite` written and
  verified" and "old file deleted," the old file becomes a harmless
  orphan — `.sqlite` is always tried first on `Load`, and a successful
  `.sqlite` load opportunistically deletes any leftover legacy file for
  the same id. Listing de-dupes the same way (an index row shadows a
  same-id legacy scan entry).
- Every message is stored as one JSON-marshaled `agent.Message` per row
  (`messages(seq, data)`), not relationally decomposed — matches the
  existing `.ojsonl` design and avoids a schema migration every time
  `agent.Message` grows a field (it has evolved multiple times already,
  see `internal/agent/client.go`).
- The existing `index.json` (`sessionIndex`, written by `updateIndex` and
  `Delete`) is legacy, write-only, and already unread by any listing path
  today (confirmed by grep — the only reads are read-modify-write inside
  `updateIndex`/`Delete` themselves). Leave it exactly as-is; do not wire
  the new `.sqlite` path into it and do not remove it — out of scope.
- No new public API: `Save`, `SaveForDir`, `Load`, `LoadForDir`, `List`,
  `ListRefsForDir`, `ListRefsPaginated`, `ListRefs`, `Delete` keep their
  current signatures. `ListAll` has no external callers (confirmed by
  grep) — leave it untouched.
- Pin `modernc.org/sqlite` to an exact version (`v1.57.0`, latest at plan
  time) per this repo's dependency-pinning rule.

Spec coverage note: every requirement above maps to a task below (see the
table). Self-review passed — no gaps.

## Execution Order

Tasks are sequential; each is independently testable and leaves the
package in a working, fully-tested state (`go build ./...` and
`go test ./internal/session/... -race` both green after every task).

| Task | File | Delivers |
|------|------|----------|
| 1 | `01-schema-and-connection-helpers.md` | `modernc.org/sqlite` dependency; `sqlitestore.go` connection/schema helpers (`openDB`, `openSessionDB`, `openIndexDB`, path helpers). |
| 2 | `02-session-read-write-core.md` | `writeSqliteSessionFull`, `appendSqliteSession`, `readSqliteSession` — the per-session `.sqlite` read/write core. |
| 3 | `03-shared-project-index.md` | `upsertIndexRow`, `deleteIndexRow`, `queryIndexMetas`, `mergeMetas` — the shared `index.sqlite`. |
| 4 | `04-write-path-and-migration.md` | `saveToDir` dispatch (new → `.sqlite` directly; existing `.sqlite` → append; existing legacy → migrate-then-delete); `migrateToSqlite`. |
| 5 | `05-read-path.md` | `Load`/`LoadForDir` try `.sqlite` first; opportunistic orphan cleanup. |
| 6 | `06-listing-path.md` | `ListRefsForDir`, `ListRefsPaginated`, `List` union migrated (indexed) and legacy (scanned) sessions. |
| 7 | `07-delete-path.md` | `Delete` removes whichever format exists, plus the index row. |
| 8 | `08-verification-and-rollout.md` | Full-suite verification across `internal/session`, `internal/server`, `internal/tui`; manual smoke test; rollout notes. |

## Verification (whole feature)

- `go build ./...` and `go test ./internal/session/... -race` green after
  every task.
- `go test ./internal/server/... ./internal/tui/... -run Session` green
  after Task 8 (these packages only consume `internal/session`'s public
  API, unchanged, but a real regression would show up as a behavior
  difference, not a compile error).
- Manual QA (after Task 8, see Task 8 for exact steps): start a brand-new
  session, confirm a `.sqlite` file appears; resume an existing `.ojsonl`
  session and send one message, confirm it now has a `.sqlite` file and
  the `.ojsonl` file is gone; confirm the project sidebar still lists both
  kinds of sessions correctly, sorted by recency.
