import JSZip from "jszip";
import {
  CellValueType,
  type ICellData,
  type IWorkbookData,
  type IWorksheetData,
} from "@univerjs/core";
import { colWidthPx } from "./xlsxStyle";

const MAIN_NS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main";
const RELS_NS =
  "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
const LOG = "[xlsxPackagePatch]";

/**
 * Patches the ORIGINAL xlsx package by diffing the live Univer snapshot against
 * the baseline snapshot captured when the file was opened. Cells the user did
 * not edit stay byte-identical in the original XML, including formatted dates
 * and numbers whose raw <v> differs from their displayed text. Drawings, media,
 * styles, sharedStrings, relationships and theme are always carried through;
 * only changed worksheet XML (and, on a formula edit, calcChain/workbook.xml)
 * is replaced.
 */

interface PatchState {
  formulaChanged: boolean;
}

interface CellChange {
  ref: string;
  live: ICellData;
  baseline: ICellData | undefined;
}

interface FormulaPlan {
  clear: boolean;
  text: string | null;
}

function parseXml(text: string, what: string): Document {
  const doc = new DOMParser().parseFromString(text, "application/xml");
  const error =
    doc.getElementsByTagName("parsererror")[0] ??
    doc.getElementsByTagNameNS("*", "parsererror")[0];
  if (error || doc.documentElement === null) {
    throw new Error(
      `${what}: invalid XML (${error?.textContent?.trim() || "no document element"})`,
    );
  }
  return doc;
}

function childrenNamed(parent: Element, name: string): Element[] {
  return Array.from(parent.children).filter((el) => el.localName === name);
}

function childNamed(parent: Element, name: string): Element | undefined {
  return childrenNamed(parent, name)[0];
}

function colLetters(index0: number): string {
  let n = index0 + 1;
  let out = "";
  while (n > 0) {
    out = String.fromCharCode(65 + ((n - 1) % 26)) + out;
    n = Math.floor((n - 1) / 26);
  }
  return out;
}

function cellRef(row0: number, col0: number): string {
  return `${colLetters(col0)}${row0 + 1}`;
}

function colIndexFromRef(ref: string, part: string): number {
  const match = /^([A-Z]+)(\d+)$/.exec(ref);
  if (!match) throw new Error(`${part}: invalid cell address "${ref}"`);
  let index = 0;
  for (const ch of match[1]) index = index * 26 + (ch.charCodeAt(0) - 64);
  return index - 1;
}

function rowNumberFromRef(ref: string, part: string): number {
  const match = /^[A-Z]+(\d+)$/.exec(ref);
  if (!match) throw new Error(`${part}: invalid cell address "${ref}"`);
  const row = Number(match[1]);
  if (!Number.isInteger(row) || row < 1)
    throw new Error(`${part}: invalid row in cell address "${ref}"`);
  return row;
}

/** Resolve a workbook relationship Target against the part's directory. */
function resolvePart(base: string, target: string): string {
  if (target.startsWith("/")) return target.slice(1);
  const segments = [...base.split("/"), ...target.split("/")].filter(
    (s) => s.length > 0 && s !== ".",
  );
  const out: string[] = [];
  for (const segment of segments) {
    if (segment === "..") out.pop();
    else out.push(segment);
  }
  return out.join("/");
}

async function mapSheetParts(
  zip: JSZip,
  data: IWorkbookData,
): Promise<Map<string, string>> {
  const workbookFile = zip.file("xl/workbook.xml");
  if (!workbookFile)
    throw new Error("xl/workbook.xml is missing from the xlsx package");
  const relsFile = zip.file("xl/_rels/workbook.xml.rels");
  if (!relsFile)
    throw new Error(
      "xl/_rels/workbook.xml.rels is missing from the xlsx package",
    );

  const workbook = parseXml(
    await workbookFile.async("string"),
    "xl/workbook.xml",
  );
  const rels = parseXml(
    await relsFile.async("string"),
    "xl/_rels/workbook.xml.rels",
  );
  const sheetsElement = childNamed(workbook.documentElement, "sheets");
  if (!sheetsElement)
    throw new Error("xl/workbook.xml: missing <sheets> element");

  const ridByName = new Map<string, string>();
  for (const sheet of childrenNamed(sheetsElement, "sheet")) {
    const name = sheet.getAttribute("name");
    const rid =
      sheet.getAttribute("r:id") ?? sheet.getAttributeNS(RELS_NS, "id");
    if (!name)
      throw new Error("xl/workbook.xml: a <sheet> has no name attribute");
    if (!rid)
      throw new Error(
        `xl/workbook.xml: sheet "${name}" has no relationship id (r:id)`,
      );
    if (ridByName.has(name))
      throw new Error(`xl/workbook.xml: duplicate sheet name "${name}"`);
    ridByName.set(name, rid);
  }

  const targetById = new Map<string, string>();
  for (const rel of childrenNamed(rels.documentElement, "Relationship")) {
    const id = rel.getAttribute("Id");
    const target = rel.getAttribute("Target");
    if (id && target) targetById.set(id, target);
  }

  const result = new Map<string, string>();
  for (const id of data.sheetOrder ?? []) {
    const name = data.sheets?.[id]?.name;
    if (typeof name !== "string" || name.length === 0) {
      throw new Error(
        `Univer sheet "${id}" has no name; cannot map it to a worksheet part`,
      );
    }
    const rid = ridByName.get(name);
    if (!rid) {
      throw new Error(
        `Univer sheet "${name}" (id ${id}) was not found in xl/workbook.xml <sheets>`,
      );
    }
    const target = targetById.get(rid);
    if (!target) {
      throw new Error(
        `xl/_rels/workbook.xml.rels has no relationship with Id "${rid}" for sheet "${name}"`,
      );
    }
    const part = resolvePart("xl", target);
    if (!zip.file(part)) {
      throw new Error(
        `worksheet part "${part}" for sheet "${name}" is missing from the xlsx package`,
      );
    }
    result.set(id, part);
  }
  return result;
}

/** Display text of a live Univer cell, in the same terms formatCell() uses. */
function liveCellText(cell: ICellData): string {
  const value = cell.v;
  if (value === null || value === undefined) return "";
  if (typeof value === "boolean") return value ? "TRUE" : "FALSE";
  return String(value);
}

/** Values compare by string coercion so a type-only change is not an edit. */
function cellValueKey(value: unknown): string {
  return value === null || value === undefined ? "" : String(value);
}

/** Key-order-independent serialization for comparing snapshot fragments. */
function stableJson(value: unknown): string {
  if (value === null || typeof value !== "object")
    return JSON.stringify(value) ?? "undefined";
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  const record = value as Record<string, unknown>;
  return `{${Object.keys(record)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableJson(record[key])}`)
    .join(",")}}`;
}

function checkNoStructuralChange(
  baseline: IWorkbookData,
  live: IWorkbookData,
): void {
  const reasons: string[] = [];
  const baseOrder = baseline.sheetOrder ?? [];
  const liveOrder = live.sheetOrder ?? [];
  if (
    baseOrder.length !== liveOrder.length ||
    baseOrder.some((id, i) => id !== liveOrder[i])
  ) {
    reasons.push("sheet order differs (sheet list changed)");
  }

  for (const id of new Set([...baseOrder, ...liveOrder])) {
    const before = baseline.sheets?.[id];
    const after = live.sheets?.[id];
    if (!before || !after) {
      reasons.push(
        `sheet "${id}" ${before ? "is missing from live" : "is new in live"}`,
      );
      continue;
    }
    if (before.rowCount !== after.rowCount) {
      reasons.push(
        `sheet "${id}" rowCount ${String(before.rowCount)} -> ${String(after.rowCount)}`,
      );
    }
    if (before.columnCount !== after.columnCount) {
      reasons.push(
        `sheet "${id}" columnCount ${String(before.columnCount)} -> ${String(after.columnCount)}`,
      );
    }
    if (
      stableJson(before.mergeData ?? []) !== stableJson(after.mergeData ?? [])
    ) {
      reasons.push(`sheet "${id}" mergeData differs`);
    }
  }

  if (reasons.length > 0)
    throw new Error(`Structural change detected: ${reasons.join("; ")}`);
}

interface ValueWrite {
  type: string | null;
  value: Element | null;
}

/** Parses a display number back to a raw value when the original cell was numeric. */
function numericFromDisplay(text: string): number | null {
  const cleaned = text.replace(/[,\s]/g, "").trim();
  if (
    cleaned === "" ||
    !/^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(cleaned)
  )
    return null;
  const value = Number(cleaned);
  return Number.isFinite(value) ? value : null;
}

function valueWrite(
  doc: Document,
  cell: ICellData,
  text: string,
  part: string,
  ref: string,
  originalType: string | null,
): ValueWrite {
  const value = cell.v;
  const type = cell.t;
  // An explicit empty value clears the cell (and its formula), whatever its type.
  if (value === null || value === undefined || text === "")
    return { type: null, value: null };
  // The loader stores display text. When the ORIGINAL cell was numeric and the
  // edited text is still a number, preserve the numeric type (with its style).
  if (
    (originalType === null || originalType === "n") &&
    typeof value === "string"
  ) {
    const parsed = numericFromDisplay(text);
    if (parsed !== null) {
      const v = doc.createElementNS(MAIN_NS, "v");
      v.textContent = String(parsed);
      return { type: null, value: v };
    }
  }
  if (originalType === "b" && typeof value === "string") {
    if (text === "TRUE" || text === "FALSE" || text === "1" || text === "0") {
      const v = doc.createElementNS(MAIN_NS, "v");
      v.textContent = text === "TRUE" || text === "1" ? "1" : "0";
      return { type: "b", value: v };
    }
  }
  if (typeof value === "boolean" || type === CellValueType.BOOLEAN) {
    const v = doc.createElementNS(MAIN_NS, "v");
    v.textContent = value === true || text === "TRUE" ? "1" : "0";
    return { type: "b", value: v };
  }
  if (typeof value === "number" || type === CellValueType.NUMBER) {
    if (typeof value !== "number" || !Number.isFinite(value)) {
      throw new Error(
        `${part}: cell ${ref} has an invalid numeric value "${text}"`,
      );
    }
    const v = doc.createElementNS(MAIN_NS, "v");
    v.textContent = String(value);
    return { type: null, value: v };
  }
  const inline = doc.createElementNS(MAIN_NS, "is");
  const t = doc.createElementNS(MAIN_NS, "t");
  if (/^\s|\s$/.test(text)) t.setAttribute("xml:space", "preserve");
  t.textContent = text;
  inline.appendChild(t);
  return { type: "inlineStr", value: inline };
}

function formulaPlan(
  baseline: ICellData | undefined,
  live: ICellData,
): FormulaPlan {
  const before = typeof baseline?.f === "string" ? baseline.f : "";
  const after = typeof live.f === "string" ? live.f : "";
  if (before !== "" && after === "") return { clear: true, text: null };
  // OOXML <f> omits Univer's leading "=".
  if (after !== "" && after !== before)
    return { clear: false, text: after.replace(/^=/, "") };
  return { clear: false, text: null };
}

function rewriteCell(
  doc: Document,
  element: Element,
  cell: ICellData,
  text: string,
  part: string,
  ref: string,
  formula: FormulaPlan,
  originalType: string | null,
): void {
  const write = valueWrite(doc, cell, text, part, ref, originalType);
  const clear = formula.clear || write.value === null;
  const preserved: Element[] = [];
  let hasFormula = false;
  for (const child of Array.from(element.children)) {
    if (child.localName === "v" || child.localName === "is") continue;
    if (child.localName === "f") {
      if (clear) continue;
      hasFormula = true;
      if (formula.text !== null) child.textContent = formula.text;
    }
    preserved.push(child);
  }
  while (element.firstChild) element.removeChild(element.firstChild);
  if (formula.text !== null && !hasFormula && !clear) {
    const f = doc.createElementNS(MAIN_NS, "f");
    f.textContent = formula.text;
    preserved.unshift(f);
  }
  for (const child of preserved) element.appendChild(child);
  if (write.value) element.appendChild(write.value);
  if (write.type === null) element.removeAttribute("t");
  else element.setAttribute("t", write.type);
}

function insertCell(
  doc: Document,
  sheetData: Element,
  ref: string,
  cell: ICellData,
  text: string,
  part: string,
  formula: FormulaPlan,
): void {
  const rowNumber = rowNumberFromRef(ref, part);
  let row = childrenNamed(sheetData, "row").find(
    (r) => r.getAttribute("r") === String(rowNumber),
  );
  if (!row) {
    row = doc.createElementNS(MAIN_NS, "row");
    row.setAttribute("r", String(rowNumber));
    const next = childrenNamed(sheetData, "row").find(
      (existing) => Number(existing.getAttribute("r")) > rowNumber,
    );
    sheetData.insertBefore(row, next ?? null);
  }
  const element = doc.createElementNS(MAIN_NS, "c");
  element.setAttribute("r", ref);
  const write = valueWrite(doc, cell, text, part, ref, null);
  const clear = formula.clear || write.value === null;
  if (!clear && formula.text !== null) {
    const f = doc.createElementNS(MAIN_NS, "f");
    f.textContent = formula.text;
    element.appendChild(f);
  }
  if (write.type !== null) element.setAttribute("t", write.type);
  if (write.value) element.appendChild(write.value);
  // A new cell has no original s= style to preserve; never write a Univer style id.
  const col = colIndexFromRef(ref, part);
  const next = childrenNamed(row, "c").find(
    (existing) => colIndexFromRef(existing.getAttribute("r") ?? "", part) > col,
  );
  row.insertBefore(element, next ?? null);
}

function collectCellChanges(
  baselineSheet: Partial<IWorksheetData>,
  liveSheet: Partial<IWorksheetData>,
  part: string,
): CellChange[] {
  const base = new Map<string, ICellData>();
  for (const [rowKey, row] of Object.entries(baselineSheet.cellData ?? {})) {
    const rowIndex = Number(rowKey);
    if (!Number.isInteger(rowIndex) || rowIndex < 0) {
      throw new Error(
        `${part}: invalid row index "${rowKey}" in baseline Univer snapshot`,
      );
    }
    for (const [colKey, cell] of Object.entries(row ?? {})) {
      const colIndex = Number(colKey);
      if (!Number.isInteger(colIndex) || colIndex < 0) {
        throw new Error(
          `${part}: invalid column index "${colKey}" in baseline Univer snapshot`,
        );
      }
      if (cell === null || cell === undefined) continue;
      base.set(cellRef(rowIndex, colIndex), cell);
    }
  }

  const changes: CellChange[] = [];
  for (const [rowKey, row] of Object.entries(liveSheet.cellData ?? {})) {
    const rowIndex = Number(rowKey);
    if (!Number.isInteger(rowIndex) || rowIndex < 0) {
      throw new Error(
        `${part}: invalid row index "${rowKey}" in Univer snapshot`,
      );
    }
    for (const [colKey, cell] of Object.entries(row ?? {})) {
      const colIndex = Number(colKey);
      if (!Number.isInteger(colIndex) || colIndex < 0) {
        throw new Error(
          `${part}: invalid column index "${colKey}" in Univer snapshot`,
        );
      }
      // Absence (or a null placeholder) means untouched, never a clear.
      if (cell === null || cell === undefined) continue;
      const ref = cellRef(rowIndex, colIndex);
      const baseline = base.get(ref);
      if (
        baseline &&
        cellValueKey((baseline as ICellData).v) ===
          cellValueKey((cell as ICellData).v) &&
        ((baseline as ICellData).f ?? "") === ((cell as ICellData).f ?? "")
      ) {
        continue;
      }
      changes.push({
        ref,
        live: cell as ICellData,
        baseline: baseline as ICellData | undefined,
      });
    }
  }
  return changes;
}

function applyCellChanges(
  doc: Document,
  sheetData: Element,
  changes: CellChange[],
  part: string,
  state: PatchState,
): void {
  const existing = new Map<string, Element>();
  for (const row of childrenNamed(sheetData, "row")) {
    for (const element of childrenNamed(row, "c")) {
      const ref = element.getAttribute("r");
      if (!ref) throw new Error(`${part}: a <c> element has no r attribute`);
      existing.set(ref, element);
    }
  }

  for (const { ref, live, baseline } of changes) {
    const element = existing.get(ref) ?? null;
    const hadXmlFormula =
      element !== null && childNamed(element, "f") !== undefined;
    const baseFormula = typeof baseline?.f === "string" ? baseline.f : "";
    const liveFormula = typeof live.f === "string" ? live.f : "";
    if (hadXmlFormula || baseFormula !== "" || liveFormula !== "")
      state.formulaChanged = true;
    const plan = formulaPlan(baseline, live);
    const text = liveCellText(live);
    if (element)
      rewriteCell(
        doc,
        element,
        live,
        text,
        part,
        ref,
        plan,
        element.getAttribute("t"),
      );
    else insertCell(doc, sheetData, ref, live, text, part, plan);
  }
}

function patchColumns(
  doc: Document,
  sheet: Partial<IWorksheetData>,
  part: string,
): void {
  const root = doc.documentElement;
  const containers = childrenNamed(root, "cols");
  const originalCols: {
    el: Element;
    min: number;
    max: number;
    chars: number | null;
  }[] = [];
  for (const cols of containers) {
    for (const col of childrenNamed(cols, "col")) {
      const min = Number(col.getAttribute("min"));
      const max = Number(col.getAttribute("max"));
      if (
        !Number.isInteger(min) ||
        !Number.isInteger(max) ||
        min < 1 ||
        max < min
      ) {
        throw new Error(`${part}: unsupported <col min="${min}" max="${max}">`);
      }
      const width = col.getAttribute("width");
      let chars: number | null = null;
      if (width !== null) {
        chars = Number(width);
        if (!Number.isFinite(chars))
          throw new Error(`${part}: unsupported <col width="${width}">`);
      }
      originalCols.push({ el: col, min, max, chars });
    }
  }

  const original = new Map<number, number>();
  for (const col of originalCols) {
    if (col.chars === null) continue;
    for (let i = col.min; i <= col.max; i++)
      original.set(i, colWidthPx(col.chars));
  }

  const live = new Map<number, number>();
  for (const [key, col] of Object.entries(sheet.columnData ?? {})) {
    const index = Number(key);
    if (!Number.isInteger(index) || index < 0) {
      throw new Error(
        `${part}: invalid column index "${key}" in Univer snapshot`,
      );
    }
    const width = col?.w;
    if (width === undefined) continue;
    if (typeof width !== "number" || !Number.isFinite(width)) {
      throw new Error(
        `${part}: unsupported column width ${String(width)} at column ${index}`,
      );
    }
    live.set(index + 1, width);
  }

  const defaultPx = colWidthPx(undefined);
  let changed = [...live].some(
    ([i, px]) => (original.get(i) ?? defaultPx) !== px,
  );
  if (!changed) changed = [...original.keys()].some((i) => !live.has(i));
  if (!changed) return;

  // Keep every original <col> so hidden/outline/style metadata survives a width
  // edit. Only widths of columns present in live change; split a ranged <col>
  // into per-column clones so unchanged columns keep their own attributes.
  const result: Element[] = [];
  const covered = new Set<number>();
  const widthAttr = (px: number) =>
    String(Math.round(((px - 5) / 7) * 1000) / 1000);
  for (const col of originalCols) {
    const current = () =>
      col.chars === null ? defaultPx : colWidthPx(col.chars);
    const needsWidth = (i: number) => live.has(i) && live.get(i) !== current();
    let split = false;
    for (let i = col.min; i <= col.max; i++) {
      if (needsWidth(i)) {
        split = true;
        break;
      }
    }
    if (!split) {
      result.push(col.el);
      for (let i = col.min; i <= col.max; i++) covered.add(i);
      continue;
    }
    for (let i = col.min; i <= col.max; i++) {
      const clone = col.el.cloneNode(true) as Element;
      clone.setAttribute("min", String(i));
      clone.setAttribute("max", String(i));
      clone.setAttribute("width", widthAttr(live.get(i) ?? current()));
      clone.setAttribute("customWidth", "1");
      result.push(clone);
      covered.add(i);
    }
  }
  for (const [index, px] of [...live].sort((a, b) => a[0] - b[0])) {
    if (covered.has(index)) continue;
    const col = doc.createElementNS(MAIN_NS, "col");
    col.setAttribute("min", String(index));
    col.setAttribute("max", String(index));
    col.setAttribute("width", widthAttr(px));
    col.setAttribute("customWidth", "1");
    result.push(col);
  }
  result.sort(
    (a, b) =>
      Number(a.getAttribute("min")) - Number(b.getAttribute("min")) ||
      Number(a.getAttribute("max")) - Number(b.getAttribute("max")),
  );
  for (const container of containers) root.removeChild(container);
  if (result.length === 0) return;
  const cols = doc.createElementNS(MAIN_NS, "cols");
  for (const col of result) cols.appendChild(col);
  root.insertBefore(cols, childNamed(root, "sheetData") ?? null);
}

function dropCalcChainRefs(
  rels: string,
  contentTypes: string,
): { rels: string; contentTypes: string } {
  return {
    rels: rels.replace(
      /<Relationship\b[^>]*\/>|<Relationship\b[^>]*>[\s\S]*?<\/Relationship>/g,
      (match) => (/Type="[^"]*\/calcChain"/.test(match) ? "" : match),
    ),
    contentTypes: contentTypes.replace(/<Override\b[^>]*\/>/g, (match) =>
      /PartName="\/xl\/calcChain\.xml"/.test(match) ? "" : match,
    ),
  };
}

function enableFullCalcOnLoad(xml: string): string {
  const selfClosing = /<calcPr\b[^>]*\/>/;
  const paired = /<calcPr\b[^>]*>[\s\S]*?<\/calcPr>/;
  const tag = selfClosing.exec(xml)?.[0] ?? paired.exec(xml)?.[0];
  if (!tag) {
    if (!xml.includes("</workbook>"))
      throw new Error("xl/workbook.xml: missing </workbook> for calcPr");
    return xml.replace(
      "</workbook>",
      '<calcPr fullCalcOnLoad="1"/></workbook>',
    );
  }
  let updated: string;
  if (/\bfullCalcOnLoad\s*=/.test(tag)) {
    updated = tag.replace(/(\bfullCalcOnLoad\s*=\s*")[^"]*(")/, "$11$2");
  } else if (tag.endsWith("/>")) {
    updated = tag.replace(/\/>$/, ' fullCalcOnLoad="1"/>');
  } else {
    updated = tag.replace(/>$/, ' fullCalcOnLoad="1">');
  }
  return xml.replace(tag, () => updated);
}

/**
 * Patches the original xlsx package in place and returns fresh bytes.
 * Every part except changed worksheet XML (and, on a formula edit, the
 * calcChain parts and workbook.xml calcPr) is carried through byte-identically.
 */
export async function patchWorkbookBytes(
  original: ArrayBuffer,
  baseline: IWorkbookData,
  live: IWorkbookData,
): Promise<ArrayBuffer> {
  let attempted =
    "check for structural changes between the baseline and live snapshots";
  try {
    checkNoStructuralChange(baseline, live);

    attempted = "open the original xlsx package with JSZip";
    const zip = await JSZip.loadAsync(original);

    attempted = "map Univer sheets to worksheet parts via xl/workbook.xml";
    const parts = await mapSheetParts(zip, baseline);
    const state: PatchState = { formulaChanged: false };

    for (const [id, part] of parts) {
      attempted = `patch worksheet part ${part} for Univer sheet ${id}`;
      const baselineSheet = baseline.sheets?.[id] ?? {};
      const liveSheet = live.sheets?.[id] ?? {};
      const changes = collectCellChanges(baselineSheet, liveSheet, part);
      const columnsChanged =
        stableJson(baselineSheet.columnData ?? {}) !==
        stableJson(liveSheet.columnData ?? {});
      if (changes.length === 0 && !columnsChanged) continue;

      const file = zip.file(part);
      if (!file)
        throw new Error(
          `worksheet part "${part}" is no longer present in the package`,
        );
      const xml = await file.async("string");
      const doc = parseXml(xml, part);
      const sheetData = childNamed(doc.documentElement, "sheetData");
      if (!sheetData) throw new Error(`${part}: missing <sheetData> element`);
      applyCellChanges(doc, sheetData, changes, part, state);
      if (columnsChanged) patchColumns(doc, liveSheet, part);
      const declaration = /^<\?xml[\s\S]*?\?>/.exec(xml)?.[0] ?? "";
      zip.file(part, declaration + new XMLSerializer().serializeToString(doc));
    }

    if (state.formulaChanged) {
      attempted =
        "drop xl/calcChain.xml and its references after a formula cell changed";
      console.warn(
        `${LOG} A formula cell changed: dropping xl/calcChain.xml, its workbook relationship and its ` +
          `[Content_Types].xml override, because the cached formula chain no longer matches the edited values.`,
      );
      zip.remove("xl/calcChain.xml");
      const relsFile = zip.file("xl/_rels/workbook.xml.rels");
      const contentTypesFile = zip.file("[Content_Types].xml");
      const workbookFile = zip.file("xl/workbook.xml");
      if (!relsFile || !contentTypesFile || !workbookFile) {
        throw new Error(
          "xl/_rels/workbook.xml.rels, [Content_Types].xml and xl/workbook.xml are required after a formula change",
        );
      }
      const updated = dropCalcChainRefs(
        await relsFile.async("string"),
        await contentTypesFile.async("string"),
      );
      zip.file("xl/_rels/workbook.xml.rels", updated.rels);
      zip.file("[Content_Types].xml", updated.contentTypes);
      zip.file(
        "xl/workbook.xml",
        enableFullCalcOnLoad(await workbookFile.async("string")),
      );
    }

    attempted = "generate the patched xlsx ArrayBuffer";
    return await zip.generateAsync({ type: "arraybuffer" });
  } catch (error) {
    console.error(`${LOG} Failed while attempting to ${attempted}:`, error);
    throw error;
  }
}
