# Embedded Browser Panel — Implementation Plan (INDEX)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Each part file is self-contained; execute in the order below. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add an embeddable web browser (side panel + first-class tab) to the ocode UI that loads arbitrary external sites and local dev servers through an isolated Go browsing proxy, with an address bar, status indicator, and a dev console (Console + Network).

**Architecture:** A **separate loopback HTTP listener** (the "browse origin") reverse-proxies pages so proxied content is cross-origin to the ocode SPA and can never reach its DOM, token, or `/api/*`. Routing is **stateless** — the upstream is encoded in the path `/b/{stateKey}/{scheme}/{host}/{path...}`. External responses get header surgery (full CSP strip, SW block), HTML/CSS URL rewriting, and an injected capture script that reroutes JS-constructed URLs and reports console/network telemetry via cross-origin `postMessage`. The authoritative address-bar URL is emitted server-side over the existing SSE bus. The frontend renders one shared `BrowserPanel` in two host modes (side / full), with at most one live iframe.

**Tech Stack:** Go 1.26.1 (`net/http`, `net/http/httputil`, `golang.org/x/net/html`, `netip`), React + TypeScript (Vite), `@tanstack/react-store` for global stores (`new Store(...)` + `useSelector` — the repo does NOT use zustand; see `web/src/stores/projectStore.tsx`), Wails v3 desktop shell.

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md`

## Global Constraints

- **Go floor:** 1.26.1 (`go.mod:3`). Use `net/netip` for IP checks; use `ServeMux` method-prefixed patterns (`"GET /..."`).
- **No new process spawns.** The proxy is an in-process `http.Server`. (Satisfies the shared-spawn rule trivially — no `exec.Command`.)
- **Fail loud.** No empty catch/`_ =` error drops; every caught error logs *what was attempted* + *reason* via the stdlib `log` package (server) or `console.error` (SPA), except benign expected cases which get an inline `// intentionally not logged: <reason>` comment.
- **Security invariants (non-negotiable, from spec):** proxied content is served only from the browse origin; the API token never reaches the browse origin; the SPA accepts `postMessage` only from the browse origin and treats its payload as untrusted; the address bar renders only server-emitted nav events, never page-reported URLs; the SSRF guard runs at connect time (dialer), not on pre-resolved hostnames.
- **Dependency pinning:** if `golang.org/x/net` is not already required, add it pinned to a specific version (no floating). Check `go.mod` first.
- **Sort/paginate:** console/network lists render newest-relevant order with ring-buffer caps (1000 entries each); no pagination needed given the cap, but note the cap in a comment.
- **TODO.md:** any deferred/stubbed item gets a TODO.md entry and is called out to the user.

## Files created (by part)

| Part | Primary files |
|------|---------------|
| 01 | `internal/browse/server.go`, `internal/browse/auth.go`, `internal/browse/route.go`, wiring in `internal/server/server.go`, `internal/desktop/boot.go` |
| 02 | `internal/browse/dialer.go` |
| 03 | `internal/browse/external.go`, `internal/browse/headers.go`, `internal/browse/cookiejar.go` |
| 04 | `internal/browse/rewrite.go` |
| 05 | `internal/browse/capture.go` + embedded `capture.js` |
| 06 | `internal/browse/local.go` |
| 07 | `internal/browse/navevents.go`, publish wiring |
| 08 | `web/src/lib/browserStore.ts`, `web/src/api/client.ts` additions |
| 09 | `web/src/components/Browser/BrowserPanel.tsx`, `AddressBar.tsx`, `DevConsole.tsx` |
| 10 | `web/src/components/Layout/UnifiedTabBar.tsx`, `web/src/lib/tabOrderPersistence.ts`, `web/src/App.tsx`, `web/src/hooks/useKeyboard.ts` |

## Execution order

Backend proxy first (01→07), because the frontend consumes the browse base URL, grant endpoint, and nav-event shapes those parts produce. Then frontend (08→10). Each part ends green (tests pass) and committed.

1. **01 — Browse server scaffold + auth** (separate listener, stateless route parse, config endpoint, grant→cookie)
2. **02 — Hardened SSRF dialer**
3. **03 — External-mode fetch, header surgery, server-side cookie jar**
4. **04 — HTML/CSS rewrite engine**
5. **05 — Capture-script injection + client-side reroute**
6. **06 — Local mode (streaming + WebSocket passthrough + SW block)**
7. **07 — Server-authoritative nav/status events over SSE**
8. **08 — Frontend browserStore + client API additions**
9. **09 — BrowserPanel component (address bar, status, viewport, dev console)**
10. **10 — UnifiedTabBar browser kind + App.tsx wiring + keyboard**

## Cross-part interface contract (authoritative — parts must match exactly)

Go (package `browse`):
- `func New(token string, logger *log.Logger) *Server` — constructs the browse server; `token` is the **main-origin** API token used only to validate grant-mint calls proxied through the main server (never stored on responses).
- `func (s *Server) Handler() http.Handler` — the browse mux.
- `func (s *Server) Listen(addr string) (net.Listener, string, error)` — binds; returns listener + `baseURL` (e.g. `http://127.0.0.1:54321`).
- `func (s *Server) MintGrant(stateKey string) string` — returns a one-time grant token (main server calls this).
- `func (s *Server) SetNavPublisher(fn func(stateKey string, ev NavEvent))` — the main server injects a closure that publishes onto the SSE bus.
- `type NavEvent struct { StateKey string; URL string; Status int; Mode string; Error string }` (`Mode` ∈ `"local"|"proxied"`).
- `type target struct { StateKey, Scheme, Host, Path, RawQuery string; Local bool }` — parsed from `/b/{stateKey}/{scheme}/{host}/{path...}`.
- `func parseTarget(urlPath, rawQuery string) (target, error)`.
- `func isPrivateIP(ip netip.Addr) bool` — the enumerated-range check.
- `func newSafeDialer(allowPrivate bool) *net.Dialer` (uses `Control` for connect-time IP validation).
- `func rewriteHTML(body []byte, t target, base string) ([]byte, error)` / `func rewriteCSS(body []byte, t target) []byte`.
- `func injectCapture(html []byte, stateKey, spaOrigin string) []byte`.

Frontend (`web/src/lib/browserStore.ts`):
- `type StateKey = \`side:${"chat"|"term"}:${string}\` | \`tab:${string}\``
- `interface BrowserTabState { url: string; history: string[]; historyIndex: number; panelOpen: boolean; collapsed: boolean; consoleEvents: ConsoleEvent[]; networkEvents: NetworkEvent[] }`
- `browserStore = new Store(...)` (`@tanstack/react-store`) plus a `useBrowserStore(key)` selector hook and `useBrowserActions()` exposing: `open(key)`, `close(key)`, `setCollapsed(key,bool)`, `navigate(key,url)`, `back(key)`, `forward(key)`, `pushConsole(key,ev)`, `pushNetwork(key,ev)`, `clearConsole(key)`, `clearNetwork(key)`, `applyNavEvent(key,NavEvent)`. (Parts 09/10 consume these exact names.)
- `web/src/api/client.ts`: `getBrowseBase(): string`, `mintBrowseGrant(stateKey: string): Promise<string>`.
