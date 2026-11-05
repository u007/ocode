import { describe, expect, it } from "vitest";
import { Workbook } from "exceljs";
import { CellValueType, LocaleType, type IWorkbookData } from "@univerjs/core";
import { rawContentHash, serializeWorkbookBytes } from "./univerSerialize";
import { workbookFromFileBytes } from "./univerData";

function syntheticData(): IWorkbookData {
  return {
    id: "test",
    name: "Spreadsheet",
    appVersion: "test",
    locale: LocaleType.EN_US,
    styles: {},
    sheetOrder: ["sheet-1", "sheet-2"],
    sheets: {
      "sheet-1": {
        id: "sheet-1",
        name: "Stock",
        cellData: {
          "0": {
            "0": { v: "name", t: CellValueType.STRING },
            "1": { v: "qty", t: CellValueType.STRING },
          },
          "1": {
            "0": { v: "apples", t: CellValueType.STRING },
            "1": { v: 3, t: CellValueType.NUMBER },
            "2": { v: true, t: CellValueType.BOOLEAN },
            "3": { v: "007", t: CellValueType.FORCE_STRING },
          },
          "2": {
            "0": { v: 0, t: CellValueType.NUMBER },
          },
        },
        columnData: { "0": { w: 75 } },
        mergeData: [{ startRow: 0, startColumn: 3, endRow: 1, endColumn: 3 }],
      },
      "sheet-2": {
        id: "sheet-2",
        name: "Cities",
        cellData: { "0": { "0": { v: "Oslo", t: CellValueType.STRING } } },
      },
    },
  } as IWorkbookData;
}

describe("serializeWorkbookBytes", () => {
  it("round-trips values, types, sheet names and merges through ExcelJS", async () => {
    const bytes = await serializeWorkbookBytes(syntheticData());
    expect(bytes.byteLength).toBeGreaterThan(0);

    const wb = new Workbook();
    // ExcelJS load() takes ArrayBuffer|Buffer; normalize for Node.
    await wb.xlsx.load(bytes as unknown as Parameters<typeof wb.xlsx.load>[0]);
    expect(wb.worksheets.map((ws) => ws.name)).toEqual(["Stock", "Cities"]);

    const stock = wb.getWorksheet("Stock");
    expect(stock?.getCell("A1").value).toBe("name");
    expect(stock?.getCell("A2").value).toBe("apples");
    expect(stock?.getCell("B2").value).toBe(3);
    expect(typeof stock?.getCell("B2").value).toBe("number");
    expect(stock?.getCell("C2").value).toBe(true);
    expect(stock?.getCell("D1").address).toBe("D1");
    expect(stock?.getCell("A3").value).toBe(0);
    // Empty cells produce no value.
    expect(stock?.getCell("E5").value).toBeNull();

    // Column width: px 75 -> chars (75-5)/7 = 10.
    expect(stock?.getColumn(1).width).toBeCloseTo(10, 5);
    // Merge D1:D2 exists (ExcelJS keeps no public merge list; read the table).
    const merges: unknown = stock === undefined ? undefined : Reflect.get(stock, "_merges");
    expect(merges).toBeDefined();
    expect(Object.keys((merges as Record<string, unknown>) ?? {}).length).toBeGreaterThan(0);

    const cities = wb.getWorksheet("Cities");
    expect(cities?.getCell("A1").value).toBe("Oslo");
  });

  it("writes a formula cell when f is present", async () => {
    const data = syntheticData();
    (data.sheets["sheet-1"] as unknown as { cellData: Record<string, Record<string, unknown>> }).cellData["1"]["1"] = {
      f: "SUM(B1:B3)",
      v: 6,
      t: CellValueType.NUMBER,
    };
    const bytes = await serializeWorkbookBytes(data);
    const wb = new Workbook();
    await wb.xlsx.load(bytes as unknown as Parameters<typeof wb.xlsx.load>[0]);
    const b2 = wb.getWorksheet("Stock")?.getCell("B2").value;
    expect(b2).toEqual(expect.objectContaining({ formula: "SUM(B1:B3)" }));
  });
});

// Loader -> save round trip. The loader keeps display text only (univerData.ts
// excelJsCell stores formatCell output as STRING), so the save writes text.
// The LOSS assertions below pin today's behaviour on purpose: when the loader
// starts keeping raw values, formulas and number formats, flip them to the
// original types and the test will say so.
describe("serializeWorkbookBytes after the loader (round trip)", () => {
  it("keeps text and sheet structure; numbers, dates, booleans and formulas come back as text", async () => {
    const src = new Workbook();
    const ws = src.addWorksheet("Ledger");
    ws.getCell("A1").value = "Item";
    ws.getCell("B1").value = "Amount";
    ws.getCell("A2").value = "Rent";
    ws.getCell("B2").value = 1234.5;
    ws.getCell("B2").numFmt = "#,##0.00";
    ws.getCell("A3").value = "Date";
    ws.getCell("B3").value = new Date(Date.UTC(2024, 0, 15));
    ws.getCell("B3").numFmt = "yyyy-mm-dd";
    ws.getCell("A4").value = "Flag";
    ws.getCell("B4").value = true;
    ws.getCell("B5").value = { formula: "B2*2", result: 2469 };
    const loaded = await workbookFromFileBytes("ledger.xlsx", (await src.xlsx.writeBuffer()) as ArrayBuffer);
    const bytes = await serializeWorkbookBytes(loaded.data);

    const out = new Workbook();
    await out.xlsx.load(bytes as unknown as Parameters<typeof out.xlsx.load>[0]);
    const saved = out.getWorksheet("Ledger");
    expect(saved?.getCell("A1").value).toBe("Item");
    expect(saved?.getCell("A2").value).toBe("Rent");
    expect(saved?.getCell("B1").value).toBe("Amount");

    // KNOWN LOSS: 1234.5 with "#,##0.00" saves as the text "1,234.50".
    expect(saved?.getCell("B2").value).toBe("1,234.50");
    // KNOWN LOSS: the date saves as text "2024-01-15", not a date serial.
    expect(saved?.getCell("B3").value).toBe("2024-01-15");
    // KNOWN LOSS: TRUE saves as the text "TRUE".
    expect(saved?.getCell("B4").value).toBe("TRUE");
    // KNOWN LOSS: =B2*2 saves as the cached text "2469", with no formula.
    expect(saved?.getCell("B5").value).toBe("2469");
  });
});

// Vectors are mirrored in internal/server/handler_files_raw_test.go
// (TestRawContentHashIsSHA256Hex). Change both or neither.
describe("rawContentHash", () => {
  it("is lowercase hex SHA-256 of the empty input", () => {
    expect(rawContentHash(new ArrayBuffer(0))).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
  });

  it("is SHA-256 of 'abc' (FIPS 180-4 vector)", () => {
    const bytes = new TextEncoder().encode("abc").buffer as ArrayBuffer;
    expect(rawContentHash(bytes)).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
  });

  it("hashes invalid UTF-8 bytes byte-exactly and tells 0xff from 0xfd", () => {
    const hi = new Uint8Array([0xff, 0xfe, 0x00]);
    const lo = new Uint8Array([0xfd, 0xfe, 0x00]);
    expect(rawContentHash(hi)).toBe("ba778c0261008c8f71ae4061ad0162ffcbe63b52c91f89f236738131d1217ec7");
    expect(rawContentHash(hi.buffer as ArrayBuffer)).toBe(rawContentHash(hi));
    expect(rawContentHash(lo)).toBe("7af5f0e495c9b5a16918176e41bec43c582443617f9daf3c35d5730cf4c4d6ab");
    expect(rawContentHash(hi)).not.toBe(rawContentHash(lo));
  });

  it("hashes only the view when given a Uint8Array with an offset", () => {
    const backing = new Uint8Array([0x00, 0x61, 0x62, 0x63, 0x00]);
    expect(rawContentHash(backing.subarray(1, 4))).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
  });
});
