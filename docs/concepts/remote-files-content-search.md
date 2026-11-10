---
type: Concept
title: 'Remote (SSH/WSL) Files-tab content search: one grep invocation'
description: 'How the remote Files-tab content search works: GET /api/files/search and /api/files/search/stream over ?host= run one remote grep, chain AND keywords through pipes, search the directory by basename to avoid the root-prune bug, emit --include before --exclude, strip the basename prefix from paths, and mask grep''s exit status behind head. Records the two 2026-11-10 grep gotchas and the honest limitations.'
tags:
  - remote
  - ssh
  - wsl
  - file search
  - content search
  - grep
  - server
  - web
timestamp: 2026-11-10T02:53:12Z
---
# Remote (SSH/WSL) Files-tab content search: one grep invocation

## What this page covers

For a remote project (`?host=`), the Files-tab content search does **real content
grep on the remote host over SSH/WSL** — it is not the local filesystem walk
(`searchFiles`) and not the remote tree walk (`remoteWalkTree`). Both routes
dispatch on `hostParam(r)`:

| Route | Local handler | Remote handler |
| --- | --- | --- |
| `GET /api/files/search` | `HandleFileSearch` | `remoteFileSearch` (`handler_remote_files.go:823`) |
| `GET /api/files/search/stream` | `HandleFileSearchStream` | `remoteFileSearchStream` (`handler_remote_files.go:883`) |

Routes: `internal/server/server.go:452-453`; the `?host=` dispatch is at
`handler_files.go:711` and `handler_search_stream.go:36`. Both remote handlers call
`remoteSearchFiles` (`handler_remote_files.go:43`), which builds and runs exactly one
shell command via `remoteSearchCommand` (`handler_remote_files.go:79`) and parses its
output with `parseRemoteSearchOutput` (`handler_remote_files.go:151`).

## Shape: one grep, keyword AND via pipes

`remoteSearchCommand` builds a single POSIX-shell command:

- `grep -rIn` (recursive, skip binary, line numbers).
- `-i` only when matching case-insensitively (`handler_remote_files.go:99-101`).
- `-w` for whole-word (`:102-104`).
- **AND keywords chain through pipes.** grep ORs multiple `-e` patterns in one
  invocation, so `k1 AND k2` is `grep -e k1 | grep -e k2`,
  extended by `extraKw` (`:133-142`, `:184-190`). Each pipe stage re-applies `-i`
  and `-w`.
- **Regex mode is one `-E -e <pattern>`** (`:105-109`); keyword mode is `-e <kw>`.
- `| head -n maxSearchResults+1` bounds output (`:143`). The extra row is the
  "capped" signal; the cap is then applied **again in memory** at
  `maxSearchResults = 5000` (`handler_files.go:279`; check at
  `handler_remote_files.go:171-174`).
- Each returned line's text is clamped to **2000 runes**
  (`handler_remote_files.go:175-178`).

The **stream** endpoint runs the whole grep first, then emits a single `result`
batch followed by `done` (`handler_remote_files.go:930-936`) — there is no
incremental progress for a remote search. A grep failure emits an `error` frame
then `done` (`:924-927`); an empty query emits `done` with total 0 (`:917-922`).

## Gotcha 1: search the directory by basename, never `cd <root> && grep … .`

The generated command is:

```sh
cd <parent> && [ -d <base> ] || exit 3; grep -rIn … <base> 2>/dev/null | … | head -n N
```

(`handler_remote_files.go:97-132`.)

It must **not** be `cd <root> && grep -rIn … .`. grep matches `--exclude-dir`
patterns against the **search root as well as its children**, and the `.*`
pattern that skips dot-directories also matches the operand `.`. grep therefore
prunes the root itself and the search silently returns **zero matches for every
query**. Verified on GNU grep 3.11 and BSD grep 2.6.0
(`handler_remote_files.go:70-78`). Naming the directory instead
(`cd <parent> && grep … <base>`) keeps the hidden-dir exclusion while the root,
now a plain basename, never matches a dot pattern.

This was the 2026-11-10 bug: "remote content search shows No matching content for
every query". `TestRemoteSearchCommandFindsMatchesAndSkipsHiddenDirs` is its
regression pin.

A `.` or `/` root cannot be named without re-introducing the bug, so
`remoteSearchCommand` refuses it with an error (`:84-90`); registered remote
projects are absolute or `~/`-rooted.

## Gotcha 2: emit every `--include` before any `--exclude`

GNU grep only honours `--include` when the first file filter is `--include`.
With an `--exclude` first it searches **every** file and silently drops the
extension filter (verified GNU grep 3.11; BSD grep tolerates either order, so the
unit test pins the string order). The command therefore writes all `--include`
flags before any `--exclude` (`handler_remote_files.go:110-118`), pinned by
`TestRemoteSearchCommandEmitsIncludesBeforeExcludes`.

## Output paths are `<basename>/rel`

Because the operand is a basename, grep's output paths are `<basename>/rel`.
`parseRemoteSearchOutput` strips the `<basename>/` prefix (`:154`, `:179`), so the
SPA's `onOpenFile(r.path, activeRoot)` receives project-relative paths.

## Default exclusions (when `includeIgnored` is false)

Mirroring the local walk's skips (`handler_remote_files.go:119-128`):

- `--exclude-dir` for `.*`, `node_modules`, `vendor`, `target`, `.history`
- `--exclude='.*'` for dotfiles

Passing `includeIgnored` disables all of them, exactly like the local
`includeIgnored` path.

## Known limitations (stated honestly)

- **`-w` is ASCII.** grep's `-w` uses `[A-Za-z0-9_]` word characters while the
  local matcher uses Unicode `\pL\pN_` boundaries (`handler_files.go:321`), so
  CJK/accented edge cases may differ (`handler_remote_files.go:29-31`).
- **Regex is validated as Go RE2 but executed as POSIX ERE.** RE2-only constructs
  (`\p{...}`, `(?:...)`, `(?i:...)`) pass `parseSearchParams` then fail remotely
  (`:36-39`).
- **grep's exit status is masked.** Output is piped through `head`, so the
  pipeline status is head's; grep's exit 2 (bad pattern/option) becomes an **empty
  result** (`:40-42`). Only the failed `cd`/root check (exit 3, emitted before the
  pipe) reaches the caller as an error.
- **Do not claim grep stderr surfaces.** The grep invocation redirects
  `2>/dev/null` (`:132`); the old doc-comment claim that stderr is surfaced was
  wrong.

## Tests

`internal/server/handler_remote_search_test.go` runs the generated script through
`/bin/sh` and asserts real matching/pruning behaviour (skipped on Windows):

- `TestRemoteSearchCommandFindsMatchesAndSkipsHiddenDirs` — matches, hidden-dir
  exclusion, extension filter.
- `TestRemoteSearchCommandReportsMissingRoot` — stale path exits 3.
- `TestRemoteSearchCommandRejectsDegenerateRoot` — `.` and `/` refused.
- `TestRemoteSearchCommandKeepsTildeRoot` — `~/…` expands `$HOME`, still searches
  the basename.
- `TestRemoteSearchCommandEmitsIncludesBeforeExcludes` — filter ordering.

## Cross-links

- [File tree walk: node budget, symlink budget, and truncation](./file-tree-walk-budget.md) — the remote **tree walk** (`remoteWalkTree`), which explicitly scopes itself away from content search.
- [File search result ordering: shortest path first](./file-search-shortest-path-ordering.md) — content search is a deliberate ordering carve-out.
