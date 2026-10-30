---
type: Concept
title: Files Tab Clipboard (Cut / Copy / Paste)
timestamp: 2026-10-30T02:22:13Z
description: Files-tab clipboard, drag-in upload and right-click download concept doc with desktop WKWebView verification caveat
tags:
  - clipboard
  - files-tab
  - os-file-paste
  - drag-and-drop
  - download
  - concept
---
---
type: Concept
title: Files Tab Clipboard (Cut / Copy / Paste)
---

# Files tab clipboard (cut / copy / paste)

Shipped feature on the web/desktop Files tab (`web/src/components/Files/FileTree.tsx`). A single `pasteDestination` rule resolves the paste target; every affordance (context menus, toolbar button, keyboard) reports the same resolved destination. This doc covers the workflow, the OS-file upload path (`POST /api/fs/upload`), drag-in upload and right-click Download (`GET /api/fs/download`), and the known gaps.

## Paste-destination rule (`FileTree.tsx` `pasteDestination`)

Given `(root, focusNode, selectionSize)`:

- Focused folder → that folder.
- Focused file → its parent (paste beside it).
- No focus, or a multi-selection (`selectionSize > 1`) → the tree root (currently browsed root: `activeRoot ?? projectPath`).
- The label shown in notices is `pasteDestinationLabel(root, destDir)` (e.g. `"src/"`, `"proj/"`).

Every paste reports the resolved destination: `"Copied 2 items to src/"` (or `"Cut …"`, `"Pasted …"`). The same label is used by the right-click menu items, the toolbar button tooltip, and the keyboard paste toast.

## Entry points (all resolve through the same helper)

1. **Right-click blank space in the tree body** — context menu with Paste / New file / New folder / Refresh; the "Paste …" item shows the resolved root label.
2. **Right-click the columns-view root column header** — same menu, root as implied destination.
3. **Toolbar Clipboard button** — shown only while the in-app clipboard has content (`clipboardItems.length > 0`); clicking pastes to the root and reports the destination.
4. **Keyboard shortcuts** (see next section) — `Cmd/Ctrl+V` pastes to the same resolved destination and shows the same destination toast.

No other Files-tab surfaces (search, git badges, reveal dialog) are involved in this workflow.

## Shortcuts (`FileTree.tsx` keyboard handlers)

Active only when the last pointer interaction landed inside the Files pane and the pane is visible (`pointerEngaged` guard, not `document.activeElement`, because WebKit does not focus a `<button>` on click):

- `Cmd/Ctrl+C` — copies selection to in-app clipboard.
- `Cmd/Ctrl+X` — cuts selection.
- `Cmd/Ctrl+V` — pastes.

The `V` key handles two channels in order:

1. Native `paste` DOM event (only path that can also carry OS files from Finder/Explorer); handled first.
2. 100 ms keydown fallback timer (`PASTE_EVENT_FALLBACK_MS`) for a webview that never dispatches the `paste` event.

One paste per keystroke: deduped by `PASTE_DEDUPE_MS` (150 ms), because the desktop (Wails / WKWebView) shell delivers a single `Cmd+V` through two routes (native Edit-menu role + the key's default action) and would otherwise double-paste.

The gate also checks that focus is inside the Files pane (`hidden` / `offsetParent` check) and skips editable targets (`input`, `textarea`, `select`, `.contenteditable`, `.monaco-editor`, `.xterm`) so typing in a file editor is never intercepted.

## OS-file paste (`clipboardData.files`)

Files copied in Finder / Explorer arrive through the native `paste` event's `clipboardData.files` (never through the in-app clipboard). Behaviour:

- Uploaded one at a time into the resolved paste destination.
- Persistent progress notice: `"Uploading 2/5 to src/…"`, then `"Added 2 files to src/"` on completion.
- A copied **folder** arrives as a file-less directory entry the browser cannot read; refused with `"Folders can't be pasted — copy the files inside instead"`.
- The in-app clipboard wins over OS files only while the window never lost focus since the copy (`window.blur` marks the in-app clipboard stale — user switched to another app).
- Uploads are sequential (not parallel) and are aborted (`AbortController`) when the project changes.

This behaviour is implemented against the standard native `paste` event and the `clipboardData.files` contract; the web tests synthesize that contract in jsdom. It has **not** been verified in the desktop shell (Wails / WKWebView): a real Finder copy must be pasted into a rebuilt desktop app to confirm WKWebView populates `clipboardData.files`. If it does not, OS-file paste is unavailable on desktop only — the in-app cut/copy/paste, context menus, toolbar button and keyboard shortcuts are unaffected, since they use the in-app clipboard. Tracked in `TODO.md` under "Open: Files-tab paste follow-ups (2026-10-27)".

See the server endpoint below for how the bytes are received.

## Server endpoint (`internal/server/handler_fs_upload.go`; route in `server.go`)

`POST /api/fs/upload?project=&dest_dir=&name=&host=` — raw file body (`application/octet-stream`), never JSON (pasted files can be binary; `PUT /api/files/content` takes a JSON string, so that path is not reused here).

Validation (`validUploadName`):

- Bare basename only; rejects path separators (`/` `\`), `.` / `..`, NUL / control chars, `: * ? " < > |`, trailing dot / space, Windows device names (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`), and names > 255 bytes.
- Unicode is **not** normalized (the OS handles APFS / NTFS equivalence; `O_CREATE | O_EXCL` prevents a collision from clobbering an existing file).

Write rules:

- `dest_dir` must exist, be inside the project, be outside `.git`, and be a real directory (not a file).
- The file is created with `os.O_CREATE | os.O_EXCL` — never overwrites. Name uniqueness is Finder-style (`uniqueDest`). A deliberate note: `os.Rename` would silently overwrite; there is no stage-then-rename path, which is the safety trade-off.
- Local cap: `fsUploadMaxBytes` (512 MiB, overridable in tests). Remote cap: `fsUploadRemoteMaxBytes` (100 MiB — lower because the bytes cross an ssh stdin pipe and occupy a long-running exec slot).
- Oversize → HTTP 413; the partial file is removed (`os.Remove`) so nothing leaks.

### Remote upload (ssh stdin)

For remote projects (`host` set):

- Bytes piped over ssh stdin (`cat > <quoted path>`) via `remoteTargetOf`. The destination must be inside the remote project.
- The remote project root is the one place `remoteUploadDestDir` accepts as a destination; `remoteTargetOf` rejects the root for every other operation on purpose (a `.` target would let delete / rename hit the whole project), which is why the paste-to-root flow uses a separate resolution path.
- Idle timeout: `fsUploadIdleTimeout` (60 s without bytes). Overall backstop: `fsUploadOverallTimeout` (30 min).
- A refusal from the remote long-running exec pool is returned as HTTP 503 + `Retry-After` (capacity, not a broken project).

## Drag-in upload and right-click Download

Both work for local and remote (`host=`) projects; the tree is one component and the same handlers serve both.

### Drag OS files onto the tree (`FileTree.tsx` `onTreeFileDrop`)

- The drop handlers sit on the tree pane (`treePaneRef`) and claim only drags whose `dataTransfer.types` include `"Files"`, so drags of text and tree rows pass through untouched.
- The target is resolved by `dropDestination` from the row markers the tree renders: `data-tree-path` on every row (tree view and columns view), and `data-tree-dir` on folder rows. A folder row takes the files, a file row sends them beside itself (its parent), and empty space or the search results land in the browsed root.
- Uploads reuse `uploadFiles` (the paste path): sequential, one progress label, aborted by a project switch, and the same `POST /api/fs/upload` route, so the remote branch and the name rules are shared.
- A dropped folder is refused with "Folders can't be added — drop the files inside instead". The folder check (`transferHasFolder`) is the same one paste uses.
- While a file drag is over the pane, the pane gets a `ring-accent` outline (`fileDragOver`).

### Right-click Download (`FileTree.tsx` menus, `api.fsDownload`)

- A **Download** item appears on file rows in both the tree view and the columns view, and only on files. It runs on the effective selection (`effectivePaths`), so a multi-selection downloads each file in turn; a folder in that selection fails on its own entry and is reported, and the rest still save.
- Files are fetched one at a time (WebKit allows about six connections per origin). The client fetches a Blob with auth headers and saves it via `saveBlobAsFile` (`web/src/lib/saveBlob.ts`). The object URL is revoked on a 10 s timer, as in `csvExport.ts` and `PdfViewer.tsx`.
- The progress label reuses the upload progress line (`Downloading 1/3…`); the outcome notice is `Downloaded 2 files` or `Downloaded 0 files — <name>: <error>`.

### Server endpoint (`internal/server/handler_fs_download.go`; route in `server.go`)

`GET /api/fs/download?project=&path=&host=` returns one project file as `application/octet-stream` with `Content-Disposition: attachment` (an ASCII fallback plus RFC 5987 `filename*`), `Content-Length`, `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.

- It is deliberately **not** `GET /api/files/raw`. That endpoint's extension allowlist and per-type caps exist for previews and must not be widened into a general file download.
- Containment is the tree's existing rule: `mutationProjectDir` plus `resolveFSPath(..., "read")` locally (symlinks resolved, `.git` excluded), and `remoteWorkFor` plus `remoteTargetOf(..., "read")` remotely. Only regular files are served; a directory is a 400.
- Access matches `/api/files/content`, which returns any readable project file. There is **no secret-file gate** on either route, so a download grants nothing the editor does not already show. If `.env`-style files should be gated, gate both routes together.
- Local cap: `fsDownloadMaxBytes` (512 MiB), streamed with `io.Copy`, so the cap bounds time, not memory. Over the cap is 413.
- Remote cap: `fsDownloadRemoteMaxBytes` (100 MiB, the upload cap). The size is stat'ed first (`remoteFileSize`, shared with `remoteReadFileCapped`) so Content-Length is exact and an oversized file is refused before any bytes move. The bytes then stream from `cat -- <quoted path>` through `runBounded(..., remoteExecLongRunning)`, bounded by a 60 s idle timer (`fsDownloadIdleTimeout`) and a 30 min overall backstop (`fsDownloadOverallTimeout`). `remoteReadFile` is NOT used: it base64-buffers the whole file in memory.
- Headers are written on the first byte (`fsDownloadStream.start`), so a failure before any data still answers with a JSON status: a capacity refusal is 503 with `Retry-After`, a dead transport is 502, anything else is 500. A failure after bytes have gone out cannot change the status; it is logged and the short body tells the client.

## Tests

Download tests: `internal/server/handler_fs_download_test.go` (local stream and headers, the Content-Disposition encoding table, directory refusal, traversal and symlink-out refusal, missing file, over-cap 413, route registered through the real mux, remote stream over the fake-ssh shim, remote empty file, remote directory and traversal refusal, remote over-cap 413).

Web: `web/src/components/Files/FileTree.download.test.tsx` (`dropDestination` and `transferHasFolder`; Download on files but not folders, in both views; the download notice and failure notice; drops into a folder row, a file row, and empty space; folder refusal; non-file drags ignored); `web/src/api/client.fsDownload.test.ts` (query args, host threading, error surfacing).

Upload tests (paste and drop share this path): `internal/server/handler_fs_upload_test.go` (raw bytes, subdirectory, never-overwrites, unsafe names table, out-of-project dest, `.git` dest, file-as-dest, 413 + no partial file, route registered through the real mux, remote stdin over the fake-ssh shim).

Web: `web/src/components/Files/FileTree.clipboard.test.tsx` (destination rules including Windows separators, all four affordances, keyboard targets, dedupe, hidden-pane and editable-field guards, OS-file upload including folder refusal and the blur rule); `web/src/api/client.fsUpload.test.ts` (raw `File` body, `octet-stream` content type, `host` threading, server error surfacing).

## Known limits worth recording

- Pasted **text** (not files) is not written anywhere by this feature — only files from the OS clipboard are handled.
- Pasted and dropped **folders** are refused explicitly; only files can be pasted or dropped.
- Download has no folder or zip support: a directory is refused (400), so a folder has to be opened and its files downloaded one by one.
- Download carries no secret-file gate (see the endpoint notes above); a change to that policy must cover `/api/files/content` and `/api/fs/download` together.
- Pasted files do **not** appear in the Changes-tab snapshot store (`internal/snapshot`) — that tab lists agent-authored edits with undo, so a user-pasted file is not considered an agent mutation and has no undo entry there.
- The clipboard-to-notice mapping (`pasteDestination` / `pasteDestinationLabel`) does not expose line-level anchors; the cited paths (`FileTree.tsx`, `handler_fs_upload.go`, `handler_fs_upload_test.go`) are the canonical references.
