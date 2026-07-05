---
name: ocode-desktop
description: How the ocode desktop shell is wired — Wails v3 entry point, native webview window lifecycle, server boot helper, dock/tray/notification integration, macOS .app bundling, and the shared web+server embed architecture. Use this whenever working on the desktop app (new window features, tray/dock/notifications, dev workflow, .app bundle, platform-specific gotchas).
when_to_use: When the user asks about the desktop app, Wails v3 integration, native window/tray/dock features, macOS .app bundling, server boot for desktop, notifications, dev URL override, or anything under cmd/ocode-desktop or internal/desktop.
---

# ocode Desktop Field Guide

A dense map of the ocode desktop shell — how the Wails v3 native wrapper bootstraps the Go server and web UI into a native window.

## 1. Architecture Overview

The desktop app is **not a separate frontend**. It's a thin native wrapper (Wails v3) around the **same Go backend + same embedded React SPA** that powers the web server. No Electron, no Tauri — Wails v3 uses the native WebView (WKWebView on macOS, WebKitGTK on Linux, WebView2 on Windows).

```
┌──────────────────────────────────────────────────────┐
│  ocode-desktop binary                                 │
│                                                       │
│  cmd/ocode-desktop/main.go                            │
│  ┌──────────────────────────────────────────────┐    │
│  │  Wails v3 WebviewWindow                       │    │
│  │  (WKWebView / WebKitGTK / WebView2)           │    │
│  │  ──→ http://127.0.0.1:PORT/?token=HEX        │    │
│  └──────────────────┬───────────────────────────┘    │
│                     │ webview navigation              │
│  ┌──────────────────▼───────────────────────────┐    │
│  │  In-process Go HTTP/SSE server                │    │
│  │  (127.0.0.1:random, random auth token)        │    │
│  │  │                                             │    │
│  │  ├── internal/server/         — API routes     │    │
│  │  ├── web.FS()                 — embedded SPA   │    │
│  │  └── internal/agent/          — LLM agent      │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │  Native services                              │    │
│  │  ├── Dock (badge count)                       │    │
│  │  ├── Notification (permission alerts)         │    │
│  │  ├── SystemTray (show/hide/quit)              │    │
│  │  └── application Menu (default Edit menu)     │    │
│  └──────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────┘
```

## 2. Key Files

| File | Purpose |
|------|---------|
| `cmd/ocode-desktop/main.go` | Entry point: resolve workdir, boot server, create Wails window, wire services |
| `cmd/ocode-desktop/native.go` | `wireNative()` — dock badge, notifications, focus tracking, tray |
| `internal/desktop/boot.go` | `StartServer()` — bind on random port, generate token, return Handle |
| `internal/desktop/watch.go` | `Watch()` — poll `Server.RunStates()`, diff and compute badge/notification state |
| `scripts/bundle-macos.sh` | macOS `.app` bundle creation script |
| `web/embed.go` | `//go:embed all:dist` — the SPA embedded in every binary |
| `web/vite.config.ts` | Vite build config (shared with server mode and desktop mode) |

## 3. Build & Dev

| Command | What it does |
|---------|-------------|
| `make desktop` | `cd web && npm install && npm run build` then `go build -o bin/ocode-desktop ./cmd/ocode-desktop` |
| `make desktop-app` | Same as `desktop` + `scripts/bundle-macos.sh` to produce `bin/ocode.app` |
| `go build -o bin/ocode-desktop ./cmd/ocode-desktop` | Build the bare binary (faster iteration) |
| `cd web && npm run dev` | Start Vite dev server separately |

**Dev mode with hot-reload**: Set `OCODE_DESKTOP_DEV_URL` env var to the Vite dev server URL:

```bash
OCODE_DESKTOP_DEV_URL=http://localhost:5173 bin/ocode-desktop
```

This bypasses the in-process server URL and points the webview at the Vite dev server instead. The desktop binary still boots the in-process API server (for API calls), but the frontend assets come from Vite's dev server with HMR.

## 4. Entry Point — `cmd/ocode-desktop/main.go`

The full startup sequence:

1. **Resolve workdir** — `os.Getwd()`. A Finder/Dock-launched `.app` starts with CWD `/`, so fall back to `os.UserHomeDir()` to prevent session/upload paths from targeting the root.

2. **Boot server** — `desktop.StartServer(web.FS(), workDir)`:
   - Generates a random 16-byte hex token
   - Calls `server.New("127.0.0.1:0", "ocode", token, webFS)` — binds on random port
   - `srv.Listen()` starts the HTTP listener
   - Returns a `Handle{URL, Token, Srv}` (e.g. `URL = "http://127.0.0.1:52341"`)
   - The server runs in a background goroutine and dies with the process — there is no graceful shutdown

3. **Create Wails services** — dock service (always) + notification service (conditional):
   - `dock.New()` — always created on macOS; `dockSvc.SetBadgeCount(n)` sets the dock badge
   - `notifications.New()` — **only created when `notificationsSupported()` is true** (see gotcha #2)
   - Wrapped in `application.NewService()` and passed to `application.New()`

4. **Build webview URL** — `handle.URL + "/?token=" + handle.Token` (same `?token=` param the web `/rc` command and EventSource use)

5. **Create Wails WebviewWindow** — 1280×800 default, 800×600 minimum, titled "ocode"

6. **Wire system tray** — Show/quit menu items (Show calls `window.Show()`)

7. **Wire native integration** — `wireNative(ctx, window, handle, notifier, dockSvc)` runs in a goroutine

8. **Run the app** — `app.Run()` (blocking)

**Error handling**: If the server fails to boot (`bootErr != nil`), a **native dialog** is shown (because stderr is invisible from a Finder-launched `.app`), then `os.Exit(1)`.

## 5. Server Boot — `internal/desktop/boot.go`

Pure Go — no Wails import, keeping unit tests cgo-free.

```go
type Handle struct {
    URL   string         // e.g. "http://127.0.0.1:52341"
    Token string         // 32 hex chars (16 random bytes)
    Srv   *server.Server
}
```

`StartServer(webFS fs.FS, workDir string) (*Handle, error)`:
- Generates 16 random bytes → hex token (32 chars)
- `server.New("127.0.0.1:0", "ocode", token, webFS)` — binds to random port
- `srv.SetWorkDir(workDir)`
- `srv.Listen()` → reads actual bound address from `ln.Addr()`
- Starts `srv.Serve(ln)` in a goroutine
- Returns the Handle

**Key detail**: The server has no graceful-shutdown API. Window close = app quit = process exit. The server goroutine dies with the process.

## 6. Run-State Watcher — `internal/desktop/watch.go`

`Watch(ctx, handle, dockSvc, notifier, focused)` polls `handle.Srv.RunStates()` every 750ms:

- Diffs the running/ended run list from the previous poll
- Computes:
  - **Badge count** = number of runs that are in progress (`RunAgentState`)
  - **Finished count** = number of runs that ended since last poll
- Updates dock badge via `dockSvc.SetBadgeCount(badgeCount)`
- Sends notification when a run completes and the window is unfocused: "Agent run finished" (single notification per batch, not one per run)

```go
watchCtx, cancel := context.WithCancel(ctx)
defer cancel()
go func() {
    t := time.NewTicker(750 * time.Millisecond)
    var prev map[string]*server.RunState
    for {
        select {
        case <-t.C:
            cur := srv.RunStates()
            // diff, update badge, send notification if !focused
            prev = cur
        case <-watchCtx.Done():
            t.Stop()
            return
        }
    }
}()
```

## 7. Native Integration — `cmd/ocode-desktop/native.go`

### `notificationsSupported()` (macOS guard)
On macOS, calling `UNUserNotificationCenter` (which `notifications.New()` does internally) from a non-`.app` binary throws `NSInternalInconsistencyException` and **aborts the process**. Returns `true` only if the executable path contains `.app/Contents/MacOS/`. On non-macOS, returns `true` unconditionally.

### `wireNative()`
Runs in a background goroutine. Connects:
1. **Focus tracking** — `window.OnWindowEvent(WindowFocus)` and `WindowLostFocus` → `focused` atomic bool
2. **Server watcher** — calls `desktop.Watch(ctx, handle, dockSvc, notifier, &focused)` with the context, runs until cancelled

## 8. macOS .app Bundle — `scripts/bundle-macos.sh`

Creates `bin/ocode.app` with the standard macOS bundle layout:

```
ocode.app/
├── Contents/
│   ├── Info.plist          # Bundle name, identifier, icon, LSUIElement
│   ├── MacOS/
│   │   └── ocode-desktop   # The compiled binary
│   └── Resources/
│       └── icon.icns       # App icon
```

`LSUIElement = true` means the app has no menu bar icon and no dock icon by default (it uses the system tray icon instead). The script copies the binary, generates a minimal `Info.plist`, and codesigns if a certificate is available.

## 9. Key Architectural Decisions & Gotchas

1. **No separate desktop frontend** — The web UI (`web/`) IS the desktop frontend. Same React SPA, same Go backend. Desktop adds native wrappers (window, tray, dock badge, notifications) but the UI code is shared. Never add desktop-only React components — use feature detection (e.g., `window.__OCODE_DESKTOP__` or similar) if you must branch.

2. **macOS notification trap** — `notifications.New()` calls `UNUserNotificationCenter`, which **crashes the process** when the binary is not inside a `.app` bundle on macOS. Always call `notificationsSupported()` before creating the notification service. The bare `bin/ocode-desktop` binary must never touch the notifier. Dev workflow uses the bare binary; hot-reload mode intentionally has no notifications.

3. **No graceful shutdown** — The server goroutine dies with the process. `server.New()` has no `Shutdown()` or `Close()` method. Window close = process exit. If you add cleanup logic, wire it to `app.OnShutdown()` or a signal handler, not a server lifecycle hook.

4. **Auth token is single-use per bootstrap** — The random token is generated once at startup and remains valid for the lifetime of the process. There's no token rotation or refresh. The webview URL includes it as `?token=` query param.

5. **Random port, loopback-only** — The server binds to `127.0.0.1:0` (random available port). There is no way to configure the port for desktop mode — it's always random for security. The dev URL override only changes what URL the webview navigates to (for HMR), not the API server port.

6. **Workdir fallback** — A Dock/Finder-launched app gets CWD `/`. The code falls back to `os.UserHomeDir()` in this case. If you add file operations that depend on the working directory, always use `handle.Srv.WorkDir()` (or the server's embedded workdir), not `os.Getwd()`.

7. **No tests in `cmd/ocode-desktop/` or `internal/desktop/`** — The Wails-dependent code in `cmd/ocode-desktop/` cannot be easily unit-tested (requires Wails runtime). `internal/desktop/` is designed to be testable (pure Go, no Wails import), but no tests currently exist. Add tests in `internal/desktop/` for any new logic in the boot helper or watcher.

8. **Dev URL `OCODE_DESKTOP_DEV_URL` override** — Set this to `http://localhost:5173` during development to get Vite HMR in the desktop webview. The API server still boots in-process; only the frontend assets are served from Vite. The server token is still generated and must match between the URL and the API client.

9. **MinWindowSize** — The window is clamped to 800×600 minimum. If you add a new chrome row or sidebar that makes the content area too small at 600px height, either reduce chrome or raise the minimum.

10. **System tray only** — The app has no visible dock icon by default (`LSUIElement = true`). Clicking the tray icon shows/hides the window. The "Show ocode" tray item calls `window.Show()`. If you add a "Quit" confirmation, wire it to `app.Quit()`, not `os.Exit()`.
