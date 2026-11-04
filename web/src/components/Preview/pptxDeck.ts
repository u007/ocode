import JSZip from "jszip";
import { imageMimeForPath } from "../../lib/previewKind";

// Reads a .pptx package into slides. Shared by PptxViewer (on-screen cards)
// and the print document, so both show the same text, images and order.

export interface DeckBlock {
  text: string;
}

export interface DeckSlide {
  index: number;
  blocks: DeckBlock[];
  /** Raw image bytes. The type comes from the extension; empty when unknown, which browsers sniff for rasters. */
  images: Blob[];
  aspect: number;
}

function xmlDoc(text: string): Document {
  return new DOMParser().parseFromString(text, "application/xml");
}

/** Resolve a rels Target (e.g. "../media/image1.png") against its base dir. */
function resolveTarget(baseDir: string, target: string): string {
  const parts = `${baseDir}/${target}`.split("/");
  const out: string[] = [];
  for (const p of parts) {
    if (p === "..") out.pop();
    else if (p !== ".") out.push(p);
  }
  return out.join("/");
}

/** Parses the deck in `buf` in slide order. Throws when the package has no slides. */
export async function parsePptx(buf: ArrayBuffer): Promise<DeckSlide[]> {
  const zip = await JSZip.loadAsync(buf);

  const presXml = await zip.file("ppt/presentation.xml")?.async("string");
  if (!presXml) throw new Error("not a readable .pptx (missing presentation.xml)");
  const pres = xmlDoc(presXml);

  let aspect = 16 / 9;
  const sz = pres.getElementsByTagName("p:sldSz")[0];
  if (sz) {
    const cx = parseFloat(sz.getAttribute("cx") || "0");
    const cy = parseFloat(sz.getAttribute("cy") || "0");
    if (cx > 0 && cy > 0) aspect = cx / cy;
  }

  const relsXml = await zip.file("ppt/_rels/presentation.xml.rels")?.async("string");
  const rels = relsXml ? xmlDoc(relsXml) : null;
  const idToTarget = new Map<string, string>();
  if (rels) {
    const r = rels.getElementsByTagName("Relationship");
    for (let i = 0; i < r.length; i++) {
      const id = r[i].getAttribute("Id") || "";
      const target = r[i].getAttribute("Target") || "";
      if (id && target) idToTarget.set(id, target.startsWith("ppt/") ? target : `ppt/${target}`);
    }
  }

  const order: string[] = [];
  const sldIds = pres.getElementsByTagName("p:sldId");
  for (let i = 0; i < sldIds.length; i++) {
    const rid = sldIds[i].getAttribute("r:id") || "";
    const target = idToTarget.get(rid);
    if (target) order.push(target);
  }
  if (order.length === 0) throw new Error("no slides found in deck");

  const parsed: DeckSlide[] = [];
  for (let si = 0; si < order.length; si++) {
    const slidePath = order[si];
    const slideXml = await zip.file(slidePath)?.async("string");
    if (!slideXml) continue;
    const doc = xmlDoc(slideXml);
    const blocks: DeckBlock[] = [];
    const paras = doc.getElementsByTagName("a:p");
    for (let pi = 0; pi < paras.length; pi++) {
      const runs = paras[pi].getElementsByTagName("a:t");
      let text = "";
      for (let ti = 0; ti < runs.length; ti++) text += runs[ti].textContent ?? "";
      text = text.trim();
      if (text) blocks.push({ text });
    }

    const images: Blob[] = [];
    const baseDir = slidePath.slice(0, slidePath.lastIndexOf("/"));
    const relName = slidePath.slice(slidePath.lastIndexOf("/") + 1);
    const slideRels = await zip.file(`${baseDir}/_rels/${relName}.rels`)?.async("string");
    if (slideRels) {
      const rd = xmlDoc(slideRels);
      const rr = rd.getElementsByTagName("Relationship");
      for (let i = 0; i < rr.length; i++) {
        const type = rr[i].getAttribute("Type") || "";
        const target = rr[i].getAttribute("Target") || "";
        if (!type.endsWith("/image") || !target) continue;
        const resolved = target.startsWith("..") ? resolveTarget(baseDir, target) : `${baseDir}/${target}`;
        const file = zip.file(resolved);
        if (!file) continue;
        const bytes = await file.async("arraybuffer");
        images.push(new Blob([bytes], { type: imageMimeForPath(resolved) ?? "" }));
      }
    }
    parsed.push({ index: si + 1, blocks, images, aspect });
  }
  return parsed;
}
