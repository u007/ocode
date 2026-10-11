# Web/Desktop `/btw` as a Side Query (TUI Parity) — Design

- **Date:** 2026-10-07
- **Status:** Draft — awaiting spec approval
- **Classification:** Architectural (new server endpoint + new session-scoped SSE event + new web surface; changes the semantics of an existing command)
- **Owners:** ocode web/desktop
- **Artifact location:** `docs/superpowers/specs/` is the superpowers process convention for
  design/plan artifacts (8+ existing specs there), separate from the curated OKF bundle under
  `docs/concepts/` that only the `context` sub-agent writes. This is a working spec, not a
  bundle page.

## 1. Problem

Web/desktop `/btw` does not behave like the TUI.

**TUI** (`internal/tui/model.go:12642` `handleBtwCmd` → `internal/agent/ask.go:68`
`AskLoopAsync`): `/btw <msg>` runs an **independent side-query agent loop** — its own
fresh LLM client, its own message list, the tool set minus dispatch/interactive tools,
an 8-step cap, permission asks denied non-blockingly. It streams live tool activity and
the answer into a popup. Neither the aside, the activity, nor the answer enter
`m.messages` or the persisted session history.

**Web/desktop** (`internal/server/handler.go:2143` `HandleBtw`): `/btw <msg>` only
prepends `"By the way: "` and **injects the aside into the running turn** (or appends it
to the transcript when idle). The client then shows `Noted: <args>`
(`web/src/components/Chat/commands.ts:1751`). There is **no side query and no answer**.
When the session is idle, `AppendUserMessageForDir` records the note but starts no turn,
so nothing ever answers it.

No server endpoint runs `AskLoopAsync` (grep of `internal/server` returns nothing), and
there is no web popup surface for it. The divergence is currently documented as
deliberate (`TODO.md:338`, `skills/ocode-web/SKILL.md` gotcha 62) — this spec removes it.

## 2. Goal

Web/desktop `/btw` produces a private, answer-producing side query whose aside, activity,
and answer never enter the conversation transcript — matching the TUI's observable
behaviour, with a **docked, non-blocking panel** instead of the TUI's modal popup.

### TUI semantics (parity target)

The observable contract to match (`internal/tui/model.go:12642` + `internal/agent/ask.go:68`):

- Runs an independent child agent with its **own fresh client**; it sees the **full
  current transcript plus the aside** (not just the aside).
- **Tools enabled** (read/grep/bash/…), minus dispatch + interactive tools
  (`question`, `task`, `task_status`, `agent_status`, `task_cancel`, `wait`,
  `todo_write`, `todo_update`, `plan_enter`, `plan_exit`, `discover_more`,
  `knowledge_lookup`, `advisor`), 8-step cap.
- Permission asks are **denied non-blockingly**; auto-permission still runs first.
- Streams live tool activity + streamed text, then the final answer.
- **Ephemeral**: nothing enters the transcript or persisted history; it is not replayed
  after a reload.
- **Instant**: runs while a turn streams (it never touches the main turn's
  `OnDelta`/`OnUsage`), cancelled on dismiss or by a new `/btw`.

### User Expectation Checklist

1. `/btw <msg>` on web/desktop yields an **answer** to `<msg>` (not just `Noted:`).
2. The aside and its answer **do not appear in the chat transcript** and are **not
   persisted** to the session.
3. The side query can use tools (read/grep/bash) and its file edits/bash mutations still
   land in the Changes tab and stay undoable (parity with TUI).
4. It works **while a turn is streaming** and **while idle**, and it works for **remote
   (SSH/WSL) projects**.
5. The answer is shown in a **docked, non-blocking panel** above the composer; the user
   can keep typing/sending while it streams.
6. Closing the panel **cancels** the running side query; a **new `/btw` cancels the
   previous** one.
7. Side-query spend is recorded in the same usage ledger as everything else.
8. `/btw` remains an **instant** command (never queued), including during a main turn.
9. No regression to the main turn's transcript persistence, live snapshots, or turn-end
   save.

### Non-goals

- Changing the TUI `/btw` behaviour (it stays as is).
- Side queries for other slash commands.
- Persisting or replaying side-query output across reloads (the TUI popup is ephemeral
  too).
- A modal/blocking dialog (explicitly rejected in favour of a docked panel).

## 3. Design

### 3.1 Server — command runs a side query

`HandleBtw` (`POST /api/sessions/{id}/btw`, already routed at `server.go:398` and
proxied for remote sessions) changes from "inject/append" to "start side query":

1. Validate `content` (unchanged).
2. Resolve the live agent with `h.getOrCreateAgentSession(id)` (builds one if the session
   is idle/evicted; never holds `h.mu` across construction).
3. Build the side-query messages: a copy of the agent's current transcript
   (`as.messages`, read under `as.mu` with `TryLock` and falling back to a disk load if
   the lock is held) plus a `{role: "user", content: <aside>}` message. Apply the same
   tool-call sequence repair the TUI relies on so a mid-turn snapshot ending on an
   unanswered tool call is valid.
4. Call `agent.AskLoopAsync(messages, opts, onResult)` with options matching the TUI:
   `ExcludedTools: agent.BtwExcludedTools`, `MaxSteps: 8`, plus `OnMessage` and `OnDelta`
   callbacks that publish bus events.
5. Register the returned `cancel` func in a per-session registry (`h`-level
   `btwMu`/`btwCancels map[string]func()`), cancelling and replacing any prior run for
   that session.
6. Respond `202 Accepted` with `{ "status": "started", "generation": <n> }`.

Reusing `AskLoopAsync` (rather than a bespoke server loop) is deliberate: `newSideQueryAgent`
already inherits everything the side query needs, including the guarantees this design
depends on —

- **session identity**: the child is tagged from the parent (`SetSessionID` /
  `SetOpenCodeSessionID`, `internal/agent/ask.go:204-208`) so its fresh client carries
  `X-Opencode-Session` and `x-session-id`; no extra wiring is needed.
- **change tracking**: `shareChangeTrackingFrom(parent)` so side-query edits/bash mutations
  land in the Changes tab and stay undoable.
- **redaction**: registry, enabled state, tier-1 net hook on the child client, tier-2 scanner.
- **permission isolation**: the shared manager with `PermissionAsk` → non-blocking deny.
- **spend**: `RecordSideUsageFromMessages` books the run into the normal ledger.

The aside and answer are **never** written to `as.messages` or the transcript. There is no
call to `tryEnqueueInjection` or `AppendUserMessageForDir` on this path.

Shared definitions move out of the TUI into `internal/agent/ask.go` so both surfaces use
one list:

- `BtwExcludedTools` (exported copy of the current `btwExcludedTools`).
- `FormatSideQueryActivity(agent.Message) string` (the `formatBtwActivity` logic).

The TUI's `btwTools()`/`formatBtwActivity` become thin wrappers (or callers) of these.

### 3.2 Server — cancel endpoint

New route `DELETE /api/sessions/{id}/btw` → invokes and clears the stored cancel func
(kills only the child's own bash processes, as `AskLoopAsync`'s cancel already does).
Respond `200` with `{ "status": "cancelled" }` (idempotent when nothing is running).

`HandleRemoteProxy` forwards both methods (path-prefix proxy); the side query runs on the
host's `serve` and results ride the relayed bus.

### 3.3 Event protocol

One new **session-scoped** bus event type, `btw`, added to `sessionScopedEvents`
(`internal/server/event_bus.go`). Deliberately **not** in `liveFrameEvents`
(`session_manager.go`): like the TUI popup, a reload drops it; replaying a stale aside
would be wrong.

Payload (`data`):

| phase      | fields                          | emitted when                          |
|------------|---------------------------------|---------------------------------------|
| `started`  | `generation`, `question`        | query accepted                        |
| `activity` | `generation`, `text`            | each assistant tool-call message      |
| `delta`    | `generation`, `text`            | each streamed text token              |
| `done`     | `generation`, `text` (answer)   | loop finished successfully            |
| `error`    | `generation`, `error`           | startup error or loop failure         |

`generation` is a per-session monotonic counter so a client can ignore stale frames after
a replace/cancel.

Publishing goes through the existing `h.publishBusEvent("btw", sessionID, data)` helper.

### 3.4 Web — command dispatch

`commands.ts` `handleBtw` stops returning a `Noted:` transcript message. It calls
`ctx.api.btwSession(sessionId, args, ctx.host)` (now "start") and returns a new result
field, e.g.:

```ts
{ handled: true, btw: { sessionId, question: args, host } }
```

`App.handleCommand` opens the docked panel from that effect (alongside the existing
`openModelPicker` / `rekeyTo` / `download` effects). `usage` / "no active session"
messages are unchanged.

`api/client.ts` adds `cancelBtw(id, host?)` (`DELETE /api/sessions/${id}/btw`).

`/btw` stays in `lib/instantCommands.ts` (now even safer — it never writes the
transcript).

### 3.5 Web — docked panel + store

A small per-session module store, `web/src/lib/btwStore.ts` (pattern: `commandActivity.ts`
/ `compactionState.ts`), subscribes to the bus `btw` event via `eventBus.on("btw", …)`,
keyed by `host\u0000sessionId`. State per session:

```ts
{ sessionId, host, question, generation, activity: string[], answer: string,
  loading: boolean, error?: string, open: boolean }
```

Transitions: `started` opens + resets + sets `question`; `activity` appends a line;
`delta` appends to `answer`; `done` sets `loading=false`; `error` sets `error` and
`loading=false`. Frames whose `generation` is older than the session's current generation
are ignored. A `started` frame opens the panel even without a local `btw` effect (e.g. a
second browser tab invoking `/btw`).

A new `BtwPanel` component (`web/src/components/Chat/BtwPanel.tsx`) renders the active
session's side-query state. It is **docked above the composer** in `App.tsx` (between
`ChatPanel` and `ChatInput`, near `AgentPreview` at `App.tsx:1774`), not a modal:

- Header `↳ By the Way` with the question and a close (X) button.
- Scrollable, wrapped body: live activity lines (`→ tool args`) then the streamed answer;
  a "Thinking…" placeholder while loading.
- **Non-blocking:** the composer stays usable; the panel does not steal focus.
- Close (X or Esc while the panel is focused) calls `api.cancelBtw(sessionId, host)` and
  clears local state.
- Renders only for the active session/tab; per-session state is kept so switching tabs
  and back restores it while it runs. State is keyed per session (`host\u0000sessionId`),
  not global; closing a tab clears that session's entry.
- Reuse existing shadcn/ui primitives (`ScrollArea`, `Card`, `Button`, `lucide` icons).
  Do **not** build a Radix `Dialog`: the panel is non-blocking by design.

### 3.6 Concurrency and safety

- **The handler does not touch turn state.** `HandleBtw` never calls `setTurnActive`, never
  starts a turn, and never holds `h.mu` across the query. `AskLoopAsync` runs on its own
  goroutine; the HTTP response returns immediately (`202`). This is why `/btw` can stay
  instant and non-queued on the web.
- The live-transcript read uses `as.mu.TryLock()` (never `Lock()` from an HTTP handler) with
  a disk-load fallback, per `CLAUDE.md`.
- `sessionScopedEvents` gains `btw`; it is deliberately **not** in `liveFrameEvents`
  (ephemeral, like the TUI popup).
- The child agent owns a fresh client, so its deltas can never race the main turn's
  `OnDelta`/`OnUsage` (existing `AskLoopAsync` guarantee).
- One side query per session; a new `/btw` cancels the previous.
- Permission asks from the side query are denied non-blockingly (existing behaviour);
  auto-permission still runs first when enabled.
- Snapshot store / changes registry are shared and concurrency-safe (as for `task`
  children); side-query edits stay undoable.
- No transcript write means none of the `liveAppendStart` / `samePrefix` /
  `ErrTranscriptConflict` hazards the old injection path was designed around.

## 4. Compatibility & migration

- The old `HandleBtw` semantics (inject/append) are removed. Anything depending on the
  aside being recorded in the transcript is intentionally dropped for parity.
- Tests that assert injection/appending are rewritten (see §6). The existing instant-command
  tests stay valid (the command is still instant).
- `docs/concepts/tui-slash-command-queuing.md` and `skills/ocode-web/SKILL.md` gotcha 62
  currently document the difference as intentional; both must be corrected.

## 5. Documentation to update

- `skills/ocode-web/SKILL.md` gotcha 62 + file map (add `btwStore.ts`, `BtwPanel.tsx`).
- `CHANGES.md` entry.
- `TODO.md` item referencing the old divergence.
- `docs/concepts/tui-slash-command-queuing.md` (via the `context` sub-agent; `doc_write`
  takes a path without the `docs/` prefix).
- This spec's sibling implementation plan.

## 6. Testing strategy

**Go**
- `handler_btw_test.go` rewritten:
  - `POST` starts a side query (fake client) and does **not** grow the transcript.
  - Emits `btw` bus frames (`started`/`activity`/`delta`/`done`).
  - `DELETE` cancels a running query; a second `POST` cancels the first.
  - Unknown session → 404.
- `AskLoopAsync`/`BtwExcludedTools`/`FormatSideQueryActivity` unit coverage if not already.
- All new tests mutation-verified (revert the fix → test fails).

**Web**
- `btwStore.test.ts`: frame → state transitions; stale generation ignored; cross-host
  keying.
- `BtwPanel.test.tsx`: renders question/activity/answer, "Thinking…", close cancels.
- `commands.btw` test: returns the `btw` effect and **no** `Noted:` message.
- `instantCommands.test.ts` unchanged (still instant).
- `api` client test for `cancelBtw` (incl. host threading).

## 7. Open questions

1. **Dock placement** — above the composer (proposed) vs in the right-hand Cowork sidebar.
   Proposal: above the composer, mirroring the TUI popup's position over the input.
2. **Esc behaviour** — Esc closes+cancels only while the panel has focus (avoid stealing
   Esc from the composer). Proposal: yes, focus-scoped.
3. **Multiple concurrent panels across tabs** — only the active session's panel is
   rendered; background sessions keep running. Proposal: yes.
