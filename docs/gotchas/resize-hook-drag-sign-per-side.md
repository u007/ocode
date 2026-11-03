---
type: Gotcha
title: 'Resize Hook Drag Direction Depends on the Pane Side'
description: 'useResizableSidebar must be told which side of its handle the pane sits on (side: "left" or "right"). A pane after its handle grows when the handle moves left, so the left-only math made the right-docked Browser/Preview pane shrink on a left drag and grow on a right drag, both inverted.'
resource: web/src/hooks/useResizableSidebar.ts; web/src/App.tsx
tags: [gotcha, web, resize, useResizableSidebar, side-pane, browser, keyboard]
timestamp: 2026-11-03T12:12:35Z
status: active
---
## Rule

A resize hook's drag sign depends on which side of the handle the pane sits
on. Never assume left. `useResizableSidebar` takes `side?: "left" | "right"`
(default `"left"`) and computes:

    newWidth = startWidth + dragSign * (clientX - startX)

with `dragSign = -1` for `side: "right"` and `+1` otherwise. A pane before its
handle (left-docked) grows when the handle moves right. A pane after its handle
(right-docked) must grow when the handle moves left.

## How it broke

The hook had no side concept, so every caller got left math. The chat-side
"Browser / Preview" panel (`browserPane` in `web/src/App.tsx`, storage key
`ocode.ui.browser_width`) is the only right-docked caller. With the left-only math,
dragging the handle left shrank the pane and dragging it right grew it, both
inverted. The ArrowLeft/ArrowRight handler on the same handle had the same
inversion.

## Fix shape

- The hook takes `side` explicitly because the DOM cannot reveal the geometry.
- `browserPane` passes `side: "right"`. Its keyboard handler is flipped to match:
  ArrowLeft grows, ArrowRight shrinks, Home resets.
- Every other caller keeps the default `"left"`: the app sidebar, the file tree,
  the Git file list, the Settings nav, the DB panel table list and the SQLite
  viewer table list.
- `useResizableSplit` (`web/src/hooks/useResizableSplit.ts`, used by the Files
  editor/preview split and Git history) is a different hook. Its pane is left of
  the handle, so it needs no side option.

## Tests

`web/src/hooks/useResizableSidebar.test.tsx` ("useResizableSidebar drag direction")
drives the real pointer path for both sides and checks clamping. A new
right-docked caller needs a matching test case, and a sign flip must fail it.

## Related

- `gotchas/side-pane-statekey-convention.md` covers the pane's per-session state
  keys. It does not cover pane width or drag direction.
- `concepts/sqlite-browser.md` and `concepts/db-connector.md` describe left-docked
  table lists that use the hook with the default side.
