import * as XLSX from "xlsx";
import {
  BooleanNumber,
  BorderStyleTypes,
  HorizontalAlign,
  VerticalAlign,
  WrapStrategy,
  type IBorderData,
  type IColorStyle,
  type IStyleData,
} from "@univerjs/core";
import type { Alignment, BorderStyle, CellValue, Fill, Font, Borders } from "exceljs";

/**
 * Pure helpers that turn ExcelJS style/value objects into Univer cell styles
 * and display text for the spreadsheet preview. No DOM, no I/O.
 */

/**
 * Excel theme-index order: 0=lt1, 1=dk1, 2=lt2, 3=dk2 (Excel swaps the
 * light/dark pairs relative to their XML order), 4..9=accent1..6, 10=hlink,
 * 11=folHlink.
 */
const THEME_SLOTS = [
  "lt1",
  "dk1",
  "lt2",
  "dk2",
  "accent1",
  "accent2",
  "accent3",
  "accent4",
  "accent5",
  "accent6",
  "hlink",
  "folHlink",
] as const;

/** Office 2013+ default colours. Used only when a workbook has no theme part. */
export const DEFAULT_THEME_PALETTE: readonly string[] = [
  "#ffffff",
  "#000000",
  "#e7e6e6",
  "#44546a",
  "#4472c4",
  "#ed7d31",
  "#a5a5a5",
  "#ffc000",
  "#5b9bd5",
  "#70ad47",
  "#0563c1",
  "#954f72",
];

/**
 * Excel's legacy indexed palette (BIFF8 default, indices 0-63). Indices 64
 * (system foreground) and 65 (system background) are handled in resolveColor.
 */
const INDEXED_PALETTE: readonly string[] = [
  "#000000", "#ffffff", "#ff0000", "#00ff00", "#0000ff", "#ffff00", "#ff00ff", "#00ffff",
  "#000000", "#ffffff", "#ff0000", "#00ff00", "#0000ff", "#ffff00", "#ff00ff", "#00ffff",
  "#800000", "#008000", "#000080", "#808000", "#800080", "#008080", "#c0c0c0", "#808080",
  "#9999ff", "#993366", "#ffffcc", "#ccffff", "#660066", "#ff8080", "#0066cc", "#ccccff",
  "#000080", "#ff00ff", "#ffff00", "#00ffff", "#800080", "#800000", "#008080", "#0000ff",
  "#00ccff", "#ccffff", "#ccffcc", "#ffff99", "#99ccff", "#ff99cc", "#cc99ff", "#ffcc99",
  "#3366ff", "#33cccc", "#99cc00", "#ffcc00", "#ff9900", "#ff6600", "#666699", "#969696",
  "#003366", "#339966", "#003300", "#333300", "#993300", "#993366", "#333399", "#333333",
];

/** Excel's "automatic" colour for borders and text on a filled cell. */
const AUTOMATIC_BLACK = "#000000";

/**
 * Reads the 12 colours of `<a:clrScheme>` from theme1.xml and returns them in
 * Excel theme-index order. Each slot is looked up by element name, never by
 * position: the XML lists dk1, lt1, dk2, lt2 while the index order is lt1,
 * dk1, lt2, dk2. A present-but-malformed scheme throws; an absent one falls
 * back to the Office default palette, which is what Excel itself does. This
 * fallback was explicitly approved by the user on 2026-11-03.
 */
export function parseThemePalette(themeXml: string | undefined): string[] {
  if (themeXml === undefined) return [...DEFAULT_THEME_PALETTE];
  return THEME_SLOTS.map((slot) => {
    const block = new RegExp(`<(?:a:)?${slot}>([\\s\\S]*?)</(?:a:)?${slot}>`).exec(themeXml);
    if (!block) throw new Error(`theme1.xml has no <${slot}> colour`);
    const hex = /(?:lastClr|srgbClr val)="([0-9a-fA-F]{6})"/.exec(block[1]);
    if (!hex) throw new Error(`theme1.xml <${slot}> has no 6-digit colour`);
    return `#${hex[1].toLowerCase()}`;
  });
}

export interface ColorRef {
  argb?: string;
  theme?: number;
  indexed?: number;
  tint?: number;
}

/**
 * Excel tint: positive lightens toward white, negative darkens toward black.
 * Applied per RGB channel. This approximates Excel's HSL luminance shift; for
 * the common tints it lands within a unit or two per channel.
 */
function applyTint(hex: string, tint: number | undefined): string {
  if (tint === undefined || tint === 0) return hex;
  const channels = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const shifted = channels.map((c) => (tint > 0 ? c + (255 - c) * tint : c * (1 + tint)));
  const out = shifted.map((v) => Math.round(Math.min(255, Math.max(0, v))).toString(16).padStart(2, "0"));
  return `#${out.join("")}`;
}

/**
 * Resolves an ExcelJS colour to "#rrggbb". Returns undefined for an absent
 * colour and for the indexed "system foreground" (64), which Excel draws as
 * automatic. Indexed 65 (system background) is white. Anything else that
 * cannot be resolved throws, so the caller can name the cell.
 */
export function resolveColor(color: ColorRef | undefined, palette: readonly string[]): string | undefined {
  if (color === undefined) return undefined;
  if (color.argb !== undefined) {
    if (!/^[0-9a-fA-F]{8}$/.test(color.argb)) throw new Error(`invalid ARGB colour "${color.argb}"`);
    return applyTint(`#${color.argb.slice(2).toLowerCase()}`, color.tint);
  }
  if (color.theme !== undefined) {
    const base = palette[color.theme];
    if (base === undefined) throw new Error(`theme index ${color.theme} is outside the ${palette.length}-colour palette`);
    return applyTint(base, color.tint);
  }
  if (color.indexed !== undefined) {
    if (color.indexed === 64) return undefined;
    if (color.indexed === 65) return "#ffffff";
    const base = INDEXED_PALETTE[color.indexed];
    if (base === undefined) throw new Error(`indexed colour ${color.indexed} is outside Excel's 0-65 palette`);
    return applyTint(base, color.tint);
  }
  return undefined;
}

const BORDER_UNIVER: Partial<Record<BorderStyle, BorderStyleTypes>> = {
  hair: BorderStyleTypes.HAIR,
  dotted: BorderStyleTypes.DOTTED,
  dashed: BorderStyleTypes.DASHED,
  dashDot: BorderStyleTypes.DASH_DOT,
  dashDotDot: BorderStyleTypes.DASH_DOT_DOT,
  slantDashDot: BorderStyleTypes.SLANT_DASH_DOT,
  thin: BorderStyleTypes.THIN,
  mediumDashed: BorderStyleTypes.MEDIUM_DASHED,
  mediumDashDot: BorderStyleTypes.MEDIUM_DASH_DOT,
  mediumDashDotDot: BorderStyleTypes.MEDIUM_DASH_DOT_DOT,
  medium: BorderStyleTypes.MEDIUM,
  thick: BorderStyleTypes.THICK,
  double: BorderStyleTypes.DOUBLE,
};

const BORDER_SIDES = ["top", "right", "bottom", "left"] as const;
const BORDER_KEY: Record<(typeof BORDER_SIDES)[number], "t" | "r" | "b" | "l"> = {
  top: "t",
  right: "r",
  bottom: "b",
  left: "l",
};

const HORIZONTAL_ALIGN: Record<NonNullable<Alignment["horizontal"]>, HorizontalAlign> = {
  left: HorizontalAlign.LEFT,
  // Univer has no "fill" alignment; left is the closest rendering. Approved by the user on 2026-11-03.
  fill: HorizontalAlign.LEFT,
  center: HorizontalAlign.CENTER,
  centerContinuous: HorizontalAlign.CENTER,
  right: HorizontalAlign.RIGHT,
  justify: HorizontalAlign.JUSTIFIED,
  distributed: HorizontalAlign.DISTRIBUTED,
};

const VERTICAL_ALIGN: Record<NonNullable<Alignment["vertical"]>, VerticalAlign> = {
  top: VerticalAlign.TOP,
  middle: VerticalAlign.MIDDLE,
  bottom: VerticalAlign.BOTTOM,
  distributed: VerticalAlign.MIDDLE,
  justify: VerticalAlign.MIDDLE,
};

export interface XlsxCellStyle {
  font?: Partial<Font>;
  fill?: Fill;
  border?: Partial<Borders>;
  alignment?: Partial<Alignment>;
}

/** Font colour that was set explicitly. Theme 1 (dk1) is Excel's default text colour, so it is not explicit. */
function explicitFontColor(color: ColorRef | undefined, palette: readonly string[]): string | undefined {
  if (color === undefined) return undefined;
  if (color.theme === 1 && (color.tint === undefined || color.tint === 0)) return undefined;
  return resolveColor(color, palette);
}

function rgb(hex: string): IColorStyle {
  return { rgb: hex };
}

/**
 * Univer cell style from an ExcelJS cell's font, fill, borders and alignment.
 *
 * Unset properties are left out so the cell inherits from the sheet. A solid
 * fill with no explicit font colour gets black text, so text stays readable on
 * the fill. Only solid pattern fills are drawn: gradients and other patterns
 * are not rendered. Unknown border styles throw; the caller adds the cell.
 *
 * `numeric` is true for number and date values. Excel right-aligns those under
 * "General" horizontal alignment; text is left-aligned. Text that does not fit
 * overflows into empty neighbours, as in Excel, unless it is wrapped.
 */
export function univerCellStyle(style: XlsxCellStyle, palette: readonly string[], numeric: boolean): IStyleData {
  const out: IStyleData = {};

  const background =
    style.fill?.type === "pattern" && style.fill.pattern === "solid" ? resolveColor(style.fill.fgColor, palette) : undefined;
  if (background !== undefined) out.bg = rgb(background);

  if (style.font !== undefined) applyFont(out, style.font);
  const fontColor = style.font === undefined ? undefined : explicitFontColor(style.font.color, palette);
  if (fontColor !== undefined) out.cl = rgb(fontColor);
  else if (background !== undefined) out.cl = rgb(AUTOMATIC_BLACK);

  if (style.border !== undefined) applyBorders(out, style.border, palette);

  if (style.alignment?.horizontal !== undefined) out.ht = HORIZONTAL_ALIGN[style.alignment.horizontal];
  else if (numeric) out.ht = HorizontalAlign.RIGHT;

  out.vt = style.alignment?.vertical === undefined ? VerticalAlign.BOTTOM : VERTICAL_ALIGN[style.alignment.vertical];
  out.tb = style.alignment?.wrapText === true ? WrapStrategy.WRAP : WrapStrategy.OVERFLOW;
  return out;
}

/** Font face, size and decorations. The font colour is resolved by the caller. */
function applyFont(out: IStyleData, font: Partial<Font>): void {
  if (font.name !== undefined) out.ff = font.name;
  if (font.size !== undefined) out.fs = font.size;
  if (font.bold === true) out.bl = BooleanNumber.TRUE;
  if (font.italic === true) out.it = BooleanNumber.TRUE;
  const underlined = font.underline !== undefined && font.underline !== false && font.underline !== "none";
  if (underlined) out.ul = { s: BooleanNumber.TRUE };
  if (font.strike === true) out.st = { s: BooleanNumber.TRUE };
}

function applyBorders(out: IStyleData, border: Partial<Borders>, palette: readonly string[]): void {
  const bd: IBorderData = {};
  for (const side of BORDER_SIDES) {
    const edge = border[side];
    if (edge?.style === undefined) continue;
    const s = BORDER_UNIVER[edge.style];
    if (s === undefined) throw new Error(`unknown border style "${edge.style}"`);
    // An unset border colour is Excel's "automatic", which renders as black.
    const color = resolveColor(edge.color, palette);
    bd[BORDER_KEY[side]] = { s, cl: rgb(color === undefined ? AUTOMATIC_BLACK : color) };
  }
  if (Object.keys(bd).length > 0) out.bd = bd;
}

/** Excel column width (in characters) to pixels. Undefined is Excel's default 8.43 characters. */
export function colWidthPx(widthChars: number | undefined): number {
  const chars = widthChars === undefined ? 8.43 : widthChars;
  return Math.round(chars * 7 + 5);
}

/** Excel row height (points) to CSS pixels at 96 dpi. */
export function rowHeightPx(points: number): number {
  return (points * 4) / 3;
}

/**
 * True for a number or date value, and for a formula whose cached result is one.
 * Excel right-aligns these under General horizontal alignment.
 */
export function isNumericValue(value: CellValue): boolean {
  if (typeof value === "number" || value instanceof Date) return true;
  if (typeof value === "object" && value !== null && "result" in value) return isNumericValue(value.result);
  return false;
}

const MS_PER_DAY = 24 * 3600 * 1000;
/** Days from 1899-12-30 (the day-zero ExcelJS and Excel share after Feb 1900) to 1970-01-01. */
const EPOCH_1970_SERIAL = 25569;
/** Days between the 1900 and 1904 date systems. */
const DATE_1904_OFFSET_DAYS = 1462;

/**
 * Excel serial for a JS Date, in the workbook's own date system. This is the
 * exact inverse of ExcelJS's read conversion (`excelToDate`, which adds the
 * 1904 offset and nothing else), so a value read from a file and written back
 * returns the stored serial.
 *
 * The Lotus 1900 leap-year bug is not corrected here. Excel stores 60 for the
 * phantom 29 Feb 1900; ExcelJS's date for 60 is the real 28 Feb, and SSF draws
 * the phantom day itself when it formats the serial. Correcting it here would
 * move every early-1900 date one serial away from the file.
 */
export function excelSerial(d: Date, date1904: boolean): number {
  const ms = d.getTime();
  if (Number.isNaN(ms)) throw new Error("invalid Date value");
  let serial = EPOCH_1970_SERIAL + ms / MS_PER_DAY;
  if (date1904) serial -= DATE_1904_OFFSET_DAYS;
  // Serial 0 (1899-12-30, or 1904-01-01 in the 1904 system) is valid: it is
  // where time-only cells (0.5 = noon) land. Anything below it is not a date.
  if (serial < 0) throw new Error(`date ${d.toISOString()} is before the ${date1904 ? "1904" : "1900"} date system's epoch`);
  return serial;
}

/** Text of a caught value, for error messages. */
export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * Display text for a number under its number format. A format SheetJS cannot
 * parse throws (it does not return the raw number), and the error names the
 * format and value. Absent format means Excel's "General".
 */
export function formatNumber(value: number, numFmt: string | undefined, date1904: boolean): string {
  const fmt = numFmt === undefined ? "General" : numFmt;
  try {
    return XLSX.SSF.format(fmt, value, { date1904 });
  } catch (e) {
    throw new Error(`number format "${fmt}" cannot format ${value}: ${errorMessage(e)}`);
  }
}

/**
 * Display text for a cell value, formatted with its number format.
 * Formula and shared-formula cells show their cached result. Rich text and
 * hyperlinks show their text. Error values show their Excel error code.
 */
export function formatCell(value: CellValue, numFmt: string | undefined, date1904: boolean): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "boolean") return value ? "TRUE" : "FALSE";
  if (typeof value === "number") return formatNumber(value, numFmt, date1904);
  if (value instanceof Date) return formatNumber(excelSerial(value, date1904), numFmt, date1904);
  if ("richText" in value) return value.richText.map((run) => run.text).join("");
  if ("hyperlink" in value) return value.text;
  if ("error" in value) return value.error;
  // Formula and shared-formula cells: the cached result carries the value.
  if (value.result === undefined) return "";
  return formatCell(value.result, numFmt, date1904);
}
