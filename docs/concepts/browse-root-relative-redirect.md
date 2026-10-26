---
type: Concept
title: Embedded Browser — Root-Relative Redirect for Dev-Server Module Graphs
description: 'Why Vite/TanStack dev servers rendered blank (or a notice) in the embedded browser, and the Referer-keyed 307 at the browse-origin root and the desktop browse-proxy root that sends escaped module imports back onto the /b/ route. Covers both local and remote (SSH) projects, why it is a redirect and not a proxy, and the HMR WebSocket gap.'
resource: internal/browse/rootrelative.go
tags:
  - browser
  - browse
  - remote
  - vite
timestamp: 2026-10-26T00:00:00Z
---

# Root-relative redirect for dev-server module graphs

## Problem

The embedded browser's local mode is a reverse proxy: a page is served under
`/b/{stateKey}/{scheme}/{host}/{path}` on the browse origin (`internal/browse/route.go`).
`rewriteHTML` maps root-relative **attribute** URLs onto that route, but JS bodies
stream untouched. Vite/TanStack dev servers emit root-absolute **static imports**
inside modules (`/node_modules/.vite/deps/react.js`, `/@fs/…`, `/@vite/env`), which
the browser resolves against the iframe's origin root, outside the mount:

- local project: the browse origin had no handler at `/` → 404 → module graph dies, blank page;
- remote (SSH) project: the iframe origin is the desktop **browse-proxy listener**
  (`/api/remote/{host}/browse/b/…`), whose root also had no handler.

Until 2026-10-26 a top-level document that looked like a dev server was replaced by a
"Development server not supported" notice offering Chrome mode, which does not exist in
remote-workspace mode at all. Port forwarding was never required: the remote host's
proxy reaches its own `localhost:3000` fine. The blocker was purely the mount.

## Mechanism

Every escaped import carries a `Referer` set by the browser: the importing module or
document, which lives on the `/b/` route. `browse.RootRelativeRedirect` rebuilds the
canonical route from it (mount prefix + stateKey + scheme + host + requested path and
query) and both roots answer **307** to it:

- browse origin: `s.mux.HandleFunc("/", s.handleRootRelative)` (`internal/browse/server.go`);
- desktop browse-proxy listener: `proxyMux.HandleFunc("/", srv.handleBrowseRootRelative)`
  (`internal/server/server.go`), which keeps a remote mount from the Referer path.

No Referer, a Referer off the route, or a malformed route → 404. The browse origin's 404
explains the cause (see Referrer policy). The desktop listener sends the same body.

## Invariants

- **Redirect, never proxy in place.** A module served at a root URL has relative imports
  that resolve at root and a root Referer, so proxying breaks one level deeper. After the
  307 the module's final URL is the `/b/` one and the chain stays on the route.
- **307, not 302**, so a root-relative `POST` from a dev SPA keeps its method and body.
- **No cookie, no upstream call.** A remote mount's browse cookie is path-scoped to the
  mount, so these requests carry none. The redirect is same-origin and derived only from
  the request's own Referer; the `/b/` handler authenticates the redirected request.
  That is also why the desktop root handler is not wrapped in `browseProxyGate`
  (a cookie-less 401 there would feed the rate limiter).
- **The mount split is unambiguous** (`^((?:/api/remote/[^/]+/browse)?)(/b/.*)$`): the
  host segment cannot contain `/`, so an upstream path containing `/b/` is harmless.

## Referrer policy

The redirect works only when the browser sends the full `/b/` path in `Referer`. The
proxy keeps that true for the documents it serves:

- `local.go` deletes the upstream `Referrer-Policy` response header with `X-Frame-Options`.
- `rewriteHTML` (`rewrite.go`) removes every `<meta name="referrer">` tag. The name
  match is case-insensitive and ignores attribute order and quoting. Other meta tags,
  including `http-equiv="refresh"`, are untouched.

Neither step is a fallback: the proxy owns the documents it serves, so it removes the
policy at the source.

Not covered: an element-level `referrerpolicy` attribute on a `<script>`, `<link>` or
`<a>` is left as is. A module entry script that sets `no-referrer` still strips the
Referer of its import graph, and those requests reach the 404 below.

When a root-relative request arrives without a usable `/b/` Referer, the browse origin
answers 404 with a plain-text body that names the path. It also logs one line. Both roots
call `browse.WriteRootRelativeNotFound`, so the body text lives in one place: the browse
origin's `handleRootRelative` and the desktop listener's `handleBrowseRootRelative`
(`internal/server/server.go`). The body points at Chrome mode, which remote-workspace
projects do not have, so there the hint is not actionable.

## Known gap

A WebSocket handshake carries no Referer, so Vite's HMR socket at the origin root is not
resolved. The page renders; hot reload does not connect (tracked in `TODO.md`).
