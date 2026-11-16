---
type: Gotcha
title: Files-Tab OS-Native Reveal (Open/Show in Finder/Explorer/File Manager)
description: 'Gotcha: OS-native reveal for Files-tab — per-platform argv, the remote-project guard, Linux DBus path, the directory-mode status-code gotcha (404 vs 400), and the tab-bar "Show in file tree" fallback that hands an out-of-tree path to the OS file manager (absolute unanchored, root-relative with its root).'
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
timestamp: 2026-11-15T15:35:50Z
---
`POST /api/files/open` now accepts `mode: "reveal"` in addition to the existing `""`/`"editor"`/`"os"` modes. Request body: `{path, mode: "reveal", project_root?}`. Handler: `internal/server/handler_open.go`.

## reveal is the only mode that accepts a directory

`reveal` is the **only** mode that accepts a directory argument — a folder opens in the OS file manager, a file is selected/highlighted inside its containing folder. `editor` and `os` still return 404 ("file not found") for a directory.

Containment is unchanged: an anchored path is still bounded by `filepath.Rel` against that root, and an unanchored absolute path must fall inside some root the file surfaces may touch — `pathWithinAnyFileRoot` over `fileAccessRoots()`: project roots, `extra_allowed_paths`, and editor grants. Traversal is still a 400.

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

## Reveal outside the browsed tree: hand the path to the OS file manager

The editor/preview tab bar's **"Show in file tree"** (`revealRequest` effect in
`web/src/components/Files/FileTree.tsx`) used to end in
"`<path>` is outside the folder this tree is browsing" whenever the tab's path
could not be a row of the tree. That is the wrong answer: the file has a tab
open, so it exists on the machine the reveal runs on. Both refusing branches now
call `revealOutsideTree`:

1. the request's root is not one of `rootOptions` (project root +
   `extra_allowed_paths` + editor grants), or
2. `treeRelativePath(req.path, root)` returns null — an absolute path that is
   not under the browsed root.

`revealOutsideTree` calls `api.revealInFileManager(...)` →
`POST /api/files/open {mode: "reveal"}` and reports
`Opened in <file manager>`, using the same server-GOOS noun as the context menu
(`fileManagerLabel(serverPlatform)`).

### Anchor by path shape: absolute unanchored, relative with its root

- An **absolute** path is sent with **no** `project_root`. The server then
  checks it against every root the file surfaces may touch (project roots,
  `extra_allowed_paths`, editor grants). No single root could anchor it.
- A **root-relative** path is sent with the root the tab came from
  (`req.root ?? browsed root`), because only that root can resolve it. This is
  the shape an out-of-project tab has — e.g. `qa-screenshots/00-home.png` under
  an editor grant that has since died.

`isAbsoluteTreePath` (`web/src/components/Files/fileTreeReveal.ts`) makes that
decision. It is exported for this caller; the helpers stay DOM-free.

### Two cases stay notice-only

- **A remote project** (`projectHost` set). Reveal has no remote branch — the
  same `canReveal: !projectHost` rule as the context menu. Handing a remote
  project's path to the local file manager would reveal an unrelated file.
- **The reveal API refuses the path** (a root that is no longer allowed, a dead
  grant, a path outside every file root). The client then shows the explanation
  instead of a false `Opened in …`.

Root cause class: editor grants are in-memory per server process
(`concepts/file-editor-grants.md`), so an out-of-project tab can name a root the
tree cannot browse. A not-yet-refreshed grant list and an absolute chat-link
path against a `~`-rooted project reach the same branch.

## Test coverage

- `internal/server/reveal_test.go`: per-platform argv matrix, Linux FileManager1 URI percent-encoding, `TestHandleOpenFileRevealAllowsDirectory`, `TestHandleOpenFileRevealKeepsContainment`.
- `internal/server/handler_config_test.go`: `TestHandleGetPathsConfigIncludesPlatform` (platform string exposed to frontend).
- `web/src/components/Files/FileTree.reveal.test.tsx`: context menu label, hidden-for-remote guard.
- `web/src/components/Files/FileTree.revealInTree.test.tsx` (17 tests): the four out-of-tree cases — an absolute path is handed over **unanchored** (`toHaveBeenCalledWith(path, undefined)`), a root-relative path keeps the tab's root, a rejected API call keeps the notice, and a remote project keeps the notice with `revealInFileManager` never called.