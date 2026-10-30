// A1 addressing helpers. Pure functions: no Office.js, fully unit tested.

export const MAX_COL = 16384; // XFD
export const MAX_ROW = 1048576;

export interface Rect {
  sheet?: string;
  /** 1-based, inclusive */
  c1: number;
  r1: number;
  c2: number;
  r2: number;
}

export function colToLetters(n: number): string {
  let s = "";
  for (let x = n; x > 0; x = Math.floor((x - 1) / 26)) s = String.fromCharCode(65 + ((x - 1) % 26)) + s;
  return s;
}

export function lettersToCol(s: string): number {
  let n = 0;
  for (const ch of s.toUpperCase()) n = n * 26 + (ch.charCodeAt(0) - 64);
  return n;
}

function splitSheet(addr: string): { sheet?: string; rest: string } {
  const bang = addr.lastIndexOf("!");
  if (bang < 0) return { rest: addr };
  let sheet = addr.slice(0, bang);
  if (sheet.startsWith("'") && sheet.endsWith("'")) sheet = sheet.slice(1, -1).replace(/''/g, "'");
  return { sheet, rest: addr.slice(bang + 1) };
}

/**
 * Parse A1, A1:B3, $A$1:$B$3, B:B, 3:5, optionally sheet-qualified. Whole-column and whole-row
 * references come back spanning the full sheet; callers clamp to the used range.
 */
export function parseAddress(addr: string): Rect | null {
  const { sheet, rest } = splitSheet(addr.trim());
  const body = rest.replace(/\$/g, "").toUpperCase();
  let m = /^([A-Z]{1,3})(\d+)(?::([A-Z]{1,3})(\d+))?$/.exec(body);
  if (m) {
    const c1 = lettersToCol(m[1]);
    const r1 = Number(m[2]);
    const c2 = m[3] ? lettersToCol(m[3]) : c1;
    const r2 = m[4] ? Number(m[4]) : r1;
    if (c1 < 1 || r1 < 1 || c2 < 1 || r2 < 1 || c1 > MAX_COL || c2 > MAX_COL || r1 > MAX_ROW || r2 > MAX_ROW) return null;
    return norm({ sheet, c1, r1, c2, r2 });
  }
  m = /^([A-Z]{1,3}):([A-Z]{1,3})$/.exec(body);
  if (m) {
    const c1 = lettersToCol(m[1]);
    const c2 = lettersToCol(m[2]);
    if (c1 > MAX_COL || c2 > MAX_COL) return null;
    return norm({ sheet, c1, r1: 1, c2, r2: MAX_ROW });
  }
  m = /^(\d+):(\d+)$/.exec(body);
  if (m) {
    const r1 = Number(m[1]);
    const r2 = Number(m[2]);
    if (r1 < 1 || r2 < 1 || r1 > MAX_ROW || r2 > MAX_ROW) return null;
    return norm({ sheet, c1: 1, r1, c2: MAX_COL, r2 });
  }
  return null;
}

function norm(r: Rect): Rect {
  return { ...r, c1: Math.min(r.c1, r.c2), c2: Math.max(r.c1, r.c2), r1: Math.min(r.r1, r.r2), r2: Math.max(r.r1, r.r2) };
}

export function formatRect(r: Rect, withSheet = false): string {
  const a = `${colToLetters(r.c1)}${r.r1}`;
  const b = `${colToLetters(r.c2)}${r.r2}`;
  const core = a === b ? a : `${a}:${b}`;
  return withSheet && r.sheet ? `${quoteSheet(r.sheet)}!${core}` : core;
}

export function quoteSheet(name: string): string {
  return /^[A-Za-z_][A-Za-z0-9_.]*$/.test(name) ? name : `'${name.replace(/'/g, "''")}'`;
}

export const rows = (r: Rect) => r.r2 - r.r1 + 1;
export const cols = (r: Rect) => r.c2 - r.c1 + 1;
export const cellCount = (r: Rect) => rows(r) * cols(r);
export const intersects = (a: Rect, b: Rect) => a.c1 <= b.c2 && b.c1 <= a.c2 && a.r1 <= b.r2 && b.r1 <= a.r2;
export const contains = (outer: Rect, inner: Rect) => outer.c1 <= inner.c1 && outer.c2 >= inner.c2 && outer.r1 <= inner.r1 && outer.r2 >= inner.r2;

/** Clamp a rect (possibly whole-column/row) to `bounds`. Returns null if nothing remains. */
export function clampTo(r: Rect, bounds: Rect): Rect | null {
  const c1 = Math.max(r.c1, bounds.c1);
  const c2 = Math.min(r.c2, bounds.c2);
  const r1 = Math.max(r.r1, bounds.r1);
  const r2 = Math.min(r.r2, bounds.r2);
  return c1 > c2 || r1 > r2 ? null : { sheet: r.sheet ?? bounds.sheet, c1, r1, c2, r2 };
}

export function cellAddress(col: number, row: number): string {
  return `${colToLetters(col)}${row}`;
}

/** Shift the relative references in a formula as if it were copied by (dRow, dCol). */
export function shiftFormula(formula: string, dRow: number, dCol: number): string {
  let out = "";
  let i = 0;
  while (i < formula.length) {
    const ch = formula[i];
    if (ch === '"') {
      // string literal, "" is an escaped quote
      let j = i + 1;
      while (j < formula.length && !(formula[j] === '"' && formula[j + 1] !== '"')) j += formula[j] === '"' ? 2 : 1;
      out += formula.slice(i, j + 1);
      i = j + 1;
      continue;
    }
    if (ch === "'") {
      // quoted sheet name
      let j = i + 1;
      while (j < formula.length && !(formula[j] === "'" && formula[j + 1] !== "'")) j += formula[j] === "'" ? 2 : 1;
      out += formula.slice(i, j + 1);
      i = j + 1;
      continue;
    }
    const prev = i > 0 ? formula[i - 1] : "";
    if (/[$A-Za-z]/.test(ch) && !/[A-Za-z0-9_.]/.test(prev)) {
      const m = /^(\$?)([A-Za-z]{1,3})(\$?)(\d+)(?![A-Za-z0-9_(])/.exec(formula.slice(i));
      if (m) {
        const col = lettersToCol(m[2]);
        const row = Number(m[4]);
        if (col <= MAX_COL && row <= MAX_ROW) {
          const nc = m[1] ? col : col + dCol;
          const nr = m[3] ? row : row + dRow;
          out += nc < 1 || nr < 1 || nc > MAX_COL || nr > MAX_ROW ? "#REF!" : `${m[1]}${colToLetters(nc)}${m[3]}${nr}`;
          i += m[0].length;
          continue;
        }
      }
      const w = /^[A-Za-z_][A-Za-z0-9_.]*/.exec(formula.slice(i));
      if (w) {
        out += w[0]; // function or defined name: copy verbatim
        i += w[0].length;
        continue;
      }
    }
    out += ch;
    i++;
  }
  return out;
}
