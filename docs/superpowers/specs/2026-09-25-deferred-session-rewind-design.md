---
type: Design
title: Deferred, durable message rewind for ocode Web/Desktop
description: 'Design spec for deferred, durable message rewind in ocode Web/Desktop — IMPLEMENTED: server pending_rewinds resource + endpoints, commit-before-202 turn path (resident/no-resident/TUI /rc), localStorage recovery and cancel, lost-response probe.'
tags:
  - design-spec
  - session
  - rewind
  - web
  - tui
  - sqlite
timestamp: 2026-09-25T11:41:14Z
---
# Deferred, durable message rewind for ocode Web/Desktop

**Type:** Design spec (implemented)
**Date:** 2026-09-25
**Status:** **IMPLEMENTED (2026-09-25).** The server resource, the three endpoints, the commit-before-202 turn path (resident, no-resident and TUI `/rc` bridge), and the web localStorage recovery/cancel UI all shipped. See *As-built notes* below for code anchors.

---

## 0. As-built notes (2026-09-25)

What the code does now, in the order the design specifies it:

1. **Server capability.** `POST|GET|DELETE /api/sessions/{id}/rewinds[/{token}]` are registered at `internal/server/server.go:225-227` and handled by `HandlePreparePendingRewind` / `HandlePendingRewindStatus` / `HandleCancelPendingRewind` (`internal/server/handler_rewind.go:143`, `:179`, `:202`). Prepare and cancel take the per-session turn lock and answer **409 while a turn is active**; status is deliberately lock-free so it never blocks a running turn. The store is `internal/session/pending_rewind.go`: one row per session in the session's own SQLite file, `pendingRewindTTL = 24 * time.Hour` (`:40`), SHA-256 transcript fingerprint computed server-side, `armed | committed | stale`, lazy expiry prune, `ErrPendingRewindAlreadyCommitted` (`:56`), and the rekey move `movePendingRewindForRekey` (`:726`, invoked from `internal/session/session.go:1667`). Cancel of a committed row answers **410**.
2. **localStorage recovery and cancel.** `web/src/lib/pendingRewindStore.ts` persists versioned `v1` records under `ocode.ui.pendingRewind.v1:<host>::<sessionId>` (load/save/clear/subscribe/rekey). `ChatInput` hydrates on session change (`loadPendingRewind`, `web/src/components/Chat/ChatInput.tsx:241`, live subscription at `:289`), keeps the draft synced back into the record (`:627`), renders the compact **Pending rewind** banner with a **Cancel restore** button (`:1116-1140`) that calls `DELETE …/rewinds/{token}` and restores the exact pre-Restore draft, and issues the cancel itself if the `localStorage` write fails after prepare (fail-closed). Restore shows the §2.10 confirmation dialog (`web/src/components/Chat/MessageBubble.tsx:363-…`). Rekey rewrites the record (`web/src/App.tsx:859`, `web/src/lib/sessionEvents.ts:315`, `:362`).
3. **Commit before 202.** `dispatchTurnWithRewind` (`internal/server/agent_session.go:1626`) queues the job; `executeTurnJob` runs `commitPendingRewindTurn` (`:1661`) **before** `close(job.persistAck)` — the 202 gate. That single transaction deletes transcript rows from the target onward, inserts the replacement user row with the next `UserSeq`, marks the resource `committed` + `committed_user_seq`, and bumps `history_gen`; afterwards it reconciles a resident agent (`as.messages = result.KeptPrefix`, `:1690`), reseeds the pending queue (`h.sessions.ReplacePending`, `:1693`), broadcasts the authoritative shortened transcript, and only then acks.
4. **No-resident path.** With no resident `agentSession` the same durable transaction runs; the ordinary bootstrap appends the already-persisted user row exactly once (`TestRewindTokenizedAsyncSendWithoutResidentKeepsOneDurableRow`). A commit failure closes `persistAck` with an error and **no turn starts**.
5. **TUI `/rc` bridge.** `RCRequest.RewindToken` plus the `AckCh` ready/error channel (`internal/server/rc_bridge.go:40-45`). The TUI commits through `commitRCRequestRewind` (`internal/tui/model.go:15620`) before rendering or starting `askAgent`, acks on success, and treats `ErrPendingRewindAlreadyCommitted` as an acknowledgment without a second turn (`internal/tui/model.go:4746-4754`); a bridge failure never starts a turn.
6. **Lost-response probe.** After a failed tokenized send the client probes `GET /rewinds/{token}` (`web/src/hooks/useChat.ts:198-231`): `committed` → treated as acceptance (clear the record, keep the transcript, **do not resend**), `stale` → clear + prompt to restore again, 404/409/410 → clear the unusable capability but keep the draft. The server side of the same contract is `ErrPendingRewindAlreadyCommitted` handling in `commitPendingRewindTurn` (`internal/server/agent_session.go:1663-1669`): a duplicate token is accepted without appending or starting another turn.
7. **Regressions held.** Slash commands and `!shell` sends still omit the token; `truncateSession` survives in `web/src/api/client.ts:636` for compatibility but no web Restore path calls it.
8. **Tests.** `internal/session/pending_rewind_test.go`, `internal/server/handler_rewind_test.go` (prepare/status/cancel, commit-before-ack, no-resident, failures-don't-append, duplicate-token, RC ack), `internal/tui/rc_rewind_test.go` + `rc_rewind_update_test.go`, `web/src/lib/pendingRewindStore.test.ts`, `web/src/hooks/useChat.rewind.test.tsx`, `web/src/api/client.rewind.test.ts`, `web/src/components/Chat/ChatInput.restore.test.tsx`.

Design sections below are retained as approved; line references in §1 are the pre-implementation evidence they were written against.

---

## 1. Problem & evidence (verified against the current code)

The existing "Restore to input" feature is immediate, lossy, and silent:

- `web/src/components/Chat/MessageBubble.tsx:302` — the Restore action emits a
  `dispatchRestore(sessionId, content, messageIndex)` window event
  (`RESTORE_EVENT`, `web/src/lib/inputRestore.ts`); `ChatInput` replaces the
  draft, and `web/src/components/Chat/ChatPanel.tsx:168-192` owns history
  mutation.
- `ChatPanel.tsx:177` — the listener **silently returns** when
  `slice.hasMore` is true ("indices are not absolute"), so on long paginated
  sessions Restore loads the draft but never truncates, with no user-visible
  error. When it does truncate, it uses the **loaded-window index**
  (`messages[idx]`, i.e. `entry.originalIndex` relative to the window, not the
  full transcript) — see the `windowStartServerIndex` contract in
  `web/src/lib/sessionSearch.ts:40-48`.
- `ChatPanel.tsx:185` — the persist call is
  `api.truncateSession(sessionId, idx)` with **no `host` argument**, so
  restore-truncate on a remote (SSH) project hits the local server instead of
  the host. Errors are fire-and-forget (`.catch(console.warn)`), and the client
  has already optimistically dispatched `TRUNCATE_MESSAGES`, so a failed
  server write leaves disk and UI diverged with no signal.
- `internal/server/handler_truncate.go` — `HandleTruncateSession` loads the
  session, slices `s.Messages`, and calls `session.ReplaceForDir`. It
  rewrites disk **only**: it does not reconcile a resident `agentSession`'s
  in-memory `as.messages`, does not clear/rebuild the pending message queue,
  and does not update TUI/RC bridge memory. A resident agent can later
  re-append or replay the pre-truncate tail (the same class of problem as the
  concurrent-writer incident in `docs/gotchas/session-writers-conflict-recovery.md`).
- Web and Desktop share the same SPA bundle, so every fix below lands once for
  both surfaces. `windowStartServerIndex` is the server index of loaded
  `messages[0]` (−1 = unanchored), and `Message.user_seq`
  (`internal/agent/client.go:120-127`) is a durable per-session monotonic
  sequence when present — this is the preferred durable target identifier.

### User-confirmed semantics (approved)

1. Restore **only** loads the selected user message into the composer. No
   history mutation happens at Restore time.
2. The selected message **and everything after it remain visible** until the
   next **normal chat send**. At that send, they are removed and the
   edited/sent message is appended as a new user message.
3. Pending state (banner + draft) survives tab switch and full reload.
4. The UI shows a compact **Pending rewind** banner with **Cancel**.
5. Cancel restores the exact pre-Restore draft (the composer text that was
   present immediately before Restore was pressed).
6. The resource TTL is **24 hours**.
7. **Decision:** the user explicitly chose a **server-issued resource** over an
   inline-only (client-side deferred truncate) rewind.

---

## 2. Architecture

### 2.1 Client pending-rewind record (localStorage)

- A versioned record (`v: 1`) persisted in `localStorage` keyed by
  `host + sessionId` (same `host\0id` keying convention as
  `web/src/lib/sessionRevision.ts` / `sessionPrefetch`).
- Fields: opaque server token, **current editable draft** (updated as the user
  edits the composer), **previous draft** (exact pre-Restore text, for
  Cancel), target preview (short excerpt of the selected user message), expiry
  timestamp.
- **No immediate history mutation.** The transcript, virtualizer scroll state,
  and autoscroll lock are untouched by Restore/prepare (§3).
- Restoring another message **supersedes** the current pending rewind: prepare
  replaces the server resource (one per session, §2.2) and the client record
  is overwritten with the new token/preview; the previous `previous draft` is
  replaced by the draft current at the moment of the new Restore.
- **Local persistence failure after server prepare:** if the `localStorage`
  write throws (quota/private mode), the client immediately issues `DELETE`
  (cancel) for the just-prepared resource and leaves history and the prior
  draft unchanged — a resource without a durable client record would be
  unreachable after reload, so we fail closed.

### 2.2 Server resource (`pending_rewinds` table)

- One pending rewind **per session**, stored in a dedicated `pending_rewinds`
  table **inside that session's own SQLite store** — **not** session metadata.
  Fields:
  - `token` — 256-bit opaque (32 random bytes, hex-encoded on the wire);
    the only capability required to commit.
  - `session_id`
  - `target_index` — raw absolute index into the full raw transcript.
  - `user_seq` — optional durable `Message.user_seq` of the target (preferred
    resolution key, §2.3).
  - `fingerprint` — SHA-256 of the full raw transcript, computed **server-side**
    at prepare time.
  - `status` — `armed` | `committed` | `stale`.
  - `created_at`, `expires_at` (created + 24h).
  - `committed_user_seq` — the `user_seq` assigned to the inserted user row,
    kept for response-loss recovery (§2.7).
- **Why a dedicated table:** prepare and cancel must NOT move
  `meta.updated_at`, `meta.history_gen`, the stored `revision` token, session
  index ordering, or live snapshots. Touching metadata would trigger
  spurious cross-process refetches
  (`docs/concepts/cross-process-session-sync.md`) and reordering. Only commit
  bumps `history_gen` (§2.5), which is exactly when the revision *should* move.
- The table is created on demand (`CREATE TABLE IF NOT EXISTS`) when the first
  prepare touches an existing session file. Existing sqlite files gain it
  without any meta rewrite.
- **Legacy transcripts** (`.json` / `.ojsonl` / schema-less) must first go
  through the existing verified raw-preserving SQLite migration
  (`session.migrateToSqlite`, `internal/session/session.go:726` — writes the
  new file, verifies it row-for-row, and only then deletes the original)
  **before** a resource can be created. Prepare fails closed if migration
  fails.
- **One resource per session:** a new prepare deletes the previous row for
  that session and inserts the new one (replacement, not accumulation).
- **Expiry:** rows past `expires_at` are pruned lazily (on prepare/status/cancel
  access for that session). TTL = 24h. Prune is a table write and follows the
  locking rule in §2.5; a read-only status check that finds no expired row
  takes no lock.

### 2.3 Target resolution

- The client translates its paginated local index into an **absolute server
  index**: `absolute = windowStartServerIndex + localIndex`
  (`windowStartServerIndex` = server index of loaded `messages[0]`; −1 means
  unanchored and prepare is rejected client-side — the selected message must
  be visible to be Restored, so an unanchored window cannot occur in practice,
  but we guard anyway).
- Server resolution order (runs at prepare **and** again inside the commit
  transaction):
  1. **`user_seq`** when present on the target: locate the row with that
     `user_seq`, and **verify `role == "user"`** plus a content check against
     the supplied preview.
  2. **Fallback (legacy messages without `user_seq`):** exact user **content**
     match near the supplied absolute index (bounded neighborhood scan), still
     requiring `role == "user"`.
  - Resolution failure **at prepare** ⇒ 409 and **no resource is created**;
    resolution failure **at commit** ⇒ see §2.8 row B (marked `stale`, never a
    blind delete).
- The full-raw-transcript **fingerprint** is validated again at commit time:
  if any unseen change occurred (another writer appended, a compact ran, a
  different process truncated), the resource becomes `stale` instead of
  silently deleting newly arrived messages.

### 2.4 API surface

All endpoints are session-scoped and host-routed like every other session
endpoint (remote projects go through `/api/remote/{host}/...` via the existing
proxy):

- `POST /api/sessions/{id}/rewinds` — **prepare.** Body: absolute
  `targetIndex`, optional `userSeq`, target `preview` content. Validates: no
  active turn (409 otherwise), migration done, target resolves; computes the
  fingerprint server-side; replaces any existing resource for the session.
  Returns token + expiry.
- `GET /api/sessions/{id}/rewinds/{token}` — **status.** Returns
  `armed` + expiry, `committed` + `committed_user_seq`, `stale`, or 410 for
  expired/consumed-and-pruned.
- `DELETE /api/sessions/{id}/rewinds/{token}` — **cancel.** Removes the
  resource; no history change; no revision movement.
- `POST /api/sessions/{id}/message` (existing `handleSendMessage`,
  `internal/server/server.go:224`) — the request body gains an optional
  `rewindToken`. Present ⇒ rewind send (§2.5); absent ⇒ normal send.
- The existing immediate `POST /api/sessions/{id}/truncate`
  (`server.go:325`, `handler_truncate.go`) **may remain** for compatibility,
  but **Web Restore must stop using it** (the `ChatPanel.tsx` RESTORE_EVENT
  truncation listener and its `api.truncateSession` call are replaced by the
  deferred flow).

### 2.5 Commit (headless, atomic, before 202)

Commit runs inside the queued turn job, **under the existing per-session
`sessionTurnLock`** (`internal/server/agent_session.go:1573`) and **before
`persistAck` is closed** (the 202 gate, `agent_session.go:1561-1616`).

**Lock ordering (explicit):** the turn job acquires `sessionTurnLock` first
(as today) and holds it across the rewind transaction; the transaction takes
no other application lock inside it (SQLite's own write lock is the innermost
resource). Prepare, cancel, and prune also acquire `sessionTurnLock` **before**
they read turn state or open their table write, matching the turn job's
acquisition order so no lock-order inversion is introduced — the implementing
commit must verify the actual sequence in `runTurn`/`executeTurnJob` and keep
this order. Read-only status checks take no application lock.

One SQLite transaction, in this order:

1. Validate: resource exists, `status == armed`, `expires_at` in the future,
   fingerprint matches the current raw transcript (target resolution as
   backup).
   - Expired ⇒ 410 (row pruned; no writes).
   - Fingerprint/resolution failure ⇒ the transaction (still inside the same
     tx) flips `status` to `stale` and commits that flip with the rollback of
     everything else — the token can never succeed later (§2.8 row B).
   - Any other failure ⇒ full rollback: resource stays `armed`, no transcript
     rows deleted, retryable (§2.8 row C).
2. Delete raw transcript rows **from the target onward**.
3. Insert the new user row with the proper `UserSeq` (one more than the
   highest surviving `user_seq` — `session.NextUserSeq`,
   `internal/session/session.go:343`; matches the `appendUserMessageTail`
   stamping in `internal/session/sqlitestore.go:682-690`).
4. Mark the resource `committed`, store `committed_user_seq`.
5. Bump `meta.history_gen` in the same transaction (the documented contract
   for synchronous shrinks, `internal/session/sqlitestore.go:389`).

On success, before closing `persistAck`:

- Reconcile the resident agent (`as.messages`) to the authoritative prefix.
  The disk already holds the new user row, so the job **skips the normal
  `persistUserMessage` disk append** (no duplicate — same append-once contract
  as `agent_session.go:1682`) while the existing in-memory append adds the
  user message **once**.
- Clear the stale pending message queue, then push this content into it.
- Broadcast the authoritative shortened transcript event so **all** in-process
  clients update (no client-side optimistic truncate anywhere).
- Then close `persistAck` (202) and proceed with `askAgent` as a normal turn.

If no resident agent (`as == nil`), the existing bootstrap path strips the
pending tail and `runTurn` appends once — no special-casing beyond the disk
transaction.

Because commit runs inside the turn transaction before any memory append, a
commit failure means **no turn starts** (see §2.8).

### 2.6 RC / TUI path

- `RCRequest` (`internal/server/rc_bridge.go:31`) gains `RewindToken string`
  plus a **ready/error acknowledgment channel** `AckCh chan error` alongside
  the existing `ResultCh`.
- The TUI runs the **same session transaction** as §2.5 before appending the
  user message and starting `askAgent`; only after persistence succeeds does
  it update the display/bridge transcript from the authoritative prefix and
  write `nil` to `AckCh` (ready).
- The handler waits on `AckCh` for rewind sends; a non-nil error is surfaced
  to the caller. **Failure ⇒ no turn starts.**

### 2.7 Idempotency / response-loss recovery

- A `committed` resource row **remains until its TTL** even after a successful
  commit. `GET /rewinds/{token}` exposes `status: committed` and
  `committed_user_seq`.
- If the commit succeeded but the HTTP response was lost, the client re-queries
  `GET /rewinds/{token}` on retry/timeout; `committed` ⇒ treated as
  **acceptance**: clear the client record, refetch the transcript, **do not
  resend** (no duplicate user row).

### 2.8 Failure semantics

| # | Condition | Status | Server state | Client behaviour |
|---|---|---|---|---|
| A | Active turn at prepare (or target unresolvable / migration failed at prepare) | 409 | **No resource created**; any previously armed resource for the session is left as-is | Draft preserved; no banner armed (or existing banner untouched). User retries after the turn ends. If a send is attempted with an already-armed token while a foreign turn persists new rows, that token fails under row B at commit. |
| B | Fingerprint mismatch or target unresolvable **at commit** (transcript changed) | 409 | Transaction rolls back (no transcript rows deleted); resource marked **`stale`** — **a stale token can never later succeed** | Draft preserved; unusable armed banner cleared; user is asked to Restore again (draft stays in the composer). |
| C | Expired / already consumed-and-pruned | 410 | No resource (pruned) | Preserve draft, clear unusable armed UI, ask user to Restore again. |
| D | Persistence/transport failure during commit | 5xx / timeout | **Transaction rolled back**; token stays `armed` | Banner and draft remain; user can retry the send (§2.7 covers "response lost after success"). |
| E | Normal slash commands and `!shell` sends | — | Not a rewind send: token **not** attached, not consumed | Pending rewind stays armed; tail stays visible until a normal chat send. |

Normal chat sends while a rewind is armed attach the token automatically —
the **next normal chat send** is the commit point (semantics §1.2). Every
other send path (slash commands, `!shell`) omits it.

### 2.9 Rekey (`/reset-id`)

- Server: the rekey flow (existing `session.RekeyForDir` + `SessionManager.Rekey`,
  documented in `docs/concepts/session-rekey-reset-id.md`, behavior pinned by
  `internal/server/handler_reset_id.go`) must also **move/update the
  `pending_rewinds` row's `session_id`** in the same rekey operation so the
  resource follows the new id.
- Client: the localStorage record for the old key is rewritten under the new
  `host + sessionId` key (mirroring `clearSessionRevision` handling of
  `session_rekeyed` in `web/src/lib/sessionEvents.ts`).
- Remote projects: all rewind endpoints route through the host proxy exactly
  like other session endpoints (`X-Window-Id` / host prefix unchanged).

### 2.10 UI

- **Confirmation dialog** (on Restore): states explicitly that history will
  **remain visible until send** and that sending will remove the selected
  message and everything after it.
- **Pending rewind banner** (compact, above the composer): target preview,
  one-line explanation ("history truncates when you send"), **Cancel**
  button. Cancel calls `DELETE`, restores the exact pre-Restore draft, and
  clears the record.
- **Controls disabled while a request is in flight** (prepare, cancel, and the
  token-carrying send) to prevent double-prepare/double-send.
- **Reload hydration:** on session load, read the localStorage record →
  `GET /rewinds/{token}` → `armed`: hydrate draft + banner; `committed`:
  recovery path (§2.7); `stale`/410: clear record, keep draft, prompt
  re-Restore; record with no server row: clear record (fail closed).
- **State cleanup summary:** record cleared on commit acceptance, cancel,
  stale, 410, or missing server row; server row cleared on cancel, commit-TTL
  prune, or expiry prune. Nothing survives past TTL on either side.
- **No optimistic truncate anywhere.** History changes only when the
  authoritative shortened-transcript broadcast arrives (and, on reload, the
  refetched transcript).

---

## 3. Autoscroll & scroll behavior

- **No autoscroll reset occurs at prepare/Restore** — there is deliberately no
  history mutation, no list shrink, and no `SET_MESSAGES`, so the virtualizer,
  at-bottom lock, and tail-follow state are untouched. This is the main reason
  deferred rewind avoids the scroll regressions catalogued in
  `docs/gotchas/autoscroll-bounce.md`.
- **Shrink detection still applies** when the authoritative post-send
  transcript lands: that is a genuine transcript reset (list shrink), and the
  existing lock re-arm / shrink handling for reset+growth described in
  `docs/gotchas/autoscroll-bounce.md` (2026-09-18 lock re-arm note) governs
  the behavior — no new scroll logic is introduced by this feature.

## 4. Not Cloudflare; storage locality

This design is **not** Cloudflare Durable Objects and does **not** use the
Cloudflare Agents SDK. The pending-rewind resource lives in **project-local
SQLite** — the same per-session `.sqlite` file the transcript already lives in
(`~/.local/share/opencode/project/<slug>/sessions/<id>.sqlite`, WAL) — with
all the single-machine, multi-process semantics documented in
`docs/gotchas/session-writers-conflict-recovery.md`.

## 5. Docs consulted — no conflicts

- `docs/gotchas/session-writers-conflict-recovery.md` — **no conflict.** The
  rewind commit is a single transaction under `sessionTurnLock` with
  fingerprint validation (optimistic concurrency at commit), which is exactly
  the write-conflict discipline that document prescribes; prepare/cancel write
  only the dedicated table, never transcript/meta rows.
- `docs/concepts/cross-process-session-sync.md` — **no conflict.** Prepare and
  cancel intentionally do not move `revision` (no spurious cross-process refetch
  waves); commit bumps `history_gen` inside its transaction, so the stored
  revision moves exactly when the transcript actually changed and other
  processes converge via the existing 15s poll. In-process clients get the
  authoritative broadcast immediately.
- `docs/gotchas/autoscroll-bounce.md` — **no conflict.** Per §3: no scroll
  mutation at prepare; the existing reset/shrink handling covers the post-send
  landing.
- `docs/concepts/interrupted-turn-notice.md` — **no conflict.** Prepare does
  not alter the tail, so interrupted detection is unaffected at prepare time.
  At commit, the turn is active (notice suppressed by design), and the post-send
  tail resolves through the normal `TranscriptTailUnfinished` precedence once
  the turn lands.
- `docs/concepts/session-rekey-reset-id.md` — **no conflict**; §2.9 extends
  the documented rekey move set rather than changing it.
- `QA_REPORT.md` contains **no restore/truncate/rewind coverage** — verified
  at spec-writing time by grep for `restore|truncate|rewind` over
  `QA_REPORT.md` (zero matches). No other claims are made about its contents.

## 6. Testing / TDD

**Rule: add failing regression tests before implementation** for every item
below (mutation-verify by reverting the fix where practical, per this
project's established habit).

**Session store**

- Resource persistence round-trip; one-per-session replacement (new prepare
  supersedes old token).
- Expiry (24h) and cancel remove/limit the resource; lazy prune on access.
- Legacy transcript migration is attempted before resource creation and fails
  prepare on migration failure.
- Stale fingerprint ⇒ commit refused, transcript untouched, status `stale`.
- Commit transaction: tail rows deleted from target onward, new user row with
  correct `UserSeq`, `history_gen` bumped, status `committed` +
  `committed_user_seq` recorded — atomically (rollback on induced failure).
- `/reset-id` moves the resource's `session_id`.

**Server**

- Prepare validation: unknown session, out-of-range/unresolvable target,
  active turn ⇒ 409 with **no resource created**.
- Async send with `rewindToken`: commit completes **before** `persistAck`/202;
  on failure the turn never starts and no user row is appended; fingerprint
  mismatch at commit marks `stale` (row B).
- Resident agent reconcile: `as.messages` prefix + pending queue cleared and
  re-seeded; broadcast emitted.
- Response-loss recovery: `GET` returns `committed` + `committed_user_seq`
  after a commit whose response was dropped; resending is refused/no-op.
- Remote proxy: all three endpoints reachable through `/api/remote/{host}`.
- RC path: `AckCh` ready/error; error ⇒ no turn.

**Web**

- Long paginated session: local index → absolute index (`windowStartServerIndex`
  offset) on prepare.
- No immediate truncate: Restore performs zero history mutation.
- Banner shown with preview; Cancel restores the exact previous draft and
  clears state.
- `localStorage` write failure after prepare ⇒ server cancel issued, history
  and prior draft unchanged.
- Reload hydration: draft + banner restored for `armed`; `committed` treated
  as acceptance; 410/stale clears banner but keeps draft.
- Remote host included on every rewind endpoint and the token-carrying send.
- Token included on the next normal send; success clears the record, failure
  retains banner + draft.
- Regressions: normal send, retry, slash commands, and `!shell` behave
  exactly as before (token only on normal chat sends).

## 7. Acceptance criteria

1. Session with **500+ messages**; Restore a **middle** user message:
   history remains fully visible after Restore.
2. Reload: draft and Pending-rewind banner survive.
3. Send the edited version: the selected message and everything after it
   **disappear immediately** (authoritative broadcast) and **stay gone after
   reopening** the session (and after a server restart).
4. Repeat with **Cancel**: draft restored exactly, history untouched, no
   resource left behind (verify `pending_rewinds` empty/pruned).
5. Repeat over **remote SSH** (host-routed endpoints; commit lands on the host).
6. Regressions: short (fully loaded) sessions and normal sends behave
   unchanged.

## 8. Scope & constraints

- **This spec was written as documentation-only and is now implemented.** The
  source, tests, and related docs have since landed (see *As-built notes*);
  nothing else outside the feature was modified by this document.
- Implementation order was TDD per §6; the legacy immediate `/truncate`
  endpoint is left in place for compatibility but Web Restore no longer calls
  it.
- Open questions: none blocking (all semantics above were user-approved; any
  post-approval change to TTL, banner copy, or commit-point behavior requires
  updating this spec first).
