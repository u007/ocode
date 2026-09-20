---
type: Gotcha
title: Project scoping is visibility, not mounting — React key remounts
description: "Project switching used to reset an open PDF/Office preview because App filtered editor panes by visibility (unmounting them) and PreviewHost was keyed on the active tab id. Fix: keep panes mounted and hide with CSS; persist per-file and per-project viewer state across remounts."
resource: web/src/App.tsx; web/src/components/Preview/sidebarPreviewState.ts; web/src/lib/previewViewState.ts; web/src/components/Preview/PreviewHost.tsx
tags:
  - gotcha
  - web
  - react
  - files-tab
  - preview
  - pdf
  - persistence
timestamp: 2026-09-20T00:39:17Z
---
# Project scoping is visibility, not mounting — React key remounts

## Symptom

Switching project re-rendered an open PDF preview from page 1 and lost
scroll position — "totally state reset".

## Root cause A: Files tab filtered panes by visibility (unmounting)

`web/src/App.tsx` rendered the Files-tab panes from
`visibleEditorTabs = visibleEditorTabsForProject(editorTabs, activeProject)`,
so switching project **removed** the outgoing project's `FileTabContent`
from the tree → unmounting `FileTabContent`'s page/slide state and
`PdfViewer`'s zoom/scroll.

## Root cause B: Sidebar PreviewHost keyed on active tab id

The sidebar panel is mounted as `<PreviewHost key={sideStateKey}>` where
`sideStateKey = side:{chat|term}:{activeTabId}`; a project switch changes
`activeTabId` → changes the React `key` → React **unmounts + remounts**
`PreviewHost`, resetting it to the Browser surface with no document.
React `key` changes are remounts, not re-renders — a common trap.

## Fix A: visibility, not mounting

App now iterates **all** `editorTabs`, always mounting each pane and
hiding non-visible ones with `className` `absolute inset-0 hidden`
(visible = `absolute inset-0`), with a `data-editor-pane={et.id}`
attribute for tests. A `visitedEditorTabsRef` Set gates first mount so an
app reload does not eagerly mount every restored tab (only tabs that have
been visible at least once mount). The per-tab `session` prop is gated
on a `visibleEditorTabIds` Set so a background project's pane does not
fetch diff decorations for the active session.

```
{editorTabs.map((et) => {
  if (!visitedEditorTabsRef.current.has(et.id) && et.id !== visibleActiveEditorTabId) return null;
  return (
    <div key={et.id} data-editor-pane={et.id}
         className={et.id === visibleActiveEditorTabId ? "absolute inset-0" : "absolute inset-0 hidden"}>
      <FileTabContent ... session={visibleEditorTabIds.has(et.id) ? activeTabId ?? undefined : undefined} />
    </div>
  );
})}
```

## Fix B: per-project persistence for the remount that remains

New `web/src/components/Preview/sidebarPreviewState.ts` — per-project
shell state keyed `host::root` in localStorage `ocode.ui.sidebarPreview.v1`
(shape `{surface, path, kind, projectRoot, projectHost, page, unsupportedPath}`).
PreviewHost seeds initial state from it once per mount and writes back
on change; a containment guard refuses to store a doc whose anchor differs
from the pane's project (no cross-project poisoning).

```ts
// sidebarPreviewState.ts
const STORAGE_KEY = "ocode.ui.sidebarPreview.v1";
export function sidebarPreviewProjectKey(projectRoot?, projectHost?): string | null {
  const host = projectHost ?? ""; const root = projectRoot ?? "";
  if (!host && !root) return null;
  return host ? `${host}::${root}` : root;
}
```

## Supporting state persistence (app reload / sidebar remount)

New shared `web/src/lib/previewViewState.ts` (localStorage
`ocode.ui.previewViewState.v1`, key = `previewViewKey(path, projectRoot, projectHost)`
= `host::root::path`, shape `{page?, zoom?, scrollTop?}`, bounded 200
entries MRU, prototype-pollution guarded). This covers the remaining
unmount (app reload / sidebar remount):

- `PdfViewer` restores zoom + a stored `scrollTop` consistent with the
  target page's band, persists debounced 250 ms + unmount flush, armed
  only after the first-placement restore (`restoreDoneRef`).
- `FileTabContent` restores/persists page (or slide for pptx) under the
  same key.

## One-shot activation

`usePreviewActivation` (web/src/components/Preview/usePreviewActivation.ts)
returns a `consume()` that clears the pending request; App threads it as
`onConsumeActivation` on PreviewHost. Without it a remount would replay a
stale activation over the just-restored state. Nonce is one monotonic
sequence.

## Rule of thumb

When you filter a list of surfaces by "active", ask whether you mean
"hide" (keep mounted, CSS `hidden`) or "unmount". Unmounting discards
child state. When a React `key` depends on active-tab/active-project
identity, the component remounts on every switch — persist what you need
across the remount.

## Tests

- `web/src/App.editorTabScope.test.tsx` — inactive project's pane is hidden, not absent
- `web/src/App.previewActivation.test.tsx`
- `web/src/components/Preview/PreviewHost.test.tsx`
- `web/src/components/Preview/PdfViewer.persistence.test.tsx`
- `web/src/lib/previewViewState.test.ts`
- `web/src/components/Files/FileTabContent.test.tsx`

All mutation-verified.

## User-facing entry

`CHANGES.md` — `## 2026-09-20 — Project switch no longer resets an open PDF preview`
