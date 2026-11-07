import { describe, it, expect } from "vitest";
import { PDFDocument, degrees } from "pdf-lib";

describe("pdf-lib page ops", () => {
  it("rotates, deletes and reorders pages", async () => {
    const doc = await PDFDocument.create();
    doc.addPage(); doc.addPage(); doc.addPage();
    expect(doc.getPageCount()).toBe(3);

    // rotate first page
    doc.getPage(0).setRotation(degrees(90));
    expect(doc.getPage(0).getRotation().angle).toBe(90);

    // move page 2 to front
    const page = doc.getPages()[2];
    doc.removePage(2);
    doc.insertPage(0, page);
    expect(doc.getPageCount()).toBe(3);

    // delete last page
    doc.removePage(2);
    expect(doc.getPageCount()).toBe(2);

    const bytes = await doc.save();
    const reloaded = await PDFDocument.load(bytes);
    expect(reloaded.getPageCount()).toBe(2);
    expect(reloaded.getPage(1).getRotation().angle).toBe(90);
  });

  it("merges pages from another pdf", async () => {
    const a = await PDFDocument.create();
    a.addPage();
    const b = await PDFDocument.create();
    b.addPage(); b.addPage();
    const copied = await a.copyPages(b, b.getPageIndices());
    copied.forEach((p) => a.addPage(p));
    expect(a.getPageCount()).toBe(3);
    const bytes = await a.save();
    const reloaded = await PDFDocument.load(bytes);
    expect(reloaded.getPageCount()).toBe(3);
  });
});
