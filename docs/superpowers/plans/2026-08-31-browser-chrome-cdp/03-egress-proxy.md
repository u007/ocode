# Part 03 — Egress proxy (`internal/browse/cdp/egress.go`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox steps. High-level by policy (no code in plans). TDD per task; commit per task.

**Goal:** A loopback-only HTTP forward proxy (plain requests + `CONNECT` tunnels) whose every outbound connection goes through the existing SSRF-safe dialer, so all Chrome egress is policy-checked at connect time on the **resolved** address.

**Spec:** `docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md` § Egress guard.

**Global constraints:** loopback and all private ranges blocked (no exception); no new deps; `go test ./internal/browse/cdp/`.

## Context an implementer needs

- `internal/browse/dialer.go`: `newSafeDialer(allowPrivate bool) *net.Dialer` sets a `Control` hook that classifies the resolved connect address with `isPrivateIP` (`route.go`) and refuses private/blocked ranges (`browseBlockedPrefixes`: loopback, RFC1918, link-local incl. `169.254.169.254`, CGNAT, multicast, IPv4-mapped IPv6 unmapped first). It is in package `browse`; the `cdp` sub-package receives it as a `*net.Dialer` value (`ManagerOptions.Dialer`) — do not import `browse` from `cdp` (Part 05 makes `browse` import `cdp`; avoid the cycle).
- Chrome proxy semantics: `Target.createBrowserContext {proxyServer:"http://user:pass@127.0.0.1:PORT", proxyBypassList:"<-loopback>"}`. Chrome sends `Proxy-Authorization: Basic …` after a `407 Proxy-Authenticate: Basic realm=…` challenge (it may also send it pre-emptively for user-info URLs; accept both). Chrome uses `CONNECT host:443` for `https:` and `wss:`, and absolute-URI `GET http://host/…` for plain `http:`/`ws:`.
- `Network.loadingFailed` on the page carries `errorText` (e.g. `net::ERR_TUNNEL_CONNECTION_FAILED`, `net::ERR_PROXY_CONNECTION_FAILED`); Part 04 maps a proxy `403` to a `blocked:"private address"` network row, so the proxy must answer **403** (not a TCP reset) for policy blocks.

## Interfaces produced (used by Part 04)

- `NewEgressProxy(dialer *net.Dialer) (*EgressProxy, error)` — listens on `127.0.0.1:0`, generates a random 24-byte credential, starts serving.
- `(*EgressProxy).ProxyServerURL() string` — `http://ocode:<cred>@127.0.0.1:<port>`.
- `(*EgressProxy).Close() error` — stops listener, closes active tunnels.
- Test-only export via `export_test.go`: `Addr()`, `Credential()`.

---

### Task 1: Listener, auth, peer check

**Files:**
- Create: `internal/browse/cdp/egress.go`, `internal/browse/cdp/export_test.go`
- Test: `internal/browse/cdp/egress_test.go`

- [ ] Step 1: Write failing tests: (a) request without `Proxy-Authorization` → `407` with `Proxy-Authenticate: Basic realm="ocode-browse"`; (b) wrong credential → `407`; (c) correct credential + `GET http://<httptest upstream>/x` → upstream body returned (use a permissive dialer in this test: `&net.Dialer{}`); (d) `ProxyServerURL()` parses and its user-info matches `Credential()`.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement as an `http.Server` whose handler checks `Proxy-Authorization` first, then branches on `r.Method == CONNECT`. Peer check: wrap the listener's `Accept` to reject non-loopback remote addresses (belt-and-braces; the bind is loopback anyway).
- [ ] Step 4: Run → pass.
- [ ] Step 5: Commit `feat(browse/cdp): egress proxy listener + auth`.

---

### Task 2: Plain forward requests

**Files:** same.

- [ ] Step 1: Write failing tests: (a) absolute-URI `GET`/`POST` with body forwarded to upstream with hop-by-hop headers stripped (`Proxy-Authorization`, `Proxy-Connection`, `Connection`, `Keep-Alive`, `TE`, `Trailer`, `Transfer-Encoding`, `Upgrade`) and the response status/headers/body relayed; (b) an `Upgrade: websocket` plain-`http` request is tunnelled bidirectionally (hijack both sides after the upstream 101) — assert a ping/pong through a `gorilla/websocket` echo upstream; (c) upstream that the dialer refuses (use a dialer whose `Control` returns an error for everything) → `403` with body `blocked: private address`.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement with an `http.Transport{DialContext: dialer.DialContext, Proxy: nil, DisableCompression: true}` for normal requests and a manual dial + hijack path for `Upgrade` requests. Map dialer `Control` errors to `403`; other dial errors to `502`.
- [ ] Step 4: Run → pass.
- [ ] Step 5: Commit `feat(browse/cdp): egress plain forwarding + upgrade tunnel`.

---

### Task 3: `CONNECT` tunnels + block-list tests against the real safe dialer

**Files:** same.

Policy dialer for these tests: `newSafeDialer` lives in package `browse`, which `cdp` must not import (Part 05 makes `browse` import `cdp`). Build an equivalent `net.Dialer` inline in the test whose `Control` refuses any resolved address inside a copy of `route.go`'s `browseBlockedPrefixes` list. Part 05 exports `browse.NewSafeDialer` and adds a test asserting the two prefix lists are identical, so the copy cannot drift silently.

- [ ] Step 1: Write failing tests: (a) `CONNECT <httptest TLS upstream host:port>` with valid auth → `200 Connection Established`, then a TLS handshake + `GET` through the tunnel succeeds; (b) `CONNECT 127.0.0.1:1` with the block-list dialer → `403`; (c) `CONNECT 10.0.0.1:443` → `403`; (d) `CONNECT [::ffff:192.168.1.1]:443` → `403`; (e) a hostname that resolves to loopback (use `localhost:443`) → `403`; (f) `CONNECT` to an unreachable public-looking address must not hang: dial timeout ≤ 10 s → `502`; (g) closing the proxy closes an open tunnel.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement: dial via `dialer.DialContext(ctx, "tcp", host)`; on success write `200`, hijack, `io.Copy` both ways, close when either side ends; track tunnels in a set for `Close()`.
- [ ] Step 4: Run with `-race` → pass.
- [ ] Step 5: Commit `feat(browse/cdp): CONNECT tunnels + policy tests`.

## Verification for the part

- `go test -race ./internal/browse/cdp/` green.
- `egress.go` ≤ ~250 lines.
