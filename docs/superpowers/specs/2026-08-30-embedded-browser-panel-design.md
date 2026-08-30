# Embedded Browser Panel — Design Spec

Date: 2026-08-30 (rev 2, post adversarial review)
Status: Approved scope: full external-mode v1 (option B)

## Summary

Add an embeddable web browser to the ocode UI (desktop shell and web UI —
same SPA) with two hosting surfaces:

1. **Side panel** — a collapsible, resizable pane on the right of the
   center region, available beside both chat sessions and terminals.
2. **Browser tab** — a first-class tab kind in the UnifiedTabBar
   alongside chat sessions and terminals; fills the center region.

Both surfaces render the same `BrowserPanel` component: address bar,
status indicator, page viewport, and a dev console drawer at the bottom
(Console + Network). Pages load through a Go browsing proxy that serves
from a **separate origin**, making arbitrary external sites embeddable
while keeping proxied content fully isolated from the ocode SPA.

External mode is **best-effort by design**: OAuth flows, SRI'd
rewritten assets, service-worker-first apps, and JS with hardcoded
cross-origin API URLs are expected to break; the UI keeps
open-in-external-browser one click away and surfaces breakage in the
status row.

## Goals

- Browse arbitrary websites and local dev servers inside the ocode UI.
- Address bar with back/forward/reload and open-in-external-browser.
- Status indicator: loading, HTTP status, error, mode chip
  (`local` / `proxied`).
- Dev console: console messages, JS errors, network (fetch/XHR) —
  filterable, clearable, ring-buffer capped (1000 entries each).
- Collapsible and resizable side panel; width persisted.
- Per-tab state: each chat/terminal tab remembers its own side-panel
  browser state; each browser tab has its own state.
- Efficient by default: nothing mounted or fetched until opened; at
  most one live iframe at any time.
- Proxied content must never gain access to the ocode SPA, its token,
  or its API (see Security model).

## Non-goals (v1)

Tracked in TODO.md, not built now: agent/tool access to the embedded
browser; screenshots; cookie persistence across server restarts;
multiple browser sub-tabs inside one panel; promoting a side panel to
a standalone browser tab.

## Security model (governs everything below)

- **Separate origin.** The browse proxy listens on its own loopback
  port (`127.0.0.1:0`, or persisted like the main port; binds the same
  interface as the main server — remote/Tailscale access requires this
  port reachable too, documented limitation). The SPA learns the
  browse base URL from a config endpoint. The iframe therefore hosts
  content **cross-origin** to the SPA: no `window.parent` DOM/heap
  access, no same-origin `/api/*` calls, frame-busting
  (`top.location = ...`) throws.
- **Iframe sandbox** as defense-in-depth:
  `sandbox="allow-scripts allow-forms allow-same-origin"` (same-origin
  here means the browse origin only), plus a restrictive `allow`
  (permissions policy: camera/mic/geolocation off).
- **Auth.** The browse origin never sees the API token. On panel open,
  the SPA calls an authenticated main-origin endpoint that mints a
  short-lived one-time grant; the first iframe navigation carries it;
  the browse listener exchanges it for a scoped `HttpOnly` session
  cookie (browse origin, `SameSite=Lax`) and redirects to the clean
  URL. All subsequent browse traffic (documents + subresources) is
  validated by that cookie. `Referer` is stripped before going
  upstream; a fixed User-Agent is sent.
- **postMessage.** Capture-script events post to the SPA with
  `targetOrigin` = SPA origin; the SPA accepts only
  `event.origin === browseOrigin` and treats every field as untrusted
  telemetry (display/console only — never navigation authority).
- **Address bar is server-authoritative.** On every HTML document
  response the proxy emits `{stateKey, upstreamURL, status}` to the
  SPA over the existing main-origin event stream (SSE). The address
  bar and status row render only these server events; page-reported
  URLs are never displayed (prevents URL spoofing by page JS).
- **Service workers actively blocked**: the proxy rejects requests
  with `Sec-Fetch-Dest: serviceworker` / `Service-Worker` header and
  never sends `Service-Worker-Allowed`.

## Backend proxy (`internal/browse/`)

Own `http.Server` on the browse port; handlers are in-process (no
process spawning). Route shape is **stateless** — the upstream is
encoded in the path, so there is no server-side "current upstream",
no races between in-flight subresources and navigation, and restarts
are harmless (in-flight requests just fail and reload):

```
/b/{stateKey}/{scheme}/{host}/{path...}?{query}
```

All rewritten URLs (and redirects) map into this shape. `stateKey`
partitions cookie jars and event routing.

### Local mode (upstream host is loopback or RFC1918)

- Transparent streaming reverse proxy; WebSocket upgrade passthrough
  (HMR works). Only transformation: inject the capture script into
  HTML responses.
- Entered only via user action (typing/choosing the URL in the address
  bar). An external-mode page cannot navigate the panel into local
  mode: document requests whose upstream is private are refused unless
  flagged by a SPA-initiated navigation (grant param on the request).

### External mode (everything else)

- Fetch upstream with a hardened client, buffer-and-rewrite HTML/CSS,
  stream everything else.
- **Header surgery:** strip `X-Frame-Options` and the **entire CSP**
  (partial stripping would block the injected script and rewritten
  assets); strip `Content-Security-Policy-Report-Only`, HSTS,
  `Service-Worker-Allowed`. Send `Accept-Encoding: gzip, identity`
  upstream and re-encode/fix `Content-Length` after rewriting.
- **Rewrite surface (HTML):** `href`, `src`, `srcset`, `action`,
  `poster`, `data`, `<base href>` (consumed and removed),
  `<meta http-equiv="refresh">`, preload/prefetch/modulepreload links,
  inline `style` `url()`. **CSS (files + `<style>`):** `url()`,
  `@import`. **SRI:** `integrity` attributes are stripped whenever the
  referenced resource is proxied (rewritten CSS/JS bytes can no longer
  match hashes).
- **Unrewritable, patched client-side:** the capture script
  monkey-patches `fetch`, `XMLHttpRequest`, `WebSocket`, and dynamic
  `import()` to reroute absolute URLs through `/b/{stateKey}/...`
  (partial coverage — hardcoded cross-origin API calls from workers or
  exotic loaders will still fail; that lands in the Network console as
  a visible error).
- **Cookies:** kept in a **server-side jar keyed by
  `(stateKey, upstream origin)`**, in-memory, never forwarded to the
  browser. `Set-Cookie` from upstream is absorbed into the jar;
  outgoing requests get jar cookies for their upstream origin. No
  cross-site leakage, no collision with the browse session cookie.
- **Methods & bodies:** GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS proxied;
  request bodies (incl. multipart uploads) streamed through untouched.
- **Limits:** rewriteable (HTML/CSS) responses capped at 10 MB (larger
  → streamed unrewritten with a status-row warning); 30 s upstream
  timeout; redirect chain cap 10; per-stateKey concurrent upstream
  connection cap 32.

### SSRF guard (external mode)

Enforced in the dialer, not by pre-checking hostnames (defeats DNS
rebinding: the resolved IP is checked at connect time via
`Control`/`DialContext` and pinned for the request). Blocked ranges,
enumerated for tests: `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`,
`192.168.0.0/16`, `100.64.0.0/10` (CGNAT), `169.254.0.0/16` (incl.
metadata `169.254.169.254`), `0.0.0.0/32`, `::1`, `fc00::/7`,
`fe80::/10`, IPv4-mapped IPv6 forms, and non-canonical IP literal
encodings (decimal/octal) — parsed via `netip` after resolution, so
encoding tricks don't matter. Each hop of a redirect chain re-runs the
guard; external→private redirects are refused.

### Injected capture script

Served from the browse origin, dependency-free. Patches `console.*`,
`window.onerror`, `unhandledrejection`, `fetch`/XHR (method, URL,
status, duration), plus the reroute patches above. Emits telemetry via
`postMessage` (see Security model). Navigation display is **not**
sourced from this script.

## Frontend (`web/src/components/Browser/`)

No new layout library — reuse `useResizableSidebar` (new storage key
`ocode.ui.browser_width`; pass an explicit wider `maxWidth` — the
hook's 500 px default is too narrow) and a small drag handle for
console drawer height.

- `BrowserPanel.tsx` — shared surface, host mode `side` | `full`:
  address bar row (URL input, back/forward/reload, open-external),
  status row (spinner / HTTP status / error badge / mode chip),
  viewport (sandboxed cross-origin iframe), dev console drawer with
  **Console** and **Network** sub-tabs (text filter, clear).
- `browserStore.ts` — store keyed by `stateKey`
  (`side:{chat|term}:{tabId}` or `tab:{browserTabId}`):
  `{ url, history, historyIndex, panelOpen, collapsed,
  consoleEvents[], networkEvents[] }`. `url`/`panelOpen`/`collapsed`
  persist per project (pattern of `tabOrderPersistence`); buffers are
  in-memory, ring-capped.

### Lifecycle / efficiency

- **Closed by default** — no iframe, buffers, grant, or proxy traffic
  until opened.
- **One live iframe max.** Only the active tab's iframe is mounted;
  panel **chrome stays mounted-hidden** (matching the codebase's
  CSS-visibility switching) — only the iframe unmounts. Switching back
  reloads the page from the store URL (documented cost: in-page form
  state is lost on tab switch).
- **History:** the store's history is the only history. Programmatic
  iframe loads use replace semantics (`location.replace`-style src
  swaps) so browse navigations never pollute the host browser's /
  shell's joint session history; back/forward buttons drive
  `historyIndex` and reload.
- **Collapse (chevron)** — side panel hides to a thin rail; iframe
  unmounts; all state kept. Expand reloads the URL, console backlog
  intact.
- **X (close)** — discards that stateKey's state incl. persisted URL
  and revokes its browse session server-side. Reopen (UnifiedTabBar
  row button / shortcut) starts fresh.

### UnifiedTabBar & App.tsx integration

- Tab kind becomes `"chat" | "terminal" | "browser"`; third add
  control **+ Browser**; globe indicator; title defaults to page title
  (from server nav events), inline-renameable; X-closable;
  Cmd/Ctrl+W in `useKeyboard.ts`.
- Type ripple acknowledged: `tabOrderPersistence.ts` `UnifiedTabKey`
  union + `reconcileTabOrder` signature gain `browser:` ids;
  `focusedKind` unions in `App.tsx` and `UnifiedTabBar.tsx` gain
  `"browser"`; @dnd-kit ordering unchanged otherwise.
- Side panel: right-hand split inside `<main>` beside the
  sessions/terminal region; resize handle mirrors existing ones
  (`role="separator"`, keyboard, double-click reset); toggle button in
  the UnifiedTabBar row + shortcut.
- Browse base URL + grant minting via `web/src/api/client.ts`
  additions; nav/status events arrive on the existing SSE channel.

## Error handling

- Upstream failures → server nav event with error class (DNS, refused,
  timeout, SSRF-blocked); status badge + inline error card with retry.
  Logged server-side with URL + reason.
- Sites broken under rewriting → status badge ("degraded: N blocked
  requests" from Network telemetry) + open-in-external.
- Oversized rewriteable responses → streamed unrewritten + warning.
- Main-server restart: browse listener restarts with it; open panels
  show the error card; reload re-mints the grant transparently.

## Testing

- **Go** (`internal/browse`): httptest — stateless path mapping;
  header surgery (full CSP strip, SW blocking); HTML/CSS rewrite
  matrix (srcset, base, meta refresh, CSS url/@import, SRI strip);
  cookie jar isolation per (stateKey, origin); grant→cookie exchange +
  revocation; SSRF dialer against every enumerated range incl. DNS
  rebinding (fake resolver) and redirect chains; local-mode WS
  passthrough; size/timeout/redirect caps; external→local navigation
  refusal.
- **Frontend** (vitest): BrowserPanel lazy mount/unmount,
  collapse-vs-X, state swap on tab switch, console/network
  rendering + filter/clear, address bar renders only server events,
  UnifiedTabBar third kind (add, reorder, close, persistence key
  migration).
- **Manual QA**: Vite dev server with HMR (local mode); external
  sites: a static docs site, a React SPA, a site with strict CSP, an
  OAuth login (expected-degraded); desktop shell + web UI; verify
  proxied page cannot reach `window.parent` or main-origin API.

## Review-driven decisions (rev 2)

- Separate browse origin + sandbox (was: same-origin — RCE via
  `parent` access to `/api/shell`; fixed).
- Stateless `/b/{key}/{scheme}/{host}/` routing (was: stateful
  "current upstream" — race-prone; fixed).
- Grant→HttpOnly-cookie auth for browse origin (was: unresolved token
  story; fixed). Referer stripped.
- Hardened connect-time SSRF dialer with enumerated ranges.
- Full CSP strip + active service-worker blocking, honest
  "known to break" list.
- Server-side cookie jar; server-authoritative address bar via SSE.
- Replace-semantics history; chrome mounted-hidden, iframe-only
  unmount.
- Go 1.22 ServeMux is specificity-routed — no registration-order
  constraint exists (removed claim); moot anyway on a separate
  listener.
