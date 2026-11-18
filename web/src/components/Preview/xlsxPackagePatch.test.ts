import JSZip from "jszip";
import { CellValueType, LocaleType, type IWorkbookData } from "@univerjs/core";
import { describe, expect, it, vi } from "vitest";
import { patchWorkbookBytes } from "./xlsxPackagePatch";
import { colWidthPx } from "./xlsxStyle";

const MAIN = "http://schemas.openxmlformats.org/spreadsheetml/2006/main";
const RELS =
  "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
const PKG_RELS = "http://schemas.openxmlformats.org/package/2006/relationships";

const DECL = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>';

function fixture(
  options: {
    withCalcChain?: boolean;
    sheetName?: string;
    calcPr?: string;
    cols?: string;
  } = {},
): JSZip {
  const { withCalcChain = false, sheetName = "Sheet1", calcPr, cols } = options;
  const zip = new JSZip();
  zip.file(
    "[Content_Types].xml",
    `${DECL}\n<Types xmlns="${PKG_RELS}/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Default Extension="png" ContentType="image/png"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
${withCalcChain ? '<Override PartName="/xl/calcChain.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.calcChain+xml"/>\n' : ""}</Types>`,
  );
  zip.file(
    "_rels/.rels",
    `${DECL}\n<Relationships xmlns="${PKG_RELS}"><Relationship Id="rId1" Type="${RELS}/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
  );
  zip.file(
    "xl/workbook.xml",
    `${DECL}\n<workbook xmlns="${MAIN}" xmlns:r="${RELS}"><sheets><sheet name="${sheetName}" sheetId="1" r:id="rId1"/></sheets>${calcPr ?? ""}</workbook>`,
  );
  zip.file(
    "xl/_rels/workbook.xml.rels",
    `${DECL}\n<Relationships xmlns="${PKG_RELS}">
<Relationship Id="rId1" Type="${RELS}/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="${RELS}/styles" Target="styles.xml"/>
<Relationship Id="rId3" Type="${RELS}/sharedStrings" Target="sharedStrings.xml"/>
<Relationship Id="rId5" Type="${RELS}/drawing" Target="drawings/drawing1.xml"/>
${withCalcChain ? `<Relationship Id="rId4" Type="${RELS}/calcChain" Target="calcChain.xml"/>\n` : ""}</Relationships>`,
  );
  zip.file(
    "xl/worksheets/sheet1.xml",
    `${DECL}\n<worksheet xmlns="${MAIN}">${cols ?? ""}<sheetData><row r="1">
<c r="A1" s="1" t="s"><v>0</v></c>
<c r="B1" s="2"><v>42</v></c>
<c r="C1" s="3"><f>1+1</f><v>2</v></c>
<c r="D1" s="4"><v>44927</v></c>
</row></sheetData></worksheet>`,
  );
  zip.file(
    "xl/sharedStrings.xml",
    `${DECL}\n<sst xmlns="${MAIN}" count="1" uniqueCount="1"><si><t>Hello</t></si></sst>`,
  );
  zip.file("xl/styles.xml", `${DECL}\n<styleSheet xmlns="${MAIN}"/>`);
  zip.file(
    "xl/drawings/drawing1.xml",
    `${DECL}\n<xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"/>`,
  );
  zip.file(
    "xl/media/image1.png",
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  );
  if (withCalcChain)
    zip.file("xl/calcChain.xml", `${DECL}\n<calcChain xmlns="${MAIN}"/>`);
  return zip;
}

/** Snapshot as captured at file-open: display text, including the date cell. */
function snapshot(): IWorkbookData {
  return {
    id: "test",
    name: "Spreadsheet",
    appVersion: "test",
    locale: LocaleType.EN_US,
    styles: {},
    sheetOrder: ["sheet-1"],
    sheets: {
      "sheet-1": {
        id: "sheet-1",
        name: "Sheet1",
        rowCount: 100,
        columnCount: 10,
        cellData: {
          0: {
            0: { v: "Hello", t: CellValueType.STRING, s: 1 },
            1: { v: 42, t: CellValueType.NUMBER, s: 2 },
            2: { v: "2", t: CellValueType.STRING, s: 3 },
            3: { v: "3/15/23", t: CellValueType.STRING, s: 4 },
          },
        },
        columnData: {},
        mergeData: [],
      },
    },
  } as IWorkbookData;
}

function baseline(): IWorkbookData {
  return JSON.parse(JSON.stringify(snapshot())) as IWorkbookData;
}

async function partBytes(zip: JSZip, name: string): Promise<Uint8Array> {
  const file = zip.file(name);
  if (!file) throw new Error(`fixture is missing ${name}`);
  return file.async("uint8array");
}

async function partText(zip: JSZip, name: string): Promise<string> {
  const file = zip.file(name);
  if (!file) throw new Error(`fixture is missing ${name}`);
  return file.async("string");
}

function cellXml(sheetXml: string, ref: string): string {
  const pattern = new RegExp(
    `<c\\b[^>]*\\br="${ref}"[^>]*\\/>|<c\\b[^>]*\\br="${ref}"[^>]*>[\\s\\S]*?<\\/c>`,
  );
  const match = pattern.exec(sheetXml);
  if (!match) throw new Error(`output has no cell ${ref}`);
  return match[0];
}

describe("patchWorkbookBytes", () => {
  it("patches only changed cells and keeps every other part byte-identical", async () => {
    const source = fixture();
    const original = await source.generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].cellData![0][0] = {
      v: "Hello!",
      t: CellValueType.STRING,
      s: "1",
    };
    live.sheets["sheet-1"].cellData![0][1] = {
      v: 43,
      t: CellValueType.NUMBER,
      s: "2",
    };
    const output = await patchWorkbookBytes(original, baseline(), live);
    const result = await JSZip.loadAsync(output);

    expect(Object.keys(result.files).sort()).toEqual(
      Object.keys(source.files).sort(),
    );
    for (const name of [
      "xl/media/image1.png",
      "xl/drawings/drawing1.xml",
      "xl/styles.xml",
      "xl/sharedStrings.xml",
      "[Content_Types].xml",
      "xl/_rels/workbook.xml.rels",
      "xl/workbook.xml",
    ]) {
      expect(await partBytes(result, name)).toEqual(
        await partBytes(source, name),
      );
    }

    const sheetXml = await partText(result, "xl/worksheets/sheet1.xml");
    const a1 = /<c\b[^>]*r="A1"[^>]*>/.exec(sheetXml)?.[0] ?? "";
    expect(a1).toContain('s="1"');
    expect(a1).toContain('t="inlineStr"');
    expect(sheetXml).toContain("<is><t>Hello!</t></is>");
    expect(sheetXml).toMatch(/<c r="B1" s="2"><v>43<\/v><\/c>/);
    // Untouched formula/date cells keep their raw XML, style and <f> byte-for-byte.
    expect(cellXml(sheetXml, "C1")).toBe(
      '<c r="C1" s="3"><f>1+1</f><v>2</v></c>',
    );
    expect(cellXml(sheetXml, "D1")).toBe('<c r="D1" s="4"><v>44927</v></c>');
    expect(result.file("xl/calcChain.xml")).toBeNull();
  });

  it("leaves an untouched formatted date cell byte-identical and numeric", async () => {
    const source = fixture();
    const original = await source.generateAsync({ type: "arraybuffer" });
    const result = await JSZip.loadAsync(
      await patchWorkbookBytes(original, baseline(), snapshot()),
    );

    // Nothing changed, so the whole worksheet part is skipped, not re-serialized.
    expect(await partBytes(result, "xl/worksheets/sheet1.xml")).toEqual(
      await partBytes(source, "xl/worksheets/sheet1.xml"),
    );
    const d1 = cellXml(
      await partText(result, "xl/worksheets/sheet1.xml"),
      "D1",
    );
    expect(d1).toBe('<c r="D1" s="4"><v>44927</v></c>');
    expect(d1).not.toContain("inlineStr");
  });

  it("writes a changed string cell as inlineStr with its original style", async () => {
    const source = fixture();
    const original = await source.generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].cellData![0][0] = {
      v: "Changed",
      t: CellValueType.STRING,
      s: "1",
    };
    const sheetXml = await partText(
      await JSZip.loadAsync(
        await patchWorkbookBytes(original, baseline(), live),
      ),
      "xl/worksheets/sheet1.xml",
    );
    const a1 = cellXml(sheetXml, "A1");
    expect(a1).toContain('s="1"');
    expect(a1).toContain('t="inlineStr"');
    expect(a1).toContain("<is><t>Changed</t></is>");
  });

  it("clears cells with explicit empty values but leaves absent cells untouched", async () => {
    const original = await fixture().generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].cellData![0][0] = {
      v: "",
      t: CellValueType.STRING,
      s: "1",
    };
    live.sheets["sheet-1"].cellData![0][1] = {
      v: null,
      t: CellValueType.NUMBER,
      s: "2",
    };
    delete live.sheets["sheet-1"].cellData![0][2]; // C1 absent => untouched, <f> kept
    const sheetXml = await partText(
      await JSZip.loadAsync(
        await patchWorkbookBytes(original, baseline(), live),
      ),
      "xl/worksheets/sheet1.xml",
    );
    for (const ref of ["A1", "B1"]) {
      const cell = cellXml(sheetXml, ref);
      expect(cell).toContain(`s="${ref === "A1" ? 1 : 2}"`);
      expect(cell).not.toContain("<v>");
      expect(cell).not.toContain("<is>");
      expect(cell).not.toContain("<f>");
    }
    expect(cellXml(sheetXml, "C1")).toBe(
      '<c r="C1" s="3"><f>1+1</f><v>2</v></c>',
    );
    expect(cellXml(sheetXml, "D1")).toBe('<c r="D1" s="4"><v>44927</v></c>');
  });

  it("throws when a Univer sheet cannot be mapped to a worksheet part", async () => {
    const original = await fixture().generateAsync({ type: "arraybuffer" });
    const base = baseline();
    base.sheets["sheet-1"].name = "Missing Sheet";
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      await expect(
        patchWorkbookBytes(original, base, snapshot()),
      ).rejects.toThrow(/was not found in xl\/workbook\.xml/);
    } finally {
      errors.mockRestore();
    }
  });

  it("throws when a mapped worksheet part is missing from the package", async () => {
    const source = fixture();
    source.remove("xl/worksheets/sheet1.xml");
    const original = await source.generateAsync({ type: "arraybuffer" });
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      await expect(
        patchWorkbookBytes(original, baseline(), snapshot()),
      ).rejects.toThrow(/worksheet part .* is missing from the xlsx package/);
    } finally {
      errors.mockRestore();
    }
  });

  it("drops calcChain.xml and sets fullCalcOnLoad when a formula cell changes", async () => {
    const source = fixture({ withCalcChain: true });
    const original = await source.generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].cellData![0][2] = {
      v: "5",
      t: CellValueType.STRING,
      s: "3",
    };
    const warnings = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      const result = await JSZip.loadAsync(
        await patchWorkbookBytes(original, baseline(), live),
      );
      expect(warnings).toHaveBeenCalledWith(
        expect.stringContaining("formula cell changed"),
      );
      expect(result.file("xl/calcChain.xml")).toBeNull();
      expect(
        await partText(result, "xl/_rels/workbook.xml.rels"),
      ).not.toContain("calcChain");
      expect(await partText(result, "[Content_Types].xml")).not.toContain(
        "calcChain",
      );
      expect(await partText(result, "xl/workbook.xml")).toContain(
        '<calcPr fullCalcOnLoad="1"/>',
      );
      expect(await partBytes(result, "xl/media/image1.png")).toEqual(
        await partBytes(source, "xl/media/image1.png"),
      );
      expect(
        cellXml(await partText(result, "xl/worksheets/sheet1.xml"), "C1"),
      ).toContain("<f>1+1</f>");
    } finally {
      warnings.mockRestore();
    }
  });

  it("sets fullCalcOnLoad on an existing calcPr without rewriting the rest", async () => {
    const original = await fixture({
      withCalcChain: true,
      calcPr: '<calcPr calcMode="auto"/>',
    }).generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].cellData![0][2] = {
      v: "5",
      t: CellValueType.STRING,
      s: "3",
    };
    const warnings = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      const workbookXml = await partText(
        await JSZip.loadAsync(
          await patchWorkbookBytes(original, baseline(), live),
        ),
        "xl/workbook.xml",
      );
      expect(workbookXml).toContain(
        '<calcPr calcMode="auto" fullCalcOnLoad="1"/>',
      );
      expect(workbookXml).not.toContain('<calcPr calcMode="auto"/>');
    } finally {
      warnings.mockRestore();
    }
  });

  it("patches column widths only when columnData changes, leaving other layout alone", async () => {
    const original = await fixture().generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].columnData = { 0: { w: 100 } };
    const sheetXml = await partText(
      await JSZip.loadAsync(
        await patchWorkbookBytes(original, baseline(), live),
      ),
      "xl/worksheets/sheet1.xml",
    );
    expect(sheetXml).toMatch(
      /<col min="1" max="1" width="13\.571" customWidth="1"\/>/,
    );
    expect(sheetXml).not.toContain("<mergeCells>");
    expect(cellXml(sheetXml, "A1")).toBe('<c r="A1" s="1" t="s"><v>0</v></c>');
  });

  it("preserves non-width column metadata when a different column is resized", async () => {
    const original = await fixture({
      cols:
        '<cols><col min="1" max="1" width="10" customWidth="1"/>' +
        '<col hidden="1" outlineLevel="1" min="4" max="4" width="13" customWidth="1"/></cols>',
    }).generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].columnData = {
      0: { w: 100 },
      3: { w: colWidthPx(13) },
    };
    const sheetXml = await partText(
      await JSZip.loadAsync(
        await patchWorkbookBytes(original, baseline(), live),
      ),
      "xl/worksheets/sheet1.xml",
    );
    expect(sheetXml).toMatch(
      /<col min="1" max="1" width="13\.571" customWidth="1"\/>/,
    );
    expect(sheetXml).toMatch(
      /<col hidden="1" outlineLevel="1" min="4" max="4" width="13" customWidth="1"\/>/,
    );
  });

  it("throws a named rowCount reason when rowCount changes", async () => {
    const original = await fixture().generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].rowCount = 200;
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      await expect(
        patchWorkbookBytes(original, baseline(), live),
      ).rejects.toThrow(/rowCount/);
    } finally {
      errors.mockRestore();
    }
  });

  it("throws a named sheet-order reason when sheetOrder changes", async () => {
    const original = await fixture().generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheetOrder = ["sheet-2"];
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      await expect(
        patchWorkbookBytes(original, baseline(), live),
      ).rejects.toThrow(/sheet order/);
    } finally {
      errors.mockRestore();
    }
  });

  it("throws a named mergeData reason when mergeData changes", async () => {
    const original = await fixture().generateAsync({ type: "arraybuffer" });
    const live = snapshot();
    live.sheets["sheet-1"].mergeData = [
      { startRow: 2, startColumn: 0, endRow: 1, endColumn: 0 },
    ];
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      await expect(
        patchWorkbookBytes(original, baseline(), live),
      ).rejects.toThrow(/mergeData/);
    } finally {
      errors.mockRestore();
    }
  });

  it("keeps a numeric cell numeric when its display text is edited", async () => {
    const source = fixture();
    source.file(
      "xl/worksheets/sheet1.xml",
      `${DECL}\n<worksheet xmlns="${MAIN}"><sheetData><row r="1"><c r="B1" s="9"><v>1234.5</v></c></row></sheetData></worksheet>`,
    );
    const original = await source.generateAsync({ type: "arraybuffer" });
    const base = baseline();
    const live = baseline();
    base.sheets["sheet-1"].cellData = {
      0: { 1: { v: "1,234.50", t: CellValueType.STRING, s: "9" } },
    };
    live.sheets["sheet-1"].cellData = {
      0: { 1: { v: "2,000.75", t: CellValueType.STRING, s: "9" } },
    };
    const result = await JSZip.loadAsync(
      await patchWorkbookBytes(original, base, live),
    );
    expect(await partText(result, "xl/worksheets/sheet1.xml")).toContain(
      '<c r="B1" s="9"><v>2000.75</v></c>',
    );
  });
});
