---
type: Concept
title: Embedded Browser — Chrome Mode Streams Over the Main Origin for Non-Local Clients
description: Why the embedded browser tab hung on "Connecting to localhost:3510…" for a phone over Tailscale, and how non-local clients now get the server's Chrome screencast over the main origin (/api/browse/cdp/{stateKey}) instead of the separate browse-proxy listener.
resource: internal/server/server.go
tags:
  - browser
  - browse
  - chrome
  - cdp
  - tailscale
  - remote
timestamp: 2026-10-31T09:38:08Z
status: active
---

# Embedded Browser — Chrome Mode Streams Over the Main Origin for Non-Local Clients

## Problem

The web SPA's embedded browser loads a page in an iframe served by a separate
browse-proxy listener (`browseProxyPort`). That origin is deliberately
cross-origin from the SPA, so proxied pages cannot touch the app.

A client on another device (a phone over Tailscale) cannot always reach that
listener. It binds `127.0.0.1` or an un-mounted port, and the Tailscale mount
the server would publish for it (`tailscale serve --https=8444`, from
`tailscaleShare.browseExpose`) may not exist. The tab for
`https://localhost:3510/` then sat on "Connecting to localhost:3510…".

Before this change, the fallback base URL for a non-local client was
`scheme://<request host>:<browseProxyPort>`. That is unreachable when the
listener is loopback-bound, and it is mixed content when the SPA is HTTPS.

## How it works now

Where it breaks before the change: the panel always loads an iframe from the
browse base URL, so reachability of the browse listener is a hard requirement.

Current flow:

1. `GET /api/browse/config` returns `stream_via_main` next to `base_url`
   (`internal/server/server.go`, `browseOriginFor` → `handleBrowseConfig`).
   It is true only when the client is off-machine AND there is no origin it can
   reach: the same-site loopback origin is not available (client not local) and
   no Tailscale share is running. A tailnet client that can reach the shared
   HTTPS mount gets `false` and keeps iframe mode.
2. The SPA stores `streamViaMain` per host in `readBrowseOrigin`
   (`web/src/api/client.ts`). `getBrowseStreamViaMain(host)` reads it.
3. `BrowserPanel.tsx` `effectiveMode` returns `"chrome"` when the flag is set,
   AFTER the remote-workspace check and AFTER `userMode`, so an explicit
   "local iframe" choice still wins. A stream client gets Chrome mode even for
   private or localhost hosts.
4. `useCdpSocket.ts` mints the one-time grant as before. When the flag is set it
   opens the screencast socket through `browseCdpWsConnection(stateKey, grant, host)`
   instead of the browse listener. The same `host` drives the flag, the grant
   and the socket URL, so panel and socket cannot disagree.
5. The socket is `GET /api/browse/cdp/{stateKey}` on the main origin, behind
   `authMiddleware`, routed to `handleBrowseCDP`. That calls
   `browse.Server.ServeCDP(w, r, terminalUpgradeRespHeader(r))`
   (`internal/browse/cdpsocket.go`). `handleCDP` is now a thin wrapper over the
   same `ServeCDP`.
6. Chrome runs on the host machine, so `https://localhost:3510` is the host's
   own loopback. An SSH `-L` tunnel there serves it. Chrome-mode navigation
   already allows loopback and private targets: `Target.setTopLevelHost` calls
   `EgressProxy.AllowHost`.

The one-time `__grant` redeem and the SPA-origin match inside `ServeCDP` still
apply. The grant mint records the SPA's own `Origin`, so the main-origin
WebSocket passes that check.

## Invariants

- **`browseClientIsLocal(r)` is the single predicate.** It is true only when the
  peer is loopback AND no proxy forwarding headers are present AND the Host is
  loopback. It is shared with `browseProxyBaseURL`, so the two must not drift.
  The peer address decides, never Host alone: a proxied tailnet request arrives
  from `127.0.0.1` with a loopback Host.
- **`ServeCDP` is the only implementation.** Do not fork the screencast
  handler for the main-origin route.
- **The stream route stays behind `authMiddleware` and the grant.** Auth
  changes must keep the query token (`token=`) in the browser path and the
  `ocode.bearer.<token>` subprotocol in remote sessions, matching the terminal
  WebSocket.
- **Stream clients ignore `userMode`.** A non-local client always gets Chrome
  mode, because the iframe path cannot reach the listener from its origin.

## Known limitations and open items

- **SSH tunnels classify as local.** A client tunnelled over `ssh -L` or a port
  forward arrives from `127.0.0.1` with no proxy headers, so it is treated as
  the desktop webview. It gets the local-origin base and no stream flag. This
  is an existing shortcut (see the comment in `browseProxyBaseURL`). No header
  or listener tells the two apart reliably.
- **Remote-host config does not emit the flag.** `handleRemoteBrowseConfig`
  (`internal/server/handler_remote_browse.go`) does not return
  `stream_via_main`, so a remote-workspace host reads `false`. Remote-workspace
  mode forces the panel to `"local"` anyway, so this is only a gap if that
  changes.
- **One sink per shared tab.** Browser tabs are server-shared, so two devices
  use the same `stateKey`. `ServeCDP` keeps one socket per key and the manager
  keeps one `FrameSink` per target, so a second viewer takes over. The displaced
  viewer gets `{"t":"detached","reason":"superseded"}` and a close, NOT an
  `{"t":"error"}`: an error is fatal in `useCdpSocket`, so the panel would die
  silently. A detached panel stops auto-reconnecting and shows **Take over**,
  which calls `reconnect()` and re-attaches (the other viewer is then detached
  in turn).
- **Not yet verified live on a phone.** Coverage is unit and route tests only.

## Tests

- `TestBrowseConfigStreamViaMain`, `TestBrowseCDPRouteAuthAndOrigin`,
  `TestBrowseCDPRouteEchoesBearerSubprotocol` (real 101 handshake selects the
  offered `ocode.bearer.<token>`), `TestBrowseCDPRouteWithoutSubprotocol` (no
  empty subprotocol echoed) and `TestBrowseCDPWithoutBrowseOriginReturns503` in
  `internal/server/server_browse_test.go`.
- `TestPrepareRemoteCDPRequestStripsHostAndSetsPath` in
  `internal/server/handler_remote_cdp_test.go` (the `?host=` rewrite).
- `TestCDP_SecondSocketDetachesFirst` in `internal/browse/cdpsocket_test.go`.
- `web/src/api/client.browseCdp.test.ts` (token in query locally, subprotocol
  remotely, no token leak, `host` threading, state-key encoding) and the
  detached/reconnect cases in `web/src/components/Browser/useCdpSocket.test.tsx`.
- `BrowserPanel.test.tsx`: "renders a localhost page in Chrome (streamed) when
  the client is on another device".
- `useCdpSocket.test.tsx`: "streams over the main origin, not the browse
  listener, when the client is on another device".

## Related

- [Browser agent tool](browser-agent-tools.md) - agent-side access to the same Chrome tabs.
- [Embedded Browser — Root-Relative Redirect](browse-root-relative-redirect.md) - the other browse-listener redirect path.
- [Embedded Browser — WebSocket Proxy 404](../gotchas/embedded-browser-websocket-proxy-404.md) - the listener-side WebSocket failure mode this path avoids for stream clients.
