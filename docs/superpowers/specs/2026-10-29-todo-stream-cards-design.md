# Todo stream cards — design

Date: 2026-10-29
Status: approved design (implementation not started)
Scope: web/desktop SPA (`web/src`) + `internal/server` + `internal/tool` + chat display config

## 1. Problem

The chat's todo plan is a compact checklist (`web/src/components/Chat/PlanPanel.tsx`,
rendered above the composer in `App.tsx`). It shows status words and a one-line
`result`, so a long turn gives no per-item view of what the agent is actually
doing. Task subagents already stream into inline cards (`TurnParts.tsx` reads
`useAgentRuns`), but only items delegated with `task(todo_item=tN)` have a run;
most items are worked by the main agent and have no live output at all.

The user wants each todo item to be an expandable card in the chat message flow,
with the live output produced while that item was in progress, capped to a
configurable height, and with a chat-display setting for the default view.

## 2. Decisions (from the user)

1. **Persist on the server.** Output survives a page reload and is served to any
   connected client, not captured client-side.
2. **Inline in chat messages.** The cards render in the transcript flow, attached
   to the current turn (immediately after the last user message), not in the
   fixed plan panel above the composer.
3. **Include thinking.** Captured blocks include the model's reasoning, prose,
   tool call headers and tool output.
4. **Two selects** in Settings → Chat display: a view mode for the latest
   running card and a preview height.

## 3. Goals

- Every plan item renders as a card: state glyph, id, text, duration, one-line
  result, and an expandable body.
- The body streams the work attributed to that item: thinking, assistant text,
  tool headers, tool output (including delegated subagent transcripts).
- Only the item the server reports as active auto-opens; the mode setting chooses
  collapsed / partial / expanded. The height setting caps the partial view.
  A manual expand always shows the whole captured output.
- Output is persisted per item and replays after reload, session switch, and
  server restart. Multiple clients converge through SSE.
- Nothing changes in the persisted transcript, the model-facing conversation, or
  the TUI.

## 4. Non-goals

- TUI rendering of these cards. The recorder lives in `internal/server`, so a
  session run only in the TUI (or headless CLI) has no captured output; its
  cards show the stored `result` line.
- A complete audit log. Each item keeps a bounded tail (see §7).
- Markdown rendering of captured prose. The card body is plain text with
  whitespace preserved so streaming does not re-layout on every token.
- Per-item output for work done while no item is `in_progress`. That work is not
  attributable and is dropped (§6.2).

## 5. Architecture

```
agent callbacks (server-owned)
  OnDelta(text|reasoning) ─┐
  OnToolOutput(call,chunk) ─┤
  OnMessage(tool_start/    ├─→ todoOutputRecorder (per session)
             tool_result)  ─┤      ├─ active item attribution
  OnSubAgentMessage(run,m) ─┘      ├─ bounded blocks per item
  OnTodoWritten ── sync plan ──────┤
                                   ├─→ sidecar <project>/.ocode/todo/<sid>.outputs.json
                                   └─→ SSE `todo_output` (bounded item snapshot)

GET /api/sessions/{id}/todo/outputs ──→ initial load / replay
```

The recorder is server-side only. `wireHeadlessAgentCallbacks`
(`internal/server/handler.go:946`) already owns every hook except
`OnSubAgentMessage`; the server currently leaves that unset
(`internal/server/agent_session.go:665`), so this feature wires it. No
`internal/agent` change.

## 6. Capture

### 6.1 Recorder lifecycle

- `Handler` gains a `todoOutputs *todoOutputStore` map keyed by session id
  (`internal/server/todo_output.go`), mirroring `h.toolOutput`.
- Created lazily when a session's callbacks are wired or its first block
  arrives. The project root comes from `h.sessions.SnapshotEntry(sessionID)`.
  Without a root the recorder stays in-memory and does not persist.
- On agent release/eviction (`internal/server/agent_session.go:1007` and
  `:1044`, where `h.toolOutput.dropSession` is called) flush, drop the map entry,
  and clear the open-call table.
- `todo_output` carries `session_id`; add it to `sessionScopedEvents`
  (`internal/server/event_bus.go`) and the web `SESSION_SCOPED_EVENTS`
  (`web/src/lib/sessionEvents.ts:172`). It is **not** added to `liveFrameEvents`
  (`internal/server/session_manager.go:147`): like `todo_updated`, replaying a
  stale item snapshot into a mid-turn reload would show a plan the session has
  moved past. The GET endpoint covers reload.

### 6.2 Active-item attribution

The recorder holds the latest plan summary and recomputes the active item on
every plan write:

- candidates = items with state `in_progress`;
- prefer the last candidate in file order whose `run_id` is empty (main-agent
  work);
- otherwise the last candidate in file order;
- no candidates → no active item.

Ordering matters: `OnTodoWritten` fires after every plan write. The server's
existing hook (`internal/server/handler.go:791`) hands the plan read and bus
publish to a goroutine. That races the first deltas of the item just marked
`in_progress`, which would be dropped. Change the hook to:

1. read the plan synchronously (a few KB; plan writes happen a handful of times
   per turn) and update the recorder's active item;
2. hand the existing `todo_updated` bus publish to the goroutine as today.

Refactor `publishTodoUpdated` (`internal/server/handler.go:1095`) into a helper
that returns the read event so the file is read once, not twice.

When callbacks are wired for an existing session (server restart, agent
rebuild), seed the active item from disk before the turn starts.

### 6.3 Hooks

All hooks are installed in `wireHeadlessAgentCallbacks`; the legacy
`handler_sse.go` chat path wires its own `OnDelta`/`OnMessage` and must call the
same recorder methods so both turn entry points capture. That path does not wire
`OnToolOutput` today, so it captures final tool results but no live tool output;
wire it there too if live streaming is required on the legacy (Telegram) path.

| Hook | Recorder action |
|---|---|
| `OnDelta("reasoning", text)` | append to the active item's open `thinking` block |
| `OnDelta("text", text)` | append to the active item's open `text` block |
| `OnToolOutput(callID, chunk)` | append to that call's open `tool_output` block |
| `OnMessage` assistant with `tool_calls` | append one `tool` header block per call (name + args, one line, capped) |
| `OnMessage` tool role | finalize the call's `tool_output` block: replace streamed text with the authoritative result, `final: true` |
| `OnSubAgentMessage(run, msg)` | append `msg` to `run.TodoItem()`'s item, tagged `run_id`/`run_name`; skip unlinked runs |

Rules:

- **Assistant prose is captured from deltas only**, never from
  `OnMessage.assistant.Content`, or every sentence lands twice. Fallback: if a
  provider produces no delta for an assistant message (non-`*GenericClient`
  backend), append that message's content once when `OnMessage` reports it and no
  text delta arrived since the previous assistant message.
- **Never capture** the plan tools `todowrite`, `todoread`, `todo_update`.
- **Skip a `task`/`agent` tool call (and its result) when its JSON args contain a
  `todo_item` that exists in the plan** — the linked run's transcript covers it.
  Unparseable args are captured normally.
- Tool output chunks are trimmed to one line-byte cap (the server already
  coalesces them in `h.toolOutput`; the recorder applies its own item cap).
- **`OnSubAgentMessage` is a read-only mirror.** It must append to the linked
  item and do nothing else: never forward the child message to the parent's
  `OnMessage`/`OnDelta` (that would persist a child's turns as the parent's),
  and never replace `TaskTool.persistChild` — the child session stays the
  authoritative transcript (`docs/concepts/subagent-transcripts-child-sessions.md`).
- **Wiring is idempotent.** `wireHeadlessAgentCallbacks` runs at agent build and
  again in the permission/question continuations
  (`internal/server/handler_permissions_resolve.go:423`,
  `internal/server/handler_questions.go:283`); the recorder must be looked up,
  never recreated, and the active item seeded only when it has none, or a
  re-wire would erase the turn's captured blocks.

## 7. Storage

New `internal/tool/todo_output_store.go`, beside the plan store:

- Path: `<projectRoot>/.ocode/todo/<sessionID>.outputs.json`.
- Atomic write: temp file in the same directory, `fsync`-free `os.Rename`
  (matching the todo store's write discipline). The recorder is the only writer;
  no flock.
- Schema:

```json
{
  "version": 1,
  "revision": 12,
  "items": {
    "t1": {
      "blocks": [
        {"kind": "thinking", "text": "..."},
        {"kind": "text", "text": "..."},
        {"kind": "tool", "tool": "bash", "call_id": "call_1", "text": "npm test"},
        {"kind": "tool_output", "tool": "bash", "call_id": "call_1", "text": "...", "final": true},
        {"kind": "text", "text": "...", "run_id": "agent-run-1", "run_name": "explore"}
      ],
      "truncated": false,
      "updated_at": "2026-10-29T10:00:00Z"
    }
  }
}
```

- Caps per item: **32 KB of block text** and **200 blocks**. On overflow drop
  whole blocks from the head; if still over, trim the new head block's text from
  its start. Set `truncated: true`. The UI shows a "truncated" hint.
- Cap for the whole sidecar: **256 KB of block text**. Once exceeded, prune the
  oldest blocks across the least-recently-updated items first. A single flush
  therefore writes at most ~256 KB.
- Prune item ids removed by a plan revision (a `todowrite` rewrite). Keep the
  sidecar bounded by the plan, not by history.
- Missing file is not an error (a session with a plan but no captured work).
  A malformed file is reported by the GET, not fatal, and is never silently
  rewritten by a read.
- **Rekey:** `/reset-id` already calls `tool.RekeyTodoSession`
  (`internal/server/handler_reset_id.go:110`). Add
  `tool.RekeyTodoOutputs(root, old, new)` beside it so the sidecar follows the
  plan; drop the old live recorder (the agent is released by the same handler).

## 8. API and events

### 8.1 `GET /api/sessions/{id}/todo/outputs`

- Registered in `Server.registerRoutes()` next to the existing todo route and
  wrapped by the same session-scoped middleware, so `?host=` proxying works
  unchanged for remote projects (capture happens on the host's server).
- Deliberately separate from `todo_updated` / `GET …/todo`: the plan event is
  consumed by the Pulse board for many sessions at once, and per-item output is
  bulky. Folding it in would inflate every Pulse read with a chat-only payload.
- Response:

```json
{
  "session_id": "ses_…",
  "revision": 12,
  "active_item": "t1",
  "items": {
    "t1": {"blocks": [], "truncated": false, "updated_at": "…"}
  }
}
```

- `active_item` is the recorder's attribution target (§6.2), so the client can
  open the same card the output is actually flowing into instead of guessing.

- Live recorder when present; otherwise the sidecar. No plan and no sidecar →
  `{"session_id": ..., "revision": 0, "items": {}}`.
- A malformed sidecar → 500 with the path in the message (never a silent empty
  plan, matching `readTodoUpdatedEvent`'s reporting rule).

### 8.2 SSE `todo_output`

- Payload: one item's bounded snapshot, published once per changed item per
  flush:

```json
{"session_id": "ses_…", "item_id": "t1", "revision": 12, "active_item": "t1",
 "blocks": [], "truncated": false, "updated_at": "…"}
```

- Full-snapshot-per-item (not deltas) keeps the client merge trivial and
  idempotent; the cap bounds the payload.
- Publish outside the recorder lock and outside the flush lock's write section.

### 8.3 Flush cadence

- Streaming appends are coalesced: at most one file write + one event per
  **200 ms** per session. Check `time.Since(lastFlush)` on append; no timer
  goroutine per delta.
- Force a flush on: plan revision change, tool block finalization, `turn_done`,
  session release, and before the GET reads a live recorder.
- A flush copies the changed items under the recorder lock, then writes and
  publishes under a separate `flushMu`.

## 9. UI

### 9.1 Data

- Reuse `useSessionPlan(sessionId)` for plan structure.
- New `web/src/lib/todoOutputs.ts`, shaped like `sessionPlan.ts`:
  - module map keyed by session id → `{revision, items}`;
  - `setTodoOutputItem(sessionId, itemId, snapshot)` ignores a snapshot whose
    plan `revision` is older than the held one;
  - `useTodoOutputs(sessionId)` via `useSyncExternalStore`;
  - `rekeySessionTodoOutputs(oldId, newId)` for `/reset-id` (called beside
    `rekeySessionPlan`).
- New `useTodoOutputsSeed(sessionId, host)` (or a small effect inside
  `PlanCards`): GET on mount for a real session id, re-fetch on
  `eventBus.onReconnect`, mirroring `PlanPanel`'s seed logic. `new-*` ids skip
  the fetch. The entry component stays mounted whenever a plan exists, even
  when the group is collapsed (only card bodies hide), so the seed and the
  event merge keep running.
- `sessionEvents.ts`: handle `todo_output` → `setTodoOutputItem`; add it to
  `SESSION_SCOPED_EVENTS` and `ROUTABLE_EVENTS` (derived).
- `api.getSessionTodoOutputs(sessionId, host?)` in `api/client.ts`.

### 9.2 Inline entry

- Extend `RenderEntry` (`ChatPanel.tsx:312`) with
  `{ kind: "plan"; originalIndex: -1 }`.
- Insert one plan entry immediately **after the last `role === "user"` message
  entry** of the loaded window; if there is no user message, append at the end.
  Only when `plan.items.length > 0`. The entry therefore starts the current turn
  and moves forward on each new user message.
- `getEntryKey` (`ChatPanel.tsx:349`) returns `"plan:current"` for it.
- Exclude `originalIndex < 0` from `entryPosByServerIndex`
  (`ChatPanel.tsx:925`) and from the `latestThinking` scan; `transcriptItemKey`
  and search jump targets must never see the synthetic index.
- `PlanCards` is a memoized child reading `useSessionPlan` +
  `useTodoOutputs`; streaming re-renders only the card subtree.

### 9.3 Cards

New `web/src/components/Chat/PlanCards.tsx`:

- Group header: `ListChecks`, "Plan", `done/total`, pulsing dot while running,
  and a collapse chevron. Collapse state reuses
  `components/Chat/planPanelPersistence.ts` (per session).
- One card per item in file order:
  - header button: state glyph (`TODO_MARK`), `tN`, text (2-line clamp),
    duration when `started`/`finished` are present, one-line result (or
    `FAILED: `/`CANCELLED: ` in destructive color), chevron;
  - body: block list in a `whitespace-pre-wrap` monospace-ish panel,
    `overflow-y-auto`, auto-stick to the bottom while the item is `in_progress`
    and the user has not scrolled up; a "truncated" hint when the server set it;
    thinking blocks muted/italic; tool blocks prefixed with the tool name and a
    one-line command; `run_id`/`run_name` changes insert a small
    "subagent <name>" divider;
    no captured blocks → the result line, else "No output captured".
- Default open state:
  - the item the server reports as `active_item` (falling back to the last
    `in_progress` item in file order when it reports none) follows `todo_card`:
    `collapsed` → closed, `partial` → open and capped,
    `expanded` → open and uncapped;
  - every other card starts closed;
  - a manual toggle overrides the default for that item for the tab's lifetime;
    manual expansion is uncapped.
- Heights for `partial`: `small` = 6 lines, `medium` = 12, `large` = 24,
  implemented as a `max-height` computed from a 1.25 rem line (7.5 / 15 / 30 rem);
  the body scrolls inside it.
- Remove `PlanPanel` from `App.tsx` (the fixed panel above the composer). Keep
  the component's persistence helper for the group. Delete `PlanPanel.tsx` and
  its test, or rewrite the test against `PlanCards`; no dead component ships.

### 9.4 Settings

- `web/src/api/types.ts`: `ChatTodoCardSetting = "collapsed" | "partial" |
  "expanded"`, `ChatTodoCardHeight = "small" | "medium" | "large"`; add
  `todo_card` and `todo_card_height` to `ChatVerbosityOverrides`.
- `web/src/lib/chatVerbosity.ts`: closed sets, defaults
  (`partial` / `medium`), normalization, validation (`Invalid chat display
  override for …`), and `CHAT_DISPLAY_OVERRIDES` untouched (these are not
  expanded/collapsed overrides).
- `internal/config/ocodeconfig.go`: constants, struct fields, `withDefaults`,
  decoder category list (`chatVerbosityOverrideField`), `chatOverrideAllowed`,
  `chatOverrideChoices`, and `ResolveChatVerbosityPolicy` validation. They are
  read from `cfg.Overrides` directly, exactly like `plan_panel`, not surfaced in
  `ChatDisplayPolicy`.
- `ChatDisplayForm.tsx`: replace the "Plan panel" select with:
  - **Todo cards** — Collapsed / Preview (capped) / Expanded;
  - **Preview height** — Small / Medium / Large.
- **Legacy `plan_panel`:** keep accepting and validating it (an old config must
  not fail the strict decoder) but stop rendering the control and stop using the
  value. Its default stays in `withDefaults` so existing configs round-trip.
  Document this as a retired setting.

## 10. Edge cases

| Case | Behavior |
|---|---|
| No plan | No inline entry; endpoint returns empty; no capture |
| Plan exists, no item in progress | Cards show result lines; deltas dropped |
| Two items in progress, one delegated | Main output → the non-delegated item; run output → its linked item |
| Only delegated items in progress | Main narration → the last in_progress item; run output → its linked item |
| `todowrite` rewrite removes an item | Its captured output is pruned from the sidecar on the next flush |
| Session released mid-stream | Final flush before the recorder is dropped |
| `/reset-id` | Sidecar moved; old recorder dropped; client store rekeyed |
| Server restart | GET replays the sidecar; the recorder re-seeds the active item when callbacks are wired |
| Remote project | Capture on the host server; SPA GET/SSE via `?host=` and the host's event stream |
| Sidecar malformed | GET 500 names the path; no silent empty plan |
| Output exceeds caps | Tail kept, `truncated: true`, UI hint |
| Provider without deltas | Assistant content captured once from `OnMessage` fallback |

## 11. Performance

- Re-renders: `PlanCards` is memoized and reads two external stores; the rest of
  the transcript does not re-render on each captured token.
- Disk: ≤ 5 writes/second/session while streaming, each ≤ the 256 KB sidecar cap
  (all items bounded). No writes when no item is active.
- Bus: ≤ 1 item snapshot per flush; cap bounds size.
- Memory: one recorder per live session, bounded identically to the file; dropped
  on release.

## 12. Testing

Go (each new test mutation-verified by reverting the fix in place):

- `internal/tool/todo_output_store_test.go`: round-trip; missing file; malformed
  file; per-item caps/truncation; prune removed items; rekey.
- `internal/server/todo_output_test.go`: attribution to the active item; no
  active item drops; plan tools excluded; linked `task` skipped; run messages
  attributed; assistant-content fallback when no deltas; flush debounce (one
  write per window); flush on release; sidecar contents after a turn.
- `internal/server/handler_todo_output_test.go`: GET shape, empty state,
  malformed 500, live recorder preferred over disk, route registered in the real
  mux (a handler-only test cannot catch a missing route).
- Event membership: `todo_output` in `sessionScopedEvents`, absent from
  `liveFrameEvents`.
- `internal/server/handler_reset_id_test.go`: sidecar moved, old file gone.

Web:

- `web/src/lib/todoOutputs.test.ts`: set/merge, stale revision ignored, rekey.
- `web/src/components/Chat/PlanCards.test.tsx`: three modes, three heights,
  manual expand uncapped, latest-running default, streaming append, auto-scroll,
  truncation hint, result fallback, no-output fallback.
- `web/src/components/Chat/ChatPanel.planEntry.test.tsx`: inserted after the last
  user message; moves on a new user message; absent without a plan; search index
  mapping unaffected.
- `web/src/components/Settings/ChatDisplayForm.test.tsx`: two selects present,
  plan-panel control gone, values saved.
- `web/src/lib/chatVerbosity.test.ts`: defaults and closed-set validation.
- `web/src/lib/sessionEvents.test.ts`: `todo_output` routed to the store.

Full validation: `go build ./...`, `go vet`, `go test ./internal/server/
./internal/tool/ ./internal/config/`, `bun check .` in `web/` (the repo's
typecheck — do not use bare `tsc`), `npm run build`, `npm run test` (affected
suites, then the full suite once).

## 13. Documentation

- `docs/concepts/persistent-todo-plan.md`: add the per-item output sidecar
  (path, schema, caps, attribution rules, rekey), note that capture is
  server-only so TUI/CLI-only sessions have no cards, and the retired
  `plan_panel` setting.
- `skills/ocode-web/SKILL.md`: inline plan entry, `todo_output` membership rules,
  and the settings keys.
- `CHANGES.md`: one entry.
- Both `CHANGES.md` and `skills/ocode-web/SKILL.md` are modified in the main
  tree by concurrent work; the worktree edits must be reconciled at merge.
- The two bundle pages (`docs/concepts/persistent-todo-plan.md` and
  `docs/concepts/subagent-transcripts-child-sessions.md`) are updated through
  the `context` sub-agent (`doc_write`) during implementation, per the
  sole-automated-writer rule. This design record is a planning artifact under
  `docs/superpowers/specs/` (no OKF frontmatter, not listed in `docs/index.md`),
  written directly like every prior spec there.

## 14. Risks

| Risk | Mitigation |
|---|---|
| Synthetic entry breaks search/pagination anchors | Explicit `originalIndex: -1` handling in key/position helpers; dedicated ChatPanel test |
| Synchronous plan read in the write hook adds latency | One small file read per plan write; the bus publish stays async; measure in the handler test |
| Recorder lock held across bus/disk I/O | Copy under the recorder lock, write/publish under `flushMu` after unlocking |
| Sidecar unbounded | Per-item caps + prune on plan revision |
| Streaming growth jitters the transcript | Partial view is a fixed `max-height` scroller; only new items/manual expands change the group height; measureElement already wired |
| `/reset-id` strands the sidecar | Rekey added beside `RekeyTodoSession`, pinned by a test |
| Legacy config rejected after upgrade | `plan_panel` stays accepted and validated |

## 15. Out of scope / future

- Server capture for TUI-only sessions (would move the recorder into
  `internal/agent`).
- Per-turn plan history (the card follows the current turn by design).
- Markdown rendering of captured prose.

## 16. Repo invariants and how this feature respects them

| Invariant (`CLAUDE.md`) | How it is respected |
|---|---|
| Plans are addressed by `tool.TodoScope` (project root + session id) | The recorder resolves the root via `SnapshotEntry` and reads with `tool.ReadTodoSummary(root, sessionID)`; it never uses the TUI's global cursor |
| The main agent is the only plan writer | The recorder is **read-only** on the plan file; it writes only the output sidecar. No new plan mutation path |
| Plan mutations hold the flock across read → verify → write | Untouched. Sidecar writes are atomic temp+rename on a different file, never inside the plan lock |
| Strict parse, never a silent reset | A malformed sidecar is reported by the GET (500 naming the path), never rewritten by a read |
| Re-anchor injection is user-role, never system-role; the prompt prefix is byte-stable | This feature changes **no prompt content and no message roles**. Nothing is injected |
| A session-scoped SSE event is a two-part change (`sessionScopedEvents`; `liveFrameEvents` only when replayable) | `todo_output` goes into `sessionScopedEvents` and **not** `liveFrameEvents`: it is a momentary reading, and the GET endpoint replays state, exactly like `todo_updated` |
| Child messages must never reach the parent's `OnMessage`/`OnDelta`; child sessions are authoritative | `OnSubAgentMessage` is a read-only mirror into the item log; the parent callbacks are untouched and `persistChild` is unchanged |
| `Handler.mu` is a map lock, never a work lock | The recorder has its own mutex; no `h.mu`/`as.mu` is held across a disk write, bus publish, or any per-turn work |
| Project dirs are per-session; never `os.Getwd()` | The recorder uses the session's `ProjectRoot` from the registry; no cwd-relative path |
| Every session-id-keyed map/journal moves on `/reset-id` | The sidecar is rekeyed beside `tool.RekeyTodoSession` and the live recorder is dropped |
| Remote projects are served by the host's server | Capture happens on the host; the SPA GET/SSE carry `?host=` and the host's stream |
| No raw TUI writes; goroutines through `crashguard.Go` | No TUI change and no stdout writes; any new goroutine uses `crashguard.Go` (the existing `OnTodoWritten` hook already does) |

## 17. Files touched and merge overlap

Go (new): `internal/tool/todo_output_store.go` + `_test.go`,
`internal/server/todo_output.go` + `_test.go`,
`internal/server/handler_todo_output.go` + `_test.go`.

Go (modified): `internal/server/handler.go` (wire hooks, split the plan read out
of `publishTodoUpdated`), `internal/server/handler_sse.go` (legacy callbacks),
`internal/server/agent_session.go` (flush/drop on release, seed on wiring),
`internal/server/event_bus.go` (event membership), `internal/server/server.go`
(route), `internal/server/handler_reset_id.go` + test (sidecar rekey),
`internal/config/ocodeconfig.go` + test (two override fields and validation).

Web (new): `web/src/lib/todoOutputs.ts` + test,
`web/src/components/Chat/PlanCards.tsx` + test,
`web/src/components/Chat/ChatPanel.planEntry.test.tsx`.

Web (modified): `web/src/api/types.ts`, `web/src/api/client.ts`,
`web/src/lib/sessionEvents.ts` + test, `web/src/lib/chatVerbosity.ts` + test,
`web/src/components/Chat/ChatPanel.tsx`, `web/src/App.tsx`,
`web/src/components/Settings/ChatDisplayForm.tsx` + test.
`web/src/components/Chat/PlanPanel.tsx` and its test are removed; the group
collapse state keeps `chat/planPanelPersistence.ts`.

Docs: `docs/concepts/persistent-todo-plan.md`,
`docs/concepts/subagent-transcripts-child-sessions.md`,
`skills/ocode-web/SKILL.md`, `CHANGES.md`.

**Uncommitted main-tree work (verified 2026-10-29, not in the worktree):**
terminal appearance (`internal/server/terminal_appearance*`,
`internal/ghosttyconfig/`, `web/src/components/Terminal/*`), JSON tree
(`web/src/components/Preview/Json*`), `internal/config/ocodeconfig.go`,
`internal/server/handler_config.go`, `web/src/components/Chat/TurnParts*`,
`skills/ocode-web/SKILL.md`, `CHANGES.md`, plus untracked
`internal/ghosttyconfig/`, `internal/agent/zz_split_repro_test.go`,
`internal/server/terminal_appearance*.go`,
`web/src/components/Preview/JsonTree.tsx`,
`web/src/components/Terminal/terminalAppearance.test.ts`.

- Any `ocodeconfig.go` hunk overlaps are in the terminal-appearance region
  (line ≥ 1080); this feature edits the chat-verbosity region (~470–700), so the
  hunks do not overlap but the file needs a normal merge. Do **not** copy the
  main-tree file into the worktree.
- `skills/ocode-web/SKILL.md` (their hunk near the tail) and `CHANGES.md`
  (their entry at the top) will likely conflict with this feature's additions;
  resolve by hand at merge.
- `internal/server/handler_config.go` is dirty for terminal config only; this
  feature does not modify it.
- This feature does **not** depend on any of that in-progress work.

