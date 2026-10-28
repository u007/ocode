import type { GhosttyCell } from "ghostty-web";

/**
 * Converts one screen row of libghostty cells to text. A width-0 cell is the
 * trailing half of a wide character and adds nothing; an empty cell is a space;
 * a cell that starts a multi-codepoint grapheme reads its whole cluster through
 * `graphemeAt` (the codepoint field only holds the first one). The row is
 * right-trimmed because the server's terminal_screen trims every row too.
 */
export function ghosttyCellsToText(cells: readonly GhosttyCell[], graphemeAt: (col: number) => string): string {
  let text = "";
  for (let col = 0; col < cells.length; col++) {
    const cell = cells[col];
    if (cell.width === 0) continue;
    if (cell.codepoint === 0) text += " ";
    else if (cell.grapheme_len > 0) text += graphemeAt(col);
    else text += String.fromCodePoint(cell.codepoint);
  }
  return text.trimEnd();
}
