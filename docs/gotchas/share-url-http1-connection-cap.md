---
type: Gotcha
title: Share URL lag — plain-HTTP browsers hit the six-connection cap
description: 'Gotcha: a browser on a share URL or `ocode serve` speaks HTTP/1.1 (six connections per origin); per-host SSE streams pinned them. /api/events now fans remote hosts in over one stream, and a caller-cancelled proxied request no longer drops the host tunnel.'
resource: internal/server/handler_events.go; internal/server/handler_events_remote.go; internal/server/handler_remote_proxy.go; internal/remote/proxy.go; web/src/lib/eventBus.ts
tags:
  - gotcha
  - server
  - share
  - sse
  - http1
  - remote
  - event-bus
timestamp: 2026-10-02T00:00:00Z
---
# Share URL lag: the HTTP/1.1 six-connection cap

## Symptom (2026-10-02)

The desktop window was fast; a second browser opened on the desktop share URL
lagged badly. The server was not the bottleneck (CPU ~20%, `curl` answered at
once). `lsof` showed the browser holding **exactly six** connections to the
server — the HTTP/1.1 per-origin cap. Only the desktop webview negotiates
HTTP/2 (`desktop-webview-http2.md`); share URLs and `ocode serve` are plain HTTP.

What sat on the six slots: the local `/api/events` stream, one
`/api/remote/<host>/api/events` stream **per remote host with an open tab**, and
remote git status calls over SSH (bounded at 30s each). Every other request
queued inside the browser.

Opening the second browser also produced a reconnect storm: its first requests
were cancelled (page still settling), each cancel reached `remoteHosts.drop()`,
and the healthy SSH tunnel was torn down under every other in-flight request
(`connection refused` burst, then a fresh 5–10s connect).

## Fix

- **One event stream per browser, whatever the host count.** The SPA names its
  remote hosts as `GET /api/events?hosts=a,b`. `HandleEvents` starts one relay
  per host (`relayRemoteEvents`, `handler_events_remote.go`) that subscribes to
  the host's own `/api/events` and writes its envelopes down the same response,
  tagged `"host": "<host>"`. Admission is the path-less proxy's: the host must
  own a saved project.
- **Reconnect moved server-side.** The relay retries a lost upstream with
  backoff (1s → 30s) and treats 45s of silence as dead (the browser cannot: the
  local stream's pings keep its own liveness timer armed). Each upstream open
  emits a `host_stream` envelope; the SPA resets that host's seq watermark on
  it and reconciles from the second one on — and on the first one too when the
  browser's own stream is a reopen (a `setHosts`/`setProjects` restart or a
  reconnect): the reconcile fired at stream open runs before the server has
  re-subscribed the host, so frames the host emitted in that gap were lost.
- **`seq` is tracked per origin** in `eventBus.ts` (local, and each host) —
  each server process has its own counter, so one watermark would report a gap
  on every interleaved frame.
- **A caller-cancelled request never drops the host connection.** Both drop
  sites check `r.Context().Err() == nil` first: the reverse-proxy
  `ErrorHandler` (`internal/remote/proxy.go`) and the registration failure path
  in `HandleRemoteProxy`.

## Rules

- **Never add a long-lived browser connection per item** (host, project,
  session). Multiplex it onto `/api/events`. Six slots is the whole budget for
  a plain-HTTP browser, and a share URL is a first-class client.
- **Do not reopen `/api/remote/<host>/api/events` from the SPA.** The proxy
  route still serves it; the bus must not use it.
- **A new `remoteHosts.drop()` call site must rule out caller cancellation.**
  `context canceled` describes the browser, not the tunnel.
- Changing the host set restarts the one stream (like `setProjects`), which
  fires the reconcile handlers. That is the designed cost of one connection.

## Not fixed here

Remote git status still holds a request for the length of an SSH exec (up to
30s). With the streams gone it has the slots to do so, and execs are capped per
host, but it is the next thing to move if a plain-HTTP browser lags again.

Tests: `handler_events_remote_test.go`, `TestNewAPIProxy_CallerCancelDoesNotFireOnErr`,
the "remote hosts: fan-in" block in `web/src/lib/eventBus.test.ts`.
