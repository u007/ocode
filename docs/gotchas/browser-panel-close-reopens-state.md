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
