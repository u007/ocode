---
type: Gotcha
title: '"git commit failed: exit status 1" with no reason (git explains on stdout, not stderr)'
description: git commit writes its reason on stdout; capturing only stderr hides the explanation
tags:
  - git
  - stderr
  - stdout
  - git-runner
  - error-reporting
timestamp: 2026-09-23T06:16:15Z
---
# "git commit failed: exit status 1" with no reason (git explains on stdout, not stderr)

## Symptom

The web/desktop Git panel (and the TUI status bar) showed a bare `git commit failed: exit status 1` with no explanation.

## Root cause

`gitRunInDir` (`internal/server/handler_git.go`) and the TUI twin `runGit` (`internal/tui/git_model.go`) captured stdout with `cmd.Output()` and, on failure, folded **only stderr** into the error.

Git does **not** always put the reason on stderr. `git commit` with nothing staged writes

> "no changes added to commit" (plus the whole "On branch … / Changes not staged" block)

to **STDOUT**. The reason was therefore discarded and the caller surfaced an opaque `exit status 1`.

## Proof (reproduce)

In a scratch repo with an unstaged edit:

```
git commit -m x 2>/dev/null   # prints the explanation, exits 1
git commit -m x 1>/dev/null   # prints nothing, exits 1
```

## Fix / contract

Both runners fold through one shared helper, `gitexec.WithOutput` (`internal/gitexec/gitexec.go`), which uses the same precedence as the remote path (`remoteRunRaw`, `internal/server/handler_remote_work.go`):

> **on failure → prefer stderr, fall back to stdout, then `err.Error()`.**

So the local path now reports, e.g.:

> `git commit failed: exit status 1: On branch main … no changes added to commit (use "git add" and/or "git commit -a")`

The underlying failure is expected git behaviour when nothing is staged; **the bug was the hidden reason, not the failure**.

## Where the contract applies

- Local `gitRunInDir` (server) and `runGit` (TUI) call `gitexec.WithOutput`; remote `remoteRunRaw` shares the precedence but keeps its own fold (it replaces the error with text for transport classification).
- `git log` outside a repo (reason on stderr) still reports "not a git repository" — unaffected.

## Regression tests

- `TestGitCommitFailureSurfacesStdoutReason` (`internal/server/handler_fs_git_test.go`)
- `TestGitRunInDirSurfacesStdoutReason` (`internal/tui/git_model_test.go`)
- `TestWithOutput` (`internal/gitexec/gitexec_test.go`)

Both mutation-verified: removing the stdout fallback makes them fail with exactly `{"error":"git commit failed: exit status 1"}`.

## Related

- `docs/gotchas/git-action-errors-disappear.md` — covers the **different** TUI stderr-loss bug (a `git status` message disappearing behind long hint strings) and the sticky-error contract.
