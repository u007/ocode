---
type: Concept
title: Cross-process session activity sync (revision revalidation)
description: How the web/desktop UI converges on session transcript changes made by another ocode server process (desktop + make dev + TUI) via an opaque stored-transcript revision token and a 15s revalidation poll.
resource: docs/gotchas/session-writers-conflict-recovery.md
tags:
  - session
  - cross-process
  - revision
  - revalidation
  - poll
  - compact
  - web
  - desktop
timestamp: 2026-09-20T17:57:24Z
---
## The problem

ocode can run several server processes that share one project's session
storage — the desktop app (`bin/ocode.app`, its own listener), a `make dev` /
`go run` server, and the TUI. All resolve the same `/api/projects` and share
`~/.local/share/opencode/project/<slug>/sessions` (per-session `<id>.sqlite` +
`index.sqlite`, WAL).

The server EventBus (`internal/server/event_bus.go`) is purely **in-process**:
a manual `/compact` (`internal/server/handler.go HandleCompactSession`) or an
auto-compact (`internal/server/agent_session.go applyCompactResult`) broadcast
only reaches clients of that same process. Clients connected to another process
kept their stale pre-compaction transcript — hence *"I ran /compact on the web
UI in another browser; it isn't reflected on my desktop UI."*

Cross-process **WRITE** conflict semantics (SQLite write lock, overlap check,
reconcile-append) are documented in `../gotchas/session-writers-conflict-recovery.md`.
That document has **no** live-sync protocol — this is it.

## Design: client-side revision revalidation

Lowest-risk of the options (the alternatives were polling the full diff or
extending the bus across processes — both far heavier).

### Server side: an opaque stored-transcript token

`session.StoredRevisionForDir(wd, id)` (`internal/session/revision.go`) returns
a token that moves whenever the stored transcript changes, by **any** writer, in
**any** process:

- **sqlite (authoritative once migrated):** one DDL-free single-row SELECT via
  `openDBRaw` (mirrors `readHistoryGen`): `meta.updated_at` + `meta.history_gen`.
  `updated_at` is bumped by every write (appends, replacements, even
  metadata-only writes); `history_gen` by synchronous shrinks
  (compaction / truncate / rewind). Together they move on any transcript
  mutation.
- **legacy `.ojsonl` / `.json`:** file mtime (ns) + size.
- **Schema-less / corrupt sqlite:** falls back to the file token rather than
  erroring, so a cold file cannot break the poll.
- **No stored session:** returns `""` — a stable "never revalidate" signal.

The token is exposed on two HTTP responses so both transcript-load and
state-only paths can establish the baseline and compare:

- `GET /api/sessions/:id` (`SessionDetail`, `internal/server/handler.go`):
  `revision` field populated from `StoredRevisionForDir(entry.ProjectRoot, id)`.
- `GET /api/sessions/:id/state`
  (`handler_session_state.go`): same, with the session state payload.
  Omitted for bridged/in-memory sessions (no file to watch → "never revalidate").

### Client side: remember the baseline, compare later

- `web/src/lib/sessionRevision.ts`: a module-level `Map` keyed
  `host\u0000sessionId` (mirrors `sessionPrefetch`'s host key) holding the
  revision each session's transcript was last fetched **at**.
  - `noteSessionRevision` — recorded centrally by `api.getSession`
    (`web/src/api/client.ts`, after every transcript fetch) so **every**
    transcript-load path establishes the baseline. Absent/empty revision clears
    the baseline (bridged / in-memory sessions).
  - `sessionRevisionMoved` — true when the server's current revision differs
    from the recorded one. Unknown baseline → false (no fetch recorded yet;
    treating "unknown" as "changed" would refetch every restored tab on the
    first tick for no reason).
  - `clearSessionRevision` — drops the baseline (tab close / rekey).

- `web/src/lib/sessionEvents.ts` `revalidateSession(sessionId, router)`:
  1. **Skip** if this client's own turn is locally `turnActive` — the live bus
     owns the transcript; a mid-turn disk snapshot is staler than memory.
  2. `GET /api/sessions/:id/state` (host-scoped via `hostFor`).
  3. **No movement** → stop (unless turn state or live pending asks changed,
     handled by `applyReconcileState` — no transcript refetch, no render churn).
  4. **Movement** → `GET /api/sessions/:id` with `limit: RECONCILE_PAGE_SIZE`
     (=100), dispatch `MERGE_SNAPSHOT` (preserves live/mid-turn content via the
     reducer's mid-turn guard; carries authoritative `total`). Turn state is
     applied by `applyReconcileState` **only when it differs** from the client's.
     A live pending ask is hydrated from `state.pending_asks`.

- `web/src/hooks/useSessionRevisionSync.ts`: mounted in `web/src/App.tsx`
  next to `useTurnWatchdogAll`; polls every open tab (any project, active or
  background) every `REVISION_POLL_MS = 15_000`, plus an immediate pass on
  `onWake` (`online` / `visibilitychange`). Dedupes in-flight per session so a
  slow `/state` can't stack up.

### Baseline hygiene

- `closeSessionBackend` (`sessionEvents.ts`) — clears the revision on tab
  close.
- `session_rekeyed` event handler (`sessionEvents.ts`) — clears the **old** id's
  baseline on `/reset-id`. The new id's baseline is established when its
  transcript is next fetched.

## What converges and what does not

| Converges | Does not |
|---|---|
| Stored transcript content (appends, `/compact` shrinks, truncations) | **Cross-process turn state** — each server's session registry is per-process; `/state` reports `turn_active:false` for a turn running in the other process. The transcript still converges as committed messages appear, but no live token streaming/spinner from the other process. |
| Pending-ask state (read from `state.pending_asks` on refetch) | **Latency** — bounded by the 15s poll or a wake; not an instant push. In-process activity still arrives instantly over the bus. |
| Session title / metadata (carried in the fetched transcript and state) | **Agent-memory reconciliation** — a resident agent in the non-writing process still holds stale in-memory messages after an out-of-process compaction; its next turn-end save surfaces the existing `ErrTranscriptConflict` contract. This feature fixes UI convergence, not agent-memory reconciliation. |

## One intentional cost

Because the baseline is set when the transcript is fetched and a local turn
leaves it stale, one redundant transcript refetch occurs on the next poll for a
session that had a turn end in another process. Bounded: roughly one refetch per
turn per open session. The refetch itself is cheap (one small transcript window
over HTTP), and the alternative — always diffing server-side — is heavier and
cannot carry authoritative `total`.

## Tests (all passing, mutation-verified)

**Go:**
- `internal/session/revision_test.go` — core contract: revision moves on append
  and on synchronous shrink, stable on read-only pass, empty for missing
  sessions, schema-less-sqlite fallback to file token.
- `internal/server/session_revision_test.go` — `TestSessionRevisionExposedAndMoves`:
  GET /state and GET /detail carry the same token; an external write moves both.
  Verified to **fail** if only `history_gen` is checked (the test also asserts
  `updated_at` participates — both columns must move).

**Web:**
- `web/src/lib/sessionRevision.test.ts` — unknown baseline is no-op, match/no-match,
  host-key independence, clearing on absent revision and on `clearSessionRevision`.
- `web/src/api/client.sessionRevision.test.ts` — pins that `api.getSession` records
  the stored revision centrally; **fails** if the `noteSessionRevision` call is
  removed (the poll would silently stop detecting out-of-process writes).
- `web/src/lib/sessionEvents.test.ts` `revalidateSession` describe — unchanged-token
  does nothing beyond the state poll, movement triggers `MERGE_SNAPSHOT` with the
  right page size, **skips entirely** while the local turn is active (both checks
  fail if the skip is removed, and both fail if `sessionRevisionMoved` is stubbed
  either way — the test depends on real movement detection, not a mock), host
  routing, spinner armed when another process reports `turn_active`, live pending
  ask hydrated without a transcript refetch.

**Validation:** `go build ./...`, `go vet ./internal/session ./internal/server`,
`go test ./internal/session ./internal/server`; web `tsgo --noEmit`, `vite build`,
full vitest suite (186 files / 1630 tests) all green.
