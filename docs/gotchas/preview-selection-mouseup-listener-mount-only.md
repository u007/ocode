---
type: Gotcha
title: A mount-only mouseup listener never attaches when the viewer renders late
description: usePreviewSelection bound its mouseup listener in a mount-only effect, but the preview viewers render their scroll container only after a Loading state, so the ref was null at mount and the Copy / Ask LLM toolbar never appeared on markdown previews. The effect now has no dependency array and re-binds after every render.
resource: web/src/components/Preview/SelectionToolbar.tsx; web/src/components/Preview/MarkdownViewer.tsx; web/src/components/Preview/DocxViewer.tsx; web/src/components/Preview/PdfViewer.tsx; web/src/components/Preview/PptxViewer.tsx
tags:
  - gotcha
  - web
  - preview
  - selection
  - react
  - useEffect
  - ref
timestamp: 2026-11-06T12:24:00Z
status: active
---
# A mount-only mouseup listener never attaches when the viewer renders late

## Symptom

On a markdown preview, drag-selecting text did not show the floating
highlight toolbar (Copy / Ask LLM). A trusted drag selected 354 characters and
the toolbar was absent. The same code path worked in the other viewers that
happened to mount their container immediately.

Reproduced and confirmed in a real browser (htrcli, headless Chrome) on
2026-11-06. After the fix the toolbar appeared for the same drag.

## Cause

`usePreviewSelection` (`web/src/components/Preview/SelectionToolbar.tsx`)
attached its `mouseup` listener inside an effect whose dependency array was
`[onMouseUp]`. `onMouseUp` is a stable `useCallback`, so the effect ran once,
on mount.

The viewers render their scroll container conditionally, after a "Loading…"
branch. On the first commit `ref.current` was still null, so the effect
returned early without binding. When the container later appeared, nothing
re-ran the effect, so the listener was never added. No error is thrown, which
is why the failure was silent.

## Fix

The effect has no dependency array. It runs after every render, removes the
previous listener and adds the current one, so it always sees the live node.
That is one add/remove pair per render, which is cheap.

Do not restore a dependency array to "optimise" this. A stable `[onMouseUp]`
array is exactly the mount-only shape that failed.

## Rule

An effect that binds a DOM listener to a ref must not run only once when the
ref's node can mount later. Pick one:

- run the effect on every render (no dependency array), with a matching
  cleanup, as `usePreviewSelection` now does; or
- key the effect on the node itself, for example with a callback ref or state
  that holds the element.

The same class of bug is noted in the comment on the scroll listener in
`web/src/components/Preview/MarkdownViewer.tsx`, which says a mount-only
`addEventListener` effect would bind to `ref.current` too early.

## Related

- `docs/concepts/reader-mode-viewer-context.md`: the viewers that use
  `usePreviewSelection` and report the selection to the chat.
