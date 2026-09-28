import type { PulseRow } from "@/api/types";

/**
 * Last path segment of a project root — what the dashboard labels a card with.
 *
 * Shared by PulseView's filter and PulseCard's label so the two cannot drift:
 * a filter that matches on the full path while the card shows a basename (or
 * vice versa) makes the visible label and the thing you can search for
 * disagree, which reads as the filter being broken.
 *
 * Trailing slashes are stripped first, so "/proj/alpha/" and "/proj/alpha" are
 * the same project rather than one labelled with an empty string.
 */
export function pulseProjectBasename(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const idx = trimmed.lastIndexOf("/");
  return idx >= 0 ? trimmed.slice(idx + 1) : trimmed;
}

/**
 * True when a row matches the dashboard's free-text filter: a case-insensitive
 * substring of the project BASENAME or the session title.
 *
 * The title is checked separately from the basename rather than the full path,
 * so a query cannot match a directory segment the user never typed.
 */
export function pulseRowMatchesFilter(row: PulseRow, needle: string): boolean {
  const q = needle.trim().toLowerCase();
  if (q === "") return true;
  return (
    pulseProjectBasename(row.project_path).toLowerCase().includes(q) ||
    row.title.toLowerCase().includes(q)
  );
}
