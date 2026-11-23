---
type: Concept
title: Embedded browser DOM inspector and annotations (local vs Chrome)
description: How the embedded browser's element inspector, element picker, hover highlight and "add annotation" flow work in both surfaces — the Chrome/CDP screencast and the local reverse-proxy iframe — including the shared DOM tree model, the capture.js message protocol, composer-only delivery of notes and screenshots, invalidation rules and known limits.
resource: web/src/lib/domInspector.ts; web/src/lib/browserStore.ts; web/src/components/Browser/{ChromeViewport,BrowserPanel,DevConsole,DomTree,AnnotationPopover,annotation,useLocalInspector}.tsx; internal/browse/capture.js; internal/browse/cdp/target.go; internal/browse/cdpsocket.go; web/src/components/Browser/useCdpSocket.ts; internal/browse/tab_origin.go
tags:
  - concept
  - web
  - browser
  - devtools
  - inspector
  - annotation
  - cdp
timestamp: 2026-11-23T12:12:17Z
---
# Embedded browser DOM inspector and annotations (local vs Chrome)

Right-click in the embedded browser offers **Inspect element** and **Add
annotation…** in both serving modes. The DOM tree, highlight, picker and
annotation note all converge on one shape, but the data arrives over two very
different transports, and the local iframe has to capture its own events because
a cross-origin frame cannot hand them to the SPA.

## The two surfaces

| | Chrome / CDP | Local (reverse-proxy iframe) |
|---|---|---|
| Menu | `ChromeViewport` builds its own context menu on the screencast canvas | a cross-origin iframe cannot deliver `contextmenu`; `capture.js` posts it and `BrowserPanel` renders the same `Layout/ContextMenu` at iframe-rect + page coordinates |
| Tree source | `DOM.getDocument` (depth `-1`, `pierce: false`) over `cdp_request` | `inspector-tree` request/reply from `capture.js` |
| Node id | Chrome `backendNodeId` (number) from pointer and menu lookups, mapped to the tree's `nodeId` before use (see Node identity) | `childNodes`-index path, e.g. `"0/2/1"` |
| Highlight | SPA overlay div positioned from the `DOM.getBoxModel` `content` quad, multiplied by page zoom | a div drawn in-page by `capture.js`, so it follows nested scroll containers |
| Picker | canvas pointer-down → `DOM.getNodeForLocation` with `x/zoom, y/zoom` | capture-phase listeners in `capture.js` swallow the click, outline the hovered element; Esc cancels |
| Screenshot | crop the live screencast canvas (`cropScreencast`) | `html-to-image` runs inside the page and the result is cropped by the element rect |

## Entry points

- **Chrome mode.** `ChromeViewport`'s canvas `onContextMenu` → `openContextMenuAt`
  asks the page for the node under the pointer (`getNodeAt(x / zoom, y / zoom)`,
  then `describeNode` by its `backendNodeId`), then renders "Inspect element"
  (`inspectNode`) and "Add annotation…" (`openAnnotation`). Both resolve the node
  with `resolveBackendNode`, see Node identity below.
- **Local mode.** `capture.js` adds a **bubble-phase** `contextmenu` listener. It
  returns early when `e.defaultPrevented` — a page with its own context menu
  keeps it — and when the picker is armed. Otherwise it applies `wantsNativeMenu`
  (below) and, if the native menu is not left in place, calls `preventDefault`
  and posts `{type:"ocode:browse:contextmenu", x, y, path, label}`.
  `BrowserPanel`'s `onContextMenu` handler offsets the viewer-relative coordinates
  by the iframe rect and renders the shared `ContextMenu`.
  - **The native menu wins** (`wantsNativeMenu`) when the page is not embedded
    (`window.parent === window`), Shift is held, the target is an input, textarea,
    select or contenteditable, or the page has a non-empty text selection. The
    ocode menu has no Copy, Paste or spellcheck, so those cases keep the browser's
    own menu.
- **Both modes** show the tree in the shared `DevConsole` DOM tab (`DomTree`),
  which has a Refresh button (`DevConsole`, `aria-label="Refresh DOM"`); its
  handler `refreshDom` lives in `BrowserPanel`. DevConsole switches to the DOM tab only on a
  transition: a tree fetch starts, or a node becomes selected. A user who then
  changes tab or collapses the console is left alone.

## DOM tree model

`web/src/lib/domInspector.ts` defines the single shape both surfaces normalize to:

```ts
interface DomNode { id: string; tag: string; attrs: Record<string,string>; text?: string; children?: DomNode[] }
```

- `normalizeChromeTree` flattens Chrome's `attributes: [{name,value}]` array into
  `attrs` and uses `nodeId` as `id`.
- `normalizeLocalTree` re-types a hostile page payload defensively; `capture.js`
  sends the same shape with `children` and path ids built from `childNodes`
  indices (text nodes included), so a path survives only while the document does.
- Helpers: `pathToNode` / `findNode` (ancestor chain / lookup by id),
  `selectorFor` (nearest `id` anchor, else an `:nth-of-type` chain),
  `serializeNode` (escaped HTML for "Copy HTML"), `rowLabel` (one-line row text).
- `DOM_NODE_LIMIT` (5000) caps the tree; a capped tree is flagged `truncated`.
  `capture.js` uses the same 5000-node cap (`INSPECT_MAX_NODES`) and a 200-char
  text cap.

Per-surface state is `BrowserTabState.dom` (`DomInspectState { mode, status,
error, root, truncated, selectedId, hoverId, pick, gen }`) in
`web/src/lib/browserStore.ts`. It is **display-only and never persisted**, and
`gen` is bumped per fetch so a reply for an older page/tree is dropped.

## Node identity (Chrome)

Chrome's `nodeId` is meaningful only for the document reply that produced it.
`DOM.getDocument` discards existing bindings, and Chrome omits `nodeId` from
`DOM.getNodeForLocation` until a document has been requested, so the pointer and
menu lookups return the stable `backendNodeId` instead. `resolveBackendNode`
(`ChromeViewport.tsx`) maps it to the current tree:

1. Map against the cached tree with `pushNodesByBackendIds`
   (`DOM.pushNodesByBackendIdsToFrontend`, `useCdpSocket.ts`). A `0` from Chrome
   means the node is no longer in the document.
2. If the mapped `nodeId` is missing from the cached tree (for example a modal
   opened after the fetch), refetch the tree once with `ensureDomTree(true)` and
   map again. There is no loop: a second miss returns `null`. Inspect then selects
   nothing, and an annotation opens without a selector.

`describeNode(backendNodeId)` sends the `backendNodeId` too. The server forwards
it as `opts.backendNodeId` with `nodeId` omitted, so a node known only by its
backend id can be described. A malformed `backendNodeIds` list (empty, or any
entry that is not a positive integer) is refused with `invalid_backend_node_ids`.

## Transport

- **Chrome.** `Target.GetDocument(ctx, depth)`, `Target.GetBoxModel(ctx,
  nodeId)`, `Target.GetNodeForLocation`, `Target.DescribeNode(ctx, nodeId, opts)`
  and `Target.PushNodesByBackendIdsToFrontend` in `internal/browse/cdp/target.go`.
  The `cdp_request` case in `internal/browse/cdpsocket.go` dispatches
  `DOM.getNodeForLocation`, `DOM.describeNode`, `DOM.getDocument`,
  `DOM.getBoxModel` and `DOM.pushNodesByBackendIdsToFrontend`. The optional
  interfaces (`nodeLocationTarget`, `describeNodeTarget`, `getDocumentTarget`,
  `getBoxModelTarget` in `internal/browse/server.go`, and `pushNodesTarget` in
  `internal/browse/cdpsocket.go`) let a target that does not implement them fail
  with the shared disconnect error. `domNodeIDParam` accepts a `float64`, `int` or
  string `nodeId`.
  **Timeouts are per method on both sides.** The server bounds each request with
  its own context: 15 s for `DOM.getDocument` (a whole-page reply is far slower
  than a node lookup) and 5 s for every other method. `cdpRequestTimeoutMs` in
  `useCdpSocket.ts` mirrors that split. The tree fetch therefore has its own 15 s
  bound and is not held to the 5 s one.
- **Local.** `capture.js` handles `inspector-tree` (request/reply),
  `inspector-highlight` (`path | null`), `inspector-pick-set`,
  `inspector-picked` (`path`/`label`/`cancelled`), `inspector-capture`
  (request/reply with `dataUrl | error`) and `inspector-reset`. Every one is
  accepted only through `isTrustedBrowseMessage` — the exact page origin **and**
  that iframe's own `window` — in `useLocalInspector.ts` (tree/capture replies)
  and `useBrowserMessages.ts` (`picked`/`reset`). Node text is rendered as React
  text in `DomTree`, never as HTML.

### Reply-echo guard

A local request and its reply share the SAME `type` — `ocode:browse:inspector-tree`
and `ocode:browse:inspector-capture` are both sent by the page and answered by
the page with that name. The `message` listener therefore cannot tell a request
from a reply by type, so it drops every message that carries a payload before
dispatching anything:

```js
if (d.root || "dataUrl" in d || "error" in d) return;
```

A page opened directly (top-level, `window.parent === window`) receives its own
reply through `postMessage`. Without the guard it re-reads that reply as a new
request and loops forever — observed live as an endless flood of `element not
found` capture replies (`capturePath` re-run with no `path`). In production the
page is embedded and the reply goes to the SPA, so the loop does not appear
there; the guard is what makes the direct-open case safe.
`TestCaptureBundleInspectorBlock` asserts the exact guard line.

## Highlight and picker

- **Chrome highlight** is an SPA div positioned from the `content` quad, scaled
  by page zoom. **Local highlight** is drawn by `capture.js` inside the page, so
  it moves with the page's own (nested) scrolling.
- **Chrome pick:** `onPointerDown` while `pick` is armed calls
  `getNodeAt(x / z, y / z)`, maps the returned `backendNodeId` through
  `resolveBackendNode`, and forwards no mouse event to the page. Pointer moves
  are suppressed while picking so a click never becomes a drag.
- **Local pick:** `capture.js` installs capture-phase `mousedown`/`click`
  listeners that swallow the event, outlines the hovered element, and posts
  `inspector-picked`; Esc posts a `cancelled` result.
- **Zoom invariant.** `Target.applyViewport` calls
  `Emulation.setDeviceMetricsOverride` with `width = viewport/zoom`, so the page
  is laid out at `viewport / zoom` and the canvas is `viewport` CSS px wide:
  layout CSS px = canvas CSS px / zoom. The picker, the context-menu node
  lookup, the hover outline and the annotation crop all apply that factor. The
  mouse-forwarding path does not; see the limits below.

## Annotation

`AnnotationPopover.tsx` collects a note; `annotation.ts` builds the message.

- **The note is injected into the chat composer, never sent.** Both surfaces
  call `dispatchBrowserAnnotation` (`annotation.ts`), which dispatches the window
  `ocode:annotation` event with `{ stateKey, text, image: Blob | null, imageName }`
  and returns whether a composer accepted it (the listener sets `accepted`).
  `App` resolves the target with `resolveAnnotationTarget` and calls
  `ChatInputHandle.addAnnotation` on that chat:
  - a `side:chat:<id>` key goes to that chat and nothing else;
  - a browser tab in a known project goes to the active chat when it is in that
    project, else to another chat in that project, never a chat in another project;
  - a browser tab whose project is unknown goes to the active chat.

  Only a chat with a registered composer qualifies. When none does, nothing
  receives the note: the sender keeps its popover and typed text open with
  `ANNOTATION_NO_CHAT_ERROR`.
- **Composer, not a turn.** `addAnnotation` writes the text into the draft
  first, then (if there is a screenshot) uploads the PNG through the existing
  `/api/uploads` path, so a slow or failed upload can never lose the note. A
  later send prefixes `@.ocode/uploads/<name>` and the agent's read tool can see
  it. The user edits and sends; the annotation itself never triggers a turn.
- **Text shape** (`composeAnnotationText`): `[Browsers note] <title> — <url>` /
  `` Element: `<selector>` `` / `Note: <remark>`. Every page-derived field
  (title, URL, selector, label) is collapsed to one line, length-capped and
  backtick-stripped, so a hostile `<title>` cannot inject instructions, a fake
  `@.ocode/uploads/...` ref, or a line break that reads as its own message. Only
  the user's note keeps line breaks.
- **Screenshot, Chrome:** `cropScreencast` crops the live screencast canvas
  around the `DOM.getBoxModel` rect (with padding and the same outline colour the
  picker used). A crop failure logs a warning to the console and still injects the note.
- **Screenshot, local:** `capture.js` lazily loads the vendored **html-to-image
  1.11.13** UMD bundle (`internal/browse/capture_lib.js`, served at
  `GET /__ocode_capture_lib.js`, MIT, refreshed with `make capture-lib`) inside
  the page. When the page fits, it renders `document.documentElement` and crops
  the element grown by 16 CSS px of real page pixels (`cropToDataUrl` with no
  margin, outline on the element itself), offset by `window.scroll` and clamped
  to the image by `clampRect`. The bundle URL is derived from the capture script's
  own `src` (`document.currentScript.src`, `/__ocode_capture.js` swapped for
  `/__ocode_capture_lib.js`), so a prefixed mount loads its prefixed sibling
  route; `reroute()`/`routeBase()` are not involved (they rewrite the page's own
  assets). Above ~12000 px per side or ~16M px it captures the element alone, with a
  16 px white margin (`cropToDataUrl` with the margin as padding), instead of the
  whole document. The inspector's own outline is filtered out of
  the render.

**Cancel and reopen invalidate the submit.** `annotationSeq` (BrowserPanel) and
`annotationSeqRef` (ChromeViewport) are request tokens. Opening a popover,
cancelling it and starting a submit each bump the token, and every await in the
submit checks it, so a capture that finishes after a cancel or a reopen neither
injects its note nor touches the popover. Pinned by `ChromeViewport.test.tsx`
("cancelling while the screenshot is pending dispatches no annotation") and
`BrowserPanel.test.tsx` ("cancelling while the screenshot is pending injects
nothing").

**The capture library is served on every mount of the capture script.**
`captureLibPath` (`/__ocode_capture_lib.js`, const in `internal/browse/tab_origin.go`)
is routed on the browse server mux (`internal/browse/server.go`), on each per-tab
origin (`serveTab`), on a forwarded remote tab (`forwardTabHandler`,
`internal/browse/tab_remote.go`), and on the main server's proxy mux
(`internal/server/server.go`). `capture.js` derives the library URL from its own
`src`, so a mount that serves the script without this route would break the lazy
load.

## Invalidation

A node id/path is only valid for the document that produced it, so the tree is
cleared on:

- a **real navigation** — `navigate` / `back` / `forward` / `applySharedNavigation`
  and `applyNavEvent` (URL changed) drop `dom`;
- a **mode flip** (`BrowserPanel` clears it when the effective surface changes);
- `capture.js`'s **`inspector-reset`**, posted on page `load`, because a
  reloaded document invalidates every path.

A **same-document `pushState` does not clear it** — the paths stay valid.

Chrome's `nodeId`s also go stale on every `DOM.getDocument`, which is why the
inspect and annotation paths map `backendNodeId`s against the tree they are about
to use (see Node identity). The
store test `browserStore.dom.test.ts` pins both the clearing and the
same-document keep.

## Verification status

- **Local mode is live-verified** end-to-end in a real headless Chromium, page
  served over HTTP and root-mounted: `inspector-tree` returned the page tree,
  the in-page outline drew and cleared, a trusted picker click posted
  `inspector-picked`, and `inspector-capture` returned a real 114×75 PNG of the
  82×43 button (16 px padding + outline). That run is what exposed the
  reply-echo loop above. It also observed a right-click on that top-level page
  post `contextmenu`; under `wantsNativeMenu` a page that is not embedded keeps
  the browser's own menu instead, so that observation no longer describes
  current behaviour. The embedded pass-through is pinned by
  `TestCaptureBundleContextMenuPassThrough`, a source-text check. The 114×75
  output is the same size on both crop branches, so it does not show which one
  ran; the full-document branch is pinned by `TestCaptureBundleFullContextCrop`,
  also a source-text check.
- **Chrome/CDP overlay position and screenshot crop at zoom ≠ 1 are unit-tested
  only** (`ChromeViewport.test.tsx` hover-zoom, `annotation.test.ts` crop math).
  They need an app-level check at a real zoom. `TODO.md` records the gap as
  "Chrome-mode inspector overlay/crop not live-checked at zoom ≠ 1".
- The same `TODO.md` records two neighbouring facts that are NOT this feature's
  regressions: Chrome mouse forwarding ignores page zoom (pre-existing, see
  Known limits), and `TestNetworkRowMarksProxyBlockedResponses` in
  `internal/browse/cdp` is a pre-existing timing flake — it fails without
  `-race` and passes with it, reproduced at HEAD via `go test -overlay`.
- **The live run was root-mounted only** (`rootMode: true`). The prefixed mount
  path is covered by tests (`basepath_test.go` asserts the injected
  `<mountPrefix>/__ocode_capture.js` src that the bundle URL is derived from),
  not by a live browser run.

## Known limits

- **Mouse forwarding is not zoom-corrected.** The mouse-forwarding path
  (`send({t:"mouse"})` in `ChromeViewport`) sends raw canvas px to
  `Input.dispatchMouseEvent`, while the page is laid out at `viewport / zoom`, so
  a forwarded click lands off-target at zoom ≠ 1. Recorded in TODO.md ("Chrome-mode
  mouse coordinates ignore page zoom"). The picker, the context-menu node lookup,
  the hover outline and the annotation crop are zoom-corrected.
- **Local screenshots depend on html-to-image.** Cross-origin images, fonts and
  stylesheets taint or go missing; `cacheBust: true` rewrites URLs (some servers
  reject the added query); iframes, `<video>` and `<canvas>` do not render;
  fixed/sticky elements and shadow-DOM content can drift. The element-only
  fallback avoids the largest canvas failures but not these.
- **Chrome captures only the live viewport.** The screencast canvas holds what
  is currently painted, so an element scrolled out of view crops to blank.
- **Text nodes are not selectable targets** for a highlight; `highlightTarget`
  returns `null` for a `#text` node.

## Tests worth knowing

- `web/src/lib/domInspector.test.ts` — Chrome/local normalization, hostile
  payloads, the 5000-node cap, `selectorFor` id anchor vs `:nth-of-type`,
  serialization and row labels.
- `web/src/lib/browserStore.dom.test.ts` — generation bumping, error-per-gen,
  selection disarms the picker, `clearDom`, and navigation vs same-document
  invalidation.
- `web/src/components/Browser/annotation.test.ts` — text composition
  (hostile-title collapse, caps, selector quoting), `cropScreencast`,
  `resolveAnnotationTarget` routing and `dispatchBrowserAnnotation` acceptance.
- `web/src/components/Browser/DevConsole.dom.test.tsx` — the DOM tab renders the
  tree, selection and hover wiring.
- `web/src/components/Browser/ChromeViewport.test.tsx` — inspect, annotation,
  hover outline scaled by zoom, picker (`getNodeAt(x/z, y/z)`, no forwarded
  mouse event), the context-menu lookup in CSS px ("looks the context-menu node
  up in layout CSS px, dividing canvas px by zoom"), the refetch-once remap
  ("refetches the tree once when the node is missing from the cached tree").
- `web/src/components/Browser/useCdpSocket.test.tsx` — DOM request correlation,
  the per-method timeout (rejects on timeout, ignores late replies) and
  `pushNodesByBackendIds` (`DOM.pushNodesByBackendIdsToFrontend`).
- `web/src/components/Browser/BrowserPanel.test.tsx` — local `contextmenu` →
  tree, and annotation → capture reply → `ocode:annotation` event.
- `web/src/components/Chat/ChatInput.annotation.test.tsx` and
  `web/src/App.annotation.test.tsx` — composer injection and target resolution.
- `internal/browse/capture_test.go` (`TestCaptureBundleInspectorBlock` — also
  pins the reply-echo guard line and the `scriptSrc`-derived library URL —
  `TestCaptureBundleFullContextCrop`, `TestServeCaptureLibBundle`,
  `TestCaptureLibRouteRejectsNonGet`), `internal/browse/tab_origin_test.go`
  (`TestTabOriginServesCaptureLib`) and `internal/browse/tab_remote_test.go`
  (`TestForwardTabServesCaptureLib`) for the library route on each mount.
- `internal/browse/cdpsocket_dom_test.go` (`TestCDP_GetDocumentRequestsWholeTree`,
  `TestCDP_GetBoxModelParsesNodeID`, `TestCDP_PushNodesByBackendIDsMapsToNodeIDs`,
  `TestCDP_PushNodesRejectsMalformedIDs`, `TestCDP_DescribeNodeForwardsBackendNodeID`)
  and `internal/browse/cdp/target_dom_test.go` (document depth, box model node id,
  backendNodeId kept without a nodeId, push-nodes round trip, describe by backend
  id, shared error vocabulary).

## Related

- [Browser DevConsole network capture (local vs CDP)](browser-dev-console-network-capture.md) — the sibling DevConsole panel and the same two-surface split.
- [Embedded Browser — Chrome Mode Streams Over the Main Origin for Non-Local Clients](browse-stream-via-main-origin.md) — how Chrome mode reaches a non-local client.
