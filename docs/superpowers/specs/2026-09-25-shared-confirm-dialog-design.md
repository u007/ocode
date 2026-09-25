---
type: Design
title: Shared confirm dialog + confirmation coverage for destructive sidebar/actions
description: Design record for a shared ConfirmDialog component replacing native window.confirm and bespoke confirms across six destructive web/desktop action sites.
tags:
  - design-spec
  - confirm-dialog
  - web
  - desktop
  - destructive-actions
timestamp: 2026-09-25T12:20:45Z
---
# Shared confirm dialog + confirmation coverage for destructive sidebar/actions

## Problem
1. Project removal in the web/desktop sidebar had NO confirmation: `ProjectSidebar.tsx` called `removeProject` directly from every entry point (row context menu, row hover trash button, collapsed-rail menu). Fixed earlier the same day with a bespoke `RemoveProjectDialog` rendered from all three surface branches.
2. `projectStore.removeProject` swallowed failures (`console.error`, never rethrows), so a rejected removal (404, remote host down) closed the confirm and told the user nothing. `renameProject` directly below it DOES rethrow — inconsistent contract.
3. Four destructive actions still used native `window.confirm`, which SILENTLY RETURNS FALSE in the Wails/WKWebView desktop webview (the bug is already documented in code at `FileTree.tsx:692-694` and `GitPanel.tsx:194`): `CronPanel.tsx:148` (delete cron job), `Logs/LogPanel.tsx:237` (clear logs), `Settings/ProfilesManager.tsx:82` (delete profile) and `:103` (remove provider key).
4. "Delete group" in the sidebar had no confirm, and it is a bulk change: `HandleDeleteGroup` (`internal/server/handler_projects.go:505`) ungroups EVERY project in the group before dropping the group.

## Decision
One shared component, `web/src/components/common/ConfirmDialog.tsx` (placed in `components/common/`, not `ui/`, because it carries product copy). Contract:
- props: `open`, `title`, `description`, `confirmLabel`, `onConfirm: () => Promise<void>`, `onCancel`.
- a REJECTED `onConfirm` renders the error INLINE and keeps the dialog OPEN with both buttons re-enabled (same contract as `FileTree`'s `ConfirmDeleteDialog` and `GitPanel`'s discard confirm).
- `data-dialog-default-action` on Cancel, so the safe action is default-focused (repo Dialog focus policy: first text-entry input → `[data-dialog-default-action]` → Radix first-tabbable).
- destructive confirm button, `max-w-sm`, pending label disables both buttons.
- copy stays at each call site; the shared component owns STRUCTURE only.

Six sites: (1) project remove — migrate the bespoke dialog, keeping name + `host:path` and the "files and chat sessions are not deleted" line (removal only drops the entry from `projects.json`); (2) delete group — "N projects move to Ungrouped; move them back one by one"; (3) cron job delete, (4) clear session logs, (5) delete profile (keeps "Removes N overrides + M keys — cannot undo" — genuinely on-disk destructive), (6) remove provider key. Wording for 3-6 is preserved from the existing native `confirm()` strings.

Store: `removeProject` and `deleteGroup` rethrow after their `console.error`, matching `renameProject`.

Deliberately NOT changed: the existing bespoke confirms in `FileTree` / `UnifiedTabBar` / `SessionDialog` / `GitPanel` (each has its own shape — multi-path lists, title-based copy, close-tab semantics); migrating them is separate work. Also not changed: `ProfilesManager.rename`'s native `prompt()` and the confirm-less cron "Clear outbox" (same no-op class, flagged to the user as follow-ups).

## Rejected alternatives
- Bespoke per-site dialogs: 6 copies of the same ~40 lines; consistency is the point of a confirm.
- `useConfirm()` hook + per-call JSX: same code volume, more indirection, harder to test than a component.
- Reporting failures through the existing app-wide `ActionErrorToast` (`web/src/lib/actionErrors.ts`, used by 15+ fire-and-forget call sites): correct default for fire-and-forget actions, wrong for a confirm — the error lands away from the dialog the user is looking at.

## Test contract
Shared component: cancel does not call `onConfirm`; confirm calls it once; a rejected `onConfirm` shows the error and leaves the dialog open; pending state disables both buttons. Store: both actions rethrow. Per site: opening the dialog does not perform the action, confirming does, cancel does not.

## Notes
Regression suites: `web/src/components/common/ConfirmDialog.test.tsx`, `web/src/components/Layout/ProjectSidebar.test.tsx`, `web/src/components/Cron/CronPanel.confirm.test.tsx`, `web/src/components/Logs/LogPanel.test.tsx`, `web/src/components/Settings/ProfilesManager.test.tsx`, `web/src/stores/projectStore.test.tsx`. Existing archive: `superpowers/specs/2026-09-25-list-dialog-keyboard-navigation-design.md` mentions the same focus policy.
