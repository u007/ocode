/** Labels for the two conflict-side buttons.
 *
 *  THE most consequential detail in this feature. During a rebase git's
 *  "ours" is the UPSTREAM branch being replayed onto and "theirs" is the
 *  user's own commit — the exact opposite of the intuitive reading. A user
 *  who reads "Use ours" as "keep my work" would silently discard their commit
 *  while believing they had kept it. So the wording changes, and the tooltip
 *  names the side explicitly.
 *
 *  Only a rebase inverts. `am` and `cherry-pick` do NOT: verified in a scratch
 *  repo with `git am -3`, stage 2 ("ours") is the current HEAD and stage 3
 *  ("theirs") is the incoming patch. Labelling am's sides as upstream/commit
 *  pointed the user at the wrong side of their own history. */
export function conflictSideLabels(operationKind?: string) {
  if (operationKind === "rebase" || operationKind === "rebase-interactive") {
    return {
      ours: "Keep upstream",
      theirs: "Keep my commit",
      oursTip: "Keep the upstream version (during a rebase, git calls this 'ours')",
      theirsTip: "Keep your own commit (during a rebase, git calls this 'theirs')",
      oursPane: "Upstream",
      theirsPane: "Your commit",
    };
  }
  return {
    ours: "Use ours",
    theirs: "Use theirs",
    oursTip: "Keep our version of this file",
    theirsTip: "Keep their version of this file",
    oursPane: "Ours",
    theirsPane: "Theirs",
  };
}
