# Embedded Browser — Headless Chrome (CDP) External Mode — Design Spec

Date: 2026-08-31 (rev 2, post adversarial review)
Status: Approved (supersedes the *External mode* sections of
`2026-08-30-embedded-browser-panel-design.md`; local mode, security
model, frontend surfaces, and store contract from that spec remain in
force unless overridden here).

## Summary

Replace the HTML-rewriting external proxy with a **headless Chrome
driven over the Chrome DevTools Protocol (CDP)**. Non-private hosts are
rendered by a real Chrome process owned by the ocode server; the panel
shows a screencast on a `<canvas>` and forwards input. Console and
network telemetry come natively from CDP. Loopback / private hosts keep
the existing transparent reverse-proxy **local mode** (iframe, HMR
WebSocket passthrough) — it is the better dev-preview experience and is
already zero-setup.

Why: external mode was "best-effort by design" (OAuth, SRI, service
workers, `wss:`, hardcoded cross-origin API URLs all break under
rewriting). A real Chrome makes all of those work, and its
console/network feed is complete rather than patched-in.

## Goals

- Non-private URLs render in headless Chrome; OAuth, SRI, service
  workers, WebSockets, and cross-origin XHR work unmodified.
- Same `BrowserPanel` UI: address bar, status row, mode chip
  (`LOCAL` / `CHROME`), Console + Network drawer — unchanged
  components, unchanged store shape apart from the mode enum.
- Address bar stays **server-authoritative** (`browse_nav` SSE
  contract unchanged; it is the *only* nav path).
- Per-stateKey isolation: one Chrome browser context per stateKey
  (separate cookies/storage), torn down on revoke.
- SSRF guard at least as strong as today's connect-time dialer guard,
  covering **all** Chrome egress (documents, subresources, WebSockets,
  workers, out-of-process iframes).
- Chrome missing or crashing degrades to a visible error, never a
  server crash.

## Non-goals (v1) — tracked in TODO.md

- **Extensions.** Reserved config shape:
  `browser.extensions: [{ path: string, enabled: bool }]` →
  `--load-extension=<enabled paths>`. Note: branded Google Chrome
  removed `--load-extension` support in 2025; it still works in
  Chromium / Canary / Edge. Not read in v1 (rejected with an error if
  present, see Config).
- **Loopback access from a Chrome context** (e.g. an OAuth flow whose
  callback is `http://localhost:PORT/callback`). Blocked in v1 — see
  Egress guard. Future: explicit per-stateKey "allow localhost:PORT
  once" prompt or `browser.allow_loopback_ports: []`.
- **Windows.** `--remote-debugging-pipe` needs inherited fds 3/4;
  Go's `exec.Cmd.ExtraFiles` is unsupported on Windows. Chrome mode
  reports "not supported on Windows yet"; local mode is unaffected.
  Future: `--remote-debugging-port=0` + `DevToolsActivePort` file.
- Headed "attach to my Chrome" mode.
- Text selection / clipboard inside the canvas, file-upload dialogs,
  downloads, IME composition, printing.
- Screenshot / agent-tool access to the Chrome target.

## Mode routing

Decision function: `hostIsLiteralPrivate(host)` in
`internal/browse/route.go`, **extended** to also return true for
`*.localhost`. A mirrored TS predicate `isPrivateHost(host)` in
`browserStore.ts` (same rule set as `normalizeBrowseURL`'s
`PRIVATE_HOST_RE`) is used by the SPA only to choose which viewport to
mount before the first server nav event.

- Private → `mode: "local"` → existing `handleLocal` iframe path.
- Otherwise → `mode: "chrome"` → CDP target for the stateKey.
- Named LAN hosts (`mybox.lan`) are hostnames, not literals → Chrome
  mode → resolved to a private address → **blocked** by the egress
  guard. Documented limitation (same as today's external mode).

**Sticky Chrome rule.** Once a stateKey is in Chrome, every navigation
originating *inside* Chrome (link clicks, redirects) stays in Chrome.
A redirect to a private host inside Chrome is **blocked** by the
egress guard and surfaces as a nav error
(`"localhost is not reachable from Chrome mode — open externally"`)
with the "Open externally" button. The user leaves Chrome mode only by
typing a private host in the address bar or by back/forward onto a
`local` history entry. The Chrome target stays alive for the stateKey
(cookies, scroll) until revoke.

**Local → Chrome hand-off (mid-navigation).** `handleLocal` is an
`httputil.ReverseProxy`; today a 3xx to an external host is streamed
back and the *iframe* follows it out of the browse origin (then dies on
`X-Frame-Options`). New behaviour, in `ModifyResponse`: if the response
is a document 3xx whose resolved `Location` is non-private, replace the
body with `204 No Content` and emit
`browse_nav {stateKey, mode:"chrome", url:<location>, status:0}`. The
SPA switches the viewport to `ChromeViewport`, whose socket then
navigates the target to `url`. Likewise, `handleBrowse`'s non-local
document branch (where `handleExternal` used to be) emits the same
nav event and answers `204` — it never proxies.

## Chrome process (Go, `internal/browse/cdp/`)

### Binary discovery

Order: `browser.chrome_path` in ocode config → `OCODE_CHROME_PATH`
env → platform defaults:

- macOS: `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`,
  then Chromium, Google Chrome Canary, Brave Browser, Microsoft Edge
  `.app` equivalents.
- Linux: `google-chrome`, `google-chrome-stable`, `chromium`,
  `chromium-browser` on `$PATH`.
- Windows: not supported in v1 (see Non-goals).

Not found → `browse_nav {mode:"chrome", status:0, error:"chrome not
found — set browser.chrome_path"}`; panel shows the error badge with
"Open externally" still available. Logged once per server.

### Launch

Exactly **one Chrome per ocode server process**, started lazily on the
first Chrome-mode navigation, through the shared supervisor path:

- `internal/server.Server` owns a `*tool.ProcessSupervisor`, created
  in `New` and shut down (`Shutdown(ctx)`) from `Server.Shutdown`
  before the browse listener closes. (Neither `internal/server` nor
  `internal/desktop` has one today; only the TUI and agents do.)
- Add `tool.ProcessKindBrowser` to the `ProcessKind` enum.
- Add an exported helper `tool.StartSupervised(sup, cmd *exec.Cmd,
  reg ProcessRegistration) (ProcessRecord, error)` that applies the
  same `SysProcAttr` process-group setup as the bash path
  (`proc_unix.go`), starts the command, and registers it with
  `OwnsProcessGroup: true`. `cdp/` never calls `exec.Command` raw.
- `Browser.close` (graceful CDP shutdown) is installed via
  `sup.RegisterShutdownCallback`; the supervisor's default graceful
  (SIGTERM to the group) / force (SIGKILL) steps run afterwards.
- The cdp manager owns `cmd.Wait` (single waiter) and calls
  `sup.MarkExited(id, code)` when it returns.

Flags:

```
--headless=new
--remote-debugging-pipe            # CDP over fd 3 (in) / fd 4 (out); no TCP port
--user-data-dir=<os.MkdirTemp("", "ocode-browse-*")>   # per server process, removed on shutdown
--no-first-run --no-default-browser-check
--disable-extensions
--disable-background-networking --disable-sync --disable-component-update
--window-size=1280,800
```

Rules:

- `--remote-debugging-pipe` is mandatory: a TCP debugging port is a
  full-machine-control socket; the pipe is reachable only by the
  parent.
- `--no-sandbox` is **forbidden**. If Chrome refuses to launch
  sandboxed (root in a container), surface the error; do not weaken
  the sandbox.
- No persistent profile: browser contexts are ephemeral anyway, and a
  shared `browse-profile` dir would hit Chrome's singleton lock when a
  desktop app and `ocode serve` (or two projects) run at once.

Lifecycle:

- **Idle reaper**: when the target count has been zero for
  `browser.idle_timeout_minutes` (default 10) the process is closed;
  the next Chrome-mode navigation relaunches. Targets live until
  `Revoke` (panel/tab closed) — a collapsed panel keeps its target —
  so in practice the reaper fires only after every Chrome-mode surface
  has been closed.
- Crash (`Wait` returns while targets exist): every open target's
  stateKey gets `browse_nav {error:"chrome exited"}`; each viewport WS
  is closed with an `error` message; the next navigation relaunches.
- Server shutdown: supervisor callback runs `Browser.close`, then the
  supervisor terminates the group like any other child; temp profile
  dir removed.

### CDP client

Own minimal client (`cdp/conn.go`): JSON messages, `\0`-terminated,
over the pipe; request/response correlation by `id`; session
multiplexing via the CDP `sessionId` string; event subscription per
session; context-aware `Call(ctx, session, method, params, &result)`.
Roughly 200 lines. No `chromedp`/`rod` dependency: they are tens of MB
of generated bindings for the ~20 methods we use.

Methods used: `Target.createBrowserContext` (with `proxyServer`),
`Target.createTarget`, `Target.attachToTarget` (`flatten:true`),
`Target.setAutoAttach` (`autoAttach:true, waitForDebuggerOnStart:true,
flatten:true` — so OOPIFs/workers are attached and resumed
deterministically), `Runtime.runIfWaitingForDebugger`,
`Target.closeTarget`, `Target.disposeBrowserContext`, `Page.enable`,
`Page.navigate`, `Page.reload`, `Page.getNavigationHistory`,
`Page.navigateToHistoryEntry`, `Page.startScreencast`,
`Page.stopScreencast`, `Page.screencastFrameAck`,
`Emulation.setDeviceMetricsOverride`, `Runtime.enable`,
`Network.enable`, `Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`,
`Browser.close`.

Events consumed: `Page.screencastFrame`, `Page.frameNavigated`
(main frame only), `Page.navigatedWithinDocument`,
`Network.responseReceived` (type `Document`, main frame → status for
the address bar), `Network.loadingFailed` (incl. `blockedReason`),
`Runtime.consoleAPICalled`, `Runtime.exceptionThrown`,
`Target.attachedToTarget`, `Target.targetCrashed`.

### Target per stateKey

`cdp.Manager` maps `stateKey → target{browserContextID, targetID,
sessionID, viewport}`. Created on first Chrome-mode navigation for the
key; `Revoke(stateKey)` (already called when the panel/tab closes)
closes the target and disposes the context. A stateKey has at most one
target; a second viewport socket for the same key (React StrictMode
double-mount, reconnect) replaces the previous subscriber, it does not
create a second target.

### Egress guard (SSRF)

Enforced **outside Chrome**, at connect time, exactly like today's
dialer:

- `cdp/egress.go` runs one in-process HTTP forward proxy on
  `127.0.0.1:0` (plain `GET/POST…` forwarding + `CONNECT` tunnelling)
  whose outbound connections go through the existing
  `newSafeDialer(allowPrivate=false)` (`internal/browse/dialer.go`).
  The dialer's `Control` hook classifies the **resolved** connect
  address with `isPrivateIP` — zero DNS-rebinding window, IPv4-mapped
  IPv6 unmapped, cloud-metadata ranges in the block list.
- Every browser context is created with
  `Target.createBrowserContext {proxyServer:"127.0.0.1:<port>",
  proxyBypassList:"<-loopback>"}` so **all** traffic from that context
  — documents, subresources, WebSockets, dedicated/shared/service
  workers, out-of-process iframes, prefetch — is routed through the
  proxy. `<-loopback>` removes Chrome's implicit loopback bypass so
  `localhost` also goes through (and is blocked).
- The proxy accepts connections only from Chrome: it checks the peer is
  loopback and requires a per-launch random `Proxy-Authorization`
  credential passed via `proxyServer` user-info; anything else gets
  `407`.
- Non-`http(s)` top-level schemes (`file:`, `chrome:`, `devtools:`,
  `data:`, `javascript:`) never reach the proxy (Chrome handles them
  internally), so the manager rejects them in `Page.navigate` (address
  bar path) and, for in-page navigations, `Page.frameNavigated` to a
  non-`http(s)` main-frame URL triggers an immediate `Page.navigate`
  to `about:blank` + nav error. (Chrome already refuses `http→file:`
  renderer-initiated navigations by default.)
- Blocked requests fail inside the page; the `network` drawer row
  comes from `Network.loadingFailed` with `blocked:"private address"`
  when the proxy answered `403`.

No `Fetch` interception is used for policy (it misses WebSocket
handshakes, OOPIF and worker requests). No `--host-resolver-rules`:
Chrome does not resolve names when proxying.

## Transport (browse origin)

New endpoint on the browse origin: `GET /b/{stateKey}/__cdp` →
WebSocket (`gorilla/websocket`, same package as the terminal socket).
Registered on the browse mux as a more specific pattern than `/b/`
(Go 1.22 mux precedence) and runs its **own** auth: `handleBrowse`'s
`parseTarget` would reject `__cdp` as a scheme.

Auth: the SPA mints a grant as today (`POST /api/browse/grant`) and
opens `ws://<browse>/b/{key}/__cdp?__grant=<token>`. The handler
redeems the one-time grant (`authStore.redeem`) **in the upgrade
handler**; no cookie is involved (the `SameSite=Lax` `ocode_browse`
cookie is not attached to a cross-site WS handshake when the SPA is
opened as `localhost` and browse is `127.0.0.1`). Origin check:
`Origin` must equal `spaOriginFor(stateKey)` — **not** the terminal's
same-host `terminalSameOrigin` check, since the browse host ≠ SPA
host.

Server → client:

| type | encoding | fields |
|---|---|---|
| `frame` | binary | 8-byte **big-endian** header `[u32 width][u32 height]` (`DataView.getUint32(0)`, `getUint32(4)`) + JPEG bytes |
| `console` | JSON text | `level, args: string[], ts` |
| `network` | JSON text | `method, url, status, durationMs, ts, blocked?: string` |
| `error` | JSON text | `message` (fatal for this target; WS closes after) |

Navigation state is **not** sent on the WS: the existing
`browse_nav` SSE bus (`emitNav`) is the single nav path, fed from
`Page.frameNavigated` + `Network.responseReceived(Document)`. This
keeps the address bar contract unchanged and reconnect-safe.

Client → server (JSON text):

| type | fields |
|---|---|
| `nav` | `url` |
| `back` / `forward` / `reload` | — |
| `resize` | `w, h, dpr` → `Emulation.setDeviceMetricsOverride` + `stopScreencast`/`startScreencast` with new `maxWidth/maxHeight` |
| `mouse` | `kind: move\|down\|up\|wheel, x, y, button, clickCount, deltaX, deltaY, modifiers` |
| `key` | `kind: down\|up\|char, key, code, text, modifiers` |

Screencast: `Page.startScreencast {format:"jpeg", quality:70,
maxWidth, maxHeight, everyNthFrame:1}`. Each `Page.screencastFrame`
carries an integer **`frameSessionId`** (distinct from the CDP
`sessionId`) which is passed to `Page.screencastFrameAck` after the
WS write completes — Chrome stops emitting until acked, so a slow
client throttles Chrome rather than queueing. A static page emits no
frames, so **every** socket (re)connect calls `stopScreencast` then
`startScreencast` to force a first paint.

Coalescing: `mouse.move` is sampled at most every 16 ms client-side;
`wheel` deltas within a frame are summed.

## Frontend (`web/src/components/Browser/`)

- `BrowserPanel.tsx` renders the viewport by `s.mode`:
  `"local"` → existing `<iframe>`; `"chrome"` → `<ChromeViewport>`;
  `null` (before first nav event) → decided by `isPrivateHost(s.url)`.
- `ChromeViewport.tsx` — `<canvas>` sized to its container via
  `ResizeObserver`; draws each decoded JPEG (`createImageBitmap`);
  pointer/wheel/keyboard listeners → WS messages; `tabIndex=0`,
  focus ring; centered spinner until the first frame; inline `error`
  message with "Open externally".
- `useCdpSocket.ts` — owns the WS for a stateKey; mints a grant via
  the existing `mintBrowseGrant`; maps `console` / `network` →
  `pushConsole` / `pushNetwork`; exposes `send()`. Reconnects with
  backoff on close unless the close reason is `error`.
- Mode enum change `"proxied"` → `"chrome"` at every site:
  `browserStore.ts` (`NavEvent.mode`, `BrowserTabState.mode`),
  `AddressBar.tsx` prop type + chip label (`LOCAL` / `CHROME`),
  `sessionEvents.ts` `browse_nav` typing, Go `browse.NavEvent.Mode`
  comment. No other store shape change.

Efficiency: at most one `ChromeViewport` mounted per key; collapsing
the panel closes the WS and stops the screencast
(`Page.stopScreencast`) but keeps the target alive (state, cookies,
scroll) — expanding reopens the socket and restarts the screencast.

## Removal

Deleted with this change (no user-facing path remains):
`internal/browse/external.go` (move `upstreamOrigin` and
`classifyFetchError` — still used by `connlimit.go` / `server.go` —
into `route.go`), `cookiejar.go`, `headers.go`, their tests, the
`Server.transport` / `Server.jar` fields and the
`newSafeTransport(false)` call in `New`, and the `"proxied"` mode
string. `TestAuthenticatedNavigationProxiesExternal` becomes a
chrome-hand-off assertion (204 + nav event).

**Kept, because local mode depends on them:** `rewrite.go`
(`local.go` calls `rewriteHTML` for root-relative dev-server assets),
all of `capture.js` including the fetch/XHR/WebSocket/`script.src`
reroute patches (Vite HMR and lazy chunks need them), `capture.go`,
`route.go`, `dialer.go` (`newSafeDialer`, `isPrivateIP`, block list —
reused by the egress proxy), `connlimit.go`.

## Config

Add `Browser BrowserConfig` to `OcodeConfig` and `ocodeConfigFile`
(`internal/config/ocodeconfig.go`), handled as `raw["browser"]` in the
same key switch as the other sections (leftover keys currently land in
`cfg.Extra` unchecked; `browser` must be consumed explicitly):

```jsonc
"browser": {
  "chrome_path": "",          // optional explicit binary
  "idle_timeout_minutes": 10  // Chrome process reaper
}
```

`browser.extensions` is reserved: `applyBrowserConfig` returns an
error (`"browser.extensions is not supported yet"`) that
`LoadOcodeConfig` propagates so it fails at boot rather than being
silently ignored — verify that error path actually surfaces (it is
logged-and-continued in some load sites today; if so, fix that for
this key).

## Error handling

| Failure | Behaviour |
|---|---|
| Chrome binary missing | nav error badge "chrome not found — set browser.chrome_path"; log once |
| Windows | nav error "Chrome mode is not supported on Windows yet" |
| Chrome fails to start / pipe handshake timeout (10 s) | nav error "chrome failed to start: …"; `sup.MarkFailedToStart` |
| Chrome crashes | all targets get nav error "chrome exited"; WS `error` + close; relaunch on next nav |
| Target crashes (`Target.targetCrashed`) | that key only: nav error, target recreated on next nav |
| Navigation error (DNS, refused, TLS) | `Network.loadingFailed` (main document) → nav `{status:0, error}` |
| Egress blocked | request fails in-page; `network` row with `blocked`; if it was the main document → nav error "…not reachable from Chrome mode — open externally" |
| WS drops | viewport shows "reconnecting…", backoff reconnect (new grant each time); screencast restarted on reattach |

## Testing

Go (`internal/browse/cdp`):

- `conn_test.go` — framing, id correlation, session routing, event
  fan-out, ctx cancellation — against an in-process fake pipe peer.
- `manager_test.go` — one target per key, revoke disposes context,
  idle reaper, crash → error fan-out, `MarkExited` called once —
  against a stub CDP server answering the method set above.
- `egress_test.go` — proxy: CONNECT + plain forward to an `httptest`
  upstream; table over the block list incl. IPv4-mapped IPv6 and a
  hostname resolving to private; loopback blocked; bad/missing
  proxy credential → 407; non-loopback peer refused.
- `integration_test.go` — **skipped unless `OCODE_CHROME_PATH` is
  set**: real Chrome, navigate to an `httptest` page that logs to
  console and fetches a subresource; assert ≥1 screencast frame, the
  console row, the network row, and that `fetch("http://10.0.0.1/")`
  and `new WebSocket("ws://127.0.0.1:1/")` are both blocked.
- `internal/tool` — `StartSupervised` registers with the group flag
  set; `ProcessKindBrowser` in `Snapshot`.
- `internal/browse/server_test.go` — `__cdp` WS: grant required,
  one-time, `Origin` must match `spaOriginFor`; local 3xx→external
  yields 204 + chrome nav event; non-local document → 204 + nav.

Web (vitest):

- `ChromeViewport.test.tsx` — pointer/wheel/key → message mapping,
  resize → `resize` message, first-frame spinner, error state, frame
  header decode (big-endian).
- `useCdpSocket.test.ts` — `console`/`network` → store; grant on
  each (re)connect.
- `BrowserPanel.test.tsx` — mounts iframe for `local`, canvas for
  `chrome`, predicate-based choice when mode is `null`.
- `browserStore.test.ts` — `isPrivateHost` incl. `*.localhost`.

## Rollout

Single PR series, behind nothing: the old external mode is removed in
the same change that lands Chrome mode (no flag). Docs:
`docs/superpowers/specs/2026-08-30-…` gets a "superseded by" banner on
its External-mode sections; `docs/index.md` / architecture notes
updated; TODO.md gains entries for extensions (with the config schema
and the branded-Chrome caveat), loopback opt-in, and Windows support.

## Review-driven decisions (rev 2)

- Dropped the sticky-Chrome **loopback exception**: an attacker page
  could self-navigate to `127.0.0.1` and then read the unauthenticated
  ocode API or a dev server's `ACAO: *` sources. Loopback is blocked
  in Chrome contexts; OAuth-to-localhost is a documented v1 limitation.
- Egress moved from `Fetch.requestPaused` to a **per-context proxy**
  over the existing safe dialer: covers WS/OOPIF/workers and removes
  the resolve-then-connect TOCTOU.
- Removal list corrected: `rewrite.go` and `capture.js` reroutes are
  local-mode dependencies and stay.
- Grant redeemed directly on the WS URL; no `__grant` endpoint, no
  cross-site cookie/CORS dependency.
- Nav on SSE only (no duplicate WS `nav`).
- Supervisor integration spelled out (server-owned instance,
  `StartSupervised` helper, `RegisterShutdownCallback` for
  `Browser.close`, single `Wait` owner).
- Per-process temp profile dir; Windows deferred.
