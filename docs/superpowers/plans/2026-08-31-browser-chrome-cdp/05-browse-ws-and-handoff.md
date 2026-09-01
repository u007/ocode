# Part 05 — Browse-origin WebSocket, local→chrome hand-off, external-proxy removal

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox steps. High-level by policy (no code in plans). TDD per task; commit per task.

**Goal:** Wire the CDP manager into the browse server: a per-stateKey WebSocket that carries frames/telemetry/input, server-side hand-off from local mode to Chrome mode, and deletion of the HTML-rewriting external proxy.

**Spec:** `docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md` § Mode routing, § Transport, § Removal, § Error handling.

**Global constraints:** nav events only on `browse_nav` SSE; `"proxied"` removed; `*.localhost` is private; grant redeemed on the WS URL (no cookie); Origin must equal `spaOriginFor(stateKey)`.

## Context an implementer needs

- `internal/browse/server.go`: `Server{auth, log, mux, transport, jar, spaOrigin, conns, publish}`; `New(apiToken, logger)` registers `/b/` → `handleBrowse` and `GET /__ocode_capture.js`; `handleBrowse` authenticates (cookie or `?__grant=`), `parseTarget`s the path, applies the local-mode session gate, then calls `handleLocal` or `handleExternal`. `MintGrant(stateKey, spaOrigin)`, `Revoke(stateKey)`, `SetNavPublisher`, `emitNav`, `spaOriginFor(stateKey)` exist. Another agent recently added `sessionLocalDoc`/`setLocalDoc`/`consumeLocalDocumentGrant` to `auth.go` — keep that gate intact.
- `internal/browse/route.go`: `parseTarget`, `hostIsLiteralPrivate`, `browseBlockedPrefixes`, `isPrivateIP`. `external.go` defines `upstreamOrigin(t)` and `classifyFetchError`, used by `connlimit.go` and `server.go`.
- `internal/browse/dialer.go`: `newSafeDialer(allowPrivate)`, `newSafeTransport(allowPrivate)`.
- `internal/browse/local.go`: `handleLocal` builds an `httputil.ReverseProxy` with `ModifyResponse` that emits the terminal nav event and calls `rewriteAndInjectResponse` for HTML.
- `internal/server/server.go`: `StartBrowse(srv, token, spaOrigin)` creates `browse.New`, sets SPA origin, listens, wires `SetNavPublisher` → `bus.Publish("browse_nav", …)`; `NavEvent{StateKey, URL, Status, Mode, Error}` with `Mode` documented as `"local" | "proxied"`. `Server.ProcessSupervisor()` from Part 01. Config from Part 01: `OcodeConfig.Browser.ChromePath`, `IdleTimeoutMinutes` — find how `StartBrowse` callers (`internal/desktop/boot.go`, `server.go` serve path) can reach the loaded config and pass the two values through (extend `StartBrowse`'s signature with a `browse.Options{ChromePath, IdleTimeout}` rather than adding globals).
- `internal/server/handler_terminal.go`: `gorilla/websocket` `Upgrader` usage pattern (read/write pump, ping); its origin check is same-host and must **not** be copied here.
- `web/src/api/client.ts` `browseSrc`/`mintBrowseGrant` — unchanged in this part.

## Interfaces consumed (Part 04)

`cdp.NewManager(ManagerOptions)`, `Attach(ctx, stateKey, sink) (*Target, error)`, `Revoke`, `Close`, `FrameSink`, `Target.Navigate/Back/Forward/Reload/Resize/Mouse/Key/Detach`, `cdp.NavEvent`, `ErrChromeNotFound`, `ErrUnsupportedPlatform`, `ErrBadScheme`.

## Interfaces produced (Part 06 consumes the wire format)

- `GET /b/{stateKey}/__cdp?__grant=<token>` on the browse origin.
- Server→client: binary frame = `[u32 BE width][u32 BE height]` + JPEG; text frames JSON `{"t":"console",level,args,ts}`, `{"t":"network",method,url,status,durationMs,ts,blocked?}`, `{"t":"error",message}` then close code 1011.
- Client→server text JSON: `{"t":"nav",url}`, `{"t":"back"}`, `{"t":"forward"}`, `{"t":"reload"}`, `{"t":"resize",w,h,dpr}`, `{"t":"mouse",kind,x,y,button,clickCount,deltaX,deltaY,modifiers}`, `{"t":"key",kind,key,code,text,modifiers}`.
- `browse.NavEvent.Mode` ∈ `"local" | "chrome"`.
- `browse.NewSafeDialer(allowPrivate bool) *net.Dialer` (exported wrapper for Part 03's tests and the manager wiring).

---

### Task 1: Routing predicate + mode enum

**Files:**
- Modify: `internal/browse/route.go` (`hostIsLiteralPrivate` accepts `*.localhost`), `internal/browse/server.go` (`NavEvent.Mode` comment), `internal/browse/connlimit.go` (mode string)
- Test: `internal/browse/route_test.go`

- [ ] Step 1: Write failing tests: `hostIsLiteralPrivate("app.localhost")`, `("a.b.localhost:3000")` → true; `("localhost.evil.com")` → false; and a test asserting no `"proxied"` literal remains in the package (grep-style test over the package source files is acceptable, or simply rely on the compile after Task 4).
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement.
- [ ] Step 4: Run → pass.
- [ ] Step 5: Commit `feat(browse): *.localhost is local; mode enum local|chrome`.

---

### Task 2: Manager wiring + `__cdp` WebSocket endpoint

**Files:**
- Create: `internal/browse/cdpsocket.go`
- Modify: `internal/browse/server.go` (`Server.cdp *cdp.Manager`, `Options`, mux registration `GET /b/{stateKey}/__cdp`, `Revoke` also calls `cdp.Revoke`, `Close` calls `cdp.Close`), `internal/browse/dialer.go` (`NewSafeDialer` export)
- Test: `internal/browse/cdpsocket_test.go` (uses a fake manager behind a small interface `targetManager` so no Chrome is needed)

- [ ] Step 1: Write failing tests: (a) no `__grant` → `401`; used grant → `401`; valid grant but wrong `Origin` → `403`; valid grant + `Origin == spaOriginFor(key)` → `101`; (b) after upgrade the manager's `Attach` was called once with the key and a sink; sink `Frame(2,3,jpeg)` produces one binary WS message whose first 8 bytes decode big-endian to `2,3` followed by the JPEG bytes; `Console`/`Network` produce the JSON shapes above; `Error("x")` produces the JSON error then close code 1011; (c) client `nav` → `Target.Navigate` with the URL; `resize` → `Resize`; `mouse`/`key` map field-for-field; malformed JSON → logged and ignored, socket stays open; (d) client close → `Target.Detach()` (not `Revoke`); (e) second socket for the same key → first socket receives error `"replaced"` and closes; (f) `Attach` returning `ErrChromeNotFound` → JSON error with the spec text then close; (g) `Attach` returning `ErrUnsupportedPlatform` → error `"Chrome mode is not supported on Windows yet"`.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement: `Upgrader{CheckOrigin: origin == s.spaOriginFor(key)}` after grant redemption (`authStore.redeem(g, false)`), one writer goroutine fed by a channel (frames + JSON) so `gorilla` write concurrency rules hold, ping every 30 s, reader loop dispatching client messages. Wire `cdp.NewManager` in `New` via `Options{ChromePath, IdleTimeout}` with `Dialer: NewSafeDialer(false)`, `Supervisor` passed in `Options`, `EmitNav` adapter → `s.emitNav(NavEvent{Mode:"chrome", …})`.
- [ ] Step 4: Run `-race` → pass.
- [ ] Step 5: Commit `feat(browse): per-stateKey CDP websocket`.

---

### Task 3: Local → chrome hand-off; non-local document path

**Files:**
- Modify: `internal/browse/local.go` (`ModifyResponse` 3xx handling), `internal/browse/server.go` (`handleBrowse` non-local branch)
- Test: `internal/browse/local_test.go`, `internal/browse/server_test.go`

- [ ] Step 1: Write failing tests: (a) local upstream answers a document request with `302 Location: https://accounts.example.com/x` → client gets `204`, no `Location` header, and a nav event `{Mode:"chrome", URL:"https://accounts.example.com/x", Status:0}`; (b) `302 Location: /relative` (resolves private) → passed through unchanged (existing behaviour); (c) a **subresource** 3xx to external → passed through (only documents hand off); (d) `handleBrowse` for `/b/{key}/https/example.com/` with a valid session → `204` + nav event `{Mode:"chrome", URL:"https://example.com/", Status:0}`; no upstream fetch happens (use a dialer that fails the test if invoked); (e) `TestAuthenticatedNavigationProxiesExternal` rewritten to assert (d).
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement; resolve `Location` against the upstream URL, classify with `hostIsLiteralPrivate` on the resolved host (a hostname is treated as non-private, matching the router).
- [ ] Step 4: Run → pass.
- [ ] Step 5: Commit `feat(browse): hand local→external navigations to chrome mode`.

---

### Task 4: Remove the external proxy

**Files:**
- Delete: `internal/browse/external.go`, `external_test.go`, `cookiejar.go`, `cookiejar_test.go`, `headers.go`, `headers_test.go`
- Modify: `internal/browse/route.go` (receive `upstreamOrigin`, `classifyFetchError`), `server.go` (drop `transport`, `jar`, `newSafeTransport(false)` call), `dialer.go` (delete `newSafeTransport` if no remaining caller), `capture.js` header comment (update "Part 04/06" references to describe local-only use; **keep all reroute code**), `rewrite.go` doc comment (local-mode only)
- Test: existing suites must still pass; add a test that `rewrite.go`'s `rewriteHTML` is still exercised by `handleLocal` (already `TestHandleLocalRewritesHTMLURLs` — keep it).

- [ ] Step 1: Move the two helpers, delete the files, fix compile errors.
- [ ] Step 2: Run `go build ./... && go test -race ./internal/browse/...` → green; run `go vet ./...`.
- [ ] Step 3: Grep the repo for `handleExternal`, `cookieJar`, `proxied`, `newSafeTransport` — none remain outside git history.
- [ ] Step 4: Commit `refactor(browse): remove HTML-rewriting external proxy`.

---

### Task 5: `StartBrowse` wiring (server + desktop) and shutdown order

**Files:**
- Modify: `internal/server/server.go` (`StartBrowse(srv, token, spaOrigin, opts browse.Options)`; pass `srv.ProcessSupervisor()`; `Shutdown` order: supervisor → browse `Close` → listeners), `internal/desktop/boot.go` and the `serve` path (pass `cfg.Browser.ChromePath`, `time.Duration(cfg.Browser.IdleTimeoutMinutes) * time.Minute`)
- Test: `internal/server/browse_nav_test.go` (existing wire test still passes; add an assertion that a `chrome` nav event reaches the SSE bus with `mode:"chrome"`)

- [ ] Step 1: Write the failing SSE assertion.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement wiring.
- [ ] Step 4: Run `go test ./internal/server/ -run 'Browse|Nav|Grant'` → pass; `go build ./cmd/...`.
- [ ] Step 5: Commit `feat(server): wire chrome-mode options + supervisor into browse`.

## Verification for the part

- `go test -race ./internal/browse/... ./internal/server/` green (pre-existing unrelated failure aside).
- Manual: `ocode serve`, open panel, type `example.com` → address bar shows `CHROME` chip and (until Part 06) the viewport area is empty; `localhost:<vite>` still renders in the iframe.
