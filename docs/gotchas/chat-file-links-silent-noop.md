---
type: Gotcha
title: Chat file links with relative or ~ hrefs silently did nothing
description: Chat file links to ~/ files and relative paths were prevented and then dropped, so clicks looked broken with no error; authored ~/ hrefs now open in the Files tab, and the prose auto-linkifier keeps the ~ inside its token instead of matching only the root part after it.
resource: web/src/components/common/MarkdownLink.tsx, web/src/lib/fileLinks.tsx
tags: [gotcha, web, chat, files, links]
timestamp: 2026-11-09T17:31:34Z
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

- `~` and `~/...` hrefs (`isHomeFileHref`,
  `web/src/components/common/MarkdownLink.tsx:42`) call `openFile`
  (`web/src/lib/fileLinks.tsx`), which dispatches `OPEN_FILE_EVENT`
  (`ocode:open-file`). `App.tsx` opens the file in the Files tab through the
  editor path. The server expands the `~`; see
  [File Editor Grants](../concepts/file-editor-grants.md).
- Relative paths, root-relative (`/x`) and protocol-relative (`//host`) hrefs
  still do nothing. That is deliberate.

## Prose auto-links vs markdown hrefs

The section above is about **authored** `[x](~/...)` hrefs. A path an agent
merely *mentions* in prose or inline code takes a different path: the rehype
plugin and `linkifyPlainText` (`web/src/lib/fileLinks.tsx`) auto-linkify it with
one regex, `PATH_SOURCE` (`fileLinks.tsx:50`). Fixing one does not fix the
other, and this asymmetry went undocumented long enough to be reported twice.

### The pre-fix failure

`~/Downloads/word-editor-sample/` in prose linked as `/Downloads/word-editor-sample`.
`PATH_SOURCE` had no `~` branch and `GUARD` (`fileLinks.tsx:29`) did not include
`~`, so the match started at the slash *after* the tilde. The tilde stayed as
plain text, the trailing slash was dropped, and the click opened a ROOT path the
server refuses — `fallbackQuickOpen` (`App.tsx:822`) then offered a grant dialog
pre-filled with a `/Downloads/...` that does not exist.

### Token shapes now recognised

`HOME_SOURCE` (`fileLinks.tsx:38`) is the first `PATH_SOURCE` alternative:

```
~/(?:[\w.\-]+/)*[\w.\-]*[\w\-]/?(?::\d+(?::\d+)?)?
```

- `~/a`, `~/a/b.go`, `~/a/`, `~/a/b.go:42` and `~/a/b.go:42:7` match; ONE link
  spans the `~` and the optional trailing slash. The slash matters: it is what
  makes the token look like a directory, and a directory grant opens the folder
  in the file tree.
- `~user/x` is deliberately NOT matched. `projects.ExpandHome` leaves it alone,
  so it would resolve against the server's cwd — a silently wrong path. The `~`
  in `GUARD` (`(?<![\w:/.~-])`) is what also stops the generic extension branch
  from matching the `user/x.go` tail.
- A bare `~`, a bare `~/`, and a mid-word `a~/b` do not link.
- A sentence-ending `.` is not part of the token: `~/Downloads/notes.` links
  `~/Downloads/notes`, because the final segment (`SEG_TAIL`,
  `[\w.\-]*[\w\-]`) must end in a word char or `-`, not a dot. Dots stay legal
  *inside* a segment, so `~/a.b.c` and `~/.env` stay whole. The root/relative
  branch applies the same rule, so `/etc/hosts.` links `/etc/hosts`.

**Why the tilde must stay inside the token:** the SPA cannot expand `~` (no
`$HOME`), so a token starting after it hands the server a path the agent never
meant. Keep `~` in the guard, or the home branch's own slash re-matches and the
ROOT-path bug returns.

### Resolution chain

1. The SPA sends the token verbatim (`openFile` → `OPEN_FILE_EVENT`); it never
   expands `~`.
2. The server owning that `$HOME` expands it: `projects.ExpandHome` in
   `resolveProjectFilePathScoped` (`internal/server/handler_files.go:1003`) and
   in `canonicalGrantRoot` (`internal/server/handler_file_grants.go:66`).
3. An expanded path outside every project root is refused, so the editor-only
   grant prompt runs — grants never widen the agent's scope.
4. The tab opens **relative to the granted root** (`relativeToRoot` /
   `openOutsideProject`), never from the typed path.

Grant lifetime and the editor/agent scope split:
[File Editor Grants](../concepts/file-editor-grants.md).

Contrast with the TUI: `internal/tui/pathlink.go:38` already had `~` in its token
character class and `resolveExistingFile` (`:75`) expands it. TUI links are
stat-verified (a directory or a missing path yields no link region at all), so
that side never showed this bug.

### Verification (prose auto-links)

`web/src/lib/fileLinks.test.tsx:127-218`: trailing slash keeps the `~`; with and
without `:line`; inside markdown prose; the exact reported sentence without
swallowing the `;`; a sentence-ending `.` dropped from both a `~/` token and a
`/` token; dots kept inside a segment (`~/a.b.c`, `~/.env`); no link for bare
`~` / `~user/x` / mid-word `~`; and the absolute/relative regression guards.
Server-side expansion:
`TestFileGrant_HomeTildePathExpandsThenContained`
(`internal/server/handler_file_grants_test.go:455`).

## Rules for future edits

- Every href shape handled by `MarkdownLink` must map explicitly to external,
  file, or no-op. The fall-through is silent, so a new shape added without a
  branch fails with no signal.
- Auto-linkified prose and markdown hrefs are separate paths. A new `~` shape
  must be added to `HOME_SOURCE` *and* kept out of the generic branches by
  `GUARD`; never "fix" a link by dropping the tilde or the trailing slash, which
  turns it back into a root path the server refuses.
- Never send a local path to `openExternalURL`. The desktop open-external bridge
  is http(s)-only on purpose; a `~` path there would hand a local file to the OS
  browser. The page-to-shell `ocode:open-external:` message carries the same
  http(s)-only rule; see [desktop webview has no Wails events](desktop-shell-webview-no-wails-events.md).
- The web client cannot expand `~` because it cannot read `$HOME`. Expansion
  stays server-side.

## Verification

`web/src/components/common/MarkdownLink.test.tsx`: "opens a home-directory file
link in the Files tab, not the OS browser".
