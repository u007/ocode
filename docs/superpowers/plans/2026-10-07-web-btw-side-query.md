# Web/Desktop `/btw` Side Query Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make web/desktop `/btw` run an independent, tool-capable side query whose aside, activity, and answer never enter the conversation transcript — matching the TUI — and stream the answer into a docked, non-blocking panel.

**Architecture:** The server's `HandleBtw` stops injecting/appending and instead runs the existing `agent.AskLoopAsync` (the TUI's side-query engine) on the session's live agent, publishing progress over one new session-scoped bus event `btw`. The SPA starts the query from the `/btw` command effect and renders a per-session docked panel fed by a small bus-subscribing store. Nothing the side query produces is persisted.

**Tech Stack:** Go 1.26.1 (`internal/server`, `internal/agent`), server-sent events via `EventBus`, React + TypeScript SPA (`web/src`), Vitest.

**Spec:** `docs/superpowers/specs/2026-10-07-web-btw-side-query-design.md`

**Detail:** decisions, not code — the default; the implementer writes the implementation.

## Global Constraints

- Go 1.26.1; `gofmt`, `go vet`, `go build ./...` must be clean.
- **Never hold `h.mu` across an LLM call, `Step`, compaction, recap, or agent construction.** Build outside the lock.
- **Live `as.messages` reads from an HTTP handler use `as.mu.TryLock()`, never `Lock()`**, with a disk-load fallback.
- **No connection pinning for the query:** reply `202` immediately; progress rides the SSE bus.
- The new event `btw` MUST be added to `sessionScopedEvents` (`internal/server/event_bus.go`) and MUST NOT be added to `liveFrameEvents` (`internal/server/session_manager.go`) — it is ephemeral.
- Do not touch the main agent's tool array or system prompt (prompt-cache stability).
- Do not persist any `system`-role message; this feature persists nothing.
- Reuse `AskLoopAsync`; do not build a bespoke side-query client (it already carries session identity headers, change tracking, redaction, and spend recording).
- `/btw` stays in the web instant-command list (`web/src/lib/instantCommands.ts`).
- New/changed tests must be **mutation-verified**: revert the fix, watch the test fail, restore.
- Shared working tree: stage/commit by explicit path; never a bare `git reset`/`git stash`.
- Tests that override `HOME` use the package's `setHomeTree(t, home)` helper where it exists.
- **The side query must not mutate the main agent's cached prefix.** It runs entirely on a child agent (its own message list, tools, system); nothing is appended to the main agent's `messages`, and no user-role injection into the main prompt is needed.
- The answer is never persisted and is never counted as a turn.
- **New session-keyed state must survive/properly die on `/reset-id`.** The `btwRuns` registry is session-id-keyed, so `HandleResetSessionID` (`internal/server/handler_reset_id.go:89`) must cancel and clear the old id's run; the web `btwStore` must rekey on `session_rekeyed`.
- Goroutines go through `crashguard.Go`; `AskLoopAsync` already does — do not spawn a raw `go func()`.
- **Turn Stop is independent of the side query.** Pressing Stop/Esc cancels the main turn only; the side query keeps streaming until the panel is closed (which cancels it).

## Review Focus

Most likely to bite a real user, most likely first. Each has a test pinned in the owning task.

1. **`/btw` on an idle/evicted session** (no resident agent): must build an agent and answer, not 404 or hang. → Task 2.
2. **`/btw` while the main turn is streaming**: must not disturb the main turn, its live snapshots, or the turn-end save. → Task 2.
3. **Remote (SSH/WSL) session**: request proxied to the host, answer relayed to the browser under the right host. → Tasks 2 + 4.
4. **Rapid double `/btw`, or `/btw` then close**: the first is cancelled, no orphan goroutine, no stale frames rendered. → Tasks 2 + 4.
5. **Side query fails at startup** (e.g. no API key): an error surfaces in the panel instead of a silent hang. → Tasks 2 + 5.
6. **`/reset-id` during a side query**: the run is cancelled and no state is stranded under the old id. → Tasks 2 + 4.
7. **Stop (Esc) during a side query**: cancels only the main turn; the side query keeps streaming until the panel closes. → Task 5.

---

## File Structure

**Server**
- `internal/agent/ask.go` — add exported `BtwExcludedTools` + `FormatSideQueryActivity` (shared with TUI).
- `internal/tui/model.go` — TUI helpers delegate to the agent package (no behavior change).
- `internal/server/handler_btw.go` — new: `HandleBtw` (start side query) + `HandleBtwCancel` + per-session cancel/generation registry.
- `internal/server/handler.go` — remove the old `HandleBtw`.
- `internal/server/event_bus.go` — add `"btw"` to `sessionScopedEvents`.
- `internal/server/server.go` — register `DELETE /api/sessions/{id}/btw` + wrapper methods.
- `internal/server/handler_reset_id.go` — cancel + clear the old id's run on `/reset-id`.
- `internal/server/handler_btw_test.go` — rewritten.

**Web**
- `web/src/api/client.ts` — add `cancelBtw`.
- `web/src/components/Chat/commands.ts` — `/btw` returns a `btw` effect; no `Noted:` message.
- `web/src/lib/btwStore.ts` — new: per-session state + `btw` bus subscription.
- `web/src/components/Chat/BtwPanel.tsx` — new: docked panel.
- `web/src/App.tsx` — apply the `btw` effect + mount the panel.
- Tests: `web/src/lib/btwStore.test.ts`, `web/src/components/Chat/BtwPanel.test.tsx`, `web/src/components/Chat/commands.btw.test.tsx`, `web/src/api/client.btw.test.ts`.

**Docs**
- `skills/ocode-web/SKILL.md`, `CHANGES.md`, `TODO.md`, `docs/concepts/tui-slash-command-queuing.md` (bundle page via the `context` sub-agent).

---

### Task 1: Share the side-query tool list and activity formatter in `internal/agent`

**Risk:** standard — pure refactor + one new unit-tested formatter; no behavior change.

**Files:**
- Modify: `internal/agent/ask.go`
- Modify: `internal/tui/model.go` (`btwExcludedTools` ~:12723, `formatBtwActivity` ~:12765)
- Test: `internal/agent/ask_test.go`

**Interfaces:**
- Produces: `var BtwExcludedTools []string` (package `agent`) — exactly the current 13 names: `question, task, task_status, agent_status, task_cancel, wait, todo_write, todo_update, plan_enter, plan_exit, discover_more, knowledge_lookup, advisor`.
- Produces: `func FormatSideQueryActivity(m Message) string` — for an assistant message with tool calls, one `"→ <name> <args>"` line per call (args trimmed, truncated to 60 chars with `"..."`); `""` otherwise.
- Consumes: `agent.Message`, `Message.ToolCalls[].Function.{Name,Arguments}`.

- [ ] **Step 1: Implement the shared definitions and update the TUI callers, with the formatter test**

Test (in `internal/agent/ask_test.go`):
```go
func TestFormatSideQueryActivity(t *testing.T) {
    m := Message{Role: "assistant", ToolCalls: []ToolCall{{Function: FunctionCall{Name: "read", Arguments: `{"file":"a.go"}`}}}}
    if got := FormatSideQueryActivity(m); !strings.Contains(got, "→ read") { t.Fatalf("got %q", got) }
    if got := FormatSideQueryActivity(Message{Role: "assistant", Content: "hi"}); got != "" { t.Fatalf("want empty, got %q", got) }
}
```
Move the list/format logic into `internal/agent/ask.go`; make `internal/tui/model.go`'s `btwExcludedTools` alias `agent.BtwExcludedTools` and `formatBtwActivity` delegate to `agent.FormatSideQueryActivity` (or replace call sites directly).

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/agent/ -run 'TestFormatSideQueryActivity' -count=1` and `go test ./internal/tui/ -run 'Btw' -count=1`
Expected: PASS (TUI `/btw` tests unchanged and green — proves the refactor is behavior-preserving).

---

### Task 2: Server side query, cancel endpoint, and `btw` event

**Risk:** high — concurrency, agent lifecycle, new wire protocol; red-first.

**Files:**
- Create: `internal/server/handler_btw.go`
- Modify: `internal/server/handler.go` (delete old `HandleBtw` ~:2139-2188)
- Modify: `internal/server/event_bus.go` (add `"btw": true` to `sessionScopedEvents`)
- Modify: `internal/server/server.go` (~:398 routes; add wrapper methods near `handleBtw` ~:2096)
- Modify: `internal/server/handler_reset_id.go` (cancel + clear the old id's `btwRuns` entry)
- Test: `internal/server/handler_btw_test.go`

**Interfaces:**
- Consumes: `agent.BtwExcludedTools`, `agent.FormatSideQueryActivity` (Task 1); `h.getOrCreateAgentSession(id)`; `h.publishBusEvent(event, sessionID, data)`; `agent.AskLoopAsync(messages []Message, opts AskLoopOptions, onResult func(string, error)) func()`.
- Produces:
  - `POST /api/sessions/{id}/btw` → `202` `{"status":"started","generation":<uint64>}`; 404 for unknown session; 400 empty content.
  - `DELETE /api/sessions/{id}/btw` → `200` `{"status":"cancelled"}` (idempotent).
  - Bus event `btw`, `data` = `{generation uint64; phase string; question,text,error string}` with phases `started|activity|delta|done|error`.
  - Handler fields: `btwMu sync.Mutex`, `btwRuns map[string]*btwRun` (or equivalent) where `btwRun` holds the cancel func + generation.

- [ ] **Step 1: Write the failing tests**

In `internal/server/handler_btw_test.go` (rewrite the file; keep the `btwRequest` helper, add a cancel helper):
```go
// start side query: 202, no transcript growth, and btw frames emitted
func TestHandleBtwStartsSideQueryWithoutWritingTranscript(t *testing.T) { ... }
// emits started + done (and activity when the fake client calls a tool)
func TestHandleBtwEmitsBusFrames(t *testing.T) { ... }
// DELETE cancels; a second POST cancels the first
func TestHandleBtwCancelAndReplace(t *testing.T) { ... }
// unknown session -> 404
func TestHandleBtwUnknownSessionStillNotFound(t *testing.T) { ... }
// idle session (no resident agent) still works -> exercises getOrCreateAgentSession
func TestHandleBtwBuildsAgentWhenNotResident(t *testing.T) { ... }
// startup failure (nil/failing client) emits an error frame
func TestHandleBtwStartupErrorEmitsErrorFrame(t *testing.T) { ... }
// /reset-id cancels the old id's run and strands no btwRuns entry
func TestHandleBtwResetIdCancelsRun(t *testing.T) { ... }
```
Use the existing test seams: a fake `LLMClient` (as in current tests), a subscribed bus channel to capture `btw` frames, and `session.LoadForDir` to assert the transcript did not grow.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestHandleBtw' -count=1`
Expected: FAIL (routes/handler not implemented; old handler semantics).

- [ ] **Step 3: Implement `handler_btw.go`, register `btw`, and wire routes**

Implementation decisions to honor:
- Validate body via `readBodyJSON`; 400 on empty.
- `as, err := h.getOrCreateAgentSession(id)`; map the not-found error to 404.
- Snapshot messages: `as.mu.TryLock()` → copy `as.messages` → unlock; on failure, load from disk via `session.LoadForDir(entry.ProjectRoot, id)`. Append `{Role:"user", Content: content}`.
- Generation: increment a per-session counter under `btwMu`; publish `started` with the question.
- Call `AskLoopAsync` with `Tools: nil`, `ExcludedTools: agent.BtwExcludedTools`, `MaxSteps: 8`; `OnMessage` → publish `activity` using `agent.FormatSideQueryActivity` (skip empty); `OnDelta` with `kind == "text"` → publish `delta`; `onResult` → publish `done` (or `error`), then clear the registry entry if it still matches this generation.
- Store the cancel func before returning; a new run cancels the previous.
- Never call `setTurnActive`, `tryEnqueueInjection`, or `AppendUserMessageForDir`.
- `HandleBtwCancel` takes `btwMu`, invokes + removes the entry.
- `server.go`: `POST /api/sessions/{id}/btw` and `DELETE /api/sessions/{id}/btw`, both through `authMiddleware`, plus `handleBtw`/`handleBtwCancel` wrappers calling the handler methods with `r.PathValue("id")`.
- `HandleResetSessionID` (`handler_reset_id.go`): under `btwMu`, invoke + delete the old id's run so the registry is never stranded under the deleted id — the same discipline every other session-keyed map follows on `/reset-id`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestHandleBtw' -count=1; go build ./...; go vet ./internal/server/`
Expected: PASS; build and vet clean.

- [ ] **Step 5: Mutation check**

Revert one guard at a time (e.g. skip the transcript-write removal; skip cancelling the previous run) and confirm the specific test fails, then restore.

---

### Task 3: Web API `cancelBtw` + `/btw` command returns a `btw` effect

**Risk:** standard — wiring a client method and a result field.

**Files:**
- Modify: `web/src/api/client.ts` (~:2773 next to `btwSession`)
- Modify: `web/src/components/Chat/commands.ts` (`CommandResult` ~:266; `handleBtw` ~:1731)
- Test: `web/src/api/client.btw.test.ts`, `web/src/components/Chat/commands.btw.test.tsx`

**Interfaces:**
- Produces: `api.cancelBtw(id: string, host?: string): Promise<{ status: string }>` → `DELETE /api/sessions/${encodeURIComponent(id)}/btw` (host threaded the same way as `btwSession`).
- Produces: `CommandResult.btw?: { sessionId: string; question: string; host?: string }`.
- Consumes: `ctx.api.btwSession`, `activeSessionId(ctx)`.

- [ ] **Step 1: Implement with tests**

Tests:
```ts
// commands.btw.test.tsx
it("returns a btw effect and does not add a Noted message", async () => {
  const result = await dispatchCommand("/btw use tabs", ctxWithSession);
  expect(result.btw).toEqual({ sessionId: "s1", question: "use tabs", host: undefined });
  expect(result.messages ?? []).toHaveLength(0);
});
```
`handleBtw` keeps the usage / "No active session" results; on success it returns the `btw` effect instead of the `Noted:` assistant message. `cancelBtw` mirrors `btwSession`'s fetch shape and host threading.

- [ ] **Step 2: Run the tests**

Run: `cd web && npx vitest run src/components/Chat/commands.btw.test.tsx src/api/client.btw.test.ts`
Expected: PASS.

---

### Task 4: `btwStore` — per-session side-query state over the bus

**Risk:** standard — event-state reduction with a staleness guard.

**Files:**
- Create: `web/src/lib/btwStore.ts`
- Test: `web/src/lib/btwStore.test.ts`

**Interfaces:**
- Consumes: `eventBus.on("btw", handler)` (envelope has `session_id`, `host`, `data`).
- Produces:
  - `type BtwState = { sessionId; host?; question; generation; activity: string[]; answer: string; loading: boolean; error?: string; open: boolean }`
  - `startBtw(sessionId, host, question)` — opens/resets local state (called from the `btw` command effect).
  - `closeBtw(sessionId, host)` — clears state (caller also calls `api.cancelBtw`).
  - `useBtwState(sessionId, host?)` — `useSyncExternalStore` hook.
  - `rekeyBtw(oldId, newId)` — move a session's state on `/reset-id` (mirrors `rekeySessionActivity`, `web/src/lib/commandActivity.ts:80`).
  - `__resetBtwStoreForTests()`.
- Keying: `host\u0000sessionId`.

- [ ] **Step 1: Implement with tests**

Tests (drive envelopes through the store's exported test hook or a mocked `eventBus`):
```ts
it("reduces started/activity/delta/done into panel state", ...);
it("ignores frames from an older generation", ...);
it("keys state per host so the same session id on two hosts does not collide", ...);
it("a started frame opens the panel even without a local startBtw call", ...);
it("rekeyBtw moves state to the new id on /reset-id", ...);
```
Subscribe once at module load (single `eventBus.on("btw", …)`); `started` sets `question`/`generation`/`loading`/`open` and clears `activity`/`answer`; `activity` appends; `delta` appends to `answer`; `done` sets `loading=false`; `error` sets `error` and `loading=false`. Ignore frames whose `generation` is older than the stored one.

- [ ] **Step 2: Run the tests**

Run: `cd web && npx vitest run src/lib/btwStore.test.ts`
Expected: PASS.

---

### Task 5: `BtwPanel` + App wiring

**Risk:** standard — new presentational component + one effect branch.

**Files:**
- Create: `web/src/components/Chat/BtwPanel.tsx`
- Modify: `web/src/App.tsx` (apply `result.btw` near ~:1171-1210; mount near ~:1774, between `AgentPreview` and `ChatInput`)
- Test: `web/src/components/Chat/BtwPanel.test.tsx`, `web/src/App.btw.test.tsx`

**Interfaces:**
- Consumes: `useBtwState`, `closeBtw` (Task 4); `api.cancelBtw` (Task 3); `CommandResult.btw`.
- Produces: `<BtwPanel sessionId={string} host={string | undefined} />`.

Design decisions to honor:
- **Docked, non-blocking** — render above the composer; do not use a Radix `Dialog`; do not move focus or block the composer. Reuse `ScrollArea`, `Card`, `Button`, lucide icons.
- Header `↳ By The Way` + the question + a close (X) button.
- Body: activity lines (`→ tool args`) then the streamed answer; `Thinking…` while `loading` and empty.
- Close (X, and Esc only when focus is within the panel) calls `api.cancelBtw(sessionId, host)` and `closeBtw`.
- `App.handleCommand`: when `result.btw` is present, call `startBtw(result.btw.sessionId, result.btw.host, result.btw.question)`.
- Mount only for the active session; per-session state persists across tab switches.
- `App.rekeySession` (`App.tsx:935`) also calls `rekeyBtw(oldId, newId)`; the server cancels the old run on `/reset-id`, so the moved panel receives no further frames.
- Composer Stop/Esc cancels the main turn only — it must not call `api.cancelBtw` (the side query is independent).

- [ ] **Step 1: Implement with tests**

Tests:
```ts
// BtwPanel.test.tsx
it("renders the question and streamed answer without adding a transcript row", ...);
it("shows Thinking… while loading", ...);
it("close calls api.cancelBtw and clears the panel", ...);
// App.btw.test.tsx
it("opens the panel when /btw returns a btw effect", ...);
```

- [ ] **Step 2: Run the tests**

Run: `cd web && npx vitest run src/components/Chat/BtwPanel.test.tsx src/App.btw.test.tsx`
Expected: PASS.

- [ ] **Step 3: Full affected suites + typecheck/build**

Run: `cd web && npx vitest run src/components/Chat src/lib && npx tsgo --noEmit && npx vite build`
Expected: green (report any pre-existing, unrelated failures distinctly).

---

### Task 6: Documentation

**Risk:** standard — docs only.

**Files:**
- Modify: `skills/ocode-web/SKILL.md` (gotcha 62 + file map: add `btwStore.ts`, `BtwPanel.tsx`)
- Modify: `CHANGES.md` (entry under the current unreleased section)
- Modify: `TODO.md` (the item referencing the old divergence)
- Modify: `docs/concepts/tui-slash-command-queuing.md` (via the `context` sub-agent; `doc_write` takes a path without the `docs/` prefix)

- [ ] **Step 1: Update the skills + changelog + TODO**

Correct gotcha 62: web and TUI `/btw` now share the same side-query semantics; the web does not record the aside into the conversation. Note `/btw` is still instant because the handler never writes the transcript.

- [ ] **Step 2: Update the bundle page via the context sub-agent**

Ask `task(agent=context)` to `doc_write` `concepts/tui-slash-command-queuing.md` so the `/btw` mechanism paragraph reads "both surfaces run a side query" and the "differ in MECHANISM on purpose" text is removed. Then re-derive and verify every `file.go:NNN` anchor on the page by hand (anchors drift silently).

---

## Open Questions

1. **Panel placement** — above the composer (proposed, matches the TUI popup's position over the input) vs the right-hand Cowork sidebar.
2. **Esc scope** — close+cancel only while focus is inside the panel (proposed), to avoid stealing Esc from the composer.
3. **Auto-close on `done`?** TUI keeps the popup until dismissed. Proposal: keep it (no auto-close) so the user can read/scroll the answer.
4. **Background sessions** — only the active session's panel renders (proposed); runs in other tabs keep streaming and appear when the tab is activated.

## Verification (end of plan)

- `go build ./...`, `go vet ./...`, `gofmt -l` clean.
- `go test ./internal/agent/ ./internal/tui/ ./internal/server/ -run 'Btw|SideQuery' -count=1` green.
- `cd web && npx vitest run src/lib/btwStore.test.ts src/components/Chat/BtwPanel.test.tsx src/components/Chat/commands.btw.test.tsx src/App.btw.test.tsx` green.
- Manual/live check: start `ocode serve`, open a session, run `/btw <question>` while idle and again mid-stream; confirm the panel streams an answer, the transcript gains no row, closing cancels, and a second `/btw` replaces the first.
