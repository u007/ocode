---
type: Concept
title: Files Tab Clipboard (Cut / Copy / Paste)
timestamp: 2026-10-27T08:20:49Z
description: Files-tab clipboard concept doc with desktop WKWebView verification caveat
tags:
  - clipboard
  - files-tab
  - os-file-paste
  - concept
---
---
type: Concept
title: Files Tab Clipboard (Cut / Copy / Paste)
---

# Files tab clipboard (cut / copy / paste)

Shipped feature on the web/desktop Files tab (`web/src/components/Files/FileTree.tsx`). A single `pasteDestination` rule resolves the paste target; every affordance (context menus, toolbar button, keyboard) reports the same resolved destination. This doc covers the workflow, the OS-file upload path (`POST /api/fs/upload`), and the known gaps.

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

## Tests

Server: `internal/server/handler_fs_upload_test.go` (raw bytes, subdirectory, never-overwrites, unsafe names table, out-of-project dest, `.git` dest, file-as-dest, 413 + no partial file, route registered through the real mux, remote stdin over the fake-ssh shim).

Web: `web/src/components/Files/FileTree.clipboard.test.tsx` (destination rules including Windows separators, all four affordances, keyboard targets, dedupe, hidden-pane and editable-field guards, OS-file upload including folder refusal and the blur rule); `web/src/api/client.fsUpload.test.ts` (raw `File` body, `octet-stream` content type, `host` threading, server error surfacing).

## Known limits worth recording

- Pasted **text** (not files) is not written anywhere by this feature — only files from the OS clipboard are handled.
- Pasted **folders** are refused explicitly; only files can be pasted.
- Pasted files do **not** appear in the Changes-tab snapshot store (`internal/snapshot`) — that tab lists agent-authored edits with undo, so a user-pasted file is not considered an agent mutation and has no undo entry there.
- The clipboard-to-notice mapping (`pasteDestination` / `pasteDestinationLabel`) does not expose line-level anchors; the cited paths (`FileTree.tsx`, `handler_fs_upload.go`, `handler_fs_upload_test.go`) are the canonical references.
