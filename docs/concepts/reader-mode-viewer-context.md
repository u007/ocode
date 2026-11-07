---
type: Concept
title: Reader Mode and Viewer Context (web/desktop chat with a preview)
description: Desktop reader mode promotes a chat's Browser/Preview side pane beside the chat column. Viewer context is a per-session record of what the user sees in that preview, reaching the model three ways - a line with each sent message, a server mirror read by the viewer_state tool, and viewer_control directives the SPA applies to the viewer. Covers the no-remount layout invariant, the reader gate, the record and its transports, both agent tools, the per-viewer support matrix and the known limits.
resource: web/src/lib/readerMode.ts; web/src/lib/viewerContext.ts; web/src/lib/viewerControl.ts; web/src/hooks/useViewerSync.ts; web/src/components/Preview/PreviewHost.tsx; web/src/components/Chat/ChatInput.tsx; web/src/App.tsx; internal/server/viewer_state.go; internal/server/viewer_state_tool.go; internal/tool/viewer_control.go
tags:
  - web
  - desktop
  - reader-mode
  - viewer-context
  - preview
  - chat
  - layout
  - invariant
  - agent-tools
timestamp: 2026-11-07T02:07:18Z
status: active
---
# Reader Mode and Viewer Context

Two web/desktop SPA features, built 2026-11-06. On 2026-11-07 viewer context
gained a server mirror and two agent tools.

- **Reader mode** puts the chat session's right-hand Browser/Preview side pane
  in the main area and turns the chat column into a narrow, resizable column on
  the right.
- **Viewer context** is a per-chat-session record of what the user is looking
  at in the preview that accompanies the session. It reaches the model three
  ways:
  1. **Send-time line.** The composer prepends a short
     `@path (kind, location) selected|visible: "…"` line to the user's message.
  2. **Pulled state (`viewer_state`).** The SPA pushes the record to the server
     on every change, and a tool call reads the latest copy mid-turn.
  3. **Directives (`viewer_control`).** A tool result carries a directive. The
     SPA applies it to the viewer (scroll, select, find) and writes the outcome
     back into the record, so the next `viewer_state` call shows it.

Reader mode is SPA-only. Viewer context has a server mirror
(`internal/server/viewer_state.go`) and the two tools. The directive hook's
rules are in `docs/gotchas/viewer-control-directive-seeding-live-buffer.md`.

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
  (`readerChatPane` in App). The separator is keyboard-operable: Arrow keys
  move 10 px (Shift: 50 px), Home resets, and double-click resets.

### Layout invariant: the chat column and the side preview must not remount

The chat column (`data-testid="chat-ask-surface"`) is the portal container for
`PermissionDialog` and `QuestionDialog`. It also holds `ChatPanel`, `PlanPanel`,
`BtwPanel` and `ChatInput`. A remount on toggle would drop the composer draft,
the transcript scroll position and any open ask. The side `PreviewHost` must not
remount either: a remount runs its unmount cleanup and reloads a live page in the
Browser tab.

Reader mode therefore changes classes and inline styles, never the element tree:

1. `RemoteVersionBanner` is a sibling of the row, not a child of the chat surface.
2. The row wrapper (`flex min-h-0 flex-1 flex-row`) is always rendered inside
   each per-tab container.
3. `sidePreviewHost` has one render site, the side-pane slot beside the main
   column. In reader mode that slot switches to `order-first` and drops its
   header, so the preview draws to the left of the chat even though the chat
   comes first in the DOM.
4. The reader separator renders only in reader mode, after the slot. It is also
   `order-first`, so it sits between the preview and the chat.
5. In reader mode the main column gets an inline `width` from
   `readerChatPane.width` and `flex: none`. The chat surface inside it gets the
   same style, and its first child is then the optional `ReaderChatHeader`.

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
outside it. `web/src/components/Layout/ReaderChatHeader.test.tsx` covers the
header. `web/src/lib/readerMode.test.ts` covers the gate.

## Viewer context

### Record and key

- **Record** (`ViewerState`, `web/src/lib/viewerContext.ts`): `path`, `kind`,
  optional `projectRoot` and `projectHost`, `location` (`page`, `pageCount`,
  `slide`, `slideCount`, `sheet`, `range`, `heading`, `lines`, `visibleLines`),
  `selection` (string or null), `visibleText` (string or null), `find` (the
  query with its match count and current index), `control` (the outcome of the
  last directive: its id, action, and applied or failed with the error), and
  `updatedAt`.
- **Key:** the chat session tab id (`sessionTabId` in ChatInput, the
  `sessionId` prop of PreviewHost). It is not the side-pane state key. A draft
  `new-` tab moves to its real session id through `rekeyViewerState`, which App
  calls next to the side-pane rekey.
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
- **Control part (each viewer, optional).** A viewer registers its controller
  with `useViewerController(path, controller)`, or with
  `registerViewerController(key, c)`, which returns an unregister function. There
  is a stack per session key. The top entry is the active controller, and its
  unregister removes exactly its own entry, so the one beneath it is active
  again. `readViewerController` reads the top. `MarkdownViewer` registers a DOM
  controller for its rendered mode, and its Monaco editor stacks on top only
  while "Edit as text" is on (`useMonacoViewerReport(path, forceEdit)`). An action the viewer does not implement returns "not supported by
  this viewer".
- **Reset.** A different path, kind or project resets location, selection and
  visible text. The same document keeps them.
- **Selection is sticky.** Clicking the composer collapses the browser's
  selection, so a viewer reports `selection: null` only for a mouseup INSIDE
  the viewer that left no highlight. That is `usePreviewSelection(label, onSelection)`
  in `web/src/components/Preview/SelectionToolbar.tsx`. Any other event keeps the
  last highlight. The Copy / Ask LLM toolbar re-reads the highlight on window
  resize, window scroll and a container resize, so it follows a reader-mode
  relayout and hides when the highlight is gone. Those re-reads do not change
  the reported selection.

### Transport 1: send-time line (ChatInput)

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
  `Sheet1!B2:D9`, `§ Heading`, `L10-L24`, and `view L1-L40` when there is a
  visible line range but no selection.
- Part order is `[contextRef, contextRefs, viewerRef, previewRef, refs, trimmed]`.
  When the Ask-LLM `previewContext` is for the same path, its `previewRef` is
  dropped and its excerpt becomes the fallback selection, so nothing repeats.
- The empty-message gate is unchanged. A message with only viewer context is
  refused, the same as an empty message.

### Transport 2: push mirror (useViewerSync, viewer_state)

- `useViewerSync` (`web/src/hooks/useViewerSync.ts`) is mounted in PreviewHost
  after the `setViewerDoc` effect, so the unmount clear reaches the server as a
  DELETE. It subscribes to `viewerContext` and PUTs the full record to
  `PUT /api/sessions/{id}/viewer` with a 300 ms trailing throttle. A cleared
  record sends `DELETE` at once and cancels a pending PUT. Draft `new-` ids are
  skipped, because they have no server session yet. The hook passes the project
  host to the client methods `putSessionViewer` and `deleteSessionViewer`
  (`web/src/api/client.ts`).
- **Server** (`internal/server/viewer_state.go`): the PUT body must be a JSON
  object. A body over 64 KB gets 413, and invalid JSON or a non-object gets 400.
  Success is 204. The store is `Handler.viewerStates`, guarded by its own
  `viewerMu`, never `h.mu`, and a push never waits on the session's turn lock.
  There is no entry cap. `rekeyViewerState` moves the snapshot on `/reset-id`
  (`internal/server/handler_reset_id.go`, next to the shell rekey), and
  `dropViewerState` runs on the DELETE and in `HandleCloseSession`.
- **`viewer_state` tool** (`internal/server/viewer_state_tool.go`):
  - Server-only. It is built per session in `buildAgentSession`, beside the
    browser tool, so the TUI never has it.
  - No parameters. `Parallel()` is true. Allowed by default (`readOnlyDefaults`
    and `isReadOnlyTool` in `internal/agent/permissions.go`). Main agent only
    (`mainAgentOnlyTools`).
  - It reads the latest snapshot and never waits for a turn. The output is plain
    text:

    ```
    Preview: <path> (<kind>)
    Location: p.3/12 | slide 2/8 | Sheet!A1:B2 | § Heading | L10-L24 | view L1-L40
    Selection: "…"
    Visible text:
    <text block>
    Find: "q" 4 matches, current 2
    Last control: vc_1a2b3c4d applied
    Updated: 3s ago
    ```

  - `Updated` is the server's receive time, not the SPA's `updatedAt`. With no
    push for the session the output is "no preview open for this session".
  - Its schema and description are static, so the tools array stays byte-stable
    for a session.

### Transport 3: directives (viewer_control, useViewerControl)

- **`viewer_control` tool** (`internal/tool/viewer_control.go`) is a builtin. It
  is registered after `PreviewOpenTool`, listed in `expectedBuiltinTools`, in
  `BtwExcludedTools` (`internal/agent/ask.go`) and in `mainAgentOnlyTools`
  (`internal/agent/subagent.go`). It is Ask by default and `Parallel()` is false.
  It uses the same directive transport as `preview_open`: no round trip, no turn
  pause, and no wait on the session mutex.
- **Parameters.** `action` is one of `scroll`, `select`, `find`, `find_next`,
  `find_prev`, `find_close`.
  - `scroll` takes exactly one of `page`, `slide`, `line`, `heading`, or
    `direction` (`up` or `down`).
  - `select` takes exactly one of `text`, `lines` (`{start, end}`), or `range`
    (`"Sheet!A1:B2"` or `"A1:B2"`).
  - `find` takes `query`. `find_next`, `find_prev` and `find_close` take nothing.
  - Unknown or extra fields, empty strings and zeros are errors.
- **Result.** One single-line JSON directive:
  `VIEWER_CONTROL:{"id":"vc_<8hex>","action":…}`. The tool does not touch the
  viewer. The SPA applies the directive.
- **`useViewerControl(sessionId)`** (`web/src/lib/viewerControl.ts`) is mounted
  in App beside `usePreviewActivation`, and it is passed the active tab only
  while the chat side panel is the active kind. It applies each directive once
  to the session's registered controller, then writes the outcome to the record's
  `control` field and pushes it. Its seeding, live-buffer and per-tick rules
  are in `docs/gotchas/viewer-control-directive-seeding-live-buffer.md`.

### Support matrix

Read is what `viewer_state` reports. Control is what `viewer_control` can do.

| Viewer | Read | Control |
|---|---|---|
| Markdown (rendered) | `heading` (nearest at or above the viewport top), visible text, selection | scroll heading or direction; select text (the DOM range comes from the find index and is reported as a selection); find open, next, prev, close via `useDomFind` (helpers in `web/src/components/Preview/viewerControlDom.ts`) |
| Word (`DocxViewer`) | same as Markdown | same as Markdown |
| PDF (`PdfViewer`) | `page` and `pageCount`, the visible page's text-layer text (null until that layer is built), selection | scroll page or direction; select text opens find on that text (no DOM text range is exposed); find via `usePdfFind` |
| PPTX (`PptxViewer`) | `slide` and `slideCount`, the current slide's block text, selection | scroll slide or direction; select text in the current slide; no find |
| HTML (`HtmlViewer`) | path and kind only. The sandboxed iframe cannot be read | find only, via `useIframeFind`. `HtmlViewer` now takes a `path` prop |
| Monaco (TextViewer, and the markdown "Edit as text" branch) | selected lines and text; visible lines and text, via `useMonacoViewerReport` (250 ms throttle, also on content change) | scroll line or direction; select lines or text; find open, next, prev, close through `model.findMatches` (no FindBar UI). `FileEditor` gained `onEditorMount` and `onViewportChange` for this |
| Excel (`ExcelViewer`, Univer) | active sheet, active range, selected cells as TSV | select range via `UniverSheetHandle.activateRange` (`FRange.activate()` plus `setActiveSheet`); nothing else |
| Image, media, json, sqlite, mermaid | path and kind only | not supported |

An unsupported target returns "not supported by this viewer". A failure (no
match, out of range, sheet not found) is reported verbatim as the directive's
`control` error.

### Prompt-cache rationale

The send-time line rides in the user message, which is the volatile tail. The
push and the directives never reach `system`. The viewer's state reaches the
model only as tool output (`viewer_state`, and the `viewer_control` result), and
both tool schemas are static. The send-time context is still a non-image `@path`
token, so `buildOpenAIContentWithImages` (`internal/agent/client.go`) sends it as
plain text. See `docs/concepts/prompt-cache-stability.md`.

### Verified

- 2026-11-06, headless Chrome (htrcli): markdown scroll reports
  `§ Persistence (Decision 2 superseded)` plus the visible text. A drag-selected
  paragraph reports `selected: "…"`, and the chip switches to "selection".
  `sales.xlsx` reports `(excel, Data!A1) selected: "Date"`. `resume.pdf` reports
  `(pdf, p.1/1) visible: "…"`.
- 2026-11-07, headless Chrome: PUT bodies observed for `README.md` (heading
  "ocode" and visible text) and for `sales.xlsx` (`Data!A1`, selection "Date").
  The Monaco chip shows `view L1-L11`.
- Not live-verified: directive application inside a real turn (unit tests only),
  and whether Univer keeps the range after `activateRange`.

## Pitfalls

- **Mount-only listeners.** `usePreviewSelection` once bound its mouseup listener
  in a mount-only effect, so the Copy / Ask LLM toolbar never appeared on
  markdown previews. See `docs/gotchas/preview-selection-mouseup-listener-mount-only.md`.
- **Frontmatter as a heading.** A markdown file whose YAML frontmatter is
  followed by `---` renders as a setext heading. The "nearest heading" then
  became the whole frontmatter block. `headingFor` in `viewerDom.ts` now
  collapses whitespace to one line and caps the result at 120 characters.
- **Any transcript text can drive the viewer.** The `VIEWER_CONTROL:` sentinel is
  scanned in every transcript string, including user text and tool output. See
  the gotcha page.

## Known limits

These are the open follow-ups in `TODO.md` under "Open: reader mode + viewer
context follow-ups". The pixel screenshot is deferred; `viewer_state` is text
only.

- The HTML viewer reports no text. It has find only.
- A preview that failed to load still reports its path. `PreviewHost` calls
  `setViewerDoc` once the access check passes, and a later "file not found" from
  the viewer does not clear it.
- A spreadsheet's active cell is always reported as a "selection".
- The chip and the Ask-LLM chip can both show for the same file.
- `viewer_state` is only as fresh as the last push (300 ms throttle), and it
  exists only for web and desktop sessions.
- Directive application inside a real turn and the Univer range after activation
  are not live-verified.

## Related

- `docs/concepts/preview-find.md`: the find engines that the find directives drive.
- `docs/gotchas/viewer-control-directive-seeding-live-buffer.md`: the directive hook's rules.
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
- `docs/concepts/prompt-cache-stability.md`: the cache rules the tail and tool
  schemas follow.
