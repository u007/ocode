/**
 * parseDiffPatch — Unified diff (patch) parser
 *
 * Takes the text output of `git diff -u` / `diff -u` and produces structured
 * hunks for rendering inline decorations in Monaco.
 *
 * Each hunk carries:
 *   - oldStart / oldLines  — position in the original file
 *   - newStart / newLines  — position in the new file
 *   - lines               — array of {type, text} where type is
 *                           'context' | 'add' | 'del'
 */

export interface DiffLine {
  type: "context" | "add" | "del";
  text: string;
}

export interface Hunk {
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  lines: DiffLine[];
}

/**
 * Parse a unified-diff patch string into hunks.
 * Returns an empty array for an empty/null patch or a patch with no hunks.
 */
export function parseDiffPatch(patch: string | null | undefined): Hunk[] {
  if (!patch) return [];

  const lines = patch.split("\n");
  const hunks: Hunk[] = [];
  let current: Hunk | null = null;

  for (const line of lines) {
    const hunkHeader = /^@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@/.exec(line);
    if (hunkHeader) {
      if (current) hunks.push(current);
      current = {
        oldStart: parseInt(hunkHeader[1], 10),
        oldLines: hunkHeader[2] ? parseInt(hunkHeader[2], 10) : 1,
        newStart: parseInt(hunkHeader[3], 10),
        newLines: hunkHeader[4] ? parseInt(hunkHeader[4], 10) : 1,
        lines: [],
      };
      continue;
    }

    if (!current) continue; // Skip header lines before the first hunk

    if (line.startsWith("+")) {
      current.lines.push({ type: "add", text: line.slice(1) });
    } else if (line.startsWith("-")) {
      current.lines.push({ type: "del", text: line.slice(1) });
    } else if (line.startsWith(" ")) {
      current.lines.push({ type: "context", text: line.slice(1) });
    } else if (line.startsWith("\\")) {
      // No newline at end of file — skip, not a content line
      continue;
    } else if (line === "" && current.lines.length > 0) {
      // Empty lines within a hunk (after the header) are literal blank lines
      // in the original. Treat as context.
      current.lines.push({ type: "context", text: "" });
    }
  }

  if (current) hunks.push(current);
  return hunks;
}
