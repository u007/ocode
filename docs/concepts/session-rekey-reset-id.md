---
type: Concept
title: Session Re-key via /reset-id
description: How /reset-id re-keys a conversation to a fresh session id while preserving the transcript, and the coordinated disk/registry/web rekey path.
tags:
  - session
  - rekey
  - reset-id
  - provider
  - conversation
  - transcript
timestamp: 2026-09-20T09:59:36Z
---
## Why

- The provider conversation identity **is** the session id.
  - `GenericClient.opencodeSessionID()` (`internal/agent/client.go`) is sent as `X-Opencode-Session` (opencode/opencode-go) and `x-session-id` (OpenRouter).
  - Providers group prompt-cache affinity, rate-limit buckets, and sticky routing by this id.
- A stale grouping previously forced starting over. `/reset-id` re-keys to a fresh `ses_…` id while keeping the conversation intact.

## Rekey set — all must move together

- **`session.RekeyForDir`** — copies the RAW transcript preserving title/metadata/`created_at`; writes a fresh sqlite session; refreshes the index; deletes the old id (all formats + index rows) via shared `deleteInDir`. Missing transcript ⇒ `ErrNoStoredSession`.
- **`Agent.RekeySession`** — moves debug-log id, provider identity, permissions session, client session. `Agent.RekeyOpenCodeSession` is the TUI variant (debug logs stay process-global).
- **`snapshot.Store.RekeySession`** — migrates journal rows via `Journal.rekeySession` **without** Reset (unlike `SwitchSession`) so the Changes tab undo history survives.
- **`tool.RekeyTodoSession`** — renames `.ocode/todo/<id>.md` and rebinds the in-memory entry.
- **`SessionManager.Rekey`** — moves the registry entry (project root, window binding, pending queue, lastSeq).

## Quiesce — ordering matters

- `POST /api/sessions/{id}/reset-id` (`internal/server/handler_reset_id.go`):
  1. Returns **409** while a turn is active.
  2. Calls `session.FlushForDir(..., 10s)`.
  3. Releases the agent via `sessions.ReleaseAgent(id)` — its `onEvict` drops `h.agents[id]`, `h.turnLocks[id]`, and `Shutdown`.
  4. Rekeys disk + registry.
- Without the drain, a queued live write could resurrect the old id.
- Publishes bus event `session_rekeyed` (in `sessionScopedEvents`, `event_bus.go`).

## Web

- `web/src/components/Chat/commands.ts` `/reset-id` calls `api.resetSessionId(id, host)` and returns `CommandResult.rekeyTo={oldId, newId}`.
- `web/src/App.tsx` applies it via `rekeySession(oldId, newId)` — `REKEY_SESSION` + rekeyQueue + rekeyDraft + `UPDATE_TAB_ID`.
- `rekeySession` gained a `placeholderTitle` parameter:
  - `/new` and draft rekeys pass `"New session"` (placeholder).
  - `/reset-id` passes nothing, so the tab title is preserved.
- Other tabs/windows rebind via the `session_rekeyed` handler in `web/src/lib/sessionEvents.ts` (before the generic session-scoped path, gated on `openSessionIds.has(oldId)`); it is in `SESSION_SCOPED_EVENTS` / `ROUTABLE_EVENTS`. Remote sessions route through `ctx.host`.

## Guards

- The bridged `/rc` TUI session is refused (`RCBridge.SessionID` is read unlocked in ~20 sites); run `/reset-id` in the terminal instead.
- The TUI handler also refuses while `/rc` is active.

## Rule / gotcha

- Any new session-id-keyed map or journal **must** be moved by the rekey path too, or `/reset-id` strands it under the deleted id.

## Tests

- `internal/session/rekey_test.go`
- `internal/snapshot/rekey_test.go`
- `internal/agent/rekey_test.go` (asserts the wire header changes)
- `internal/server/handler_reset_id_test.go`
- `internal/tui/reset_id_test.go`
- `web/src/components/Chat/commands.resetId.test.tsx`
- `web/src/lib/sessionEvents.test.ts`

## References

- Handler: `internal/server/handler_reset_id.go`
- Core rekey entry: `session.RekeyForDir`, `Agent.RekeySession`, `snapshot.Store.RekeySession`, `tool.RekeyTodoSession`, `SessionManager.Rekey`
- Client identity source: `GenericClient.opencodeSessionID()` (`internal/agent/client.go`)
