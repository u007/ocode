import { describe, it, expect } from "vitest";
import { PDFDocument } from "pdf-lib";
import { addHighlight, addTextNote, canvasToPdfRect } from "./pdfAnnotate";

async function makePdf(pages = 1): Promise<Uint8Array> {
  const doc = await PDFDocument.create();
  for (let i = 0; i < pages; i++) doc.addPage([400, 400]);
  return doc.save();
}

describe("pdfAnnotate", () => {
  it("maps canvas pixels to PDF points (bottom-left origin)", () => {
    const rect = canvasToPdfRect({ x: 30, y: 30, width: 60, height: 30 }, { scale: 0.3, pageHeight: 400 });
    expect(rect.x).toBeCloseTo(100);
    expect(rect.y).toBeCloseTo(200);
    expect(rect.width).toBeCloseTo(200);
    expect(rect.height).toBeCloseTo(100);
  });


  it("maps a rotated page through the pdf.js viewport transform", () => {
    // 400x400 page rotated 90deg at scale 0.3: pdf.js transform is [0,0.3,0.3,0,0,0].
    const rect = canvasToPdfRect(
      { x: 30, y: 30, width: 60, height: 30 },
      { scale: 0.3, pageHeight: 400, transform: [0, 0.3, 0.3, 0, 0, 0] },
    );
    expect(rect.x).toBeCloseTo(100);
    expect(rect.y).toBeCloseTo(100);
    expect(rect.width).toBeCloseTo(100);
    expect(rect.height).toBeCloseTo(200);
  });

  it("maps a 180deg page through the transform", () => {
    // 400x400 page rotated 180deg at scale 0.3: transform [-0.3,0,0,0.3,120,0].
    const rect = canvasToPdfRect(
      { x: 30, y: 30, width: 60, height: 30 },
      { scale: 0.3, pageHeight: 400, transform: [-0.3, 0, 0, 0.3, 120, 0] },
    );
    expect(rect.x).toBeCloseTo(100);
    expect(rect.y).toBeCloseTo(100);
    expect(rect.width).toBeCloseTo(200);
    expect(rect.height).toBeCloseTo(100);
  });

  it("maps a 270deg page through the transform", () => {
    // 400x400 page rotated 270deg at scale 0.3: transform [0,-0.3,-0.3,0,120,120].
    const rect = canvasToPdfRect(
      { x: 30, y: 30, width: 60, height: 30 },
      { scale: 0.3, pageHeight: 400, transform: [0, -0.3, -0.3, 0, 120, 120] },
    );
    expect(rect.x).toBeCloseTo(200);
    expect(rect.y).toBeCloseTo(100);
    expect(rect.width).toBeCloseTo(100);
    expect(rect.height).toBeCloseTo(200);
  });

  it("maps through a transform with a non-zero MediaBox origin", () => {
    // MediaBox [10,20,410,420] at scale 0.3: pdf.js transform [0.3,0,0,-0.3,-3,126].
    const rect = canvasToPdfRect(
      { x: 30, y: 30, width: 60, height: 30 },
      { scale: 0.3, pageHeight: 400, transform: [0.3, 0, 0, -0.3, -3, 126] },
    );
    expect(rect.x).toBeCloseTo(110);
    expect(rect.y).toBeCloseTo(220);
    expect(rect.width).toBeCloseTo(200);
    expect(rect.height).toBeCloseTo(100);
  });

  it("adds a highlight and keeps the page count", async () => {
    const before = await makePdf(1);
    const after = await addHighlight(before, 0, { x: 10, y: 20, width: 100, height: 30 });
    const doc = await PDFDocument.load(after);
    expect(doc.getPageCount()).toBe(1);
    // content stream grew (drawing appended)
    expect(after.byteLength).toBeGreaterThan(before.byteLength);
  });

  it("adds a text note", async () => {
    const before = await makePdf(2);
    const after = await addTextNote(before, 1, 50, 60, "Review this");
    const doc = await PDFDocument.load(after);
    expect(doc.getPageCount()).toBe(2);
    expect(after.byteLength).toBeGreaterThan(before.byteLength);
  });

  it("throws for an out-of-range page", async () => {
    const before = await makePdf(1);
    await expect(addHighlight(before, 5, { x: 0, y: 0, width: 1, height: 1 })).rejects.toThrow();
  });
});
