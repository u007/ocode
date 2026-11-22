---
type: Concept
title: Embedded Browser — Tab-Strip Page Titles (local vs Chrome mode)
description: How the embedded browser's tab-strip title is fed in both rendering modes — local mode's capture.js MutationObserver posting ocode:browse:title to the SPA, and Chrome/CDP mode's immediate Page.loadEventFired probe plus a per-target Target.getTargetInfo poll backstop (Chrome no longer emits targetInfoChanged for a document.title change), the title+URL change gate that re-emits on same-title navigation, TitleEvent, browse.Server.emitTitle and the global browse_title SSE event — plus the stale-event URL guard, why the emitted event omits the URL, the measured load/tick latency, and what to re-check if Chrome starts reporting titles again.
resource: internal/browse/cdp/target.go
tags:
  - browser
  - browse
  - cdp
  - chrome
  - titles
  - sse
timestamp: 2026-11-22T01:45:00+08:00
status: active
---

# Embedded Browser — Tab-Strip Page Titles

The tab strip shows a page's `document.title` next to its URL. The title is
**display-only**: the address bar renders only server-authoritative `browse_nav`
events, never page-reported text (spoofing defence). A page title travels on its
own channel because it changes without a navigation.

There are two producers, one per rendering mode (`BrowseMode` in
`web/src/lib/browserStore.ts:112`: `local` = reverse-proxy iframe for private
hosts, `chrome` = headless-Chrome screencast for public hosts). Both end at the
same store action, `browserStore.setPageTitle` (`web/src/lib/browserStore.ts:436`).

## Local mode: capture.js MutationObserver

Proxied pages get `internal/browse/capture.js` injected into `<head>`
(`injectCapture`, `internal/browse/capture.go:22`). Its `watchTitle`
(`internal/browse/capture.js:836`) reports the title once at
`DOMContentLoaded`, then observes `document.head` for `childList`,
`characterData` and `subtree` changes (`:846`). Setting `document.title`
mutates the existing `<title>` text node, so no setter wrapping is needed.

`reportTitle` (`:829`) dedupes on the last reported title and posts
`{type: "ocode:browse:title", title, url: originalURL()}` to the SPA origin
(`:834`). `originalURL` (`:814`) reconstructs the **upstream** URL
(`scheme://host/path…`) from the proxy path, because `location` inside the iframe
is the browse route, not the upstream page.

SPA side: `useBrowserMessages` handles the message
(`web/src/components/Browser/useBrowserMessages.ts:108`), which calls the
`onTitle` callback wired in `BrowserPanel.tsx:117` — ultimately
`setPageTitle(key, title, url)`. The local path carries the reconstructed
upstream URL whenever the proxy path parses, so it goes through the stale-event
guard described below; an unparseable path posts `""`, which the panel turns
into `undefined` and the store applies unguarded (like the poll). An empty string
is an explicit
clear (the page removed its `<title>`), so the strip falls back to the manual
rename or "New tab" (`web/src/components/Layout/UnifiedTabBar.tsx:338`).

## Chrome mode: load-event probe, with a `Target.getTargetInfo` poll backstop

**Measured behaviour (Chrome 157.0.8103.79 headless, raw CDP probe with
`Target.setDiscoverTargets{discover:true}`):** after `Target.createTarget` on a
page that sets `document.title` from `setTimeout`, the only target events were
`targetCreated` (type `page`, title `""`) and one `targetInfoChanged` (title
`"localhost:8123"`) once the initial load set it. **No `targetInfoChanged`
arrived for the JS title changes**, while `Target.getTargetInfo{targetId}`
returned the live title (`"Rendered Title"`) on demand.

So the event-driven `targetInfoChanged` producer could only ever deliver a title
that existed at attach time. Two producers now read the live title instead: an
immediate probe on the top document's load, and a 2 s poll as a backstop.

### Immediate probe on load

`Page.loadEventFired` is session-scoped and carries no `frameId`, so it is the
top document's load — the first moment a freshly parsed `<title>` can be read.

- `startHandlers` registers it as `chLoad, c14`
  (`internal/browse/cdp/target.go`) and starts
  `go t.handleLoadEvent(chLoad)` (`target.go`).
- `handleLoadEvent` (`target.go`) calls `probeTitle()` for every event.
- A probe at `Page.frameNavigated` was deliberately **not** added: it would race
  the document's own `<title>` parse.

Measured live against real Chrome 157 headless (temporary
`OCODE_CHROME_PATH`-gated test, deleted afterwards): a static
`<title>Load Title</title>` reached `EmitTitle` **234 ms** after `Navigate`; a JS
`document.title = "Dynamic Title"` set 400 ms after load reached it **1.74 s**
after `Navigate` — one poll tick. Full trail:
`["", "about:blank", "about:blank", "127.0.0.1:64438/static", "Load Title",
"127.0.0.1:64438/dynamic", "Static Title", "Dynamic Title"]`. The URL-shaped
entries are Chrome's own pre-`<title>` placeholder reported by
`handleTargetInfoChanged`, which is browser-accurate and unchanged.

### Poll backstop

- `titlePollInterval = 2 * time.Second` (`target.go`) is a package var so
  tests can shorten it.
- `Target.startHandlers` starts `go t.watchTitle(stop)` (`target.go`) with a
  `titleStop` channel (`target.go`) closed by `stopHandlers`
  (`target.go`); the loop also exits on `t.conn.Done()`.
- `watchTitle` (`target.go`) probes **immediately** (a restored tab
  navigates right after Attach) and then once per interval.
- The probe is **sink-gated** (`target.go`): only a target whose
  `FrameSink` is live is probed. Agent headless tabs (`EnsureHeadless` /
  `OpenHeadless` attach with a nil sink) are not rendered in the strip, so
  polling them would be pure CDP traffic on the shared browser connection —
  one `Target.getTargetInfo` round trip every 2 s per target for its whole
  life, serialized against renderer-bound commands. A sink attached later
  re-arms at the next tick, so a panel opened onto an existing tab still gets
  titles.
- `probeTitle` (`target.go`) calls `Target.getTargetInfo` at the **browser
  level** (empty session id, `targetId` only) and ignores non-`page` target
  types.
- `emitTitleIfChanged(title, url)` (`target.go`) is the single change gate:
  it keeps **both** `lastTitle` and `lastURL` under `titleMu` and emits when
  either differs. The URL is part of the gate because the SPA's
  `browserStore.applyNavEvent` sets `pageTitle: null` on every URL change, so
  navigating between pages that share one title (most SPA routes, e.g. every
  route titled "Dashboard") would otherwise leave the tab on its fallback
  forever. `titleMu`, not `mu`, so the probe never queues behind a
  renderer-bound command holding `mu`.
- The emitted `TitleEvent` still carries **no URL** — the URL is used only for
  change detection (see below). An empty title is emitted **once** as an
  explicit clear; a fresh `about:blank`'s `""` equals the initial `lastTitle`
  and is never emitted.

Delivery chain:

1. `Target.probeTitle` (`internal/browse/cdp/target.go`, called by `handleLoadEvent` and by
   `watchTitle`) → `Target.emitTitleIfChanged` (same file) → `Manager.emitTitle`
   (`internal/browse/cdp/manager.go`) → `ManagerOptions.EmitTitle`.
2. Wired in `internal/browse/server.go:328` → `Server.emitTitle`
   (`internal/browse/server.go:583`). This also records the last non-empty
   title and last non-empty URL per stateKey (`procTitle`/`procURL`), which label
   `BrowserProcesses` rows — a clear does not delete that label.
3. `SetTitlePublisher` (`internal/browse/server.go:573`, wired in the server's browse setup in
   `internal/server/server.go`) → `publishBrowseTitle` (`internal/server/server.go`) →
   `bus.Publish("browse_title", "", "", ev)`.
4. `browse_title` is a **global/project-scoped** event: it must never be added to
   `sessionScopedEvents` (`internal/server/event_bus.go:56`); the stateKey rides
   in the payload.
5. SPA: `ROUTABLE_EVENTS` (`web/src/lib/sessionEvents.ts:224`) →
   `routeBusEnvelope`'s `browse_title` branch (`sessionEvents.ts:497`) →
   `browserActions.setPageTitle(t.state_key, t.title, t.url)`.

`handleTargetInfoChanged` (`internal/browse/cdp/manager.go`) is kept: it
still carries URL changes, and a Chrome build that reports a title there would
still deliver it through `emitTargetTitle` (`internal/browse/cdp/manager.go`) **with** a URL.

## The stale-event URL guard, and why the poll omits the URL

`setPageTitle` (`web/src/lib/browserStore.ts:436`) applies an event carrying a
URL only when `normalizeBrowseURL(url) === t.url`; otherwise it drops the update
as stale (closed-tab echo, or a title from before the current navigation) —
clears included. The contract is documented on `TitleEvent` in
`web/src/lib/browserStore.ts:91` and `internal/browse/server.go:37`.

The poll emits a **URL-less** `TitleEvent` on purpose. It already reads *this
target's* live document, so it cannot be an event about some other page. Adding a
URL could only make it match less often: a typed URL can arrive before the
canonical nav event updates the surface, and a rendered URL may carry a fragment
the surface's URL does not. Either mismatch would silently discard a real title.
A URL-less event skips the guard and is applied directly.

## Latency and clearing

- A title present at load lands with `Page.loadEventFired` (measured 234 ms
  after `Navigate`); a JS title change *after* load waits for the next poll
  tick, so it shows up within `titlePollInterval` (2 s) and the strip shows the
  previous title until then.
- A page that empties `<title>` produces one empty `TitleEvent` → `setPageTitle`
  trims it to `null` → the strip falls back (manual rename, else "New tab").

## If Chrome starts reporting titles again

The event path was never deleted, so a future Chrome that emits
`Target.targetInfoChanged` on a title change would immediately deliver a
URL-bearing title again. Before shortening or removing the poll, re-run the same
probe against the then-current Chrome (`Target.setDiscoverTargets`, create a page
that sets `document.title` from `setTimeout`, log every `targetInfoChanged`), and
check `Target.getTargetInfo` still agrees. Remember that a URL-bearing event
re-engages the stale-event guard, i.e. it can be dropped on a URL mismatch where
the poll's URL-less event would not.

## Tests

- `internal/browse/cdp/target_title_test.go:79`
  (`TestTarget_TitlePollReportsRenderedTitle`) with a stub CDP and a 10 ms
  interval pins: no event before a title exists, one event per changed title, no
  re-emit for an unchanged title, **same-title navigation must re-emit**, exactly
  one empty-title clear, and `URL == ""` on every event. Mutation-checked:
  dropping `|| url != t.lastURL` fails the same-title-navigation assertion, and
  removing `go t.watchTitle(stop)` fails the poll pin.
- `TestTarget_StopHandlersTwiceIsSafe` (`target_title_test.go:132`) pins that a
  revoke and a Chrome exit both calling `stopHandlers` do not panic on a second
  close of `titleStop`.
- `internal/browse/title_processes_test.go` pins the `procTitle`/`procURL` side
  effect of `Server.emitTitle`.
- `internal/server/browse_title_test.go` pins `browse_title` as a global-scoped
  bus event.
- Local mode is covered indirectly through `useBrowserMessages` / `browserStore`
  tests. `internal/browse/capture_test.go` only asserts injected markers and the
  route-base helpers in `capture.js`; the title observer has no test of its own.

## Related

- [Embedded Browser — Chrome Mode Streams Over the Main Origin](browse-stream-via-main-origin.md) - the Chrome screencast transport this title poll rides alongside.
- [Embedded Browser — Per-Tab Root-Mounted Origin for Root-Basepath SPAs](browse-per-tab-origin.md) - where local-mode iframes are served from.
- [Browser DevConsole network capture (local vs CDP)](browser-dev-console-network-capture.md) - the other per-mode telemetry channel.
- [Browser agent tool](browser-agent-tools.md) - agents reading page titles through the browser tool.
