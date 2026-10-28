import type { Terminal } from "ghostty-web";

// Replacement for xterm's SerializeAddon, used by the localStorage scrollback snapshot
// (terminalPersistence.ts stores the text and replays it with term.write). ghostty-web has no
// serializer, and its cells expose every colour already resolved to 0xRRGGBB, so the only way to
// recognise "default colour" is to compare against the theme colours.

export interface SerializeDefaults {
  foreground: number; // 0xRRGGBB
  background: number; // 0xRRGGBB
}

interface Sgr {
  bold: boolean;
  faint: boolean;
  italic: boolean;
  underline: boolean;
  blink: boolean;
  inverse: boolean;
  invisible: boolean;
  strike: boolean;
  fg: number | null;
  bg: number | null;
}

interface SerCell {
  text: string;
  sgr: Sgr;
}

const DEFAULT_SGR: Sgr = {
  bold: false,
  faint: false,
  italic: false,
  underline: false,
  blink: false,
  inverse: false,
  invisible: false,
  strike: false,
  fg: null,
  bg: null,
};

function isDefaultSgr(s: Sgr): boolean {
  return sameSgr(s, DEFAULT_SGR);
}

function sameSgr(a: Sgr, b: Sgr): boolean {
  return (
    a.bold === b.bold &&
    a.faint === b.faint &&
    a.italic === b.italic &&
    a.underline === b.underline &&
    a.blink === b.blink &&
    a.inverse === b.inverse &&
    a.invisible === b.invisible &&
    a.strike === b.strike &&
    a.fg === b.fg &&
    a.bg === b.bg
  );
}

function sgrCodes(s: Sgr): string {
  const codes: string[] = [];
  if (s.bold) codes.push("1");
  if (s.faint) codes.push("2");
  if (s.italic) codes.push("3");
  if (s.underline) codes.push("4");
  if (s.blink) codes.push("5");
  if (s.inverse) codes.push("7");
  if (s.invisible) codes.push("8");
  if (s.strike) codes.push("9");
  if (s.fg !== null) codes.push(`38;2;${(s.fg >> 16) & 0xff};${(s.fg >> 8) & 0xff};${s.fg & 0xff}`);
  if (s.bg !== null) codes.push(`48;2;${(s.bg >> 16) & 0xff};${(s.bg >> 8) & 0xff};${s.bg & 0xff}`);
  return codes.join(";");
}

export function serializeTerminalBuffer(term: Pick<Terminal, "buffer">, defaults: SerializeDefaults): string {
  const buf = term.buffer.active;
  const nullCell = buf.getNullCell();

  // Physical rows -> cells, keeping the wrap flag so soft-wrapped rows rejoin without a break.
  const rows: { cells: SerCell[]; wrapped: boolean }[] = [];
  for (let y = 0; y < buf.length; y++) {
    const line = buf.getLine(y);
    const cells: SerCell[] = [];
    if (line) {
      for (let x = 0; x < line.length; x++) {
        const cell = line.getCell(x);
        if (!cell) {
          cells.push({ text: " ", sgr: DEFAULT_SGR });
          continue;
        }
        if (cell.getWidth() === 0) continue; // trailing half of a wide char
        const fg = cell.getFgColor();
        const bg = cell.getBgColor();
        const sgr: Sgr = {
          bold: !!cell.isBold(),
          faint: !!cell.isFaint(),
          italic: !!cell.isItalic(),
          underline: !!cell.isUnderline(),
          blink: !!cell.isBlink(),
          inverse: !!cell.isInverse(),
          invisible: !!cell.isInvisible(),
          strike: !!cell.isStrikethrough(),
          fg: fg === defaults.foreground ? null : fg,
          bg: bg === defaults.background ? null : bg,
        };
        // ghostty-web synthesizes cells past the row's stored array with its null cell's colours; they are empty, not coloured.
        if (
          cell.getChars() === "" &&
          cell.getWidth() === 1 &&
          fg === nullCell.getFgColor() &&
          bg === nullCell.getBgColor() &&
          isDefaultSgr({ ...sgr, fg: null, bg: null })
        ) {
          cells.push({ text: " ", sgr: DEFAULT_SGR });
          continue;
        }
        cells.push({ text: cell.getChars() || " ", sgr });
      }
    }
    rows.push({ cells, wrapped: !!line && line.isWrapped });
  }

  // Trim trailing blanks. A row followed by a wrapped continuation is full by definition, so
  // trimming it would shift the continuation's columns on replay.
  for (let y = 0; y < rows.length; y++) {
    const continues = y + 1 < rows.length && rows[y + 1].wrapped;
    if (continues) continue;
    const cells = rows[y].cells;
    let end = cells.length;
    while (end > 0 && cells[end - 1].text === " " && isDefaultSgr(cells[end - 1].sgr)) end--;
    cells.length = end;
  }

  // Drop trailing all-blank rows.
  let last = rows.length;
  while (last > 0 && rows[last - 1].cells.length === 0) last--;

  let out = "";
  let prev: Sgr = DEFAULT_SGR;
  let everEmitted = false;
  for (let y = 0; y < last; y++) {
    if (y > 0 && !rows[y].wrapped) out += "\r\n";
    for (const c of rows[y].cells) {
      if (!sameSgr(prev, c.sgr)) {
        if (!isDefaultSgr(prev)) out += "\x1b[0m";
        const codes = sgrCodes(c.sgr);
        if (codes) {
          out += `\x1b[${codes}m`;
          everEmitted = true;
        }
        prev = c.sgr;
      }
      out += c.text;
    }
  }
  if (everEmitted) out += "\x1b[0m";
  return out;
}
