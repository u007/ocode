import type { PulseRow } from "@/api/types";

/**
 * Last path segment of a project root — the shared basis for the dashboard's
 * card labels and its filter, so the two cannot drift: a filter that matches on
 * the full path while the card shows a basename (or vice versa) makes the
 * visible label and the thing you can search for disagree, which reads as the
 * filter being broken.
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
 * The card's project label: the basename locally, and `basename @ host` for a
 * remote row. The host is not decoration — two remote projects can hold the same
 * basename on different machines, and a dashboard that showed only "aimsai2"
 * would invite the user to click the wrong one.
 *
 * Trailing slashes are stripped first, so "/proj/alpha/" and "/proj/alpha" are
 * the same project rather than one labelled with an empty string.
 */
export function pulseProjectLabel(row: PulseRow): string {
  const base = pulseProjectBasename(row.project_path);
  return row.host ? `${base} @ ${row.host}` : base;
}

/**
 * True when a row matches the dashboard's free-text filter: a case-insensitive
 * substring of the project BASENAME, the session title, or the host.
 *
 * The title is checked separately from the basename rather than the full path,
 * so a query cannot match a directory segment the user never typed. The host is
 * included because that is the only way to isolate the remote cards once more
 * than one host is in the board.
 */
export function pulseRowMatchesFilter(row: PulseRow, needle: string): boolean {
  const q = needle.trim().toLowerCase();
  if (q === "") return true;
  return (
    pulseProjectBasename(row.project_path).toLowerCase().includes(q) ||
    row.title.toLowerCase().includes(q) ||
    row.host.toLowerCase().includes(q)
  );
}
