---
type: Concept
title: Browser DevConsole network capture (local vs CDP)
description: How the embedded browser's DevConsole Network panel is fed in Chrome/CDP mode versus local reverse-proxy iframe mode, including dedupe, redaction, body bounds, the UI expansion contract and the tail-follow scrolling of the output tabs.
resource: internal/browse/capture.js; web/src/components/Browser/DevConsole.tsx; web/src/components/Browser/useBrowserMessages.ts; web/src/lib/browserStore.ts
tags:
  - concept
  - web
  - browser
  - devtools
  - network
timestamp: 2026-11-24T05:02:06Z
---
# Browser DevConsole network capture (local vs CDP)

The Network panel has exactly two feeders, chosen by how the tab is being served,
and they have very different fidelity. In **Chrome/CDP mode** the browser's own
network stack reports `cdp.NetworkEvent` values (`internal/browse/cdp/manager.go:67`)
with request/response headers, post data, size and content type, and response
**bodies are fetched lazily** via `Network.getResponseBody` (`Target.GetResponseBody`,
`internal/browse/cdp/target.go:864`) only when a row is expanded. In **local mode**
(the reverse-proxy iframe) there is no CDP session at all, so
`internal/browse/capture.js`, injected into the page, is the only source: it wraps
`fetch`/`XMLHttpRequest`, reports response bodies (fetch: as a follow-up message;
XHR: inline on its row), and supplements the wrappers with a Resource Timing
observer for traffic it cannot see. Everything the panel shows is
display state pushed to `browserStore`; nothing is persisted.

| | Chrome / CDP | Local (reverse-proxy iframe) |
|---|---|---|
| Producer | `cdp.NetworkEvent` from the CDP network domain | `capture.js` wrappers + `PerformanceObserver` |
| Request ID | CDP `RequestID` | `local-<ts>-<seq>` (`capture.js:176`) |
| Headers | request + response, bounded/redacted in `target.go` | read by page JS, bounded/redacted in `capture.js` |
| Body | fetched on demand via `Network.getResponseBody` | fetch: a later `ocode:browse:networkbody` message; XHR: inline `responseBody` on the row |
| Timing / size | event fields | wrapper timing; Size also reads `content-length` (fetch and XHR) when present (`capture.js:613`, `capture.js:676`), else `transferSize`/`encodedBodySize` |
| No-JS-visible traffic | fully visible (beacons, `<img>`, `<script>`) | `<img>`/`<script>` markup and any request the page's JS did not issue: only via Resource Timing, method `-`; `sendBeacon` is wrapped and shows method `POST` + request body, but no status/headers/response body |

## Rules that are easy to get wrong

- **Bodies are on-demand in CDP, eager in local mode.** `DevConsole.toggleRow`
  calls `onRequestBody` ONLY for a truthy `requestId` that does not start with
  `local-` (`web/src/components/Browser/DevConsole.tsx:295`); a local body is
  already in the store (fetch: posted just after its row; XHR: on its row), and a
  synthetic row id (`` `${ts}-${i}` `` when a row has no requestId) must never
  trigger a CDP request for an id the backend never saw.
- **A fetch body is a second message, never a second row.** `capture.js` posts the
  `ocode:browse:network` row as soon as the response headers arrive
  (`capture.js:625`), before any body is read. The body follows as
  `ocode:browse:networkbody` with the same `requestId` (`capture.js:332`), handled
  by its own case in `useBrowserMessages.ts` (`:98`), which calls only
  `pushResponseBody` and never creates a row. A response that never settles (SSE,
  a long stream) therefore still shows its row. XHR is the exception: it has no
  async read, so its `responseBody` travels inline on the network row
  (`capture.js:682`) and `useBrowserMessages.ts` (`:89`) stores it from there.
- **Which bodies are read** (`capturesBody`, `capture.js:295`). Only `res.type`
  `basic`/`cors`/`default`, and never for `text/event-stream`,
  `multipart/x-mixed-replace`, ndjson/jsonl, `stream+json` or gRPC/gRPC-web
  (`STREAMING_TYPE`, `capture.js:281`), since those never end and a tee would
  buffer without bound. Otherwise a textual or untyped content type qualifies.
  Every length reads `clone().body` through `readCapped` (`capture.js:304`), never
  `clone().text()`: a text read buffers the whole body, and the page's own branch
  of the tee holds the same bytes. `captureBody` (`capture.js:328`) runs it; it
  stops after the chunk that crosses 256K, cancels the clone, and marks
  `truncated` exactly. A null body (204/304) is skipped, and a read that fails
  mid-stream is logged with `console.debug` while the row keeps no body.
- **Request bodies share one renderer,** `bodyText` (`capture.js:234`):
  `URLSearchParams` as its query string, `FormData` as `k=v` (a `File` as
  `[File name size]`), a `Blob` as `[Blob type size]`, an `ArrayBuffer` or typed
  array as `[binary N bytes]`, a `ReadableStream` as `[stream]`, and everything
  else as `JSON.stringify`. Fetch, XHR and beacon all go through `requestBodyText`,
  so a body is never shown as `{}`; one the renderer cannot stringify (a circular
  object) is logged with `console.debug` and the row keeps no request body.
- **Every row must expand.** The old `hasDetails` gate suppressed expansion for
  local rows that had no requestId/headers, which is the reported "cannot expand
  to see request header and response header and body" bug. Expansion is now
  unconditional (`DevConsole.tsx:535`); a row with nothing recorded renders
  explicit `No headers available` / `Body not captured in local mode` sections
  rather than refusing the click. Resource Timing rows never carry headers or a
  body, so they show the same two sections.
- **Dedupe absolute URLs, both sides of the reroute.** A wrapped `fetch`/XHR also
  appears in Resource Timing under the *rerouted* `/b/…` URL, so `beginWrapped`
  (`capture.js:492`) records BOTH the page URL and the routed URL, absolutized with
  `absUrl` (`capture.js:485`). A raw string comparison never matches and duplicates
  every request — this was a real bug, now fixed.
  - A record is in flight until the fetch settles (`endWrapped`, `capture.js:513`)
    or the XHR fires `loadend`, then lives `WRAP_GRACE_MS` (2 s, `capture.js:482`)
    because the timing entry is queued at response end.
  - A timing entry matches with `consumeWrapped` (`capture.js:518`), which removes
    the record, so a later request to the same URL is still shown. Only
    `initiatorType` `fetch`, `xmlhttprequest` or `beacon` can match
    (`isWrapperInitiated`, `capture.js:536`), so an `<img>` or `<script>` for the
    same URL is never swallowed, and a beacon's own timing entry cannot duplicate
    its wrapped row. The observer calls it at `capture.js:559`.
  - Known gap: a body that streams for more than 2 s after its headers can outlive
    the grace window, so its timing entry arrives unmatched and the request shows a
    second row.
- **Resource Timing has no method.** Observer-sourced rows (`<img>`/`<script>`
  markup, anything the wrappers cannot see) are reported with method `-` on
  purpose (`capture.js:569`); guessing from `initiatorType` would invent a verb.
  A wrapped beacon row is not observer-sourced and keeps its real `POST` verb.
- **The page document is reported once, from its FINISHED navigation entry.**
  Chromium delivers the one navigation entry to a buffered observer more than
  once — a still-loading one (duration/`loadEventEnd` 0, though `responseEnd` is
  already set) and the finished one — so a naive per-entry post renders the
  page's own document as several rows and a first-delivery post shows 0 ms.
  `capture.js` posts a `navigation` entry only when `loadEventEnd > 0` and at
  most once per document (`navPosted`, `capture.js:547`). Regression:
  `browseCaptureNetwork.test.ts` "reports the document once". Deliberate gap: a
  document whose `load` event never fires (a hung subresource) gets no document
  row at all — better than a duplicate or a 0 ms lie.
- **`sendBeacon` is fire-and-forget.** By browser design a beacon's response is
  discarded, so its row carries no status/headers/response body; the request body
  is capped and rendered by the shared `bodyText`, and the row is deduped against
  Resource Timing by `initiatorType === "beacon"` (`isWrapperInitiated`).
- **Readability is not negotiable.** Bodies are only read when the response is
  readable (see the gating rule above), and XHR reads `responseText` only for
  `responseType` `""`/`text` (`capture.js:680`). An opaque cross-origin response is
  reported with status and timing and NO headers/body — never invented.
- **Bounds and redaction** (header rule mirrors the CDP side,
  `target.go:314` `redactHeaders`; local copy `redactHeader`, `capture.js:187`):
  20 headers per side, 200-char values, and
  `authorization`/`proxy-authorization`/`cookie`/`set-cookie`/`x-api-key`/
  `x-auth-token`/`x-csrf-token` → `[redacted]`. Request bodies cap at 64K
  characters (`MAX_POST_DATA`, `capture.js:180`), response bodies at 256K
  characters (`MAX_BODY`, `capture.js:177`). Characters are JavaScript string
  length (UTF-16 code units), not bytes.
- **Body retention in `browserStore`** (`retainBodies`, `web/src/lib/browserStore.ts:254`,
  run by `pushNetwork` at `:517`):
  - a local body (`local-` request id, `isLocalRequestId`) is kept only for the
    newest `NETWORK_BODY_CAP` (20) local rows (`browserStore.ts:193`);
  - any other body (CDP, fetched on demand through `setResponseBody`) is kept
    while its row is still in the `NETWORK_CAP` (1000) ring buffer
    (`browserStore.ts:186`);
  - `setResponseBody` (`:553`) does not prune. A fetch body that arrives after its
    row can sit above the local cap until the next `pushNetwork`;
  - `clearNetwork` drops rows and bodies together.
- **Trust boundary is page → parent SPA only.** `capture.js` posts only to
  `spaOrigin` (`capture.js:44`). `useBrowserMessages.ts` drops any message whose
  `stateKey` differs (`:56`) and applies `isTrustedBrowseMessage` first
  (`:53`), which requires the exact document origin AND `event.source`
  (`web/src/components/Browser/browseOrigin.ts:33`). Everything downstream is
  display state, held in memory only and never persisted.
- **Body text is not redacted (decision recorded 2026-11-05).** Redaction would
  hurt auth debugging, and the body is data the page already holds, shown only to
  the parent SPA in an in-memory ring buffer. Sensitive headers are still redacted
  at capture (see Bounds above); this decision does not change that rule. If body
  redaction is ever wanted, do it at display time in `DevConsole` behind a reveal
  toggle, so CDP mode is covered as well.
- **Request bodies are not secret-scanned** (header values are) in local mode, and
  CDP mode has the same property — see TODO.md "local-mode request-body capture is
  not secret-scanned".
- **Tab clicks re-open, never collapse.** Clicking Console/Network/Performance
  calls `selectTab`, which sets the tab and forces the panel open
  (`DevConsole.tsx:141`); clicking an already-selected tab must not toggle it shut.

## Tail-follow behaviour (console, network, performance)

Console, Network and Performance are logs, so the one shared scroll container
(`data-testid="devconsole-content"`, `flex-1 overflow-auto text-[11px] leading-relaxed select-text`)
follows its tail: `pinToBottom` sets `scrollTop = scrollHeight` from a
`useLayoutEffect` — deliberately not `useEffect`, so the very first paint already
shows the newest row instead of flashing the old scroll position. `stickRef` is
"the reader is at the tail": `onContentScroll` clears it once the container is
more than `STICK_THRESHOLD_PX` (8 CSS px) from the bottom and re-arms it at the
bottom, so output arriving while the reader is scrolled up is never pulled back
down; it also clears on the wheel itself (`onContentWheel`, an upward
`deltaY < 0`) — but only while the list actually overflows
(`scrollHeight > clientHeight`), since a short list would never fire the `scroll`
event that re-arms the follow — so a burst of rows landing between the gesture and
its `scroll` event cannot pin the reader back down (`ChatPanel` uses the same
gate). Two layout effects split the work: one re-arms the follow and lands on the
newest row on every tab switch and every fresh open of the collapsed panel; the
other pins only while `stickRef` is true, keyed on `props.consoleEvents`,
`props.networkEvents`, `props.perfMetrics` and the panel `height` (a resize must
not leave the tail out of view).

**The DOM tab is exempt.** It is a tree, not a log: switching to it resets the
scroll to 0, and `DomTree` scrolls the selected node into view itself — its
passive effect runs after the parent layout effect, so it wins.

Pinned by the `follows the tail` block in
`web/src/components/Browser/DevConsole.test.tsx`: opens at the newest output,
follows new console rows while at the tail, leaves a scrolled-up reader alone,
honours an upward wheel before its `scroll` event lands, keeps following when an
upward wheel cannot move a short list, re-arms once back at the bottom, starts
Network and Performance at the bottom and re-arms their follow, and leaves the DOM
tab at 0. jsdom has no layout, so that block installs
`scrollHeight`/`clientHeight` getters on `HTMLElement.prototype` for each test and
deletes them in `afterEach` — a test added there cannot assume real geometry.

## Tests worth knowing

`web/src/components/Browser/browseCaptureNetwork.test.ts` evaluates the real
Go-embedded bundle once via `?raw` and pins the dedupe/limits/redaction behaviour
(mutation-verified); `useBrowserMessages.network.test.tsx` covers the message →
store path, `DevConsole.test.tsx` the expansion and on-demand-fetch contract, and
`web/src/lib/browserStore.test.ts` the body cap, and a `retainBodies equivalence` block checks the early-exit walk against a full-recompute reference. A headless-Chromium run against
the bundle in rootMode confirms fetch, XHR and image requests each appear once,
with headers and bodies where readable.

## Related

- [Embedded browser DOM inspector and annotations (local vs Chrome)](browser-dom-inspector-annotations.md) — the sibling DevConsole DOM tab, element picker, hover highlight and composer annotation, sharing this page's two-surface split.