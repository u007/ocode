import type { BashRulesDelta, PermissionLevelName, PermissionRule } from "../api/types";

/** A staged row in the Settings rule editor: an editable prefix plus a level. */
export interface BashRuleRow {
  prefix: string;
  level: PermissionLevelName;
}

const LEVELS: readonly string[] = ["allow", "ask", "deny"];

export function normalizeLevel(level: string): PermissionLevelName {
  return (LEVELS.includes(level) ? level : "ask") as PermissionLevelName;
}

/** Normalize a prefix the same way the server does (trim, collapse inner
 * whitespace) so the editor stores exactly the key that will match a command. */
export function normalizePrefix(prefix: string): string {
  return prefix.trim().replace(/\s+/g, " ");
}

/**
 * Validate one rule the way the server does (agent.ValidateBashPrefixRule).
 * Kept in sync deliberately: a rule rejected here would fail the whole batch
 * save, so catching it before the request keeps the form usable.
 *
 * `git` cannot be always-allowed because it would auto-approve every git
 * subcommand, including destructive ones — the server refuses it too.
 */
export function validateRule(prefix: string, level: PermissionLevelName): string | null {
  if (!prefix) return "prefix is required";
  if (prefix.startsWith("__inroot__:")) return "that prefix is reserved for internal rules";
  if (prefix === "git" && level === "allow") return '"git" cannot be always-allowed';
  return null;
}

/** Collapse duplicate prefixes, last row wins. A rename typed onto a prefix
 * that another row already holds would otherwise show two identical rows. */
export function dedupeRows(rows: BashRuleRow[]): BashRuleRow[] {
  const byPrefix = new Map<string, BashRuleRow>();
  for (const row of rows) byPrefix.set(row.prefix, row);
  return [...byPrefix.values()];
}

/**
 * Compute the delta to send for staged rows against the last-loaded baseline.
 *
 * A DELTA, not a replacement map: a rule another surface added (TUI /ban, the
 * /ban slash command) between the load and the save is not in `loaded`, so it is
 * never mentioned in the payload and therefore never deleted. A renamed rule
 * shows up as a remove of the old key plus a set of the new one, which the
 * server resolves in favour of the set.
 */
export function diffBashRules(
  loaded: BashRuleRow[],
  staged: BashRuleRow[],
): BashRulesDelta {
  const loadedByPrefix = new Map(loaded.map((r) => [r.prefix, r.level]));
  const set: Record<string, PermissionLevelName> = {};
  const stagedPrefixes = new Set<string>();

  for (const row of staged) {
    const prefix = normalizePrefix(row.prefix);
    if (!prefix) continue;
    stagedPrefixes.add(prefix);
    const level = normalizeLevel(row.level);
    if (loadedByPrefix.get(prefix) !== level) set[prefix] = level;
  }

  const remove = [...loadedByPrefix.keys()]
    .filter((prefix) => !stagedPrefixes.has(prefix))
    .sort();

  return {
    set: Object.fromEntries(Object.entries(set).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))),
    remove,
  };
}

/** True when a delta would change nothing — the form must not call the API. */
export function isEmptyDelta(delta: BashRulesDelta): boolean {
  return Object.keys(delta.set ?? {}).length === 0 && (delta.remove ?? []).length === 0;
}

/** Server response rows -> editor rows, sorted by prefix. */
export function rowsFromResponse(rules: PermissionRule[] | undefined): BashRuleRow[] {
  return (rules ?? [])
    .filter((r) => !!r?.tool)
    .map((r) => ({ prefix: r.tool, level: normalizeLevel(r.level) }))
    .sort((a, b) => (a.prefix < b.prefix ? -1 : a.prefix > b.prefix ? 1 : 0));
}
