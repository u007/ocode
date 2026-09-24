---
type: Gotcha
title: Browser Panel Close Reopens Deleted State
description: Closing the side browser deletes its state, but App.tsx mistakes that deletion for an active-tab switch and recreates the panel.
resource: web/src/App.tsx
tags:
  - gotcha
  - browser-panel
  - state-management
  - close
  - react
timestamp: 2026-09-14T02:15:07Z
status: deprecated
deprecated_reason: Superseded by per-session side-pane scoping (2026-09-24). The cross-session propagation effect in App.tsx (which called browserActions.open(sideStateKey) for a new session when the old one was open) and the panelClosedByUser ref that caused the close-then-reopen bug were both removed. Pane open/collapsed state now lives under each session's own side:chat:<id> key in localStorage ocode.ui.sidebarPreview.v2; closing the pane in one chat never affects another chat, and switching to a different chat shows that chat's own (likely closed) pane. See gotchas/project-scope-is-mounting-not-visibility.md for the current mechanism.
---
## Problem

Closing the side browser panel removes its browser state, but the panel immediately reappears. The close control is therefore ineffective.

## Root cause

`web/src/App.tsx` has an effect that preserves an open side-browser state when the active chat or terminal tab changes. The effect treats `prevOpen && !currentExists` as evidence that the active tab switched and calls `browserActions.open(sideStateKey)`.

A deliberate close also deletes the current side-browser state, so it satisfies the same condition. Because the effect does not distinguish a state-key change from deletion of the current key, it recreates the state and reopens the panel.

## Fix guidance

Only migrate the previous open state when `sideStateKey` actually changes. A deletion of the current key caused by `browserActions.close` must remain closed. Add a regression test covering the close action and a separate test covering preservation across an actual active-tab switch.

## Affected code

- `web/src/App.tsx` — side-browser state preservation effect
- `web/src/lib/browserStore.ts` — close action deletes browser state
