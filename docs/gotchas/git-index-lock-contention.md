---
type: Gotcha
title: Git index.lock contention in ocode git subprocesses
description: ocode git subprocesses clash with user git commands over .git/index.lock, causing stage failures and HTTP 500s
timestamp: 2026-09-20T17:00:24Z
---
Symptom

The web or desktop Git tab Stage button returned HTTP 500 with body: git add failed: exit status 128: fatal: Unable to create '.git/index.lock': File exists. Another git process seems to be running in this repository, or the lock file may be stale.

That prefix is produced only by server git endpoints (handler_git_actions.go gitStageLocal, handler_git_hunks.go, handler_remote_git_actions.go); the TUI says stage failed instead of returning 500. A user seeing a 500 on stage in the web UI while the TUI says stage failed is a strong signal of this issue.

Cause one: constant optional-lock contention

git status and git diff refresh the index through an optional lock, and ocode runs both constantly. The git-status emitter probes every viewed project every 10 seconds (internal/server/emitters.go). The web Git tab polls on open and on refresh. The file-tree badge probe runs git status --short per request (handler_files.go).

The TUI re-badges on a ticker (internal/tui/files_model.go). None of these set GIT_OPTIONAL_LOCKS, so ocode was a permanent lock contender against the user's own git commands.

Any git operation the user runs in a checked-out repo could race against ocode's probes and lose. The probes are frequent and relentless, so the window for contention is always open.

Cause two: SIGKILL-stranded lock files

gitStatusForDir probes run under a 10 second deadline via exec.CommandContext, which SIGKILLs git if it runs too long. A probe killed while holding the optional index lock can strand .git/index.lock on disk, breaking the next git add even after ocode itself has moved on.

The lock file is not automatically cleared because the process died while holding it. The user then sees failures that have nothing to do with their own git operation. This is particularly insidious because the failure appears to be about the user's git when it is really about ocode's probe dying mid-hold.

Cause three: no retry on the first attempt

gitRunInDir in the server and gitRunInDir/gitRunTimeout in the TUI failed on the first attempt and surfaced the error immediately. There was no retry logic at all.

So any short-lived holder of the lock became a permanent red error in the UI. The user's terminal running a commit, an editor refreshing git status, a second ocode tab, or the agent's own git add could all be the holder. A brief delay would have let the holder release and the operation succeed.

The failure was premature and the user had no way to recover except retry manually.

Retry approach now in place

- ocode retries only on genuine lock contention errors, not on all failures.
- Retry budget is about 900ms total with delays of 100, 150, 250, and 400ms.
- Other errors are returned untouched with git's own stderr preserved.
- On exhaustion the message notes ocode attempted the command N times and that a lock still present may be stale and can be deleted manually.
- Retrying is safe because a failed lock acquisition means git never started the operation.

Fix: internal/gitexec package

New package internal/gitexec centralizes the safe way to run git from ocode. Env() returns the process environment plus GIT_OPTIONAL_LOCKS=0, dropping any inherited value. Git documents this as the background-process setting, the environment form of --no-optional-locks.

Mandatory locks are unaffected and verified with git 2.54.0 (git add still exits 128 with a pre-existing .git/index.lock present, because mandatory locks ignore the variable). LockHeld(err) matches the strings "another git process seems to be running" or "unable to create" plus "index.lock", case-insensitively.

The match is deliberately narrow so a pathspec error naming index.lock.bak does not qualify as a lock conflict. Only genuine lock contention triggers a retry. WithLockRetry(fn) retries only on LockHeld. Retrying is safe because a failed lock acquisition means git never started the operation, so there is no side effect to repeat.

Wiring

Server git endpoints use the shared gitBinary variable with environment plus retry where appropriate. gitRunInDir has environment plus retry. gitStatusForDir probes have environment only. gitApplyInDir has environment plus retry. gitDiffRawInDir has environment only. The file-tree badge probe has environment only.

TUI git commands use runGit, gitRunInDir and gitRunTimeout with environment plus retry and a new gitBinary variable. Both TUI file-tree badge probes have environment only. internal/agent/context.go hasUnstagedChangesAt uses the environment. internal/commandctx gitRun uses the environment.

Remote git commands built by remoteGitCommand carry a leading GIT_OPTIONAL_LOCKS=0 assignment. Remote mutations go through remoteGitMutation with retry.

Tests

All mutation verified: disabling the retry or dropping the environment variable from any wired call site makes the tests fail. internal/gitexec/gitexec_test.go tests the package directly.

internal/server/git_lock_retry_test.go covers server-side retry. internal/server/remote_git_lock_retry_test.go covers remote retry paths. internal/tui/git_lock_retry_test.go covers TUI paths.

Deliberate residuals

A stale .git/index.lock is never auto-deleted by ocode. Git creates it with O_CREAT or O_EXCL, so the advisory flock helper in internal/filelock cannot coordinate with it. ocode retries for about 900ms then reports the error to the user.

internal/tool/repo.go runGit (fresh clone directories) and the orchestrator worktree operations were left alone and do not use the retry path. These were not changed because they operate in directories where git lock contention is not a concern.

Cross reference

docs/gotchas/project-endpoint-isolation.md covers the 2026-09-18 incident where an index.lock held by a dead process stalled the shared git emitter: that is the timeout and fan-out half of the story. This doc is the contention and retry half. Together they explain both why the shared emitter became unresponsive and why individual git operations in the UI failed with misleading errors. Also see the AGENTS.md section titled Git subprocesses: gitexec, never a bare exec.Command.
