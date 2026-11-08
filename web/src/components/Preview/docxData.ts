import JSZip from "jszip";
import type { IDocumentData } from "@univerjs/core";

function escapeXml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

interface RunStyle {
  bl?: number;
  it?: number;
  ul?: { s?: number } | number;
  st?: { s?: number } | number;
  cl?: { rgb?: string | null };
  bg?: { rgb?: string | null };
  fs?: number;
  ff?: string | null;
  va?: number;
}

const W_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";

const OOXML_HIGHLIGHT_HEX: Record<string, string> = {
  black: "000000",
  blue: "0000FF",
  cyan: "00FFFF",
  green: "00FF00",
  magenta: "FF00FF",
  red: "FF0000",
  yellow: "FFFF00",
  white: "FFFFFF",
  darkBlue: "000080",
  darkCyan: "008080",
  darkGreen: "008000",
  darkMagenta: "800080",
  darkRed: "800000",
  darkYellow: "808000",
  darkGray: "808080",
  lightGray: "C0C0C0",
};

/** Normalise a Univer colour (`#RGB`, `#RRGGBB`, `rgb(r,g,b)`) to OOXML hex. */
function toOoxmlHex(value: string | null | undefined): string | null {
  if (!value) return null;
  const s = value.trim();
  let m = /^#([0-9a-fA-F]{6})$/.exec(s);
  if (m) return m[1].toUpperCase();
  m = /^#([0-9a-fA-F]{3})$/.exec(s);
  if (m) return m[1].split("").map((c) => c + c).join("").toUpperCase();
  m = /^rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)$/.exec(s);
  if (m) return [m[1], m[2], m[3]].map((n) => Number(n).toString(16).padStart(2, "0")).join("").toUpperCase();
  return null;
}

/** Univer decorations are either a number or `{ s }`; both mean "on" when non-zero. */
function decorationOn(value: { s?: number } | number | undefined): boolean {
  if (value === undefined) return false;
  return typeof value === "number" ? value !== 0 : (value.s ?? 0) !== 0;
}

/** OOXML on/off property: absent `w:val` means on; explicit false/0/off means off. */
function ooxmlOn(el: Element | undefined): boolean {
  if (!el) return false;
  const val = el.getAttributeNS(W_NS, "val") ?? el.getAttribute("w:val") ?? el.getAttribute("val");
  if (val === null || val === undefined || val === "") return true;
  return !/^(false|0|off|no)$/i.test(val);
}

function rPrFromTs(ts: RunStyle | undefined): string {
  if (!ts) return "";
  const parts: string[] = [];
  // Keep EG_RPrBase sequence order: rFonts, b, i, strike, color, sz, u, shd, vertAlign.
  if (ts.ff) parts.push(`<w:rFonts w:ascii="${escapeXml(ts.ff)}" w:hAnsi="${escapeXml(ts.ff)}"/>`);
  if (ts.bl) parts.push("<w:b/>");
  if (ts.it) parts.push("<w:i/>");
  if (decorationOn(ts.st)) parts.push("<w:strike/>");
  const color = toOoxmlHex(ts.cl?.rgb);
  if (color) parts.push(`<w:color w:val="${color}"/>`);
  if (ts.fs && ts.fs > 0) parts.push(`<w:sz w:val="${Math.round(ts.fs * 2)}"/>`);
  if (decorationOn(ts.ul)) parts.push('<w:u w:val="single"/>');
  const bg = toOoxmlHex(ts.bg?.rgb);
  if (bg) parts.push(`<w:shd w:val="clear" w:color="auto" w:fill="${bg}"/>`);
  if (ts.va === 2) parts.push('<w:vertAlign w:val="subscript"/>');
  else if (ts.va === 3) parts.push('<w:vertAlign w:val="superscript"/>');
  if (parts.length === 0) return "";
  return `<w:rPr>${parts.join("")}</w:rPr>`;
}

const PRESERVED_MARKERS = {
  table: "[Table preserved]",
  image: "[Image preserved]",
  content: "[Content preserved]",
  section: "[Section break preserved]",
} as const;
type PreservedKind = keyof typeof PRESERVED_MARKERS;
const MARKER_TEXTS = new Set<string>(Object.values(PRESERVED_MARKERS));

/**
 * A verbatim block captured at import. `kind: "section"` marks a paragraph-level
 * `w:sectPr` (a mid-document section break): on export it is merged into the
 * paragraph before it rather than emitted as a block of its own.
 */
interface RawBlockEntry {
  startIndex: number;
  xml: string;
  kind?: "section";
}

/** The direct `w:sectPr` child of a paragraph's own `w:pPr`, if it has one. */
function paragraphSectPr(p: Element, wNs: string): Element | undefined {
  const pPr = Array.from(p.children).find((c) => c.namespaceURI === wNs && c.localName === "pPr");
  return pPr
    ? Array.from(pPr.children).find((c) => c.namespaceURI === wNs && c.localName === "sectPr")
    : undefined;
}

/** True when a paragraph holds content we cannot model as text runs (images, shapes, embedded objects). */
function hasUnsupportedObject(p: Element, wNs: string): boolean {
  return (
    p.getElementsByTagNameNS(wNs, "drawing").length > 0 ||
    p.getElementsByTagNameNS(wNs, "pict").length > 0 ||
    p.getElementsByTagNameNS(wNs, "object").length > 0 ||
    p.getElementsByTagNameNS(wNs, "txbxContent").length > 0
  );
}

/** Conventional prefixes for the namespaces a Word block can reference. */
const CANONICAL_PREFIX: Record<string, string> = {
  "http://schemas.openxmlformats.org/wordprocessingml/2006/main": "w",
  "http://schemas.openxmlformats.org/officeDocument/2006/relationships": "r",
  "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing": "wp",
  "http://schemas.openxmlformats.org/drawingml/2006/main": "a",
  "http://schemas.openxmlformats.org/drawingml/2006/picture": "pic",
};

/**
 * XMLSerializer invents prefixes (`ns1:`) for namespaces declared on the
 * document root rather than the serialized subtree. Rewrite those back to the
 * conventional Word prefixes so the fragment reads like the original.
 */
function canonicalizePrefixes(raw: string): string {
  const remap: Record<string, string> = {};
  const re = /xmlns:([A-Za-z0-9]+)="([^"]+)"/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(raw))) {
    const canonical = CANONICAL_PREFIX[m[2]];
    if (canonical && canonical !== m[1]) remap[m[1]] = canonical;
  }
  let out = raw;
  for (const [from, to] of Object.entries(remap)) {
    out = out.replace(new RegExp(`xmlns:${from}=`, "g"), `xmlns:${to}=`);
    out = out.replace(new RegExp(`\\b${from}:`, "g"), `${to}:`);
  }
  return out;
}

/**
 * The body-level `w:sectPr`, which carries the final section's page setup and
 * header/footer references. A paragraph-level sectPr (`w:pPr/w:sectPr`) closes
 * an earlier section and must not be mistaken for it: the body's own sectPr is
 * always the last direct child of `w:body`.
 */
function bodySectPrXml(docXml: string): string | null {
  const dom = new DOMParser().parseFromString(docXml, "application/xml");
  const body = dom.getElementsByTagNameNS(W_NS, "body")[0];
  if (!body) return null;
  const sectPrs = Array.from(body.children).filter((c) => c.namespaceURI === W_NS && c.localName === "sectPr");
  const last = sectPrs[sectPrs.length - 1];
  return last ? serializeRawBlock(last) : null;
}

/** Serialize a block for lossless re-emission, guaranteeing the `w` namespace is declared. */
function serializeRawBlock(el: Element): string {
  let raw = canonicalizePrefixes(new XMLSerializer().serializeToString(el));
  if (!raw.includes("xmlns:w=")) {
    raw = raw.replace(/^<([A-Za-z0-9:]+)/, `<$1 xmlns:w="${W_NS}"`);
  }
  return raw;
}

/**
 * Univer document-stream control characters
 * (`DataStreamTreeTokenType` in @univerjs/core). A table is
 * `TABLE_START (ROW_START (CELL_START text PARAGRAPH SECTION_BREAK CELL_END)* ROW_END)* TABLE_END`
 * followed by one PARAGRAPH that encloses the whole table.
 */
const DS = {
  PARAGRAPH: "\r",
  SECTION_BREAK: "\n",
  TABLE_START: "\x1a",
  TABLE_ROW_START: "\x1b",
  TABLE_CELL_START: "\x1c",
  TABLE_CELL_END: "\x1d",
  TABLE_ROW_END: "\x0e",
  TABLE_END: "\x0f",
} as const;

const CUSTOM_BLOCK = "\b"; // DataStreamTreeTokenType.CUSTOM_BLOCK

/**
 * Tokens Univer allows between one paragraph's terminator and the next
 * paragraph's text (section break and table/row/cell sentinels). They are never
 * text, so the exporter skips them when it slices a paragraph.
 */
const PARAGRAPH_GAP_TOKENS = new Set<string>([
  DS.SECTION_BREAK,
  DS.TABLE_START,
  DS.TABLE_ROW_START,
  DS.TABLE_CELL_START,
  DS.TABLE_CELL_END,
  DS.TABLE_ROW_END,
  DS.TABLE_END,
]);
const WP_NS = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing";
const A_NS = "http://schemas.openxmlformats.org/drawingml/2006/main";
const R_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
const IMAGE_MIME: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  bmp: "image/bmp",
  svg: "image/svg+xml",
  webp: "image/webp",
};

async function parseImageRelations(zip: JSZip): Promise<Map<string, string>> {
  const rels = await zip.file("word/_rels/document.xml.rels")?.async("string");
  const map = new Map<string, string>();
  if (!rels) return map;
  const dom = new DOMParser().parseFromString(rels, "application/xml");
  for (const rel of Array.from(dom.getElementsByTagName("Relationship"))) {
    const id = rel.getAttribute("Id");
    const target = rel.getAttribute("Target");
    const type = rel.getAttribute("Type") ?? "";
    if (!id || !target || !type.endsWith("/image")) continue;
    map.set(id, target.startsWith("/") ? target.slice(1) : `word/${target}`);
  }
  return map;
}

/** Rewrite `wp:extent`/`a:ext` to the drawing's current size (px -> EMU). */
function patchDrawingExtent(xml: string, widthPx: number, heightPx: number): string {
  const cx = Math.max(1, Math.round(widthPx * 9525));
  const cy = Math.max(1, Math.round(heightPx * 9525));
  let out = xml.replace(/(<wp:extent\b[^>]*\bcx=")\d+("[^>]*\bcy=")\d+(")/, `$1${cx}$2${cy}$3`);
  out = out.replace(/(<a:ext\b[^>]*\bcx=")\d+("[^>]*\bcy=")\d+(")/, `$1${cx}$2${cy}$3`);
  return out;
}

function bytesToBase64(bytes: Uint8Array): string {
  let out = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    out += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(out);
}

/** True when a paragraph holds exactly one drawing and no text. */
function isImageOnlyParagraph(p: Element, wNs: string): boolean {
  const text = Array.from(p.getElementsByTagNameNS(wNs, "t"))
    .map((t) => t.textContent ?? "")
    .join("");
  return p.getElementsByTagNameNS(wNs, "drawing").length === 1 && text.trim() === "";
}

/** Approximate letter page content width in layout px (8.5in - 2in margins at 96 dpi). */
const TABLE_CONTENT_WIDTH = 624;

interface ParsedCell {
  text: string;
  /** Original `w:tcPr`, re-emitted on export so cell styling survives. */
  tcPrXml: string;
}

interface ParsedTable {
  /** Grid column widths in twips (empty when the table has no `w:tblGrid`). */
  gridTwips: number[];
  /** Original `w:tblPr`, re-emitted on export so borders/styles survive. */
  tblPrXml: string;
  /** Original `w:trPr` per row. */
  rowPrXml: string[];
  /** Cell text per row; the v1 model is one paragraph per cell, no merges. */
  rows: ParsedCell[][];
}

/**
 * Parse a `w:tbl` into Univer's native table model. Returns null for anything
 * the v1 model cannot represent (merged cells, nested tables, multi-paragraph
 * cells, pictures, links, fields, content controls); the caller then falls back
 * to verbatim preservation.
 */
function parseSimpleTable(tbl: Element, wNs: string): ParsedTable | null {
  const attr = (el: Element, name: string) =>
    el.getAttributeNS(wNs, name) ?? el.getAttribute(`w:${name}`) ?? el.getAttribute(name);
  const gridEl = Array.from(tbl.children).find((c) => c.namespaceURI === wNs && c.localName === "tblGrid");
  const gridTwips = gridEl
    ? Array.from(gridEl.children)
        .filter((c) => c.namespaceURI === wNs && c.localName === "gridCol")
        .map((c) => Number(attr(c, "w") ?? "0"))
    : [];
  const tblPrEl = Array.from(tbl.children).find((c) => c.namespaceURI === wNs && c.localName === "tblPr");
  const tblPrXml = tblPrEl ? serializeRawBlock(tblPrEl) : "";
  const rowEls = Array.from(tbl.children).filter((c) => c.namespaceURI === wNs && c.localName === "tr");
  if (rowEls.length === 0) return null;
  const rowPrXml: string[] = [];
  const rows: ParsedCell[][] = [];
  for (const tr of rowEls) {
    const trPrEl = Array.from(tr.children).find((c) => c.namespaceURI === wNs && c.localName === "trPr");
    rowPrXml.push(trPrEl ? serializeRawBlock(trPrEl) : "");
    const cellEls = Array.from(tr.children).filter((c) => c.namespaceURI === wNs && c.localName === "tc");
    if (cellEls.length === 0) return null;
    const cells: ParsedCell[] = [];
    for (const tc of cellEls) {
      const tcPr = Array.from(tc.children).find((c) => c.namespaceURI === wNs && c.localName === "tcPr");
      if (tcPr && ["gridSpan", "vMerge", "hMerge"].some((n) => tcPr.getElementsByTagNameNS(wNs, n).length > 0)) {
        return null;
      }
      if (tc.getElementsByTagNameNS(wNs, "tbl").length > 0) return null;
      // Cell text is rebuilt as one plain run on export, so a picture, link,
      // field or content control in the cell would be dropped. Keep such tables verbatim.
      const unmodelled = ["drawing", "pict", "object", "txbxContent", "hyperlink", "sdt", "fldSimple", "fldChar"];
      if (unmodelled.some((name) => tc.getElementsByTagNameNS(wNs, name).length > 0)) return null;
      const paras = Array.from(tc.children).filter((c) => c.namespaceURI === wNs && c.localName === "p");
      if (paras.length > 1) return null;
      cells.push({
        text:
          paras.length === 1
            ? Array.from(paras[0].getElementsByTagNameNS(wNs, "t"))
                .map((t) => t.textContent ?? "")
                .join("")
            : "",
        tcPrXml: tcPr ? serializeRawBlock(tcPr) : "",
      });
    }
    if (rows.length > 0 && cells.length !== rows[0].length) return null;
    rows.push(cells);
  }
  if (gridTwips.length > 0 && gridTwips.length !== rows[0].length) return null;
  return { gridTwips, tblPrXml, rowPrXml, rows };
}

function makeTableSource(tableId: string, rows: string[][], gridTwips: number[]): any {
  const rowCount = rows.length;
  const colCount = rows[0]?.length ?? 0;
  const total = gridTwips.reduce((a, b) => a + b, 0);
  const colWidths =
    gridTwips.length === colCount && total > 0
      ? gridTwips.map((w) => (w / total) * TABLE_CONTENT_WIDTH)
      : new Array(colCount).fill(TABLE_CONTENT_WIDTH / Math.max(1, colCount));
  const margin = () => ({ start: { v: 10 }, end: { v: 10 }, top: { v: 5 }, bottom: { v: 5 } });
  const makeRow = () => ({
    tableCells: new Array(colCount).fill(0).map(() => ({ margin: margin() })),
    trHeight: { val: { v: 30 }, hRule: 0 /* TableRowHeightRule.AUTO */ },
  });
  return {
    tableId,
    tableRows: new Array(rowCount).fill(0).map(() => makeRow()),
    tableColumns: colWidths.map((w) => ({ size: { type: 1 /* SPECIFIED */, width: { v: w } } })),
    align: 0, // TableAlignmentType.START
    indent: { v: 0 },
    textWrap: 0, // TableTextWrapType.NONE
    position: {
      positionH: { relativeFrom: 0 /* ObjectRelativeFromH.PAGE */, posOffset: 0 },
      positionV: { relativeFrom: 0 /* ObjectRelativeFromV.PAGE */, posOffset: 0 },
    },
    dist: { distB: 0, distL: 0, distR: 0, distT: 0 },
    cellMargin: margin(),
    size: { type: 0 /* TableSizeType.UNSPECIFIED */, width: { v: TABLE_CONTENT_WIDTH } },
  };
}

/** Rebuild a `w:tbl` from a native table range in the data stream. */
function renderTableXml(
  range: { startIndex: number; endIndex: number; tableId: string },
  stream: string,
  tableSource: Record<string, any>,
  tableStyles: Record<string, { tblPrXml?: string; rowPrXml?: string[]; tcPrXml?: string[][] }>,
): string {
  const seg = stream.slice(range.startIndex, range.endIndex);
  if (seg[0] !== DS.TABLE_START) return "";
  const rows: string[][] = [];
  let row: string[] | null = null;
  let cell = "";
  let inCell = false;
  for (let i = 1; i < seg.length; i++) {
    const ch = seg[i];
    if (ch === DS.TABLE_ROW_START) {
      row = [];
      inCell = false;
    } else if (ch === DS.TABLE_CELL_START) {
      inCell = true;
      cell = "";
    } else if (ch === DS.TABLE_CELL_END) {
      if (row) row.push(cell);
      inCell = false;
    } else if (ch === DS.TABLE_ROW_END) {
      if (row) rows.push(row);
      row = null;
    } else if (ch === DS.PARAGRAPH || ch === DS.SECTION_BREAK) {
      // paragraph/section delimiters are structural
    } else if (ch === DS.TABLE_START || ch === DS.TABLE_END) {
      // outer delimiters
    } else if (inCell) {
      cell += ch;
    }
  }
  const colCount = rows[0]?.length ?? 0;
  const src = tableSource[range.tableId];
  const widths: number[] = (src?.tableColumns ?? []).map((c: any) =>
    Math.max(1, Math.round((c?.size?.width?.v ?? 0) * 15)),
  );
  const grid =
    widths.length === colCount && widths.every((w) => w > 0)
      ? widths
      : new Array(Math.max(1, colCount)).fill(Math.round((TABLE_CONTENT_WIDTH * 15) / Math.max(1, colCount)));
  const style = tableStyles[range.tableId];
  const tblPr = style?.tblPrXml || "<w:tblPr/>";
  const cellsXml = rows
    .map((cells, ri) => {
      const trPr = style?.rowPrXml?.[ri] ?? "";
      const tcs = cells
        .map((text, ci) => {
          const tcPr = style?.tcPrXml?.[ri]?.[ci] ?? "";
          return `<w:tc>${tcPr}<w:p><w:r><w:t xml:space="preserve">${escapeXml(text)}</w:t></w:r></w:p></w:tc>`;
        })
        .join("");
      return `<w:tr>${trPr}${tcs}</w:tr>`;
    })
    .join("");
  return `<w:tbl>${tblPr}<w:tblGrid>${grid
    .map((w) => `<w:gridCol w:w="${w}"/>`)
    .join("")}</w:tblGrid>${cellsXml}</w:tbl>`;
}

export async function docxToUniver(bytes: Uint8Array): Promise<IDocumentData> {
  const zip = await JSZip.loadAsync(bytes);
  const xml = await zip.file("word/document.xml")?.async("string");
  if (!xml) throw new Error("docx has no word/document.xml");
  const parser = new DOMParser();
  const dom = parser.parseFromString(xml, "application/xml");
  const wNs = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";
  const bodyEl = dom.getElementsByTagNameNS(wNs, "body")[0];
  const imageRels = await parseImageRelations(zip);

  let stream = "";
  const paraStarts: number[] = [];
  const paragraphAligns: (number | undefined)[] = [];
  const textRuns: { st: number; ed: number; ts: RunStyle }[] = [];
  const rawBlocks: RawBlockEntry[] = [];
  const sectionBreaks: { sectionId: string; startIndex: number }[] = [];
  const customBlocks: { startIndex: number; blockId: string }[] = [];
  const drawings: Record<string, any> = {};
  const drawingsOrder: string[] = [];
  const imageMeta: Record<string, { xml: string; width: number; height: number }> = {};
  const tables: { startIndex: number; endIndex: number; tableId: string }[] = [];
  const tableSource: Record<string, any> = {};
  // Original table/cell property XML, keyed by tableId, re-emitted on export.
  const tableStyles: Record<string, any> = {};
  let idSeq = 0;

  const importParagraph = (p: Element) => {
    const pPr = p.getElementsByTagNameNS(wNs, "pPr")[0];
    let hAlign: number | undefined;
    if (pPr) {
      const jc = pPr.getElementsByTagNameNS(wNs, "jc")[0];
      const val = jc?.getAttributeNS(wNs, "val") ?? jc?.getAttribute("w:val") ?? jc?.getAttribute("val");
      if (val === "center") hAlign = 2;
      else if (val === "right" || val === "end") hAlign = 3;
      else if (val === "both" || val === "justify") hAlign = 4;
      else if (val === "left" || val === "start") hAlign = 1;
      else if (val === "distribute") hAlign = 6;
    }
    paragraphAligns.push(hAlign);
    const runs = Array.from(p.getElementsByTagNameNS(wNs, "r"));
    for (const r of runs) {
      const text = Array.from(r.getElementsByTagNameNS(wNs, "t"))
        .map((t) => t.textContent ?? "")
        .join("");
      if (text === "") continue;
      const rPr = r.getElementsByTagNameNS(wNs, "rPr")[0];
      const ts: RunStyle = {};
      if (rPr) {
        const first = (name: string) => rPr.getElementsByTagNameNS(wNs, name)[0];
        const attr = (name: string) => {
          const el = first(name);
          return el ? el.getAttributeNS(wNs, "val") ?? el.getAttribute("w:val") ?? el.getAttribute("val") : null;
        };
        if (ooxmlOn(first("b"))) ts.bl = 1;
        if (ooxmlOn(first("i"))) ts.it = 1;
        const uVal = attr("u");
        if (first("u") && (uVal === null || !/^(none|false|0|off|no)$/i.test(uVal))) ts.ul = { s: 1 };
        if (ooxmlOn(first("strike"))) ts.st = { s: 1 };
        const color = attr("color");
        if (color && color.toLowerCase() !== "auto") ts.cl = { rgb: `#${color.replace(/^#/, "")}` };
        const halfPoints = attr("sz");
        if (halfPoints) {
          const fs = Number(halfPoints) / 2;
          if (Number.isFinite(fs) && fs > 0) ts.fs = fs;
        }
        const fonts = first("rFonts");
        const ff =
          fonts?.getAttributeNS(wNs, "ascii") ??
          fonts?.getAttribute("w:ascii") ??
          fonts?.getAttributeNS(wNs, "hAnsi") ??
          fonts?.getAttribute("w:hAnsi");
        if (ff) ts.ff = ff;
        const highlight = attr("highlight");
        const shadeEl = first("shd");
        const shade = shadeEl
          ? shadeEl.getAttributeNS(wNs, "fill") ?? shadeEl.getAttribute("w:fill") ?? shadeEl.getAttribute("fill")
          : null;
        const bgHex = highlight
          ? OOXML_HIGHLIGHT_HEX[highlight]
          : shade && shade.toLowerCase() !== "auto"
            ? shade
            : null;
        if (bgHex) ts.bg = { rgb: `#${bgHex.replace(/^#/, "")}` };
        const vert = attr("vertAlign");
        if (vert === "subscript") ts.va = 2;
        else if (vert === "superscript") ts.va = 3;
      }
      const st = stream.length;
      stream += text;
      const ed = stream.length - 1;
      if (ts.bl || ts.it || ts.ul || ts.st || ts.cl || ts.bg || ts.fs || ts.ff || ts.va) {
        textRuns.push({ st, ed, ts });
      }
    }
    // Univer anchors a paragraph at its terminating "\r", not at its first character.
    paraStarts.push(stream.length);
    stream += "\r\n";
  };

  const appendTable = (table: ParsedTable) => {
    const tableStart = stream.length;
    stream += DS.TABLE_START;
    for (const row of table.rows) {
      stream += DS.TABLE_ROW_START;
      for (const cell of row) {
        stream += DS.TABLE_CELL_START;
        stream += cell.text;
        const pStart = stream.length;
        stream += DS.PARAGRAPH;
        const sStart = stream.length;
        stream += DS.SECTION_BREAK;
        stream += DS.TABLE_CELL_END;
        paraStarts.push(pStart);
        paragraphAligns.push(undefined);
        sectionBreaks.push({ sectionId: `s${idSeq++}`, startIndex: sStart });
      }
      stream += DS.TABLE_ROW_END;
    }
    stream += DS.TABLE_END;
    // One PARAGRAPH after TABLE_END wraps the table; the parser treats it as
    // the table's enclosing paragraph.
    const enclosingStart = stream.length;
    stream += DS.PARAGRAPH;
    paraStarts.push(enclosingStart);
    paragraphAligns.push(undefined);
    const tableId = `tbl${idSeq++}`;
    tables.push({ startIndex: tableStart, endIndex: enclosingStart, tableId });
    tableSource[tableId] = makeTableSource(
      tableId,
      table.rows.map((row) => row.map((cell) => cell.text)),
      table.gridTwips,
    );
    tableStyles[tableId] = {
      tblPrXml: table.tblPrXml,
      rowPrXml: table.rowPrXml,
      tcPrXml: table.rows.map((row) => row.map((cell) => cell.tcPrXml)),
    };
  };

  /**
   * Convert a paragraph whose only content is one INLINE drawing into a native
   * Univer image. Anchored (`wp:anchor`) drawings, VML and mixed text+image
   * paragraphs return false and fall back to verbatim preservation.
   */
  const importImageParagraph = async (p: Element): Promise<boolean> => {
    const drawingEl = p.getElementsByTagNameNS(wNs, "drawing")[0];
    if (!drawingEl || !drawingEl.getElementsByTagNameNS(WP_NS, "inline")[0]) return false;
    const xml = serializeRawBlock(drawingEl);
    const blip = drawingEl.getElementsByTagNameNS(A_NS, "blip")[0];
    const embed = blip?.getAttributeNS(R_NS, "embed") ?? blip?.getAttribute("r:embed");
    const mediaPath = embed ? imageRels.get(embed) : undefined;
    let source = "";
    if (mediaPath) {
      const bytes = await zip.file(mediaPath)?.async("uint8array");
      if (bytes) {
        const ext = mediaPath.split(".").pop()?.toLowerCase() ?? "";
        source = `data:${IMAGE_MIME[ext] ?? "application/octet-stream"};base64,${bytesToBase64(bytes)}`;
      }
    }
    // wp:extent is in EMU; Univer layout uses px (1 px = 9525 EMU at 96 dpi).
    const extentEl = drawingEl.getElementsByTagNameNS(WP_NS, "extent")[0];
    const cx = Number(extentEl?.getAttribute("cx") ?? "0");
    const cy = Number(extentEl?.getAttribute("cy") ?? "0");
    const width = cx > 0 ? Math.round(cx / 9525) : 100;
    const height = cy > 0 ? Math.round(cy / 9525) : 100;
    const drawingId = `img${idSeq++}`;
    const startIndex = stream.length;
    // The custom block sentinel is the paragraph's text; its "\r" follows it.
    paraStarts.push(startIndex + 1);
    paragraphAligns.push(undefined);
    stream += `${CUSTOM_BLOCK}\r\n`;
    customBlocks.push({ startIndex, blockId: drawingId });
    drawings[drawingId] = {
      drawingId,
      unitId: "",
      subUnitId: "",
      imageSourceType: "BASE64",
      title: "",
      description: "",
      source,
      layoutType: 0, // PositionedObjectLayoutType.INLINE
      drawingType: 0, // DrawingTypeEnum.DRAWING_IMAGE
      transform: { width, height, angle: 0 },
      docTransform: {
        angle: 0,
        size: { width, height },
        positionH: { relativeFrom: 2 /* ObjectRelativeFromH.CHARACTER */, posOffset: 0 },
        positionV: { relativeFrom: 2 /* ObjectRelativeFromV.LINE */, posOffset: 0 },
      },
    };
    drawingsOrder.push(drawingId);
    imageMeta[drawingId] = { xml, width, height };
    return true;
  };

  /** Append a marker paragraph and return the offset of its first character. */
  const pushMarker = (text: string): number => {
    const offset = stream.length;
    paraStarts.push(offset + text.length);
    paragraphAligns.push(undefined);
    stream += `${text}\r\n`;
    return offset;
  };

  // Walk only top-level body children: paragraphs become editable text, every
  // other block (table, content control, image-bearing paragraph, ...) is
  // preserved verbatim. A marker paragraph occupies its position so the editor
  // shows that something is there; the exporter swaps the marker back for the
  // original XML.
  const children = bodyEl ? Array.from(bodyEl.children) : Array.from(dom.getElementsByTagNameNS(wNs, "p"));
  for (const el of children) {
    if (el.namespaceURI !== wNs) continue;
    if (el.localName === "sectPr") continue;
    if (el.localName === "p") {
      if (!hasUnsupportedObject(el, wNs)) {
        importParagraph(el);
        // A mid-document section break: keep its text as a normal paragraph,
        // then preserve the section properties as a raw block after it.
        const sectPr = paragraphSectPr(el, wNs);
        if (sectPr) {
          const offset = pushMarker(PRESERVED_MARKERS.section);
          rawBlocks.push({ startIndex: offset, xml: serializeRawBlock(sectPr), kind: "section" });
        }
        continue;
      }
      // An image-only paragraph that carries a section break is kept verbatim so
      // its w:sectPr survives.
      if (isImageOnlyParagraph(el, wNs) && !paragraphSectPr(el, wNs) && (await importImageParagraph(el))) continue;
    }
    if (el.localName === "tbl") {
      const parsed = parseSimpleTable(el, wNs);
      if (parsed) {
        appendTable(parsed);
        continue;
      }
    }
    const kind: PreservedKind =
      el.localName === "tbl" ? "table" : hasUnsupportedObject(el, wNs) ? "image" : "content";
    const offset = pushMarker(PRESERVED_MARKERS[kind]);
    rawBlocks.push({ startIndex: offset, xml: serializeRawBlock(el) });
  }

  if (paraStarts.length === 0) {
    stream = "\r\n";
    paraStarts.push(0);
    paragraphAligns.push(undefined);
  }

  const paragraphs = paraStarts.map((st, i) => {
    const entry: any = { startIndex: st, paragraphId: `p${i}` };
    if (paragraphAligns[i] !== undefined) entry.paragraphStyle = { horizontalAlign: paragraphAligns[i] };
    return entry;
  });

  const body: any = { dataStream: stream, paragraphs, textRuns };
  if (rawBlocks.length > 0) body.docxRawBlocks = rawBlocks;
  if (sectionBreaks.length > 0) body.sectionBreaks = sectionBreaks;
  if (tables.length > 0) body.tables = tables;
  if (customBlocks.length > 0) body.customBlocks = customBlocks;
  const doc: any = { id: `doc-${Date.now()}`, documentStyle: {}, body };
  if (tables.length > 0) {
    doc.tableSource = tableSource;
    doc.__tableStyles = tableStyles;
  }
  if (drawingsOrder.length > 0) {
    doc.drawings = drawings;
    doc.drawingsOrder = drawingsOrder;
    doc.__imageMeta = imageMeta;
  }
  return doc as IDocumentData;
}

const CONTENT_TYPES = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`;

const RELS = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`;

const DOC_RELS = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`;

export interface DocxExportContext {
  /**
   * Original .docx bytes. When supplied, every package part except
   * `word/document.xml` is copied verbatim, so media, relationships, styles
   * and headers/footers referenced by preserved raw blocks survive the save.
   */
  originalBytes?: Uint8Array;
  /**
   * Raw blocks captured at import time. Used only when the editor snapshot did
   * not carry `body.docxRawBlocks` (e.g. an editor that drops unknown fields).
   */
  rawBlocks?: RawBlockEntry[];
  /**
   * Original table/cell property XML captured at import, keyed by tableId, so
   * borders and shading are not lost when the native table is written back.
   */
  tableStyles?: Record<string, { tblPrXml?: string; rowPrXml?: string[]; tcPrXml?: string[][] }>;
  /**
   * Original `w:drawing` XML captured at import, keyed by drawingId. Native
   * images re-emit it verbatim; without it a new/pasted image has no media part.
   */
  imageMeta?: Record<string, { xml: string; width?: number; height?: number }>;
  /**
   * Resolve an image source that is not a data URL or fetchable URL — a UUID
   * allocated by Univer's image host. Must return a data URL, a blob URL or an
   * http(s) URL, or null when the host cannot resolve it. The editor wires this
   * to `IImageIoService.getImage`.
   */
  resolveImage?: (source: string, imageSourceType?: string) => Promise<string | null>;
  /**
   * Fetch override for URL images. The editor uses it to add the bearer token
   * for same-origin requests; external hosts must be fetched without
   * credentials. Size/time limits and content checks stay in this module.
   */
  imageFetch?: (url: string, init: RequestInit) => Promise<Response>;
  /**
   * Called for each image that could not be embedded (dead URL, unresolvable
   * UUID, non-image body). When supplied the save continues and reports the
   * failures; when absent the export throws instead.
   */
  onImageError?: (message: string) => void;
  /**
   * Receives the metadata of every image this export embedded, keyed by the
   * Univer drawing id. The caller merges it into its `imageMeta` after a
   * successful save, so the next save reuses the part and relationship written
   * here instead of embedding the image again.
   */
  imageMetaOut?: Record<string, { xml: string; width: number; height: number }>;
}

function renderParagraphXml(
  stream: string,
  start: number,
  end: number,
  para: any,
  textRuns: any[],
): string {
  if (end < start) return "<w:p/>";
  const pText = stream.slice(start, end + 1);
  const hAlign = para?.paragraphStyle?.horizontalAlign;
  let jcVal: string | null = null;
  if (hAlign === 2) jcVal = "center";
  else if (hAlign === 3) jcVal = "right";
  else if (hAlign === 4) jcVal = "both";
  else if (hAlign === 1) jcVal = "left";
  else if (hAlign === 6) jcVal = "distribute";
  const pPr = jcVal ? `<w:pPr><w:jc w:val="${jcVal}"/></w:pPr>` : "";
  if (pText === "") return `<w:p>${pPr}</w:p>`;
  const relevant = textRuns
    .filter((r) => r.ed >= start && r.st <= end)
    .sort((a, b) => a.st - b.st);
  const segments: { text: string; ts: RunStyle }[] = [];
  let cursor = start;
  for (const r of relevant) {
    const rs = Math.max(r.st, start);
    const re = Math.min(r.ed, end);
    if (rs > cursor) segments.push({ text: stream.slice(cursor, rs), ts: {} });
    if (re >= rs) segments.push({ text: stream.slice(rs, re + 1), ts: (r.ts as RunStyle) || {} });
    cursor = Math.max(cursor, re + 1);
  }
  if (cursor <= end) segments.push({ text: stream.slice(cursor, end + 1), ts: {} });
  const runsXml = segments
    .filter((seg) => seg.text.length > 0)
    .map((seg) => `<w:r>${rPrFromTs(seg.ts)}<w:t xml:space="preserve">${escapeXml(seg.text)}</w:t></w:r>`)
    .join("");
  return `<w:p>${pPr}${runsXml}</w:p>`;
}

/**
 * Add a section break's `w:sectPr` to a plain paragraph's `w:pPr`. The schema
 * orders `w:pPr` children as `... w:jc ... w:rPr, w:sectPr, w:pPrChange`.
 * renderParagraphXml emits neither `w:rPr` nor `w:pPrChange`, so appending the
 * `w:sectPr` as the last child keeps the order valid.
 */
function attachSectPr(paraXml: string, sectPrXml: string): string {
  if (paraXml === "<w:p/>") return `<w:p><w:pPr>${sectPrXml}</w:pPr></w:p>`;
  if (paraXml.includes("<w:pPr>")) return paraXml.replace("</w:pPr>", () => `${sectPrXml}</w:pPr>`);
  return paraXml.replace(/^<w:p>/, () => `<w:p><w:pPr>${sectPrXml}</w:pPr>`);
}

function parseImageDataUrl(source: string | null | undefined): { mime: string; ext: string; bytes: Uint8Array } | null {
  if (!source) return null;
  const m = /^data:([^;,]+);base64,([\s\S]*)$/.exec(source);
  if (!m) return null;
  const bin = atob(m[2]);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  const ext = Object.entries(IMAGE_MIME).find(([, v]) => v === m[1])?.[0] ?? "png";
  return { mime: m[1], ext, bytes };
}

const MAX_IMAGE_BYTES = 20 * 1024 * 1024;
const IMAGE_FETCH_TIMEOUT_MS = 15_000;

interface ImageBytes {
  mime: string;
  ext: string;
  bytes: Uint8Array;
}

/** Identify common raster formats by magic bytes; SVG is deliberately rejected. */
function sniffImageMime(bytes: Uint8Array): { mime: string; ext: string } | null {
  const b = bytes;
  if (b.length >= 8 && b[0] === 0x89 && b[1] === 0x50 && b[2] === 0x4e && b[3] === 0x47) return { mime: "image/png", ext: "png" };
  if (b.length >= 3 && b[0] === 0xff && b[1] === 0xd8 && b[2] === 0xff) return { mime: "image/jpeg", ext: "jpg" };
  if (b.length >= 6 && b[0] === 0x47 && b[1] === 0x49 && b[2] === 0x46) return { mime: "image/gif", ext: "gif" };
  if (b.length >= 12 && b[0] === 0x52 && b[1] === 0x49 && b[2] === 0x46 && b[3] === 0x46 && b[8] === 0x57 && b[9] === 0x45 && b[10] === 0x42 && b[11] === 0x50) return { mime: "image/webp", ext: "webp" };
  if (b.length >= 2 && b[0] === 0x42 && b[1] === 0x4d) return { mime: "image/bmp", ext: "bmp" };
  return null;
}

/** Read a response body, aborting once it exceeds `cap` bytes. */
async function readCapped(res: Response, cap: number): Promise<Uint8Array> {
  if (!res.body) {
    const buf = new Uint8Array(await res.arrayBuffer());
    if (buf.length > cap) throw new Error(`image exceeds ${Math.round(cap / 1024 / 1024)} MB`);
    return buf;
  }
  const reader = res.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    if (!value) continue;
    total += value.length;
    if (total > cap) {
      await reader.cancel();
      throw new Error(`image exceeds ${Math.round(cap / 1024 / 1024)} MB`);
    }
    chunks.push(value);
  }
  const out = new Uint8Array(total);
  let off = 0;
  for (const chunk of chunks) {
    out.set(chunk, off);
    off += chunk.length;
  }
  return out;
}

/** Fetch an http(s)/blob image with a bounded body and a timeout. */
async function fetchImageBytes(source: string, ctx: DocxExportContext | undefined): Promise<ImageBytes> {
  const url = new URL(source, typeof window !== "undefined" ? window.location.href : "http://localhost/");
  if (url.protocol !== "http:" && url.protocol !== "https:" && url.protocol !== "blob:") {
    throw new Error(`unsupported image URL scheme ${url.protocol}`);
  }
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), IMAGE_FETCH_TIMEOUT_MS);
  try {
    const init: RequestInit = { mode: "cors", credentials: "omit", signal: controller.signal };
    const res = ctx?.imageFetch ? await ctx.imageFetch(url.href, init) : await fetch(url.href, init);
    if (!res.ok) throw new Error(`HTTP ${res.status}${res.statusText ? ` ${res.statusText}` : ""}`);
    const bytes = await readCapped(res, MAX_IMAGE_BYTES);
    const kind = sniffImageMime(bytes);
    if (!kind) throw new Error("response is not a supported image (png/jpeg/gif/webp/bmp; SVG is not embedded)");
    return { ...kind, bytes };
  } finally {
    clearTimeout(timer);
  }
}

function isFetchableSource(source: string): boolean {
  return /^https?:\/\//i.test(source) || /^blob:/i.test(source) || source.startsWith("/") || /^\.\.?\//.test(source);
}

function describeImageFailure(source: string | undefined, sourceType: string | undefined): string {
  if (!source) return "the drawing has no image source";
  if ((sourceType ?? "").toUpperCase() === "UUID") {
    return "the UUID image host service returned nothing for this image (a UUID is not resolvable without a hosting service)";
  }
  return "the source is not a data URL, an http(s)/blob URL, or a resolvable UUID";
}

function truncateSource(source: string | undefined): string {
  const text = source ?? "(none)";
  return text.length > 60 ? `${text.slice(0, 57)}…` : text;
}

/**
 * Resolve a drawing `source` to bytes. Base64 data URLs decode locally;
 * http(s)/blob/relative URLs are fetched; anything else (a UUID) is delegated
 * to `ctx.resolveImage`, which the editor wires to Univer's image host.
 */
async function resolveImageBytes(
  source: string | undefined,
  sourceType: string | undefined,
  ctx: DocxExportContext | undefined,
  cache: Map<string, ImageBytes>,
): Promise<ImageBytes | null> {
  if (!source) return null;
  const key = `${sourceType ?? ""}|${source}`;
  const hit = cache.get(key);
  if (hit) return hit;
  let out: ImageBytes | null = null;
  const data = parseImageDataUrl(source);
  if (data) out = data;
  else if (isFetchableSource(source)) out = await fetchImageBytes(source, ctx);
  else if (ctx?.resolveImage) {
    const resolved = await ctx.resolveImage(source, sourceType);
    if (resolved && resolved !== source) out = await resolveImageBytes(resolved, undefined, ctx, cache);
  }
  if (out) cache.set(key, out);
  return out;
}

/**
 * Headers for an image fetch. The bearer token is attached only when the URL
 * is same-origin; a cross-origin host must never receive it.
 */
export function imageRequestHeaders(
  url: string,
  pageOrigin: string,
  initHeaders: Record<string, string> | undefined,
  auth: Record<string, string>,
): Record<string, string> {
  const headers = { ...(initHeaders ?? {}) };
  if (new URL(url).origin === pageOrigin) return { ...headers, ...auth };
  return headers;
}

/** First `word/media/opencode-image-N.<ext>` name not present in the package. */
function nextMediaPath(ext: string, zip: JSZip | null, pending: { path: string }[]): string {
  const used = new Set<string>(pending.map((p) => p.path));
  if (zip) for (const name of Object.keys(zip.files)) used.add(name);
  for (let n = 1; ; n++) {
    const path = `word/media/opencode-image-${n}.${ext}`;
    if (!used.has(path)) return path;
  }
}

/**
 * Hosts of http(s) images the save will download. Used by the save dialog to
 * disclose the outbound requests; empty when every image is embedded.
 */
export function externalImageHosts(
  data: IDocumentData,
  imageMeta: Record<string, { xml: string }> | undefined,
): string[] {
  const drawings = ((data as any).drawings ?? {}) as Record<string, { source?: string }>;
  const hosts = new Set<string>();
  for (const [id, drawing] of Object.entries(drawings)) {
    if (imageMeta && imageMeta[id]) continue;
    const source = drawing?.source;
    if (!source || !/^https?:\/\//i.test(source)) continue;
    try {
      hosts.add(new URL(source).host);
    } catch (e) {
      // Best-effort host list only: the save path reports a malformed URL itself.
      console.warn("[docx] could not parse image URL for the save dialog:", source, e);
    }
  }
  return [...hosts].sort();
}

function buildInlineDrawingXml(rId: string, widthPx: number, heightPx: number, id: number): string {
  const cx = Math.max(1, Math.round(widthPx * 9525));
  const cy = Math.max(1, Math.round(heightPx * 9525));
  return `<w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="${cx}" cy="${cy}"/><wp:docPr id="${id}" name="Picture ${id}"/><wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="${id}" name="Image ${id}"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="${rId}"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="${cx}" cy="${cy}"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing>`;
}

function nextRelSeq(relsXml: string): number {
  let max = 0;
  for (const m of relsXml.matchAll(/Id="rId(\d+)"/g)) max = Math.max(max, Number(m[1]));
  return max + 1;
}

export async function univerToDocx(data: IDocumentData, ctx?: DocxExportContext): Promise<Uint8Array> {
  const stream = data.body?.dataStream ?? "";
  const paragraphs = data.body?.paragraphs ?? [];
  const textRuns = data.body?.textRuns ?? [];

  // Paragraph spans follow Univer's convention: a paragraph's `startIndex` is
  // the index of its terminating "\r" (`anchor`). Its text runs from just after
  // the previous terminator (skipping gap tokens) up to that "\r".
  interface ParagraphSpan {
    /** First text index (inclusive). */
    start: number;
    /** Last text index (inclusive); `start - 1` for an empty paragraph. */
    end: number;
    /** Index of the paragraph's terminating "\r". */
    anchor: number;
    para: any;
  }
  const spans: ParagraphSpan[] = [];
  if (paragraphs.length > 0) {
    const sorted = paragraphs.slice().sort((a, b) => a.startIndex - b.startIndex);
    let prevEnd = 0;
    for (const para of sorted) {
      const anchor = para.startIndex;
      let start = Math.min(prevEnd, anchor);
      while (start < anchor && PARAGRAPH_GAP_TOKENS.has(stream[start])) start++;
      spans.push({ start, end: anchor - 1, anchor, para });
      prevEnd = anchor + 1;
    }
  } else {
    // No paragraph table: each "\r\n" pair closes one paragraph.
    let idx = 0;
    for (const part of stream.split(/\r\n/)) {
      if (part === "" && idx >= stream.length) break;
      const anchor = idx + part.length;
      spans.push({ start: idx, end: anchor - 1, anchor, para: undefined });
      idx = anchor + 2;
    }
  }

  // Opaque blocks (tables, images, content controls) are re-emitted verbatim at
  // their stream anchor. The snapshot's copy is preferred because the editor
  // shifts anchors as text is inserted/deleted; the import-time copy is the
  // fallback when the snapshot dropped the field.
  const snapshotBlocks = (data.body as { docxRawBlocks?: RawBlockEntry[] } | undefined)?.docxRawBlocks;
  const rawBlocks = (snapshotBlocks && snapshotBlocks.length > 0 ? snapshotBlocks : ctx?.rawBlocks ?? [])
    .slice()
    .sort((a, b) => a.startIndex - b.startIndex);

  const tableRanges = (((data.body as any)?.tables ?? []) as { startIndex: number; endIndex: number; tableId: string }[])
    .slice()
    .sort((a, b) => a.startIndex - b.startIndex);
  const tableSource: Record<string, any> = (data as any).tableSource ?? {};
  const tableStyles: Record<string, any> = ctx?.tableStyles ?? (data as any).__tableStyles ?? {};
  const imageMeta: Record<string, { xml: string; width?: number; height?: number }> =
    ctx?.imageMeta ?? (data as any).__imageMeta ?? {};
  const drawingOrder: string[] = (data.drawingsOrder ?? []) as string[];
  const blockAt = new Map<number, string>(
    (((data.body as any)?.customBlocks ?? []) as { startIndex: number; blockId: string }[]).map((cb) => [
      cb.startIndex,
      cb.blockId,
    ]),
  );
  let imageIdx = 0;
  const startsInTable = (p: number) => tableRanges.some((t) => p >= t.startIndex && p <= t.endIndex);
  let tableIdx = 0;

  // Load the source package up front so new images can get fresh relationship
  // ids and media part names, and so the original parts are reused on save.
  let originalZip: JSZip | null = null;
  let relsXml = DOC_RELS;
  let contentTypesXml = CONTENT_TYPES;
  if (ctx?.originalBytes) {
    originalZip = await JSZip.loadAsync(ctx.originalBytes);
    relsXml = (await originalZip.file("word/_rels/document.xml.rels")?.async("string")) ?? DOC_RELS;
    contentTypesXml = (await originalZip.file("[Content_Types].xml")?.async("string")) ?? CONTENT_TYPES;
  }
  let relSeq = nextRelSeq(relsXml);
  const pendingMedia: { path: string; bytes: Uint8Array; mime: string; ext: string; rId: string }[] = [];
  const imageBytesCache = new Map<string, ImageBytes>();
  const imageMediaBySource = new Map<string, { rId: string; path: string }>();
  // New drawings take docPr ids above every id already in the package, so an
  // image embedded by an earlier save never shares an id with one embedded now.
  let drawingId = 0;
  const existingDocXml = (await originalZip?.file("word/document.xml")?.async("string")) ?? "";
  for (const m of existingDocXml.matchAll(/<(?:\w+:)?docPr\b[^>]*?\bid="(\d+)"/g)) {
    drawingId = Math.max(drawingId, Number(m[1]));
  }

  const parts: string[] = [];
  let rawIdx = 0;
  // Index in `parts` of the most recent plain paragraph. A section break can only
  // merge into it while it is still the last part (checked against parts.length).
  let plainIdx = -1;
  const sectionParagraphXml = (sectPrXml: string): string => `<w:p><w:pPr>${sectPrXml}</w:pPr></w:p>`;
  /** Emit a section break, merged into the paragraph before it when that is a plain paragraph. */
  const pushSectionBreak = (sectPrXml: string) => {
    if (plainIdx !== -1 && plainIdx === parts.length - 1) {
      parts[plainIdx] = attachSectPr(parts[plainIdx], sectPrXml);
      return;
    }
    parts.push(sectionParagraphXml(sectPrXml));
  };
  for (const { start, end, anchor, para } of spans) {
    while (tableIdx < tableRanges.length && tableRanges[tableIdx].startIndex <= anchor) {
      parts.push(renderTableXml(tableRanges[tableIdx], stream, tableSource, tableStyles));
      tableIdx++;
    }
    // Cell paragraphs and the table's enclosing paragraph are rendered by the
    // table itself, not as body text.
    if (startsInTable(anchor)) continue;
    const text = end >= start ? stream.slice(start, end + 1) : "";
    if (text === CUSTOM_BLOCK) {
      const blockId = blockAt.get(start) ?? drawingOrder[imageIdx];
      const meta = blockId ? imageMeta[blockId] : undefined;
      const drawing = blockId ? ((data as any).drawings ?? {})[blockId] : undefined;
      const width = drawing?.transform?.width ?? meta?.width ?? 100;
      const height = drawing?.transform?.height ?? meta?.height ?? 100;
      if (meta) {
        const xml = width > 0 && height > 0 ? patchDrawingExtent(meta.xml, width, height) : meta.xml;
        parts.push(`<w:p><w:r>${xml}</w:r></w:p>`);
        imageIdx++;
        continue;
      }
      // An image inserted/pasted in the editor has no package part yet. Resolve
      // its source (base64, http(s)/blob URL, or a host-resolved UUID) and add a
      // media part + relationship, so the save keeps the image. Identical
      // sources share one part.
      const source = drawing?.source as string | undefined;
      const sourceType = drawing?.imageSourceType as string | undefined;
      const sourceKey = `${sourceType ?? ""}|${source ?? ""}`;
      let media = imageMediaBySource.get(sourceKey);
      let failure: string | null = null;
      if (!media) {
        try {
          const img = await resolveImageBytes(source, sourceType, ctx, imageBytesCache);
          if (!img) {
            failure = describeImageFailure(source, sourceType);
          } else {
            const rId = `rId${relSeq++}`;
            const path = nextMediaPath(img.ext, originalZip, pendingMedia);
            pendingMedia.push({ path, bytes: img.bytes, mime: img.mime, ext: img.ext, rId });
            media = { rId, path };
            imageMediaBySource.set(sourceKey, media);
          }
        } catch (e) {
          failure = e instanceof Error ? e.message : String(e);
        }
      }
      if (!media) {
        const message = `Cannot save image "${truncateSource(source)}": ${failure ?? "unresolved source"}.`;
        if (ctx?.onImageError) ctx.onImageError(message);
        else throw new Error(message);
        imageIdx++;
        continue;
      }
      const xml = buildInlineDrawingXml(media.rId, width, height, ++drawingId);
      // Reported by Univer drawing id so the caller can reuse this part on its next save.
      if (ctx?.imageMetaOut && blockId) ctx.imageMetaOut[blockId] = { xml, width, height };
      parts.push(`<w:p><w:r>${xml}</w:r></w:p>`);
      imageIdx++;
      continue;
    }
    if (MARKER_TEXTS.has(text) && rawIdx < rawBlocks.length) {
      // Marker paragraph: restore the original block in its place.
      const block = rawBlocks[rawIdx];
      rawIdx++;
      if (block.kind === "section") pushSectionBreak(block.xml);
      else parts.push(block.xml);
      continue;
    }
    parts.push(renderParagraphXml(stream, start, end, para, textRuns));
    plainIdx = parts.length - 1;
  }
  while (tableIdx < tableRanges.length) {
    parts.push(renderTableXml(tableRanges[tableIdx], stream, tableSource, tableStyles));
    tableIdx++;
  }
  // A block whose marker was edited away is still preserved: append it rather
  // than silently dropping it.
  while (rawIdx < rawBlocks.length) {
    const block = rawBlocks[rawIdx];
    rawIdx++;
    // A section break with no marker left is emitted on an empty paragraph of its own.
    parts.push(block.kind === "section" ? sectionParagraphXml(block.xml) : block.xml);
  }

  let sectPrXml =
    '<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>';
  if (originalZip) {
    // Copy the original package and replace only the main document part, so
    // media parts, relationships, styles and headers/footers stay intact. The
    // original sectPr is reused so page setup and header/footer references
    // survive as well.
    const origDoc = await originalZip.file("word/document.xml")?.async("string");
    const originalSectPr = origDoc ? bodySectPrXml(origDoc) : null;
    if (originalSectPr) sectPrXml = originalSectPr;
  }

  const docXml = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
  <w:body>
    ${parts.join("")}
    ${sectPrXml}
  </w:body>
</w:document>`;

  let finalRels = relsXml;
  let finalContentTypes = contentTypesXml;
  if (!finalRels.includes("</Relationships>")) {
    finalRels = finalRels.replace(/<Relationships([^>]*)\/>/, "<Relationships$1></Relationships>");
  }
  for (const m of pendingMedia) {
    const target = m.path.replace(/^word\//, "");
    finalRels = finalRels.replace(
      "</Relationships>",
      `<Relationship Id="${m.rId}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="${target}"/></Relationships>`,
    );
    if (!new RegExp(`Extension="${m.ext}"`, "i").test(finalContentTypes)) {
      finalContentTypes = finalContentTypes.replace(
        "</Types>",
        `<Default Extension="${m.ext}" ContentType="${m.mime}"/></Types>`,
      );
    }
  }

  if (originalZip) {
    originalZip.file("word/_rels/document.xml.rels", finalRels);
    originalZip.file("[Content_Types].xml", finalContentTypes);
    for (const m of pendingMedia) originalZip.file(m.path, m.bytes);
    originalZip.file("word/document.xml", docXml);
    return originalZip.generateAsync({ type: "uint8array" });
  }

  const zip = new JSZip();
  zip.file("[Content_Types].xml", finalContentTypes);
  zip.file("_rels/.rels", RELS);
  zip.file("word/_rels/document.xml.rels", finalRels);
  for (const m of pendingMedia) zip.file(m.path, m.bytes);
  zip.file("word/document.xml", docXml);
  const out = await zip.generateAsync({ type: "uint8array" });
  return out;
}
