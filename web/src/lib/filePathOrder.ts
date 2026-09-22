/**
 * Shared file-search ordering rule: shortest path first.
 *
 * "Shortest" means fewest path segments (a shallow file surfaces above a
 * deeply nested one), then lexicographic order for determinism. Used to break
 * ties between equally relevant Ctrl+P / Files-tab search results and to order
 * an unfiltered file list. Mirrors the TUI's `lessPathShortest`
 * (internal/tui/path_order.go) so every surface agrees.
 */

/** Count the path segments in a relative path, ignoring "./" and separators. */
export function pathSegmentCount(filePath: string): number {
  const normalised = filePath
    .replace(/\\/g, "/")
    .replace(/^\.\//, "")
    .replace(/^\/+|\/+$/g, "");
  if (!normalised) return 0;
  return normalised.split("/").filter(Boolean).length;
}

/** Comparator: fewest segments first, then lexicographic. */
export function compareByShortestPath(a: string, b: string): number {
  const sa = pathSegmentCount(a);
  const sb = pathSegmentCount(b);
  if (sa !== sb) return sa - sb;
  if (a < b) return -1;
  if (a > b) return 1;
  return 0;
}
