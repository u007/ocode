# Phase 02 — Status surface: conflicted files and the operation

Phase 2 of the web Git tab conflict/operation-recovery work, tracked from
`.opencode/plans/2026-09-25-git-conflicts-and-operations/INDEX.md`.

## What this phase delivers

`GitStatus` gains a conflicted-file list and an optional operation, and the
existing file lists stop double-counting conflicted paths.

## Context you need

`internal/server/handler_git.go` defines `GitStatus` (around line 55) and
builds it in `gitStatusForDir` (around line 170). That function runs a fixed
set of probes under one shared 10-second deadline: the abbreviated branch,
`git diff --name-only --cached`, `git diff --name-only`, `git status
--porcelain -u` (used only to append untracked `??` entries), and
`git status --porcelain=v2 --branch` for the ahead/behind counts.
`gitWorkspaceForDir` (around line 347) embeds a `GitStatus` in
`GitWorkspace`, so anything added to the status is automatically available to
the Git tab through the existing `GET /api/git/workspace` call. **No
workspace-level field is needed** — the web reads conflicts and the operation
off the embedded status.

The status struct is also serialized by the background emitter
(`internal/server/emitters.go`, around line 175) which polls every *viewed*
project on an interval and publishes a `git_status` bus event only when the
marshaled status string changes. That is why the operation must be an optional
pointer field rather than a set of always-present fields: an idle repository
must continue to marshal byte-identically, or every idle project would emit
on every poll.

Two web consumers read the file lists: `web/src/components/Layout/TopTabs.tsx`
around lines 98–99 for the session Git tab badge, and
`web/src/lib/projectGitCounts.ts` around lines 92–93 for the project sidebar
badge. They are fixed in the same change, described in phase 7.

## The new fields

- A **conflict** records one unmerged path: the repo-relative path, git's
  two-character porcelain status code for the entry, and two booleans saying
  whether the "ours" (index stage 2) and "theirs" (index stage 3) entries
  exist. A false side means that side is a deletion, which matters because
  `git checkout --ours/--theirs` cannot be used for a deleted side. The list
  is always serialized as an empty array, never null, because the web reads
  its length directly — matching the existing convention for the staged and
  changed lists.
- An optional **operation** pointer carrying the kind, label, step and total
  produced by the phase 1 parser. Absent (not null-with-zeroes) when idle.

## How conflicts are detected

Use one new probe, `git status --porcelain=v2 -z`, and read its unmerged
records. A `-z` unmerged record contains the status code plus the object ids
for all three index stages, and an all-zero object id means that stage does
not exist. That single command therefore yields both the code and the
ours/theirs presence — no separate `git ls-files -u` call is needed, and paths
arrive unquoted.

Do not reuse the existing non-`-z` porcelain probe for this. Adding `-z` to
it would change the shape of the branch-header records that the ahead/behind
parse depends on.

## Correcting the counts

- Remove every conflicted path from both `StagedFiles` and `ChangedFiles`, so
  a conflicted file is listed exactly once, in the new conflicts list.
- Widen `HasChanges` so it is also true when the conflicts list is
  non-empty. A repository stopped on a conflict does have uncommitted work,
  and the sidebar's non-repo/empty logic keys off this flag.
- Deduplicate defensively. Git can legitimately emit the same path more than
  once in a `--name-only` listing for an unmerged path, so the lists must not
  assume uniqueness.

## Operation detection wiring

One additional probe, `git rev-parse --absolute-git-dir`, gives the git
directory; stat and size-boundedly read the fixed probe list relative to it,
then hand the results to the phase 1 parser. Keep the whole thing inside the
existing shared deadline so a wedged repository still cannot pin the caller.
The remote path is not in this phase.

## Verified findings this phase relies on

All checked by building throwaway repositories in `/tmp` and inspecting
their output.

- **VERIFIED.** A conflicted path appears in `git diff --name-only`
  **twice** and in `git diff --name-only --cached` once. So today a single
  conflicted file is counted three times across the two lists, and appears
  twice inside `changed_files` alone. This is an existing bug that the new
  filtering fixes; the plan would be wrong without the filtering.
- **VERIFIED.** The unmerged form of `git diff` uses a `diff --cc` header,
  not `diff --git`. `parseUnifiedDiff` in the same file keys on `diff --git`
  and therefore cannot parse a combined diff, which is why a conflicted file
  contributes no patch to the staged or unstaged panes today. This phase does
  not change that parser and does not attempt a preview; phase 6 opens the
  file in the editor instead.
- **VERIFIED.** A modify/delete conflict (`UD`) reports the "theirs" stage
  object id as all zeros, and `git ls-files -u` shows only stages 1 and 2.
  This confirms the all-zero rule that the ours/theirs booleans rely on.
- **VERIFIED.** During a plain merge conflict `git rev-parse --abbrev-ref
  HEAD` still returns the real branch name, so a merge does not disturb the
  branch display. The rebase case is the one that detaches HEAD, and the
  phase 1 parser already takes the branch from `head-name` for that reason.
- **ASSUMED** for the codes not directly reproduced: that the same all-zero
  rule identifies a deleted side for the remaining unmerged codes
  (`AU`, `UA`, `DU`, `DD`), and that `UU` and `AA` have all three stages
  present. Cover every code with a table test over synthesized `u` records so
  the assumption is pinned rather than left implicit.

## Tests first

New test file `internal/server/handler_git_conflicts_status_test.go`, or
additions to `internal/server/handler_git_test.go`.

- A table test over synthesized `-z` unmerged records: all seven codes map to
  the right code, and the ours/theirs booleans come out right for the
  all-zero and all-present cases.
- Real-repository tests in a temp repo, each failing loudly if setup fails:
  a content conflict asserts the file is in the conflicts list with the right
  code, is absent from both file lists, and that `HasChanges` is true; a
  modify/delete conflict asserts the deleted side's boolean is false; a clean
  repository and a non-repository both assert an empty conflicts list and a
  nil operation, with the operation absent from the JSON when idle.
- A test asserting a conflicted path that also appears in both listings is
  counted once.
- A test asserting the marshaled status of an idle repository is unchanged by
  this work, so the emitter does not start firing spuriously.

## Verification

- `go test ./internal/server/ -run 'GitConflict|GitStatus' -count=1`
- `go test ./internal/server/ -count=1`
- `gofmt -l internal/server/`
- `go vet ./internal/server/`
- `go build ./...`
- Mutation-check: skip the filtering of conflicted paths out of the two file
  lists and confirm the "counted once" test goes red; invert one
  ours/theirs boolean and confirm its test goes red.

## Risks and open questions

- The extra `git status --porcelain=v2 -z` probe and the
  `rev-parse --absolute-git-dir` probe add two processes to every status
  computation, and the emitter runs that for every viewed project. Both are
  read-only and share the existing 10-second deadline, so the cost is small,
  but if the emitter's timings regress, the fix is to fold both probes into a
  single shell invocation the way the remote path already batches its probes.
- Because conflicted paths leave the two file lists, any other consumer that
  inferred "unresolved" from those lists changes meaning. The two known web
  consumers are fixed in phase 7; a repository-wide search for
  `staged_files` and `changed_files` is part of that phase.
