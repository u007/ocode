# ocode Desktop Shell (Wails v3) — Design

Date: 2026-07-02
Status: Approved

## Goal

Cross-platform (macOS/Windows/Linux) desktop app that reuses the existing
`web/` React SPA and the built-in `internal/server` HTTP/SSE API with zero
duplication. The desktop shell is a native window over the same server the
browser uses today.

## Decisions (made during brainstorming)

- **Transport:** existing loopback HTTP/SSE only. No Wails JS↔Go bindings, no
  second RPC surface. The `/api/*` contract stays the single API surface.
- **Binary:** separate binary `cmd/ocode-desktop`, same Go module (must import
  `internal/server`). Wails v3 (alpha) dependency is only compiled into this
  binary; the main `ocode` binary stays pure-Go and cross-compilable.
- **Shell library:** Wails v3 alpha (plain-URL windows, multi-window, tray,
  notifications). Risk of alpha API churn is contained to this one small binary.
- **Server lifecycle:** the app always owns its server. Start `internal/server`
  in-process on `127.0.0.1:0` (random port) with a freshly generated auth
  token; shut it down (context cancel) when the window closes. No
  attach-to-running-server discovery.
- **Scope:** full desktop citizen (menus, tray, dock badge, notifications).

## Architecture

```
cmd/ocode-desktop (Wails v3 app, cgo)
  ├─ starts internal/server on 127.0.0.1:0 + fresh auth token
  ├─ WebviewWindow → http://127.0.0.1:<port>/?token=... (same flow as TUI /rc)
  └─ native features poll the same Handler snapshot accessors the SSE
     handlers use (Go-side, in-process)

web/ SPA and internal/server handlers run unchanged.
```

**Embedded assets:** the `//go:embed all:web/dist` directive currently lives in
the root `main.go`; `go:embed` cannot reach `../..`, so a second main package
cannot reuse it. Add `web/embed.go` (package `web`, embeds `dist/`) and have
both binaries import it — single embed point, DRY.

## Native features (all Go-side, no JS bridge)

- **Menus:** native app menu including Edit menu (fixes Cmd+C/V in macOS
  webviews), window/quit shortcuts.
- **Tray:** icon with show/hide window and quit.
- **Dock badge:** count of pending permission prompts / running agents.
  macOS/Windows only (Wails v3 badge service has no Linux support).
- **Notifications:** run finished / permission needed, emitted only when the
  window is unfocused; clicking a notification focuses the window.

There is no push-based app event bus (`internal/notebus` is the subagent
notes/handoff bus — not usable here). Run state is exposed by polling the
agent run registry, exactly as `HandleRunsStream` does (750ms poll +
snapshot diff). Badge/notification logic reuses the same `Handler` snapshot
accessors with the same poll-and-diff pattern — one data source, two
consumers.
- **External links:** open in the OS default browser, not the webview.

## Lifecycle & error handling

- Window close = app quit = process exit (the listener dies with the
  process). `internal/server` has no graceful-shutdown API (`http.Serve`
  without a retained `http.Server`); process exit is sufficient for a
  desktop app and avoids touching the server package.
- Port bind or server startup failure → native error dialog, exit non-zero.
- Implementation verify item: confirm `Server.Listen` handles `:0` random
  ports (its retry loop scans `port..port+N`).
- Windows: Wails handles the WebView2 bootstrap. Linux: WebKitGTK is a
  documented package prerequisite.

## Dev workflow & packaging

- `OCODE_DESKTOP_DEV_URL` env var points the window at the Vite dev server for
  frontend hot reload; the API server still runs in-process.
- Packaging: `make desktop` builds the binary; `make desktop-app` bundles a
  macOS `.app` via `scripts/bundle-macos.sh`. Installer packaging (Windows,
  Linux) and signing are deferred — tracked in TODO.md.

## Testing

- Unit test: server-boot helper (port selection, token generation, readiness).
- Unit test: run-snapshot diff → badge/notification mapping, using fake
  snapshots.
- Window/tray/menus: manual smoke checklist per platform (Wails v3 alpha has
  no headless test story).

## Risks

- Wails v3 is alpha: API churn possible. Mitigation: the shell is a thin,
  isolated binary (~few hundred lines); the server and SPA are untouched.
- cgo/platform SDK requirements apply only to `cmd/ocode-desktop` builds, not
  to the main `ocode` binary or its CI path.
