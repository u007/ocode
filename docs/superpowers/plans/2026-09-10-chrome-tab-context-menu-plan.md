# Chrome Tab Context Menu — Implementation Plan (revised)

Based on spec: `docs/superpowers/specs/2026-09-10-chrome-tab-context-menu-design.md`

## End-to-end transport / backend
- `internal/browse/cdp/target.go`: `DOM.getNodeForLocation` + `DOM.describeNode` calls.
- `internal/browse/server.go`: optional interface / `chromeTarget` extension; fakes unchanged.
- `internal/browse/cdpsocket.go`: routing (`requestId` string), typed `NodeLocation`, timeout/error mapping (`timeout`/`disconnected`/`target_unavailable`/`protocol_error`).
- `web/src/components/Browser/cdpProtocol.ts`: client/server union types (`CdpRequest`/`CdpResponse`).
- `useCdpSocket.ts`: pending-request map (`requestId` → resolver), 5s timeout per request, reject-all on close, ignore late replies.

## Component / event ownership
- `ContextMenu.tsx`: already exists; keeps opening via bubbling `contextmenu`. `ChromeViewport` owns a second controlled menu (or extends existing) by intercepting `contextmenu` with `stopPropagation` so parent does not open. Menu receives `{x, y, url}` props; updates `disabled` state asynchronously from `getNodeAt`.
- `ChromeViewport`: `onContextMenu` suppresses native + opens menu; does NOT emit another CDP mouse event (`onPointerDown`/`onPointerUp` stay the only CDP source at 451-477, 492-502).

## Async lifecycle
- Open menu with initial disabled states (`Back`/`Forward`/`Inspect`/`Copy Link` disabled until lookup). Prefetch `getNodeAt` on open; update item states when result arrives.
- Stale lookup after second right-click / unmount: new request cancels previous; result ignored if menu closed or tab unmounted.
- Brief inline error for timeout/unavailable/clipboard fail (no crash). Inspect result routed to existing `DevConsole`.

## Tests
- `cdpsocket_test.go`: request routing, typed response, timeout rejection, close cleanup.
- `internal/browse/cdp/target` tests: `getNodeForLocation` + `describeNode` wire format.
- `cdpProtocol.ts` / `useCdpSocket`: pending-request map, timeout, disconnect.
- `ChromeViewport.test.tsx`: no duplicate mouse emission, `onContextMenu` suppression, disabled-state updates.


## Concrete file / symbol targets
- `internal/browse/cdpsocket.go`: `SendDOMGetNodeForLocation`, typed `NodeLocation`.
- `internal/browse/cdp/target.go`: `DOM.getNodeForLocation`, `DOM.describeNode`.
- `internal/browse/server.go`: `chromeTarget` optional interface; no fake break.
- `web/src/components/Browser/cdpProtocol.ts`: `CdpRequest`/`CdpResponse` union types.
- `useCdpSocket.ts`: `getNodeAt(x,y)`, pending map, timeout/reject-all/ignore-late.
- `ChromeViewport` (`web/src/components/Browser/ChromeViewport.tsx`: lines 451-502 mouse events; new `onContextMenu`, `ContextMenu` render, `browserStore.getState(s => s.url)`).
- `ContextMenu` (`web/src/components/Layout/ContextMenu.tsx`): controlled via props, no bubbling.
- Tests: `internal/browse/cdpsocket_test.go`, `internal/browse/cdp/target` tests, `ChromeViewport.test.tsx`, `cdpProtocol.ts`/`useCdpSocket` tests.

## Validation performed
Document inspection only (`git diff --check` passed; spec + plan revised; `/tmp/fix_spec.py` removed). No build, test, or runtime validation executed — design/planning phase only.
