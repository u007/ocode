import JSZip from "jszip";
import type { IDocumentData } from "@univerjs/core";

function escapeXml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

interface RunStyle {
  bl?: number;
  it?: number;
  ul?: { s?: number } | number;
}

function rPrFromTs(ts: RunStyle | undefined): string {
  if (!ts) return "";
  const parts: string[] = [];
  if (ts.bl) parts.push("<w:b/>");
  if (ts.it) parts.push("<w:i/>");
  if (ts.ul) parts.push('<w:u w:val="single"/>');
  if (parts.length === 0) return "";
  return `<w:rPr>${parts.join("")}</w:rPr>`;
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
  const textRuns: { st: number; ed: number; ts: RunStyle }[] = [];
  for (const p of paras) {
    paraStarts.push(stream.length);
    const runs = Array.from(p.getElementsByTagNameNS(wNs, "r"));
    for (const r of runs) {
      const text = Array.from(r.getElementsByTagNameNS(wNs, "t"))
        .map((t) => t.textContent ?? "")
        .join("");
      if (text === "") continue;
      const rPr = r.getElementsByTagNameNS(wNs, "rPr")[0];
      const ts: RunStyle = {};
      if (rPr) {
        if (rPr.getElementsByTagNameNS(wNs, "b").length > 0) ts.bl = 1;
        if (rPr.getElementsByTagNameNS(wNs, "i").length > 0) ts.it = 1;
        if (rPr.getElementsByTagNameNS(wNs, "u").length > 0) ts.ul = { s: 1 };
      }
      const st = stream.length;
      stream += text;
      const ed = stream.length - 1;
      if (ts.bl || ts.it || ts.ul) textRuns.push({ st, ed, ts });
    }
    stream += "\r\n";
  }
  if (paras.length === 0) {
    stream = "\r\n";
    paraStarts.push(0);
  }
  const paragraphs = paraStarts.map((st, i) => {
    // re-derive alignment per paragraph (stored above not retained); recompute
    const p = paras[i];
    const pPr = p?.getElementsByTagNameNS(wNs, "pPr")[0];
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
    const entry: any = { startIndex: st, paragraphId: `p${i}` };
    if (hAlign !== undefined) entry.paragraphStyle = { horizontalAlign: hAlign };
    return entry;
  });
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
  const paragraphs = data.body?.paragraphs ?? [];
  const textRuns = data.body?.textRuns ?? [];

  // Determine paragraph text ranges [start, end] (inclusive) in the stream.
  const paraRanges: { start: number; end: number }[] = [];
  if (paragraphs.length > 0) {
    for (let i = 0; i < paragraphs.length; i++) {
      const start = paragraphs[i].startIndex;
      const nextStart = i + 1 < paragraphs.length ? paragraphs[i + 1].startIndex : stream.length + 2;
      // paragraph text ends 2 chars before the next paragraph start (the \r\n)
      const end = Math.max(start, Math.min(stream.length - 1, nextStart - 3));
      paraRanges.push({ start, end });
    }
  } else {
    let idx = 0;
    for (const part of stream.split(/\r\n/)) {
      if (part === "" && idx >= stream.length) break;
      paraRanges.push({ start: idx, end: idx + part.length - 1 });
      idx += part.length + 2;
    }
  }

  const paraXml = paraRanges
    .map(({ start, end }, idx) => {
      if (end < start) return "<w:p/>";
      const pText = stream.slice(start, end + 1);
      const para = paragraphs[idx];
      const hAlign = para?.paragraphStyle?.horizontalAlign;
      let jcVal: string | null = null;
      if (hAlign === 2) jcVal = "center";
      else if (hAlign === 3) jcVal = "right";
      else if (hAlign === 4) jcVal = "both";
      else if (hAlign === 1) jcVal = "left";
      else if (hAlign === 6) jcVal = "distribute";
      const pPr = jcVal ? `<w:pPr><w:jc w:val="${jcVal}"/></w:pPr>` : "";
      if (pText === "") return `<w:p>${pPr}</w:p>`;
      // build runs with formatting
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
    })
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
