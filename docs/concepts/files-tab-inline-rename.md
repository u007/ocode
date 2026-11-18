---
type: Concept
title: Files Tab Inline Rename (Finder-style click to rename)
description: "Web/desktop Files-tab inline rename (Finder-style): the second plain click on an already-selected file name opens an editor in place of the row (tree and columns views), with the commit, cancel and double-commit rules, the shared performRename request path, and the verification gaps."
resource: web/src/components/Files/FileTree.tsx
tags:
  - files-tab
  - rename
  - inline-edit
  - web
  - desktop
  - concept
timestamp: 2026-11-18T02:55:00Z
status: active
---

# Files tab inline rename

Finder-style rename on the web/desktop Files tab (`web/src/components/Files/FileTree.tsx`). A second plain click on the name of a file that was already the sole selection turns that row into a text input. The context-menu "Rename…" dialog is the other entry point and shares the same network call. The server side is unchanged: `POST /api/fs/rename` (route in `server.go`), with `fsRenameLocal` (`internal/server/handler_fs.go`) and `fsRenameRemote` (`internal/server/handler_remote_fs.go`). Related: [Files tab clipboard](files-tab-clipboard.md) covers the other Files-tab mutations.

## The gesture (`isInlineRenameClick`)

`isInlineRenameClick(e, path, isDir, selectedPaths, selectedPath)` is the single predicate. A click starts a rename only when all of these hold:

- The target is inside the name span (`data-tree-name`). The icon, the badge, the checkbox and the row padding do not count.
- The row is a file. Directories never rename: a click on a folder toggles its expansion, so a click-again rename would hijack collapse.
- No shift, meta, ctrl or alt modifier.
- `e.detail < 2`. The second click of a double-click does not start a rename; the double-click still opens the file.
- The selection was exactly this path before the click: `selectedPaths.size === 1`, `selectedPaths.has(path)` and `selectedPath === path`. The first click of the pair makes the normal selection; only the next plain click renames.

The predicate is shared by two call sites: `TreeNode.handleRowClick` (tree view), which checks it before `setFocus` so it sees the pre-click selection, and the Miller-columns file row.

## The inline editor

- Starting a rename calls `beginInlineRename`, which sets `renamingPath` (state in `FileTree`). `FileMenuActions` exposes `renamingPath`, `beginInlineRename`, `commitInlineRename` and `cancelInlineRename` to both views.
- While a path is renaming, its row's `<button>` is replaced by a `<div>`. An input inside a button is invalid HTML. The `<div>` keeps the file icon and renders `InlineRenameInput` in the name slot, in both views.
- `InlineRenameInput` preselects the stem: everything up to the last dot. A leading dot does not count, so `.gitignore` selects the whole name.
- Enter commits. Blur also commits, so clicking elsewhere saves the edit. Escape cancels.
- A `doneRef` flag makes the first finish win. Enter followed by the blur from unmounting does not commit twice. Escape sets the flag before cancelling, so the blur from unmounting cannot commit a cancelled edit.
- The input stops propagation of click, mousedown, pointerdown, contextmenu and keydown. The row, the Radix context-menu trigger and the document-level tree shortcuts never see those events while the editor is open.
- `commitInlineRename` clears `renamingPath` first, trims the value, and returns without a request when the value is empty or equals the current name.

## The request path (`performRename`)

`performRename(path, value)` is the only rename network path. It runs `runFs` around `api.fsRename(anchorPath(path), value, requestRoot, projectHost)` (`web/src/api/client.ts`). It then dispatches the `ocode:fs-rename` window event with `oldPath`, `newPath`, `projectRoot` and, for remote projects, `host`. `newPath` comes from the response's `path`, falling back to the parent directory plus the new name.

The context-menu "Rename…" item still opens `PromptDialog` (title "Rename"). Its confirm handler calls `performRename` and returns early on an empty value. The dialog itself is unchanged.

## Known limits

- `renamingPath` is not reset on project switch. No existing selection-reset effect was found to hook into, so the stale path stays set and the editor could reappear if the same path shows up in the newly opened project.
- No keyboard shortcut (F2) starts an inline rename. The context-menu "Rename…" dialog is the keyboard-reachable path.

## Verification status (2026-11-18)

Verified:

- `bun check .` in `web/` is clean.
- `bunx vitest run src/components/Files` passes 37 files and 282 tests, including `FileTree.inlineRename.test.tsx` (tree view, 6 cases).
- In a headless Chrome against a private dev server, the tree view showed the input after the second click, with the name selected and focused. Escape closed it.

Not verified:

- The columns-view gesture, in a browser or a test.
- A committed rename in a browser.
- The desktop WKWebView shell.
- Remote (SSH/WSL) projects.

## Where to look

Cite by name, not line: `isInlineRenameClick`, `InlineRenameInput`, `TreeNode.handleRowClick`, `FileTree` (`renamingPath`, `beginInlineRename`, `commitInlineRename`, `cancelInlineRename`, `performRename`), `FileMenuActions`. Tests: `web/src/components/Files/FileTree.inlineRename.test.tsx` and `FileTree.renameCancel.test.tsx`.
