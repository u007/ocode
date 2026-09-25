# Phase 04 — Operation recovery endpoint (continue / abort / skip)

Phase 4 of the web Git tab conflict/operation-recovery work, tracked from
`.opencode/plans/2026-09-25-git-conflicts-and-operations/INDEX.md`.

## What this phase delivers

A new endpoint that drives a halted git operation forward or unwinds it:
continue, abort, or skip. For a bisect, which has no notion of "continue", it
instead exposes good, bad, skip and reset.

## Context you need

This phase adds a second handler to `internal/server/handler_git_conflicts.go`
and registers it beside the phase 3 route in
`internal/server/server.go` (the git routes are around lines 249–267).

It depends on phase 1, which defines the operation kinds and the state files
behind them, and on phase 2, which puts the current operation into
`GitStatus`. The web tab will read that operation and offer the matching
buttons, so the set of kinds and the set of allowed actions must match exactly.

The motivating failure: `POST /api/git/pull`
(`internal/server/handler_git_actions.go`, `HandleGitPull`, around line 296)
runs a bare `git pull`. When the pull stops on a conflict, the endpoint
returns 500 with git's output and the user has no way to continue from the
web UI. This endpoint is the way out.

## Request shape and the stale guard

The body carries an `action` and the `kind` the client last saw. The server
**re-detects** the current operation and ignores the client's `kind` except as
a staleness check: if the re-detected kind differs from the one the client
believed, answer 409. This is what prevents an Abort issued from a panel that
has been open since before a rebase from aborting a *different* operation that
started since. If no operation is in progress at all, also answer 409.

The action is never a caller-supplied git subcommand. It is one of a small
closed vocabulary that the server maps onto git itself, so a request can never
smuggle an arbitrary argument into a git invocation.

## The action matrix

| kind | continue | abort | skip | extra |
| --- | --- | --- | --- | --- |
| merge | merge --continue | merge --abort | not supported | |
| rebase | rebase --continue | rebase --abort | rebase --skip | |
| rebase-interactive | rebase --continue | rebase --abort | rebase --skip | |
| am | am --continue | am --abort | am --skip | |
| cherry-pick | cherry-pick --continue | cherry-pick --abort | cherry-pick --skip | |
| revert | revert --continue | revert --abort | revert --skip | |
| bisect | not supported | bisect reset | bisect skip | good → bisect good, bad → bisect bad |

A merge genuinely has no skip. A bisect genuinely has no continue: the way to
advance one is to mark the current commit good or bad, which is why those two
actions exist alongside skip and reset. Any action that is not in the row for
the detected kind is a 400 — silently ignoring it would leave the user
clicking a button that does nothing.

## Running the command

`--continue` invokes commit hooks and may open an editor, so the environment
must disable both, or the HTTP request hangs forever waiting on a terminal
nobody can see. The existing `runGitNetwork`
(`internal/server/handler_git_actions.go`, around line 225) already sets
`GIT_EDITOR=true`, `GIT_SEQUENCE_EDITOR=true`, `GIT_MERGE_AUTOEDIT=no` and
`GIT_TERMINAL_PROMPT=0`; follow that pattern. Add `GIT_LITERAL_PATHSPECS=1`
for the same reason as phase 3, and keep `gitexec.Env()`'s
`GIT_OPTIONAL_LOCKS=0`.

Because continue runs hooks, the command's combined output matters. Capture it
with the same combined-output approach `runGitNetwork` uses — never plain
output capture, which is how git errors get lost. Fold a failure's output into
the returned error so the web shows git's own words. Wrap the run in
`gitexec.WithLockRetry`: a halted repository is exactly the situation where a
user's terminal or editor is also touching the same index.

On success, return the refreshed workspace together with the captured output,
so the web can surface hook output as a notice rather than dropping it.

## Verified findings this phase relies on

- **VERIFIED.** A real merge conflict leaves `MERGE_HEAD` and `MERGE_MSG`
  present, so a merge can be produced and driven end to end in a test with
  `git merge` alone, which this environment permits.
- **VERIFIED.** `git rebase` is denied by this environment's permission
  rules, so a rebase cannot be driven end to end here. A real interactive or
  rebase-driven `git am` cannot be produced either.
- **VERIFIED.** A rebase state directory can be created by hand in a linked
  worktree's git directory and is then detected correctly, which is how
  phase 1 tests the rebase shapes. The same technique works for building
  `am` and bisect states.
- **ASSUMED** (documented git behavior): that `git rebase --continue`,
  `git rebase --abort`, `git rebase --skip`, `git am --*`,
  `git cherry-pick --*` and `git revert --*` accept exactly these flags.
  These are stable, long-standing git interfaces, but they are pinned by the
  mapping test below rather than by a live rebase in this environment.

## Tests first

New test file `internal/server/handler_git_operation_test.go`. Use the merge
path for real end-to-end coverage and synthesized state for the kinds that
cannot be produced here. No test may silently skip because a fixture could not
be built.

- Real merge: force a conflict with `git merge`, assert the status reports a
  merge, then abort and assert the working tree returns to the pre-merge
  content and the operation is gone.
- Command mapping: a table over every kind and every action asserting the
  exact git argv the server would run, and a 400 for each unsupported
  combination — including skip on a merge and continue on a bisect. This is
  what pins the rebase flags that cannot be exercised live here.
- Real merge with a conflict resolved by hand: stage the resolved file, then
  continue, and assert the merge completes and the operation clears. This is
  the closest live analogue of "pull, resolve, continue".
- Stale guard: a request whose `kind` does not match the detected operation
  returns 409 and runs nothing.
- No operation in progress returns 409.
- A synthesized bisect state accepts good, bad, skip and reset and rejects
  continue.
- The editor-suppressing environment is asserted to reach the child process,
  because a regression there is a hang in production rather than a test
  failure.

## Verification

- `go test ./internal/server/ -run 'GitOperation|GitConflict' -count=1`
- `go test ./internal/server/ -count=1`
- `gofmt -l internal/server/`
- `go vet ./internal/server/`
- `go build ./...`
- Mutation-check: remove one row of the action matrix and confirm its 400
  test goes red; remove the stale-kind guard and confirm the 409 test goes
  red; drop `GIT_EDITOR` from the environment and confirm the environment
  assertion goes red.

## Implementation decisions (recorded 2026-09-25)

Three places where the built behaviour differs from, or refines, this plan.

- **The continue-with-conflicts pre-check is kept, and it is the only
  reinterpretation.** This plan's Risks section says a failing continue must
  be "surfaced verbatim rather than retried or reinterpreted." A 409 up front
  when conflicts remain does reinterpret one specific case. It is retained
  because git's own message ("Committing is not possible because you have
  unmerged files") is prose the user has to decode, while the pre-check names
  the action to take, and the web UI already disables Continue in exactly this
  state. **Every other failure is surfaced verbatim** — no retry, no rewriting
  of git's output. Skip is deliberately not pre-checked, because skipping a
  conflicted step is the point of skip.

- **A git refusal maps to 409, not 500.** When git itself refuses (a hook vetoed
  the commit, the state moved under us) the request was legitimate and git gave
  a legitimate answer; 500 would misreport that as a server fault. The response
  body is git's combined output, verbatim. A missing `kind`, an unknown action
  and an unsupported (kind, action) pair are 400; no operation in progress and
  a stale kind are 409.

- **The operation command is bounded by a 60s timeout** and runs under the
  request context. `--continue` and `--skip` run commit hooks and may invoke
  credential helpers, so a child that never exits would otherwise hang the HTTP
  request forever. This matches the existing network-action timeout.

- **The bisect tests use a real `git bisect start`, not hand-built state.** The
  plan suggested synthesizing `BISECT_START`/`BISECT_LOG`. A real bisect was
  verified to work in this environment (unlike `git rebase` and
  `git checkout`, which are denied) and produces a genuine `BISECT_START`, so
  the tests use it. The synthesized rebase state remains necessary for
  `rebase`/`am` in Phase 01, which cannot be produced here at all.

## Risks and open questions

- **Abort is destructive.** It discards the in-progress merge or rebase
  state. It must never be reachable without the web's confirmation affordance,
  and it must never be the endpoint's response to an unrecognized kind.
- **Continue can fail legitimately** — for example while conflicts remain, or
  because a hook failed. That is git's answer and must be surfaced verbatim
  rather than retried or reinterpreted.
- Because rebase and am cannot be driven end to end in this environment,
  their live behavior is covered only by the command-mapping test. If a real
  rebase ever behaves differently from the documented flags, this test suite
  will not have caught it. Note that limitation rather than assuming the
  mapping is complete.
- Never use `git stash` or a reset as part of any of these actions.
