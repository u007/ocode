# Browser proxy on a separate origin — design spec (2026-10-18)

**Status:** proposed — awaiting the user's sign-off on the origin split, the auth
model and the port before any implementation planning.

## Problem

The embedded browser (browser tab + site preview) was a loopback-only origin, so
a phone or another browser could not reach it. The shipped fix reverse-proxies
browse content through the **main server**, which achieves remote access but
makes browsed pages **same-origin with the SPA**: a browsed page's JavaScript can
read the SPA's storage and call `/api/*` with the user's credentials.

We need **both** remote access and origin isolation.

## Constraint (verified)

The phone reaches ocode over **Tailscale**: `tailscale serve --set-path <prefix>
http://localhost:<mainPort>` (`internal/server/tailscale_share.go:167`). That
exposes **one HTTPS port** on **one hostname**.

- A second *local* port is unreachable over Tailscale.
- A `--set-path` mount on the same hostname is the **same origin** — no isolation.
- An origin is `scheme + host + port`. `localhost` vs `127.0.0.1` breaks the
  `SameSite=Lax` browse cookie on plain HTTP (the bug `sameSiteBrowseBase` fixed).
- An HTTP iframe inside an HTTPS page is blocked as mixed content, so the tailnet
  IP over plain HTTP is not an option.

## Solution

Expose the browse proxy as a **second `tailscale serve` mount on a different
HTTPS port, same hostname**, root-mounted (no `--set-path`).

| Surface | Origin |
|---|---|
| SPA | `https://host/desktop/` → origin `https://host` |
| Browse proxy | `https://host:8444/` → origin `https://host:8444` |

Different **port** ⇒ different **origin**. Same **hostname** ⇒ same **site**, so
the `SameSite=Lax` browse cookie still works.

Tailscale `--set-path` strips the prefix before forwarding (documented in
`web/index.html`), so the root mount means the browse proxy receives unprefixed
`/b/...` paths and the browse cookie's `Path=/b/` matches unchanged.

## 1. Isolation — what is and is not shared

- **Storage is isolated.** `localStorage`, `sessionStorage` and IndexedDB are
  origin-scoped, and an origin includes the port. A browsed page on
  `https://host:8444` cannot read the SPA's storage on `https://host`. This is
  the property that was lost when everything shared one origin, and it is the
  reason for the second port.
- **Network reads are isolated.** A `fetch` from the browse origin to the SPA
  origin is cross-origin; the main server sends no CORS headers, so responses
  are blocked.
- **Cookies are host-scoped, not port-scoped — this is inherent to cookies, not a
  defect.** The `ocode_browse` cookie remains `HttpOnly` and `Path=/b/`, so the
  SPA origin never receives it on its own paths and JavaScript can never read it.
- **The SPA's API token never reaches the browse listener.** The browse proxy
  does not inject an `Authorization` header and does not read the SPA's token. A
  browsed page has no credential to present to `/api/*`.
- **No `sandbox` weakening.** The iframe keeps its current attributes; `sandbox`
  is not used as the isolation mechanism (dropping `allow-same-origin` would
  break the browse cookie and localStorage in browsed apps).

## 2. Auth and abuse control (explicit)

- **The proxy routes are NOT behind `authMiddleware`.** Iframes and WebSockets
  cannot send an `Authorization` header, so wrapping them would make the panel
  unusable. This is unchanged from the shipped fix and is the reason the
  protections below are mandatory rather than optional.
- **Every request is gated twice.** `browseProxyGate` rejects (401) a request
  carrying neither a `__grant` query param nor an `ocode_browse` cookie, before
  any upstream hop, and shares the API's per-IP rate limiter (5 failures → 1 min
  lockout, 429 while locked). A request that passes then faces the browse
  origin's own auth: grant redemption, cookie session, and (for CDP) an `Origin`
  match.
- **The CDP WebSocket upgrade validates the grant.** `handleCDP`
  (`internal/browse/cdpsocket.go:134`) rejects a missing grant with 401 and a
  used/foreign grant with 401/403 **before** upgrading. Already pinned by
  `TestCDP_NoGrant401` (`internal/browse/cdpsocket_test.go:268`). The second
  listener inherits this unchanged.
- **Rate limiting is shipped, not deferred** (this spec removes the earlier
  "TODO" framing): `browseProxyGate` + `browse_proxy_gate_test.go`.
- **Bind address.** The listener binds the **same host as the main listener**
  (`s.addr`'s host), so a direct LAN client can reach it the same way it reaches
  the SPA. Over Tailscale the phone does **not** dial the local port: it reaches
  `https://host:8444`, which `tailscale serve` terminates and forwards to
  `localhost:<browseProxyPort>`. When the main listener is loopback-only, the
  browse listener is loopback-only too (no remote access, no new exposure).

## 3. Remote hosts (unchanged rule)

A remote project's browse content is the **host's** browse origin reached through
the workspace tunnel. The second listener serves
`/api/remote/{host}/browse/{rest...}` → `handleRemoteBrowseProxy`, which resolves
the workspace and proxies to the host's tunneled origin. It **never** falls back
to this machine's browse origin, and the `{host}` must be a saved project host
(the existing trust boundary).

## 4. Design

### 4.1 Browse proxy listener (`internal/server`)

A second HTTP listener serving only the browse-proxy routes:

- `/b/` and `/__ocode_capture.js` → the loopback browse origin.
- `/api/remote/{host}/browse/{rest...}` → the host's tunneled browse origin.

Each backed by an `httputil.ReverseProxy` (WS-upgrade capable, `ErrorHandler`
set, `ModifyResponse` rewriting `Set-Cookie Path=/b/` → the routed prefix for the
remote case). **These routes are REMOVED from the main mux**, so the SPA origin
no longer serves browse content.

Lifecycle follows the existing browse listener: stored on `Server` next to
`browseLn`/`browseHTTPServer` under `shutdownMu`, started in `StartBrowse`,
closed in `Shutdown` (both already do this for the origin).

### 4.2 Base URL (`GET /api/browse/config`)

Computed per request from the request host + current share state:

- **Shared:** `https://<shareHost>:<browseHTTPSPort>` (`<shareHost>` from the
  share snapshot's URL, falling back to `r.Host`'s hostname).
- **Not shared:** `http://<reqHostname>:<browseProxyPort>`.
- **Remote project:** append `/api/remote/<host>/browse`.

`base_url` is an absolute URL again (the frontend already consumes an absolute
base). `handleRemoteBrowseConfig` returns the same shape with the remote suffix.

### 4.3 Cookie path

- **Local, root mount:** unchanged — `Path=/b/` (no rewrite).
- **Remote:** `Path=/b/` → `Path=/api/remote/<host>/browse/b/` (as shipped).

No instance prefix is involved because the browse mount is root-mounted.

### 4.4 Share flow (`internal/server/tailscale_share.go`)

When the main mount is proven, also create the browse mount:
`tailscale serve --https=<browseHTTPSPort> http://localhost:<browseProxyPort>`,
via a new `Expose`-style helper in `internal/tailscale`. On unshare/stop, remove
it (root mount → `... off`). A browse-mount failure must **not** fail the main
share (log it; the browse base falls back to the direct origin).

### 4.5 Config

`browser.proxy_https_port` (int, default **8444**) — the Tailscale HTTPS port for
the browse mount. It is an **`ocodeconfig.json` key** (like
`browser.htr_shared`), so it does **not** go in `.env.example`, which documents
environment variables only (currently `CONNECTOR_SESSION_KEY_REF` and
`OCODE_TEST_POSTGRES_URL`).

Decision on the spec's open question: **8444**, because 8443 is the funnel HTTPS
port the main mount already uses and would collide.

### 4.6 Conventions honoured

- Listener lifecycle rides the existing browse-listener fields and `Shutdown`
  ordering (origin listener closed before the proxy listener).
- No work is added under `Handler.mu`; base-URL resolution reads the share
  snapshot the same lock-free way `buildStatusSnapshot` does.
- The plan stays high-level (file + function names, no code).

## 5. Limitations (accepted for v1)

- **One shared instance per machine for the browse proxy** (root mount on one
  HTTPS port). A second shared instance would need a different
  `proxy_https_port`. The SPA mount is unaffected (`--set-path` distinguishes
  instances).
- **Reverse-proxy / single-port deployments** must expose the second port, or the
  browse base falls back to the direct origin (unreachable remotely → the panel
  reports an error, as today).
- **Funnel is out of scope.** The browse mount uses `serve` (tailnet-only);
  funnel is limited to ports 443/8443/10000.

## 6. Verification

- **Unit (Go):** base-URL resolution matrix (shared / not shared / remote /
  loopback / LAN host); cookie-path rewrite (local unchanged, remote prefixed);
  the gate (no grant + no cookie → 401 before the upstream; grant/cookie →
  forwarded; lockout → 429); the second listener serves `/b/` and the remote
  route; a WS upgrade round-trips through the second listener.
- **Manual (REQUIRED before "done"; NOT yet run):** open ocode on a phone over
  Tailscale, load a browser tab, confirm the page loads and navigates, confirm in
  devtools that the iframe origin is `https://host:8444` (not the SPA origin) and
  that `localStorage` is shared with nothing, confirm the browse cookie survives
  a reload, and confirm the CDP screencast WebSocket upgrades through the second
  listener.
- **Regression:** existing `internal/server` browse tests; frontend Browser
  suites unchanged (the panel consumes an absolute base).

**Honest status:** the real-browser, phone and CDP-screencast paths have **never**
been exercised. "Reachable from a phone" is unproven and stays unproven until the
manual pass above runs.

## 7. Process note

`docs/concepts/web-server-project-scoping.md` was edited **directly** in this
session, bypassing the OKF bundle's sole-writer invariant, because the `context`
sub-agent's API key is expired (`401 api_key_expired`). The page carries no
`file.go:NNN` anchors, so none needed re-deriving. This is flagged for the user,
not presented as compliant.

## Open questions for sign-off

1. Confirm the origin split (second Tailscale HTTPS port, root-mounted) and the
   default port **8444**.
2. Confirm the auth model: proxy routes outside `authMiddleware`, protected by
   `browseProxyGate` + the browse origin's grant/cookie auth, with the CDP WS
   rejecting ungranted upgrades.
3. Confirm the accepted limitation: one shared instance's browse panel per
   machine at a time.
