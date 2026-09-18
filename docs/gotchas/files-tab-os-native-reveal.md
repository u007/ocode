---
type: Gotcha
title: Files-Tab OS-Native Reveal (Open/Show in Finder/Explorer/File Manager)
description: 'Gotcha: OS-native reveal for Files-tab — per-platform argv, the remote-project guard, Linux DBus path, and the directory-mode status-code gotcha (404 vs 400).'
tags:
  - gotcha
  - files-tab
  - reveal
  - finder
  - explorer
  - xdg-open
  - dbus
  - file-manager
  - remote-guard
  - linux
  - darwin
  - windows
timestamp: 2026-09-18T18:39:31Z
---
`POST /api/files/open` now accepts `mode: "reveal"` in addition to the existing `""`/`"editor"`/`"os"` modes. Request body: `{path, mode: "reveal", project_root?}`. Handler: `internal/server/handler_open.go`.

## reveal is the only mode that accepts a directory

`reveal` is the **only** mode that accepts a directory argument — a folder opens in the OS file manager, a file is selected/highlighted inside its containing folder. `editor` and `os` still return 404 ("file not found") for a directory.

Containment is unchanged: `resolveWithinWorkdir` / `filepath.Rel` against the allowed project root still applies, so reveal cannot escape the workdir — traversal returns 400.

## Per-platform argv (`revealCommand`)

`internal/server/reveal.go` exposes `revealCommand(goos, absPath, isDir)` — a pure function, fully unit-tested:

| Platform | Directory | File |
|----------|-----------|------|
| **darwin** | `open <dir>` | `open -R <file>` (Finder selects the file) |
| **windows** | `explorer <dir>` | `explorer /select,<file>` |
| **linux/other** | `xdg-open <dir>` | DBus: `dbus-send --session --dest=org.freedesktop.FileManager1 --type=method_call --print-reply /org/freedesktop/FileManager1 org.freedesktop.FileManager1.ShowItems array:string:<file:// URI> string:` |

The Linux DBus path is the **only reliable Wayland path** for selecting a file — it is the standard freedesktop interface used by Nautilus, Thunar, Dolphin, and Nemo. `revealPathInFileManager` probes dbus-send with a 2-second bounded wait and falls back to opening the containing folder via `systemOpener` when `org.freedesktop.FileManager1` is absent (headless/minimal host).

`--print-reply` is load-bearing: without it dbus-send is fire-and-forget and always exits 0, so a missing service would be undetectable. The test suite (`internal/server/reveal_test.go`) verifies percent-encoding of the `file://` URI.

`revealPathFn` is a package-level test seam (same pattern as `notifyGitAction`) so tests never launch a real file manager.

## Web UI: platform noun comes from the server, not the browser

FileTree context menus (`web/src/components/Files/FileTree.tsx`) offer **"Open in Finder/Explorer/File Manager"** for directories and **"Show in Finder/Explorer/File Manager"** for files. The manager noun comes from `GET /api/config/ocode/paths.platform` (the server's `runtime.GOOS`), **never** from `navigator.platform` — the browser's platform describes the machine the browser runs on, not the machine that executes the command. A user running the web UI on macOS against a Linux server must see "File Manager", not "Finder".

Client call: `api.revealInFileManager(path, projectRoot)` in `web/src/api/client.ts`. FileTree passes the anchored `requestPath` (same as "Copy path" — project-root-joined), so the server resolves it against the active project root.

## GOTCHA (critical): reveal is HIDDEN for remote projects

**The reveal action is hidden when the project has a remote host: `canReveal: !projectHost`.**

`POST /api/files/open` has **no remote branch** — it runs on the local server host. Offering reveal for a remote project would open/reveal an unrelated path on the **local** machine, not the remote one.

Any future "reveal" surface — new UI, CLI flag, API variant — **must apply the same `!host` guard**. This is a trust-boundary rule, not a feature gap.

## Test coverage

- `internal/server/reveal_test.go`: per-platform argv matrix, Linux FileManager1 URI percent-encoding, `TestHandleOpenFileRevealAllowsDirectory`, `TestHandleOpenFileRevealKeepsContainment`.
- `internal/server/handler_config_test.go`: `TestHandleGetPathsConfigIncludesPlatform` (platform string exposed to frontend).
- `web/src/components/Files/FileTree.reveal.test.tsx`: context menu label, hidden-for-remote guard.