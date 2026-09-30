---
type: Gotcha
title: 'A failed git probe is not the same answer as "not a repository" - rev-parse exit 128 hides git-missing and safe.directory faults'
description: 'Gotcha: git rev-parse exits 128 both for a plain non-repo and for a real fault (git missing from PATH, safe.directory / dubious-ownership refusal, unreadable .git), so gitStatusForDir classified on the exit code alone and a server with no git on PATH reported every project as a plain directory with nothing logged. The decision now rests on stderr text, which forces two things: pin LC_ALL=C, and classify on the RAW cmd.Output() error before runRaw wraps it with fmt.Errorf and discards the *exec.ExitError and with it Stderr.'
resource: internal/server/handler_git.go; internal/server/git_status_timeout_test.go; internal/server/emitters.go; internal/server/handler_git_conflicts.go
tags:
  - gotcha
  - git
  - diagnostics
  - server
  - error-handling
  - locale
timestamp: 2026-09-29T11:26:38Z
---

# A failed git probe is not the same answer as "not a repository"

`gitStatusForDir` (`internal/server/handler_git.go`) answers one question with
two very different outcomes, and conflating them is silent:

- **"this directory is not a repository"** — a normal answer. `IsRepo:false`,
  no error, published to the UI.
- **"git could not do its job"** — a fault. Returned as an error carrying git's
  own message, logged, never published as a status.

The old code only distinguished the *timeout*, and returned `IsRepo:false` for
every other `rev-parse` failure. So a server with no `git` on `PATH` reported
every project as a plain directory, and a `safe.directory` refusal looked
identical — with nothing logged either way.

## Why exit codes are not enough

`git rev-parse` exits **128** for both cases:

```
$ cd /tmp/empty && git rev-parse --git-dir
fatal: not a git repository (or any of the parent directories): .git   # exit 128

$ GIT_TEST_ASSUME_DIFFERENT_OWNER=1 git -C ownedrepo rev-parse --git-dir
fatal: detected dubious ownership in repository at '/tmp/ownedrepo'    # exit 128
```

(`GIT_TEST_ASSUME_DIFFERENT_OWNER=1` is git's own test hook for the ownership
check — the only practical way to reproduce the second case without a second
user on the box. The regression test uses it.)

So the decision rests on **stderr text**, which makes two things mandatory:

1. **Pin the locale.** `cmd.Env = append(withoutLCAll(gitexec.Env()), probeCLocale)`
   (`LC_ALL=C`). Without it the marker text varies with the user's locale and
   the classification silently inverts. Reuses the helpers
   `handler_git_conflicts.go` already had for the same reason.
2. **Classify on the RAW error, before it is wrapped.** `runRaw` folds stderr
   into the returned error with `fmt.Errorf("git %s: %s", ..., stderr)` — the
   `%s` verb discards the `*exec.ExitError`, so `errors.As` can no longer reach
   `exitErr.Stderr` afterwards. The classifier therefore runs inside `runRaw`
   and stashes the verdict in `notARepo`.

A non-`*exec.ExitError` (e.g. `exec.ErrNotFound` — git missing) is never a
non-repo answer and is classified as a fault without consulting stderr.

## The marker list must stay narrow

A bare `not a git repository` substring is too loose: git also prints
`fatal: not a git repository: '<path>'` when `GIT_DIR` points at something
broken, which is a real fault. The markers require the
`(or any of the parent directories)` qualifier to tell the benign case from the
broken-`GIT_DIR` one. `gitNotARepoMarkers` documents which spellings are
accepted; extend it only with a real observed message, never by loosening the
match.

## Why returning an error is safe for the UI

The obvious worry is that a fault now becomes a `500` where the client expected
a plain "no repo". Two things prevent that regression:

- Git **mutations** gate on their own `rev-parse --show-toplevel` in
  `gitDirForMutation` and answer `400 "not a git repository"` before ever
  reaching the status probe. A non-repo mutation is unaffected.
- A healthy repository still returns `200` with `IsRepo:true`, pinned by
  `TestGitStatusForDirHealthyRepoUnaffected`. A blanket "rev-parse failed is an
  error" would fail it.

Both the HTTP path (`writeLocalGitError` → `slog.Error`) and the emitter
(`emitters.go`, `slog.Warn` once per *distinct* error, since the poll runs every
10s per viewed project) log the fault. Neither is a silent fallback.

## Regression coverage

`internal/server/git_status_timeout_test.go`:
`TestGitStatusForDirMissingGitIsAnError`,
`TestGitStatusForDirUnreadableRepoIsAnError` (via the ownership hook),
`TestGitStatusForDirHealthyRepoUnaffected`,
`TestGitStatusForDirFaultReachesClientAsError`, plus the pre-existing
`TestGitStatusForDirNonRepoIsNotAnError` and
`TestGitStatusForDirBrokenRepoIsAnError`.

## Related

- `docs/gotchas/git-action-errors-disappear.md` — the other half of git
  diagnostics: making a *user-visible* action error stick rather than being
  wiped by a background refresh.
- CHANGES.md 2026-09-29 — "Remote target hardening".
