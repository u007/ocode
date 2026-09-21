import type { ITerminalInitOnlyOptions, ITerminalOptions } from "@xterm/xterm";

export interface TerminalOptionsInput {
  /** Scrollback lines retained by this terminal (from user settings). */
  scrollbackLines: number;
  fontFamily: string;
  fontSize: number;
  /**
   * Buffer geometry to construct at (from the persisted snapshot). When both
   * cols and rows are present the terminal is created at the size the buffer
   * was serialized at, so restoring it doesn't reflow/garble the text before
   * the ResizeObserver-driven fit() ever runs. Legacy saved buffers (or no
   * buffer) carry no cols/rows and fall back to xterm's defaults.
   */
  savedBuffer?: { cols?: number; rows?: number } | null;
}

/**
 * Builds the xterm constructor options for a terminal panel.
 *
 * Exported (and kept free of React/refs) so a regression test can construct a
 * REAL terminal with the exact production options and exercise the real
 * SearchAddon — the component tests mock @xterm/addon-search, which is how a
 * "find never matches anything" regression previously slipped through.
 */
export function buildTerminalOptions({
  scrollbackLines,
  fontFamily,
  fontSize,
  savedBuffer,
}: TerminalOptionsInput): ITerminalOptions & ITerminalInitOnlyOptions {
  return {
    cursorBlink: true,
    scrollback: scrollbackLines,
    // Required by SearchAddon: creating the find-match highlight decorations
    // calls Terminal.registerDecoration(), which xterm gates behind
    // allowProposedApi. Without it, findNext() throws internally
    // ("You must set the allowProposedApi option to true to use proposed
    // API") before it can select a match or report results, so the find bar
    // is stuck on "No matches" no matter what the buffer contains.
    allowProposedApi: true,
    fontFamily,
    fontSize,
    // Wheel/trackpad scrolled ~1 row per notch at the default
    // scrollSensitivity (1), which feels very slow on large scrollback.
    // 3x normal + 5x Alt-held fast scroll, instant (no smooth animation
    // lag) keeps long-history navigation responsive.
    scrollSensitivity: 3,
    fastScrollSensitivity: 5,
    smoothScrollDuration: 0,
    theme: { background: "#18181b", foreground: "#e4e4e7" },
    ...(savedBuffer?.cols && savedBuffer?.rows
      ? { cols: savedBuffer.cols, rows: savedBuffer.rows }
      : {}),
  };
}
