import type { Tab } from "../stores/projectStore";

/** The checkout root a tab's session-scoped API calls must be rooted at.
 *
 *  A tab opened on a linked Git worktree carries `worktreePath`; a legacy tab
 *  (or one on the project's main checkout) carries none and resolves to the
 *  owning project path. Never fall back the other way: a tab naming a missing
 *  checkout stays bound to it and is rendered disabled rather than retargeted. */
export function tabRoot(
  tab: Pick<Tab, "projectPath" | "worktreePath">,
): string {
  return tab.worktreePath || tab.projectPath;
}
