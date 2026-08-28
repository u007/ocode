// Pure drop-resolution logic for project sidebar drag-and-drop.
// Given the current projects/groups state and a dnd-kit active/over pair,
// decides what the drop means: a within-bucket reorder, a cross-bucket move
// (group change + position), or nothing.

export interface DragProject {
  path: string;
  group?: string;
}

export interface DragGroup {
  name: string;
  order: number;
}

export type ProjectDragResult =
  | { type: "none" }
  | { type: "reorder"; paths: string[] }
  | { type: "move"; path: string; group: string; paths: string[] };

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
  const fullOrderedPaths: string[] = [];
  for (const g of sortedGroups) {
    for (const p of byGroup[g.name] || []) fullOrderedPaths.push(p.path);
  }
  for (const p of byGroup[""] || []) fullOrderedPaths.push(p.path);

  const pathToGroup = new Map<string, string>();
  for (const p of projects) pathToGroup.set(p.path, p.group || "");

  if (!pathToGroup.has(activeId)) return { type: "none" };
  const activeGroup = pathToGroup.get(activeId) ?? "";

  // Dropped on a group header → move into that group, appended at the end.
  if (overId.startsWith(GROUP_PREFIX)) {
    const targetGroup = overId.slice(GROUP_PREFIX.length);
    if (targetGroup === activeGroup) return { type: "none" };
    if (!groups.some((g) => g.name === targetGroup)) return { type: "none" };
    const without = fullOrderedPaths.filter((p) => p !== activeId);
    const members = without.filter((p) => pathToGroup.get(p) === targetGroup);
    const insertAt = members.length
      ? without.indexOf(members[members.length - 1]) + 1
      : without.length;
    without.splice(insertAt, 0, activeId);
    return { type: "move", path: activeId, group: targetGroup, paths: without };
  }

  if (!pathToGroup.has(overId)) return { type: "none" };
  const overGroup = pathToGroup.get(overId) ?? "";

  const oldIdx = fullOrderedPaths.indexOf(activeId);
  const overIdx = fullOrderedPaths.indexOf(overId);
  const without = fullOrderedPaths.filter((p) => p !== activeId);
  // arrayMove semantics: dragging down lands after the target, dragging up before it.
  const insertAt = oldIdx < overIdx ? without.indexOf(overId) + 1 : without.indexOf(overId);
  without.splice(insertAt, 0, activeId);

  if (activeGroup === overGroup) {
    return { type: "reorder", paths: without };
  }
  return { type: "move", path: activeId, group: overGroup, paths: without };
}
