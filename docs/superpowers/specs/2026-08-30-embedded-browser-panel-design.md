# Embedded Browser Panel — Design Spec

Date: 2026-08-30
Status: Approved (brainstorm 2026-08-30)

## Summary

Add an embeddable web browser to the ocode UI (desktop shell and web UI —
same SPA) with two hosting surfaces:

1. **Side panel** — a collapsible, resizable pane on the right of the
   center region, available beside both chat sessions and terminals.
2. **Browser tab** — a first-class tab kind in the UnifiedTabBar
   alongside chat sessions and terminals; fills the center region.

Both surfaces render the same `BrowserPanel` component: address bar,
status indicator, page viewport, and a dev console drawer at the bottom
(Console + Network). Pages load through a Go-side browsing proxy served
by the existing ocode HTTP server, which makes arbitrary external sites
embeddable and enables console/network capture via same-origin script
injection.

## Goals

- Browse arbitrary websites and local dev servers inside the ocode UI.
- Address bar with back/forward/reload and open-in-external-browser.
- Status indicator: loading, HTTP status, error, proxy mode chip.
- Dev console at the bottom: console messages, JS errors, network
  (fetch/XHR) — filterable, clearable.
- Collapsible and resizable side panel; width persisted.
- Per-tab state: each chat/terminal tab remembers its own side-panel
  browser state; each browser tab has its own state.
- Efficient by default: nothing mounted or fetched until opened; at most
  one live iframe at any time.

## Non-goals (v1)

Tracked in TODO.md, not built now:

- Agent/tool access to the embedded browser (reading pages, clicking).
- Screenshots, recordings.
- Cookie/auth session persistence across restarts.
- Multiple browser sub-tabs inside one panel.
- Promoting a side panel to a standalone browser tab.

## Architecture

### Frontend (`web/src/components/Browser/`)

New components, no new layout library — reuse `useResizableSidebar`
(new storage key `ocode.ui.browser_width`) for the side-panel split and
a small drag handle for the console drawer height.

- `BrowserPanel.tsx` — the shared surface. Props select host mode:
  `side` (chat/terminal companion) or `full` (browser tab). Renders,
  top to bottom:
  1. Address bar: URL input, back/forward/reload, open-external.
  2. Status row: spinner / HTTP status / error badge; mode chip
     (`local` vs `proxied`).
  3. Viewport: `<iframe src="/browse/{stateKey}/?url=...">` — same
     origin, so postMessage from the injected script is trusted after
     an origin + stateKey check.
  4. Dev console drawer (collapsible, drag-resizable height) with two
     sub-tabs: **Console** (log/warn/info/error, uncaught errors,
     unhandled rejections; text filter; clear) and **Network**
     (method, URL, status, duration for fetch/XHR).
- `browserStore.ts` — Zustand-style store matching existing project
  stores. State keyed by `stateKey`:
  - side panels: `side:{tabKind}:{tabId}` (chat or terminal tab id)
  - browser tabs: `tab:{browserTabId}`
  - Shape: `{ url, history, historyIndex, panelOpen, collapsed,
    consoleEvents[], networkEvents[] }`. `url`/`panelOpen`/`collapsed`
    persist per project (same pattern as `tabOrderPersistence`);
    console/network buffers are in-memory only, ring-buffer capped
    (e.g. 1000 entries each).

#### Lifecycle / efficiency rules

- **Closed by default.** No iframe, no buffers, no proxy traffic until
  the user opens a panel or creates a browser tab.
- **Only the active tab's iframe is mounted.** Switching tabs swaps the
  iframe (page reloads); URL, history, and console backlog survive in
  the store.
- **Collapse (chevron)** on the side panel: hides the pane (thin edge
  rail with reopen chevron), unmounts the iframe, keeps all state.
  Expanding reloads the same URL; console backlog intact.
- **X (close)** on the side panel: unmounts and **discards** that tab's
  browser state, including persisted URL. Reopening (toolbar button in
  the UnifiedTabBar row) starts fresh with an empty address bar.

### UnifiedTabBar changes (`web/src/components/Layout/UnifiedTabBar.tsx`)

- Tab kind becomes `"chat" | "terminal" | "browser"`.
- Third add control: **+ Browser**. New browser tabs get a globe
  indicator; title defaults to the loaded page title, inline-renameable
  like other tabs.
- Browser tabs join the persisted interleaved ordering
  (`tabOrderPersistence`) and @dnd-kit drag-reorder; closable with X;
  Cmd/Ctrl+W closes a focused browser tab (desktop shell handler in
  `useKeyboard.ts`).
- `focusedKind` gains `"browser"`; App.tsx shows the browser tab's
  full-width `BrowserPanel` in the center region (CSS-visibility
  switching consistent with existing behavior, but inactive browser
  tabs unmount their iframe per the lifecycle rules).

### App.tsx layout

- Side panel: new right-hand split inside `<main>`, rendered beside the
  sessions/terminal region so it accompanies whichever of chat/terminal
  is focused. Resize handle mirrors the existing sidebar handles
  (a11y: `role="separator"`, keyboard, double-click reset).
- Toggle button for the side panel lives in the UnifiedTabBar row;
  keyboard shortcut added alongside existing bindings.

### Backend proxy (`internal/browse/`)

Routes registered in `internal/server` **before** the SPA catch-all
(same constraint the live-preview plan documented for
`internal/server/server.go`). Auth: the same token model as every other
route. Pure in-process HTTP handlers — no process spawning.

- `GET/POST /browse/{stateKey}/?url=...` (plus subresource paths under
  `/browse/{stateKey}/...` resolved against the current upstream):
  - **Local mode** — upstream host is loopback or RFC1918: transparent
    streaming reverse proxy with WebSocket upgrade passthrough (HMR
    works). Only transformation: inject the capture script into HTML
    responses.
  - **External mode** — everything else: fetch upstream; strip
    `X-Frame-Options` and CSP `frame-ancestors`; rewrite absolute
    URLs, redirects, and cookies' scope to stay inside
    `/browse/{stateKey}/`; inject the capture script into HTML.
  - SSRF guard: external mode refuses redirects that land on
    loopback/RFC1918 unless the original request was local mode;
    the proxy only serves requests carrying a valid session token.
- Injected capture script (served from the ocode origin, tiny, no
  dependencies): patches `console.*`, `window.onerror`,
  `unhandledrejection`, `fetch` + `XMLHttpRequest` (method, URL,
  status, duration), and hooks `history` + link clicks for navigation
  tracking. Emits events to `window.parent.postMessage` tagged with
  the `stateKey`; the SPA validates origin + key before appending to
  buffers and updating the address bar / status row.

### Error handling

- Upstream fetch failure → status badge (`connection refused`, DNS,
  timeout) in the status row; viewport shows an inline error card with
  a retry button. Errors logged server-side with URL + reason.
- Site still refuses to render usefully under the proxy (heavy
  service-worker / OAuth flows) → status badge + open-in-external
  button remains one click away.
- Proxy responses that aren't HTML pass through untouched (streaming),
  so downloads/media behave sanely.

## Testing

- **Go** (`internal/browse`): httptest-based — header stripping,
  absolute-URL/redirect rewrite, cookie scoping, script injection,
  local-mode passthrough + WS upgrade, SSRF redirect guard, token auth.
- **Frontend** (vitest, matching existing component tests):
  `BrowserPanel` lazy mount/unmount, collapse-vs-X semantics,
  per-tab state swap on tab switch, console/network rendering +
  filter/clear, UnifiedTabBar third kind (add, reorder, close).
- **Manual QA**: Vite dev server with HMR through local mode; several
  real external sites through proxied mode; desktop shell + web UI.

## Open decisions resolved during brainstorm

- Arbitrary websites are in scope → Go browsing proxy chosen over
  plain iframe, hybrid, or a separate native window (Wails v3 cannot
  embed a second webview in-window).
- Localhost also flows through the proxy (cheap streaming mode) —
  a direct iframe would be cross-origin and kill console capture.
- Per-tab state (not per-project), console **and** network capture
  in v1, closed by default, collapse vs X semantics as above.
