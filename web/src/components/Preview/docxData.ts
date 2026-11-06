import JSZip from "jszip";
import type { IDocumentData } from "@univerjs/core";

function escapeXml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

export async function docxToUniver(bytes: Uint8Array): Promise<IDocumentData> {
  const zip = await JSZip.loadAsync(bytes);
  const xml = await zip.file("word/document.xml")?.async("string");
  if (!xml) throw new Error("docx has no word/document.xml");
  const parser = new DOMParser();
  const dom = parser.parseFromString(xml, "application/xml");
  const wNs = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";
  const paras = Array.from(dom.getElementsByTagNameNS(wNs, "p"));
  let stream = "";
  const paraStarts: number[] = [];
  for (const p of paras) {
    const runs = Array.from(p.getElementsByTagNameNS(wNs, "r"));
    let text = "";
    for (const r of runs) {
      const ts = Array.from(r.getElementsByTagNameNS(wNs, "t"));
      text += ts.map((t) => t.textContent ?? "").join("");
    }
    paraStarts.push(stream.length);
    stream += text + "\r\n";
  }
  if (paras.length === 0) {
    stream = "\r\n";
    paraStarts.push(0);
  }
  const paragraphs = paraStarts.map((st, i) => ({ startIndex: st, paragraphId: `p${i}` }));
  const textRuns = stream.length > 1 ? [{ st: 0, ed: stream.length - 1, ts: {} }] : [];
  return {
    id: `doc-${Date.now()}`,
    documentStyle: {},
    body: {
      dataStream: stream,
      paragraphs,
      textRuns,
    },
  } as IDocumentData;
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

export async function univerToDocx(data: IDocumentData): Promise<Uint8Array> {
  const stream = data.body?.dataStream ?? "";
  const lines = stream.split(/\r\n/);
  // drop trailing empty from final \r\n
  const paras = lines.filter((_, i) => i < lines.length - 1 || lines[i] !== "");
  const paraXml = paras
    .map((t) => `<w:p><w:r><w:t xml:space="preserve">${escapeXml(t)}</w:t></w:r></w:p>`)
    .join("");
  const docXml = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:body>
    ${paraXml}
    <w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>
  </w:body>
</w:document>`;

  const zip = new JSZip();
  zip.file("[Content_Types].xml", CONTENT_TYPES);
  zip.file("_rels/.rels", RELS);
  zip.file("word/_rels/document.xml.rels", DOC_RELS);
  zip.file("word/document.xml", docXml);
  const out = await zip.generateAsync({ type: "uint8array" });
  return out;
}
