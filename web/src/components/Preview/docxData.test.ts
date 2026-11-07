import { describe, it, expect } from "vitest";
import JSZip from "jszip";
import { docxToUniver, univerToDocx } from "./docxData";

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
      body: { dataStream: "Foo\r\nBar\r\n", paragraphs: [{ startIndex: 0, paragraphId: "p0" }, { startIndex: 5, paragraphId: "p1" }], textRuns: [] },
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
});
