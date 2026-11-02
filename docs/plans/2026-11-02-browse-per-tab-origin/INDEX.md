# Browse per-tab origin (root-mounted, one loopback port per tab)

## Problem
Proxied pages are served at `{prefix}/b/{stateKey}/{scheme}/{host}/{path}`, so
`location.pathname` is the proxy path. Client routers with a root basepath
(TanStack Router SPA mode, React Router, Vue Router) find no route and throw
`Expected to find a match below the root match in SPA mode`. Reproduced on a
remote SSH project (`/api/remote/<host>/browse/b/tab:…/https/localhost:3510/`).

## Decision
Additive **root-mounted mode** for local desktop/web clients: each tab gets its
own loopback TLS listener `127.0.0.1:<port>` bound to one
`(stateKey, scheme, host)`; the document path IS the upstream path. Prefix
mode stays for tailnet/phone clients (single `tailscale serve` hostname).
Rejected: `*.localhost` subdomains (cert SANs and the macOS pin in
`cmd/ocode-desktop/localcert_darwin.m` accept only 127.0.0.1/::1/localhost;
would need cert + pin changes on 3 platforms). Rejected for now: injected
`history.replaceState` shim (breaks reload/deep link/bare-relative URLs).

## Not fixed by this plan
Tailnet / phone clients stay in prefix mode (one `tailscale serve` hostname), so
a TanStack SPA tab there still fails. Only local desktop/web clients get root mode.

## Invariants to keep
- Every request self-describes its upstream (route.go): the port is an immutable
  binding, not mutable "current upstream" state.
- Remote: saved hosts only; token never reaches the browser; restore websocket
  subprotocol on 101; never fall back to the local browse server.
- Cross-upstream subresources keep a same-origin route under a reserved prefix
  `/__ocode/b/…` (never `/b/`, real apps use it). Top-level navigation to a
  different upstream redirects to that upstream's own origin.
- Browse auth cookie name is per-tab (cookies are not port-isolated), derived by
  encoding/hash of the stateKey (stateKeys contain `:`, illegal in cookie names).
  The binding check (`t.StateKey != stateKey`) also defeats cookie tossing.
- Acceptance case is the remote SSH tab (aimsai2 TanStack SPA), not an add-on.

## Parts (self-contained, do in order)
1. `01-repro.md` — failing tests first.
2. `02-listener-and-binding.md` — per-tab listener, stable port, per-tab cookie.
3. `03-rewrite-and-capture.md` — root-mode branches in rewrite.go / capture.js.
4. `04-remote.md` — root-mounted tunnel forwarding for remote projects.
5. `05-spa-and-persistence.md` — SPA origin selection, port + cookie-jar persistence.
6. `06-docs-and-verify.md` — docs, real-app verification.
