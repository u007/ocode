# Chrome Tab Context Menu Design

Status: approved — 2026-09-10
Scope: embedded Chrome tab (`--headless=new`) right-click.

## Problem
No native context menu in headless; existing `ContextMenu.tsx` (local React portal) opens on bubbling `contextmenu`, but `ChromeViewport` suppresses that event at line 625, so no open occurs. Right-click currently sends CDP mouse only.

## Revised Design

### Event path (resolve contradiction)
Keep `ChromeViewport` suppression of native `contextmenu`. Architecture: `ChromeViewport` owns menu state directly (not a separate component). It renders `ContextMenu` (`web/src/components/Layout/ContextMenu.tsx`) with `{items}` prop, manages `open`/`position`/`disabled` locally, suppresses bubbling `contextmenu` (`stopPropagation`), and does NOT emit a second CDP mouse event (`onPointerDown`/`onPointerUp` at `ChromeViewport.tsx:451-502` stay the only source). Menu updates item states asynchronously via `getNodeAt`.

### Menu items & semantics
- Back / Forward — use `browserStore` `historyIndex`/`history`: disabled when `index <= 0` / `index >= history.length-1` or disconnected. Send existing socket `back` / `forward` commands, not `history.go()`.
- Reload — send existing socket `reload` command (not `location.reload()`).
- Copy URL — `browserStore.getState(s => s.url)` (remote page URL, not SPA host) → clipboard via `navigator.clipboard.writeText`; disabled when disconnected.
- Copy Link or Image — NEW: (a) `DOM.getNodeForLocation(x,y)` → `nodeId`; (b) `DOM.describeNode(nodeId)` to get `nodeName`, `attributes` (map), `frameId`, `contentDocument` node; (c) extract `href` (nodeName `A` with `href` attr) or `src` (`IMG`/`VIDEO`/`SOURCE` with `src`); (d) deepest visible node wins (if overlapping, use describeNode result with `nodeId` from deepest); (e) if no node (`nodeId == 0`) → disable menu item; (f) if `nodeName` is `A`/`IMG`/etc but no attr → fall back to `browserStore.getState(s => s.url)`. Disabled when disconnected.
- Inspect — `DOM.getNodeForLocation(x,y)` → `nodeId` → `DOM.describeNode(nodeId, {depth: 0, pierce: false})` → log `{nodeName, nodeValue, attributes, frameId}` to `DevConsole`. Skip shadow-DOM descendants unless `pierce: true`. Disabled when disconnected.
- Open in External Browser — `window.open(browserStore.getState(s => s.url), '_blank', 'noopener')`; NOT native OS launch (SPA has no native access). Disabled if disconnected.

`SendDOMGetNodeForLocation` reuses existing `useCdpSocket` string-request-ID correlation (not new integer-only mechanism). Typed request/response with 5s timeout; pending-request rejected with `timeout` error on socket close; disconnect cleanup clears pending `getNodeAt`. `NodeLocation`: `{nodeId, backendNodeId?, frameId?, href?, src?}`. No-node = `nodeId: 0` + `error: "no_node"`. Error codes: `target_unavailable`, `timeout`, `disconnected`, `protocol_error`. Wire format: `{t: "cdp_request", method: "DOM.getNodeForLocation", params: {x, y}, requestId: "node:<ts>"}`; response `{requestId, result: {nodeId, ...}}`. Late responses after timeout ignored; disconnect closes all pending.

### Tests (before coding)
- Right-click opens menu at correct canvas coords (zoom/offset included).
- Dismiss on click-outside / Escape.
- Does not open when pointer capture disabled (select mode).
- Async node lookup: success (href/src), missing target, CDP disconnected, timeout, clipboard failure.
- Back/Forward disabled states.
- No duplicate CDP right-click emission (pointerdown only; menu open does not trigger second mouse event).
- Long-press cancellation: touch long-press does not duplicate; cancel on scroll/move.
- Stale timeout response ignored after disconnect.
- Remote URL correctness: Copy URL / Open External uses `browserStore` URL, not `window.location.href`.

### Protocol / backend change (restored)
`internal/browse/cdpsocket.go` adds `SendDOMGetNodeForLocation(x,y int) (*NodeLocation, error)`. `useCdpSocket.ts` exposes async `getNodeAt(x,y): Promise<NodeResult>`. Timeout + unavailable-target errors propagate to UI (show brief inline error, not crash).

### Coordinates
`clientX/clientY` (fixed viewport) for menu portal; `canvasPos(x,y)` (offset + CSS scale + zoom + scroll) for `DOM.getNodeForLocation`. Menu clamped to viewport (no overflow).

### UX gaps clarified
Inspect result shown in `DevConsole` / debug overlay; touch long-press opens same host menu (`onContextMenu` only, not native); iframe/shadow-DOM nodes returned as-is with `frameId` included; deepest visible node returned for overlapping elements; `href` (anchor) / `src` (img/video) extracted from `attributes`; fall back to page URL.
