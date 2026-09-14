---
type: Gotcha
title: Remote Terminal Custom Port Omitted from WebSocket
description: TerminalPanel propagates remotePort for history restoration but omits it from the live WebSocket connection, breaking non-default SSH port routing.
resource: web/src/components/Terminal/TerminalPanel.tsx
tags:
  - gotcha
  - terminal
  - remote
  - ssh
  - websocket
  - port
timestamp: 2026-09-14T02:15:21Z
---
## Problem

Remote terminal history restoration can use the configured SSH port, while the live terminal WebSocket connection omits that port. Sessions on non-default SSH ports can therefore be admitted or routed using the wrong port after history has loaded.

## Root cause

`TerminalPanel` passes `remotePort` to `restoreTerminalHistory`, and `terminalHistory.ts` includes it as the `port` query parameter for remote history requests. However, the `connectSocket` call in `web/src/components/Terminal/TerminalPanel.tsx` passes `host` and other connection fields to `buildTerminalWsConnection` but omits `remotePort`.

The helper supports `remotePort` and serializes it as `port` when `host` is present, but it cannot include a value that the caller failed to pass. This creates an inconsistent split: history can target the configured SSH port while the live WebSocket uses the default routing.

## Fix guidance

Pass `remotePort` into `buildTerminalWsConnection` from the live socket connection path. Add a mounted/component-level regression test that renders or exercises `TerminalPanel` with a non-default remote port and verifies the actual WebSocket URL contains the expected `port` query parameter; a helper-only test is insufficient.

## Affected code

- `web/src/components/Terminal/TerminalPanel.tsx` — `connectSocket` and `buildTerminalWsConnection` call site
- `web/src/components/Terminal/terminalHistory.ts` — history request already propagates `remotePort`
- `web/src/components/Terminal/TerminalPanel.wsAuth.test.ts` — helper-level URL coverage; does not replace the component-level regression test
