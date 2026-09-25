---
type: Design
title: Last-dispatched model in the status bar — design
description: 'Implemented design spec: per-session last-dispatched model shown in the web/desktop bottom status bar, captured from every dispatch-acknowledging 202 (send, command/continue, retry, permission/question continuation, rewind) plus turn_started.model as the headless server-event fallback; bridged RC 202s report RCBridge.ModelForDispatch (live TUIStatus.MainModel, registration Model fallback); rewind 202 reports the queued job''s model; StatusBar renders only lastDispatchedModel and never falls back to snap.main_model (absent until first dispatch).'
tags:
  - design
  - status-bar
  - model
  - web
  - desktop
  - spec
timestamp: 2026-09-25T13:32:16Z
---
# Last-dispatched model in the status bar — design

**Date:** 2026-09-25
**Status:** **Implemented (2026-09-25).** Store field/action, every dispatch-acknowledging send path (initial send, command/continue, retry, permission/question continuations, rewind), the `turn_started.model` fallback, the bridged `ModelForDispatch` 202 source, and the StatusBar-only rendering all shipped. Tests live in the four `*.lastModel.*` web suites plus `internal/server/handler_last_model_test.go` and the `turn_started.model` assertions in `internal/server/agent_session_turn_test.go` (§8; suites re-verified passing at this update).

> **Reading the citations.** This tree carries extensive concurrent, unrelated
> work-in-progress, so line numbers were correct when written but may drift.
> Treat every `file:NNN` as an anchor to the symbol named beside it and
> re-verify at implementation time; the symbol names are the contract.

## As-built

- **Store:** `SessionSlice.lastDispatchedModel` (`web/src/stores/chatStore.tsx:275`),
  `SET_LAST_DISPATCHED_MODEL` action (`:462`) and reducer (`:1077-1084`, trims and
  refuses an empty value); `REKEY_SESSION` (`:1013-1021`) moves the whole slice so
  the value survives rekey by construction.
- **Send paths (client):** `useChat.sendMessage` reads the 202 body through the
  shared `dispatchedModel()` trim/guard helper (`web/src/hooks/useChat.ts:30`,
  capture at `:186-203`); `retryLastTurn` (`:301-303`), `resolvePermission`
  (`:337-339`) and `submitQuestionAnswers` (`:377-380`) capture their continuation
  202s the same way; `App.sendCommandToSession` does the same for the
  command/`/model`/Continue paths (`web/src/App.tsx:887-896`), rekeying first and
  keying the value to the post-rekey id.
- **New-session keying:** the draft `new-*` 202 records the model **after** the
  `onNewSession`/rekey callback and **keyed to `res.sessionId`**
  (`useChat.ts:188-199`; `App.tsx:886-889`). This is correct under both legal
  rekey orders — response-first (the callback rekeys before the dispatch) and
  SSE-first (`session_started` already rekeyed; the later `REKEY_SESSION` no-ops,
  `chatStore.tsx:1013-1015`, and the dispatch lands on the existing real slice).
  Both orders reduce to the same reducer sequence — rekey, then a dispatch keyed
  to the real id — which the reducer tests in §8 (cases 3–4) pin.
- **Event fallback:** `sessionEvents.ts` handles `turn_started.model`
  (`web/src/lib/sessionEvents.ts:468-479`) and dispatches the same action;
  `turn_done` / `turn_error` handlers are untouched, so the value is retained.
- **Server — `turn_started`:** `publishTurnStarted(sessionID, model string)`
  (`internal/server/agent_session.go:811`) adds `data["model"]` when non-empty;
  its only call site is `runTurn` (`agent_session.go:1084`). This is the
  **headless/server-event fallback** — see §5.1 and R1 for exactly which turns
  emit it.
- **Server — rewind 202:** the tokenized-rewind 202 reports the model handed to
  the queued job (`h.effectiveSessionModel` → `dispatchTurnWithRewind`), **not** a
  stale resident-agent model (`internal/server/handler.go:1332-1352`, with an
  inline comment forbidding the old overwrite).
- **Server — bridged 202s:** both RC-bridge 202 responses (async send
  `handler.go:1367`, rewind `handler.go:1329`) return
  `ChatResponse{Model: rc.ModelForDispatch()}` — live `TUIStatus.MainModel`
  first, registration `RCBridge.Model` only as fallback
  (`internal/server/rc_bridge.go:276-286`).
- **Rendering:** `StatusBar` reads `lastDispatchedModel` from the session slice
  (`web/src/components/common/StatusBar.tsx:149`), derives
  `dispatchedModel = lastDispatchedModel || ""` (`:196-199`, with a comment that
  it is deliberately independent of `snap.main_model`), and renders
  `· last model: …` gated on the value being non-empty (`:288-290`, title
  "Last dispatched model"); the segment is absent while unset.
- **Tests:** `web/src/stores/chatStore.lastModel.test.ts`,
  `web/src/lib/sessionEvents.lastModel.test.ts`,
  `web/src/hooks/useChat.lastModel.test.tsx`,
  `web/src/components/common/StatusBar.lastModel.test.tsx`,
  `internal/server/handler_last_model_test.go`, plus `turn_started.model`
  assertions in `internal/server/agent_session_turn_test.go` (§8).
- **Docs shipped:** `CHANGES.md` entry "Web/desktop status bar shows the last
  dispatched model" (`CHANGES.md:53`); `skills/ocode-web/SKILL.md`
  "Status-bar model semantics".
- **R1 resolved (see §10):** `publishTurnStarted` is called only from `runTurn`
  (`agent_session.go:1084`), and `runTurn` publishes `turn_started` regardless of
  an attached bridge — the bridge only suppresses the headless status-snapshot
  push. Turns the TUI executes through the RC channel never reach `runTurn` and
  emit **no** `turn_started`; they are covered by their 202, which reports
  `rc.ModelForDispatch()`.

## 1. Problem / coupling before this change

The bottom status bar (`web/src/components/common/StatusBar.tsx`) and the
right-hand sidebar (`web/src/components/Layout/CoworkSidebar.tsx`) both display
"the model", but they mean different things and were coupled to the same
snapshot:

- `StatusBar` read `snap?.main_model` (rendered in the expanded row as
  `· model: …`), where `snap` is the per-session `TUIStatus` snapshot
  (`tui_status.go` `MainModel`, `json:"main_model"`, `tui_status.go:13`).
- `CoworkSidebar` computes `effectiveModel = tuiStatus?.main_model ||
  sessionModel` (`CoworkSidebar.tsx:167`).

`main_model` is the session's **effective/configured** model — what the server
*would* use on the next dispatch. It changes the moment a model is picked
(`ModelDialog.handleSelect` → `SET_SESSION_MODEL` / `setSessionModel`, see
`docs/gotchas/main-model-pick-also-sets-global-default.md`) and whenever the
config default changes, **without any turn having been dispatched with it**.
It is also the field the sidebar and several dialogs deliberately treat as
authoritative (`ModelDialog.tsx:82-87`, `sessionEvents.ts` explicitly refuses to
propagate snapshot `main_model` globally). The result: the status bar's
"model: …" was not a statement about what the running or last turn actually
used — it tracked a picker/config value, indistinguishable from the sidebar's,
so the two surfaces restate the same snapshot field and neither answered "what
model did this session just dispatch?".

## 2. Goals / non-goals

**Goals**

1. The bottom status bar shows a **separate, per-session
   `lastDispatchedModel`**: the model of the most recent turn *dispatched* for
   that session.
2. It updates **immediately** from the backend's HTTP 202 `ChatResponse.model`
   — at dispatch time, not at turn end — for **every dispatch-acknowledging 202:
   initial send, command/Continue, retry, permission/question continuations,
   and rewind**.
3. It **retains** through `turn_error`, failed sends, and status-snapshot
   refreshes; it is only replaced by a later successful dispatch.
4. A `turn_started.model` event payload acts as a **fallback** for turns this
   client did not initiate through its own 202 (another window's headless turn,
   scheduler/external dispatches, remote-host turns mirrored over the bus, a
   reconnect that missed the 202). It is the **headless/server-event** fallback;
   it does not cover turns the TUI executes over the RC bridge (those are
   covered by their 202, §5.1 / R1).
5. New-session rekey (`new-*` → real id) **preserves** the value — the 202
   records it against the real id after the rekey, in either arrival order.
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
  after a reload (or before the first accepted dispatch) the model segment is
  **absent** until the next dispatch or `turn_started` (§3). No other surface's
  value is substituted.
- No per-message model in the transcript; no server-side "last dispatched"
  registry.

## 3. Chosen semantics: immediate-dispatched

`lastDispatchedModel` (a `SessionSlice` field) means: **the model string
carried by the most recent successful turn-dispatch acknowledgement (HTTP 202
`ChatResponse.model`) or `turn_started` event for this session, as observed by
this client.**

- **Set on 202.** Every dispatch-acknowledging path resolves a `ChatResponse`
  (`server.go:2486-2490`; `Content`, `SessionID`, `Model`): initial send
  (`HandleChat` async 202, `handler.go:1048-1051`; `HandleSendMessage` async
  202, injection 202, sync 200), retry (`handler_retry.go:98`),
  permission-resolve continuation (`handler_permissions_resolve.go:371`),
  question-answer continuation (`handler_questions.go:336`), rewind
  (`handler.go:1352`), and the two RC-bridge 202s (`handler.go:1329`,
  `:1367` — `ModelForDispatch()`, see below). The moment the response resolves,
  the client dispatches `SET_LAST_DISPATCHED_MODEL` with the trimmed, non-empty
  `model` — the bar flips synchronously with acceptance, without waiting on any
  turn event (arrival-order race with `turn_started` in §6.3).
- **Rewind reports the queued job's model.** The rewind 202 echoes the model
  handed to `dispatchTurnWithRewind` (`handler.go:1332-1352`), never the resident
  agent's pre-rewind model — the 202 must identify the dispatch the client just
  accepted, not whichever model a to-be-rebuilt agent happened to hold.
- **Bridged 202s report the live TUI model.** The RC-bridge async-send and
  rewind 202s use `rc.ModelForDispatch()` (`rc_bridge.go:276-286`): the live
  `TUIStatus.MainModel` when present, the registration-time
  `RCBridge.Model` only as fallback. The registration field goes stale as soon
  as the TUI switches model, so it is not used while a live snapshot exists.
- **Set on `turn_started.model` (fallback).** Covers turns not dispatched by
  this client's 202 where the server executed the turn via `runTurn`: another
  window's headless turn, scheduler/external dispatches, remote-host turns
  mirrored over the bus, a reload/reconnect that missed the 202. It is a
  headless/server-event fallback — it does **not** fire for turns the TUI runs
  through the RC channel (§5.1, R1).
- **Only non-empty strings count.** `ChatResponse{}` is a legal response
  (bridge-mode ask resolutions return it with no model —
  `handler_questions.go:139`/`:240`, `handler_permissions_resolve.go:194` — and
  the client-side guard also covers any empty `model`). An empty `model`
  **must not** overwrite the stored value — a guard, not a clear.
- **Never cleared.** Not by `turn_error`, not by a failed/rejected send, not by
  `SET_TUI_STATUS` / `MERGE_SNAPSHOT`, not by the failed-send `SET_ERROR` path.
  Absence of a new dispatch means "keep showing the last one".
- **Display rule in StatusBar:** the model segment renders **only when
  `lastDispatchedModel` is non-empty**, and StatusBar **never reads
  `snap.main_model`** for it. Before the first accepted dispatch observed by
  this client — a fresh session, or right after a page/app reload with no
  in-memory value — the segment is **absent**: no `· last model: …` chip at all,
  not an empty or placeholder value. There is deliberately **no display
  fallback** to the effective/configured model: falling back to `main_model`
  would restore exactly the picker-tracking behavior this design removes (§1),
  making the bar's meaning conditional on which surface happened to populate it
  last. An empty segment until first dispatch is the honest state — the session
  genuinely has no *dispatched* model observable by this client.

Consequence: pick a new model in the sidebar → the bar keeps showing the model
the last turn used until a turn is actually dispatched with the new one, at
which point it updates immediately from the 202.

## 4. Approaches considered

**A. Client-side slice field fed by 202 + `turn_started.model` (chosen).**
The 202 already carries `Model` on every dispatch endpoint; `turn_started` is
already a per-session bus event routed to every subscribed client. Zero new
endpoints, dispatch-time latency, and the store already has the per-session
slice + rekey plumbing. Cost: client-memory only — lost on reload, which
renders as an **absent segment until the next accepted dispatch or
`turn_started`** (§3), never as another surface's value — and one additive
server field on `turn_started`.

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

### 5.1 Server

- **`ChatResponse.Model` is pre-existing on all dispatch paths** — no wire
  change was needed:
  - `HandleChat` async 202 (`handler.go:1048-1051`) and sync 200
    (`:1102-1106`); `HandleSendMessage` live-injection 202 (`:1443`), async
    dispatch 202 (`:1465`), sync 200 (`:1483-1487`).
  - `HandleRetrySession` 202 (`handler_retry.go:83` → `:98`, model =
    `effectiveSessionModel`). Bridged sessions get a 409 here (retry runs in
    the terminal), so no bridged retry 202 exists.
  - Permission-resolve continuation 202 (`handler_permissions_resolve.go:281`
    → `:371`, model = `as.model`) and question-answer continuation 202
    (`handler_questions.go:281` → `:336`). Bridge-mode ask resolutions return
    `200 ChatResponse{}` (`handler_permissions_resolve.go:194`,
    `handler_questions.go:139`/`:240`) — the client guard makes those a no-op.
  - **Rewind 202** (`handler.go:1332-1352`): `model :=
    h.effectiveSessionModel(id)` is handed to `dispatchTurnWithRewind` and
    echoed verbatim. The pre-fix code overwrote it with the resident agent's
    model (`if as := h.lookupAgentSession(id); as != nil { model = as.model }`);
    that overwrite was removed with an inline comment — the 202 must report the
    model the queued job will use, not a stale resident-agent model. Pinned by
    `TestRewindAcceptedResponseUsesDispatchedModel`.
  - **RC-bridge 202s** (async send `handler.go:1367`, bridged rewind
    `handler.go:1329`): `ChatResponse{Model: rc.ModelForDispatch()}`.
    `ModelForDispatch` (`rc_bridge.go:276-286`) returns the live
    `TUIStatus.MainModel` when set and falls back to the registration-time
    `RCBridge.Model` only when the live snapshot is empty. Pinned by
    `TestRCBridgeModelForDispatchPrefersLiveStatus`. Caveat: the *synchronous*
    bridged 200 (`handler.go:1391-1395`) still reports the registration
    `rc.Model` — it is not a 202 and the web client never sees it (both send
    endpoints always pass `async: true`).
- **`publishTurnStarted` (`agent_session.go:811`)** now accepts the model and
  adds `"model": …` to the data map when non-empty; its only call site is
  `runTurn` (`:1084`, where `as.model` is in scope). `publishTurnDone`
  (`:858`) already emitted `"model"` — `turn_done` remains the wrong moment for
  immediate semantics and is not used as a source.
- **Which turns emit `turn_started`:** exactly the turns that execute through
  `runTurn` — headless dispatches from any client (web 202s above, scheduler,
  Telegram, remote-host mirrors). `publishTurnStarted` is *not* bridge-gated:
  a `runTurn` on a session with a bridge attached still publishes
  `turn_started` (only the headless status-snapshot push is skipped) — pinned
  by `TestBridgedTurnTimingStillFlowsOnBus`. Turns the TUI executes through the
  RC channel (`rc.RcCh`) never reach `runTurn`, so those dispatches emit **no**
  `turn_started` (the TUI broadcasts `messages`/`turn_done`/`status` frames,
  not `turn_started`). Do not read this as "every bridged turn emits
  `turn_started`": it does not — RC-channel turns are covered by their 202
  (`ModelForDispatch()`), which is precisely why the bridged 202 needed the
  live-model fix.

### 5.2 Store (`web/src/stores/chatStore.tsx`)

- `SessionSlice.lastDispatchedModel?: string` (`:275`) — alongside the existing
  draft-tab `model?: string` (unrelated field, do not conflate).
- Action `{ type: "SET_LAST_DISPATCHED_MODEL"; sessionId: string; model:
  string }` (`:462`); reducer (`:1077-1084`) trims, returns `state` unchanged
  for an empty value, and otherwise goes through the `updateSession` helper
  (`:583-590`) — one-key immutable update, so other tabs' slices are untouched
  and there is **no new subscription** for `StatusBar` (it already subscribes
  to this exact slice; per the `TODO.md` per-token re-render history, keep it
  that way).
- `REKEY_SESSION` (`:1013-1021`) moves the whole slice object
  (`sessions[newId] = slice`) and no-ops when the old slice is already gone
  (`:1014-1015`), so the value is preserved **by construction** — but only if the
  dispatch was keyed to an id the rekey actually touches. The keying rule is
  §6.1; it is the main race.
- `SET_TUI_STATUS` (`:1053`), `MERGE_SNAPSHOT` (`:1185`), `SET_ERROR` (`:728`),
  `INTERRUPT` (`:1135`) do not touch the field (retain-by-construction).

### 5.3 Send paths (client)

All captures share one helper: `dispatchedModel(response)` returns the trimmed
string or `""` (`web/src/hooks/useChat.ts:30-32`); the dispatch is guarded on
that non-empty result — an empty model never overwrites (§3).

**`useChat.sendMessage` (`web/src/hooks/useChat.ts:138-262`)** — the `.then`
on the 202 (`:186-203`):

- Real session: `api.sendMessage(...)` resolves `ChatResponse` → dispatch
  keyed to the session id.
- Draft `new-*` tab: `onNewSession?.(res.sessionId)` runs **first**
  (`:188-190`, which synchronously rekeys via `App.rekeySession`), then
  `targetSessionId = isRealSession ? sessionId : res.sessionId`
  (`:196`) and the dispatch uses `targetSessionId` (`:199`). The value is
  therefore always recorded against the **real** session id, after the rekey —
  never against the temp id — in both the response-first and SSE-first orders
  (§6.1). A rewind send (`rewindToken`) rides this same `.then`, so the
  rewind 202's model (§5.1) is captured like any other dispatch.
- Failure path (`catch`, `:204-259`) dispatches only `SET_ERROR` /
  `SET_STREAMING:false` (and rewind recovery) — **no model dispatch** (failed
  sends do not update); a 409 pending-ask refusal (`:253`) likewise retains.

**`retryLastTurn` (`useChat.ts:293-315`)** — `api.retrySession` resolves the
202 → dispatch (`:301-303`); the `catch` dispatches only error/streaming state.

**`resolvePermission` (`useChat.ts:330-355`)** — `api.resolvePermission`
resolves the 202 → dispatch (`:337-339`). **`submitQuestionAnswers`
(`useChat.ts:366-397`)** — `api.answerQuestion` resolves the 202 → dispatch
(`:377-380`). Both continuations return a 202 carrying the model the resumed
turn will use (`handler_permissions_resolve.go:371`,
`handler_questions.go:336`); on failure they dispatch only `SET_ERROR` +
re-hydration, no model.

**`App.sendCommandToSession` (`web/src/App.tsx:876-905`)** — commands,
`/model`-argument sends, and the interrupted-turn Continue route here:

- `new-*` branch: `rekeySession(sessionId, result.sessionId, …)` **first**
  (`:886`), then dispatch keyed to `result.sessionId` (`:887-890`).
- Real branch: dispatch keyed to `sessionId` (`:894-896`).
- `catch`: no model dispatch.

**Out of scope:** `executeShell` (not a model turn); `cancelQuestion`
(`useChat.ts:406+`) — its response body is deliberately **not** captured: a
dismissal dispatches no turn, so recording the model the headless dismiss
happens to echo (`handler_questions.go:189`, `Model: as.model`) would misreport
a dispatch that never occurred; the bridge-mode dismiss returns an empty
`ChatResponse{}` anyway (`:139`).

### 5.4 Event fallback (`web/src/lib/sessionEvents.ts`)

`turn_started` is in `SESSION_SCOPED_EVENTS` (`:160`) and handled at
`:468-479`: when `data.model` is a non-empty string it dispatches
`SET_LAST_DISPATCHED_MODEL` keyed to the routed session id, then the existing
`SET_TURN_STATE` / `SET_ERROR null`. Old servers that omit the field take the
guard branch — no behavior change. `turn_done` (`:488-494`) and `turn_error`
(`:495+`) handlers are untouched (retain).

### 5.5 StatusBar rendering (`web/src/components/common/StatusBar.tsx`)

- The slice read (`:149`) already returns the whole slice — `lastDispatchedModel`
  is destructured there (no new subscription).
- The source is `const dispatchedModel = lastDispatchedModel || ""`
  (`:196-199`), with an inline comment that it is deliberately independent of
  `snap.main_model`. StatusBar's model segment is sourced **solely** from
  `lastDispatchedModel`; `snap.main_model` is not read anywhere on this path
  (no display fallback, §3).
- The truthiness gate at the render site (`:288-290`) renders
  `· last model: {dispatchedModel}` (title "Last dispatched model", keeping the
  `reasoning=${thinking_budget}` suffix). With no in-memory value the segment
  is **absent** (fresh session or post-reload until the first accepted
  dispatch) — never an empty chip and never another field's value. Note the
  label changed from the old `· model:` / "Active model" to `· last model:` /
  "Last dispatched model" so the chip self-describes its new semantics;
  collapsed row and layout are otherwise unchanged.

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
   rekey wins (`sessionEvents.ts:298-330`, `REKEY_SESSION` at `:310`) — by the
   time the 202 resolves, the temp slice is already gone and the callback's
   rekey no-ops. **Rule: always dispatch keyed to `res.sessionId`, after
   invoking `onNewSession` when present** (`useChat.ts:188-199`; the App
   command path mirrors it: rekey then dispatch to `result.sessionId`,
   `App.tsx:886-889`). In (a) the rekey has just moved the slice under the
   real id, so the dispatch lands on it; in (b) the real slice already exists.
   Keying to the *temp* id after a rekey would resurrect a ghost slice
   (`updateSession` creates one from `emptySessionSlice`, `chatStore.tsx:588`)
   and lose the value on the visible tab — this is the failure the reducer
   tests in §8 (cases 3–4) pin.
2. **Double rekey.** `REKEY_SESSION` no-ops when the old slice is missing
   (`chatStore.tsx:1014-1015`), so `onNewSession` rekeying after an SSE rekey
   cannot clobber the real slice; the subsequent dispatch to `res.sessionId`
   still lands.
3. **202 vs. `turn_started` arrival order.** Either may arrive first: the turn
   goroutine can emit `turn_started` before the HTTP response reaches the
   client. Both write the same model string for the same dispatch, so
   last-write-wins with equal values is harmless; no ordering guarantee is
   required (and an occasional mismatch — a reconcile landing between the two —
   resolves to whichever arrived last, both of which are legitimate "last
   dispatched" observations per §3).
4. **Failed send.** Network/validation rejection resolves the `catch` path —
   no `SET_LAST_DISPATCHED_MODEL` dispatch, previous value retained. A 409
   (pending ask) also takes the catch (`useChat.ts:253`) — retained.
5. **`turn_error`.** The handler flushes deltas and clears turn state but
   never touches the field — retained by construction (tested, §8).
6. **Empty-model acknowledgements.** Bridge-mode ask resolutions and cancel
   paths return `ChatResponse{}`; the trim/empty guard means they never
   overwrite.
7. **Status snapshots.** `SET_TUI_STATUS` writes `tuiStatus` only. A sidebar
   model pick or config push changing `main_model` can never move the bar —
   StatusBar does not read `snap.main_model` at all (§5.5), so the bar is
   decoupled whether or not the field is set (the decoupling test, §8).
8. **Background re-renders.** The field lives in the already-subscribed slice;
   no new per-token work is added to `StatusBar` (per `TODO.md` history, keep
   it that way).

## 7. Compatibility

- **Wire:** `ChatResponse.model` is pre-existing (no `omitempty` — present,
  possibly empty → client guard). `turn_started.model` is additive; older
  servers omit it (guard branch), older clients ignore it. New client + old
  server: 202 path fully functional, event fallback inert. Old client + new
  server: unaffected.
- **Bridged TUI sessions:** the async-send and rewind 202s return
  `ChatResponse{Model: rc.ModelForDispatch()}` (`handler.go:1367`,
  `:1329`) — live `TUIStatus.MainModel`, registration `RCBridge.Model` only as
  fallback. The synchronous bridged 200 (`handler.go:1391-1395`) still reports
  the registration `rc.Model`, but it is not a 202 and the web client always
  sends `async: true`, so it is outside this feature's capture set. RC-channel
  turns emit no `turn_started` (§5.1); see risk R1.
- **Retry/ask continuations:** headless 202s carry `Model` and are captured;
  bridge-mode ask resolutions return an empty 200 and are ignored by the
  guard.
- **API surface:** none changed; no endpoint, no schema migration, no config.
- **Desktop:** bundle-level only; rebuild required.

## 8. Test plan / shipped coverage

All suites below existed and passed at this spec update (re-verified
2026-09-25: the four web suites 14/14, `internal/server` focused Go tests
green). The repo's failing-test-first / mutation-verify convention applies to
any future change to these paths.

**Store — `web/src/stores/chatStore.lastModel.test.ts` (4)**

1. `SET_LAST_DISPATCHED_MODEL` sets the field on the target session slice
   only — other slices and the global `model` are untouched.
2. `SET_TUI_STATUS` with a changed `main_model` does not alter
   `lastDispatchedModel` (and the slice keeps both fields side by side).
3. **Preservation across rekey:** a value recorded under the `new-*` id
   survives `REKEY_SESSION` to the real id, and the temp slice is gone — the
   whole slice moves, nothing is dropped.
4. **Dispatch after an existing rekey** (the sequence both legal §6.1 orders
   reduce to): the response lands on the real id without recreating the
   temporary slice (no ghost under `new-*`).
   (Cases 3–4 are the reducer-level pins for the new-session keying rule in
   §6.1.)

**Events — `web/src/lib/sessionEvents.lastModel.test.ts` (3)**

5. `turn_started` with `data.model: "openai/gpt-4o"` dispatches
   `SET_LAST_DISPATCHED_MODEL` for that session and stores it on the slice.
6. `turn_started` with an empty / whitespace-only `model` dispatches nothing
   new.
7. `turn_error` leaves the field unchanged.

**Send paths — `web/src/hooks/useChat.lastModel.test.tsx` (5)**

8. Successful `sendMessage` (202 `{model}`) records the value for the session.
9. Rejected send (network error) dispatches no model update and retains the
   previous value.
10. **Retry:** `api.retrySession` 202 `{model}` is recorded.
11. **Permission continuation:** `api.resolvePermission` 202 `{model}` is
    recorded.
12. **Question continuation:** `api.answerQuestion` 202 `{model}` is recorded.

**Rendering — `web/src/components/common/StatusBar.lastModel.test.tsx` (2)**

13. With the field **unset** and a status snapshot carrying `main_model`, the
    segment is **absent** (no model chip) — StatusBar never falls back to
    `snap.main_model`.
14. With a dispatched value set, the segment shows that value, not the
    sidebar's `main_model`.

**Server — `internal/server/handler_last_model_test.go` (2) +
`agent_session_turn_test.go`**

15. `TestRewindAcceptedResponseUsesDispatchedModel`: a rewind 202 reports the
    model handed to the queued job (`effectiveSessionModel`), **not** the
    stale resident-agent model (the resident agent in the fixture is seeded
    with a different model and must not win).
16. `TestRCBridgeModelForDispatchPrefersLiveStatus`: `ModelForDispatch()`
    prefers the live `TUIStatus.MainModel` and falls back to the registration
    `RCBridge.Model` only when the live snapshot is empty.
17. `turn_started` envelope data includes `"model"` equal to the session's
    model — asserted in `TestTurnDoneIncludesTookMs` (headless) and
    `TestBridgedTurnTimingStillFlowsOnBus` (bridge attached, `runTurn`
    still publishes) in `internal/server/agent_session_turn_test.go`.
18. Non-regression: existing 202 `ChatResponse.model` decoding in
    `send_message_routing_test.go` still holds.

**Coverage note (known gap):** the `App.sendCommandToSession` capture
(`App.tsx:886-896`) and the `useChat` draft-tab `targetSessionId` keying have
no dedicated App/hook test — the keying *rule* they follow is pinned
reducer-level by cases 3–4, and the capture shape is identical to the
hook-covered paths (cases 8–12). If either path diverges, add the missing
suite rather than relying on the reducer pins.

## 9. Documentation and rollout

- **This document** (`docs/superpowers/specs/2026-09-25-last-dispatched-model-status-design.md`)
  is the design record.
- **`CHANGES.md`** — one entry: status bar shows the last-dispatched model
  (202 + `turn_started.model`), decoupled from the sidebar's effective model;
  notes the `turn_started` payload addition. *(Landed, `CHANGES.md:53`.)*
- **`skills/ocode-web/SKILL.md`** — file-map/gotcha line: the status bar's
  model segment is driven **solely** by `lastDispatchedModel` and is hidden
  while unset; it never falls back to `snap.main_model`. New send paths must
  dispatch `SET_LAST_DISPATCHED_MODEL` from the 202 body. *(Landed,
  "Status-bar model semantics".)*
- **No user-facing migration.** Web ships with the next bundle build; the
  desktop app needs a rebuild/restart (embedded `web/dist`). Remote hosts need
  no upgrade for the 202 path; the `turn_started.model` fallback appears when
  the host binary is next updated (graceful degrade until then).

## 10. Open risks

- **R1 — RC-bridged turns and `turn_started` (resolved, precise scope).**
  `turn_started` is published only by `runTurn` (`agent_session.go:1084`) —
  and `runTurn` publishes it even when a bridge is attached (the bridge only
  suppresses the headless status push; `TestBridgedTurnTimingStillFlowsOnBus`).
  The gap is RC-*channel* turns: web sends, rewinds and ask continuations on a
  bridged session are executed inside the TUI and never reach `runTurn`, so
  they emit no `turn_started` (the TUI broadcasts `messages`/`turn_done`/
  `status`, not `turn_started`). Those dispatches are covered by their 202,
  which now reports `rc.ModelForDispatch()` — the live TUI model, with the
  registration `Model` only as fallback — so the bar is correct for any client
  that sent the message. Do **not** claim every bridged turn emits
  `turn_started`: TUI-initiated turns observed by a passive web client (no 202
  of its own) still leave the bar stale until that client's next 202 or
  status-pick. Acceptable (the TUI user is not watching the web bar).
- **R2 — Multi-client last-writer-wins.** Two windows dispatching different
  models for one session converge on whichever dispatched last. This matches
  the semantics ("last dispatched") and is accepted.
- **R3 — Value lost on reload.** Deliberate (no persistence, no new endpoint).
  Consequence: the model segment is **absent** from the first page load until
  the session's next accepted dispatch or `turn_started` — there is no display
  fallback to `main_model` or any other field (§3, §5.5). If users report the
  bar "forgetting" (or missing) as a problem, persistence would require
  revisiting approach B — not a fallback.
- **R4 — Continuation turns without a captured 202.** The local client's own
  retry/permission/question 202s **are** captured (§5.3, tests 10–12). What
  remains uncovered: (a) continuations initiated by *another* client emit
  `turn_done.model` but no `turn_started` (they drive turn state directly,
  `handler_permissions_resolve.go:321-339`), so the observing client's value
  can be one model stale; (b) bridge-mode ask resolutions return an empty 200
  and the TUI-run continuation emits no `turn_started`, with the same stale
  result for passive observers. Low impact; note if observed.
- **R5 — Concurrent working-tree WIP.** All line anchors must be re-verified
  at implementation time; run the affected suites in isolation first and
  compare failure sets against a pristine baseline before attributing a failure
  to this change.
- **R6 — Coverage gap on the App command path.** `App.sendCommandToSession`
  and the `useChat` draft-tab keying are covered only indirectly (reducer
  rekey tests 3–4 + the shared capture shape, §8). If the rekey/capture
  ordering in either path changes, those reducer pins will not fail — add a
  direct test when touching `App.tsx:876-905` or `useChat.ts:186-203`.