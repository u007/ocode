import { CellValueType, type IWorkbookData } from "@univerjs/core";
import { sha256 } from "@noble/hashes/sha256";
import { bytesToHex } from "@noble/hashes/utils";

/**
 * Serializes Univer `IWorkbookData` back to .xlsx bytes via ExcelJS.
 *
 * FIDELITY LIMITS (values-only round trip — pre-acknowledged, by design):
 * the loader (`univerData.ts` `excelJsCell`) already discarded formulas,
 * number formats and style granularity on load, storing display text as
 * STRING cells only. So a save writes back the edited VALUES, not the
 * original file's formulas, charts, images, pivots or full formatting.
 * This module does not attempt to recover what the loader dropped.
 * Styles are intentionally NOT written back either: `IWorksheetData`
 * carries only style IDs into `IWorkbookData.styles`, and re-emitting the
 * loader's simplified styles would be lossy and misleading.
 */

/**
 * Lowercase hex SHA-256 of the exact bytes: the `expected_hash` for
 * `PUT /api/files/raw` (`rawContentHash` in internal/server/handler_files.go
 * hashes the same bytes). Pure JS via noble, because `crypto.subtle` is
 * unavailable on plain-http origins.
 */
export function rawContentHash(bytes: ArrayBuffer | Uint8Array): string {
  const view = ArrayBuffer.isView(bytes)
    ? new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength)
    : new Uint8Array(bytes);
  return bytesToHex(sha256(view));
}

/** Univer column pixel width → ExcelJS width in characters. Inverse of colWidthPx. */
function pxToChars(px: number): number {
  return Math.max(0, (px - 5) / 7);
}

/**
 * Builds .xlsx bytes from `IWorkbookData` (the live snapshot from the Univer
 * handle). Blank/absent cells are skipped; `null` values emit no cell.
 */
export async function serializeWorkbookBytes(data: IWorkbookData): Promise<ArrayBuffer> {
  const { Workbook } = await import("exceljs");
  const wb = new Workbook();

  for (const sheetId of data.sheetOrder) {
    const sheet = data.sheets[sheetId];
    const name = sheet?.name ?? sheetId;
    // ExcelJS requires sheet names ≤31 chars and forbids []:*?/\; Univer data
    // comes from the loader so this is a guard, not an expected path.
    const ws = wb.addWorksheet(name.slice(0, 31));

    // Column widths: Univer stores pixels per 0-based column index.
    const columnData = sheet?.columnData ?? {};
    for (const [colKey, col] of Object.entries(columnData)) {
      const w = (col as { w?: number } | undefined)?.w;
      if (typeof w === "number") {
        ws.getColumn(Number(colKey) + 1).width = pxToChars(w);
      }
    }

    // Merges: 0-based inclusive ranges → ExcelJS "A1:B2" address strings.
    for (const m of sheet?.mergeData ?? []) {
      const top = ws.getCell(m.startRow + 1, m.startColumn + 1).address;
      const bottom = ws.getCell(m.endRow + 1, m.endColumn + 1).address;
      ws.mergeCells(top, bottom);
    }

    // Cells: iterate the sparse maps; row/col keys are strings of 0-based ints.
    const cellData = sheet?.cellData ?? {};
    for (const [rowKey, row] of Object.entries(cellData)) {
      const r = Number(rowKey) + 1;
      for (const [colKey, cell] of Object.entries(row ?? {})) {
        if (cell === null || cell === undefined) continue;
        const c = Number(colKey) + 1;
        const target = ws.getCell(r, c);
        const f = (cell as { f?: string }).f;
        if (typeof f === "string" && f.length > 0) {
          target.value = { formula: f.startsWith("=") ? f.slice(1) : f };
          continue;
        }
        const v = (cell as { v?: unknown }).v;
        if (v === null || v === undefined) continue;
        const t = (cell as { t?: number }).t;
        if (t === CellValueType.NUMBER || (t === undefined && typeof v === "number")) {
          target.value = Number(v);
        } else if (t === CellValueType.BOOLEAN || (t === undefined && typeof v === "boolean")) {
          target.value = Boolean(v);
        } else {
          target.value = String(v);
        }
      }
    }
  }

  const buf = (await wb.xlsx.writeBuffer()) as unknown as Uint8Array | ArrayBuffer;
  // writeBuffer returns a Node Buffer in this (jsdom) environment. jsdom runs
  // in a separate realm, so neither `instanceof ArrayBuffer` nor JSZip's
  // cross-realm type detection accepts the Node-realm buffer. Copy the bytes
  // into a fresh jsdom-realm ArrayBuffer so every downstream consumer
  // (fetch body, JSZip, hash) sees a plain ArrayBuffer.
  if (typeof buf === "object" && buf !== null && "buffer" in buf && "byteOffset" in buf && "byteLength" in buf) {
    const u8 = buf as unknown as Uint8Array;
    const src = new Uint8Array(u8.buffer, u8.byteOffset, u8.byteLength);
    const out = new ArrayBuffer(src.byteLength);
    new Uint8Array(out).set(src);
    return out;
  }
  const ab = buf as ArrayBuffer;
  const out = new ArrayBuffer(ab.byteLength);
  new Uint8Array(out).set(new Uint8Array(ab));
  return out;
}
