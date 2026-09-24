# Part 08 — `PulseView` and entry points

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("Entry points", "Layout", empty/error states).

## Context

- App views: `activeView` union in `web/src/App.tsx:248`
  (`"files" | "git" | "cron" | "assets" | "sessions" | "settings"`),
  persisted per project via `saveViewStateForProject` (:350).
- Tab bar: `web/src/components/Layout/UnifiedTabBar.tsx`.
- Shortcuts: `web/src/hooks/useKeyboard.ts` (Cmd+K/P/S/N/T/W; J unused).
- Data: `usePulse()` from `web/src/stores/pulseStore.tsx` →
  `{ rows, scope, setScope, loadMore, hasMore, error, retry, counts: { running, needsYou } }`.
- Card: `<PulseCard row compact />` from
  `web/src/components/Pulse/PulseCard.tsx`.

## Files

- Create: `web/src/components/Pulse/PulseView.tsx`
- Create: `web/src/components/Pulse/PulseBadge.tsx`
- Modify: `web/src/App.tsx` — add `"pulse"` to `activeView`; render
  `PulseView`; do NOT persist `"pulse"` per project (it is global — restore
  the project's saved view instead).
- Modify: `web/src/components/Layout/UnifiedTabBar.tsx` — pinned Pulse
  entry at the start of the bar.
- Modify: `web/src/hooks/useKeyboard.ts` — `⌘J`/`Ctrl+J` toggles
  Pulse ↔ previous view.
- Header: place `PulseBadge` in the app header next to existing status
  items in `App.tsx`.
- Test: `web/src/components/Pulse/PulseView.test.tsx`,
  `web/src/components/Pulse/PulseBadge.test.tsx`, extend
  `web/src/hooks/useKeyboard` tests if present (else new
  `useKeyboard.pulse.test.tsx`).

## Behavior

- Sections in fixed order: Needs you, Running, Recent; hidden when empty;
  header shows count.
- Grid: `role="list"`; responsive columns (1 at phone width, up to 4).
  Arrow keys move focus between cards in reading order.
- Toolbar: filter input (case-insensitive match on project basename or
  title, client-side over loaded rows) and `Live | All` toggle →
  `setScope`. "Load more" button when `hasMore`.
- Empty (live scope, zero rows): "No live sessions" + the 5 most recent
  sessions — fetch with `getPulse("all", null, 5)` via the store, render
  compact.
- Error: banner with message and Retry → `retry()`; no rows rendered from
  stale state.
- Badge: `● {running} · ◆ {needsYou}`, hidden when both 0; click →
  Pulse view; `aria-label` spells counts.
- `⌘J` from any view opens Pulse; from Pulse returns to the prior view.
  Ignored while focus is inside an xterm (match the existing `Cmd+W` guard).

## Steps

- [ ] **Failing tests**: section order and hiding; filter narrows; scope
  toggle calls `setScope("all")`; Load more; empty state shows recent 5;
  error banner + retry; badge hidden at zero, click switches view; ⌘J
  toggles and returns to previous view; ⌘J ignored in xterm.
- [ ] Run `cd web && pnpm vitest run src/components/Pulse src/hooks` → FAIL.
- [ ] **Implement.**
- [ ] Run → PASS; `pnpm test && pnpm typecheck`.
- [ ] **Manual (htrcli or claude-in-chrome)**: run the web app, start turns
  in two projects, trigger a permission ask; screenshot Pulse at desktop
  and 390px widths, light and dark; verify hover expand does not reflow.
- [ ] Commit: `feat(web): add Pulse view, header badge, tab entry and Cmd+J`.
