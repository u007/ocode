# Phase 01 — Operation-state detection (shared parser)

Phase 1 of the web Git tab conflict/operation-recovery work, tracked from
`.opencode/plans/2026-09-25-git-conflicts-and-operations/INDEX.md`.

## What this phase delivers

A pure, transport-neutral parser that answers one question: *is this
repository in the middle of a git operation, and which one?* It must be
usable unchanged for a local repository and for a remote SSH/WSL project, so
that remote parity is structural rather than a second implementation that can
drift.

## Context you need

The web Git tab (`web/src/components/Git/GitPanel.tsx`) currently has no
concept of a halted operation. `POST /api/git/pull`
(`internal/server/handler_git_actions.go`, `HandleGitPull`) runs a bare
`git pull` and returns a 500 with git's combined output when it halts on a
conflict, leaving the user no way forward from the UI. The server's
`GitStatus` struct (`internal/server/handler_git.go`, around line 55) carries
only branch, staged/changed file lists, a repo flag, and ahead/behind counts.

Git records an in-progress operation purely as files and directories inside
the repository's git directory. This phase reads those files and nothing else.
Do **not** parse the long-format `git status` output — it is human prose and
is locale-dependent.

## Approach

Create a new file `internal/server/handler_git_conflicts.go` holding both
this phase's parser and the later phases' handlers, so the conflict feature
stays in one place instead of growing `handler_git.go` further.

Two small types, named for what they carry:

- A **state entry** records one probed path inside the git directory: its name
  relative to that directory (for example `rebase-merge/msgnum`), whether it
  is a directory, and, for a small text file, its contents.
- The **operation** value describes a halted operation: a kind, a
  human-facing label, a current step, and a total. A nil result means the
  repository is idle.

The parser takes a slice of state entries and returns an operation or nil.
It performs no I/O, which is what makes it exhaustively testable.

## The probe list

A fixed list of names, relative to the git directory, must be probed:
`MERGE_HEAD`, `MERGE_MSG`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`,
`BISECT_START`, the `sequencer` directory with its `todo` and `head` files,
the `rebase-merge` directory with its `interactive`, `msgnum`, `end`,
`head-name` and `onto` files, and the `rebase-apply` directory with its
`applying`, `next`, `last`, `head-name` and `onto` files.

Both transports probe exactly this list, so the parser is the only place that
knows what a rebase looks like.

## Precedence

More specific states win, in this order:

1. `rebase-merge` present → a rebase, reported as *interactive* when its
   `interactive` file exists. Progress comes from `msgnum` and `end`; the
   branch name comes from `head-name`, with the `refs/heads/` prefix stripped;
   the target comes from `onto`.
2. `rebase-apply` present → *am* when its `applying` file exists, otherwise a
   plain rebase. Progress comes from `next` and `last`; branch and target
   from `head-name` and `onto`.
3. `CHERRY_PICK_HEAD` present → cherry-pick, labelled with the short sha.
4. `REVERT_HEAD` present → revert, labelled with the short sha.
5. A `sequencer` directory with neither head marker → a multi-commit
   cherry-pick or revert. Classify by the first token of `sequencer/todo`:
   a leading `revert` means revert, anything else means cherry-pick.
6. `MERGE_HEAD` present → merge, labelled from the first line of `MERGE_MSG`.
7. `BISECT_START` present → bisect.

`BISECT_START` is the marker git's own status logic checks. Do not use
`BISECT_LOG`, which can outlive a completed bisect.

The label is human-facing, for example "Rebasing main onto 1a2b3c4 (3/7)".
During a rebase the branch is detached, so the branch name must come from
`head-name` rather than from `git rev-parse --abbrev-ref HEAD`.

## Verified findings this phase relies on

Each was checked by building throwaway repositories in `/tmp` with
`git init`, `git switch`, `git merge`, `git commit` and inspecting the result.

- **VERIFIED.** A plain merge conflict creates `MERGE_HEAD` and `MERGE_MSG`
  and none of `rebase-merge`, `rebase-apply`, `CHERRY_PICK_HEAD`,
  `REVERT_HEAD`, `BISECT_START` or `sequencer`. The precedence order above is
  therefore safe, and a merge state is unambiguous.
- **VERIFIED.** `git rev-parse --absolute-git-dir` inside a linked worktree
  returns that worktree's own git directory (the main repository's
  `.git/worktrees/<name>`), and a rebase state created there is invisible to
  the main worktree. So resolving the git directory once with
  `--absolute-git-dir` and joining the probe names onto it is correct for
  linked worktrees, and keeps detection to a single git process.
- **VERIFIED.** `git rev-parse --git-path <a> --git-path <b> --git-path <c>`
  prints one path per flag, so batching is possible that way too. Both
  approaches cost one process; this phase uses `--absolute-git-dir` because
  it is already verified for the linked-worktree case above.
- **ASSUMED** (documented git behavior, not verified here because
  `git rebase` is denied by this environment's permission rules): a rebase
  leaves `rebase-merge` populated as described, and the sequencer todo token
  distinguishes revert from pick.

## Linked worktrees must be covered

`BISECT_START`, `sequencer/`, `rebase-merge/` and `rebase-apply/` are all
per-worktree, exactly like the rebase state verified above. The table tests
must therefore include a case that builds state under a linked worktree's
git directory and asserts it is detected, and a case that asserts state under
a linked worktree is **not** reported for the main worktree. A test that only
exercises a single `.git` directory would miss a wrong-directory bug.

## Tests first

Write the failing tests before the parser. New test file
`internal/server/handler_git_conflicts_test.go`.

- A table test over synthesized state entries covering: empty input returns
  nil; each of the seven kinds is detected from its own markers;
  `rebase-merge` plus `interactive` is reported as interactive; `rebase-apply`
  with `applying` is *am* and without it is a rebase; a `sequencer/todo`
  beginning with `revert` is a revert and a `sequencer/todo` beginning with
  `pick` is a cherry-pick; a rebase reports the expected step and total; the
  `head-name` `refs/heads/` prefix is stripped in the label.
- Precedence cases: `rebase-merge` wins over a co-present `MERGE_HEAD`, and
  `CHERRY_PICK_HEAD` wins over a co-present `sequencer`.
- A real-repository test: create a temp repo, force a genuine conflict with
  `git merge`, and assert the detector reports a merge. This must fail
  loudly if the repo cannot be created — never skip.
- A linked-worktree test as described above, using `git worktree add` and a
  hand-built state directory. This approach was exercised successfully during
  exploration, so it does not need a real rebase.

## Verification

- `go test ./internal/server/ -run 'GitOperation|GitState' -count=1`
- `go test ./internal/server/ -count=1`
- `gofmt -l internal/server/handler_git_conflicts.go`
- `go vet ./internal/server/`
- Mutation-check: remove one precedence rule and one `rebase-apply` versus
  `rebase-merge` distinction, confirm the table test goes red, then restore.

## Risks and open questions

- The `sequencer/todo` token heuristic is the least certain classification
  rule. If a real multi-commit revert does not lead with a `revert` token, the
  UI would label it a cherry-pick; the action commands git runs for continue
  and abort are the same for both, so only the label and the button wording
  would be wrong. Cover it with the table test now and revisit if a live
  multi-commit revert ever mislabels.
- Reading state files must be size-bounded. Git's own state files are small,
  but a cap is still required so a pathological file cannot be slurped.
- Do not log or surface the contents of `MERGE_MSG` beyond its first line.
