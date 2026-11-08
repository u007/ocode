import { PDFDocument, StandardFonts, rgb } from "pdf-lib";

/** A rectangle in PDF points, origin bottom-left. */
export interface PdfRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/**
 * Draws a translucent yellow highlight rectangle on one page and returns the
 * new PDF bytes. This is a page-content operation (pdf-lib has no annotation
 * object API), so the mark is flattened into the page and cannot be un-drawn
 * by a PDF reader.
 */
/**
 * Maps a selection rectangle in canvas pixels (top-left origin) to PDF points
 * (bottom-left origin).
 *
 * When the pdf.js viewport `transform` is supplied, the mapping is exact for
 * any page rotation (the transform already includes rotation, scale and the
 * y-flip). Without it the unrotated formula is used.
 */
export function canvasToPdfRect(
  sel: { x: number; y: number; width: number; height: number },
  opts: { scale: number; pageHeight: number; transform?: number[] },
): PdfRect {
  const t = opts.transform;
  if (t && t.length === 6) {
    const [a, b, c, d, e, f] = t;
    const det = a * d - b * c;
    if (Math.abs(det) > 1e-9) {
      const toPdf = (vx: number, vy: number) => ({
        x: (d * (vx - e) - c * (vy - f)) / det,
        y: (-b * (vx - e) + a * (vy - f)) / det,
      });
      const p1 = toPdf(sel.x, sel.y);
      const p2 = toPdf(sel.x + sel.width, sel.y + sel.height);
      return {
        x: Math.min(p1.x, p2.x),
        y: Math.min(p1.y, p2.y),
        width: Math.abs(p2.x - p1.x),
        height: Math.abs(p2.y - p1.y),
      };
    }
  }
  const scale = opts.scale;
  return {
    x: sel.x / scale,
    y: opts.pageHeight - (sel.y + sel.height) / scale,
    width: sel.width / scale,
    height: sel.height / scale,
  };
}

export async function addHighlight(pdfBytes: Uint8Array, pageIndex: number, rect: PdfRect): Promise<Uint8Array> {
  const doc = await PDFDocument.load(pdfBytes);
  const page = doc.getPage(pageIndex);
  page.drawRectangle({
    x: rect.x,
    y: rect.y,
    width: rect.width,
    height: rect.height,
    color: rgb(1, 0.92, 0.2),
    opacity: 0.35,
    borderWidth: 0,
  });
  return doc.save();
}

/**
 * Draws a text note (a sticky-note-style label) at a page position and
 * returns the new PDF bytes.
 */
export async function addTextNote(
  pdfBytes: Uint8Array,
  pageIndex: number,
  x: number,
  y: number,
  text: string,
): Promise<Uint8Array> {
  const doc = await PDFDocument.load(pdfBytes);
  const font = await doc.embedFont(StandardFonts.Helvetica);
  const page = doc.getPage(pageIndex);
  page.drawText(text, { x, y, size: 11, font, color: rgb(0.1, 0.1, 0.1) });
  return doc.save();
}
