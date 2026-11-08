import { describe, it, expect, vi } from "vitest";
import JSZip from "jszip";
import { docxToUniver, univerToDocx, externalImageHosts, imageRequestHeaders } from "./docxData";

describe("docxData round-trip", () => {
  it("imports paragraphs from a minimal docx", async () => {
    const zip = new JSZip();
    zip.file(
      "word/document.xml",
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>Hello</w:t></w:r></w:p>
    <w:p><w:r><w:t>World</w:t></w:r></w:p>
  </w:body>
</w:document>`,
    );
    const bytes = await zip.generateAsync({ type: "uint8array" });
    const data = await docxToUniver(bytes);
    expect(data.body?.dataStream).toContain("Hello");
    expect(data.body?.dataStream).toContain("World");
    expect(data.body?.paragraphs?.length).toBe(2);
  });

  it("throws on docx without document.xml", async () => {
    const zip = new JSZip();
    const bytes = await zip.generateAsync({ type: "uint8array" });
    await expect(docxToUniver(bytes)).rejects.toThrow();
  });

  it("preserves bold/italic/underline runs with offsets", async () => {
    const zip = new JSZip();
    zip.file(
      "word/document.xml",
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r><w:rPr><w:b/></w:rPr><w:t>Bold</w:t></w:r>
      <w:r><w:t> plain </w:t></w:r>
      <w:r><w:rPr><w:i/><w:b/></w:rPr><w:t>BoldItalic</w:t></w:r>
      <w:r><w:rPr><w:u w:val="single"/></w:rPr><w:t>Under</w:t></w:r>
    </w:p>
  </w:body>
</w:document>`,
    );
    const bytes = await zip.generateAsync({ type: "uint8array" });
    const data = await docxToUniver(bytes);
    const stream = data.body?.dataStream ?? "";
    expect(stream).toContain("Bold");
    const tr = data.body?.textRuns ?? [];
    // Bold run at offset 0..3
    const boldRun = tr.find((r: any) => r.ts?.bl === 1 && !r.ts?.it);
    expect(boldRun).toBeDefined();
    expect(boldRun!.st).toBe(0);
    expect(boldRun!.ed).toBe(3);
    // Combined bold+italic run
    const biRun = tr.find((r: any) => r.ts?.bl === 1 && r.ts?.it === 1);
    expect(biRun).toBeDefined();
    const biText = stream.slice(biRun!.st, biRun!.ed + 1);
    expect(biText).toBe("BoldItalic");
    // Underline run
    const uRun = tr.find((r: any) => r.ts?.ul);
    expect(uRun).toBeDefined();
    expect(stream.slice(uRun!.st, uRun!.ed + 1)).toBe("Under");
    // xml:space preserve on styled runs in export
    const out = await univerToDocx(data);
    const z2 = await JSZip.loadAsync(out);
    const xml = await z2.file("word/document.xml")?.async("string");
    expect(xml).toContain("<w:b/>");
    expect(xml).toContain("<w:i/>");
    expect(xml).toContain('<w:u w:val="single"/>');
    expect(xml).toContain('xml:space="preserve"');
  });

  it("XML-escapes special characters in styled runs", async () => {
    const zip = new JSZip();
    zip.file(
      "word/document.xml",
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r><w:rPr><w:b/></w:rPr><w:t>A &amp; B &lt; C</w:t></w:r>
    </w:p>
  </w:body>
</w:document>`,
    );
    const bytes = await zip.generateAsync({ type: "uint8array" });
    const data = await docxToUniver(bytes);
    const out = await univerToDocx(data);
    const z2 = await JSZip.loadAsync(out);
    const xml = await z2.file("word/document.xml")?.async("string");
    expect(xml).toContain("A &amp; B &lt; C");
    expect(xml).toContain("<w:b/>");
  });

  it("preserves paragraph alignment", async () => {
    const zip = new JSZip();
    zip.file(
      "word/document.xml",
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>Centered</w:t></w:r></w:p>
    <w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:r><w:t>Right</w:t></w:r></w:p>
    <w:p><w:r><w:t>Left</w:t></w:r></w:p>
  </w:body>
</w:document>`,
    );
    const bytes = await zip.generateAsync({ type: "uint8array" });
    const data = await docxToUniver(bytes);
    const paras = data.body?.paragraphs ?? [];
    expect(paras[0].paragraphStyle?.horizontalAlign).toBe(2);
    expect(paras[1].paragraphStyle?.horizontalAlign).toBe(3);
    expect(paras[2].paragraphStyle).toBeUndefined();
    const out = await univerToDocx(data);
    const z2 = await JSZip.loadAsync(out);
    const xml = await z2.file("word/document.xml")?.async("string");
    expect(xml).toContain('<w:jc w:val="center"/>');
    expect(xml).toContain('<w:jc w:val="right"/>');
  });

  it("maps start/end/distribute alignment values", async () => {
    const zip = new JSZip();
    zip.file(
      "word/document.xml",
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:jc w:val="start"/></w:pPr><w:r><w:t>S</w:t></w:r></w:p>
    <w:p><w:pPr><w:jc w:val="end"/></w:pPr><w:r><w:t>E</w:t></w:r></w:p>
    <w:p><w:pPr><w:jc w:val="distribute"/></w:pPr><w:r><w:t>D</w:t></w:r></w:p>
  </w:body>
</w:document>`,
    );
    const bytes = await zip.generateAsync({ type: "uint8array" });
    const data = await docxToUniver(bytes);
    const paras = data.body?.paragraphs ?? [];
    expect(paras[0].paragraphStyle?.horizontalAlign).toBe(1);
    expect(paras[1].paragraphStyle?.horizontalAlign).toBe(3);
    expect(paras[2].paragraphStyle?.horizontalAlign).toBe(6);
    const out = await univerToDocx(data);
    const z2 = await JSZip.loadAsync(out);
    const xml = await z2.file("word/document.xml")?.async("string");
    expect(xml).toContain('<w:jc w:val="left"/>');
    expect(xml).toContain('<w:jc w:val="right"/>');
    expect(xml).toContain('<w:jc w:val="distribute"/>');
  });

  it("exports and re-imports paragraphs", async () => {
    const data = {
      id: "d1",
      documentStyle: {},
      body: { dataStream: "Foo\r\nBar\r\n", paragraphs: [{ startIndex: 3, paragraphId: "p0" }, { startIndex: 8, paragraphId: "p1" }], textRuns: [] },
    } as any;
    const bytes = await univerToDocx(data);
    const zip = await JSZip.loadAsync(bytes);
    const xml = await zip.file("word/document.xml")?.async("string");
    expect(xml).toContain("Foo");
    expect(xml).toContain("Bar");
    const data2 = await docxToUniver(bytes);
    expect(data2.body?.dataStream).toContain("Foo");
    expect(data2.body?.dataStream).toContain("Bar");
  });

  it("imports colour, size, font, strike, highlight and vertAlign runs", async () => {
    const zip = new JSZip();
    zip.file(
      "word/document.xml",
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:strike/><w:color w:val="FF0000"/><w:sz w:val="21"/><w:highlight w:val="yellow"/><w:vertAlign w:val="superscript"/></w:rPr><w:t>Rich</w:t></w:r>
      <w:r><w:rPr><w:b w:val="0"/><w:i w:val="false"/><w:u w:val="none"/></w:rPr><w:t>Plain</w:t></w:r>
      <w:r><w:rPr><w:shd w:val="clear" w:color="auto" w:fill="00FF00"/></w:rPr><w:t>Shaded</w:t></w:r>
    </w:p>
  </w:body>
</w:document>`,
    );
    const bytes = await zip.generateAsync({ type: "uint8array" });
    const data = await docxToUniver(bytes);
    const stream = data.body?.dataStream ?? "";
    const runs = data.body?.textRuns ?? [];

    const rich: any = runs.find((r: any) => stream.slice(r.st, r.ed + 1) === "Rich");
    expect(rich).toBeDefined();
    expect(rich.ts.cl.rgb).toBe("#FF0000");
    expect(rich.ts.fs).toBeCloseTo(10.5);
    expect(rich.ts.ff).toBe("Arial");
    expect(rich.ts.st.s).toBe(1);
    expect(rich.ts.bg.rgb).toBe("#FFFF00");
    expect(rich.ts.va).toBe(3);

    const plain: any = runs.find((r: any) => stream.slice(r.st, r.ed + 1) === "Plain");
    expect(plain).toBeUndefined();

    const shaded: any = runs.find((r: any) => stream.slice(r.st, r.ed + 1) === "Shaded");
    expect(shaded).toBeDefined();
    expect(shaded.ts.bg.rgb).toBe("#00FF00");
  });

  it("exports body paragraphs whose startIndex sits on the paragraph terminator", async () => {
    // Univer convention: each paragraph's startIndex is the index of its "\r".
    // "First" is 0-4 and its \r is 5; "Second" is 7-12 and its \r is 13.
    const data = {
      id: "d1",
      documentStyle: {},
      body: {
        dataStream: "First\r\nSecond\r\n",
        paragraphs: [
          { startIndex: 5, paragraphId: "p0" },
          { startIndex: 13, paragraphId: "p1" },
        ],
        textRuns: [],
      },
    } as any;
    const out = await univerToDocx(data);
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain(">First<");
    expect(xml).toContain(">Second<");
    expect(xml).not.toContain("<w:p/>");
    expect(xml.indexOf("First")).toBeLessThan(xml.indexOf("Second"));
  });

  it("anchors every imported body paragraph at its \\r terminator", async () => {
    const bytes = await buildDocx(
      '<w:p><w:r><w:t>Alpha</w:t></w:r></w:p><w:p><w:r><w:t>Beta</w:t></w:r></w:p>',
    );
    const data: any = await docxToUniver(bytes);
    const stream: string = data.body.dataStream;
    expect(data.body.paragraphs).toHaveLength(2);
    for (const p of data.body.paragraphs) expect(stream[p.startIndex]).toBe("\r");
    // Text ends right before each terminator.
    expect(stream.slice(0, data.body.paragraphs[0].startIndex)).toBe("Alpha");
  });

  it("exports rich run properties and re-imports them", async () => {
    const data = {
      id: "d1",
      documentStyle: {},
      body: {
        dataStream: "Red\r\n",
        paragraphs: [{ startIndex: 3, paragraphId: "p0" }],
        textRuns: [
          {
            st: 0,
            ed: 2,
            ts: {
              bl: 1,
              it: 1,
              cl: { rgb: "rgb(255, 0, 0)" },
              fs: 10.5,
              ff: "Times New Roman",
              st: { s: 1 },
              bg: { rgb: "#00FF00" },
              va: 2,
            },
          },
        ],
      },
    } as any;
    const bytes = await univerToDocx(data);
    const zip = await JSZip.loadAsync(bytes);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain('<w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman"/>');
    expect(xml).toContain("<w:b/>");
    expect(xml).toContain("<w:i/>");
    expect(xml).toContain("<w:strike/>");
    expect(xml).toContain('<w:color w:val="FF0000"/>');
    expect(xml).toContain('<w:sz w:val="21"/>');
    expect(xml).toContain('<w:shd w:val="clear" w:color="auto" w:fill="00FF00"/>');
    expect(xml).toContain('<w:vertAlign w:val="subscript"/>');

    const data2 = await docxToUniver(bytes);
    const stream = data2.body?.dataStream ?? "";
    const run: any = (data2.body?.textRuns ?? []).find((r: any) => stream.slice(r.st, r.ed + 1) === "Red");
    expect(run).toBeDefined();
    expect(run.ts.bl).toBe(1);
    expect(run.ts.it).toBe(1);
    expect(run.ts.cl.rgb).toBe("#FF0000");
    expect(run.ts.fs).toBeCloseTo(10.5);
    expect(run.ts.ff).toBe("Times New Roman");
    expect(run.ts.st.s).toBe(1);
    expect(run.ts.bg.rgb).toBe("#00FF00");
    expect(run.ts.va).toBe(2);
  });

  it("does not export disabled decorations", async () => {
    const data = {
      id: "d1",
      documentStyle: {},
      body: {
        dataStream: "X\r\n",
        paragraphs: [{ startIndex: 1, paragraphId: "p0" }],
        textRuns: [{ st: 0, ed: 0, ts: { ul: { s: 0 }, st: { s: 0 } } }],
      },
    } as any;
    const bytes = await univerToDocx(data);
    const zip = await JSZip.loadAsync(bytes);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).not.toContain("<w:u ");
    expect(xml).not.toContain("<w:strike/>");
  });

  const W_DOC_OPEN =
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n' +
    '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ' +
    'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ' +
    'xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ' +
    'xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">';

  async function buildDocx(
    bodyXml: string,
    extra: Record<string, string | Uint8Array> = {},
  ): Promise<Uint8Array> {
    const zip = new JSZip();
    zip.file("word/document.xml", `${W_DOC_OPEN}<w:body>${bodyXml}</w:body></w:document>`);
    for (const [name, content] of Object.entries(extra)) zip.file(name, content);
    return zip.generateAsync({ type: "uint8array" });
  }

  function assertWellFormed(xml: string) {
    const parsed = new DOMParser().parseFromString(xml, "application/xml");
    expect(parsed.getElementsByTagName("parsererror")).toHaveLength(0);
  }

  it("imports a simple table as a native Univer table and round-trips it", async () => {
    const table =
      '<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="5000" w:type="pct"/></w:tblPr>' +
      '<w:tblGrid><w:gridCol w:w="1200"/><w:gridCol w:w="800"/></w:tblGrid>' +
      '<w:tr><w:tc><w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="EEEEEE"/></w:tcPr><w:p><w:r><w:t>Cell A</w:t></w:r></w:p></w:tc>' +
      '<w:tc><w:p><w:r><w:t>Cell B</w:t></w:r></w:p></w:tc></w:tr>' +
      '<w:tr><w:tc><w:p><w:r><w:t>Cell C</w:t></w:r></w:p></w:tc><w:tc><w:p/></w:tc></w:tr></w:tbl>';
    const bytes = await buildDocx(
      `<w:p><w:r><w:t>Before</w:t></w:r></w:p>${table}<w:p><w:r><w:t>After</w:t></w:r></w:p>`,
      { "word/styles.xml": "<w:styles/>" },
    );
    const data: any = await docxToUniver(bytes);
    const stream = data.body?.dataStream ?? "";
    expect(stream).toContain("Before");
    expect(stream).toContain("After");
    // Native model: cell text lives in the stream, wrapped in sentinels.
    expect(stream).toContain("Cell A");
    expect(stream).toContain("\x1a");
    expect(data.body.tables).toHaveLength(1);
    expect(data.body.docxRawBlocks ?? []).toHaveLength(0);
    const tableId = data.body.tables[0].tableId;
    expect(data.tableSource[tableId].tableRows).toHaveLength(2);
    expect(data.tableSource[tableId].tableColumns).toHaveLength(2);
    // Every cell has a paragraph entry plus the table's enclosing paragraph.
    expect(data.body.paragraphs.length).toBe(2 + 4 + 1);

    const out = await univerToDocx(data, { originalBytes: bytes, rawBlocks: [] });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain("<w:tbl");
    expect(xml).toContain("<w:gridCol");
    // Original table/cell properties survive the native round-trip.
    expect(xml).toContain('w:tblStyle w:val="TableGrid"');
    expect(xml).toContain('w:fill="EEEEEE"');
    expect(xml).not.toContain("<w:tblPr/>");
    expect(xml.match(/Cell A/g)).toHaveLength(1);
    expect(xml.match(/<w:tr>/g)).toHaveLength(2);
    expect(xml.match(/<w:tc>/g)).toHaveLength(4);
    expect(xml.indexOf("Before")).toBeLessThan(xml.indexOf("<w:tbl"));
    expect(xml.indexOf("<w:tbl")).toBeLessThan(xml.indexOf("After"));
    assertWellFormed(xml);
    expect(zip.file("word/styles.xml")).not.toBeNull();
  });

  it("falls back to verbatim preservation for a merged-cell table", async () => {
    const table =
      '<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/></w:tblGrid>' +
      '<w:tr><w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p><w:r><w:t>Merged</w:t></w:r></w:p></w:tc></w:tr></w:tbl>';
    const bytes = await buildDocx(`<w:p><w:r><w:t>Before</w:t></w:r></w:p>${table}`, {
      "word/styles.xml": "<w:styles/>",
    });
    const data: any = await docxToUniver(bytes);
    expect(data.body.tables ?? []).toHaveLength(0);
    expect(data.body.dataStream).not.toContain("Merged");
    expect(data.body.dataStream).toContain("[Table preserved]");
    const raw = data.body.docxRawBlocks ?? [];
    expect(raw).toHaveLength(1);
    expect(raw[0].xml).toContain("w:gridSpan");

    const out = await univerToDocx(data, { originalBytes: bytes, rawBlocks: raw });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain("w:gridSpan");
    expect(xml).not.toContain("[Table preserved]");
    assertWellFormed(xml);
  });

  it("falls back to verbatim preservation when a table cell holds a drawing", async () => {
    const table =
      '<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/></w:tblGrid>' +
      '<w:tr><w:tc><w:p><w:r><w:t>Cell A</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Cell B</w:t></w:r></w:p></w:tc></w:tr>' +
      '<w:tr><w:tc><w:p><w:r><w:drawing><wp:inline><wp:extent cx="952500" cy="476250"/><a:graphic><a:graphicData>' +
      '<a:blip r:embed="rId5"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p></w:tc>' +
      '<w:tc><w:p><w:r><w:t>Cell D</w:t></w:r></w:p></w:tc></w:tr></w:tbl>';
    const bytes = await buildDocx(`<w:p><w:r><w:t>Before</w:t></w:r></w:p>${table}<w:p><w:r><w:t>After</w:t></w:r></w:p>`);
    const data: any = await docxToUniver(bytes);
    // A picture cannot be modelled as cell text, so the whole table stays verbatim.
    expect(data.body.tables ?? []).toHaveLength(0);
    expect(data.body.dataStream).toContain("[Table preserved]");
    const raw = data.body.docxRawBlocks ?? [];
    expect(raw).toHaveLength(1);
    expect(raw[0].xml).toContain('r:embed="rId5"');

    const out = await univerToDocx(data);
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    // The captured table XML is written back exactly, drawing and all.
    expect(xml).toContain(raw[0].xml);
    expect(xml).toContain('r:embed="rId5"');
    expect(xml).not.toContain("[Table preserved]");
    expect(xml.match(/Cell A/g)).toHaveLength(1);
    assertWellFormed(xml);
  });

  it("imports an inline image as a native drawing and round-trips it", async () => {
    const drawing =
      '<w:p><w:r><w:drawing><wp:inline><wp:extent cx="952500" cy="476250"/><a:graphic><a:graphicData>' +
      '<a:blip r:embed="rId5"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>';
    const png = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 1, 2, 3]);
    const rels =
      '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>' +
      '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">' +
      '<Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>' +
      "</Relationships>";
    const bytes = await buildDocx(`<w:p><w:r><w:t>Caption</w:t></w:r></w:p>${drawing}`, {
      "word/_rels/document.xml.rels": rels,
      "word/media/image1.png": png,
    });
    const data: any = await docxToUniver(bytes);
    expect(data.body?.dataStream).toContain("Caption");
    // Inline drawing becomes a native Univer drawing, not a raw block.
    expect(data.body?.docxRawBlocks ?? []).toHaveLength(0);
    expect(data.body?.dataStream).toContain("\b");
    expect(data.body?.customBlocks).toHaveLength(1);
    expect(data.drawingsOrder).toHaveLength(1);
    const drawingId = data.drawingsOrder[0];
    expect(data.drawings[drawingId].drawingType).toBe(0);
    expect(data.drawings[drawingId].layoutType).toBe(0);
    expect(data.drawings[drawingId].imageSourceType).toBe("BASE64");
    expect(data.drawings[drawingId].source.startsWith("data:image/png;base64,")).toBe(true);
    expect(data.__imageMeta[drawingId].xml).toContain('r:embed="rId5"');
    expect(data.drawings[drawingId].transform.width).toBe(100);
    expect(data.drawings[drawingId].transform.height).toBe(50);
    expect(data.__imageMeta[drawingId].width).toBe(100);

    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain('r:embed="rId5"');
    const media = await zip.file("word/media/image1.png")?.async("uint8array");
    expect(Array.from(media ?? [])).toEqual(Array.from(png));
    const outRels = (await zip.file("word/_rels/document.xml.rels")?.async("string")) ?? "";
    expect(outRels).toContain("media/image1.png");
    assertWellFormed(xml);
  });

  it("writes a resized native image extent back to the drawing", async () => {
    const drawing =
      '<w:p><w:r><w:drawing><wp:inline><wp:extent cx="952500" cy="476250"/><a:graphic><a:graphicData>' +
      '<a:blip r:embed="rId5"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>';
    const bytes = await buildDocx(drawing);
    const data: any = await docxToUniver(bytes);
    const id = data.drawingsOrder[0];
    data.drawings[id].transform = { width: 200, height: 100, angle: 0 };
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain('cx="1905000"');
    expect(xml).toContain('cy="952500"');
    assertWellFormed(xml);
  });

  it("writes an image pasted in the editor into the package", async () => {
    const png = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 9, 9, 7, 5]);
    const b64 = btoa(String.fromCharCode(...png));
    const data: any = {
      id: "d1",
      documentStyle: {},
      body: {
        dataStream: "\b\r\n",
        paragraphs: [{ startIndex: 1, paragraphId: "p0" }],
        textRuns: [],
        customBlocks: [{ startIndex: 0, blockId: "imgPasted" }],
      },
      drawings: {
        imgPasted: {
          drawingId: "imgPasted",
          transform: { width: 20, height: 10, angle: 0 },
          imageSourceType: "BASE64",
          source: `data:image/png;base64,${b64}`,
        },
      },
      drawingsOrder: ["imgPasted"],
    };
    const out = await univerToDocx(data);
    const zip = await JSZip.loadAsync(out);
    const mediaFiles = Object.keys(zip.files).filter((n) => n.startsWith("word/media/") && !n.endsWith("/"));
    expect(mediaFiles).toHaveLength(1);
    const media = await zip.file(mediaFiles[0])!.async("uint8array");
    expect(Array.from(media)).toEqual(Array.from(png));

    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain("<w:drawing");
    assertWellFormed(xml);
    const rId = /r:embed="(rId\d+)"/.exec(xml)?.[1];
    expect(rId).toBeTruthy();
    const rels = (await zip.file("word/_rels/document.xml.rels")?.async("string")) ?? "";
    expect(rels).toContain(`Id="${rId}"`);
    expect(rels).toContain(mediaFiles[0].replace("word/", ""));
    const ct = (await zip.file("[Content_Types].xml")?.async("string")) ?? "";
    expect(ct).toMatch(/Extension="png"/i);
  });

  it("adds pasted-image media to an existing package without dropping its parts", async () => {
    const png = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 4, 2]);
    const b64 = btoa(String.fromCharCode(...png));
    const base = await buildDocx('<w:p><w:r><w:t>Text</w:t></w:r></w:p>', {
      "word/styles.xml": "<w:styles/>",
    });
    const data: any = {
      id: "d1",
      documentStyle: {},
      body: {
        dataStream: "\b\r\n",
        paragraphs: [{ startIndex: 1, paragraphId: "p0" }],
        textRuns: [],
        customBlocks: [{ startIndex: 0, blockId: "imgPasted" }],
      },
      drawings: {
        imgPasted: {
          drawingId: "imgPasted",
          transform: { width: 30, height: 15, angle: 0 },
          imageSourceType: "BASE64",
          source: `data:image/png;base64,${b64}`,
        },
      },
      drawingsOrder: ["imgPasted"],
    };
    const out = await univerToDocx(data, { originalBytes: base });
    const zip = await JSZip.loadAsync(out);
    expect(zip.file("word/styles.xml")).not.toBeNull();
    const mediaFiles = Object.keys(zip.files).filter((n) => n.startsWith("word/media/") && !n.endsWith("/"));
    expect(mediaFiles).toHaveLength(1);
    const media = await zip.file(mediaFiles[0])!.async("uint8array");
    expect(Array.from(media)).toEqual(Array.from(png));
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    const rId = /r:embed="(rId\d+)"/.exec(xml)?.[1];
    expect(rId).toBeTruthy();
    const rels = (await zip.file("word/_rels/document.xml.rels")?.async("string")) ?? "";
    expect(rels).toContain(`Id="${rId}"`);
  });

  it("keeps an anchored image as a preserved block", async () => {
    const drawing =
      '<w:p><w:r><w:drawing><wp:anchor><a:graphic><a:graphicData>' +
      '<a:blip r:embed="rId6"/></a:graphicData></a:graphic></wp:anchor></w:drawing></w:r></w:p>';
    const bytes = await buildDocx(`<w:p><w:r><w:t>Text</w:t></w:r></w:p>${drawing}`);
    const data: any = await docxToUniver(bytes);
    expect(data.drawingsOrder ?? []).toHaveLength(0);
    expect(data.body.docxRawBlocks).toHaveLength(1);
    expect(data.body.docxRawBlocks[0].xml).toContain("wp:anchor");
    expect(data.body.dataStream).toContain("[Image preserved]");

    const out = await univerToDocx(data, { originalBytes: bytes, rawBlocks: data.body.docxRawBlocks });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain("wp:anchor");
    expect(xml).not.toContain("[Image preserved]");
    assertWellFormed(xml);
  });

  it("orders text, table and image blocks in a mixed document", async () => {
    const table =
      '<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid>' +
      '<w:tr><w:tc><w:p><w:r><w:t>T1</w:t></w:r></w:p></w:tc></w:tr></w:tbl>';
    const drawing =
      '<w:p><w:r><w:drawing><wp:inline><a:graphic><a:graphicData>' +
      '<a:blip r:embed="rId9"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>';
    const bytes = await buildDocx(
      `<w:p><w:r><w:t>One</w:t></w:r></w:p>${table}<w:p><w:r><w:t>Two</w:t></w:r></w:p>${drawing}<w:p><w:r><w:t>Three</w:t></w:r></w:p>`,
    );
    const data: any = await docxToUniver(bytes);
    // Both the table and the image are native; no raw blocks remain.
    expect(data.body?.docxRawBlocks ?? []).toHaveLength(0);
    expect(data.body?.tables ?? []).toHaveLength(1);
    expect(data.drawingsOrder ?? []).toHaveLength(1);
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    // Every paragraph, including cell, enclosing, and image paragraphs, is anchored at its terminator.
    const stream: string = data.body.dataStream;
    for (const p of data.body.paragraphs) expect(stream[p.startIndex]).toBe("\r");
    const order = ["One", "<w:tbl", "Two", "w:drawing", "Three"].map((s) => xml.indexOf(s));
    expect(order.every((i) => i >= 0)).toBe(true);
    expect(order).toEqual([...order].sort((a, b) => a - b));
  });

  it("keeps a native table in place after text is inserted before it", async () => {
    const table =
      '<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid>' +
      '<w:tr><w:tc><w:p><w:r><w:t>Cell A</w:t></w:r></w:p></w:tc></w:tr></w:tbl>';
    const bytes = await buildDocx(
      `<w:p><w:r><w:t>Before</w:t></w:r></w:p>${table}<w:p><w:r><w:t>After</w:t></w:r></w:p>`,
    );
    const data = await docxToUniver(bytes);
    const body: any = data.body;
    // Simulate typing a new paragraph ahead of the table in the editor.
    const inserted = "New paragraph\r\n";
    body.dataStream = inserted + body.dataStream;
    body.paragraphs = [
      { startIndex: inserted.indexOf("\r"), paragraphId: "n0" },
      ...body.paragraphs.map((p: any) => ({
        ...p,
        paragraphId: `s${p.paragraphId}`,
        startIndex: p.startIndex + inserted.length,
      })),
    ];
    // The editor shifts table ranges along with the text they follow.
    body.tables = (body.tables ?? []).map((t: any) => ({
      ...t,
      startIndex: t.startIndex + inserted.length,
      endIndex: t.endIndex + inserted.length,
    }));
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain("New paragraph");
    expect(xml.indexOf("New paragraph")).toBeLessThan(xml.indexOf("<w:tbl"));
    expect(xml.indexOf("Before")).toBeLessThan(xml.indexOf("<w:tbl"));
    expect(xml.indexOf("<w:tbl")).toBeLessThan(xml.indexOf("After"));
    expect(xml.match(/Cell A/g)).toHaveLength(1);
    expect(xml).not.toContain("[Table preserved]");
  });

  it("reuses the original sectPr and keeps header references", async () => {
    const sectPr =
      '<w:sectPr><w:headerReference w:type="default" r:id="rId7"/>' +
      '<w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="720" w:right="720" w:bottom="720" w:left="720"/></w:sectPr>';
    const rels =
      '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>' +
      '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">' +
      '<Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>' +
      "</Relationships>";
    const bytes = await buildDocx(`<w:p><w:r><w:t>Body</w:t></w:r></w:p>${sectPr}`, {
      "word/header1.xml": "<w:hdr/>",
      "word/_rels/document.xml.rels": rels,
    });
    const data = await docxToUniver(bytes);
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain('r:id="rId7"');
    expect(xml).toContain('w:top="720"');
    expect(zip.file("word/header1.xml")).not.toBeNull();
  });

  it("reuses the last body-level sectPr in a multi-section document", async () => {
    // A paragraph-level sectPr (section break) appears before the body's final sectPr.
    const sectionBreak =
      '<w:p><w:pPr><w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="360" w:right="360" w:bottom="360" w:left="360"/></w:sectPr></w:pPr>' +
      '<w:r><w:t>End of section one</w:t></w:r></w:p>';
    const finalSectPr =
      '<w:sectPr><w:headerReference w:type="default" r:id="rId7"/>' +
      '<w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>';
    const rels =
      '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>' +
      '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">' +
      '<Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>' +
      "</Relationships>";
    const bytes = await buildDocx(`${sectionBreak}<w:p><w:r><w:t>Section two</w:t></w:r></w:p>${finalSectPr}`, {
      "word/header1.xml": "<w:hdr/>",
      "word/_rels/document.xml.rels": rels,
    });
    const data = await docxToUniver(bytes);
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    // The section break stays on its own paragraph; the body's final sectPr comes last.
    expect(xml.match(/<w:sectPr\b/g)).toHaveLength(2);
    expect(paragraphAt(xml, xml.indexOf("End of section one"))).toContain('w:top="360"');
    const bodyEnd = xml.slice(xml.lastIndexOf("<w:sectPr"));
    expect(bodyEnd.indexOf("Section two")).toBe(-1);
    expect(xml.lastIndexOf("<w:sectPr")).toBeGreaterThan(xml.indexOf("Section two"));
    expect(bodyEnd).toContain('r:id="rId7"');
    expect(bodyEnd).toContain('w:top="1440"');
    expect(zip.file("word/header1.xml")).not.toBeNull();
  });

  /** The whole `w:p` element that contains index `idx` of the serialized XML. */
  function paragraphAt(xml: string, idx: number): string {
    const starts = [...xml.slice(0, idx).matchAll(/<w:p(?:\s[^>]*)?>/g)];
    const start = starts[starts.length - 1]?.index ?? -1;
    expect(start).toBeGreaterThanOrEqual(0);
    return xml.slice(start, xml.indexOf("</w:p>", idx) + "</w:p>".length);
  }

  // The mid-document break carries its own header reference (rId8) so the test can check
  // that a reference inside a preserved sectPr keeps its r: prefix on export.
  const landscapeBreak =
    '<w:sectPr><w:headerReference w:type="default" r:id="rId8"/>' +
    '<w:pgSz w:w="15840" w:h="12240" w:orient="landscape"/></w:sectPr>';
  const portraitFinal =
    '<w:sectPr><w:headerReference w:type="default" r:id="rId7"/>' +
    '<w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>';
  const sectionRels =
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>' +
    '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">' +
    '<Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>' +
    '<Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header2.xml"/>' +
    "</Relationships>";
  const landscapeMiddleDocx = () =>
    buildDocx(
      '<w:p><w:r><w:t>Paragraph A</w:t></w:r></w:p>' +
        `<w:p><w:pPr>${landscapeBreak}</w:pPr><w:r><w:t>Paragraph B</w:t></w:r></w:p>` +
        '<w:p><w:r><w:t>Paragraph C</w:t></w:r></w:p>' +
        portraitFinal,
      { "word/header1.xml": "<w:hdr/>", "word/header2.xml": "<w:hdr/>", "word/_rels/document.xml.rels": sectionRels },
    );

  it("keeps a mid-document section break on its paragraph and the body's final sectPr", async () => {
    const bytes = await landscapeMiddleDocx();
    const data = await docxToUniver(bytes);
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    assertWellFormed(xml);
    expect(xml).not.toContain("ns1:");
    // Paragraph B keeps its text and carries the landscape section break in its own pPr.
    const paraB = paragraphAt(xml, xml.indexOf("Paragraph B"));
    expect(paraB).toContain("Paragraph B");
    expect(paraB).toMatch(/<w:pPr>(?:(?!<\/w:pPr>).)*<w:sectPr\b(?:(?!<\/w:pPr>).)*w:orient="landscape"/s);
    // The break's header reference keeps its r: prefix and its header part survives the save.
    expect(paraB).toContain('r:id="rId8"');
    expect(zip.file("word/header2.xml")).not.toBeNull();
    // Exactly one landscape break, and two sectPrs in total: the break, then the body's final one.
    expect(xml.match(/orient="landscape"/g)).toHaveLength(1);
    expect(xml.match(/<w:sectPr\b/g)).toHaveLength(2);
    const bodyEnd = xml.slice(xml.lastIndexOf("<w:sectPr"));
    expect(bodyEnd).toContain('r:id="rId7"');
    expect(bodyEnd).toContain('w:top="1440"');
    expect(bodyEnd).not.toContain("landscape");
    expect(xml.indexOf("Paragraph C")).toBeLessThan(xml.lastIndexOf("<w:sectPr"));
    expect(xml).not.toContain("[Section break preserved]");
  });

  it("merges a section break into an empty paragraph as its only pPr", async () => {
    // Word often puts a section break on an empty paragraph, which renders as <w:p/>.
    const bytes = await buildDocx(
      '<w:p><w:r><w:t>Paragraph A</w:t></w:r></w:p>' +
        `<w:p><w:pPr>${landscapeBreak}</w:pPr></w:p>` +
        '<w:p><w:r><w:t>Paragraph C</w:t></w:r></w:p>' +
        portraitFinal,
      { "word/header1.xml": "<w:hdr/>", "word/header2.xml": "<w:hdr/>", "word/_rels/document.xml.rels": sectionRels },
    );
    const data = await docxToUniver(bytes);
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    assertWellFormed(xml);
    expect(xml.match(/orient="landscape"/g)).toHaveLength(1);
    const emptyPara = paragraphAt(xml, xml.indexOf('orient="landscape"'));
    expect(emptyPara).toMatch(/^<w:p(?:\s[^>]*)?><w:pPr>(?:(?!<\/w:pPr>).)*<w:sectPr\b/s);
    expect(emptyPara).not.toContain("Paragraph");
    expect(xml.indexOf("Paragraph A")).toBeLessThan(xml.indexOf('orient="landscape"'));
    expect(xml.indexOf('orient="landscape"')).toBeLessThan(xml.indexOf("Paragraph C"));
  });

  it("merges a section break into an existing pPr alongside its alignment", async () => {
    const bytes = await buildDocx(
      '<w:p><w:r><w:t>Paragraph A</w:t></w:r></w:p>' +
        `<w:p><w:pPr><w:jc w:val="center"/>${landscapeBreak}</w:pPr><w:r><w:t>Paragraph B</w:t></w:r></w:p>` +
        portraitFinal,
      { "word/header1.xml": "<w:hdr/>", "word/header2.xml": "<w:hdr/>", "word/_rels/document.xml.rels": sectionRels },
    );
    const data = await docxToUniver(bytes);
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    assertWellFormed(xml);
    const paraB = paragraphAt(xml, xml.indexOf("Paragraph B"));
    // One pPr holds both the centring and the section break; the break is not a second pPr.
    expect(paraB.match(/<w:pPr\b/g)).toHaveLength(1);
    expect(paraB).toMatch(/<w:pPr>(?:(?!<\/w:pPr>).)*<w:jc w:val="center"\/>(?:(?!<\/w:pPr>).)*<w:sectPr\b/s);
    expect(paraB).toContain('w:orient="landscape"');
  });

  it("imports the section break as a marker paragraph right after its paragraph", async () => {
    const data: any = await docxToUniver(await landscapeMiddleDocx());
    const stream: string = data.body.dataStream;
    expect(stream).toContain("Paragraph B\r\n[Section break preserved]\r\n");
    const block = (data.body.docxRawBlocks ?? []).find((b: { kind?: string }) => b.kind === "section");
    expect(block).toBeDefined();
    expect(block.xml).toMatch(/^<w:sectPr\b/);
    expect(block.xml).toContain('w:orient="landscape"');
    // Every paragraph, the marker included, is anchored at its terminator.
    for (const p of data.body.paragraphs) expect(stream[p.startIndex]).toBe("\r");
  });

  it("still emits a section break when its marker paragraph was edited away", async () => {
    const bytes = await landscapeMiddleDocx();
    const data: any = await docxToUniver(bytes);
    const body = data.body;
    const marker = "[Section break preserved]";
    const at = body.dataStream.indexOf(marker);
    expect(at).toBeGreaterThan(-1);
    // Overwrite the marker text in place so no anchor moves.
    body.dataStream = body.dataStream.slice(0, at) + " ".repeat(marker.length) + body.dataStream.slice(at + marker.length);
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    assertWellFormed(xml);
    expect(xml).not.toContain(marker);
    // The break is not dropped: it appears exactly once, after the body text and before the final sectPr.
    expect(xml.match(/orient="landscape"/g)).toHaveLength(1);
    expect(xml.indexOf('orient="landscape"')).toBeGreaterThan(xml.indexOf("Paragraph C"));
    expect(xml.indexOf('orient="landscape"')).toBeLessThan(xml.lastIndexOf("<w:sectPr"));
    expect(xml.lastIndexOf("<w:sectPr")).toBeGreaterThan(xml.indexOf('orient="landscape"'));
    expect(xml).toContain('r:id="rId7"');
  });

  it("keeps a section break on an image-only paragraph verbatim", async () => {
    const drawingPara =
      `<w:p><w:pPr>${landscapeBreak}</w:pPr><w:r><w:drawing><wp:inline><a:graphic><a:graphicData>` +
      '<a:blip r:embed="rId9"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>';
    const bytes = await buildDocx(`<w:p><w:r><w:t>Before</w:t></w:r></w:p>${drawingPara}${portraitFinal}`, {
      "word/_rels/document.xml.rels": sectionRels,
    });
    const data: any = await docxToUniver(bytes);
    // Not converted to a native image, so the break is not lost with the paragraph.
    expect(data.drawingsOrder ?? []).toHaveLength(0);
    const out = await univerToDocx(data, { originalBytes: bytes });
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml.match(/orient="landscape"/g)).toHaveLength(1);
    expect(paragraphAt(xml, xml.indexOf("<w:drawing"))).toContain('w:orient="landscape"');
  });

  it("emits a plain document with no raw blocks", async () => {
    const bytes = await buildDocx('<w:p><w:r><w:t>Plain</w:t></w:r></w:p>');
    const data = await docxToUniver(bytes);
    expect(data.body?.docxRawBlocks ?? []).toHaveLength(0);
    const out = await univerToDocx(data);
    const zip = await JSZip.loadAsync(out);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml).toContain("Plain");
    expect(xml).not.toContain("<w:tbl");
  });
});

describe("docxData image sources", () => {
  const png = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 1, 2, 3, 4]);
  const pngDataUrl = `data:image/png;base64,${btoa(String.fromCharCode(...png))}`;

  function imageDoc(source: string, imageSourceType: string, count = 1): any {
    const drawings: Record<string, any> = {};
    const customBlocks: { startIndex: number; blockId: string }[] = [];
    const drawingsOrder: string[] = [];
    let stream = "";
    for (let i = 0; i < count; i++) {
      drawings[`img${i}`] = {
        drawingId: `img${i}`,
        transform: { width: 20, height: 10, angle: 0 },
        imageSourceType,
        source,
      };
      customBlocks.push({ startIndex: stream.length, blockId: `img${i}` });
      drawingsOrder.push(`img${i}`);
      stream += "\b\r\n";
    }
    return {
      id: "d1",
      documentStyle: {},
      body: {
        dataStream: stream,
        // Each block's paragraph is anchored at the "\r" that follows its sentinel.
        paragraphs: customBlocks.map((cb) => ({ startIndex: cb.startIndex + 1, paragraphId: `p${cb.startIndex}` })),
        textRuns: [],
        customBlocks,
      },
      drawings,
      drawingsOrder,
    };
  }

  async function mediaNames(bytes: Uint8Array): Promise<string[]> {
    const zip = await JSZip.loadAsync(bytes);
    return Object.keys(zip.files).filter((n) => n.startsWith("word/media/") && !n.endsWith("/"));
  }

  it("fetches an http(s) image source and embeds it without credentials", async () => {
    const fetchSpy = vi.fn(async () => new Response(png, { status: 200 }));
    vi.stubGlobal("fetch", fetchSpy);
    try {
      const out = await univerToDocx(imageDoc("https://example.com/logo.png", "URL"));
      const zip = await JSZip.loadAsync(out);
      const media = Object.keys(zip.files).filter((n) => n.startsWith("word/media/") && !n.endsWith("/"));
      expect(media).toHaveLength(1);
      expect(Array.from(await zip.file(media[0])!.async("uint8array"))).toEqual(Array.from(png));
      expect(fetchSpy).toHaveBeenCalledTimes(1);
      const call = fetchSpy.mock.calls[0] as unknown as [string, RequestInit];
      expect(call[0]).toBe("https://example.com/logo.png");
      expect(call[1].credentials).toBe("omit");
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("resolves a UUID image through the supplied resolver", async () => {
    const resolveImage = vi.fn(async () => pngDataUrl);
    const out = await univerToDocx(imageDoc("hosted-uuid-1", "UUID"), { resolveImage });
    expect(resolveImage).toHaveBeenCalledWith("hosted-uuid-1", "UUID");
    const media = await mediaNames(out);
    expect(media).toHaveLength(1);
  });

  it("reuses one media part for repeated identical sources", async () => {
    const out = await univerToDocx(imageDoc(pngDataUrl, "BASE64", 2));
    const zip = await JSZip.loadAsync(out);
    const media = Object.keys(zip.files).filter((n) => n.startsWith("word/media/") && !n.endsWith("/"));
    expect(media).toHaveLength(1);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    const embeds = [...xml.matchAll(/r:embed="(rId\d+)"/g)].map((m) => m[1]);
    expect(embeds).toHaveLength(2);
    expect(embeds[0]).toBe(embeds[1]);
  });

  it("reports embedded images so the next save reuses their media and relationship", async () => {
    const embedded: Record<string, { xml: string; width: number; height: number }> = {};
    const first = await univerToDocx(imageDoc("hosted-uuid-2", "UUID"), {
      resolveImage: vi.fn(async () => pngDataUrl),
      imageMetaOut: embedded,
    });
    // Keyed by the Univer drawing id, so the editor can match it on the next save.
    expect(Object.keys(embedded)).toEqual(["img0"]);

    const resolveAgain = vi.fn(async () => pngDataUrl);
    const second = await univerToDocx(imageDoc("hosted-uuid-2", "UUID"), {
      originalBytes: first,
      imageMeta: embedded,
      resolveImage: resolveAgain,
    });
    expect(resolveAgain).not.toHaveBeenCalled();
    expect(await mediaNames(second)).toHaveLength(1);
    const zip = await JSZip.loadAsync(second);
    const xml = (await zip.file("word/document.xml")?.async("string")) ?? "";
    expect(xml.match(/<w:drawing/g)).toHaveLength(1);
    const rels = (await zip.file("word/_rels/document.xml.rels")?.async("string")) ?? "";
    expect(rels.match(/relationships\/image"/g)).toHaveLength(1);
  });

  it("reports a failed fetch and still saves the rest of the document", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("network down"); }));
    try {
      const errors: string[] = [];
      const out = await univerToDocx(imageDoc("https://example.com/missing.png", "URL"), {
        onImageError: (message) => errors.push(message),
      });
      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain("missing.png");
      expect(await mediaNames(out)).toHaveLength(0);
      const zip = await JSZip.loadAsync(out);
      expect(zip.file("word/document.xml")).not.toBeNull();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("rejects a response that is not an image and reports it", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(new TextEncoder().encode("<html>nope</html>"), { status: 200 })));
    try {
      const errors: string[] = [];
      const out = await univerToDocx(imageDoc("https://example.com/page", "URL"), {
        onImageError: (message) => errors.push(message),
      });
      expect(errors[0]).toContain("not a supported image");
      expect(await mediaNames(out)).toHaveLength(0);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("throws for an unsupported source when no error handler is supplied", async () => {
    await expect(univerToDocx(imageDoc("javascript:alert(1)", "URL"))).rejects.toThrow(/Cannot save image/);
  });

  it("attaches the bearer token only to same-origin image fetches", () => {
    expect(
      imageRequestHeaders("http://localhost:3000/a.png", "http://localhost:3000", undefined, { Authorization: "Bearer t" }),
    ).toEqual({ Authorization: "Bearer t" });
    expect(
      imageRequestHeaders("https://cdn.example/a.png", "http://localhost:3000", { "X-A": "1" }, { Authorization: "Bearer t" }),
    ).toEqual({ "X-A": "1" });
  });

  it("rejects an SVG response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(new TextEncoder().encode('<svg xmlns="http://www.w3.org/2000/svg"></svg>'), { status: 200 })),
    );
    try {
      const errors: string[] = [];
      const out = await univerToDocx(imageDoc("https://example.com/x.svg", "URL"), {
        onImageError: (message) => errors.push(message),
      });
      expect(errors[0]).toContain("not a supported image");
      expect(await mediaNames(out)).toHaveLength(0);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("lists external hosts the save will download, skipping packaged images", () => {
    const data: any = {
      drawings: {
        a: { source: "https://a.example/x.png" },
        b: { source: "https://b.example/y.png" },
        c: { source: "https://ignored.example/z.png" },
      },
    };
    expect(externalImageHosts(data, { c: { xml: "<w:drawing/>" } })).toEqual(["a.example", "b.example"]);
  });
});
