# Part 06 — `jumpToSession` helper

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("Web → `lib/jumpToSession.ts`").

## Context

- `web/src/stores/projectStore.tsx`: `selectProject` (:754) activates a
  project and restores its cached session list; `openSessionTab(id, title,
  projectPath)` (:778) opens/binds a session tab. Jumping across projects
  needs both, in that order.
- Terminals restore per project (`components/Terminal/terminalPersistence.ts`,
  `stores/terminalStore.tsx`); browser tabs per project
  (`stores/browserTabsStore.tsx`); side pane per session
  (`lib/sidePaneState.ts`). No extra restore work should be needed — verify.
- Host scoping rule: `docs/concepts/web-session-host-scoping.md`; local host
  is `""`.
- App view state: `activeView` in `web/src/App.tsx:248`; jumping must set it
  to `"sessions"` with chat focused.

## Files

- Create: `web/src/lib/jumpToSession.ts`
- Test: `web/src/lib/jumpToSession.test.tsx`

## Interfaces

- Produces: `useJumpToSession(): (target: { projectPath: string; host: string; sessionId: string; title: string }) => void`
  — calls `selectProject(projectPath, host)` then
  `openSessionTab(sessionId, title, projectPath)`, then switches App to the
  sessions view with chat focused.
- Produces: `useJumpToPendingAsk(): (target: same shape) => void` — runs the
  jump, then opens the session's side pane on its pending ask (use the
  existing side-pane open path in `lib/sidePaneState.ts`).
- App exposes view switching to the helper via the existing mechanism the
  command palette uses to change `activeView` (find it in `App.tsx`; if it is
  local state only, lift a `setActiveView` into a small context in this part).

## Steps

- [ ] **Failing tests** with mocked project store:
  - cross-project jump calls `selectProject` strictly before
    `openSessionTab`, with host passed;
  - same-project jump still calls both (idempotent) and ends in sessions
    view;
  - `useJumpToPendingAsk` opens the side pane for that session id after the
    tab opens.
- [ ] Run `cd web && pnpm vitest run src/lib/jumpToSession.test.tsx` → FAIL.
- [ ] **Implement.**
- [ ] Run → PASS; `pnpm test && pnpm typecheck`.
- [ ] **Manual check**: two projects each with a terminal and a browser tab;
  jump between their sessions; confirm terminals, browser tabs and side
  pane restore. If any do not, stop and report which store needs a hook.
- [ ] Commit: `feat(web): add jumpToSession helper for cross-project session jumps`.
