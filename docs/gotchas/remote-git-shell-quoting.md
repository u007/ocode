---
type: Gotcha
title: Remote git commit/stash messages must be shell-quoted (remoteGitCommand contract)
description: Free-form git messages passed to remoteGitCommand must be shell-quoted by the caller; pathspecs from remoteSafeSpec must not be.
tags:
  - remote
  - git
  - shell
  - security
  - quoting
timestamp: 2026-09-22T08:48:55Z
---
## Symptom

Remote `git commit -m` or `git stash push -m` fails with `error: pathspec 'review' did not match any file(s) known to git` over SSH/WSL, even though the message is a single phrase. Worse, a message containing `;` or `$(...)` executes arbitrary commands on the host — a remote command-injection hole.

## Root cause

`remoteGitCommand(dir, args...)` in `internal/server/handler_remote_work.go` builds a POSIX shell command line:

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

Pathspecs produced by `remoteSafeSpec` are already metachar-free and **must not** be pre-quoted — double-quoting breaks rev resolution (e.g. `remote.ShellQuote(rev)+"^{commit}"` would literalize the `^{commit}` suffix).

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

## See also

- `gotchas/git-ext-transport-auto-allow-bypass.md` — a **distinct** injection surface (agent permission allowlist vs. the server's own git command builder). Different layer, same class of lesson: never trust raw strings in command construction.
