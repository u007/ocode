// Pure helpers behind the Files tab's "Show in file tree" action (the
// editor/preview tab context menu).
//
// The tree and the editor tabs disagree about what a path IS:
//
//   * The tree addresses every node ROOT-RELATIVE — `internal/server/
//     handler_files.go:buildFileTree` sets `Path: filepath.Rel(base, cur)` —
//     and it renders those relative paths as row keys, expansion keys, and
//     `data-tree-path` attributes.
//   * An editor tab's `path` is whatever the opener passed, so it can be
//     root-relative (opened from the tree) or absolute (a chat file link, a
//     `preview_open`), while its `projectRoot` names the browsed root.
//
// Everything here is therefore about reconciling those two vocabularies
// WITHOUT a network round trip, so a reveal is instant and testable. No DOM,
// no React: the tree component owns all state changes.

/**
 * Directories the server's tree walk skips even when hidden files are shown.
 * Mirrors `ignoredDirNames` in `internal/server/handler_files.go` — generated /
 * vendored trees that are large and not source-navigable. A mismatch only
 * costs a late "not shown" notice (the walk itself is authoritative); the
 * dot-prefix rule below is the one that matters in practice.
 */
const SERVER_IGNORED_DIRS: ReadonlySet<string> = new Set([
  "node_modules",
  "vendor",
  "dist",
  "build",
  "target",
  ".next",
  ".venv",
  "venv",
  "__pycache__",
  "coverage",
  "bower_components",
  ".cache",
]);

/**
 * Canonical form of a tree path: forward slashes (Windows separators and
 * redundant trailing slashes removed) and no leading `./`. Node paths, tab
 * paths, expansion keys and `data-tree-path` attributes all go through this so
 * a Windows `src\app` node still matches a computed `src/app`.
 */
export function normalizeTreePath(path: string): string {
  return (path ?? "")
    .trim()
    .replace(/\\/g, "/")
    .replace(/\/+/g, "/")
    .replace(/^\.\//, "")
    .replace(/\/+$/, "");
}

/** True for `/abs/path` and `C:/abs/path` (post-normalization forms). */
function isAbsoluteTreePath(path: string): boolean {
  return path.startsWith("/") || isWindowsTreePath(path);
}

/** True for a Windows drive-rooted path (`C:/proj`). */
function isWindowsTreePath(path: string): boolean {
  return /^[a-z]:\//i.test(path);
}

/**
 * `path` with the `root` prefix removed, or `null` when it is not under it.
 * Drive-rooted Windows paths compare case-insensitively (`C:\Proj` vs
 * `c:/proj`); the boundary check keeps `/project/x` from matching `/proj`.
 * The root itself yields `null` — the tree root has no row to reveal.
 */
function stripRootPrefix(path: string, root: string): string | null {
  if (path === root) return null;
  if (path.startsWith(`${root}/`)) return path.slice(root.length + 1);
  if (isWindowsTreePath(path) || isWindowsTreePath(root)) {
    if (path.toLowerCase().startsWith(`${root.toLowerCase()}/`)) return path.slice(root.length + 1);
  }
  return null;
}

/** Directory part of a root-relative path; `""` for a direct child of the root. */
export function treeDirOf(relPath: string): string {
  const p = normalizeTreePath(relPath);
  const i = p.lastIndexOf("/");
  return i >= 0 ? p.slice(0, i) : "";
}

/**
 * Every ancestor directory of a root-relative path, outermost first
 * (`src/app/a.ts` → `["src", "src/app"]`). These are exactly the directories
 * that must be expanded for the row to exist, and each one is a key in the
 * tree's persisted expansion set.
 */
export function ancestorDirs(relPath: string): string[] {
  const p = normalizeTreePath(relPath);
  if (!p) return [];
  const parts = p.split("/").slice(0, -1);
  const out: string[] = [];
  for (let i = 0; i < parts.length; i++) {
    if (!parts[i]) continue;
    out.push(parts.slice(0, i + 1).join("/"));
  }
  return out;
}

/**
 * The tree-relative form of `path`, or `null` when it lies outside `root`
 * (including the root itself, which has no row to reveal).
 *
 * Accepts both shapes an editor tab can hold: a path already relative to the
 * root, or an absolute path under it. A `null` result is a real "outside the
 * browsed folder" case (e.g. an absolute chat-link path against a `~`-rooted
 * remote project), not an error to retry.
 */
export function treeRelativePath(path: string, root?: string): string | null {
  const p = normalizeTreePath(path);
  const r = normalizeTreePath(root ?? "");
  if (!p) return null;
  if (!r) return p;
  const stripped = stripRootPrefix(p, r);
  if (stripped !== null) return stripped;
  // Not under the root. A relative `path` is already root-relative by the
  // tree's own convention, whatever shape the root has; an absolute one that
  // does not live under the root is genuinely outside it.
  if (!isAbsoluteTreePath(p)) return p;
  return null;
}

/**
 * First path segment the tree walk would omit while hidden files are off, or
 * `null` when every segment is visible. Lets a reveal explain itself
 * immediately instead of expanding folders that can never render a row.
 */
export function hiddenTreeSegment(relPath: string): string | null {
  for (const segment of normalizeTreePath(relPath).split("/")) {
    if (!segment || segment === "." || segment === "..") continue;
    if (segment.startsWith(".") || SERVER_IGNORED_DIRS.has(segment)) return segment;
  }
  return null;
}

/**
 * Whether two project roots name the same browsable folder. Windows drive
 * paths compare case-insensitively (`C:\Proj` vs `c:/proj`); everything else
 * compares exactly, because a macOS/Linux root is case-sensitive on disk even
 * where the filesystem is not.
 */
export function sameTreeRoot(a?: string, b?: string): boolean {
  const x = normalizeTreePath(a ?? "");
  const y = normalizeTreePath(b ?? "");
  if (x === y) return true;
  if (!x || !y) return false;
  if (isWindowsTreePath(x) || isWindowsTreePath(y)) return x.toLowerCase() === y.toLowerCase();
  return false;
}