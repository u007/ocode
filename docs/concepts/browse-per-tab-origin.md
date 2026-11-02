---
type: Concept
title: Embedded Browser — Per-Tab Root-Mounted Origin for Root-Basepath SPAs
description: Why client routers (TanStack Router SPA mode, React Router, Vue Router) failed inside the embedded browser with "Expected to find a match below the root match", and how local and remote tabs now load on a root-mounted per-tab origin: one loopback listener per (stateKey, scheme, host), a stable persisted port, a per-tab cookie, a reserved route for foreign upstreams, and a root-mode request header for remote projects.
resource: internal/browse/tab_origin.go
tags:
  - browser
  - browse
  - remote
  - spa
  - routing
timestamp: 2026-11-02T00:00:00Z
status: active
---

# Embedded Browser — Per-Tab Root-Mounted Origin for Root-Basepath SPAs

## Problem

A proxied page is served under the prefix route
`{prefix}/b/{stateKey}/{scheme}/{host}/{path}` (`route.go`, `parseTarget`), so the
document's `location.pathname` is the proxy path, not the upstream path. A client
router with a root basepath (TanStack Router in SPA mode, React Router, Vue Router)
matches routes against that pathname, finds no route below the root, and throws:

    Expected to find a match below the root match in SPA mode

Reproduced on a remote SSH project, where the tab sat at
`/api/remote/<host>/browse/b/tab:…/https/localhost:3510/`.

## How it works now

**How it worked:** every top-level iframe loaded
`browseBase/b/<stateKey>/<scheme>/<host>/<path>` from the single browse base returned
by `GET /api/browse/config`, and the page read that path as its location.

**Where it broke:** the router's root match is the origin root, and on the shared
browse origin the root is the proxy route tree. No route below it matches. Rewriting
cannot change the pathname. A `history.replaceState` shim was rejected because it
breaks reload, deep links and bare-relative URLs.

**What changes:** a local client gets a loopback origin of its own for each tab, and
that origin's root IS the upstream. The document path is the upstream path, so
`location.pathname` is the upstream path. The SPA calls `POST /api/browse/tab-origin`
on every load and gets back the origin plus a one-use grant. Clients the endpoint
refuses (409) keep prefix mode.

### Local tabs: listener, port, cookie, grant

- `tabOrigins.bind` (`internal/server/browse_tab_origin.go`) opens one listener per
  binding. The binding key is (project host, stateKey, scheme, host), with project
  host empty for a local upstream. `browse.Server.ListenTab` registers it through
  `openTab`. A binding can be open once, and a stateKey holds at most
  `maxTabListenersPerState` (8) listeners.
- Bind address: the listener binds the SPA origin's hostname
  (`browseProxyBindHost`, or 127.0.0.1 when the SPA origin does not parse). A page on
  `localhost` is then same-site with its tab. The origin the endpoint returns is built
  from the request's own host (`requestHostName`), not from the listener address, so
  the iframe stays same-site with the page and the per-tab cookie is not third-party.
- Scheme: plain HTTP, unless the server has a local TLS certificate (`localTLS`). Then
  the listener is a sniff listener that serves TLS and plain HTTP on one port, and the
  origin is `https` (`browseProxyScheme`).
- Stable port: `bind` tries the saved port first. If that port is in use
  (EADDRINUSE), it binds a new one and logs it. That tab's origin-keyed storage then
  resets.
- Persistence: `<GlobalDataDir>/browse/tab-ports.json` (`tabPortStore`). Entries are
  keyed by (stateKey, scheme, host[, project host]). Each write takes the cross-process
  lock (`tab-ports.json.lock`), merges its own key, and evicts least-recently-used
  entries past 512. Every successful POST writes the entry.
- Cookie: `tabCookieName` is `ocode_browse_` plus the first 16 hex characters of
  SHA-256(stateKey). Cookies are scoped by host, not by port, so every tab origin on one
  host shares one jar. The per-tab name keeps two tabs from overwriting each other's
  session. The cookie path is `/` (root mount).
- Binding check: `serveBoundTarget` answers 403 "state key mismatch" when the target's
  stateKey differs from the authenticated session's. A cookie minted for one panel
  therefore cannot read another panel's upstream. This is the cookie-tossing defence.
  The check compares the stateKey only (see Known gaps).
- Grant: a `__grant` redeemed on a binding's own origin sets the per-tab cookie and
  redirects to the clean path (`authenticate`). A grant minted for another panel is
  refused (`refuseGrantForPanel`). A grant on the reserved foreign route is refused and
  left unconsumed (`refuseGrantOnForeignRoute`).

### Reserved route for foreign upstreams

- Every tab origin reserves `/__ocode/b/`. `tabTarget` reads a path under that prefix as
  `{scheme}/{host}{path}` (`parseForeignRoute`: http or https only, host with optional
  port, no userinfo). Any other path is the binding's own upstream. An app path that
  begins with `/__ocode/b/` cannot be served on a tab origin.
- A foreign upstream is a `target` with `Foreign` set. Its binding is kept in
  `BindScheme`/`BindHost` (`route.go`, `binding()`). A reserved-route URL that names the
  origin itself resolves to the binding, and a document on it is 307-redirected to the
  binding's own path (`selfRouteRedirect`), so `location.pathname` stays the upstream path.
- Foreign document (top-level, or an iframe, since `isDocumentRequest` counts
  `Sec-Fetch-Dest: iframe`): the tab origin does not serve it. `serveBoundTarget`
  publishes a nav event (mode `local` for a private host, `chrome` otherwise) and
  answers 204. The SPA then loads that upstream through its own tab origin. This is a
  handoff, not a redirect: the iframe follows nothing. A 3xx from the binding to a
  foreign document is handed off the same way (`handOffNav`, `local.go`).
- Foreign subresource: proxied through `handleLocal`, with the foreign target. The
  SSRF-safe dialer, the local-mode gate for a private host and the cookie jar all apply,
  as in prefix mode. A redirect from the binding to a foreign subresource is rewritten
  onto the reserved route (`mapURL` with `RootMounted`).
- Referer: a foreign request receives `scheme://<binding host>/` in place of the page's
  Referer (`trimForeignReferer`). An https-to-http downgrade sends none. Requests to the
  binding's own upstream forward the Referer unchanged.
- Rewriting and capture: root mode maps same-upstream URLs to plain paths and any other
  upstream to the reserved route (`rootPathFor`, `mapURL`). `capture.js` does the same
  for fetch, XHR, WebSocket, and script and link URLs, in its `rootMode` branches
  (`rerouteRoot`, the WebSocket wrapper, `originalURL`). It learns the binding from
  `rootMode`, `scheme` and `host` in the injected bootstrap (`injectCapture`, `capture.go`).
  Prefix mode omits those fields.

### Remote projects: forwarding tab origin

- A saved remote project's tab is a forwarding listener on this machine
  (`ListenForwardTab`, keyed with the project host). It answers no request from local
  state. Each request goes to the host's own browse origin over the workspace tunnel
  (`connectedBrowseTunnel`, the resolver the prefix proxy also uses), at
  `/b/{stateKey}/{scheme}/{host}{path}`.
- The binding travels in the request header `X-Ocode-Browse-Root-Mode: scheme://host`
  (`RootModeHeader`, set in `forwardRemoteTab`). It cannot travel in the path, because a
  foreign upstream is forwarded on that same `/b/` path. The host answers in root mode
  (`handleRootModeBrowse`, reached from `handleBrowse` whenever the header is present).
  It sets `X-Ocode-Browse-Root-Mode: 1` (`RootModeMarker`) on every reply.
- A reply without the marker is never relayed. It becomes a 502 "remote host too old for
  root-mounted browse; restart the host" (`errRootModeUnsupported`). The sender fails
  closed. The host is never inferred from the path, and an older host that ignores the
  header is never mistaken for a root-mode one.
- The forwarder deletes the prefix header (`BasePathHeader`), keeps only this tab's own
  cookie (`keepOnlyCookie`), and never falls back to this machine's browse origin.
- A remote tab's grant is minted on the host through its API proxy (`mintRemoteGrant`,
  `remoteBrowseCall`, `/api/remote/<host>/api/browse/grant`), so the host's own session
  records it. The host's bearer token stays in the proxy.

### POST /api/browse/tab-origin

Request body: `{state_key, url, host}`. `host` may also come as `?host=`. If both are
given and they differ, the answer is 400. The 200 response is `{origin, grant, url}`,
where `url` is the origin, the upstream path, the upstream query, and
`__grant=<grant>`, followed by the upstream fragment if there is one.

`handleBrowseTabOrigin` checks in this order:

| Status | When | SPA reaction (`openBrowseTabOrigin`) |
|---|---|---|
| 400 | Bad JSON, empty `state_key`, body and query `host` differ, URL not http(s), no host, or credentials | Tab error |
| 409 `tab origin unavailable: this server runs in remote mode` | Server runs in remote mode | Capability answer: prefix mode, remembered per host for the session |
| 409 `tab origin unavailable: the client is not on this machine` | `browseClientIsLocal` is false (tailnet, phone, proxied loopback) | Same as above |
| 403 | `host` is not a saved project | Tab error |
| 503 `tab origin unavailable: …` | Listener bind failed (per-state cap, listen error) | Tab error. Not 409, so no prefix fallback |
| 503 `tab origin: browse origin not available` | Tab-origin manager or browse server not wired | Tab error |
| 500 | Persisting the port failed | Tab error |
| Host's status, wrapped as `tab origin: remote grant: …` | Remote grant refused by the host, e.g. 409 "remote host not connected" | Tab error |
| 200 | Success | Iframe loads `url`; page origin set to `origin` |

Two cases need care:

- A 409 whose message does not start with `tab origin unavailable` is not a capability
  answer. A 409 relayed from the host's grant call therefore shows as an error.
- A remote host that is still connecting answers 503 with `Retry-After` on the
  forwarded-request path (`connectedBrowseTunnel`). On the POST path, the grant relay
  copies no headers (`writeError` sets none), so the SPA sees a 503 without `Retry-After`
  and shows an error. See Known gaps.

The SPA treats a 409 with the `tab origin unavailable` prefix as the capability answer.
It remembers that answer per host for the session (`_tabOriginUnavailable`) and loads
prefix mode with a fresh grant per load (`mintBrowseGrant`, `browseSrc`). A 503 with a
numeric `Retry-After` is `connecting`: the panel retries after that delay and gives up
with a visible error once `REMOTE_STALL_AFTER_MS` has passed. Anything else is thrown as
an error on the tab, and prefix mode is not tried.

### SPA loading and message trust

- `BrowserPanel.loadInto` calls `openBrowseTabOrigin` for every iframe load, remote
  tabs included. A navigation, such as a foreign-document nav event that changes
  `s.url`, runs the load again. The remote origin is therefore read on every load.
- `browseOrigin.ts` holds the message gate: `browseOriginOf` and `isTrustedBrowseMessage`.
  `useBrowserMessages` accepts a message from the iframe only when its origin equals the
  origin of the page the panel last loaded (`pageOriginRef`: the tab origin, or the prefix
  base in prefix mode) and its source is the iframe's window. The page's own
  `ocode:browse:nav` report is ignored. The address bar follows server nav events only.

## Invariants

- A tab listener's binding is fixed for its life. A request chooses a resource on that
  upstream, never another upstream, except through the reserved `/__ocode/b/` route. That
  route owns its whole path prefix on every tab origin.
- Root mode is decided by the `X-Ocode-Browse-Root-Mode` header, never by the path. A
  remote reply without `RootModeMarker` is never relayed.
- A grant is redeemed only on its own binding's origin and only for its own stateKey.
  Grants on the foreign route are refused and left unconsumed.
- The per-tab cookie name is derived from the stateKey. `serveBoundTarget` accepts a cookie
  only when the target's stateKey matches.
- A foreign document is never served on a tab origin. It becomes a nav event and a 204.
  Foreign subresources pass the same checks as prefix mode: SSRF-safe dial, local-mode
  gate, cookie jar.
- A remote tab origin exists only for a saved project. The host's bearer token never
  reaches the browser. The one-use grant does, in the tab URL, and the forwarder sends
  only this tab's cookie to the host.
- A tab's port is persisted before its grant is minted (`bind`, then `persist`, then
  `tabGrant`). The store merges under the file lock and never replaces the whole map.
- The endpoint answers the capability refusal only as 409 with the `tab origin unavailable`
  prefix, and the SPA maps that exact answer to prefix mode. Every other failure is shown
  and never falls back.
- The SPA accepts a page message only when its origin is the page the panel last loaded
  and its source is the iframe window (`isTrustedBrowseMessage`).
- Revoke closes the panel's tab listeners and drops its persisted ports
  (`TestTabOriginRevokeClosesListeners`, `TestRemoteTabRevokeClosesForwardersAndHostSession`).
  For a remote binding it also revokes the host's session.

## Known gaps

These are the open items recorded in the `TODO.md` blocks "Browse per-tab origin"
and re-checked against the code. Items the code has since overtaken are left out.

- **Foreign iframe does not render.** A foreign document in an iframe on a tab origin
  gets a 204 and a nav event, so the embed stays empty. This is deliberate: a foreign
  page on the tab origin would share an origin with the binding's page. The rule for
  iframes is not decided.
- **Top-level POST to a foreign host loses its body.** The 204 handoff carries no body,
  so the SPA loads the foreign URL as a GET.
- **`Origin` header not trimmed.** Only Referer is trimmed. A foreign CORS or POST request
  carries the tab origin (`http://127.0.0.1:<tab port>`) in `Origin`.
- **JS navigation to an absolute foreign URL is not rerouted.** `capture.js` wraps fetch,
  XHR, WebSocket and script or link URLs, but not `location.href = …` or `window.open`.
  On a remote tab, such a page was observed not to render. Prefix mode has the same gap.
- **Foreign HTML as a subresource gets the binding's capture.** `<object>` and `<embed>`
  content from a foreign host is rewritten with the binding's `rootMode`, so its reported
  pathname and title are wrong.
- **Non-HTML terminal navigation (pre-existing, both modes).** `handleLocal` emits a
  terminal nav for any HTML response, so a same-upstream `fetch` of HTML can move the
  address bar. Foreign targets are exempt. The binding's own path is not.
- **Foreign route is decoded like prefix mode.** `parseForeignRoute` reads the decoded
  path, so `%2F` in an upstream path reaches the upstream as `/`. Fix it together with
  `parseTarget`.
- **No TLS bypass on tab origins.** `POST /b/{stateKey}/__bypass` is registered only on the
  prefix mux, so "Continue anyway" cannot reach a tab listener.
- **Session not bound to scheme or host.** The cookie check compares the stateKey only. A
  session minted through one listener of a stateKey is accepted by another listener of the
  same stateKey.
- **Default-port mismatch on the Go side.** `tabBindingHost` drops `:443` and `:80` from
  the binding, but `rootPathFor` and `mapURL` compare the literal host. A link that writes
  the default port is therefore foreign, and it is handed off as a document. `capture.js`
  compares `URL.host`, which the browser normalises, so the mismatch is on the Go side only.
  This comes from reading the code. No test covers it.
- **Public upstream hosts get tab origins with no gate.** A root-mounted binding for a
  public host takes the chrome-mode 204 path, and foreign routes always proxy. Decide
  whether public hosts should get tab listeners at all.
- **Revoke ordering race.** Browse revoke closes the listeners before it drops the
  bookkeeping. A tab-origin POST for the same panel that lands in between can reuse a
  closing listener. Harmless in practice.
- **`localTLS` wrap path is untested.** The tests run plain HTTP only, so the sniff-listener
  wrap and the https origin from `browseProxyScheme` have no test.
- **Port file written on every POST.** Each successful call rewrites `tab-ports.json` under
  the lock. Fine for navigation, not for polling.
- **Session cookie jar is in memory only.** Persisting `cookiejar.go` across restart is
  new scope, awaiting the user's confirmation. Stable ports keep a tab's origin, but its
  site cookies do not survive a restart.
- **Remote 503 loses `Retry-After`.** `mintRemoteGrant` relays the host's status through
  `writeError`, which copies no headers. The SPA therefore shows an error instead of
  retrying. Fix in `browse_tab_remote.go`.
- **Remote foreign nav events go to the host's bus.** A foreign document on a forwarded
  tab publishes its nav event on the host's SSE bus, not this machine's. The local SPA sees
  it only through the host's event stream. Not verified end to end.
- **One project host per panel is assumed.** Revoke (`forgetTabOrigins`, `remoteTabHosts`)
  treats every binding of a stateKey as belonging to one host. Scope by project host if a
  panel can span hosts.
- **Grant minted after the listener is bound.** A refused grant leaves the listener and its
  persisted port in place until the panel is revoked.
- **Capture bundle version skew.** `/__ocode_capture.js` comes from this machine's binary,
  while the bootstrap (`rootMode`, binding) comes from the host. A host on another ocode
  version is not checked.
- **WebSocket subprotocol path tested at the browse layer only.**
  `TestForwardTabWebSocketSubprotocolPassesThrough` covers the 101 path. No server-level test
  goes through the tab-origin POST.
- **Public upstream on a remote host.** The host serves it through its external transport.
  The host's SSRF-safe path is not covered by a test here.
- **Prefix proxy 503 change on remote connecting.** `handleRemoteBrowseProxy` answers 503 with
  `Retry-After` while a host connects (it used to answer 409). The SPA's prefix path has
  not been re-checked against this.
- **Reload burst after a foreign handoff.** One 302 handoff to a second upstream produced
  about 30 upstream GETs in about 2 s, then stopped. The cause is not established. The nav
  event carries `upstream + Path` without the query, so `?q=` is dropped and one reload
  follows.
- **Duplicated test helper.** The headless Chrome launcher in
  `internal/server/browse_tab_remote_repro_test.go` copies the one in
  `internal/browse/pathname_repro_test.go`. Share it if a third copy appears.
- **Slow eviction test.** `TestTabOriginStoreEvictsLeastRecentlyUsed` writes 513 entries one
  at a time (about 3 s).
- **Stable ports across restart rely on the WKWebView data store.** Plan 05 found no
  non-persistent store in Wails v3 beta.12 or ocode-desktop, so the default store is assumed
  to persist. Not confirmed at runtime.

## Verification

Automated, run on 2026-11-02 (macOS) for this page:

- Untagged Go tests: `go test ./internal/browse/ ./internal/server/ -run 'Tab|RootMode|Forward'`
  passes. The coverage is in `internal/browse/tab_origin_test.go` (binding immutable,
  per-tab cookie name, 403 state-key mismatch, 401 unauthenticated, duplicate binding refused,
  per-state cap, revoke and close, caller-closed listener frees its binding, root-mode
  rewrite emits paths), `tab_origin_foreign_test.go` (foreign subresource proxied, foreign
  document handed off as 204 with a nav event, local gate refuses a public tab for a private
  host, SSRF-safe transport, bad targets, grant refused on the foreign route, redirects,
  foreign `mapURL`, Referer rules, self-route is the binding, WebSocket passthrough) and
  `tab_remote_test.go` (root-mounted page through the tunnel, marker on every reply, binding
  is not the path, missing marker fails closed, refusal writes no forward, only the tab cookie
  is sent, grant refused on the foreign route, foreign route through the tunnel, WebSocket
  subprotocol). In `internal/server/`: `browse_tab_origin_test.go` (origin reuse, default
  port shares one binding, saved port reused after restart, busy saved port falls back and
  persists, non-local and proxied loopback clients get 409, unsaved host 403, conflicting hosts
  400, remote server 409, unwired 503, same-site loopback host kept, grant redeems to the
  upstream, store merge, LRU eviction, revoke drops the persisted port) and
  `browse_tab_remote_test.go` (forwarded page through the host, old host fails closed 502,
  connecting 503, port keyed by project host, revoke, revoke needs a stateKey).
- Chrome-gated repro tests: `go test -tags repro -run Repro ./internal/browse/ ./internal/server/`.
  They skip unless Chrome is at `OCODE_CHROME_PATH` or `/Applications/Google Chrome.app`, and
  they use a temporary profile. They passed on this machine: `TestReproPrefixModePathnameIsProxyPath`
  (the failing prefix shape, kept as a baseline), `TestReproTabOriginPageSeesUpstreamPathname`,
  `TestReproTabOriginForeignSubresourceThroughProxy`, `TestReproTabOriginSelfLinkNavigatesToOwnPath`
  (`tab_origin_self_repro_test.go`), and `TestReproRemoteTabPageSeesUpstreamPathname`
  (`internal/server/browse_tab_remote_repro_test.go`).
- Web: `web/src/components/Browser/browseOrigin.test.ts` and `BrowserPanel.test.tsx`, with
  the tab-origin cases (local tab loaded through its root-mounted origin with the grant in
  the URL, a tab-origin failure becomes the tab's error and never loads prefix mode, a
  connecting remote host retried after `Retry-After`, a remote origin re-read per load).
  41 tests pass across the two files under vitest.

Not done:

- The desktop WKWebView certificate-pin check (an iframe on `https://127.0.0.1:<port>/` against
  the pin in `cmd/ocode-desktop/localcert_darwin.m`). Plan 06 requires it; it has not been run.
- A real SSH-host end-to-end run (the aimsai2 TanStack SPA through the workspace tunnel),
  including reload and restart with storage kept. Not run.
- Windows and Linux. Not run.
- The 302 handoff to a second upstream was reported as verified in headless Chrome in
  `TODO.md`. It was not re-run for this page.
- HMR over a WebSocket on a tab origin. No test covers a binding-upstream WebSocket, so the
  HMR gap is unverified and not claimed as fixed.

## Related

- [Embedded Browser — Chrome Mode Streams Over the Main Origin](browse-stream-via-main-origin.md)
- [Embedded Browser — Root-Relative Redirect for Dev-Server Module Graphs](browse-root-relative-redirect.md)
- [Web/Desktop Server Project Scoping](web-server-project-scoping.md)
- [Browser agent tool](browser-agent-tools.md)
- Plan: `plans/2026-11-02-browse-per-tab-origin/INDEX.md`
