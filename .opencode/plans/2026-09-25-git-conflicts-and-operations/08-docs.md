# Phase 08 — Documentation

Phase 8 of the web Git tab conflict/operation-recovery work, tracked from
`.opencode/plans/2026-09-25-git-conflicts-and-operations/INDEX.md`.

## What this phase delivers

Curated project documentation for the new behavior, plus the changelog entry.
The feature adds user-visible Git tab behavior, a pair of new HTTP endpoints,
and a new failure mode the UI now knows how to recover from, so it needs a
durable record rather than only a code comment.

## Context you need

The project's documentation bundle under `docs/` is owned by the knowledge
system. Do **not** read or write files under `docs/` directly with the file
tools. Route the page through the context sub-agent, which can search the
bundle first, write the page, and maintain the generated index and log
consistently. A knowledge lookup for this topic was already run at design time
and found nothing on conflicts, rebases, or in-progress git state, so this is
a genuinely new page rather than an edit to an existing one.

The nearest existing pages, useful as structural references for the new one,
are the git-stash UI concept page and the gotcha about git action errors
disappearing. The latter is worth cross-linking, because the new banner
deliberately supersedes the sticky pull error in exactly one narrow case, and a
future reader comparing the two pages should find that explained.

`CHANGES.md` at the repository root is the changelog. It has an Unreleased
section at the top; add the entry there, matching the surrounding entries'
format and dating convention.

If a project skill file documents the Git tab — the web skill's file map and
conventions section is the likely candidate — add a line there pointing at the
new page and naming the new components, so the next agent working on the Git
tab finds the conflict and operation behavior without re-deriving it.

## What the page should cover

- The user-facing model: a conflicted file is reported in its own section with
  its git status code, and a halted operation is reported in a banner with
  the actions valid for it.
- The three resolution actions, and the rebase inversion — during a rebase the
  side labeled "ours" in git is the upstream branch, which is why the UI
  relabels the buttons. This is the detail most likely to be got wrong by a
  future change, so it belongs in prose, not just in the UI.
- The operation kinds and their action matrix, including that a merge has no
  skip and a bisect has no continue and instead advances by marking a commit
  good or bad.
- The endpoint contracts, including that the operation endpoint re-detects
  state and rejects a stale kind, and that mark-resolved refuses a file that
  still has conflict markers.
- The two environment hardenings that matter to a maintainer: editor
  suppression so a continue can never hang waiting for a terminal, and literal
  pathspecs so a filename can never be reinterpreted as pathspec magic.
- The known limitations: conflicted paths containing a colon cannot be
  resolved on a remote project, because the remote pathspec validator rejects
  colons; and a conflicted path no longer appears in the staged or changed
  file lists, which is why the badge consumers add the conflicts count back.

## Deliverables

1. A new concept page, written through the context agent, covering the points
   above.
2. A `CHANGES.md` entry under Unreleased describing the feature in user terms.
3. A pointer line in the web skill's Git tab documentation.
4. A `TODO.md` entry for the two follow-ups this feature knowingly leaves
   behind: the remote colon-path limitation, and any other deferred item
   surfaced during implementation.

## Verification

- Confirm the context agent reports the page written and the index updated.
- Confirm the `CHANGES.md` entry renders in the right section and does not
  disturb the existing Unreleased entries.
- Re-read the page once to confirm it matches what was actually built, rather
  than what was planned. In particular, if the rebase labeling or the action
  matrix changed during implementation, the page must reflect the code.

## Risks and open questions

- The context agent has failed tool calls in this project before, in which case
  the page body had to be written with the file editor and the index entry
  added separately. If that happens here, follow the same recovery and note it
  in `TODO.md` so the bundle's log stays consistent.
- The documentation bundle's index and log are generated. Do not hand-edit
  them.
