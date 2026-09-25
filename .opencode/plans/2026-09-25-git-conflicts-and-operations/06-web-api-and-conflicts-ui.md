# Phase 06 — Web API types, conflicts section, operation banner

Status: **DONE + VERIFIED 2026-09-25.** See the decisions at the end.

Phase 6 of the web Git tab conflict/operation-recovery work, tracked from
`.opencode/plans/2026-09-25-git-conflicts-and-operations/INDEX.md`.

## What this phase delivers

The user-visible half: a Conflicts section listing every conflicted file with
a per-file resolution action, and an operation banner with the buttons that
continue, abort or skip a halted operation.

## Context you need

Three files carry the client side.

**`web/src/api/types.ts`** mirrors the Go response structs. `GitStatus` is
around line 487 and `GitWorkspace` around line 520. Add the conflict shape
(path, status code, and the two side-presence booleans) and the optional
operation shape (kind, label, step, total), and add them to `GitStatus`. The
workspace type needs no change, because the conflicts and the operation arrive
inside its embedded status.

**`web/src/api/client.ts`** exposes the git calls around lines 975–1077. Every
one takes an optional project and an optional host, and the host argument is
what routes a call through the remote proxy. Add the two new calls with the
same signature discipline — a caller that omits the host must stay local, and
a caller that supplies one must reach the remote project.

**`web/src/components/Git/GitPanel.tsx`** is the panel. It is about 1974 lines
and already organized into staged, unstaged, commits and stashes sections, with
a `STATUS_BADGES` map near line 76 giving each file state a one-letter badge
and a color, a header near lines 842–960 carrying the branch, the ahead/behind
arrows and the fetch/pull/push/force-push/reset-remote buttons, and a
`runMutation` helper near line 360 that sets the busy state, runs the call,
reloads, shows a 5-second success notice, and leaves a failed action's error
sticky.

Reuse `runMutation` for every new action. Do not introduce a second
error/notice mechanism; the sticky-error and short-lived-notice contract is
deliberate, and a background refresh must never clear a user-action error.

`STATUS_BADGES` needs an entry for the conflict state, colored red, and the
section header's existing "N staged · M unstaged" summary needs a conflicts
count.

## The conflicts section

Render it above the staged section, and only when there is at least one
conflict, so a clean repository's layout is unchanged.

Each row shows the file's status code as its badge, its path, and three
actions: keep our side, keep their side, and mark resolved. Clicking the row
itself opens the file through the panel's existing `onOpenFile` prop, which
opens it in the normal editor. That is deliberate and replaces any inline
diff preview: the server's unmerged diff is a combined diff that the existing
parser cannot read, so rendering it would produce half-correct output. The
editor shows the real conflict markers, which is what the user needs in order
to choose a side.

The two choice buttons need **operation-aware labels**. During a rebase, git's
"ours" is the upstream branch being rebased onto and git's "theirs" is the
commit being replayed — the opposite of the intuitive reading. So during a
rebase, label them "Keep upstream" and "Keep my commit"; otherwise "Use ours"
and "Use theirs". A tooltip should state which side each one takes. Getting
this wrong is the single most dangerous mistake in this feature, because the
user would discard their own work while believing they were keeping it.

## The operation banner

Place it directly under the header, above the conflicts section, whenever the
status reports an operation. It shows the operation's label, including step
progress when there is any, plus the buttons valid for that operation's kind.
The button set must match the server's action matrix exactly:

- merge: Continue, Abort
- rebase and interactive rebase: Continue, Abort, Skip
- am: Continue, Abort, Skip
- cherry-pick and revert: Continue, Abort, Skip
- bisect: Good, Bad, Skip, Reset

Continue is disabled while any conflict remains unresolved, with a tooltip
saying conflicts must be resolved first — git will refuse anyway, and a
disabled button explains why instead of relaying a cryptic error. Abort is
never disabled, but it discards the in-progress operation, so it goes through
the panel's existing confirmation-dialog pattern rather than firing directly.
Reset for a bisect is the same kind of action and gets the same treatment.

Successful continues and aborts show a notice, and any hook output the server
returned is shown with it rather than dropped.

## Disabling everything else mid-operation

While an operation is in progress, disable commit, pull, push, force-push,
reset-remote, stash, and the per-hunk stage/unstage/discard controls. An
in-progress merge or rebase is exactly the state where those operations are
either meaningless or dangerous, and letting them fire turns a clear banner
into a confusing git error. Stash is included for that reason even though this
feature never uses stash itself.

## Superseding the old error

A pull that stops on a conflict currently leaves a sticky error reading like
"git pull failed: CONFLICT ...". Once the banner is showing, that message is
stale and confusing. Clear it on the narrow transition from no operation to
some operation, and only then. A general "clear the error whenever the
operation appears" rule is nearly that narrow, but state the condition
explicitly in code: a background reload must not clear a user-action error for
any other reason, because that regression is exactly what the sticky-error
contract exists to prevent.

## Tests first

- `web/src/api/types.ts` and client additions covered through
  `web/src/api/client.*.test.ts` by asserting each new call produces the right
  URL and body, and that omitting the host yields a local URL while supplying
  it yields a remote one. Note that these tests assert call arity exactly, so
  an added optional argument must be passed explicitly as undefined rather
  than omitted.
- `web/src/components/Git/GitPanel.test.tsx`: the banner renders the label and
  the step progress; Continue is disabled while conflicts exist and enabled
  once they are gone; Abort is always enabled; a bisect renders Good, Bad,
  Skip and Reset and no Continue; each conflict row's three actions call the
  right endpoint with the right arguments; the resolution button labels
  change to the rebase wording while a rebase is in progress; destructive
  controls are disabled during an operation; the sticky error survives a
  background refresh but is cleared on the idle-to-operation transition.
- Keep the existing `GitPanel` suites green, including the loading and
  background-refresh tests, since the load path is being touched.

## Verification

- `cd web && npm run typecheck`
- `cd web && npm run test -- GitPanel client.projectGitCounts TopTabs`
- `cd web && npm run test`
- `cd web && npm run build`
- Mutation-check: make the rebase button labels read "ours"/"theirs" and
  confirm the label test goes red; remove the Continue disable condition and
  confirm its test goes red; clear the error on every reload and confirm the
  sticky-error test goes red.

## Risks and open questions

- The panel is already long and has established section, dialog and notice
  patterns. The new banner and section must reuse them rather than
  introducing a parallel visual language, or the panel's behavior will drift
  across its sections.
- The narrow-width layout of a new banner plus a new section is untested
  geometry in jsdom. Check that the banner's buttons wrap rather than
  overflow at the panel's narrow widths, and consider a real browser check if
  the build allows it.
- The rebase ours/theirs inversion is the highest-consequence detail in this
  phase. It must be pinned by a test that asserts the *wording*, not just the
  presence of two buttons.

## Implementation decisions (recorded 2026-09-25)

- **The rebase inversion also covers `am`.** The plan named rebase; an in-
  progress `git am` is a patch series being replayed onto the current branch
  and has the same ours/theirs semantics, so it gets the same "Keep upstream"
  / "Keep my commit" wording. Showing the plain labels during an `am` would be
  the same dangerous misreading.

- **The stale-error clear uses a ref, not a state updater.** Comparing the
  previous operation state and calling `setError` inside a `setState` updater
  is a side effect in the render phase, which double-fires under StrictMode.
  A ref makes the transition explicit and idempotent.

- **The panel normalizes `conflicts` / `operation` on arrival.** The Go
  structs always populate them, but a server predating this feature omits them
  from the JSON, and both are read during render — so `status.conflicts.length`
  was a latent crash of the entire Git panel against a mixed-version
  deployment. Caught by an existing test whose fixture had no `conflicts`
  field; normalized once where `status` is derived, with a regression test.

- **Host travels as `?host=`, not a URL prefix.** The plan said "the host
  argument is what routes a call through the remote proxy", which is true but
  easy to misread as a path prefix. Every existing git call uses
  `projQuery(project, host)` and the server proxies on that basis; the first
  draft of the client test asserted `/api/remote/<host>/...` and was wrong.

## Verification (2026-09-25)

`client.gitConflicts.test.ts` (4) and `GitPanel.conflicts.test.tsx` (19), plus
the pre-existing GitPanel suites (55 total across `src/components/Git/`).
Full web suite: 2121 passing; the only 3 failures belong to another session's
untracked `useListNavigation.test.tsx`. `npm run build` clean, typecheck clean
for every file this phase touched.

Three mutations confirmed the tests bite: reverting the rebase labels to
ours/theirs; removing the Continue disable condition; and clearing the error on
every reload instead of only on the idle -> operation edge.

## Correction found in Phase 07 review (2026-09-25)

Two details in this phase's implementation contradicted the contract this
phase was supposed to deliver. Both are now fixed; the concept page written in
Phase 08 was correct all along, so the code had drifted from its own docs.

- **Action naming.** A bisect's stop action is the verb **`abort`** on the
  wire, because that is the entry in the server's `gitOperationCommand` table;
  the server maps it to `git bisect reset`. The button is still *labelled*
  "Reset", because that is what git calls the command. The client had been
  sending `"reset"`, which the server rejects with 400 — so every click on
  Reset during a bisect failed. The server's `GitOperationRequest.Action` doc
  comment also listed `reset` as an accepted action; it now lists the real
  vocabulary and explains the difference.
- **Which kinds invert the ours/theirs labels.** Only `rebase` and
  `rebase-interactive`. `am` was included here, which is wrong: verified in a
  scratch repo with `git am -3`, stage 2 ("ours") is the current HEAD and
  stage 3 ("theirs") is the incoming patch, i.e. cherry-pick semantics. Under
  the old code an `am` conflict offered "Keep upstream" / "Keep my commit",
  pointing at the wrong side of the user's own history.
