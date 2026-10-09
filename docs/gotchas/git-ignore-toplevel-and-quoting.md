---
type: Gotcha
title: '"Add to .gitignore" — toplevel targeting, C-quoting, and append-not-truncate'
description: 'Gotchas behind the web/desktop Git tab''s right-click "Add to .gitignore": entries MUST resolve against the repo toplevel (git status porcelain paths are toplevel-relative, so a nested ?project= subdir double-prefixes), C-quoted porcelain names must be decoded before pattern-building, patterns are anchored/escaped, and the file is appended to (O_APPEND local / read-modify-write remote) rather than rewritten. Also records the latent remoteReadFile MISSING-sentinel bug fixed in the same change.'
resource: internal/server/handler_git_ignore.go; web/src/components/Git/GitPanel.tsx; internal/server/handler_remote_work.go
tags:
  - git
  - gitignore
  - web
  - remote-ssh
  - gotcha
  - porcelain
  - c-quoting
  - append
timestamp: 2026-10-07T04:40:33Z
---
# "Add to .gitignore" — toplevel targeting, C-quoting, append-not-truncate

Shipped feature: a **right-click → "Add to .gitignore"** action on the web/desktop Git
tab's file-row context menu. Backend `POST /api/git/ignore` appends repository-relative
paths to the repo root `.gitignore` and returns the refreshed git status (same contract as
stage/unstage).

The menu wiring is trivial; the load-bearing part is how the path is turned into a
`.gitignore` line. These four rules are each a bug class if violated.

## KEY RULES

1. **Resolve against the repo TOPLEVEL, never a nested `?project=` subdir.**
   `git status --porcelain` reports paths relative to the toplevel. If you resolve them
   against a nested project dir you **double-prefix** (`sub/sub/file`) and write the entry
   to the wrong `.gitignore`. Local gets the toplevel from
   `gitRunInDir(dir, "rev-parse", "--show-toplevel")` (`handler_git_ignore.go:43`); remote
   uses `remoteGitDirForMutation`, which normalizes `work.Path` to the remote toplevel
   (`handler_remote_git_actions.go:93`, normalization at `:107-111`). Regression:
   `TestGitIgnoreNestedProjectUsesToplevel` (`handler_git_ignore_test.go:169`).

2. **Decode git's C-quoting before building the pattern.**
   Porcelain C-quotes names containing spaces, quotes, backslashes or non-ASCII bytes
   (`"sp ace.txt"`, `"caf\303\251.txt"`). `gitignoreUnquotePath`
   (`handler_git_ignore.go:256`) reverses the quoting; without it the pattern would contain
   the literal quotes/octal escapes. The Git tab now sends raw names (its listings are `-z` or
   decoded, see `docs/gotchas/git-status-c-quoted-paths.md`), so this decode is defensive: it
   still mis-decodes a raw name that begins and ends with `"`. Removing it needs the user's
   sign-off. Regression: `TestGitIgnoreUnquotesCPath`
   (`handler_git_ignore_test.go:126`) and `TestGitignoreUnquotePath` (`:276`).

3. **Anchor with a leading `/` and escape glob metacharacters.**
   `gitignoreLineForPath` (`handler_git_ignore.go:222`) prefixes `/` so the pattern is
   pinned to the repo root (a bare path matches at any depth — wrong for toplevel-relative
   input). Once anchored, a leading `#`/`!` is no longer special, so no extra escape is
   needed. It backslash-escapes `\ * ? [ ]` and spaces (`:241-243`), preserves a trailing
   `/` for a collapsed untracked directory (`:246-248`), and rejects newlines / empty / `.`
   (`:226-228`, `:234`). Regression: `TestGitignoreEscapesSpecials`
   (`handler_git_ignore_test.go:90`), `TestGitignoreLineForPath` (`:209`).

4. **Append, never truncate.**
   `buildGitignoreAppend` (`handler_git_ignore.go:188`) dedupes against existing lines
   (trailing `\r` trimmed, `:189-192`), preserves the file's LF-vs-CRLF choice (`:193-196`),
   and inserts a separator when the last existing line has no terminator (`:204-207`).
   Local writes with `os.O_APPEND|os.O_CREATE|os.O_WRONLY` (`:96`) so a concurrent editor
   save is **not clobbered**. Remote has no `O_APPEND` primitive, so it is
   read-modify-write via `remoteReadFile`/`remoteWriteFile` (`:156-169`). A missing
   `.gitignore` is created. Regression: `TestGitIgnoreAppendsUntrackedPath` (`:32`),
   `TestGitIgnoreDedupesExistingEntry` (`:56`), `TestGitIgnoreAddsNewlineWhenMissing`
   (`:72`), `TestBuildGitignoreAppend` (`:248`).

## Frontend gating (a gitignore entry does not untrack a tracked file)

`web/src/components/Git/GitPanel.tsx`:

- `ignoreFiles(targets)` (`:590`) filters to `t.untracked` members only and appends
  `(skipped N tracked)` to the success notice (`:598`); a mixed selection therefore acts on
  the untracked members and tells the user what it skipped.
- `fileMenuItems` (`:638`) adds the item only when `ignoreCount > 0` (`:669-677`) — i.e. it
  is **hidden for a single tracked row** and for any selection with no untracked member.
  Icon: `FileX2` (`:674`, imported at `:17`).
- The **staged pane returns before the ignore item is built** (`:653-666`), so it never
  appears there.
- API call: `api.gitIgnore(paths, projectPath, projectHost)` (`:597`) →
  `web/src/api/client.ts:1828` → `POST /api/git/ignore${projQuery(project, host)}` with body
  `{paths}`.

The **API itself accepts a tracked path** — the frontend is what restricts the menu
(`TestGitIgnoreAcceptsTrackedPath`, `handler_git_ignore_test.go:108`). Do not "fix" the
server to reject tracked paths; the endpoint is a mechanism, the UI is the policy.

Route is registered in `internal/server/server.go:318` behind `s.authMiddleware`, next to
`/api/git/stage` (`:315`). A handler-only test cannot catch a missing/shadowed route
(it presents as an SPA 404), hence `TestGitIgnoreRouteIsRegistered`
(`handler_git_ignore_test.go:293`) drives the **real mux**.

Path containment (`../evil.txt` → 400, never a write outside the repo) is enforced by
`resolveRepoPath` (`internal/server/handler_git_actions.go:59`); remote mirrors it with
`remoteAbsJoin`/`remoteRelCheck`. Regression: `TestGitIgnoreRejectsPathOutsideRepo`
(`handler_git_ignore_test.go:142`).

## Latent bug fixed in the same change: `remoteReadFile` ignored its own MISSING sentinel

`remoteReadFile` (`internal/server/handler_remote_work.go:646`) runs a fixed shell script
that emits the literal `MISSING` when the remote path does not exist (`:622`). The base
function **did not check for it** — a missing remote file fell through to
`"remote read failed: unexpected output"` (`:639`). Its sibling `remoteReadFileCapped`
already had the check (`:678`); only the base function lacked it.

Impact: `remoteFileContent` maps `!found` → `404` (`internal/server/handler_remote_files.go:401-402`),
so a **missing remote file returned HTTP 500 instead of 404**. The `.gitignore`
read-modify-write path is what made it reachable (it calls `remoteReadFile` on a
possibly-absent `.gitignore`). Fixed by adding the `trimmed == "MISSING"` check at
`:631`. Regression: `TestRemoteReadFileMissingReturnsNotFound`
(`handler_git_ignore_test.go:316`), which pins `found=false, err=nil, data=nil`.

## Tests

- Go: `internal/server/handler_git_ignore_test.go` — append/dedupe/newline, escaping,
  C-unquote, tracked-path accepted, path-outside-repo 400, nested-project-uses-toplevel,
  remote over fake SSH (`installFakeSSH` + `newTestHandlerWithRemote`), real-mux route,
  `remoteReadFile` missing→found=false, plus pure unit tests for `gitignoreLineForPath`,
  `buildGitignoreAppend`, `gitignoreUnquotePath`.
- Web: `web/src/components/Git/GitPanel.test.tsx` — add via context menu (`:332`), hidden
  for a tracked row (`:343`), hidden on the staged pane (`:353`).
