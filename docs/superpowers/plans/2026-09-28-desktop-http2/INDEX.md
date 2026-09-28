# Desktop HTTP/2 — remove the 6-connection webview ceiling

## Problem

The desktop webview loads the SPA from `http://127.0.0.1:PORT`. Over plain
HTTP every engine (WKWebView, WebView2, WebKitGTK) speaks HTTP/1.1 and caps a
host at 6 concurrent connections. Measured 2026-09-28: the WebKit networking
process held exactly 6 sockets to the desktop server; at idle 2 were pinned by
long-lived streams (`/api/events` and a `HandleRemoteProxy` events stream for a
remote host). Every extra long-lived stream (remote hosts, logs, runs, file
search, second window) shrinks the pool; once all 6 are pinned, other requests
queue in the client and look like server hangs. The server itself answered
`curl` in <60 ms during the same sample.

## Decision (user-confirmed 2026-09-28)

- Serve the desktop webview over **HTTPS + HTTP/2** so all requests multiplex
  over one connection.
- Trust: **per-launch self-signed cert, pinned in-app** — no keychain changes.
- Platforms: **macOS, Windows and Linux** in this pass.
- Plain HTTP stays available **on the same port** (TLS sniffed from the first
  byte) so LAN/tailscale share URLs, `curl` via the debug handle, htrcli and
  TUI `/rc` keep working.
- Origin changes `http→https`, so do a **one-time localStorage migration**
  (terminal/editor tabs, unsaved drafts, layout, scrollback).

## Acceptance test

In the desktop DevTools console: open 7 concurrent `/api/events` fetches, then
fetch `/api/health`. Before: `/api/health` never resolves. After: it resolves
immediately, `curl -k -w '%{http_version}'` against the https URL prints `2`,
and `lsof` shows the webview holding ~1 connection to the port.

## Parts

1. [01-tls-listener.md](01-tls-listener.md) — local cert + dual-protocol h2 listener
2. [02-desktop-boot.md](02-desktop-boot.md) — desktop boot uses https (local + remote mode)
3. [03-macos-pin.md](03-macos-pin.md) — WKWebView cert pin
4. [04-windows-pin.md](04-windows-pin.md) — WebView2 SPKI pin
5. [05-linux-pin.md](05-linux-pin.md) — WebKitGTK cert allow
6. [06-storage-migration.md](06-storage-migration.md) — one-time http→https localStorage copy
7. [07-docs.md](07-docs.md) — docs, TODO, CHANGES

Windows and Linux can only be build-verified from macOS; their runtime check is
tracked in TODO.md.
