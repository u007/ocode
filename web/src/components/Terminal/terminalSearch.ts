import type { IDisposable, Terminal } from "ghostty-web";

// Replacement for xterm's SearchAddon. ghostty-web ships no search and no decoration API, so this
// is a plain case-insensitive substring search that highlights the current match through the
// caller-supplied `highlight` (the panel uses a selection) and has no all-matches decorations.

export interface TerminalSearchResult {
  resultCount: number;
  resultIndex: number; // -1 = none
}

export interface TerminalSearch {
  findNext(query: string): boolean;
  findPrevious(query: string): boolean;
  /** Clears the highlight and reports {0, -1}. */
  clear(): void;
  onDidChangeResults(cb: (r: TerminalSearchResult) => void): IDisposable;
  dispose(): void;
  /** Drops the cached row text. Call when terminal output changed. */
  invalidate(): void;
}

interface Match {
  row: number;
  col: number;
}

export function createTerminalSearch(
  term: Pick<Terminal, "buffer" | "rows" | "getViewportY" | "scrollToLine" | "scrollToBottom" | "clearSelection">,
  /** Highlights one match: absolute row, first and last column (inclusive). */
  highlight: (absRow: number, startCol: number, endCol: number) => void,
): TerminalSearch {
  const listeners = new Set<(r: TerminalSearchResult) => void>();
  let lastQuery = "";
  let index = -1;
  // Lowered text of every buffer row, read once and reused across searches. Reading every row
  // on each keystroke is what made find slow on a long scrollback.
  let rowsCache: string[] | null = null;

  const fire = (r: TerminalSearchResult) => {
    for (const cb of [...listeners]) cb(r);
  };

  const rowTexts = (): string[] => {
    if (rowsCache) return rowsCache;
    const buf = term.buffer.active;
    const rows: string[] = [];
    for (let y = 0; y < buf.length; y++) {
      const line = buf.getLine(y);
      rows.push(line ? line.translateToString(false).toLowerCase() : "");
    }
    rowsCache = rows;
    return rows;
  };

  // Matches never cross a row. Columns are string indexes, so a wide char counts as one column
  // and a match after one is highlighted a column early.
  const collect = (needle: string): Match[] => {
    const rows = rowTexts();
    const matches: Match[] = [];
    for (let y = 0; y < rows.length; y++) {
      const text = rows[y];
      let from = 0;
      for (;;) {
        const at = text.indexOf(needle, from);
        if (at === -1) break;
        matches.push({ row: y, col: at });
        from = at + needle.length;
      }
    }
    return matches;
  };

  const none = (): boolean => {
    term.clearSelection();
    lastQuery = "";
    index = -1;
    fire({ resultCount: 0, resultIndex: -1 });
    return false;
  };

  const find = (query: string, dir: 1 | -1): boolean => {
    if (query.trim() === "") return none();
    const needle = query.toLowerCase();
    const matches = collect(needle);
    if (matches.length === 0) return none();

    const buf = term.buffer.active;
    if (query !== lastQuery) {
      const topRow = buf.length - term.rows - Math.floor(term.getViewportY());
      const first = matches.findIndex((m) => m.row >= topRow);
      index = first === -1 ? 0 : first;
    } else {
      const cur = index >= 0 && index < matches.length ? index : dir === 1 ? -1 : 0;
      index = (cur + dir + matches.length) % matches.length;
    }
    lastQuery = query;

    const m = matches[index];
    if (m.row >= buf.length - term.rows) term.scrollToBottom();
    // scrollToLine takes lines above the bottom (0 = bottom), not an absolute row; this puts the match on the top row.
    else term.scrollToLine(buf.length - term.rows - m.row);
    highlight(m.row, m.col, m.col + needle.length - 1);
    fire({ resultCount: matches.length, resultIndex: index });
    return true;
  };

  return {
    findNext: (q) => find(q, 1),
    findPrevious: (q) => find(q, -1),
    clear() {
      term.clearSelection();
      lastQuery = "";
      index = -1;
      rowsCache = null;
      fire({ resultCount: 0, resultIndex: -1 });
    },
    onDidChangeResults(cb) {
      listeners.add(cb);
      return { dispose: () => void listeners.delete(cb) };
    },
    dispose() {
      listeners.clear();
    },
    invalidate() {
      rowsCache = null;
    },
  };
}
