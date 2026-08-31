---
type: Gotcha
title: Embedded Browser — WebSocket Proxy 404
description: 'Gotcha: WebSocket connections through the embedded browser proxy fail with 404 during handshake due to missing or unreachable upgrade path in the browse server.'
tags:
  - gotcha
  - browse
  - websocket
  - proxy
  - embedded-browser
timestamp: 2026-08-31T00:26:58Z
---
## Problem

WebSocket connections through the embedded browser panel's proxy fail with HTTP 404 during the handshake. The browser client attempts a `ws://` upgrade to a proxy-proxied URL, but the proxy server does not recognise the upgrade request and returns a 404, preventing the WebSocket connection from establishing.

## Symptoms

- Browser DevTools console shows `WebSocket connection to '...' failed (404)`.
- The 404 originates from the browse server (e.g. `GET /__ocode_browse/ws/...` or the rewritten upstream path), not from the upstream target.
- HTTP requests to the same proxy origin succeed; only the upgrade path fails.
- The embedded panel renders the page content but no real-time features (console forwarding, network mirroring, live reload) work.

## Root Cause

The proxy server's HTTP handler does not implement or does not reach the WebSocket upgrade path. Common causes:

1. **Missing `IsWebsocketUpgrade` / `Upgrade: websocket` detection** in `handleExternal` or `handleLocal` — the handler falls through to the normal HTTP path, which returns 404 for the rewritten upgrade request URL.
2. **URL rewriting strips or corrupts the WebSocket upgrade path** — `rewriteHTML` rewrites `ws://` origins to the proxy origin, but the resulting URL does not match any registered route on the browse server mux.
3. **Router order / trailing-slash mismatch** — the upgrade request hits a catch-all or a missing trailing-slash route, returning 404 before the WebSocket handler can intercept.
4. **`Upgrade` header stripped by security header middleware** — `stripSecurityHeaders` removes the `Upgrade` header before the WebSocket detection logic runs, so the server never sees the upgrade request.

## Fix Checklist

- Verify the browse server mux registers a handler that checks `r.Header.Get("Upgrade") == "websocket"` (or equivalent) and calls `http.Hijacker` + `websocket.Upgrade`.
- Confirm rewritten `ws://` URLs map to an actual registered route on the browse server, including trailing slashes.
- Ensure `stripSecurityHeaders` does not remove the `Upgrade` header for WebSocket upgrade requests.
- Test with a minimal `ws://` connection to the proxy origin to isolate routing vs. upstream issues.

## Affected Code Paths

- `internal/browse/local.go` — `handleLocal` (local mode streaming proxy)
- `internal/browse/external.go` — `handleExternal` (external fetch proxy)
- `internal/browse/rewrite.go` — `rewriteHTML` / `mapURL` (origin rewriting for `ws://` origins)
- `internal/browse/server.go` — mux registration and route matching