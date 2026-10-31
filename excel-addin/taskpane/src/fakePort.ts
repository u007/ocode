// In-memory ExcelPort for tests: a sparse grid per sheet, a tiny formula evaluator
// (numbers, cell refs, + - * /, parentheses, SUM, COUNT) and the same observable behaviour the tools rely on.
import { MAX_COL, MAX_ROW, Rect, cellAddress, clampTo, colToLetters, intersects, lettersToCol, parseAddress, shiftFormula } from "./a1";
import type {
  CellValue, ChartInfo, ChartSpec, ExcelPort, FormatSpec, NameInfo, PivotInfo, PivotSpec, RangeData, SheetInfo, TableInfo, ValueType,
} from "./port";
import { chartFootprint } from "./tools/chartpivot";

interface Cell { raw: CellValue; fmt: string }
type Grid = Map<string, Cell>;
const key = (c: number, r: number) => `${c},${r}`;

export class FakePort implements ExcelPort {
  sheets = new Map<string, Grid>();
  hidden = new Set<string>();
  active = "";
  sel: { sheet: string; address: string } | null = null;
  tables: TableInfo[] = [];
  names: NameInfo[] = [];
  charts: ChartInfo[] = [];
  pivots: PivotInfo[] = [];
  settings = new Map<string, string>();
  formats: { sheet: string; address: string; spec: FormatSpec }[] = [];
  calcs = 0;
  /** Operations performed, in order, for assertions about what a tool did NOT do. */
  log: string[] = [];

  addSheet(name: string, rows: CellValue[][] = [], at = "A1"): this {
    const g: Grid = this.sheets.get(name) ?? new Map();
    this.sheets.set(name, g);
    if (!this.active) this.active = name;
    const r0 = parseAddress(at)!;
    rows.forEach((row, i) => row.forEach((v, j) => {
      if (v !== null && v !== undefined) g.set(key(r0.c1 + j, r0.r1 + i), { raw: v, fmt: "General" });
    }));
    return this;
  }

  private grid(sheet: string): Grid {
    const g = this.sheets.get(sheet);
    if (!g) throw new Error(`The sheet "${sheet}" does not exist`);
    return g;
  }

  private used(sheet: string): Rect | null {
    let c1 = Infinity, r1 = Infinity, c2 = 0, r2 = 0;
    for (const [k, v] of this.grid(sheet)) {
      if (v.raw === null || v.raw === "") continue;
      const [c, r] = k.split(",").map(Number);
      c1 = Math.min(c1, c); r1 = Math.min(r1, r); c2 = Math.max(c2, c); r2 = Math.max(r2, r);
    }
    return c2 === 0 ? null : { sheet, c1, r1, c2, r2 };
  }

  async listSheets(): Promise<SheetInfo[]> {
    return [...this.sheets.keys()].map((name) => {
      const u = this.used(name);
      return { name, visibility: this.hidden.has(name) ? "Hidden" : "Visible", usedAddress: u ? `${cellAddress(u.c1, u.r1)}:${cellAddress(u.c2, u.r2)}` : null };
    });
  }
  async activeSheet() { return this.active; }
  async selection() { return this.sel ?? { sheet: this.active, address: "A1" }; }
  /** Like Excel: the sheet becomes active and the range becomes the selection. Cells are untouched. */
  async select(sheet: string, address: string): Promise<void> {
    this.log.push(`select ${sheet}!${address}`);
    if (!parseAddress(address)) throw new Error(`"${address}" is not a valid A1 address`);
    this.grid(sheet); // throws for an unknown sheet, as Excel does
    this.active = sheet;
    this.sel = { sheet, address };
  }

  private rect(sheet: string, address: string): Rect {
    const r = parseAddress(address);
    if (!r) throw new Error(`"${address}" is not a valid A1 address`);
    const bounds = this.used(sheet) ?? { sheet, c1: 1, r1: 1, c2: 1, r2: 1 };
    // whole column / row references are clamped to the used range, like Excel's getUsedRange intersect
    const whole = r.r2 === MAX_ROW || r.c2 === MAX_COL;
    return whole ? (clampTo(r, bounds) ?? { sheet, c1: r.c1, r1: r.r1, c2: r.c1, r2: r.r1 }) : r;
  }

  evalCell(sheet: string, c: number, r: number, depth = 0): CellValue {
    const cell = this.grid(sheet).get(key(c, r));
    if (!cell) return null;
    if (typeof cell.raw === "string" && cell.raw.startsWith("=")) return depth > 20 ? "#REF!" : this.evalFormula(sheet, cell.raw.slice(1), depth + 1);
    return cell.raw;
  }

  private evalFormula(sheet: string, f: string, depth: number): CellValue {
    let pos = 0;
    const peek = () => f[pos];
    const err = (e: string) => { throw new FormulaError(e); };
    const num = (v: CellValue): number => {
      if (typeof v === "string" && v.startsWith("#")) throw new FormulaError(v);
      if (v === null || v === "") return 0;
      if (typeof v === "number") return v;
      if (typeof v === "boolean") return v ? 1 : 0;
      const n = Number(v);
      return Number.isNaN(n) ? err("#VALUE!") : n;
    };
    const rangeVals = (a: string): CellValue[] => {
      const rr = parseAddress(a);
      if (!rr) return err("#REF!");
      const out: CellValue[] = [];
      for (let r = rr.r1; r <= rr.r2; r++) for (let c = rr.c1; c <= rr.c2; c++) out.push(this.evalCell(rr.sheet ?? sheet, c, r, depth));
      return out;
    };
    const expr = (): number => {
      let v = term();
      while (peek() === "+" || peek() === "-") { const op = f[pos++]; const t = term(); v = op === "+" ? v + t : v - t; }
      return v;
    };
    const term = (): number => {
      let v = factor();
      while (peek() === "*" || peek() === "/") {
        const op = f[pos++]; const t = factor();
        if (op === "/" && t === 0) err("#DIV/0!");
        v = op === "*" ? v * t : v / t;
      }
      return v;
    };
    const factor = (): number => {
      if (peek() === "-") { pos++; return -factor(); }
      if (peek() === "(") { pos++; const v = expr(); if (f[pos++] !== ")") err("#VALUE!"); return v; }
      const rest = f.slice(pos);
      let m = /^\d+(\.\d+)?/.exec(rest);
      if (m) { pos += m[0].length; return Number(m[0]); }
      m = /^([A-Z]+)\(([^()]*)\)/.exec(rest);
      if (m) {
        pos += m[0].length;
        const vals = m[2].split(",").flatMap((a) => (/^[$A-Z0-9:]+$/.test(a.trim()) && /\d|:/.test(a) ? rangeVals(a.trim()) : [Number(a)]));
        if (m[1] === "SUM") return vals.reduce<number>((s, v) => s + (typeof v === "number" ? v : 0), 0);
        if (m[1] === "COUNT") return vals.filter((v) => typeof v === "number").length;
        return err("#NAME?");
      }
      m = /^\$?([A-Z]{1,3})\$?(\d+)/.exec(rest);
      if (m) { pos += m[0].length; return num(this.evalCell(sheet, lettersToCol(m[1]), Number(m[2]), depth)); }
      return err("#NAME?");
    };
    try {
      const v = expr();
      if (pos < f.length) return "#VALUE!";
      return Math.round(v * 1e10) / 1e10;
    } catch (e) {
      if (e instanceof FormulaError) return e.code;
      throw e;
    }
  }

  async read(sheet: string, address: string): Promise<RangeData> {
    const r = this.rect(sheet, address);
    const g = this.grid(sheet);
    const values: CellValue[][] = [], formulas: string[][] = [], numberFormats: string[][] = [], valueTypes: ValueType[][] = [];
    for (let row = r.r1; row <= r.r2; row++) {
      const vr: CellValue[] = [], fr: string[] = [], nr: string[] = [], tr: ValueType[] = [];
      for (let c = r.c1; c <= r.c2; c++) {
        const cell = g.get(key(c, row));
        const v = this.evalCell(sheet, c, row);
        vr.push(v === null ? "" : v);
        fr.push(cell ? (typeof cell.raw === "string" && cell.raw.startsWith("=") ? cell.raw : String(cell.raw ?? "")) : "");
        nr.push(cell?.fmt ?? "General");
        tr.push(v === null || v === "" ? "Empty" : typeof v === "number" ? "Double" : typeof v === "boolean" ? "Boolean" : /^#[A-Z/0!?]+[!?]?$/.test(v) ? "Error" : "String");
      }
      values.push(vr); formulas.push(fr); numberFormats.push(nr); valueTypes.push(tr);
    }
    return { address: `${cellAddress(r.c1, r.r1)}:${cellAddress(r.c2, r.r2)}`, values, formulas, numberFormats, valueTypes };
  }

  async write(sheet: string, address: string, cells: CellValue[][], numberFormat?: string): Promise<void> {
    this.log.push(`write ${sheet}!${address}`);
    const r = parseAddress(address)!;
    const g = this.grid(sheet);
    cells.forEach((row, i) => row.forEach((v, j) => {
      const k = key(r.c1 + j, r.r1 + i);
      const prev = g.get(k);
      g.set(k, { raw: v, fmt: numberFormat ?? prev?.fmt ?? "General" });
    }));
  }

  async setNumberFormats(sheet: string, address: string, formats: string[][]): Promise<void> {
    this.log.push(`formats ${sheet}!${address}`);
    const r = parseAddress(address)!;
    const g = this.grid(sheet);
    for (let row = r.r1; row <= r.r2; row++) for (let c = r.c1; c <= r.c2; c++) {
      const fmt = formats[row - r.r1]?.[c - r.c1] ?? "General";
      g.set(key(c, row), { raw: g.get(key(c, row))?.raw ?? null, fmt });
    }
  }

  async fillFormula(sheet: string, address: string, formula: string): Promise<void> {
    this.log.push(`fill ${sheet}!${address}`);
    const r = parseAddress(address)!;
    const g = this.grid(sheet);
    for (let row = r.r1; row <= r.r2; row++) for (let c = r.c1; c <= r.c2; c++) {
      g.set(key(c, row), { raw: shiftFormula(formula, row - r.r1, c - r.c1), fmt: g.get(key(c, row))?.fmt ?? "General" });
    }
  }

  async clear(sheet: string, address: string, what: "contents" | "formats" | "all"): Promise<void> {
    this.log.push(`clear ${sheet}!${address} ${what}`);
    const r = parseAddress(address)!;
    const g = this.grid(sheet);
    for (let row = r.r1; row <= r.r2; row++) for (let c = r.c1; c <= r.c2; c++) {
      if (what !== "formats") g.delete(key(c, row));
    }
  }

  async format(sheet: string, address: string, spec: FormatSpec): Promise<void> {
    this.log.push(`format ${sheet}!${address}`);
    this.formats.push({ sheet, address, spec });
    if (spec.numberFormat) {
      const r = parseAddress(address)!;
      const g = this.grid(sheet);
      for (let row = r.r1; row <= r.r2; row++) for (let c = r.c1; c <= r.c2; c++) {
        const cell = g.get(key(c, row)) ?? { raw: null, fmt: "General" };
        g.set(key(c, row), { ...cell, fmt: spec.numberFormat });
      }
    }
  }

  async listTables() { return this.tables; }
  async addTable(sheet: string, address: string, name: string | undefined): Promise<TableInfo> {
    this.log.push(`table ${sheet}!${address}`);
    const t = { name: name ?? `Table${this.tables.length + 1}`, sheet, address };
    this.tables.push(t);
    return t;
  }
  async listNames() { return this.names; }
  async listCharts(sheet: string) { return this.charts.filter((c) => c.sheet === sheet); }
  /** Each chart covers the same 8 x 16 block addChart gives it, measured from its anchor cell. */
  async findChartOverlap(sheet: string, address: string): Promise<string | null> {
    const target = parseAddress(address);
    if (!target) throw new Error(`"${address}" is not a valid A1 address`);
    for (const c of this.charts) {
      if (c.sheet !== sheet) continue;
      const anchor = parseAddress(c.anchor);
      if (anchor && intersects(chartFootprint(anchor), target)) return c.name;
    }
    return null;
  }
  async addChart(spec: ChartSpec): Promise<ChartInfo> {
    this.log.push(`chart ${spec.sheet}`);
    const c = { name: `Chart ${this.charts.length + 1}`, sheet: spec.sheet, type: spec.kind, title: spec.title ?? "", dataAddress: spec.dataAddress, anchor: spec.anchor };
    this.charts.push(c);
    return c;
  }
  async listPivots() { return this.pivots; }
  async addPivot(spec: PivotSpec): Promise<PivotInfo> {
    this.log.push(`pivot ${spec.destSheet}!${spec.destAddress}`);
    const p = { name: spec.name, sheet: spec.destSheet, address: spec.destAddress };
    this.pivots.push(p);
    return p;
  }
  async calculationMode() { return "Automatic"; }
  async calculate() { this.calcs++; }
  async getSetting(k: string) { return this.settings.get(k) ?? null; }
  async setSetting(k: string, v: string) { this.settings.set(k, v); }

  /** Test helper: set a raw cell (value or "=formula"). */
  set(sheet: string, address: string, v: CellValue) {
    const r = parseAddress(address)!;
    this.grid(sheet).set(key(r.c1, r.r1), { raw: v, fmt: "General" });
  }
  /** Test helper: the evaluated value of one cell. */
  value(sheet: string, address: string): CellValue {
    const r = parseAddress(address)!;
    return this.evalCell(sheet, r.c1, r.r1);
  }
}

class FormulaError extends Error {
  constructor(public code: string) { super(code); }
}

export { colToLetters };
