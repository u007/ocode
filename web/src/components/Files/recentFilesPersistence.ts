// Recently opened files per project, for the Ctrl+P / Cmd+P file picker.
//
// Keys are the per-project key of the hidden-files preference
// (`showHiddenFilesProjectKey`), so a remote host's files never rank inside a
// local project that happens to share the same path.

import { treeRelativePath } from "./fileTreeReveal";

export const RECENT_FILES_STORAGE_PREFIX = "ocode.ui.recentFiles.v1:";
export const RECENT_FILES_LIMIT = 50;

function storageKey(projectKey: string): string {
  return `${RECENT_FILES_STORAGE_PREFIX}${projectKey}`;
}

/** Recently opened paths for one project, most recent first. */
export function loadRecentFiles(projectKey: string): string[] {
  let raw: string | null;
  try {
    raw = window.localStorage.getItem(storageKey(projectKey));
  } catch (err) {
    console.warn("Failed to read recent files from localStorage", err);
    return [];
  }
  if (raw == null) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    console.warn("Recent files value is not valid JSON; ignoring it", err);
    return [];
  }
  if (!Array.isArray(parsed) || !parsed.every((p) => typeof p === "string")) {
    console.warn("Recent files value is not a string array; ignoring it");
    return [];
  }
  return parsed;
}

/** Move `path` to the front of the project's recent list (deduped, capped). */
export function recordRecentFile(projectKey: string, path: string): void {
  const next = [path, ...loadRecentFiles(projectKey).filter((p) => p !== path)].slice(0, RECENT_FILES_LIMIT);
  try {
    window.localStorage.setItem(storageKey(projectKey), JSON.stringify(next));
  } catch (err) {
    console.warn("Failed to save recent files to localStorage", err);
  }
}

/**
 * `paths` with the recently opened ones moved to the front, most recent first.
 * Everything else keeps its order.
 *
 * `recent` may hold absolute paths under `projectPath`; they are reduced to the
 * tree-relative form the picker lists. A recent path that is not in `paths` is
 * dropped (the file may have been deleted), and so is one in `excluded`, which
 * holds folder paths: only files are ever "recent".
 */
export function rankByRecent(
  paths: string[],
  recent: string[],
  projectPath?: string,
  excluded: ReadonlySet<string> = new Set(),
): string[] {
  const present = new Set(paths);
  const front: string[] = [];
  const moved = new Set<string>();
  for (const raw of recent) {
    const rel = treeRelativePath(raw, projectPath);
    if (rel === null || moved.has(rel) || excluded.has(rel) || !present.has(rel)) continue;
    moved.add(rel);
    front.push(rel);
  }
  if (front.length === 0) return paths;
  return [...front, ...paths.filter((p) => !moved.has(p))];
}
