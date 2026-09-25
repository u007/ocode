# Phase 03 — Per-file conflict resolution endpoint

Phase 3 of the web Git tab conflict/operation-recovery work, tracked from
`.opencode/plans/2026-09-25-git-conflicts-and-operations/INDEX.md`.

## What this phase delivers

A new endpoint that resolves one conflicted path three ways: keep our side,
keep their side, or mark the file resolved as the user has already edited it.
The response is a refreshed workspace so the Git tab updates from one round
trip.

## Context you need

This phase adds a handler to `internal/server/handler_git_conflicts.go` and
registers it in `internal/server/server.go`, where the other git routes are
declared around lines 249–267, each wrapped in the server's auth middleware
and paired with the handler method on `*Handler`.

It depends on phase 2: the conflicted-file list, including the ours/theirs
presence booleans, must already be in the status. The handler re-detects
rather than trusting the request body, so a stale panel cannot resolve
something that is no longer a conflict.

Existing local conventions to follow, all in
`internal/server/handler_git_actions.go`:

- `gitDirForMutation` (around line 205) validates the target project from the
  `project` query parameter and normalizes to the repository toplevel via
  `rev-parse --show-toplevel`, so a nested project path cannot broaden the
  operation. Use it first, exactly as the other mutation handlers do.
- Reuse the local pathspec validation already used by stage, unstage and
  discard for the paths in the request. Do not invent a second, laxer check.
- Always place `--` between a subcommand and a caller-supplied path.
- Wrap the mutation in `gitexec.WithLockRetry`, which is the package's
  mechanism for turning a momentary `.git/index.lock` loss into a success
  rather than a user-visible error.
- Return the refreshed `GitWorkspace` on success, matching the existing
  mutation handlers.

## Request and behavior

The body carries a repo-relative `path` and a `resolution` of `ours`,
`theirs`, or `mark`.

**Ours and theirs.** The chosen side normally maps to `git checkout --ours` or
`git checkout --theirs` followed by `git add`. But when the chosen side's
index stage does not exist — meaning that side is a deletion — `checkout`
cannot be used, and git reports that the path has no such version. In that
case run `git rm` on the path instead, which is the correct way to accept a
deletion and mark the file resolved. This distinction must be driven by the
detected stage presence, never by matching git's error text.

**Mark resolved.** Before staging anything, read the working-tree file and
refuse the request if it still contains conflict markers. Only a line that
begins with the opening marker followed by a space, or a line that begins
with the closing marker, counts. A line consisting only of the separator
run does **not** count: that sequence is a Markdown or reStructuredText
setext heading underline and an ASCII rule, and matching it would refuse
perfectly ordinary files. On refusal, answer 400 with a message naming the
file, and stage nothing.

A path that is not currently unmerged is not an error the user can act on;
answer 409 so the web can reload rather than pretend the resolution happened.

## Environment hardening

The new mutation helper must set `GIT_LITERAL_PATHSPECS=1` in addition to the
environment `gitexec.Env()` provides. `gitexec.Env()`
(`internal/gitexec/gitexec.go`, around lines 55–65) only forces
`GIT_OPTIONAL_LOCKS=0`; without a literal-pathspec setting, a path such as
`:(top)file` would be interpreted as pathspec magic rather than a filename.
Scope this variable to the new helper only — changing `gitexec.Env()` itself
would alter the behavior of every existing git call in the product.

## Verified findings this phase relies on

- **VERIFIED.** A modify/delete conflict yields an all-zero object id for the
  deleted stage in the `-z` unmerged record, and `git ls-files -u` lists only
  stages 1 and 2. The stage-presence signal this phase branches on is real.
- **VERIFIED.** The conflicted file's on-disk content is the conflict-marked
  merge, so reading it to check for leftover markers is meaningful and does
  not need any extra plumbing.
- **ASSUMED** (documented git behavior; `git checkout` is denied by this
  environment's permission rules so it could not be exercised here): that
  `git checkout --ours` on a path with no stage-2 entry fails with a "does
  not have our version" style error. The design deliberately does not depend
  on that error text — it checks stage presence up front — so this assumption
  affects only the wording of a fallback, not correctness.

## Tests first

New test file `internal/server/handler_git_conflict_resolve_test.go`, using
the existing temp-repository helper style already used by
`internal/server/handler_git_test.go`. Every test must fail loudly if its
repository fixture cannot be built.

- A content conflict: resolving ours leaves our content in the working tree
  and staged, and the path leaves the conflicts list; likewise for theirs.
- A modify/delete conflict: choosing the deleted side removes the file and
  clears the conflict; choosing the surviving side restores the modified
  content. This is the case that fails without the `git rm` branch.
- Mark resolved is refused with 400 while markers remain, stages nothing,
  and leaves the file conflicted; after the test rewrites the file without
  markers it succeeds.
- A Markdown file whose content contains a setext-heading underline made of
  the separator run is accepted by mark resolved. This is the regression guard
  for the false positive described above.
- A path containing a space resolves correctly.
- A path shaped like pathspec magic is treated as a literal filename, proving
  `GIT_LITERAL_PATHSPECS=1` is in effect.
- Resolving a path that is not conflicted returns 409.
- An unknown `resolution` value returns 400.

## Verification

- `go test ./internal/server/ -run 'ConflictResolve|GitConflict' -count=1`
- `go test ./internal/server/ -count=1`
- `gofmt -l internal/server/`
- `go vet ./internal/server/`
- `go build ./...`
- Mutation-check: remove the `git rm` branch and confirm the modify/delete
  test goes red; drop `GIT_LITERAL_PATHSPECS` and confirm the pathspec-magic
  test goes red; make the marker check match the separator line and confirm
  the Markdown test goes red.

## Risks and open questions

- This endpoint is destructive by nature: accepting a side overwrites working
  -tree content. The web confirms through its own action affordance, but the
  server has no undo. The existing discard endpoint in the same file sets the
  precedent for how destructive git actions are exposed, and this endpoint
  must be treated as at least as dangerous — no shortcut that bypasses the
  unmerged check is acceptable.
- Do not add a `git reset` fallback if `checkout` fails. Resetting paths
  requires inspecting the diff first, and a reset would silently discard more
  than the user asked for.
- Never use `git stash` to preserve content before a resolution.
