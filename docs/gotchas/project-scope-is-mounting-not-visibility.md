---
type: Gotcha
title: Project scoping is visibility, not mounting — React key remounts
description: "\"Project switching used to reset an open PDF/Office preview because App filtered editor panes by visibility (unmounting them) and PreviewHost was keyed on the active tab id. Fix: keep panes mounted and hide with CSS; persist per-file viewer state across remounts; pane shell state is now per-session (side:chat:<id> / side:term:<id>, v2).\""
resource: web/src/App.tsx; web/src/components/Preview/sidebarPreviewState.ts; web/src/lib/previewViewState.ts; web/src/lib/sidePaneState.ts; web/src/lib/browserStore.ts; web/src/components/Preview/PreviewHost.tsx
tags:
  - gotcha
  - web
  - react
  - files-tab
  - preview
  - pdf
  - persistence
  - session
timestamp: 2026-09-24T04:41:07Z
---
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
The shell state that survives this remount is persisted per session
surface key (see Fix B), **not per project** — session A's pane state
never leaks into session B.

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

## Fix B: per-session persistence for the remount that remains

New `web/src/components/Preview/sidebarPreviewState.ts` — pane shell
state (active surface + previewed file/page) persisted **per session
surface key**, not per project. localStorage `ocode.ui.sidebarPreview.v2`
(shape `{surface, path, kind, page, unsupportedPath}`). The key is the
side stateKey: `side:chat:<sessionId>` for chat sessions
(`sideChatKey(sessionId)` in `web/src/lib/sidePaneState.ts`) and
`side:term:<terminalId>` for terminals (`sideTermKey(terminalId)`).
PreviewHost seeds initial state from the store once per mount and writes
back on change; the foreign-anchor containment guard (`docBelongsHere`,
unchanged) still refuses to persist a doc whose anchor differs from the
pane's project (no cross-project poisoning).

```ts
// sidebarPreviewState.ts
const STORAGE_KEY = "ocode.ui.sidebarPreview.v2";
export function loadSidebarPreviewState(stateKey: string): PaneShellState | null;
export function saveSidebarPreviewState(stateKey: string, state: PaneShellState): void;
export function rekeySidebarPreviewState(oldKey: string, newKey: string): void;
```

`web/src/lib/browserStore.ts` gained `browserActions.rekey(oldKey,
newKey)` — moves a surface's live browser state to the new key; no-op if
the source is absent or the target already exists.

`web/src/lib/sidePaneState.ts` provides `rekeySidePaneState(oldId,
newId)`, called wherever per-tab state is rekeyed: `App.rekeySession`
(when a new chat's temp id becomes its real `ses_...` id on the first
message) and `sessionEvents.ts` in both the `session_started` and
`session_rekeyed` event handlers. It invokes `browserActions.rekey` for
the live browser surface and `rekeySidebarPreviewState` for the
persisted preview shell. This keeps the pane attached across
session-id changes: opening a pane in one chat never opens it in another,
and returning to a session restores exactly what it had.

## Supporting state persistence (app reload / sidebar remount)

New shared `web/src/lib/previewViewState.ts` (localStorage
`ocode.ui.previewViewState.v1`, key = `previewViewKey(path, projectRoot, projectHost)`
= `host::root::path`, shape `{page?, zoom?, scrollTop?}`, bounded 200
entries MRU, prototype-pollution guarded). This covers the remaining
unmount (app reload / sidebar remount) and is **unchanged** — it stays
per-file per-project:

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
child state. When a React `key` depends on active-tab/active-session
identity, the component remounts on every switch — persist what you need
across the remount. The pane shell state is keyed by `side:chat:<id>` /
`side:term:<id>` (per session), while viewer detail (zoom/scroll/page) is
keyed by `host::root::path` (per file per project) — two different
scopes for two different concerns.

## Tests

- `web/src/App.editorTabScope.test.tsx` — inactive project's pane is hidden, not absent
- `web/src/App.previewActivation.test.tsx`
- `web/src/components/Preview/PreviewHost.test.tsx` — "keeps each chat session's preview separate within the SAME project" (mutation-verified against the old cross-session propagation effect)
- `web/src/components/Preview/PdfViewer.persistence.test.tsx`
- `web/src/components/Preview/sidebarPreviewState.test.tsx`
- `web/src/lib/previewViewState.test.ts`
- `web/src/lib/sidePaneState.test.tsx`
- `web/src/lib/browserStore.test.tsx` (rekey move + no-op cases)
- `web/src/App.sidePaneScope.test.tsx` (open pane in session s1 → switch to s2 → pane absent; back to s1 → pane restored; mutation-verified)
