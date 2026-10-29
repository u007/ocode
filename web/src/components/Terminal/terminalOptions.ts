import type { Ghostty, ITerminalOptions, ITheme } from "ghostty-web";
import { themeColorToInt } from "./terminalAppearance";

/**
 * Theme colours as 0xRRGGBB, for the buffer serializer: ghostty-web resolves every
 * cell to RGB, so these are the only way to recognise "default colour". Keep in
 * sync with DEFAULT_TERMINAL_THEME below.
 */
export const TERMINAL_THEME_DEFAULTS = { foreground: 0xe4e4e7, background: 0x18181b };

/**
 * ocode's own palette, used when the resolved appearance carries no theme. It is
 * also the value a LIVE terminal must be reset to when the appearance switches
 * from a Ghostty theme back to "no theme" — otherwise the old palette would stay
 * on screen until the panel remounts.
 */
export const DEFAULT_TERMINAL_THEME: ITheme = { background: "#18181b", foreground: "#e4e4e7" };

export interface TerminalOptionsInput {
  /** Scrollback lines retained by this terminal (from user settings). */
  scrollbackLines: number;
  fontFamily: string;
  fontSize: number;
  /**
   * Resolved theme (Ghostty config or ocode settings — see terminalAppearance).
   * Omitted keeps ocode's built-in palette. ghostty-web fixes the wasm colour
   * palette at construction, which is why the theme is passed here rather than
   * applied afterwards: a new terminal must never need a rebuild-and-replay.
   */
  theme?: ITheme;
  /**
   * Buffer geometry to construct at (from the persisted snapshot). When both
   * cols and rows are present the terminal is created at the size the buffer
   * was serialized at, so restoring it doesn't reflow/garble the text before
   * the ResizeObserver-driven fit() ever runs. Legacy saved buffers (or no
   * buffer) carry no cols/rows and fall back to the terminal's defaults.
   */
  savedBuffer?: { cols?: number; rows?: number } | null;
  /** The shared libghostty-vt instance (see ghosttyRuntime.ts). */
  ghostty: Ghostty;
}

/**
 * Builds the ghostty-web constructor options for a terminal panel.
 *
 * Exported (and kept free of React/refs) so a regression test can assert the
 * exact production options without mounting the panel.
 */
/** The default foreground/background pair for the serializer, taken from the
 *  ACTIVE theme. The serializer recognises "default colour" cells by RGB, so a
 *  stale pair would tag every plain cell as explicitly coloured. */
export function serializeDefaultsFor(theme: ITheme | undefined): { foreground: number; background: number } {
  return {
    foreground: themeColorToInt(theme?.foreground, TERMINAL_THEME_DEFAULTS.foreground),
    background: themeColorToInt(theme?.background, TERMINAL_THEME_DEFAULTS.background),
  };
}

export function buildTerminalOptions({
  scrollbackLines,
  fontFamily,
  fontSize,
  theme,
  savedBuffer,
  ghostty,
}: TerminalOptionsInput): ITerminalOptions {
  return {
    ghostty,
    cursorBlink: true,
    // Read at construction and again by Terminal.reset(); the panel's scrollback
    // effect sets it and resets to apply a changed setting to an open terminal.
    scrollback: scrollbackLines,
    fontFamily,
    fontSize,
    // Instant scrolling (no smooth animation lag) keeps long-history navigation
    // responsive.
    smoothScrollDuration: 0,
    // A program with mouse tracking on (claude code sends ?1000/?1006 for wheel
    // scrolling) owns plain left drags: terminalMouse.ts reports them to the pty
    // instead of letting ghostty-web select. Shift+drag or Alt/Option+drag is
    // left to ghostty-web so text can still be selected under such a program —
    // the same convention on every platform. (xterm's scrollSensitivity and
    // macOptionClickForcesSelection options do not exist here.)
    theme: theme ?? DEFAULT_TERMINAL_THEME,
    ...(savedBuffer?.cols && savedBuffer?.rows
      ? { cols: savedBuffer.cols, rows: savedBuffer.rows }
      : {}),
  };
}
