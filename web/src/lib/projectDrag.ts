// Pure drop-resolution logic for project sidebar drag-and-drop.
// Given the current projects/groups state and a dnd-kit active/over pair,
// decides what the drop means: a within-bucket reorder, a cross-bucket move
// (group change + position), or nothing.

export interface DragProject {
  path: string;
  group?: string;
  /** Remote host (`[user@]host` or `wsl:<distro>`); absent/empty for local. */
  host?: string;
}

export interface DragGroup {
  name: string;
  order: number;
}

/** Scoped identity for a sidebar project. Local entries key on the path;
 *  remote entries key on (host, verbatim path) so the same path on two
 *  hosts never collides. The "\x00" separator is an internal map key only —
 *  it is never persisted or sent over the wire (reorder payloads carry
 *  structured {path, host} refs). */
export function projectDragKey(path: string, host?: string): string {
  return host ? `${host}\x00${path}` : `\x00${path}`;
}

export interface ProjectDragRef {
  path: string;
  host?: string;
}

export type ProjectDragResult =
  | { type: "none" }
  | { type: "reorder"; refs: Array<{ path: string; host?: string }> }
  | { type: "move"; ref: ProjectDragRef; group: string; refs: Array<{ path: string; host?: string }> };

const GROUP_PREFIX = "group:";

export function computeProjectDrag(
  projects: DragProject[],
  groups: DragGroup[],
  activeId: string,
  overId: string,
): ProjectDragResult {
  if (activeId.startsWith(GROUP_PREFIX)) return { type: "none" };

  // Full global order: groups first (by group order), then ungrouped —
  // includes collapsed groups' projects so the reorder payload covers everything.
  const sortedGroups = [...groups].sort((a, b) => a.order - b.order);
  const byGroup: Record<string, DragProject[]> = {};
  for (const p of projects) {
    const key = p.group || "";
    if (!byGroup[key]) byGroup[key] = [];
    byGroup[key].push(p);
  }
  const keyOf = (p: DragProject) => projectDragKey(p.path, p.host);
  const fullOrderedKeys: string[] = [];
  const keyToRef = new Map<string, { path: string; host?: string }>();
  for (const g of sortedGroups) {
    for (const p of byGroup[g.name] || []) {
      const k = keyOf(p);
      fullOrderedKeys.push(k);
      if (!keyToRef.has(k)) keyToRef.set(k, { path: p.path, host: p.host });
    }
  }
  for (const p of byGroup[""] || []) {
    const k = keyOf(p);
    fullOrderedKeys.push(k);
    if (!keyToRef.has(k)) keyToRef.set(k, { path: p.path, host: p.host });
  }

  const keyToGroup = new Map<string, string>();
  for (const p of projects) keyToGroup.set(keyOf(p), p.group || "");

  if (!keyToGroup.has(activeId)) return { type: "none" };
  const activeGroup = keyToGroup.get(activeId) ?? "";
  const toRefs = (keys: string[]) =>
    keys.map((k) => keyToRef.get(k) ?? { path: k });

  // Dropped on a group header → move into that group, appended at the end.
  if (overId.startsWith(GROUP_PREFIX)) {
    const targetGroup = overId.slice(GROUP_PREFIX.length);
    if (targetGroup === activeGroup) return { type: "none" };
    if (!groups.some((g) => g.name === targetGroup)) return { type: "none" };
    const without = fullOrderedKeys.filter((k) => k !== activeId);
    const members = without.filter((k) => keyToGroup.get(k) === targetGroup);
    const insertAt = members.length
      ? without.indexOf(members[members.length - 1]) + 1
      : without.length;
    without.splice(insertAt, 0, activeId);
    return { type: "move", ref: keyToRef.get(activeId) ?? { path: activeId }, group: targetGroup, refs: toRefs(without) };
  }

  if (!keyToGroup.has(overId)) return { type: "none" };
  const overGroup = keyToGroup.get(overId) ?? "";

  const oldIdx = fullOrderedKeys.indexOf(activeId);
  const overIdx = fullOrderedKeys.indexOf(overId);
  const without = fullOrderedKeys.filter((k) => k !== activeId);
  // arrayMove semantics: dragging down lands after the target, dragging up before it.
  const insertAt = oldIdx < overIdx ? without.indexOf(overId) + 1 : without.indexOf(overId);
  without.splice(insertAt, 0, activeId);

  if (activeGroup === overGroup) {
    return { type: "reorder", refs: toRefs(without) };
  }
  return { type: "move", ref: keyToRef.get(activeId) ?? { path: activeId }, group: overGroup, refs: toRefs(without) };
}
