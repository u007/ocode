---
type: Gotcha
title: Desktop webview uses HTTPS + HTTP/2 to escape the six-connection cap
description: 'Gotcha: the desktop webview loads https://127.0.0.1:PORT (per-launch self-signed cert pinned in-app) so it negotiates HTTP/2; plain HTTP stays on the same port via first-byte TLS sniffing; one-time localStorage migration from the old http origin.'
resource: internal/server/localtls.go; internal/desktop/boot.go; internal/desktop/storage_migration.go; cmd/ocode-desktop/localcert_*.go; cmd/ocode-desktop/localcert_darwin.m; web/src/lib/desktopStorageMigration.ts; web/src/main.tsx
tags:
  - gotcha
  - desktop
  - http2
  - tls
  - webview
timestamp: 2026-09-28T13:00:00Z
---
# Desktop webview: HTTPS + HTTP/2

## Symptom (2026-09-28)

Desktop UI endpoints hung while the TUI stayed fast. WebKit's networking
process held exactly 6 connections to the desktop server (HTTP/1.1 per-host
cap). Two were pinned by `/api/events` + a remote-host events proxy; remote
git status/log/stash calls (SSH, bounded at `remoteExecTimeout` = 30s) held
the rest, so every other request queued in the client. `curl` to the server
answered in <60 ms at the same time — the server was not wedged.

## Fix

- `server.NewLocalCert` makes a per-launch ECDSA cert for 127.0.0.1/::1/localhost.
- `server.NewSniffTLSListener` serves TLS (ALPN `h2`, `http/1.1`) and plain
  HTTP on one port by peeking the first byte (`0x16` = TLS). Plain HTTP stays
  for LAN/tailscale share URLs, the debug handle, `curl`, htrcli, TUI `/rc`.
  WebSockets still work over TLS: Go keeps h2 extended CONNECT off, so
  browsers open WS on an HTTP/1.1 TLS connection.
- Desktop `Handle.URL` is https (webview), `Handle.HTTPURL` is http (debug
  handle file, "Copy Debug URL").
- Pinning (never system trust): macOS adds
  `webView:didReceiveAuthenticationChallenge:completionHandler:` to Wails'
  `WebviewWindowDelegate` via `class_addMethod` and accepts only the pinned
  DER on loopback hosts; Windows passes
  `--ignore-certificate-errors-spki-list=<spki sha256>` to WebView2; Linux
  calls `webkit_web_context_allow_tls_certificate_for_host` (gtk3) /
  `webkit_network_session_allow_tls_certificate_for_host` (gtk4) on the default
  context. The cert is created before `application.New` because WebView2
  browser args are fixed there.

## Traps

- **Wails upgrade:** if `WebviewWindowDelegate` ever implements the challenge
  method itself, `class_addMethod` returns NO and boot fails with a dialog
  (`pin local cert: ... already implements`). Re-check the pin then.
- **Origin change:** http→https is a new localStorage origin. The first launch
  after the switch opens the http origin with `migrateTo=`; the SPA POSTs all
  localStorage to `/api/desktop/storage-migration` and redirects; the https page
  (`storageImport=1`) GETs it once and writes keys it lacks. Consuming it writes
  `desktop-https-migrated` next to `desktop-port`. This runs before any app
  module is imported (`main.tsx` dynamic-imports `bootstrap.tsx`) because
  stores read localStorage at import time.
- **HTTP/2 is not unlimited:** Go's default is 250 concurrent streams per
  connection; beyond that the client queues.
- **The cap was hiding server load.** Without it, many projects can hit the
  server at once; remote execs are therefore capped per host (see
  `project-endpoint-isolation.md` rule 4).
- **Browsers on `ocode serve` still have the 6-connection cap** — the
  AGENTS.md "do not pin a connection per turn" rule still applies.
