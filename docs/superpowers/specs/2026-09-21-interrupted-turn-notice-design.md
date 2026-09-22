# Interrupted-turn notice in chat — design

**Date:** 2026-09-21
**Status:** design (approved in principle; pending user review of this file)

> **Reading the citations.** Line numbers were correct when written, but this tree is edited
> concurrently (I watched `runTurn`'s body shift by a line mid-review). Treat every `:NNN` as
> an anchor to the symbol named beside it and re-verify at implementation time; the symbol
> names are the contract.

## 1. Problem

A chat whose turn was cut off looks identical to a chat that simply finished. Nothing in
the UI says "the reply never arrived", so the session reads as "it reset / my work is gone".

Reported case: the remote aimsai2 session `ses_2026-09-21-101152-d376c0b6`. A question/ask
was answered and the stored transcript tail is the answered tool row; the app process was
replaced mid-continuation (true 17:23:34 +08, 9 s after the answer was persisted at
17:23:26), so no assistant reply was ever produced. `pending_asks` is derived from the
*unanswered* sentinel, so after a restart the session reports idle and the client shows
nothing. (Separately fixed in `ChatPanel`: a *failed transcript load* no longer renders as
the empty "Start a conversation" state — `web/src/components/Chat/ChatPanel.loadFailure.test.tsx`.)

`HandleAnswerQuestion` (`internal/server/handler_questions.go:201-333`) persists the answer
(`rewriteAskResult`, `:270`) **before** running the continuation (`as.agent.Step`, `:296`),
so "answer recorded, no reply" is a durable, detectable state.

## 2. Goal / non-goals

**Goal.** When a session has settled on an unfinished turn, the chat shows one inline row at
the cut-off point — *"The previous reply was interrupted"* — with a **Continue** button that
re-sends the turn through the normal message path.

**Non-goals (v1).** No OS-level crash log / unclean-shutdown marker (chosen separately); no
auto-resume (always a user click); no TUI surface (see §7).

## 3. Detection rule (server, single source)

### 3.1 Tail classification

A single pure predicate over `[]agent.Message` — so the resident agent's memory and the
decoded stored rows share exactly one rule — classifies the last row as one of:

- **complete** — an `assistant` row with non-empty `content` or a `notice`: a reply landed,
  so the turn is *not* interrupted;
- **waiting** — an unanswered question/permission sentinel on a `tool` row: the dialog owns
  this state, so it is *not* interrupted either;
- **unfinished** — everything else: a `user` row; an answered-ask `tool` row; an `assistant`
  row carrying only `tool_calls`; a content-less `assistant` row; any other `tool` row; a
  truncate/rewind tail.

Only the **last** row matters: an `assistant` row with `tool_calls` has no tool results after
it by construction, and a `tool` tail means no assistant reply ever followed.

### 3.2 Settled-ness gate (the part that must not false-positive)

`interrupted` is true only when the session is **provably idle and unattended**. Four probes
must all agree — and note that they are *different* locks, because the two false-positive
windows below are gated by different ones:

- no active turn (`turnActive`);
- **no turn job in flight** — the per-session turn lock (`h.turnLocks[id]`,
  `agent_session.go:1271-1286`) can be taken. `executeTurnJob` holds that lock for its whole
  duration (persist → bootstrap → turn), so it is the only probe that covers the
  pre-`turnActive` window; the agent lock is *not* held there.
- **not parked on an ask** — the per-session agent lock can be taken **or no resident agent
  exists**. `as.mu` covers the continuation window (`HandleAnswerQuestion` holds it from
  `findPendingSession` through the answer persist); its absence alone means nothing without
  the turn-lock probe above, which is exactly the hole this gate closes.
- the tail is unfinished (§3.1).

Two healthy windows make the naive rule (tail + turnActive only) fire on ordinary traffic —
both verified in source, both fixed by the gate above:

| Window | Why the naive rule fires | Fix |
|---|---|---|
| `executeTurnJob` (`agent_session.go:1358-1386`) persists the user row and closes `persistAck` (the caller's 202) **before** bootstrap and before `runTurn` sets `turnActive` (`:828`); `as.mu` is not taken until `runTurn` (`:798`) | tail = user row, no ask, `turn_active:false` → "interrupted" on **every** normal send to a session with no resident agent | probe the **session turn lock**, which the job holds for its whole duration |
| `HandleAnswerQuestion` (`handler_questions.go:246-296`) holds `as.mu` and persists the answer at `:270`; `setTurnActive(true)` only at `:277` | same tail shape as the real bug, on the healthy path | the **agent-lock** probe fails while `as.mu` is held → busy |

`livePendingAsks` (`handler_session_state.go:106-116`) collapses *"TryLock failed (busy)"* and
*"lock free, no ask"* into `nil`. The new code needs the lock outcomes explicitly, so add a
sibling — e.g. `liveAskState(id) (asks *PendingAsks, settled bool)` — whose `settled` is the
conjunction of the turn-lock probe and the agent-lock probe, plus a small
`sessionTurnInFlight(id)` helper that looks the lock up **without creating it** (a plain map
read under `turnMu`) and `TryLock`s it, releasing immediately: success means no job is in
flight. Both probes are non-blocking, so the `/state` poll can never pin an HTTP connection
behind a turn (the reason `livePendingAsks` uses `TryLock` today). Keep `livePendingAsks` for
its current callers.

### 3.3 Source of the tail

- **Resident agent** (the usual case): classify `as.messages` under the lock — free, and
  more authoritative than disk (no mid-turn/persist skew).
- **No resident agent** (post-restart, idle-evicted — the reported case): classify the
  stored transcript's last row.

Both go through §3.1, so the rule cannot drift.

## 4. Transport

- `internal/session/revision.go`: extend the existing cheap read so one raw SQLite open
  yields both values — `StoredTranscriptStateForDir(wd, id) (revision string, tail []byte, err error)`
  (or equivalent returning the decoded last row). One `meta` SELECT (as today) plus one
  `SELECT data FROM messages ORDER BY seq DESC LIMIT 1`. `StoredRevisionForDir` stays as a
  thin wrapper, so nothing else changes.
  **Scope of the read:** the sqlite store only. Legacy `.ojsonl`/`.json` sessions and
  unreadable sqlite files — for which `StoredRevisionForDir` still serves a file-token
  revision — yield `tailUnfinished = false`: fail open, never invent an interruption from a
  format we cannot classify.
- `sessionStateResponse` (`internal/server/handler_session_state.go:71-82`) gains
  `Interrupted bool \`json:"interrupted,omitempty"\`` next to `pending_asks` / `revision`.
- No new endpoint, no new client request: the client already consumes this payload on
  connect (`reconcileOpenSessions`), on the 15 s revalidation poll (`revalidateSession`), and
  in `hydratePendingAsks`. Remote sessions work unchanged because the **host** computes it.

## 5. Client

- `SessionSlice.interrupted: boolean` (default `false`), set from `state.interrupted` in the
  reconcile/revalidation paths. The server is authoritative (it clears as soon as a turn
  starts or the tail becomes a reply), so the client keeps no clearing logic beyond the
  optimistic hide on click.
- `ChatPanel` renders, at the very end of the transcript (inside the scroll container), a
  single row — *"The previous reply was interrupted"* — with a **Continue** button. It shows
  only when all of these hold: the slice's `interrupted` flag is set; `wasInterrupted` is
  clear; no turn is active and nothing is streaming; no question or permission is pending;
  the transcript has at least one message; and the tab is a real session (not `new-*`) with
  no load error.
  `wasInterrupted` must suppress it: that flag is the live, session-only user-Stop signal
  (and `ChatInput.tsx` treats it as "sending blocked" — `effectiveBusy`/`workBlockedRef`/the
  drain guard), so the two affordances stay separate and never both show.
  Announced as `role="status"` (informational), not `role="alert"` like the load-failure
  block — this is not an error, so it must not interrupt the screen reader.
- **Continue wiring.** `ChatPanel` has no send path, so `App.tsx` passes
  `onContinueInterrupted` implemented next to the existing command dispatch, reusing the
  normal send/queue path with the literal text `"continue"` (same as a typed message:
  busy-queueing, remote-host thread, persistence). Clicking hides the row immediately; the
  `turn_started` frame then keeps it hidden.

## 6. Error handling / accepted behavior

- **Fail-open**: unreadable store, decode failure, or a lock we could not inspect → `false`.
  Never invent an interruption.
- **Waiting ≠ interrupted**: an unanswered sentinel (live or stored) yields `false`; the
  dialog owns that state.
- **Truncate mid-round** (`HandleTruncateSession`) can leave an unfinished tail on purpose;
  after the user walks away it will read as interrupted. Accepted: Continue is a valid
  action there. No suppression marker (would be speculative complexity).
- **Failed bootstrap**: when the agent cannot be built, the job exits with the user row as the
  tail and the message left pending for retry (`h.sessions.PushPending`,
  `agent_session.go:1385`), so an idle session reads as interrupted. The message is right
  ("no reply arrived"), but check the pending-message semantics before shipping: if a queued
  message already means work is in flight, exclude sessions with a pending message from the
  gate so Continue cannot double-queue alongside the retry.
- **Compaction / `/reset-id`**: no case found that leaves a non-terminal tail, but both get a
  table-test row rather than an assumption.
- A stopped turn whose client state was lost (refresh/reload) shows the notice — correct: the
  reply *was* interrupted.

## 7. Out of scope, recorded

- TUI parity: no reconcile-time equivalent exists today; add a `TODO.md` line per repo
  convention rather than silently skipping.
- Desktop crash log / exit-reason marker / unclean-shutdown flag: separate follow-up (it is
  what would upgrade the copy from "interrupted" to a precise cause).

## 8. Testing

- **Go, pure rule**: table test for `transcriptTailUnfinished` — assistant-with-content,
  assistant-with-notice, assistant-with-only-tool_calls, empty assistant, user, tool,
  answered-ask tool row, unanswered sentinel (→ false), empty transcript.
- **Go, handler**: `interrupted:true` for an answered-ask tail with no live agent; `false`
  for a completed turn, a pending ask, `turn_active:true`, and an unreadable/absent store.
  For the two busy windows, drive the **real mechanism**, not an approximation: window 1 by
  exercising `executeTurnJob` far enough that the user row is persisted and bootstrap is in
  flight (no resident agent — the state where `as.mu` is free), or by holding
  `sessionTurnLock(id)` directly; window 2 by holding the agent lock across the state read.
  A test that only holds `as.mu` would pass against the broken rule and prove nothing.
  Mutation-verify by inverting the completed-reply predicate and by dropping each probe
  independently — dropping the turn-lock probe must fail window 1, dropping the agent-lock
  probe must fail window 2.
- **Web**: `ChatPanel` — the notice renders with Continue when `interrupted` is set; it is
  absent while `wasInterrupted`/`turnActive`/streaming/pending-ask/empty/load-error; clicking
  Continue calls the wired send with `"continue"`; a store test pins `state.interrupted →
  slice.interrupted`.
- Suites: `go test ./internal/...`, `cd web && npm run test`, `npm run typecheck`, and a
  race build of `internal/server` (the lock-acquisition change is concurrency-adjacent).

## 9. Docs

- This spec; a `CHANGES.md` entry at implementation time.
- A `docs/concepts/` page for the new response field + the notice flow (one concept per file,
  matching `auto-permission-enforced-categories.md` etc.), written through the context agent
  (docs/ is bundle-owned).
- `TESTING.md` gains the feature under "Tested & Working" when it ships.
