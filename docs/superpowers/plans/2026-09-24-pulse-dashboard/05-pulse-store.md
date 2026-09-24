# Part 05 — Web API client and app-level `pulseStore`

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("Web → `stores/pulseStore.tsx`").

## Context

- Server endpoint: `GET /api/pulse?scope=live|all&cursor=&limit=` →
  `{items: PulseRow[], next_cursor: string|null}`. `PulseRow` JSON:
  `session_id, project_path, title, status` (`needs_permission |
  needs_question | running | error | idle`), `current_task {kind: todo|tool|text, text} | null`,
  `todo {done, total, current, items: [{text, state}]} | null`, `pending_ask {kind: permission|question, summary} | null`,
  `turn_started_at, updated_at` (RFC 3339), `child_count`.
- New SSE event `todo_updated` `{session_id, done, total, current, items}`.
- Existing SSE events: `agent_activity`, `turn_started`, `turn_done`,
  `turn_error`, `turn_heartbeat`, `permission`, `permission_resolved`,
  `question`, `question_resolved`, `text`, `session_rekeyed`, title updates.
- `web/src/lib/sessionEvents.ts` drops events for sessions without an open
  tab via `sessionIsTracked` (:669, :680; also :286). Pulse must see them.
- `web/src/lib/eventBus.ts` exposes `onReconnect` handlers (:22-51).
- Remote hosts are out of scope: only the local host (`host === ""`).

## Files

- Modify: `web/src/api/types.ts` — `PulseStatus`, `PulseRow`,
  `PulsePage`, `TodoUpdatedEvent`.
- Modify: `web/src/api/client.ts` (`api` object at :586) — `getPulse`.
- Create: `web/src/stores/pulseStore.tsx`
- Modify: `web/src/lib/sessionEvents.ts` — forward to pulse store before
  the tracked filter.
- Modify: `web/src/App.tsx` — mount `PulseProvider` at app level.
- Test: `web/src/stores/pulseStore.test.tsx`,
  `web/src/lib/sessionEvents.pulse.test.ts`

## Interfaces

- Produces: `api.getPulse(scope: "live" | "all", cursor: string | null, limit: number): Promise<PulsePage>`.
- Produces: `PulseProvider`, `usePulse()` returning
  `{ rows: PulseRow[] /* already server-sorted, re-sorted on update with the same rule */, scope, setScope(s), loadMore(), hasMore: boolean, error: string | null, retry(), counts: { running: number; needsYou: number } }`.
- Produces: `pulseEventSink(event: string, sessionId: string, data: unknown): void`
  — module-level function the SSE router calls; no-op until the provider
  mounts.
- Sort rule (client re-sort after event updates): rank
  (`needs_*`=0, `running`=1, else 2) → `updated_at` desc → `session_id` asc.

## Steps

- [ ] **Failing store tests** (Vitest + mocked `api.getPulse`):
  - seeds from `getPulse("live", null, 50)` on mount;
  - `turn_started` for known row → status `running`, moves to Running rank;
  - `permission` → `needs_permission` with summary; `permission_resolved`
    → back to `running`;
  - `turn_done` → `idle`; `turn_error` → `error`;
  - `todo_updated` → row `todo` updated, `current_task` becomes todo kind
    when not needs-you;
  - event for unknown session id → exactly one debounced refetch (300ms),
    no row synthesized;
  - `onReconnect` → reseed;
  - `getPulse` rejects → `error` set and logged via `console.error` with
    context, `rows` unchanged (no stale substitute);
  - `session_rekeyed` → row re-keyed.
  - `setScope("all")` refetches from first page; `loadMore` appends with cursor.
- [ ] **Failing router test**: an event for a session with no open tab
  reaches `pulseEventSink`; tracked-session routing unchanged (existing
  `sessionEvents.test.ts` stays green).
- [ ] Run `cd web && pnpm vitest run src/stores/pulseStore.test.tsx src/lib/sessionEvents.pulse.test.ts` → FAIL.
- [ ] **Implement** types, client method, provider (reducer-based, matching
  existing stores' pattern e.g. `chatStore.tsx`), and the one-line forward
  in `sessionEvents.ts` placed before both `sessionIsTracked` checks.
- [ ] Run → PASS; `pnpm test && pnpm typecheck`.
- [ ] Commit: `feat(web): add pulseStore fed by /api/pulse and all-session SSE events`.
