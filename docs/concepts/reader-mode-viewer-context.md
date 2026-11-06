---
type: Concept
title: Reader Mode and Viewer Context (web/desktop chat with a preview)
description: Desktop reader mode promotes a chat's Browser/Preview side pane to fill the session area with the chat as a narrow right column, and viewer context records what the user is looking at so the next message carries it. Covers the no-remount layout invariant, the reader state and gate, the viewer-context contract module, and what each viewer reports.
resource: web/src/lib/readerMode.ts; web/src/lib/viewerContext.ts; web/src/lib/viewerDom.ts; web/src/components/Preview/PreviewHost.tsx; web/src/components/Chat/ChatInput.tsx; web/src/App.tsx
tags:
  - web
  - desktop
  - reader-mode
  - viewer-context
  - preview
  - chat
  - layout
  - invariant
timestamp: 2026-11-06T12:24:00Z
status: active
---
# Reader Mode and Viewer Context

Two web/desktop SPA features, built together on 2026-11-06.

- **Reader mode** puts the chat session's right-hand Browser/Preview side pane
  in the main area and turns the chat column into a narrow, resizable column on
  the right.
- **Viewer context** is a per-chat-session record of what the user is looking
  at in the preview that accompanies the session. The composer reads it at send
  time and prepends a short `@path (kind, location) selected|visible: "…"` line
  to the user's message.

Neither feature touches the server or Go code.

## Reader mode

### Entering and leaving

- **Enter:** the `Maximize2` button, title and aria "Focus preview (reader mode)",
  in the side pane header (`web/src/App.tsx`). It is shown only while the pane is
  open and reader mode is off.
- **Leave:** the `Minimize2` button, "Exit reader mode", in `ReaderChatHeader`
  (`web/src/components/Layout/ReaderChatHeader.tsx`). That strip sits above the
  chat column while reader mode is on. Its `Menu` button ("Show sessions
  sidebar") re-shows the sessions sidebar.

### State and gate

- `reader: boolean` on `BrowserTabState` (`web/src/lib/browserStore.ts`), set
  with `browserActions.setReader(key, bool)`. It sits beside `collapsed`, under
  the same per-session side-pane key `side:chat:<sessionTabId>`, and is
  in-memory only. See `docs/gotchas/side-pane-statekey-convention.md`.
- Gate: `shouldRenderReaderPreview` (`web/src/lib/readerMode.ts`) is true only
  when all of these hold: a side-pane state key exists, the pane kind is `chat`,
  the browser pane is open, not collapsed, `reader` is set, the side pane is
  visible, and the viewport is not mobile. Reader mode therefore exists only on
  the Chat sub-tab on desktop, the same gate as the side pane.
- `sessionAskSurfaceVisible` and `dialogScope.ts` are unchanged. Reader mode
  keeps `activeSubTab === "chat"`, so permission and question asks still show.
- Sessions and cowork sidebars: on entering, App remembers `sidebarOpen` and
  `coworkOpen` in a ref and sets both false. On leaving, it restores them. One
  effect keyed on `readerActive` does both.
- Chat column width: `useResizableSidebar({ storageKey: "ocode.ui.reader_chat_width", defaultWidth: 440, minWidth: 340, maxWidth: 900, side: "right" })`
  (`readerChatPane` in App.tsx). The separator is keyboard-operable: Arrow keys
  move 10 px (Shift: 50 px), Home resets, and double-click resets.

### Layout invariant: the chat column and the side preview must not remount

The chat column (`data-testid="chat-ask-surface"`, `App.tsx:2088`) is the
portal container for `PermissionDialog` and `QuestionDialog`. It also holds
`ChatPanel`, `PlanPanel`, `BtwPanel` and `ChatInput`. A remount on toggle would
drop the composer draft, the transcript scroll position and any open ask. The
side `PreviewHost` must not remount either: a remount runs its unmount cleanup
and reloads a live page in the Browser tab.

Reader mode therefore changes classes and inline styles, never the element tree:

1. `RemoteVersionBanner` is a sibling of the row, not a child of the surface
   (`App.tsx:2080`).
2. The row wrapper (`flex min-h-0 flex-1 flex-row`, `App.tsx:2081`) is always
   rendered inside each per-tab container.
3. `sidePreviewHost` has one render site, the side-pane slot beside the main
   column (`App.tsx:2377`, slot at `App.tsx:2331`). In reader mode that slot
   switches to `order-first` and drops its header (`App.tsx:2332-2339`), so the
   preview draws to the left of the chat even though the chat comes first in
   the DOM.
4. The reader separator renders only in reader mode, after the slot
   (`App.tsx:2380-2390`). It is also `order-first`, so it sits between the
   preview and the chat.
5. In reader mode the main column gets an inline `width` from
   `readerChatPane.width` and `flex: none` (`App.tsx:1738`). The chat surface
   inside it gets the same style (`App.tsx:2090`), and its first child is then
   the optional `ReaderChatHeader` (`App.tsx:2092`).

`PreviewHost` keeps its fiber across the toggle, so its viewer selection, doc
and page state, and the Browser tab's iframe all survive.

Known side effects of the single slot:

- Keyboard tab order is chat, then preview, while visual order is preview,
  then chat.
- The `RemoteVersionBanner` sits inside the main column, so in reader mode it
  spans only the chat column.

Tests that pin this: `web/src/App.readerPreviewMount.test.tsx` mocks
`PreviewHost` with a mount counter and asserts one mount across entering and
leaving reader mode. `web/src/App.askDialogScope.test.tsx` checks that the
permission dialog's parent is the surface and that the version banner sits
outside it (`banner.parentElement` is `surface.parentElement.parentElement`).
`web/src/components/Layout/ReaderChatHeader.test.tsx` covers the header.
`web/src/lib/readerMode.test.ts` covers the gate.

## Viewer context

### Contract module: `web/src/lib/viewerContext.ts`

- **Record** (`ViewerState`): `path`, `kind`, optional `projectRoot` and
  `projectHost`, `location` (`page`, `pageCount`, `slide`, `slideCount`,
  `sheet`, `range`, `heading`, `lines`), `selection` (string or null),
  `visibleText` (string or null), and `updatedAt`.
- **Key:** the chat session tab id (`sessionTabId` in ChatInput, the
  `sessionId` prop of PreviewHost). It is not the side-pane state key.
- **Store:** a module-level map with listeners. `useViewerState(key)` reads it
  through `useSyncExternalStore`.
- **Caps:** `VIEWER_SELECTION_MAX` is 1500 characters and `VIEWER_VISIBLE_MAX`
  is 1200. `tidyViewerText` collapses runs of spaces, trims spaces at line
  ends, drops blank lines, keeps TAB characters (a spreadsheet selection is
  TSV, and the tabs are its column boundaries), and appends `…` when it cuts.

### Who writes what

- **Document part (PreviewHost).** `setViewerDoc(sessionId, doc | null)` runs in
  an effect. It writes only when `surface === "preview"` and the document passed
  its access check (`docVerified`). It clears on unmount. PreviewHost also
  provides `ViewerReportContext` (value: the session id) around `PreviewSurface`.
  A session-less panel, such as the Files tab, has a null context.
- **View part (each viewer).** A viewer calls `useViewerReport(path)` and then
  `report({ location?, selection?, visibleText? })`. `patchViewerState` drops a
  patch whose path is not the current document. Outside a
  `ViewerReportContext` provider the reporter is a stable no-op, so the Files
  tab and the Preview sub-tab report nothing.
- **Reset.** A different path, kind or project resets location, selection and
  visible text. The same document keeps them.
- **Selection is sticky.** Clicking the composer collapses the browser's
  selection, so a viewer reports `selection: null` only for a mouseup INSIDE
  the viewer that left no highlight. That is `usePreviewSelection(label, onSelection)`
  in `web/src/components/Preview/SelectionToolbar.tsx`. Any other event keeps the
  last highlight.

### Reader to model

- `ChatInput` subscribes with `useViewerState(sessionTabId)` and renders
  `ViewerContextChip` (`web/src/components/Chat/ViewerContextChip.tsx`). The chip
  shows the file's basename, its location, and whether a selection or visible
  text is attached. Its tooltip is the exact text that will be sent.
- The chip's X excludes the viewer context from the current message only. The
  exclusion resets on send and whenever `viewer.path` changes.
- At send, `formatViewerContext(state, fallbackSelection)` produces the line.
  Examples:
  - `@docs/guide.md (markdown, § Install) selected: "…"`
  - `@report.pdf (pdf, p.3/12) visible: "…"`
- Location rendering (`describeViewerLocation`): `p.3/12`, `slide 2/8`,
  `Sheet1!B2:D9`, `§ Heading`, `L10-L24`.
- Part order is `[contextRef, contextRefs, viewerRef, previewRef, refs, trimmed]`.
  When the Ask-LLM `previewContext` is for the same path, its `previewRef` is
  dropped and its excerpt becomes the fallback selection, so nothing repeats.
- The empty-message gate is unchanged. A message with only viewer context is
  refused, the same as an empty message.

### Prompt-cache rationale

The context rides in the user message, which is the volatile tail. It never
goes into `system` or `tools`, so the cached prefix is untouched. The server
needs no change: in `buildOpenAIContentWithImages` (`internal/agent/client.go`),
the `@` token branch special-cases only image files, so a non-image `@path`
token is sent as plain text. See `docs/concepts/prompt-cache-stability.md`.

### What each viewer reports

| Viewer | Reports | How |
|---|---|---|
| `MarkdownViewer` | `heading` (nearest heading at or above the viewport top) and `visibleText`; `selection` | `collectVisibleText` in `web/src/lib/viewerDom.ts`, throttled 250 ms (trailing) on scroll and after each render |
| `DocxViewer` | same as markdown | same helper and throttle |
| `PdfViewer` | `page` and `pageCount`, plus the visible page's text-layer text (null until that layer is built); `selection` | throttled 250 ms; re-reported on page change, count change and `renderTick` |
| `PptxViewer` | `slide` and `slideCount`, plus the current slide's block text; `selection` | on slide change and after the deck loads |
| `TextViewer` (Monaco through `FileEditor`) | selected line range (`lines`) and selected text only | `FileEditor.onSelectionChange`. `FileEditor` does not expose the editor, so there is no visible range |
| `ExcelViewer` (Univer 1.1.0) | active sheet (`sheet`), active range in A1 (`range`), and the selected cell values as TSV, capped at 20 rows by 10 columns (`selection`) | `UniverSheetHandle` methods `activeSheetName()`, `selection()` and `onSelectionChange(cb)` in `web/src/components/Preview/univerMount.ts`, on the facade events `SelectionChanged`, `SelectionMoveEnd` and `ActiveSheetChanged` |
| `HtmlViewer` | nothing | it renders into a sandboxed iframe that the DOM helper cannot read |
| Image, media, json, sqlite, mermaid | path and kind only | set by PreviewHost |

Verified in a real browser on 2026-11-06 (htrcli, headless Chrome):

- Markdown scroll reports `§ Persistence (Decision 2 superseded)` plus the
  visible text.
- A drag-selected paragraph reports `selected: "…"`, and the chip label
  switches to "selection".
- `sales.xlsx` reports `(excel, Data!A1) selected: "Date"`.
- `resume.pdf` reports `(pdf, p.1/1) visible: "…"`.

## Pitfalls

- **Mount-only listeners.** `usePreviewSelection` once bound its mouseup listener
  in a mount-only effect, so the Copy / Ask LLM toolbar never appeared on
  markdown previews. See `docs/gotchas/preview-selection-mouseup-listener-mount-only.md`.
- **Frontmatter as a heading.** A markdown file whose YAML frontmatter is
  followed by `---` renders as a setext heading. The "nearest heading" then
  became the whole frontmatter block. `headingFor` in `viewerDom.ts` now
  collapses whitespace to one line and caps the result at 120 characters.
- **Known limits** are listed as open follow-ups in `TODO.md` under
  "Open: reader mode + viewer context follow-ups (2026-11-06)". They include:
  the model sees the viewer only at send time (there is no mid-turn tool), the
  HTML viewer reports nothing, the text viewer has no visible range, a preview
  that failed to load still reports its path, a spreadsheet's active cell is
  always reported as a "selection", and the chip and the Ask-LLM chip can both
  show for the same file.

## Related

- `docs/concepts/web-chat-transcript-layout.md`: the chat column's flex layout
  and shrink-weight rules, which apply inside the surface in reader mode too.
- `docs/gotchas/side-pane-statekey-convention.md`: where the `reader` flag lives
  and how it is keyed.
- `docs/gotchas/resize-hook-drag-sign-per-side.md`: why the chat column's
  resize hook is told `side: "right"`.
- `docs/concepts/excel-preview-univer-sheet.md`: the Univer sheet the Excel
  viewer reads.
- `docs/concepts/web-ask-dialog-llm-context.md`: the Ask-LLM preview context
  that the viewer block can absorb.
