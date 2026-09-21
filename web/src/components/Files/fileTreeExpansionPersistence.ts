// Persisted directory-expansion state for the Files-tab tree.
//
// The tree is rebuilt (and every TreeNode remounted) whenever the user
// switches project or root, so expansion was previously lost on every switch.
// We store the set of expanded directory paths per tree root in localStorage,
// keyed by host + root, so both project switches and app restarts restore the
// folders the user had open.

const STORAGE_KEY = "ocode.ui.fileTreeExpansion.v1";

// Guard against an unbounded localStorage blob if a pathological tree is
// expanded. Far above any realistic single-root expansion count.
const MAX_PATHS_PER_ROOT = 2000;

interface PersistedFile {
  version: 1;
  /** treeRootKey -> expanded directory paths (root-relative, e.g. "src"). */
  roots: Record<string, string[]>;
}

/**
 * Stable persistence key for one browsable tree root. Includes the remote host
 * because two hosts can expose the same relative path (e.g. `~/www/app`), and
 * the active root because extra allowed paths can share a project's paths.
 * Returns null when there is no usable root, which disables persistence.
 */
export function fileTreeRootKey(
  projectPath: string | undefined,
  projectHost: string | undefined,
  root: string | undefined,
): string | null {
  const base = root ?? projectPath;
  if (!base) return null;
  const trimmed = base.replace(/[\\/]+$/, "") || base;
  return projectHost ? `${projectHost}\u0000${trimmed}` : trimmed;
}

function readFile(): PersistedFile {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return { version: 1, roots: {} };
    const parsed = JSON.parse(raw) as PersistedFile;
    if (
      !parsed ||
      parsed.version !== 1 ||
      typeof parsed.roots !== "object" ||
      parsed.roots === null ||
      Array.isArray(parsed.roots)
    ) {
      return { version: 1, roots: {} };
    }
    return parsed;
  } catch {
    return { version: 1, roots: {} };
  }
}

/** Expanded directory paths for a root, or an empty set when none/unknown. */
export function loadExpandedDirs(key: string | null): Set<string> {
  if (!key) return new Set();
  const arr = readFile().roots[key];
  if (!Array.isArray(arr)) return new Set();
  const out = new Set<string>();
  for (const p of arr) {
    if (typeof p === "string" && p.length > 0) out.add(p);
  }
  return out;
}

/** Persist the expanded directory paths for a root. Empty removes the entry. */
export function saveExpandedDirs(key: string | null, paths: Iterable<string>): void {
  if (!key) return;
  const file = readFile();
  const list: string[] = [];
  for (const p of paths) {
    if (typeof p !== "string" || p.length === 0) continue;
    if (!list.includes(p)) list.push(p);
    if (list.length >= MAX_PATHS_PER_ROOT) break;
  }
  if (list.length === 0) {
    delete file.roots[key];
  } else {
    file.roots[key] = list;
  }
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(file));
  } catch {
    // ignore — quota/blocked storage must not break the tree
  }
}

export const FILE_TREE_EXPANSION_STORAGE_KEY = STORAGE_KEY;
