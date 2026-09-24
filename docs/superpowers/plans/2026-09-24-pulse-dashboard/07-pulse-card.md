# Part 07 — `PulseCard`

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("Card (collapsed)", "Card (expanded)", "Actions", "Accessibility",
"Live tail").

## Context

- Row shape (`web/src/api/types.ts`): `PulseRow{ session_id, project_path,
  title, status, current_task, todo, pending_ask, turn_started_at,
  updated_at, child_count }`; `todo` is
  `{ done, total, current, items: [{ text, state }] } | null`, status ∈ `needs_permission | needs_question |
  running | error | idle`.
- Jump actions: `useJumpToSession()` and `useJumpToPendingAsk()` from
  `web/src/lib/jumpToSession.ts`, each taking
  `{ projectPath, host, sessionId, title }`; host is `""` (local only in v1).
- Live tail sources: `GET /api/sessions/{id}/state` (buffered frames, only
  while a turn runs — frames are cleared when a turn ends) and the `text`
  SSE event via `web/src/lib/eventBus.ts` `on(...)`. For idle/error cards
  use the last assistant message lines instead (existing session messages
  client call).
- UI primitives: `web/src/components/ui/` (`badge`, `progress`, `tooltip`).
  No hover-card installed; expansion is CSS overlay, not a popover.

## Files

- Create: `web/src/components/Pulse/PulseCard.tsx`
- Create: `web/src/components/Pulse/usePulseTail.ts`
- Test: `web/src/components/Pulse/PulseCard.test.tsx`,
  `web/src/components/Pulse/usePulseTail.test.ts`

## Interfaces

- Produces: `<PulseCard row={PulseRow} compact={boolean} />` —
  `compact` true for Recent section (idle/error) cards.
- Produces: `usePulseTail(sessionId: string, enabled: boolean, status: PulseStatus): { lines: string[]; error: string | null }`
  — last 6 lines; subscribes only while `enabled`; clears on disable.

## Behavior

- Collapsed: glyph (◆ needs you, ● running, ✕ error, ○ idle) with
  `aria-label` of the status word; project basename (full path in
  `title` attr); session title; task line = `pending_ask.summary` for
  needs-you, else `current_task.text`; todo `Progress` when `todo`
  non-null; elapsed = live-updating turn duration while running, else
  relative "N ago" from `updated_at`; child-count chip when > 0.
- Running glyph pulses; disabled under `prefers-reduced-motion`.
- Expand after 150ms hover or immediately on keyboard focus; overlay is
  absolutely positioned above neighbors (card wrapper keeps its grid size so
  nothing reflows); collapse on leave/blur/`Esc`.
- Expanded shows tail lines, full todo list from `todo.items` with state
  marks (☐ pending, ▸ in progress, ☑ done), running tool text, pending ask
  text.
- Click / `⏎` → `useJumpToSession` immediately (no double-click delay).
  Double-click → `useJumpToPendingAsk` when `pending_ask` non-null, else
  nothing extra. The jump is idempotent, so the preceding single-click jump
  is harmless; the double-click only adds opening the side pane.
- Root is a `role="listitem"` containing one full-card `<button>`.

## Steps

- [ ] **Failing `usePulseTail` tests**: running → fetches state once,
  appends `text` events, keeps last 6; disabled → unsubscribes and clears;
  idle → uses last assistant message; fetch failure → `error` set and
  logged with session id.
- [ ] **Failing `PulseCard` tests** (fake timers): each status renders glyph
  + aria-label; needs-you shows ask summary; hover <150ms no expand, ≥150ms
  expand; focus expands immediately; `Esc` collapses; click calls jump with
  host `""`; double-click with pending ask calls pending-ask jump; expanded
  shows every `todo.items` entry with its state mark; reduced
  motion removes pulse class.
- [ ] Run `cd web && pnpm vitest run src/components/Pulse` → FAIL.
- [ ] **Implement.**
- [ ] Run → PASS; `pnpm test && pnpm typecheck`.
- [ ] Commit: `feat(web): add PulseCard with hover-expand live tail`.
