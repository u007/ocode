# Phase 07 — Badge totals parity across the three surfaces

Phase 7 of the web Git tab conflict/operation-recovery work, tracked from
`.opencode/plans/2026-09-25-git-conflicts-and-operations/INDEX.md`.

## STATUS: PARTLY DONE — read this before starting

The `TopTabs.tsx` and `projectGitCounts.ts` work below was completed as part of
**Phase 02**, not deferred to this phase. It could not wait: Phase 02 changes
what those consumers count, so leaving them would have made the badges wrong
the moment Phase 02 landed.

Already done (do NOT redo):

- `web/src/components/Layout/TopTabs.tsx` — `gitConflicted` state, added to
  `gitTotal`, and the badge title now reads "N conflicted · N staged · N
  unstaged" when conflicts exist.
- `web/src/lib/projectGitCounts.ts` — `conflicted` field on
  `ProjectGitCounts`, added to `total` and to `sameCounts`, with a documented
  version-skew default for a server that omits the field.
- `web/src/api/types.ts` — `GitConflict` and `GitOperation` types.
- Tests: two `projectGitCounts` cases (conflicted counted; legacy server
  without the field) and a `TopTabs` case (badge total + title).

Still to do in this phase:

- The `editorDiffSource` decision (see "Also check for other consumers").
- The repo-wide sweep for remaining `staged_files` / `changed_files` readers,
  on the Go side as well as the web side.

## What this phase delivers

The three places that show "how much work is in this repository" continue to
agree once conflicted files stop appearing in the staged and changed lists.

## Context you need

Phase 2 removes every conflicted path from both `staged_files` and
`changed_files` and exposes them in a separate conflicts list instead. Two
consumers sum those two lists to produce a badge:

- `web/src/components/Layout/TopTabs.tsx`, around lines 98–99, sets the
  session-level Git tab's staged and unstaged counts from a lightweight
  `GET /api/git/status` poll, refreshed on the server-pushed `git_status` bus
  event.
- `web/src/lib/projectGitCounts.ts`, around lines 92–93, derives the project
  sidebar's staged, unstaged and total counts from the same endpoint. That
  module is deliberately strict: one request per host-and-project pair, shared
  across every mounted row, gated so it never cold-connects a remote host, and
  a failed fetch keeps the last known counts rather than zeroing them.

The Git tab's own header shows "N staged · M unstaged" from the same source.

After phase 2, all three totals silently shrink by the number of conflicted
files unless they add the conflicts count back. That is the whole point of
this phase.

## What to change

Add the conflicts-list length to each total, in the same way the existing
untracked entries were folded into `changed_files` when that was fixed. Read
the new list defensively with the same optional-chaining and nullish-coalescing
style the surrounding lines already use, because an older server that does not
send the field must not break a newer web bundle.

The sidebar's total is documented as equal to the Git tab's badge so the two
surfaces can never disagree; preserving that invariant is the acceptance
criterion for this phase.

## Also check for other consumers

Before finishing, search the web tree for every remaining reader of
`staged_files` and `changed_files`. Known readers at the time of planning are
the two above, plus test fixtures:

- `web/src/lib/editorDiffSource.ts` and its test use both fields to decide
  whether a file has an unstaged git diff versus falling back to session
  diffs. A conflicted file is not in the changed list any more, so the editor
  will no longer decorate it as git-modified. Decide deliberately whether
  that is correct. It is defensible — a conflicted file is neither a clean
  git state nor simply edited — but it must be a decision, not an accident,
  and if the decorator should show conflicts, it needs the new list.
- `web/src/components/Git/GitPanel.loading.test.tsx` and
  `web/src/components/Git/GitPanel.test.tsx` build status fixtures that will
  need the new field.
- `web/src/components/Layout/TopTabs.test.tsx` mocks the status endpoint and
  will need updating too.

## Tests first

- Update `web/src/lib/projectGitCounts.test.ts` so a status with conflicts
  asserts the sidebar total includes them, and so a status from a server that
  omits the field entirely still works.
- Update or add a `TopTabs` test asserting the tab badge includes conflicts.
- Add a `GitPanel` test asserting the header's staged/unstaged/conflict
  summary reflects the conflicts list.
- If `editorDiffSource` is changed, add a case pinning the new behavior for a
  conflicted path either way.

## Verification

- `cd web && npm run typecheck`
- `cd web && npm run test -- projectGitCounts TopTabs GitPanel editorDiffSource`
- `cd web && npm run test`
- `cd web && npm run build`
- Mutation-check: drop the conflicts term from the sidebar total and confirm
  its test goes red.

## Risks and open questions

- The failure mode here is silent: a shrinking badge looks plausible, and
  nothing crashes. The tests are the only protection, so they must assert the
  conflicted case explicitly rather than only the ordinary cases.
- If `editorDiffSource` stops decorating conflicted files, that is a visible
  change in the file tree. Confirm it is acceptable rather than incidental.
- If any consumer outside `web/src` reads these fields — a Go test asserting
  on the status shape, for instance — it must be updated in the same change.
  Search the Go tree too, not just the web tree.
