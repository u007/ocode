# Desktop Single Instance Lock

**Status:** Implemented 2026-08-31 (`cmd/ocode-desktop/main.go`).

## Rule

Only one `ocode-desktop` process may run per user. The desktop process owns
an in-process API server (random loopback port + token), the terminal ptys
(see `terminal-detach-reattach.md`), and the `windowId=main` per-window
profile state. A second copy would silently fork all of that: two servers,
two sets of shells, and terminal tabs that could never reattach across them.

## Mechanism

- `application.Options.SingleInstance` with `UniqueID: "com.ocode.desktop"`.
  Wails acquires a per-user lock inside `application.New` (macOS: flock on
  `<NSTemporaryDirectory>/com.ocode.desktop.lock`; Linux: equivalent file
  lock; Windows: named mutex).
- `application.New` is therefore called **before** `desktop.StartServer`.
  The losing process forwards its argv to the first instance and
  `os.Exit`s before it ever boots a server. Do not reorder these.
- `OnSecondInstanceLaunch` in the first instance raises the window
  (`UnMinimise` → `Show` → `Focus`) and, if the forwarded args carry
  `-session <id>` / `--session <id>`, navigates the webview to that
  session route via `SetURL`. `sessionIDFromArgs` is shared by both the
  normal launch and this path.
- The callback runs on Wails' listener goroutine before the window may
  exist, so it reads the window through an `atomic.Pointer[desktopWindow]`
  stored right after window creation; a nil load (second launch racing our
  own startup) is dropped with a log line.

## Gotchas

- The `lsp-daemon` hidden subcommand dispatches *before* `application.New`
  and never touches the lock — the LSP broker child must keep running
  alongside the desktop app.
- Dev runs (`OCODE_DESKTOP_DEV_URL`) share the same lock: stop the packaged
  app before launching a dev build, or the dev build just focuses it.
