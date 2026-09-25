# Last-dispatched model status implementation plan

## Goal
Make the shared web/desktop bottom `StatusBar` display the model from the most recent backend-accepted message dispatch, independently of the model selected in `CoworkSidebar`.

Approved semantics:
- Update immediately from the HTTP 202 `ChatResponse.model`.
- Use `turn_started.model` as a server-event fallback.
- A failed submit does not change the displayed value.
- A dispatched turn that later errors retains its model.
- A new-session rekey preserves the value.
- `StatusBar` never reads or falls back to `tuiStatus.main_model`; before the first accepted dispatch the model segment is absent.
- No new endpoint or persistence layer.

## Preconditions / safety
- Re-read target files immediately before editing because the working tree contains concurrent unrelated WIP.
- Do not reset, stash, checkout, or overwrite unrelated changes.
- Keep commits scoped to this feature; do not stage existing `docs/index.md`/`docs/log.md` edits.

## Implementation order (tests first)

### 1. Add failing frontend regression tests
Touch only test files first and run them against the current code to prove the old behavior fails:
- `web/src/components/common/StatusBar.model.test.tsx` (or extend the nearest existing StatusBar test):
  - renders `lastDispatchedModel` from a session slice;
  - does not render `tuiStatus.main_model` when no last-dispatched value exists;
  - changing the sidebar/status snapshot model does not change the bar after a dispatched value exists.
- `web/src/stores/chatStore.lastModel.test.ts`:
  - `SET_LAST_DISPATCHED_MODEL` updates the target session;
  - `REKEY_SESSION` carries the value from temporary to real id;
  - ordinary `SET_TUI_STATUS`/sidebar model changes do not overwrite it.
- `web/src/lib/sessionEvents.lastModel.test.ts`:
  - routes `turn_started.data.model` into the target session;
  - ignores an empty model;
  - `turn_error` leaves the prior value unchanged.
- `web/src/hooks/useChat.lastModel.test.tsx` and/or the closest existing hook test:
  - records `ChatResponse.model` after a successful normal send;
  - does not record a model after a rejected send.
- `App` command/continue send coverage if the existing test harness can assert the response model without broad fixture changes.

### 2. Add the per-session frontend state
Files:
- `web/src/stores/chatStore.tsx`
  - add `lastDispatchedModel?: string` to `SessionSlice`;
  - add `SET_LAST_DISPATCHED_MODEL` action;
  - implement reducer update and preserve the field in `REKEY_SESSION` (verify rekey already spreads/moves the complete slice);
  - do not alter `tuiStatus.main_model` behavior used by the sidebar.
- `web/src/api/types.ts` only if the event payload type needs a named shape; otherwise use a local typed payload.

### 3. Capture the backend response in every web send path
Files:
- `web/src/hooks/useChat.ts`
  - after `api.sendMessage` or `api.chat` resolves successfully, dispatch `SET_LAST_DISPATCHED_MODEL` with `res.model` (trim/ignore empty values).
  - do this before/alongside the new-session rekey callback so the temporary id receives the value and rekey moves it.
  - leave the existing `.catch` path unchanged so rejected submissions do not update the field.
- `web/src/App.tsx`
  - update the command/continue `sendCommandToSession` path from the returned `ChatResponse` as well.
  - preserve temporary-id rekey behavior and existing error handling.

### 4. Add the authoritative event fallback
Files:
- `internal/server/agent_session.go`
  - change `publishTurnStarted` to accept the resolved model and include `model` in its event data;
  - pass the resolved turn model at the existing call site;
  - retain existing timing/session metadata and event ordering.
- `web/src/lib/sessionEvents.ts`
  - in the `turn_started` route, read `data.model`, trim it, and dispatch `SET_LAST_DISPATCHED_MODEL` before/alongside turn state updates;
  - empty/missing model is a no-op;
  - do not clear the field on `turn_done` or `turn_error`.
- `internal/server/event_bus.go` / event type declarations only if the event payload requires a type update.

### 5. Render only the last-dispatched value
File:
- `web/src/components/common/StatusBar.tsx`
  - remove the `main_model` display source;
  - read the per-session `lastDispatchedModel` value;
  - keep the existing conditional rendering so the model segment is absent before the first accepted dispatch;
  - keep the rest of status-bar activity/timing behavior unchanged;
  - update comments/title text to say “Last dispatched model” rather than “Active model”.

### 6. Validate incrementally
After each implementation slice, run the smallest relevant test:
- frontend store/event/status-bar tests;
- hook/App tests;
- Go server turn tests for `turn_started.model`.

Then run formatting and static checks:
- `gofmt` on changed Go files;
- `npm run typecheck` / `tsgo --noEmit` in `web`;
- targeted `go test` for the server package;
- targeted Vitest files;
- `git diff --check`.

## Acceptance checklist
- [x] Bottom bar shows the model from the latest successful backend dispatch.
- [x] Sidebar model changes do not alter the bottom bar.
- [x] Bar has no model segment before the first accepted dispatch and never substitutes `main_model`.
- [x] `turn_started.model` updates the correct session for remote/background/other event-driven turns.
- [x] Failed HTTP submissions leave the previous value unchanged.
- [x] A later `turn_error` retains the dispatched model.
- [x] Temporary-to-real session rekey preserves the value.
- [x] Desktop and web use the same shared frontend behavior.
- [x] No new API endpoint or persistence contract was introduced.
- [x] Regression tests fail against the pre-change implementation and pass after the fix.
- [x] Relevant project documentation is updated without overwriting concurrent docs edits.

## As-built adjustments
- New-session response dispatch is keyed to the real session id after the
  rekey callback, so an SSE-first rekey cannot create a ghost temporary slice.
- Retry, permission, question, and rewind 202 responses are captured too.
- Bridged RC responses use `RCBridge.ModelForDispatch()` (live TUI status model,
  registration model fallback); `turn_started.model` remains the server-event
  fallback for turns executed through `runTurn`.
- Rewind 202 responses report the model passed to the queued job, not a stale
  resident-agent model.

## Validation record
- Focused web tests: 15 passed.
- Focused Go model/turn/rewind tests: passed.
- `go test ./...`: passed.
- `go build ./...`: passed.
- `go vet ./...`: passed.
- `cd web && npm run typecheck`: passed.
- `cd web && npm run build`: passed.
- Full web suite: passed in the final validation run.

## Expected files
Primary:
- `web/src/stores/chatStore.tsx`
- `web/src/hooks/useChat.ts`
- `web/src/App.tsx`
- `web/src/lib/sessionEvents.ts`
- `web/src/components/common/StatusBar.tsx`
- `internal/server/agent_session.go`

Tests:
- nearest StatusBar/store/event/hook/App test files, added or extended without weakening existing coverage.

Docs:
- existing web/desktop skill notes or CHANGELOG only if the implementation changes documented behavior; the approved design spec is already updated and committed.
