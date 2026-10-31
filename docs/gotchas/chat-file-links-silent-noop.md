---
type: Gotcha
title: Chat file links with relative or ~ hrefs silently did nothing
description: Chat markdown links to ~/ files and relative paths were prevented and then dropped, so clicks looked broken with no error; ~/ links now open in the Files tab, while relative and root-relative hrefs stay no-op by design.
resource: web/src/components/common/MarkdownLink.tsx
tags: [gotcha, web, chat, files, links]
timestamp: 2026-10-31T16:44:05Z
status: active
---

# Chat file links with relative or ~ hrefs silently did nothing

## Symptom

Clicking a file link in an agent chat message did nothing. Examples:
`[diagram](~/.agent/diagrams/x.html)` or `[foo](internal/x/foo.go)`. No error,
no dialog, no tab.

## Cause

`MarkdownLink` prevents the default navigation on every link. Only absolute
http(s) hrefs go on to `openExternalURL`. Any other href was prevented and then
dropped, so the failure was invisible to the user.

## Current behaviour

- `~` and `~/...` hrefs (`isHomeFileHref`) call `openFile`
  (`web/src/lib/fileLinks.tsx`), which dispatches `OPEN_FILE_EVENT`
  (`ocode:open-file`). `App.tsx` opens the file in the Files tab through the
  editor path. The server expands the `~`; see
  [File Editor Grants](../concepts/file-editor-grants.md).
- Relative paths, root-relative (`/x`) and protocol-relative (`//host`) hrefs
  still do nothing. That is deliberate.

## Rules for future edits

- Every href shape handled by `MarkdownLink` must map explicitly to external,
  file, or no-op. The fall-through is silent, so a new shape added without a
  branch fails with no signal.
- Never send a local path to `openExternalURL`. The desktop open-external bridge
  is http(s)-only on purpose; a `~` path there would hand a local file to the OS
  browser. The page-to-shell `ocode:open-external:` message carries the same
  http(s)-only rule; see [desktop webview has no Wails events](desktop-shell-webview-no-wails-events.md).
- The web client cannot expand `~` because it cannot read `$HOME`. Expansion
  stays server-side.

## Verification

`web/src/components/common/MarkdownLink.test.tsx`: "opens a home-directory file
link in the Files tab, not the OS browser".
