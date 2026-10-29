import type { ITheme } from "ghostty-web";

/**
 * Terminal appearance: the bridge between the server's resolved appearance
 * block and ghostty-web.
 *
 * The server owns precedence (Ghostty config vs ocode settings) and reports the
 * result as `appearance` on GET /api/config/terminal. This module only maps that
 * wire shape onto the renderer's `ITheme` plus the cell-metric adjustments, so
 * the panel and the Settings UI cannot disagree about what "brightRed" or
 * "10% taller" means.
 */

/** A cell-size DELTA on the font's natural metrics (Ghostty's adjust-cell-*). */
export interface CellAdjust {
  /** Percentage of the measured cell to add (10 = 10% taller). */
  percent?: number;
  /** Pixels to add on top of the percentage. */
  px?: number;
}

/** Theme colours as the server reports them (snake_case, Ghostty terminology). */
export interface TerminalThemeColors {
  foreground?: string;
  background?: string;
  cursor?: string;
  cursor_text?: string;
  selection_background?: string;
  selection_foreground?: string;
  /** ANSI 0-15, "#rrggbb". */
  palette?: string[];
}

export type TerminalAppearanceSource = "ghostty" | "ocode";

/** The resolved appearance block from GET /api/config/terminal. */
export interface TerminalAppearance {
  /** Which source actually applied. */
  source: TerminalAppearanceSource;
  /** What the setting asked for; differs from `source` when Ghostty had no config. */
  requested: TerminalAppearanceSource;
  font_family: string;
  font_size: number;
  cell_width_adjust: CellAdjust;
  cell_height_adjust: CellAdjust;
  theme_name?: string;
  light?: TerminalThemeColors;
  dark?: TerminalThemeColors;
  /** Non-fatal problems with the CONFIGURED appearance (e.g. a theme that does
   *  not resolve). Never blocks a save. */
  errors?: string[];
  /** The PERSISTED ocode-source values (not the resolved ones). Present so the
   *  Settings UI can keep editing them while Ghostty is the active source. */
  settings?: TerminalAppearanceSettings;
}

/** The ocode-source values as stored in ocodeconfig.json (absolute percentages). */
export interface TerminalAppearanceSettings {
  font_family: string;
  font_size: number;
  cell_width_percent: number;
  cell_height_percent: number;
  theme?: string;
}

/** A Ghostty keybind the server parsed (applied subset only — see ghosttyKeybinds.ts). */
export interface GhosttyKeybind {
  chord: string;
  action: string;
  arg?: string;
  unbound?: boolean;
  global?: boolean;
}

export interface GhosttyIgnoredKey {
  key: string;
  value?: string;
  reason: string;
}

/** Report of what the server read from the user's Ghostty configuration. */
export interface GhosttyReport {
  found: boolean;
  paths?: string[];
  errors?: string[];
  font_family?: string;
  font_size?: number;
  cell_width?: CellAdjust;
  cell_height?: CellAdjust;
  theme_name?: string;
  keybinds?: GhosttyKeybind[];
  ignored?: GhosttyIgnoredKey[];
}

/** The client's own defaults, mirroring config.DefaultTerminal* on the server.
 *
 * They live HERE (not in useTerminalConfig) so this module has no runtime
 * dependency on a hook: dozens of panel tests mock that hook partially, and a
 * missing export would crash every one of them. The hook re-exports them.
 */
export const DEFAULT_TERMINAL_FONT_FAMILY = "ui-monospace, SFMono-Regular, Menlo, monospace";
/** One point smaller than the pre-ghostty-web default, paired with the extra
 *  cell height below: same visual weight, airier rows. */
export const DEFAULT_TERMINAL_FONT_SIZE = 12;
/** Extra cell height applied when ocode's own settings are in use. */
export const DEFAULT_TERMINAL_CELL_HEIGHT: CellAdjust = { percent: 10 };

/** Fallback appearance for a client that has not loaded the API yet. */
export const DEFAULT_TERMINAL_APPEARANCE: TerminalAppearance = {
  source: "ocode",
  requested: "ocode",
  font_family: DEFAULT_TERMINAL_FONT_FAMILY,
  font_size: DEFAULT_TERMINAL_FONT_SIZE,
  cell_width_adjust: {},
  cell_height_adjust: DEFAULT_TERMINAL_CELL_HEIGHT,
};

/** The app's current polarity, from the applied `--background` HSL triplet.
 *
 * This is deliberately NOT `getEditorAppearance()`: that value is the file
 * editor's own light/dark OVERRIDE, which must never leak into the terminal.
 * The app palette is a single global choice applied as CSS variables, so its
 * lightness is the one honest signal. Falls back to dark (the app's default).
 */
export function appPolarity(): "light" | "dark" {
  try {
    const bg = getComputedStyle(document.documentElement).getPropertyValue("--background").trim();
    const match = /(\d+(?:\.\d+)?)%\s*$/.exec(bg);
    if (match) return Number.parseFloat(match[1]) > 50 ? "light" : "dark";
  } catch (err) {
    console.warn("[terminalAppearance] failed to read app polarity", err);
  }
  return "dark";
}

const PALETTE_KEYS = [
  "black",
  "red",
  "green",
  "yellow",
  "blue",
  "magenta",
  "cyan",
  "white",
  "brightBlack",
  "brightRed",
  "brightGreen",
  "brightYellow",
  "brightBlue",
  "brightMagenta",
  "brightCyan",
  "brightWhite",
] as const;

/**
 * Maps the server's theme colours onto ghostty-web's ITheme for one polarity.
 *
 * Returns undefined when the appearance carries no colours at all, so the panel
 * keeps the renderer's built-in palette rather than receiving an empty object
 * that would silently override it with "undefined" entries.
 */
export function resolveTerminalTheme(
  appearance: TerminalAppearance | null | undefined,
  polarity: "light" | "dark" = appPolarity(),
): ITheme | undefined {
  const colors = polarity === "light" ? appearance?.light : appearance?.dark;
  if (!colors) return undefined;
  const theme: ITheme = {};
  if (colors.foreground) theme.foreground = colors.foreground;
  if (colors.background) theme.background = colors.background;
  if (colors.cursor) theme.cursor = colors.cursor;
  if (colors.cursor_text) theme.cursorAccent = colors.cursor_text;
  if (colors.selection_background) theme.selectionBackground = colors.selection_background;
  if (colors.selection_foreground) theme.selectionForeground = colors.selection_foreground;
  const palette = colors.palette;
  if (palette && palette.length === PALETTE_KEYS.length) {
    PALETTE_KEYS.forEach((key, i) => {
      if (palette[i]) theme[key] = palette[i];
    });
  }
  return Object.keys(theme).length > 0 ? theme : undefined;
}

/**
 * A stable identity for "the theme in effect", so panels can tell a real theme
 * change from a re-render. JSON is fine: themes are small and compared rarely.
 */
export function terminalThemeKey(theme: ITheme | undefined): string {
  return theme ? JSON.stringify(theme) : "default";
}

export interface CellMetrics {
  width: number;
  height: number;
  baseline: number;
}

/**
 * Scales a measured cell by a Ghostty-style adjustment.
 *
 * Result values are INTEGERS on purpose. The renderer multiplies metrics by the
 * cell count to size the canvas (`cols * width`), and a fractional width makes
 * the CSS size and the backing store disagree by a fraction of a pixel per cell
 * — which accumulates into mouse/selection hit-testing drift on wide terminals.
 */
export function scaleCellMetrics(
  base: CellMetrics,
  widthAdjust: CellAdjust | undefined,
  heightAdjust: CellAdjust | undefined,
): CellMetrics {
  const width = scaleOne(base.width, widthAdjust);
  const height = scaleOne(base.height, heightAdjust);
  // Ghostty centers the glyph vertically in the taller cell; doing the same
  // here is what makes extra line spacing look like padding rather than text
  // sliding to the top of each row. The baseline stays inside the cell even
  // for a shrink clamp, or the text would be drawn outside its own row.
  const baseline = Math.min(height, Math.max(1, Math.round(base.baseline + (height - base.height) / 2)));
  return { width, height, baseline };
}

function scaleOne(base: number, adjust: CellAdjust | undefined): number {
  const percent = adjust?.percent ?? 0;
  const px = adjust?.px ?? 0;
  const next = base + base * (percent / 100) + px;
  return Math.max(1, Math.round(next));
}

/** Converts "#rrggbb" to 0xRRGGBB for the buffer serializer's default-colour test. */
export function themeColorToInt(hex: string | undefined, fallback: number): number {
  if (!hex) return fallback;
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return fallback;
  return Number.parseInt(m[1], 16);
}
