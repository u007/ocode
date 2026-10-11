---
type: Gotcha
title: 'Git tab "git add failed: pathspec ''"…"'' did not match" — C-quoted names from non-`-z` listings'
description: 'The web/desktop Git tab could not stage files whose names contain non-ASCII bytes (an em dash, an accented letter), locally or over SSH/WSL. Listings were read without -z, so git C-quoted the name and the quoted form was passed back to git add; remote command args were also joined unquoted, so a space split the name. Every listing that feeds a path back into git must use -z or decode, and every spec that becomes a command argument must be shell-quoted.'
resource: internal/server/handler_git.go; internal/server/handler_remote_git.go; internal/server/handler_remote_git_actions.go; internal/server/handler_git_quoting_test.go; internal/server/handler_remote_git_quoting_test.go
tags:
  - git
  - porcelain
  - c-quoting
  - stage
  - remote-ssh
  - web
  - desktop
  - gotcha
timestamp: 2026-10-09T14:30:00Z
---
# Git tab stage failed on non-ASCII filenames — C-quoted names from non-`-z` listings

## Symptom

On the desktop/web Git tab, staging an untracked file whose name contains a non-ASCII
character fails with:

```
git add failed: exit status 128: fatal: pathspec '"docs/cv/James Tan \342\200\224 Technical Lead & AI Specialist.pdf"' did not match any files
```

The quotes and `\342\200\224` octal escapes are git's C-quoting, not part of the name.

## Why

Git C-quotes any path containing bytes ≥ 0x80, `"`, `\` or control characters when
`core.quotepath` is at its default. A listing read without `-z` therefore returns the quoted
form. The UI sends that string back on stage, and the handler runs `git add -- <string>`,
which matches nothing. Side effects: the untracked patch preview ran
`git diff --no-index /dev/null <quoted>` and came back empty, and the staged/changed lists
showed the quoted name.

Tracked names had the same bug through `git diff --name-only` (no `-z`), and through the
`diff --git "a/…" "b/…"` header that `parseUnifiedDiff` split on `" b/"`.

Remote projects had a second, independent bug: `remoteGitCommand` joins its arguments raw,
so a name with a space reached the remote shell as several words (`pathspec 'Résumé' did not
match`). That applied to any pathspec, not only non-ASCII ones.

## Rules

1. **Every local listing whose output becomes a path argument uses `-z`.** Status and
   workspace code: `git status --porcelain -z -u` and `git diff --name-only -z [--cached]`.
   Split on NUL (`\x00`), never on newline.
2. **Use `gitRunInDirRaw` (or `runRaw` inside `gitStatusForDir`) for `-z` output, not
   `gitRunInDir`.** `gitRunInDir` trims whitespace, and a leading space is the status code of
   the first record in `-z` porcelain.
3. **Skip the original-path record after a rename or copy.** In `-z` porcelain, an `R` or `C`
   in either column is followed by the original path as its own NUL record with no status
   prefix. `untrackedPathsFromStatusZ` (`handler_git.go:794`) does this, so an original path
   such as `old?y.txt` is never read as an untracked entry.
4. **Decode `diff --git` headers when git quoted them.** `diffHeaderPath`
   (`handler_git.go:818`) passes the quoted `"b/…"` form through `gitignoreUnquotePath`
   (`handler_git_ignore.go:256`). That decoder is byte-exact; `strconv.Unquote` would turn
   invalid UTF-8 into U+FFFD. The plain form still splits on `" b/"`. This also fixes remote
   diffs, which share `parseUnifiedDiff`.
5. **Remote non-`-z` listings are decoded per line.** Remote status and workspace use
   `remoteUnquoteNames` (`handler_remote_git.go:180`) and `gitignoreUnquotePath` on the
   untracked lines. Decoding is safe on non-`-z` output because git always quotes a name
   containing `"`, so an unquoted line never starts with `"`. The `-z` section stays
   base64-wrapped; raw paths there can contain the `\x1e` section separator.
6. **Every spec that becomes a remote command argument is shell-quoted, and that quoting is the
   injection guard.** Use `remoteQuoteSpecs` (`handler_remote_git.go:332`) for a list and
   `remoteQuoteSpecPath` for one. Git pathspecs are validated by `remoteGitSpec`
   (`handler_remote_work.go`), which allows shell metacharacters and refuses only what quoting
   cannot neutralize. File operations keep the strict `remoteSafeSpec`. Keep the raw spec wherever
   it is looked up in a path set (for example `remoteRestoreStashPaths`).
7. **A remote `diff --no-index /dev/null` needs `|| true`.** `remoteRun` discards stdout on any
   nonzero exit, and `diff --no-index` exits 1 for every untracked file. Without the masking,
   every remote untracked patch came back empty.
8. **Remote `ls-tree` uses `-z`.** `remotePathSetForRev` builds the path set that stash restore
   looks specs up in. Without `-z`, a name with quotes or non-ASCII bytes came back C-quoted and
   never matched, so stash apply reported "path not found in stash".

## Still open

- **Remote pathspecs with `*`, `?`, `[`, a leading `:` or a drive prefix are refused.** These are
  git glob and pathspec syntax, which shell quoting does not neutralize. Metacharacters such as
  `& ' " ; $ # ( )` are accepted since 2026-10-09, so `Technical Lead & AI.pdf` lists and stages
  over SSH. Remote conflict resolve has the same refusals (see `TODO.md`).
- **`nonEmptyLines` trims each listed line.** A name with a trailing space loses it on the
  remote side. Local `-z` output is not affected.
- **`gitignoreUnquotePath` on the ignore endpoint.** Local paths are now raw, so a filename
  that literally begins and ends with `"` is mis-decoded when "Add to .gitignore" sends it.
  Rare. Removing the decode and `TestGitIgnoreUnquotesCPath` needs the user's sign-off.

## Regression tests

`internal/server/handler_git_quoting_test.go` (local):

- `TestGitStatusListsNonASCIIPathsUnquoted` — untracked and modified names come back raw.
- `TestGitWorkspaceDiffsKeepNonASCIIPathsUnquoted` — workspace paths and untracked patches.
- `TestGitStageNonASCIIUntrackedPath` — stage by raw name returns 200 and the file is staged.
- `TestGitStatusZUntrackedPaths` — `-z` parser, including rename original-path records.
- `TestParseUnifiedDiffDecodesQuotedHeader` — quoted `diff --git` header decodes.

`internal/server/handler_remote_git_quoting_test.go` (SSH over the fake-ssh harness):

- `TestRemoteGitStatusListsNonASCIIPathsUnquoted` — remote status lists raw names.
- `TestRemoteGitWorkspaceListsNonASCIIPathsUnquoted` — remote workspace lists both names, with patches.
- `TestRemoteGitStageNonASCIIUntrackedPath` — remote stage of a name containing spaces returns 200.
- `TestRemoteGitWorkspaceListsAndStagesQuotedName` — a name containing `"` is listed and stageable.
- `TestRemoteGitSpecValidator` — the accepted and refused pathspec sets.

`internal/server/handler_remote_git_injection_test.go` (canary matrix):

- `TestRemoteGitCanaryPathsStayLiteral` — two canaries (one quote-bearing, one that executes if
  spliced raw) through stage, unstage, discard, commit, stash push and apply, hunk stage, and the
  diff path filter. Fails on any `PWNED*` file. Conflict resolve is not covered.
