---
type: Gotcha
title: Remote git commit/stash messages must be shell-quoted (remoteGitCommand contract)
description: Free-form git messages and git pathspecs passed to remoteGitCommand must be shell-quoted by the caller; remote lookups against path sets keep the raw spec. Remote file operations quote at the command site and validate with remoteSpecCore.
tags:
  - remote
  - git
  - shell
  - security
  - quoting
timestamp: 2026-11-03T13:00:59Z
---
# Remote git commit/stash messages must be shell-quoted (remoteGitCommand contract)

## Symptom

Remote `git commit -m` or `git stash push -m` fails with `error: pathspec 'review' did not match any file(s) known to git` over SSH/WSL, even though the message is a single phrase. Worse, a message containing `;` or `$(...)` executes arbitrary commands on the host — a remote command-injection hole.

## Root cause

`remoteGitCommand(dir, args...)` in `internal/server/handler_remote_work.go:674` builds a POSIX shell command line:

```
cd "<dir>" && GIT_OPTIONAL_LOCKS=0 git <args...>
```

It does **not** quote its arguments — callers are responsible for quoting any free-form strings. `HandleGitCommit` (internal/server/handler_git_actions.go) passed the user-supplied commit message raw: `args := []string{"commit", "-m", message}`. The remote shell word-split it, and git parsed the tail as trailing pathspecs. The same defect lived in `stashPushArgs` for `stash push -m`.

Local mutations are unaffected: they call `gitRunInDir(dir, args...)`, which passes argv directly to `os/exec` (no shell), so no quoting is needed or wanted.

## Fix

- **Remote commit** now passes `remote.ShellQuote(message)` so a multi-word or metachar-containing message arrives at git as a single shell word.
- **Stash**: a shared `stashPushArgsQuoted(req, specs, quoteMessage bool)` helper shells-quotes the message **only for the remote transport** (call site in `HandleGitCommit`-equivalent remote path). The local exec path via `stashPushArgs` keeps the raw message — quoting locally would embed literal `"` characters into the argument.
- Both variants share the helper; the `quoteMessage` flag selects the behavior.

## Rule for future callers

**ANY** free-form argument (commit message, stash message, a ref, a branch name) passed to `remoteGitCommand` **must** be wrapped with `remote.ShellQuote` by the caller.

**Pathspecs are quoted too** (2026-10-09). A git pathspec is validated by `remoteGitSpec` (`handler_remote_work.go:711`), which
allows shell metacharacters, so the quoting at the command site is the guard. Quote with
`remoteQuoteSpecs` (a list) or `remoteQuoteSpecPath` (one spec) only where the spec becomes a
command argument. Keep the raw spec where it is compared against a path set, such as
`remoteRestoreStashPaths`, because a quoted value would not match. Do not pre-quote a rev that
already carries a suffix: `remote.ShellQuote(rev)+"^{commit}"` is correct, but quoting the suffix
too would literalize `^{commit}`.

**File operations are NOT different** (corrected 2026-11-03). Every remote file-op command
site shell-quotes its absolute path with `remote.ShellQuotePath` (exported at
`internal/remote/execcmd.go:27`, implemented at `internal/remote/multiplex.go:144`), so shell
metacharacters are literal. `remoteTargetOf` (`handler_remote_fs.go:48`) validates with
`remoteSpecCore` (`handler_remote_work.go:692`) only — non-empty, repo-relative, no control
characters — while `remoteRelCheck` still enforces project containment and the `.git` refusal.
Ordinary filenames containing spaces, parentheses, brackets, quotes, `$`, `&` and `#` are
therefore allowed. Git specs stay stricter (`remoteGitSpec`) because git glob/pathspec semantics
cannot be neutralized by quoting.

## 2026-11-03 — file operations no longer reject shell-metacharacter names

**Symptom:** a remote download failed with
`Downloaded 0 files — NMB_Cloud Risk Assessement Form (AIS).xlsx: path contains unsupported characters`.

**Root cause:** the deleted `remoteSafeSpec` rejected any path containing any of
`'"` + backtick + `$&|<>\!*?[](){}#:` plus control characters, and every remote file operation
reached it through `remoteTargetOf`. The rule assumed file-op callers did not quote; they do.

**Fix:** `remoteTargetOf` validates with `remoteSpecCore` only, and `fsRenameRemote`'s `new_name`
uses `remoteSpecCore` plus a slash check. Containment (`.git` refusal included) is unchanged.

**Why it is safe:** every command site quotes the absolute path at the point it becomes shell
text — download `cat --` (`handler_fs_download.go:134`), `cp -a --`, `mv --`, `rm -rf --`, create,
duplicate and upload destination. `remoteSaveFileContent` already worked this way, which is what
made the download failure an inconsistency.

**Tests:** `TestRemoteFileOpsAllowShellMetacharacterNames` (download + delete of
`NMB_Cloud Risk Assessement Form (AIS).xlsx`, `Q1 [draft] {final}.xlsx`, `it's a $5 file.txt`,
`cost&profit#1.txt`), `TestRemoteRenameToShellMetacharacterName`, and
`TestRemoteDownloadCanaryNameStaysLiteral` (a `payloadName` canary plus `assertNoCanaryFiles`).
Mutation check: removing `remote.ShellQuotePath` from the download's `cat --` command makes the
canary create `PWNED`/`PWNED2`/`PWNED3`. `TestRemoteSpecCore` replaces `TestRemoteSafeSpec`.

## Tests

`internal/server/handler_git_message_quote_test.go` — all mutation-verified (each fails against the pre-fix code by temporary revert):

| Test | What it pins |
|------|-------------|
| `TestRemoteGitCommitShellQuotesMessage` | Commit message arrives quoted on the remote command line |
| `TestRemoteGitCommitQuotesShellMetacharacters` | Message containing `;`/`$(...)` does **not** execute on the host |
| `TestRemoteGitStashShellQuotesMessage` | Stash message arrives quoted on the remote command line |
| `TestStashPushArgsQuoteOnlyForRemote` | Quoting applied only for the remote transport, not local |
| `TestStashPushArgsOmitEmptyMessage` | Empty stash message is omitted, not passed as `-m ""` |

These run through the fake-SSH shim (`installFakeSSH` in `handler_remote_git_test.go`), which translates `ssh` → `/bin/sh -c`, so real word-splitting is exercised — not a mock assertion.

Pathspec quoting is pinned by `internal/server/handler_remote_git_injection_test.go`
(`TestRemoteGitCanaryPathsStayLiteral`). A canary filename full of metacharacters goes through
stage, unstage, discard, commit, stash push and apply, hunk stage, and the diff path filter.
After each call the test fails if a `PWNED*` file appeared in the repo or the test directory.
Mutation check: removing `remoteQuoteSpecs` from the remote stage argv fails the test with
`shell injection: canary command created [...PWNED...]`. Conflict resolve is not covered.

## See also

- `gotchas/git-ext-transport-auto-allow-bypass.md` — a **distinct** injection surface (agent permission allowlist vs. the server's own git command builder). Different layer, same class of lesson: never trust raw strings in command construction.