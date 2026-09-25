---
type: Design
title: Last-dispatched model in the status bar — design
description: 'Design spec: per-session last-dispatched model shown in the web/desktop bottom status bar, fed by 202 ChatResponse.model with turn_started.model fallback.'
tags:
  - design
  - status-bar
  - model
  - web
  - desktop
  - spec
timestamp: 2026-09-25T07:18:18Z
---
# Last-dispatched model in the status bar — design

**Date:** 2026-09-25
**Status:** design (approved in conversation; pending spec-review checkpoint)

> **Reading the citations.** This tree carries extensive concurrent, unrelated
> work-in-progress, so line numbers were correct when written but may drift.
> Treat every `file:NNN` as an anchor to the symbol named beside it and
> re-verify at implementation time; the symbol names are the contract.

## 1. Problem / current coupling

The bottom status bar (`web/src/components/common/StatusBar.tsx`) and the
right-hand sidebar (`web/src/components/Layout/CoworkSidebar.tsx`) both display
"the model", but they mean different things and are coupled to the same
snapshot:

- `StatusBar` reads `snap?.main_model` (`StatusBar.tsx:196`, rendered in the
  expanded row at `:285-288` as `· model: …`), where `snap` is the per-session
  `TUIStatus` snapshot (`tui_status.go` `MainModel`, `json:"main_model"`).
- `CoworkSidebar` computes `effectiveModel = tuiStatus?.main_model ||
  sessionModel` (`CoworkSidebar.tsx:167`).

`main_model` is the session's **effective/configured** model — what the server
*would* use on the next dispatch. It changes the moment a model is picked
(`ModelDialog.handleSelect` → `SET_SESSION_MODEL` / `setSessionModel`, see
`docs/gotchas/main-model-pick-also-sets-global-default.md`) and whenever the
config default changes, **without any turn having been dispatched with it**.
It is also the field the sidebar and several dialogs deliberately treat as
authoritative (`ModelDialog.tsx:80-85`, `sessionEvents.ts:314-317` explicitly
refuse to propagate it globally). The result: the status bar's "model: …" is
not a statement about what the running or last turn actually used — it tracks
a picker/config value, indistinguishable from the sidebar's, so the two
surfaces restate the same snapshot field and neither answers "what model did
this session just dispatch?".

## 2. Goals / non-goals

**Goals**

1. The bottom status bar shows a **separate, per-session
   `lastDispatchedModel`**: the model of the most recent turn *dispatched* for
   that session.
2. It updates **immediately** from the backend's HTTP 202 `ChatResponse.model`
   — at dispatch time, not at turn end.
3. It **retains** through `turn_error`, failed sends, and status-snapshot
   refreshes; it is only replaced by a later successful dispatch.
4. A `turn_started.model` event payload acts as a **fallback** so turns this
   client did not initiate (TUI, another window, reconnect after the 202) also
   update the bar.
5. New-session rekey (`new-*` → real id) **preserves** the value.
6. **No new endpoint**; `ChatResponse.model` already exists on the wire.
7. Web UI and desktop app — one change: the desktop shell embeds the same React
   bundle.

**Non-goals**

- `CoworkSidebar`'s `effectiveModel` is unchanged (it answers "what is
  configured", which is correct for a picker surface).
- `StatusPanel.tsx:92` (`snap?.main_model`) and `ModelDialog` precedence are
  unchanged; promoting them to the new value is a possible follow-up, not this
  change.
- The TUI sidebar/status is untouched (it already reports its own model).
- No persistence of the value across page reloads — it is client-memory state;
  after a reload the bar falls back until the next dispatch or `turn_started`
  (§3).
- No per-message model in the transcript; no server-side "last dispatched"
  registry.

## 3. Chosen semantics: immediate-dispatched

`lastDispatchedModel` (new `SessionSlice` field) means: **the model string
carried by the most recent successful turn-dispatch acknowledgement (HTTP 202
`ChatResponse.model`) or `turn_started` event for this session, as observed by
this client.**

- **Set on 202.** Every send path resolves a `ChatResponse`
  (`server.go:2485-2490`; `Content`, `SessionID`, `Model`). The moment it
  resolves, the client dispatches `SET_LAST_DISPATCHED_MODEL` with
  `res.model` — the bar flips synchronously with acceptance, without waiting
  on any turn event (the arrival-order race with `turn_started` is covered in
  §6.3).
- **Set on `turn_started.model` (fallback).** Covers turns not dispatched by
  this client's 202: TUI-initiated turns, another window, a reload that missed
  the response, remote-host turns mirrored over the bus.
- **Only non-empty strings count.** `ChatResponse{}` is a legal response
  (cancel/ack paths such as `handler_questions.go:139`,
  `handler_permissions_resolve.go:194` return it with no model). An empty
  `model` **must not** overwrite the stored value — a guard, not a clear.
- **Never cleared.** Not by `turn_error`, not by a failed/rejected send, not by
  `SET_TUI_STATUS` / `MERGE_SNAPSHOT`, not by the failed-send `SET_ERROR`
  path. Absence of a new dispatch means "keep showing the last one".
- **Display precedence in StatusBar:** `lastDispatchedModel` when set;
  otherwise fall back to `snap?.main_model` (today's behavior — the
  never-dispatched-in-this-client-session state, e.g. right after a reload).
  Once set, the segment no longer tracks `main_model`, which is the whole point
  of the decoupling. An alternative (hide the segment until first dispatch) was
  considered and rejected: an empty model segment after reload reads as a
  regression, and the effective model is the honest best guess for a session
  that has not dispatched yet in this window.

Consequence: pick a new model in the sidebar → the bar keeps showing the model
the last turn used until a turn is actually dispatched with the new one, at
which point it updates immediately from the 202.

## 4. Approaches considered

**A. Client-side slice field fed by 202 + `turn_started.model` (chosen).**
The 202 already carries `Model` on every dispatch endpoint; `turn_started` is
already a per-session bus event routed to every subscribed client. Zero new
endpoints, dispatch-time latency, and the store already has the per-session
slice + rekey plumbing. Cost: client-memory only (lost on reload — covered by
the `main_model` display fallback), and one additive server field on
`turn_started`.

**B. Server-persisted last-dispatched model exposed on the status snapshot**
(e.g. a `last_dispatched_model` field on `TUIStatus`, populated at dispatch).
Rejected: snapshots are pushed at turn boundaries and config changes, not at
dispatch-accept time — it would either be as late as `turn_done` or require a
fresh snapshot push per 202 (extra bus traffic duplicating what the 202
already says); it also needs a persistence home (registry entry or transcript
metadata — the latter collides with the session-writer conflict discipline in
`docs/gotchas/session-writers-conflict-recovery.md`), and it keeps the value
entangled with the snapshot pipeline that caused the coupling in the first
place. The "no new endpoint" constraint doesn't forbid a new snapshot field,
but the latency and persistence costs buy nothing the 202 doesn't already
deliver.

**C. Derive from the transcript** (read the last turn's model from stored
metadata at load). Rejected: there is no per-turn model stored today, adding
one means a metadata write per turn (writer-conflict surface), it only helps
after reload (not dispatch-time), and it cannot cover turns whose model was
never written. Strictly more work for strictly less immediacy.

**D. Keep sourcing `main_model` but stop the sidebar from writing it.**
Rejected: `main_model` is *defined* as the effective model and the sidebar
rightly shows it; "fixing" the coupling by making the snapshot mean something
else would break every consumer (`ModelDialog`, `StatusPanel`,
`sessionEvents` global-seeding refusal). The semantics belong in a new field,
not a redefinition.

## 5. Component / data flow

### 5.1 Server (one additive change)

- `ChatResponse.Model` — **already shipped** on all dispatch paths:
  `HandleChat` async 202 (`handler.go:1047-1051`) and sync 200 (`:1458-1462`),
  `HandleSendMessage` 202 for rewind (`:1327`), live-turn injection
  (`:1417-1419`), async dispatch (`:1439-1440`), sync (`:1458`),
  `HandleRetrySession` (`handler_retry.go:98`), permission-resolve
  continuation (`handler_permissions_resolve.go:371`), question-answer
  continuation (`handler_questions.go:336`). No change needed.
- `publishTurnStarted` (`agent_session.go:807-821`) currently emits
  `session_id`, `started_at`, `session_created_at` — **no model**. Change:
  accept the model (call site is `runTurn` at `:1078`, where `as.model` is in
  scope) and add `"model": …` to the data map. Note `publishTurnDone`
  (`:852-866`) *already* emits `"model"` — `turn_done` is simply the wrong
  moment for immediate semantics, so it is not used as a source.

### 5.2 Store (`web/src/stores/chatStore.tsx`)

- `SessionSlice` gains `lastDispatchedModel?: string` (alongside the existing
  draft-tab `model?: string`, `:287-293` — unrelated field, do not conflate).
- New action `{ type: "SET_LAST_DISPATCHED_MODEL"; sessionId: string; model:
  string }`; reducer via the existing `updateSession` helper (`:568-575`) —
  one-key immutable update, so other tabs' slices are untouched and the
  selector subscription cost is identical to today's (relevant given the
  StatusBar per-token re-render history in `TODO.md:1667+`: this adds **no new
  subscription** — `StatusBar` already subscribes to this exact slice).
- `REKEY_SESSION` (`:922-929`) moves the whole slice object
  (`sessions[newId] = slice`), so the value is preserved **by construction**
  — but only if the dispatch was keyed to an id that the rekey actually moves.
  See §6 for the keying rule; this is the main race.
- `SET_TUI_STATUS`, `MERGE_SNAPSHOT`, `SET_ERROR`, `INTERRUPT` etc. do not
  touch the field (retain-by-construction).

### 5.3 Send paths

**`useChat.sendMessage` (`web/src/hooks/useChat.ts:121-187`)** — currently
discards the response body (`submitPromise.then(() => true)`):

- Real session: `api.sendMessage(...)` resolves `ChatResponse` →
  `.then((res) => { if (res?.model) dispatch(SET_LAST_DISPATCHED_MODEL,
  res.sessionId, res.model); return true; })`.
- Draft `new-*` tab: `api.chat(...)` (`:155-160`) → its `.then` already calls
  `options?.onNewSession?.(res.sessionId)` (which synchronously rekeys via
  `App.rekeySession`). **Dispatch keyed to `res.sessionId` and only after that
  rekey** (§6).
- Failure path (`catch`, `:175-185`) dispatches only `SET_ERROR` /
  `SET_STREAMING:false` — **no model dispatch** (failed sends do not update).

**`App.sendCommandToSession` (`web/src/App.tsx:855-884`)** — commands,
`/model`-argument sends, and the interrupted-turn Continue all route here; it
awaits `api.chat` / `api.sendMessage` and currently uses only
`result.sessionId`:

- `new-*` branch: after `rekeySession(sessionId, result.sessionId, …)`,
  dispatch `SET_LAST_DISPATCHED_MODEL` keyed to `result.sessionId`.
- Real branch: dispatch keyed to `sessionId` (== `result.sessionId`).
- `catch`: no model dispatch.

**Also in scope (same hook, same semantics, each returns a `ChatResponse`
that dispatches a turn):**

- `retryLastTurn` (`useChat.ts:219-241`, `api.retrySession` → 202 with
  `Model`).
- `resolvePermission` (`useChat.ts:252+`, 202 at
  `handler_permissions_resolve.go:371`) and `submitQuestionAnswers`
  (`useChat.ts:284+`, 202 at `handler_questions.go:336`) — these dispatch
  continuation turns; guard on non-empty `model` as above.
- Out of scope: `executeShell` (not a model turn), `cancelQuestion`
  (returns `ChatResponse{}` — the empty guard makes it a natural no-op).

### 5.4 Event fallback (`web/src/lib/sessionEvents.ts`)

`turn_started` is already in `SESSION_SCOPED_EVENTS` (`:160`) and handled at
`:380-385` (`SET_TURN_STATE true`, `SET_ERROR null`). Add: when
`data.model` is a non-empty string, dispatch `SET_LAST_DISPATCHED_MODEL`
keyed to the routed session id. Old servers that omit the field take the
guard branch — no behavior change. `turn_error` and `turn_done` handlers are
untouched (retain).

### 5.5 StatusBar rendering (`web/src/components/common/StatusBar.tsx`)

- The slice read at `:146` already returns the whole slice — add
  `lastDispatchedModel` to the destructure (no new subscription).
- Replace `const mainModel = snap?.main_model || ""` (`:196`) with
  `lastDispatchedModel || snap?.main_model || ""` (display fallback, §3).
- Render site (`:285-288`), collapsed row, titles, and the
  `reasoning=${thinking_budget}` suffix are unchanged.

### 5.6 Remote / desktop

- **Remote SSH/WSL projects:** the 202 arrives through the same
  `/api/remote/{host}` proxy as a plain `ChatResponse` — `Model` needs no
  host-specific handling. `turn_started` arrives via the host's
  `/api/remote/{host}/api/events` stream (`eventBus.ts:256`) into the same
  `routeBusEnvelope` — same dispatch, no host threading (the store key is the
  session id; sessions are unique per host and the event's session id routes
  it to the right slice).
- **Desktop:** same embedded `web/dist` bundle in the Go binary — no
  Wails-specific code. Requires an app rebuild/restart to take effect (the
  running app serves the embedded bundle).

## 6. Race and error handling

1. **202 vs. `session_started` rekey order (new tabs).** Two legal orders:
   (a) the 202 wins — `onNewSession` rekeys `new-*` → real id inside the
   `api.chat().then`, before our dispatch; (b) the SSE `session_started`
   rekey wins (`sessionEvents.ts:217-258`) — by the time the 202 resolves, the
   temp slice is already gone. **Rule: always dispatch keyed to
   `res.sessionId`, after invoking `onNewSession` when present.** In (a) the
   rekey has just moved the slice under the real id, so the dispatch lands on
   it; in (b) the real slice already exists. Keying to the *temp* id after a
   rekey would resurrect a ghost slice (`updateSession` creates one from
   `emptySessionSlice`, `:573`) and lose the value on the visible tab — this
   is the failure the tests in §8 pin.
2. **Double rekey.** `REKEY_SESSION` no-ops when the old slice is missing
   (`:924`), so `onNewSession` rekeying after an SSE rekey cannot clobber the
   real slice; the subsequent dispatch to `res.sessionId` still lands.
3. **202 vs. `turn_started` arrival order.** Either may arrive first: the turn
   goroutine can emit `turn_started` before the HTTP response reaches the
   client. Both write the same model string for the same dispatch, so
   last-write-wins with equal values is harmless; no ordering guarantee is
   required (and an occasional mismatch — a reconcile landing between the two —
   resolves to whichever arrived last, both of which are legitimate "last
   dispatched" observations per §3).
4. **Failed send.** Network/validation rejection resolves the `catch` path —
   no `SET_LAST_DISPATCHED_MODEL` dispatch, previous value retained. A 409
   (pending ask) also takes the catch (`useChat.ts:177-182`) — retained.
5. **`turn_error`.** The handler flushes deltas and clears turn state but
   never touches the field — retained by construction (tested, §8).
6. **Empty-model acknowledgements.** Cancel/ack endpoints return
   `ChatResponse{}`; the truthy guard means they never overwrite.
7. **Status snapshots.** `SET_TUI_STATUS` writes `tuiStatus` only; a sidebar
   model pick or config push changing `main_model` cannot move the bar once
   the field is set (the decoupling test, §8).
8. **Background re-renders.** The field lives in the already-subscribed slice;
   no new per-token work is added to `StatusBar` (per `TODO.md` history, keep
   it that way).

## 7. Compatibility

- **Wire:** `ChatResponse.model` is pre-existing (no `omitempty` — present,
  possibly empty → client guard). `turn_started.model` is additive; older
  servers omit it (guard branch), older clients ignore it. New client + old
  server: 202 path fully functional, event fallback inert. Old client + new
  server: unaffected.
- **Bridged TUI sessions:** `HandleSendMessage`'s bridged async path returns
  `ChatResponse{Model: rc.Model}` (`handler.go:1342`) — covered by the 202
  path. See risk R1 for TUI-initiated turns.
- **API surface:** none changed; no endpoint, no schema migration, no config.
- **Desktop:** bundle-level only; rebuild required.

## 8. Test plan (failing-test-first)

Each case is written first and verified to fail against current code (the
repo's mutation-verify convention), then the minimal change makes it pass.

**Store — `web/src/stores/chatStore.test.tsx`**

1. `SET_LAST_DISPATCHED_MODEL` sets the field on the target session slice
   only — other slices and the global `model` are untouched. *(Fails: action
   unknown.)*
2. `REKEY_SESSION` carries `lastDispatchedModel` from old id to new id.
   *(Fails: field doesn't exist yet.)*
3. `SET_TUI_STATUS` with a changed `main_model` does not alter
   `lastDispatchedModel`; `MERGE_SNAPSHOT` preserves it.

**Events — `web/src/lib/sessionEvents.test.ts`**

4. `turn_started` with `data.model: "p/m"` dispatches
   `SET_LAST_DISPATCHED_MODEL` for that session; without `model` it dispatches
   nothing new. *(Fails: no dispatch today.)*
5. `turn_error` (and the failed-send flow it follows) leaves the field
   unchanged.

**Send paths — `web/src/hooks/useChat.test.tsx` (+ the App send-path suite)**

6. Successful `sendMessage` (202 `{model}`) dispatches the value keyed to the
   real session id. *(Fails: response body discarded at `useChat.ts:168`.)*
7. New-tab send: value ends up under the **real** id when the 202 wins the
   rekey, *and* when SSE `session_started` rekeyed first — no ghost slice
   under the `new-*` id. *(Fails both orders today.)*
8. Rejected send (network error) dispatches no model update and retains the
   previous value; a 202 with empty `model` also retains.
9. `App.sendCommandToSession`: `api.chat` result model lands under the
   rekeyed id; `api.sendMessage` result under the session id.

**Rendering — `web/src/components/common/StatusBar.test.ts(x)`**

10. Renders `lastDispatchedModel` when set; falls back to `snap.main_model`
    when unset; a subsequent `SET_TUI_STATUS` with a different `main_model`
    does **not** change the rendered value once set. *(Fails: `:196` reads
    `main_model` unconditionally.)*

**Server — `internal/server/agent_session_turn_test.go`**

11. `turn_started` envelope data includes `"model"` equal to the session's
    model (extend the existing started-at assertions at `:399-413`).
    *(Fails: field not emitted.)*
12. Confirm existing 202 `ChatResponse.model` assertions still hold
    (`send_message_routing_test.go` already decodes `ChatResponse`) — a
    non-regression check, not new coverage.

## 9. Documentation and rollout

- **This document** (`docs/superpowers/specs/2026-09-25-last-dispatched-model-status-design.md`)
  is the design record.
- **`CHANGES.md`** — one entry: status bar shows the last-dispatched model
  (202 + `turn_started.model`), decoupled from the sidebar's effective model;
  notes the `turn_started` payload addition.
- **`skills/ocode-web/SKILL.md`** — file-map/gotcha line: the status bar's
  model segment is `lastDispatchedModel`-first with `main_model` display
  fallback; new send paths must dispatch `SET_LAST_DISPATCHED_MODEL` from the
  202 body.
- **No user-facing migration.** Web ships with the next bundle build; the
  desktop app needs a rebuild/restart (embedded `web/dist`). Remote hosts need
  no upgrade for the 202 path; the `turn_started.model` fallback appears when
  the host binary is next updated (graceful degrade until then).

## 10. Open risks

- **R1 — TUI-initiated turns on bridged sessions.** The fallback depends on
  `publishTurnStarted` running for the turn; bridged sessions that execute the
  turn through the RC path rather than `runTurn` may not emit it, leaving the
  bar stale until this client's next 202. Acceptable (the TUI user is not
  watching the web bar), but verify during implementation; if bridged turns
  skip `runTurn`, document it here.
- **R2 — Multi-client last-writer-wins.** Two windows dispatching different
  models for one session converge on whichever dispatched last. This matches
  the semantics ("last dispatched") and is accepted.
- **R3 — Value lost on reload.** Deliberate (no persistence, no new endpoint);
  the `main_model` display fallback covers the gap. If users report the bar
  "forgetting", persistence would require revisiting approach B.
- **R4 — Continuation turns without a captured 202.** Permission/question
  continuations initiated by *another* client emit `turn_done.model` but may
  not emit `turn_started` (they drive turn state directly,
  `handler_permissions_resolve.go:324-329`). The local client's own
  resolve/answer 202s are captured (§5.3); a remote-initiated continuation may
  leave the value one model stale. Low impact; note if observed.
- **R5 — Concurrent working-tree WIP.** All line anchors must be re-verified
  at implementation time; run the affected suites in isolation first and
  compare failure sets against a pristine baseline before attributing a failure
  to this change.
