---
type: Gotcha
title: Sidebar row stopPropagation breaks mobile drawer dismiss
description: 'Gotcha: a stopPropagation control nested in a sidebar row breaks the mobile drawer-dismiss path; thread an onRevealTab callback to restore it.'
tags:
  - gotcha
  - web-ui
  - sidebar
  - mobile
  - drawer
  - event-propagation
  - react
timestamp: 2026-09-30T04:28:16Z
---
# Gotcha: nested `stopPropagation` controls break the sidebar row's mobile drawer dismiss

## Symptom

On mobile (≤767px), tapping a control that lives *inside* a `ProjectSidebar` row — e.g. the expand/collapse toggle of the remote session/terminal inventory rendered by `RemoteProjectStatus` — opens the requested tab but leaves the project drawer open, covering the workspace. Tapping the row itself still dismisses the drawer fine.

## Root cause

The mobile drawer dismiss path is single-threaded through the row's `onSelect`:

- `web/src/components/Layout/ProjectSidebar.tsx` — on mobile, the row's `onSelect` calls `onToggle()` (auto-dismiss the drawer when a project/tab is chosen).
- `web/src/components/Layout/RemoteProjectStatus.tsx:146,178-179,...` — the inventory's nested controls intentionally call `e.stopPropagation()` (a shared `stop()` helper) so that expanding/collapsing the inventory or launching a session does **not** also fire the row's `onSelect` (which would wrongly select the project / collapse the inventory).

Those two behaviours are individually correct but mutually exclusive: because the nested control stops propagation, the row's `onSelect` never runs, so on mobile **nothing dismisses the drawer**. The stopPropagation is load-bearing (it must not be removed), and `onSelect` is the only other path to the dismiss — there is no event that re-reaches it.

## Fix

Thread an explicit `onRevealTab` callback down to the nested control so it restores the dismiss behaviour itself, instead of relying on the event bubbling it deliberately blocks:

- `ProjectSidebar.tsx:400,416` — `SortableProjectRow` accepts an optional `onRevealTab?: () => void`.
- `ProjectSidebar.tsx:1569` — passes `onRevealTab={isMobile ? onToggle : undefined}` (desktop needs no dismiss; keep the prop mobile-gated so desktop behaviour is unchanged).
- `RemoteProjectStatus.tsx:62,70,162-166` — `revealTab()` calls `onRevealTab?.()` after queueing the tab focus, so every reveal from inside the inventory dismisses the drawer.

The comment at `ProjectSidebar.tsx:1566-1568` documents the coupling and must be kept.

## Rule

**Any control nested in a sidebar row that calls `stopPropagation` must also thread an explicit callback to re-run the row-level side effect it displaced** (here: drawer dismiss via `onToggle`). Do not "fix" this by removing the `stopPropagation` — it exists to prevent the row's `onSelect` from firing. When adding a new nested interactive element to a row, ask: *what does the row's `onSelect` do on mobile, and does my nested control still trigger it?* If not, thread the callback.

## Related

- `gotchas/web-ui-mobile-layout-breakage.md` — the mobile drawer pattern this interacts with (Fix Rule 1: selecting a project auto-dismisses the drawers).
- `concepts/remote-persistent-sessions-terminals.md` — the sidebar remote inventory and tab reveal architecture.
